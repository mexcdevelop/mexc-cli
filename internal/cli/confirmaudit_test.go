package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// fakeReporter records events instead of sending them, and can fail on demand.
type fakeReporter struct {
	mu           sync.Mutex
	events       []confirmaudit.Event
	err          error
	sawDeadlines []bool
}

func (r *fakeReporter) Report(ctx context.Context, ev confirmaudit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	_, ok := ctx.Deadline()
	r.sawDeadlines = append(r.sawDeadlines, ok)
	return r.err
}

func (r *fakeReporter) all() []confirmaudit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]confirmaudit.Event(nil), r.events...)
}

// auditFactory is the minimum Factory the reporting path reads.
func auditFactory(rep confirmaudit.Reporter) *Factory {
	return &Factory{
		Config:          func() config.Config { return config.Config{Env: "prod"} },
		Env:             func(string) string { return "" },
		ConfirmReporter: func() confirmaudit.Reporter { return rep },
		Out:             io.Discard,
		Err:             io.Discard,
	}
}

// stubTTY drives the terminal branches without a pty.
// stubTTY makes the caller ATTENDED and the prompt answerable. Forcing stdio is
// not decoration: under `go test` the real stdio is a pipe, so leaving it alone
// would send every one of these cases down the unattended refusal and quietly
// stop testing the interactive path at all.
//
// The prompt is stubbed rather than run because it renders on the real stdout and
// reads the real stdin now. A test that let it start would block on input nobody
// sends, which is precisely the failure this package spent a commit removing.
func stubTTY(t *testing.T, confirmed, asked bool) {
	t.Helper()
	origStdio, origRun := stdioIsTerminalFn, runConfirmTUIFn
	stdioIsTerminalFn = func() bool { return true }
	runConfirmTUIFn = func(string, string, api.RiskTier) (bool, bool) { return confirmed, asked }
	t.Cleanup(func() {
		stdioIsTerminalFn, runConfirmTUIFn = origStdio, origRun
	})
}

// stubNoTTY makes the caller UNATTENDED: its own streams are not a terminal, so
// the gate refuses before anything is rendered. That is the agent's shape and,
// since the prompt stopped opening a device, the only way left to be
// unpromptable.
func stubNoTTY(t *testing.T) {
	t.Helper()
	orig := stdioIsTerminalFn
	stdioIsTerminalFn = func() bool { return false }
	t.Cleanup(func() { stdioIsTerminalFn = orig })
}

// runGate drives the gate and then does what the caller does on its way out, so a
// test sees the whole path's records rather than only the gate's.
func runGate(f *Factory, base confirmaudit.Event, op api.Operation, values map[string]string, data string, assumeYes bool) error {
	ch, err := confirmForCLI(f, base, op, values, data, assumeYes, credential.ConfirmGrade{})
	if err == nil {
		reportConfirmed(f, base, ch)
	}
	return err
}

// stages projects recorded events to the ORDERED sequence of "STAGE/channel".
//
// Ordered, because every send is synchronous on the caller's goroutine: within one
// judgement the sequence is fully determined, so asserting it pins the "record only
// what has already happened" rule rather than just the set of records.
func stages(evs []confirmaudit.Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, string(e.Stage)+"/"+string(e.Channel))
	}
	return out
}

func writeOp() api.Operation {
	return api.Operation{
		Name: "place", Method: "POST", Path: "/api/v3/order", Access: api.Write,
		Summary: "Place a spot order",
		Params:  []api.Param{{Name: "symbol"}, {Name: "side"}},
	}
}

// --- what each gate outcome reports -----------------------------------------

func TestConfirmAudit_GateOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		op        api.Operation
		assumeYes bool
		setup     func(*testing.T)
		want      []string
	}{{
		// A read never reaches the gate, so it leaves no trace.
		name: "read reports nothing",
		op:   api.Operation{Name: "price", Access: api.Read},
		want: nil,
	}, {
		// The decisive case: CONFIRMED with no REMIND is how a self-approving caller
		// is found, so the missing half must not be synthesised.
		name:      "assume-yes reports confirmed only",
		op:        writeOp(),
		assumeYes: true,
		want:      []string{"CONFIRMED/assume-yes"},
	}, {
		name:  "no terminal reports remind as a refusal",
		op:    writeOp(),
		setup: func(t *testing.T) { stubNoTTY(t) },
		want:  []string{"REMIND/refusal"},
	}, {
		// Nothing was rendered and nothing answered, and the caller gets the same
		// refusal as a caller with no terminal — so the record says refusal too,
		// which is what lets a later --yes pair against it.
		name:  "prompt could not run reports a refusal",
		op:    writeOp(),
		setup: func(t *testing.T) { stubTTY(t, false, false) },
		want:  []string{"REMIND/refusal"},
	}, {
		// An explicit refusal gets its own stage: a reminder with no confirmation is
		// also what an unanswered prompt looks like.
		name:  "cancel reports a cancellation",
		op:    writeOp(),
		setup: func(t *testing.T) { stubTTY(t, false, true) },
		want:  []string{"REMIND/tty", "CANCELLED/tty"},
	}, {
		// Both stages, in the order they happened, and both on the way out: the
		// reminder is deliberately NOT sent from inside the gate, where it would sit
		// between the user's approval and the write.
		name:  "confirmed at the prompt reports both stages",
		op:    writeOp(),
		setup: func(t *testing.T) { stubTTY(t, true, true) },
		want:  []string{"REMIND/tty", "CONFIRMED/tty"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup(t)
			}
			rep := &fakeReporter{}
			f := auditFactory(rep)
			base := auditEvent(f, tt.op, map[string]string{"symbol": "BTCUSDT"}, "", "mexc-cli spot trade place", faceCLI, credential.ConfirmGrade{})

			_ = runGate(f, base, tt.op, map[string]string{"symbol": "BTCUSDT"}, "", tt.assumeYes)

			got := stages(rep.all())
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("reported %v, want %v", got, tt.want)
			}
			for _, e := range rep.all() {
				if e.InvocationID != base.InvocationID {
					t.Errorf("event lost the judgement id: %q, want %q", e.InvocationID, base.InvocationID)
				}
				if e.ConfirmKey != base.ConfirmKey {
					t.Errorf("event lost the confirm key: %q, want %q", e.ConfirmKey, base.ConfirmKey)
				}
			}
		})
	}
}

