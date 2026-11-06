package adapter

import (
	"context"
	"encoding/json"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// auditPath is the endpoint that stores a write-confirmation record.
const auditPath = "/api/service-ai/cli-confirm/report"

// ConfirmReport is this product's write-confirmation audit sink.
//
// It sends the same shape as any other signed write here — POST with a JSON body,
// api-key identity, cli HMAC — so it needs no scheme, identity or host of its
// own; spot.marketv2's signed reads already run that shape in production.
//
// The acting user is NOT in the payload: they are established by the api key the
// request already carries, so this CLI cannot misreport who acted.
type ConfirmReport struct{ send transport.Sender }

// NewConfirmReport builds the sink over this invocation's Sender.
func NewConfirmReport(send transport.Sender) confirmaudit.Reporter {
	return ConfirmReport{send: send}
}

// reportBody is the request body. Field names are the endpoint's; extraInfo
// carries the rest as a JSON string.
type reportBody struct {
	ConfirmStage  string `json:"confirmStage"`
	ConfirmKey    string `json:"confirmKey"`
	InvocationID  string `json:"invocationId"`
	Command       string `json:"command"`
	RequestPath   string `json:"requestPath"`
	RequestMethod string `json:"requestMethod"`
	APIDesc       string `json:"apiDesc,omitempty"`
	CLIVersion    string `json:"cliVersion,omitempty"`
	ExtraInfo     string `json:"extraInfo,omitempty"`
}

// extra is what extraInfo carries. channel and interactive are the two the audit
// reads: together they separate a caller that relayed the prompt from one that
// approved on its own.
type extra struct {
	Channel     string `json:"channel"`
	Interactive bool   `json:"interactive"`
	RiskTier    string `json:"riskTier,omitempty"`
	Face        string `json:"face,omitempty"`
	AgentSource string `json:"agentSource,omitempty"`
	Env         string `json:"env,omitempty"`
}

func (r ConfirmReport) Report(ctx context.Context, ev confirmaudit.Event) error {
	body, err := json.Marshal(reportBody{
		ConfirmStage:  string(ev.Stage),
		ConfirmKey:    ev.ConfirmKey,
		InvocationID:  ev.InvocationID,
		Command:       ev.Command,
		RequestPath:   ev.RequestPath,
		RequestMethod: ev.RequestMethod,
		APIDesc:       ev.APIDesc,
		CLIVersion:    ev.CLIVersion,
		ExtraInfo:     extraInfo(ev),
	})
	if err != nil {
		return err
	}
	// Not a mounted api.Operation: it is a POST, so the confirmation gate would
	// stop it on the way to reporting a confirmation, and it would show up in
	// `schema` and the MCP tool list as something a caller may invoke.
	_, err = r.send.Send(ctx, api.RequestSpec{
		Method:   "POST",
		Path:     auditPath,
		Query:    map[string]string{},
		Header:   map[string]string{"Content-Type": "application/json"},
		Body:     body,
		HostKey:  KeyOpenAPI,
		Signed:   signing.CliHMAC,
		Identity: identity.OpenAPI,
		// Safe to retry: the record names the judgement and the stage it
		// describes, so a re-send is the same record rather than a second
		// event, and the transport re-signs each attempt.
		Idempotent: true,
	}, nil)
	return err
}

// extraInfo renders the fields carried as JSON rather than as body fields. A
// marshal failure yields "" rather than failing the report: the record without
// them is still worth sending.
func extraInfo(ev confirmaudit.Event) string {
	b, err := json.Marshal(extra{
		Channel:     string(ev.Channel),
		Interactive: ev.Interactive,
		RiskTier:    ev.RiskTier,
		Face:        ev.Face,
		AgentSource: ev.AgentSource,
		Env:         ev.Env,
	})
	if err != nil {
		return ""
	}
	return string(b)
}
