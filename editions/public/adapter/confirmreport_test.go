package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// captureSender records the spec it is handed instead of sending it.
type captureSender struct {
	spec api.RequestSpec
	err  error
	n    int
}

func (s *captureSender) Send(_ context.Context, spec api.RequestSpec, _ any) (*transport.Plan, error) {
	s.n++
	s.spec = spec
	return &transport.Plan{}, s.err
}

func (s *captureSender) IsDryRun() bool { return false }

func sampleEvent() confirmaudit.Event {
	return confirmaudit.Event{
		Stage:         confirmaudit.StageConfirmed,
		Channel:       confirmaudit.ChannelAssumeYes,
		ConfirmKey:    "9f3a1c72d4e08b55",
		InvocationID:  "4d90fa1372ab5c60a7e1b3c2d5f80916",
		Command:       "mexc spot trade place --symbol BTCUSDT",
		RequestPath:   "/api/v3/order",
		RequestMethod: "POST",
		APIDesc:       "Place a spot order",
		CLIVersion:    "1.4.2",
		RiskTier:      "heavy",
		Face:          "cli",
		Interactive:   false,
		AgentSource:   "claude-code",
		Env:           "prod",
	}
}

func TestConfirmReport_RequestShape(t *testing.T) {
	s := &captureSender{}
	if err := NewConfirmReport(s).Report(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if s.n != 1 {
		t.Fatalf("sent %d requests, want 1", s.n)
	}
	spec := s.spec

	if spec.Method != "POST" || spec.Path != auditPath {
		t.Errorf("target = %s %s, want POST %s", spec.Method, spec.Path, auditPath)
	}
	// The product's existing axes: the gateway needs the api key to resolve who
	// acted, so this is authenticated as the acting user like any other write.
	if spec.Identity != identity.OpenAPI {
		t.Errorf("Identity = %q, want %q", spec.Identity, identity.OpenAPI)
	}
	if spec.Signed != signing.CliHMAC {
		t.Errorf("Signed = %q, want %q", spec.Signed, signing.CliHMAC)
	}
	if spec.HostKey != KeyOpenAPI {
		t.Errorf("HostKey = %q, want %q", spec.HostKey, KeyOpenAPI)
	}
	if spec.Header["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", spec.Header["Content-Type"])
	}
	// A re-send of one judgement's stage is the same record, not a second event, so
	// a transient failure may be retried.
	if !spec.Idempotent {
		t.Error("the report should be retryable")
	}
}

func TestConfirmReport_Body(t *testing.T) {
	s := &captureSender{}
	ev := sampleEvent()
	if err := NewConfirmReport(s).Report(context.Background(), ev); err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	var got reportBody
	if err := json.Unmarshal(s.spec.Body, &got); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, s.spec.Body)
	}
	want := reportBody{
		ConfirmStage:  "CONFIRMED",
		ConfirmKey:    ev.ConfirmKey,
		InvocationID:  ev.InvocationID,
		Command:       ev.Command,
		RequestPath:   ev.RequestPath,
		RequestMethod: ev.RequestMethod,
		APIDesc:       ev.APIDesc,
		CLIVersion:    ev.CLIVersion,
	}
	got.ExtraInfo = "" // asserted separately
	if got != want {
		t.Errorf("body =\n%+v\nwant\n%+v", got, want)
	}
}

// The body carries EXACTLY these keys and nothing else. Closed rather than a list
// of forbidden names: the caller must not be able to name the acting user here —
// that is established by the api key — and a closed set catches any future field
// that would let it, without having to predict what such a field is called.
func TestConfirmReport_BodyKeysAreClosed(t *testing.T) {
	s := &captureSender{}
	if err := NewConfirmReport(s).Report(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(s.spec.Body, &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	allowed := map[string]bool{
		"confirmStage": true, "confirmKey": true, "invocationId": true,
		"command": true, "requestPath": true, "requestMethod": true,
		"apiDesc": true, "cliVersion": true, "extraInfo": true,
	}
	for k := range raw {
		if !allowed[k] {
			t.Errorf("body carries an undeclared key %q", k)
		}
	}
	for k := range allowed {
		if _, ok := raw[k]; !ok {
			t.Errorf("body is missing %q", k)
		}
	}
}

func TestConfirmReport_ExtraInfo(t *testing.T) {
	s := &captureSender{}
	ev := sampleEvent()
	if err := NewConfirmReport(s).Report(context.Background(), ev); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	var body reportBody
	if err := json.Unmarshal(s.spec.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	var got extra
	if err := json.Unmarshal([]byte(body.ExtraInfo), &got); err != nil {
		t.Fatalf("extraInfo is not JSON: %v (%s)", err, body.ExtraInfo)
	}
	want := extra{
		Channel: "assume-yes", Interactive: false,
		RiskTier: "heavy", Face: "cli", AgentSource: "claude-code", Env: "prod",
	}
	if got != want {
		t.Errorf("extraInfo = %+v, want %+v", got, want)
	}
}

// extraInfo travels as a JSON string in one field, so it has to stay compact: it
// is the one field here whose size grows with what the record carries, and a
// truncated JSON string loses every field in it, not just the last. Keep it small.
func TestConfirmReport_ExtraInfoStaysBounded(t *testing.T) {
	s := &captureSender{}
	if err := NewConfirmReport(s).Report(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	var body reportBody
	if err := json.Unmarshal(s.spec.Body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if n := len(body.ExtraInfo); n > 512 {
		t.Fatalf("extraInfo is %d bytes; keep it compact so it survives intact", n)
	}
}

func TestConfirmReport_EveryStageAndChannelSurvives(t *testing.T) {
	stages := []confirmaudit.Stage{confirmaudit.StageRemind, confirmaudit.StageConfirmed, confirmaudit.StageCancelled}
	channels := []confirmaudit.Channel{
		confirmaudit.ChannelTTY, confirmaudit.ChannelRefusal,
		confirmaudit.ChannelElicitation, confirmaudit.ChannelAssumeYes,
	}
	for _, st := range stages {
		for _, ch := range channels {
			s := &captureSender{}
			ev := sampleEvent()
			ev.Stage, ev.Channel = st, ch
			if err := NewConfirmReport(s).Report(context.Background(), ev); err != nil {
				t.Fatalf("%s/%s: %v", st, ch, err)
			}
			var body reportBody
			if err := json.Unmarshal(s.spec.Body, &body); err != nil {
				t.Fatalf("%s/%s: body is not JSON: %v", st, ch, err)
			}
			if body.ConfirmStage != string(st) {
				t.Errorf("%s/%s: confirmStage = %q", st, ch, body.ConfirmStage)
			}
			if !strings.Contains(body.ExtraInfo, string(ch)) {
				t.Errorf("%s/%s: extraInfo lost the channel: %s", st, ch, body.ExtraInfo)
			}
		}
	}
}

// The caller discards this, but it must be returned rather than swallowed here —
// swallowing would make an endpoint that rejects everything look healthy.
func TestConfirmReport_SendFailureIsReturned(t *testing.T) {
	s := &captureSender{err: errors.New("gateway said no")}
	if err := NewConfirmReport(s).Report(context.Background(), sampleEvent()); err == nil {
		t.Fatal("a send failure must be returned")
	}
}