// One judgement reports a given stage once. A repeat is the same record, not a
// correction, so a first record that turned out to be wrong cannot be taken back
// by a second — the gate has to be right the first time.
func TestConfirmAudit_OneRecordPerStagePerJudgement(t *testing.T) {
	outcomes := []struct {
		name      string
		assumeYes bool
		setup     func(*testing.T)
	}{
		{"assume-yes", true, nil},
		{"no terminal", false, func(t *testing.T) { stubNoTTY(t) }},
		{"prompt could not run", false, func(t *testing.T) { stubTTY(t, false, false) }},
		{"cancelled", false, func(t *testing.T) { stubTTY(t, false, true) }},
		{"confirmed", false, func(t *testing.T) { stubTTY(t, true, true) }},
	}
	for _, o := range outcomes {
		t.Run(o.name, func(t *testing.T) {
			if o.setup != nil {
				o.setup(t)
			}
			rep := &fakeReporter{}
			f := auditFactory(rep)
			op := writeOp()
			base := auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{})
			_ = runGate(f, base, op, nil, "", o.assumeYes)

			if len(rep.all()) == 0 {
				t.Fatal("nothing was reported; the invariant below would pass vacuously")
			}
			seen := map[string]int{}
			for _, e := range rep.all() {
				seen[e.InvocationID+"/"+string(e.Stage)]++
			}
			for k, n := range seen {
				if n > 1 {
					t.Errorf("%s reported %d times; a stage is recorded once per judgement", k, n)
				}
			}
		})
	}
}

// deadSink blocks until its context expires — a black-holed audit endpoint.
type deadSink struct{ entered chan struct{} }

func (d *deadSink) Report(ctx context.Context, _ confirmaudit.Event) error {
	select {
	case d.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// On the accepted path the gate must send NOTHING.
//
// From the moment the user answers the prompt, they are waiting on the write. A send
// placed there is paid for by the write: against a black-holed endpoint the whole
// report timeout lands between "Confirm" and the order going out, which for a
// trading CLI is market exposure bought with nothing. Every record an accepted
// judgement produces belongs to the caller, on its way out (reportConfirmed).
//
// This is the assertion whose absence let exactly that ship.
func TestConfirmGate_SendsNothingOnTheAcceptedPath(t *testing.T) {
	op := writeOp()
	tests := []struct {
		name string
		want confirmaudit.Channel
		run  func(*testing.T, *Factory, confirmaudit.Event) (confirmaudit.Channel, error)
	}{{
		name: "cli, confirmed at the prompt",
		want: confirmaudit.ChannelTTY,
		run: func(t *testing.T, f *Factory, base confirmaudit.Event) (confirmaudit.Channel, error) {
			stubTTY(t, true, true)
			return confirmForCLI(f, base, op, nil, "", false, credential.ConfirmGrade{})
		},
	}, {
		name: "cli, asserted with --yes",
		want: confirmaudit.ChannelAssumeYes,
		run: func(_ *testing.T, f *Factory, base confirmaudit.Event) (confirmaudit.Channel, error) {
			return confirmForCLI(f, base, op, nil, "", true, credential.ConfirmGrade{})
		},
	}, {
		name: "mcp, accepted in the host dialog",
		want: confirmaudit.ChannelElicitation,
		run: func(_ *testing.T, f *Factory, base confirmaudit.Event) (confirmaudit.Channel, error) {
			return mcpConfirm(context.Background(), f, base, &fakeElicitor{action: "accept"}, op, "d", false)
		},
	}, {
		name: "mcp, asserted with confirm=true",
		want: confirmaudit.ChannelAssumeYes,
		run: func(_ *testing.T, f *Factory, base confirmaudit.Event) (confirmaudit.Channel, error) {
			return mcpConfirm(context.Background(), f, base, nil, op, "d", true)
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &deadSink{entered: make(chan struct{}, 1)}
			f := auditFactory(sink)
			base := auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{})

			start := time.Now()
			ch, err := tt.run(t, f, base)
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("the gate must accept: %v", err)
			}
			if ch != tt.want {
				t.Fatalf("channel = %q, want %q", ch, tt.want)
			}
			select {
			case <-sink.entered:
				t.Fatalf("the gate sent a record on the accepted path; the write waited on it for %s", elapsed)
			default:
			}
			// Measured, because the point is the delay, not the call count: a future
			// send added here would be caught by the clock even if it dodged the sink.
			if elapsed > reportTimeout/4 {
				t.Errorf("the gate took %s to accept against a dead sink; the write is waiting on all of it", elapsed)
			}
		})
	}
}

