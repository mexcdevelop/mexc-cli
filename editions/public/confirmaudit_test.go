package public

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
)

// End-to-end: assemble this edition, run a real write through it, and assert what
// the audit endpoint actually receives. The reporter's own unit tests assert the
// spec it builds; this asserts the spec survives the transport — that the record
// is signed, carries the api key, and lands on the audit path.

// capturedReport is one request the fake gateway received. arrival is its position
// in the stream of ALL requests, business writes included, so a test can assert
// what the record came after.
type capturedReport struct {
	path      string
	apiKey    string
	signature string
	body      map[string]any
	arrival   int
}

// fakeGateway stands in for the openapi gateway. It answers every path with the
// endpoint's success envelope and records what arrived, in order.
type fakeGateway struct {
	mu       sync.Mutex
	arrivals int
	reports  []capturedReport
	// writeArrivals holds the arrival position of each business write.
	writeArrivals []int
}

func (g *fakeGateway) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.arrivals++
		if strings.Contains(r.URL.Path, "cli-confirm/report") {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			g.reports = append(g.reports, capturedReport{
				path:      r.URL.Path,
				apiKey:    r.Header.Get("X-MEXC-APIKEY"),
				signature: r.Header.Get("X-MEXC-CLI-SIGNATURE"),
				body:      body,
				arrival:   g.arrivals,
			})
		} else {
			g.writeArrivals = append(g.writeArrivals, g.arrivals)
		}
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{}}`))
	})
}

func (g *fakeGateway) captured() []capturedReport {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]capturedReport(nil), g.reports...)
}

func (g *fakeGateway) writes() []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]int(nil), g.writeArrivals...)
}

// withCredentials provisions a saved profile so the write clears the credential
// precheck, which runs BEFORE the confirmation gate.
func withCredentials(t *testing.T) {
	t.Helper()
	isolatedHome(t)
	if err := profile.Save(profile.Profile{Name: "audit", APIKey: "test-key", SecretKey: "test-secret"}); err != nil {
		t.Fatalf("save profile: %v", err)
	}
	if err := profile.SetActive("audit"); err != nil {
		t.Fatalf("activate profile: %v", err)
	}
}

func placeArgs(extra ...string) []string {
	return append([]string{
		"spot", "trade", "place",
		"--data", `{"currency":"BTC","market":"USDT","price":"90000","quantity":"0.001","tradeType":"BUY","orderSource":"CLI"}`,
	}, extra...)
}

func TestConfirmAudit_ReportReachesTheGateway(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	// --yes: the caller asserts the user confirmed out of band, which is the case
	// that reports a confirmation with no reminder.
	_, stderr, code := run(nil, placeArgs("--yes", "--base-url", srv.URL)...)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}

	reports := g.captured()
	if len(reports) != 1 {
		t.Fatalf("gateway received %d reports, want 1", len(reports))
	}
	got := reports[0]

	// The exact path is pinned against the constant in the reporter's own test; here
	// it only has to be a path of its own, not the operation's.
	if got.path == "/api/cli/spot/order/place" {
		t.Errorf("the record went to the business path: %q", got.path)
	}
	// Without these the record cannot be attributed to anyone.
	if got.apiKey != "test-key" {
		t.Errorf("X-MEXC-APIKEY = %q, want the profile's key", got.apiKey)
	}
	if got.signature == "" {
		t.Error("the report must be signed")
	}
	if got.body["confirmStage"] != "CONFIRMED" {
		t.Errorf("confirmStage = %v, want CONFIRMED", got.body["confirmStage"])
	}
	for _, field := range []string{"confirmKey", "invocationId", "command", "requestPath", "requestMethod"} {
		if v, _ := got.body[field].(string); v == "" {
			t.Errorf("%s is empty in the record", field)
		}
	}
	// Not synthesised: a confirmation with no reminder is the signal that the
	// caller approved on its own.
	if got.body["requestPath"] != "/api/cli/spot/order/place" {
		t.Errorf("requestPath = %v, want the path template", got.body["requestPath"])
	}
	var ex struct {
		Channel     string `json:"channel"`
		Interactive bool   `json:"interactive"`
	}
	if err := json.Unmarshal([]byte(got.body["extraInfo"].(string)), &ex); err != nil {
		t.Fatalf("extraInfo is not JSON: %v", err)
	}
	if ex.Channel != "assume-yes" {
		t.Errorf("channel = %q, want assume-yes", ex.Channel)
	}
	if ex.Interactive {
		t.Error("interactive must be false with no controlling terminal")
	}
}

// The trail must not add lines to the user's stderr. A command now sends twice
// against --base-url — its own request and the record — and the transport's
// overridden-host advisory used to be narrated once per send, which reads as two
// separate problems with one command.
func TestConfirmAudit_ReportAddsNoAdvisoryNoise(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	_, stderr, code := run(nil, placeArgs("--yes", "--base-url", srv.URL)...)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(g.captured()) != 1 {
		t.Fatalf("reports = %d, want 1 (the advisory count below assumes the record was sent)", len(g.captured()))
	}
	// Exactly one: still stated, but stated once. Zero would mean the advisory
	// regressed into silence, which is the opposite failure.
	if n := strings.Count(stderr, "overridden host"); n != 1 {
		t.Errorf("the overridden-host advisory appeared %d times, want 1:\n%s", n, stderr)
	}
}

// The record must not leak the credentials the request is signed with.
func TestConfirmAudit_ReportLeaksNoSecret(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	_, _, _ = run(nil, placeArgs("--yes", "--base-url", srv.URL)...)
	reports := g.captured()
	if len(reports) != 1 {
		t.Fatalf("gateway received %d reports, want 1", len(reports))
	}
	raw, _ := json.Marshal(reports[0].body)
	if strings.Contains(string(raw), "test-secret") {
		t.Fatalf("the record leaked the signing secret:\n%s", raw)
	}
}

func TestConfirmAudit_RefusedWriteReportsAReminder(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	// No --yes and no terminal: the gate refuses, and the reminder must still be
	// reported — this path returns straight into process exit.
	_, _, code := run(nil, placeArgs("--base-url", srv.URL)...)
	if code == 0 {
		t.Fatal("a write with no --yes and no terminal must be refused")
	}
	reports := g.captured()
	if len(reports) != 1 {
		t.Fatalf("gateway received %d reports, want 1", len(reports))
	}
	if reports[0].body["confirmStage"] != "REMIND" {
		t.Errorf("confirmStage = %v, want REMIND", reports[0].body["confirmStage"])
	}
	if n := len(g.writes()); n != 0 {
		t.Errorf("the business write was sent %d times; it must not run", n)
	}
}

// The rule the whole design rests on, asserted on the wire: the record must reach
// the endpoint AFTER the order did.
//
// Without this the report can be moved back ahead of the write and every other test
// in the suite still passes. The interactive half of the rule is pinned in
// internal/cli, where the prompt can be driven (TestConfirmGate_SendsNothingOnTheAcceptedPath).
func TestConfirmAudit_RecordArrivesAfterTheWrite(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	if _, stderr, code := run(nil, placeArgs("--yes", "--base-url", srv.URL)...); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}

	writes := g.writes()
	if len(writes) != 1 {
		t.Fatalf("business writes = %d, want 1", len(writes))
	}
	reports := g.captured()
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
	if reports[0].arrival < writes[0] {
		t.Errorf("the record arrived at position %d, BEFORE the write at %d — the trail must never run ahead of what it records",
			reports[0].arrival, writes[0])
	}
}

// The refusal and the --yes retry are separate processes, joined only by
// confirmKey — which is what the audit's pairing depends on.
func TestConfirmAudit_RefusalAndRetrySharePairingKey(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	_, _, _ = run(nil, placeArgs("--base-url", srv.URL)...)
	_, _, _ = run(nil, placeArgs("--yes", "--base-url", srv.URL)...)

	reports := g.captured()
	if len(reports) != 2 {
		t.Fatalf("gateway received %d reports, want 2", len(reports))
	}
	remind, confirmed := reports[0].body, reports[1].body
	if remind["confirmStage"] != "REMIND" || confirmed["confirmStage"] != "CONFIRMED" {
		t.Fatalf("stages = %v, %v", remind["confirmStage"], confirmed["confirmStage"])
	}
	if remind["confirmKey"] != confirmed["confirmKey"] {
		t.Errorf("confirmKey must match: %v vs %v", remind["confirmKey"], confirmed["confirmKey"])
	}
	if remind["invocationId"] == confirmed["invocationId"] {
		t.Error("two processes must not share a judgement id")
	}
}

func TestConfirmAudit_DryRunAndReadsReportNothing(t *testing.T) {
	withCredentials(t)
	g := &fakeGateway{}
	srv := httptest.NewServer(g.handler())
	defer srv.Close()

	if _, _, code := run(nil, placeArgs("--dry-run", "--base-url", srv.URL)...); code != 0 {
		t.Fatalf("dry-run exit = %d", code)
	}
	if _, _, code := run(nil, "activity", "online", "category-list", "--base-url", srv.URL); code != 0 {
		t.Fatalf("read exit = %d", code)
	}
	if n := len(g.captured()); n != 0 {
		t.Fatalf("gateway received %d reports, want none", n)
	}
}

// A sink that fails must not stop the write: the trail is evidence, not a gate.
func TestConfirmAudit_SinkFailureDoesNotBlockTheWrite(t *testing.T) {
	withCredentials(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "cli-confirm/report") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":1002,"msg":"system error"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{}}`))
	}))
	defer srv.Close()

	_, stderr, code := run(nil, placeArgs("--yes", "--base-url", srv.URL)...)
	if code != 0 {
		t.Fatalf("a failing audit sink must not fail the write: exit %d, stderr %s", code, stderr)
	}
}