// The timeout is a budget per JUDGEMENT, not per record.
//
// A judgement that produces two records must not cost twice as much as one that
// produces one. That cost is paid at process exit, after the user already has their
// result, so it is invisible in every test that only checks output — it has to be
// measured.
func TestConfirmAudit_TimeoutIsPerJudgementNotPerRecord(t *testing.T) {
	op := writeOp()
	cases := []struct {
		name    string
		records int
		run     func(*testing.T, *Factory, confirmaudit.Event)
	}{{
		name:    "accepted at the prompt (reminder + confirmation)",
		records: 2,
		run: func(_ *testing.T, f *Factory, base confirmaudit.Event) {
			reportConfirmed(f, base, confirmaudit.ChannelTTY)
		},
	}, {
		name:    "asserted with --yes (confirmation only)",
		records: 1,
		run: func(_ *testing.T, f *Factory, base confirmaudit.Event) {
			reportConfirmed(f, base, confirmaudit.ChannelAssumeYes)
		},
	}, {
		name:    "cancelled at the prompt (reminder + cancellation)",
		records: 2,
		run: func(t *testing.T, f *Factory, base confirmaudit.Event) {
			stubTTY(t, false, true)
			_, _ = confirmForCLI(f, base, op, nil, "", false, credential.ConfirmGrade{})
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &deadSink{entered: make(chan struct{}, tc.records)}
			f := auditFactory(sink)
			base := auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{})

			start := time.Now()
			tc.run(t, f, base)
			elapsed := time.Since(start)

			// Generous, because this asserts "one budget" not "exactly 3s": the failure
			// it exists to catch is a per-record timeout, which would be 2x.
			if elapsed > reportTimeout+reportTimeout/2 {
				t.Errorf("%d records cost %s against an unreachable sink; the budget is %s per judgement",
					tc.records, elapsed, reportTimeout)
			}
			t.Logf("%d record(s) cost %s (budget %s)", tc.records, elapsed, reportTimeout)
		})
	}
}

// The trail is evidence, not a gate on the write.
func TestConfirmAudit_SinkFailureNeverBlocksTheWrite(t *testing.T) {
	rep := &fakeReporter{err: errors.New("sink is down")}
	f := auditFactory(rep)
	op := writeOp()
	base := auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{})

	if err := runGate(f, base, op, nil, "", true); err != nil {
		t.Fatalf("a failing sink must not block a confirmed write: %v", err)
	}
	if len(rep.all()) != 1 {
		t.Fatalf("the report should still have been attempted, got %d", len(rep.all()))
	}
}

// A panicking sink is an edition's bug, and it must not become the user's failed
// write — same rule as a returned error.
func TestConfirmAudit_PanickingSinkNeverBlocksTheWrite(t *testing.T) {
	op := writeOp()
	// Each subtest asserts the sink was actually reached: the panic must be raised on
	// this goroutine and recovered here, not merely never triggered.
	t.Run("panic while sending", func(t *testing.T) {
		rep := &panicReporter{}
		f := auditFactory(rep)
		if err := runGate(f, auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{}), op, nil, "", true); err != nil {
			t.Fatalf("a panicking sink must not block a confirmed write: %v", err)
		}
		if !rep.called.Load() {
			t.Fatal("the sink was never reached; the panic was not actually exercised")
		}
	})
	t.Run("panic while resolving the sink", func(t *testing.T) {
		var reached atomic.Bool
		f := auditFactory(nil)
		f.ConfirmReporter = func() confirmaudit.Reporter { reached.Store(true); panic("cannot build the sink") }
		if err := runGate(f, confirmaudit.Event{}, op, nil, "", true); err != nil {
			t.Fatalf("a panicking sink factory must not block a confirmed write: %v", err)
		}
		if !reached.Load() {
			t.Fatal("the factory was never called; the panic was not actually exercised")
		}
	})
}

type panicReporter struct{ called atomic.Bool }

func (p *panicReporter) Report(context.Context, confirmaudit.Event) error {
	p.called.Store(true)
	panic("sink exploded")
}

// Without a deadline an unreachable sink would hang in front of a user action.
func TestConfirmAudit_SendIsBounded(t *testing.T) {
	rep := &fakeReporter{}
	f := auditFactory(rep)
	op := writeOp()
	_ = runGate(f, auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{}), op, nil, "", true)

	if len(rep.sawDeadlines) != 1 || !rep.sawDeadlines[0] {
		t.Fatal("the report context must carry a deadline")
	}
}

// A unit test or write-free build drives the engine with no sink.
func TestConfirmAudit_NilSinkIsTolerated(t *testing.T) {
	op := writeOp()
	for _, f := range []*Factory{nil, {Config: func() config.Config { return config.Config{} }}, auditFactory(nil)} {
		if err := runGate(f, confirmaudit.Event{}, op, nil, "", true); err != nil {
			t.Fatalf("a build with no sink must still confirm: %v", err)
		}
	}
}

// --- the payload -------------------------------------------------------------

func TestAuditEvent_Payload(t *testing.T) {
	stubNoTTY(t)
	rep := &fakeReporter{}
	f := auditFactory(rep)
	f.Env = func(k string) string {
		if k == "CLAUDECODE" {
			return "1"
		}
		return ""
	}
	op := writeOp()
	ev := auditEvent(f, op, map[string]string{"symbol": "BTCUSDT", "side": "BUY"}, "", "mexc-cli spot trade place", faceCLI, credential.ConfirmGrade{})

	if ev.RequestPath != "/api/v3/order" {
		t.Errorf("RequestPath = %q, want the path template", ev.RequestPath)
	}
	if ev.RequestMethod != "POST" {
		t.Errorf("RequestMethod = %q, want POST", ev.RequestMethod)
	}
	if ev.APIDesc != "Place a spot order" {
		t.Errorf("APIDesc = %q", ev.APIDesc)
	}
	if ev.RiskTier != string(api.TierHeavy) {
		t.Errorf("RiskTier = %q, want heavy for an ungraded write", ev.RiskTier)
	}
	if ev.Face != faceCLI {
		t.Errorf("Face = %q", ev.Face)
	}
	if ev.Env != "prod" {
		t.Errorf("Env = %q, want the resolved env", ev.Env)
	}
	if ev.AgentSource != "claude-code" {
		t.Errorf("AgentSource = %q, want the harness that identified itself", ev.AgentSource)
	}
	// The field separating a person typing --yes from a caller adding it.
	if ev.Interactive {
		t.Error("Interactive must be false when no controlling terminal is reachable")
	}
	if len(ev.ConfirmKey) != confirmKeyHexLen {
		t.Errorf("ConfirmKey = %q, want %d hex chars", ev.ConfirmKey, confirmKeyHexLen)
	}
	if len(ev.InvocationID) != 32 {
		t.Errorf("InvocationID = %q, want 32 hex chars", ev.InvocationID)
	}
}

func TestAuditEvent_InteractiveTracksTheTerminal(t *testing.T) {
	stubTTY(t, true, true)
	f := auditFactory(&fakeReporter{})
	if ev := auditEvent(f, writeOp(), nil, "", "", faceCLI, credential.ConfirmGrade{}); !ev.Interactive {
		t.Fatal("Interactive must be true when a controlling terminal is reachable")
	}
}

// The audit's version of the shape that used to hang, and here it failed in a
// worse direction than a hang: the machine's console opens (an agent's child
// inherited the person's terminal) while the agent holds the streams, so the
// device-only answer this field used to take recorded interactive=true — a
// self-approving caller wearing a human's mark, in the one field kept to tell the
// two apart.
//
// The console is deliberately NOT stubbed. On a machine that has one — every
// developer box, and this is where it was found — the real device opens, so
// leaving it alone is what reproduces the original conditions rather than
// simulating them.
func TestAuditEvent_InteractiveIsFalseWhenStdioIsPiped(t *testing.T) {
	orig := stdioIsTerminalFn
	t.Cleanup(func() { stdioIsTerminalFn = orig })
	stdioIsTerminalFn = func() bool { return false }

	f := auditFactory(&fakeReporter{})
	if ev := auditEvent(f, writeOp(), nil, "", "", faceCLI, credential.ConfirmGrade{}); ev.Interactive {
		t.Error("Interactive must be false when the caller's own streams are pipes, however " +
			"openable the machine's console is — an agent adding --yes must not be recorded " +
			"as a person who typed it")
	}
}

// A long-lived process serves many judgements; each needs its own id.
func TestNewInvocationID_DistinctPerCall(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newInvocationID()
		if seen[id] {
			t.Fatalf("invocation id repeated: %q", id)
		}
		seen[id] = true
	}
}

// --- confirmKey --------------------------------------------------------------

func TestConfirmKey_PairsTheSameRequest(t *testing.T) {
	op := writeOp()
	vals := map[string]string{"symbol": "BTCUSDT", "side": "BUY"}

	// The refusal in one process and the --yes retry in another must pair.
	if a, b := confirmKey("prod", op, vals, ""), confirmKey("prod", op, vals, ""); a != b {
		t.Fatalf("the same request must pair: %q vs %q", a, b)
	}
}

func TestConfirmKey_Distinctions(t *testing.T) {
	op := writeOp()
	base := confirmKey("prod", op, map[string]string{"symbol": "BTCUSDT"}, "")

	t.Run("a different parameter value does not pair", func(t *testing.T) {
		if got := confirmKey("prod", op, map[string]string{"symbol": "ETHUSDT"}, ""); got == base {
			t.Fatal("different parameters produced the same key")
		}
	})
	t.Run("a different operation does not pair", func(t *testing.T) {
		other := op
		other.Path = "/api/v3/order/cancel"
		if got := confirmKey("prod", other, map[string]string{"symbol": "BTCUSDT"}, ""); got == base {
			t.Fatal("different operations produced the same key")
		}
	})
	t.Run("a different env does not pair", func(t *testing.T) {
		// A test-deployment reminder must not vouch for a prod confirmation.
		if got := confirmKey("testnet", op, map[string]string{"symbol": "BTCUSDT"}, ""); got == base {
			t.Fatal("a test-env reminder must not pair with a prod confirmation")
		}
	})
}

func TestConfirmKey_IgnoresBodySerialisation(t *testing.T) {
	op := writeOp()
	op.Body = struct{}{}
	a := confirmKey("prod", op, nil, `{"a":1,"b":"x"}`)
	b := confirmKey("prod", op, nil, "{ \"b\" : \"x\" ,\n \"a\" : 1 }")
	if a != b {
		t.Fatalf("key order/whitespace must not break pairing: %q vs %q", a, b)
	}
	if c := confirmKey("prod", op, nil, `{"a":2,"b":"x"}`); c == a {
		t.Fatal("a different body value must not pair")
	}
}

// Guards the UseNumber decode: through float64 two different large integers would
// collapse onto one key, and two different writes would look like a pair.
//
// Both cases matter, because masking re-serialises the body: a body with a sensitive
// field is decoded and re-encoded on the way to the fingerprint, and that round trip
// is where a float64 would silently rewrite the number. The masked case is the one
// that used to be broken while this comment claimed otherwise.
func TestConfirmKey_PreservesLargeNumbers(t *testing.T) {
	t.Run("plain body", func(t *testing.T) {
		op := writeOp()
		op.Body = struct{}{}
		a := confirmKey("prod", op, nil, `{"id":10000000000000000001}`)
		b := confirmKey("prod", op, nil, `{"id":10000000000000000002}`)
		if a == b {
			t.Fatal("two different large integers must not fingerprint alike")
		}
	})
	t.Run("body carrying a sensitive field", func(t *testing.T) {
		type body struct {
			ID    json.Number `json:"id"`
			Token string      `json:"token" sensitive:"true"`
		}
		op := writeOp()
		op.Body = body{}
		a := confirmKey("prod", op, nil, `{"id":10000000000000000001,"token":"s"}`)
		b := confirmKey("prod", op, nil, `{"id":10000000000000000002,"token":"s"}`)
		if a == b {
			t.Fatal("masking must not collapse two different large integers onto one key")
		}
	})
}

// The masked body a user is asked to approve must be the body that gets sent: if
// masking rewrites a number, the prompt shows a quantity nobody requested.
func TestMaskSensitiveBody_KeepsNumbersExact(t *testing.T) {
	type body struct {
		ID    json.Number `json:"id"`
		Token string      `json:"token" sensitive:"true"`
	}
	op := writeOp()
	op.Body = body{}
	got := op.MaskSensitiveBody(`{"id":10000000000000000001,"token":"secret"}`)
	if !strings.Contains(got, "10000000000000000001") {
		t.Errorf("masking rewrote the number: %s", got)
	}
	if strings.Contains(got, "secret") {
		t.Errorf("masking left the sensitive value readable: %s", got)
	}
}

// No secret enters the fingerprint: two different sensitive values pair, because
// both entered as the mask. A raw one would be recoverable — the fingerprint is
// short and the rest of the request is in the record alongside it.
func TestConfirmKey_UsesTheMaskedValue(t *testing.T) {
	t.Run("parameter", func(t *testing.T) {
		op := writeOp()
		op.Params = []api.Param{{Name: "symbol"}, {Name: "token", Sensitive: true}}
		a := confirmKey("prod", op, map[string]string{"symbol": "BTCUSDT", "token": "secret-one"}, "")
		b := confirmKey("prod", op, map[string]string{"symbol": "BTCUSDT", "token": "secret-two"}, "")
		if a != b {
			t.Fatal("a sensitive parameter must enter the key as the mask, not verbatim")
		}
	})
	t.Run("body field", func(t *testing.T) {
		type body struct {
			AppID string `json:"appId"`
			Token string `json:"token" sensitive:"true"`
		}
		op := writeOp()
		op.Body = body{}
		a := confirmKey("prod", op, nil, `{"appId":"a","token":"secret-one"}`)
		b := confirmKey("prod", op, nil, `{"appId":"a","token":"secret-two"}`)
		if a != b {
			t.Fatal("a sensitive body field must enter the key as the mask, not verbatim")
		}
		// Still discriminating on what is not sensitive.
		if c := confirmKey("prod", op, nil, `{"appId":"b","token":"secret-one"}`); c == a {
			t.Fatal("a non-sensitive body change must still change the key")
		}
	})
}

// Why parameters are keyed rather than concatenated in declaration order: editing
// a declaration must not break a pair that spans a CLI upgrade.
func TestConfirmKey_SurvivesParameterReordering(t *testing.T) {
	before := writeOp()
	after := writeOp()
	after.Params = []api.Param{{Name: "side"}, {Name: "symbol"}}
	vals := map[string]string{"symbol": "BTCUSDT", "side": "BUY"}

	if confirmKey("prod", before, vals, "") != confirmKey("prod", after, vals, "") {
		t.Fatal("reordering a declaration must not change the fingerprint")
	}
}

// --- auditCommand ------------------------------------------------------------

func TestAuditCommand(t *testing.T) {
	type body struct {
		AppID string `json:"appId"`
		Token string `json:"token" sensitive:"true"`
	}
	op := writeOp()
	op.Params = []api.Param{{Name: "symbol"}, {Name: "token", Sensitive: true}}
	op.Body = body{}

	got := auditCommand("mexc-cli spot trade place", op,
		map[string]string{"symbol": "BTCUSDT", "token": "sk-live-abc"},
		`{"appId":"a","token":"sk-live-xyz"}`)

	for _, want := range []string{"mexc-cli spot trade place", "--symbol BTCUSDT", "--token " + api.SensitiveMask, "--data"} {
		if !strings.Contains(got, want) {
			t.Errorf("command missing %q:\n%s", want, got)
		}
	}
	// The record is read by people with no business seeing a key.
	for _, secret := range []string{"sk-live-abc", "sk-live-xyz"} {
		if strings.Contains(got, secret) {
			t.Errorf("command leaked %q:\n%s", secret, got)
		}
	}
}

func TestAuditCommand_NamesTheOperationWithoutACommandPath(t *testing.T) {
	// The MCP face has no command path; the record must still read as one.
	got := auditCommand("", writeOp(), nil, "")
	if !strings.Contains(got, "place") {
		t.Fatalf("command should name the operation: %q", got)
	}
}

func TestAuditCommand_IsBounded(t *testing.T) {
	op := writeOp()
	op.Params = []api.Param{{Name: "note"}}
	got := auditCommand("mexc-cli spot trade place", op, map[string]string{"note": strings.Repeat("x", 2000)}, "")
	if len(got) > auditCommandMax {
		t.Fatalf("command is %d bytes, want at most %d", len(got), auditCommandMax)
	}
}

func TestTruncateBytes_KeepsValidUTF8(t *testing.T) {
	// Cutting mid-rune would store replacement bytes.
	s := strings.Repeat("€", 10) // 3 bytes each in UTF-8
	for limit := 1; limit <= len(s); limit++ {
		if got := truncateBytes(s, limit); !isValidUTF8(got) {
			t.Fatalf("limit %d produced invalid UTF-8: %q", limit, got)
		}
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '\uFFFD' {
			return false
		}
	}
	return true
}

// --- agentSource -------------------------------------------------------------

func TestAgentSource(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"claude code", map[string]string{"CLAUDECODE": "1"}, "claude-code"},
		{"cursor", map[string]string{"CURSOR_TRACE_ID": "abc"}, "cursor"},
		{"ci", map[string]string{"GITLAB_CI": "true"}, "gitlab-ci"},
		// Reported plainly, not guessed from weaker signals.
		{"unrecognised", map[string]string{"TERM": "xterm"}, "unknown"},
		{"nothing", nil, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentSource(func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Fatalf("agentSource = %q, want %q", got, tt.want)
			}
		})
	}
	if got := agentSource(nil); got != "unknown" {
		t.Fatalf("a nil reader must report unknown, got %q", got)
	}
}

// --- assembly fail-closed ----------------------------------------------------

func TestValidateConfirmReporter(t *testing.T) {
	writeFree := []api.DomainSpec{{Name: "sample", Subs: []api.Subdomain{{
		Name: "public", Ops: []api.Operation{{Name: "price", Access: api.Read}},
	}}}}

	t.Run("writes without a sink is refused", func(t *testing.T) {
		err := validateConfirmReporter(domains(), false)
		if err == nil {
			t.Fatal("a build mounting writes with no sink must fail assembly")
		}
		var e *errs.Error
		if !errors.As(err, &e) || e.Type != errs.TypeInternal {
			t.Fatalf("want an internal wiring error, got %v", err)
		}
		if !strings.Contains(e.Hint, "Discard") {
			t.Errorf("the hint should name the opt-out: %q", e.Hint)
		}
	})
	t.Run("writes with a sink is accepted", func(t *testing.T) {
		if err := validateConfirmReporter(domains(), true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("a write-free build needs no sink", func(t *testing.T) {
		if err := validateConfirmReporter(writeFree, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSetConfirmReporter_RefusesAMissingSink(t *testing.T) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &out, &errBuf)
	app.SetConfirmReporter(nil)

	// The failure must reach the first command, not wait for a write.
	if code := app.Run([]string{"sample", "public", "ticker", "--symbol", "BTCUSDT", "--dry-run"}); code == 0 {
		t.Fatal("a build with writes and no sink must fail every invocation")
	}
	if !strings.Contains(errBuf.String(), "audit sink") {
		t.Fatalf("the error should name the missing sink: %s", errBuf.String())
	}
}

// --- end-to-end through the command tree -------------------------------------

// runAudited drives the real tree with a recording sink, as the root installs one.
func runAudited(rep confirmaudit.Reporter, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &out, &errBuf)
	app.SetConfirmReporter(func() confirmaudit.Reporter { return rep })
	code = app.Run(args)
	return out.String(), errBuf.String(), code
}

func TestConfirmAudit_CLIWiring(t *testing.T) {
	stubNoTTY(t)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	testOpenAPICredentials = credential.Credentials{APIKey: "pk", Secret: "ps"}
	t.Cleanup(func() { testOpenAPICredentials = credential.Credentials{} })

	place := []string{"sample", "orders", "create", "--symbol", "BTCUSDT", "--side", "BUY", "--quantity", "0.001"}

	t.Run("a refused write records the reminder", func(t *testing.T) {
		rep := &fakeReporter{}
		_, stderr, code := runAudited(rep, place...)
		if code != errs.ExitConfirmationRequired {
			t.Fatalf("exit = %d, want %d; stderr=%s", code, errs.ExitConfirmationRequired, stderr)
		}
		evs := rep.all()
		if got := stages(evs); len(got) != 1 || got[0] != "REMIND/refusal" {
			t.Fatalf("reported %v, want one REMIND/refusal", got)
		}
		ev := evs[0]
		if !strings.Contains(ev.Command, "sample orders create") || !strings.Contains(ev.Command, "--symbol BTCUSDT") {
			t.Errorf("Command should render the invocation: %q", ev.Command)
		}
		if ev.RequestPath != "/fixture/orders" || ev.RequestMethod != "POST" {
			t.Errorf("request identity = %s %s", ev.RequestMethod, ev.RequestPath)
		}
		if ev.ConfirmKey == "" || ev.InvocationID == "" {
			t.Error("both correlation fields must be populated")
		}
	})

	t.Run("the same write with --yes records only the confirmation", func(t *testing.T) {
		// The pair the audit checks: separate processes, joined only by ConfirmKey.
		refused := &fakeReporter{}
		_, _, _ = runAudited(refused, place...)

		confirmed := &fakeReporter{}
		// The send then fails against the unreachable test host; what is asserted is
		// what was recorded before it.
		_, _, _ = runAudited(confirmed, append(append([]string{}, place...), "--yes")...)

		got := stages(confirmed.all())
		if len(got) != 1 || got[0] != "CONFIRMED/assume-yes" {
			t.Fatalf("reported %v, want one CONFIRMED/assume-yes", got)
		}
		if a, b := refused.all()[0].ConfirmKey, confirmed.all()[0].ConfirmKey; a != b {
			t.Fatalf("the reminder and its confirmation must share a key: %q vs %q", a, b)
		}
		if a, b := refused.all()[0].InvocationID, confirmed.all()[0].InvocationID; a == b {
			t.Fatal("two processes must not share a judgement id")
		}
	})

	t.Run("dry-run records nothing", func(t *testing.T) {
		// A preview sends nothing, so there is no write to confirm.
		rep := &fakeReporter{}
		_, _, code := runAudited(rep, append(append([]string{}, place...), "--dry-run")...)
		if code != 0 {
			t.Fatalf("dry-run should succeed, got %d", code)
		}
		if evs := rep.all(); len(evs) != 0 {
			t.Fatalf("dry-run reported %v, want nothing", stages(evs))
		}
	})

	t.Run("a read records nothing", func(t *testing.T) {
		rep := &fakeReporter{}
		_, _, _ = runAudited(rep, "sample", "public", "ticker", "--symbol", "BTCUSDT", "--dry-run")
		if evs := rep.all(); len(evs) != 0 {
			t.Fatalf("a read reported %v, want nothing", stages(evs))
		}
	})
}

// --- MCP face ----------------------------------------------------------------

func TestConfirmAudit_MCPFace(t *testing.T) {
	op := writeOp()

	tests := []struct {
		name         string
		el           elicitor
		confirmParam bool
		want         []string
	}{{
		// The host renders the dialog, so the agent cannot answer for the user. Both
		// stages go out on the way back, in the order they happened.
		name: "accepted elicitation reports both stages",
		el:   &fakeElicitor{action: "accept"},
		want: []string{"REMIND/mcp-elicitation", "CONFIRMED/mcp-elicitation"},
	}, {
		name: "declined elicitation reports a cancellation",
		el:   &fakeElicitor{action: "decline"},
		want: []string{"REMIND/mcp-elicitation", "CANCELLED/mcp-elicitation"},
	}, {
		name:         "confirm=true reports the confirmation only",
		confirmParam: true,
		want:         []string{"CONFIRMED/assume-yes"},
	}, {
		// The dialog broke: whether the user saw anything is unknown, and the caller
		// is not told to relay, so there is no half-pair to open. Nothing is claimed.
		name: "a failed dialog claims nothing",
		el:   &fakeElicitor{err: errors.New("dialog is unavailable")},
		want: nil,
	}, {
		name: "no dialog and no flag reports a refusal",
		want: []string{"REMIND/refusal"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := &fakeReporter{}
			f := auditFactory(rep)
			base := auditEvent(f, op, nil, "", "", faceMCP, credential.ConfirmGrade{})
			ch, err := mcpConfirm(context.Background(), f, base, tt.el, op, "msg", tt.confirmParam)
			if err == nil {
				reportConfirmed(f, base, ch) // what toolHandler defers
			}

			if got := stages(rep.all()); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("reported %v, want %v", got, tt.want)
			}
		})
	}
}

// Keeps a long-lived MCP process from looking like one repeated judgement.
func TestConfirmAudit_MCPJudgementsAreDistinct(t *testing.T) {
	f := auditFactory(&fakeReporter{})
	op := writeOp()
	first := auditEvent(f, op, map[string]string{"symbol": "BTCUSDT"}, "", "", faceMCP, credential.ConfirmGrade{})
	second := auditEvent(f, op, map[string]string{"symbol": "BTCUSDT"}, "", "", faceMCP, credential.ConfirmGrade{})

	if first.InvocationID == second.InvocationID {
		t.Fatal("two tool calls in one process must not share a judgement id")
	}
	// Same request, so they still pair against the same reminder.
	if first.ConfirmKey != second.ConfirmKey {
		t.Fatal("the same request must fingerprint alike across tool calls")
	}
}

// Why the rendered command line is not a fingerprint input: the faces render it
// differently for one request, and a reminder from one must pair with the other.
func TestConfirmAudit_FacesAgreeOnTheKey(t *testing.T) {
	f := auditFactory(&fakeReporter{})
	op := writeOp()
	vals := map[string]string{"symbol": "BTCUSDT"}

	cli := auditEvent(f, op, vals, "", "mexc-cli spot trade place", faceCLI, credential.ConfirmGrade{})
	mcp := auditEvent(f, op, vals, "", "", faceMCP, credential.ConfirmGrade{})

	if cli.Command == mcp.Command {
		t.Fatal("the two faces are expected to render the command differently")
	}
	if cli.ConfirmKey != mcp.ConfirmKey {
		t.Fatalf("the rendering must not affect the key: %q vs %q", cli.ConfirmKey, mcp.ConfirmKey)
	}
}

// Smoke check that the fields a sink forwards survive JSON encoding.
func TestAuditEvent_MarshalsToTheReportShape(t *testing.T) {
	f := auditFactory(&fakeReporter{})
	ev := auditEvent(f, writeOp(), map[string]string{"symbol": "BTCUSDT"}, "", "mexc-cli spot trade place", faceCLI, credential.ConfirmGrade{})
	ev.Stage, ev.Channel = confirmaudit.StageRemind, confirmaudit.ChannelRefusal

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{"REMIND", "refusal", "/api/v3/order", ev.ConfirmKey, ev.InvocationID} {
		if !strings.Contains(string(b), want) {
			t.Errorf("encoded event missing %q:\n%s", want, b)
		}
	}
}

// The trail has to say WHY a confirmation was asked. Without it an auditor
// cannot tell an ordinary write confirmation from one an identity demanded,
// which is the first question they ask.
func TestAuditEvent_CarriesTheGradeReason(t *testing.T) {
	f := auditFactory(&fakeReporter{})
	op := api.Operation{Name: "list", Access: api.Read}

	ev := auditEvent(f, op, nil, "", "", faceCLI, credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "graded: because",
	})
	// The code is what an audit groups by, and it must survive a rewording of
	// the reason — which is why they are two fields.
	if ev.GradeCode != "X2" {
		t.Errorf("GradeCode = %q, want X2", ev.GradeCode)
	}
	if ev.GradeReason != "graded: because" {
		t.Errorf("GradeReason = %q, want the reason", ev.GradeReason)
	}
}

func TestAuditEvent_GradeIsEmptyForAnUngradedWrite(t *testing.T) {
	f := auditFactory(&fakeReporter{})

	ev := auditEvent(f, writeOp(), nil, "", "", faceCLI, credential.ConfirmGrade{})
	if ev.GradeCode != "" || ev.GradeReason != "" {
		t.Errorf("grade fields = %q/%q, want both empty", ev.GradeCode, ev.GradeReason)
	}
}
