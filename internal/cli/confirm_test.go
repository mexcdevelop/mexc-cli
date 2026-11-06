package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

func TestConfirmDisplay(t *testing.T) {
	op := api.Operation{
		Name: "place", Access: api.Write, Summary: "Place a spot order",
		Params: []api.Param{
			{Name: "symbol"}, {Name: "side"}, {Name: "price"}, {Name: "unused"},
		},
	}
	got := confirmDisplay(op, map[string]string{"symbol": "BTCUSDT", "side": "BUY", "price": "60000"}, "")
	if !strings.HasPrefix(got, "Place a spot order") {
		t.Fatalf("display should start with the summary, got:\n%s", got)
	}
	for _, want := range []string{"symbol", "BTCUSDT", "side", "BUY", "price", "60000"} {
		if !strings.Contains(got, want) {
			t.Errorf("display missing %q:\n%s", want, got)
		}
	}
	// Params in declaration order; empty ones are omitted.
	if strings.Contains(got, "unused") {
		t.Errorf("empty param must be omitted:\n%s", got)
	}
	if strings.Index(got, "symbol") > strings.Index(got, "side") {
		t.Errorf("params must render in declaration order:\n%s", got)
	}
}

// gradedDisplay is the single merge point, so both halves of what it decides are
// pinned by equality rather than by Contains: an ungraded operation's prompt must
// be byte-identical to what it was before grades existed, and a graded one reads
// the reason FIRST. Contains passes either way round, which is how both survived
// a mutation that reversed the order and one that prepended an empty reason.
func TestGradedDisplay(t *testing.T) {
	op := api.Operation{Name: "place", Access: api.Write, Summary: "Place an order"}
	plain := confirmDisplay(op, nil, "")

	if got := gradedDisplay(op, nil, "", credential.ConfirmGrade{}); got != plain {
		t.Errorf("an ungraded prompt changed:\n got %q\nwant %q", got, plain)
	}

	got := gradedDisplay(op, nil, "",
		credential.ConfirmGrade{Confirm: true, Reason: "graded: because"})
	if want := "graded: because\n\n" + plain; got != want {
		t.Errorf("graded prompt:\n got %q\nwant %q", got, want)
	}
}

func TestConfirmDisplay_Body(t *testing.T) {
	op := api.Operation{Name: "add", Access: api.Write, Summary: "Create app", Body: struct{}{}}
	got := confirmDisplay(op, nil, `{"appId":"x"}`)
	if !strings.Contains(got, "body") || !strings.Contains(got, `{"appId":"x"}`) {
		t.Fatalf("body op should show its JSON body:\n%s", got)
	}
}

func TestConfirmModel_DefaultCursorByTier(t *testing.T) {
	// Light defaults the cursor to Confirm; Heavy defaults to Cancel.
	if m := newConfirmModel("x", "write", api.TierLight); m.cursor != 0 {
		t.Errorf("light default cursor = %d, want 0 (Confirm)", m.cursor)
	}
	if m := newConfirmModel("x", "write", api.TierHeavy); m.cursor != 1 {
		t.Errorf("heavy default cursor = %d, want 1 (Cancel)", m.cursor)
	}
	if !newConfirmModel("x", "write", api.TierHeavy).heavy {
		t.Error("heavy tier must set the irreversible flag")
	}
}

// step drives one key through the model and returns the updated model.
func step(m confirmModel, msg tea.Msg) confirmModel {
	next, _ := m.Update(msg)
	return next.(confirmModel)
}

func TestConfirmModel_Decisions(t *testing.T) {
	base := newConfirmModel("x", "write", api.TierLight) // cursor starts at Confirm

	// Enter on Confirm → confirmed.
	if m := step(base, tea.KeyMsg{Type: tea.KeyEnter}); !m.done || !m.confirmed {
		t.Errorf("enter on Confirm: done=%v confirmed=%v", m.done, m.confirmed)
	}
	// 'y' → confirmed regardless of cursor.
	if m := step(newConfirmModel("x", "write", api.TierHeavy), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); !m.confirmed {
		t.Error("'y' must confirm")
	}
	// esc → cancelled.
	if m := step(base, tea.KeyMsg{Type: tea.KeyEsc}); !m.done || m.confirmed {
		t.Errorf("esc: done=%v confirmed=%v, want done && !confirmed", m.done, m.confirmed)
	}
	// 'n' → cancelled.
	if m := step(base, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); m.confirmed {
		t.Error("'n' must cancel")
	}
	// Move to Cancel then Enter → cancelled.
	moved := step(base, tea.KeyMsg{Type: tea.KeyRight})
	if m := step(moved, tea.KeyMsg{Type: tea.KeyEnter}); m.confirmed {
		t.Error("enter on Cancel must not confirm")
	}
}

func TestConfirmForCLI_ReadAndYes(t *testing.T) {
	// A nil Factory means "no audit sink"; report tolerates it, so these cases
	// exercise the gate's decision alone. The reporting itself is covered in
	// confirmaudit_test.go.
	// A read never confirms.
	if _, err := confirmForCLI(nil, confirmaudit.Event{}, api.Operation{Access: api.Read}, nil, "", false, credential.ConfirmGrade{}); err != nil {
		t.Errorf("read must not need confirmation: %v", err)
	}
	// --yes proceeds without a prompt (no tty touched).
	write := api.Operation{Name: "place", Access: api.Write}
	if _, err := confirmForCLI(nil, confirmaudit.Event{}, write, nil, "", true, credential.ConfirmGrade{}); err != nil {
		t.Errorf("--yes must proceed: %v", err)
	}
}

// TestWriteConfirmation_CLIWiring drives a real write op (spot.trade.place)
// through the whole command tree to prove runOperation gates it. The caller is
// forced unattended so the refusal branch is taken deterministically — a test
// must never launch the interactive TUI, which now renders on the real stdout and
// would block on input nobody sends.
func TestWriteConfirmation_CLIWiring(t *testing.T) {
	unattended(t)

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	place := []string{"sample", "orders", "create", "--symbol", "BTCUSDT", "--side", "BUY", "--quantity", "0.001"}

	// Ordering guard: with NO credentials, the write fails fast on the credential
	// precheck (login/api-key BEFORE the interactive confirm), NOT confirmation —
	// the user is never asked to confirm an action that provably cannot run.
	_, preStderr, preCode := run(nil, place...)
	if preCode != 2 {
		t.Fatalf("no-credentials write should fail the precheck (exit 2), got %d; stderr=%s", preCode, preStderr)
	}
	if !strings.Contains(preStderr, "credentials") {
		t.Errorf("precheck error should be about credentials, got: %s", preStderr)
	}

	// With credentials present, the precheck passes and the write reaches the
	// confirmation gate: no --yes, no tty, not dry-run → confirmation_required
	// (exit 10), never sent.
	testOpenAPICredentials = credential.Credentials{APIKey: "pk", Secret: "ps"}
	t.Cleanup(func() { testOpenAPICredentials = credential.Credentials{} })
	_, stderr, code := run(nil, place...)
	if code != errs.ExitConfirmationRequired {
		t.Fatalf("write without --yes should exit %d, got %d; stderr=%s", errs.ExitConfirmationRequired, code, stderr)
	}
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr not an error envelope: %v (%s)", err, stderr)
	}
	if env.Error.Subtype != errs.SubtypeConfirmationRequired {
		t.Fatalf("subtype = %q, want confirmation_required", env.Error.Subtype)
	}
	if !strings.Contains(env.Error.Hint, "--yes") {
		t.Errorf("hint should mention --yes: %q", env.Error.Hint)
	}

	// --dry-run previews without executing → confirmation is skipped (exit 0).
	stdout, _, code2 := run(nil, append(place, "--dry-run")...)
	if code2 != 0 {
		t.Fatalf("dry-run must skip confirmation, got exit %d", code2)
	}
	if !strings.Contains(stdout, "/fixture/orders") {
		t.Errorf("dry-run should print the plan, got: %s", stdout)
	}
}

// unattended puts the gate in the state an agent's shell-out is in: this
// process's own streams are not a terminal.
//
// It replaces a helper that pointed the confirmation opener at a regular file, a
// shape that stopped existing when the prompt moved onto stdio and stopped
// opening a device. The Windows two-device path it also exercised is still
// covered where the device is still used, by
// TestOpenTTY_RefusesWhenTheDeviceIsTwoSeparatePaths in the profile-set path.
func unattended(t *testing.T) {
	t.Helper()
	prev := stdioIsTerminalFn
	stdioIsTerminalFn = func() bool { return false }
	t.Cleanup(func() { stdioIsTerminalFn = prev })
}

// TestConfirmForCLI_NoTTY_TwoDeviceShape WAS HERE, and is gone rather than
// ported. It drove console.Open's two-handle Windows shape through this gate,
// which the gate no longer reaches: the prompt runs on stdio and opens no device,
// so there is no split for it to survive. The shape still matters where the
// device is still used, and is still tested there —
// TestOpenTTY_RefusesWhenTheDeviceIsTwoSeparatePaths, in the profile-set path.
//
// Recorded rather than deleted silently: a test disappearing in a diff reads like
// coverage lost, and here the code under it is what went away.

func TestConfirmForCLI_NoTTY(t *testing.T) {
	// The agent's shape: this process's own streams are not a terminal, so the
	// gate refuses without rendering anything.
	unattended(t)

	op := api.Operation{
		Name: "place", Access: api.Write, Summary: "Place a spot order",
		Params: []api.Param{{Name: "symbol"}, {Name: "side"}},
	}
	_, err := confirmForCLI(nil, confirmaudit.Event{}, op, map[string]string{"symbol": "BTCUSDT", "side": "BUY"}, "", false, credential.ConfirmGrade{})
	if err == nil {
		t.Fatal("a write with no tty and no --yes must be refused")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeConfirmationRequired {
		t.Fatalf("want confirmation_required, got %v", err)
	}
	if errs.ExitCode(err) != errs.ExitConfirmationRequired {
		t.Fatalf("exit code = %d, want %d", errs.ExitCode(err), errs.ExitConfirmationRequired)
	}
	// The refusal must carry the summary + the parameters, so the agent relays the
	// EXACT pending request (not a reconstruction).
	for _, want := range []string{"Place a spot order", "symbol", "BTCUSDT", "side", "BUY"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("confirmation_required message missing %q:\n%s", want, e.Message)
		}
	}
}

func TestConfirmDisplay_MasksSensitive(t *testing.T) {
	type reqBody struct {
		AppID string `json:"appId"`
		Token string `json:"token" sensitive:"true"`
	}
	op := api.Operation{
		Name: "add", Access: api.Write, Summary: "Create app", Body: reqBody{},
		Params: []api.Param{{Name: "id"}, {Name: "apikey", Sensitive: true}},
	}
	got := confirmDisplay(op,
		map[string]string{"id": "public-123", "apikey": "SECRET-KEY"},
		`{"appId":"AI-TECH","token":"SECRET-TOKEN"}`)

	for _, secret := range []string{"SECRET-KEY", "SECRET-TOKEN"} {
		if strings.Contains(got, secret) {
			t.Errorf("sensitive value %q must be masked:\n%s", secret, got)
		}
	}
	for _, shown := range []string{"public-123", "AI-TECH"} {
		if !strings.Contains(got, shown) {
			t.Errorf("non-sensitive value %q must be shown:\n%s", shown, got)
		}
	}
	if !strings.Contains(got, api.SensitiveMask) {
		t.Errorf("mask marker must appear:\n%s", got)
	}
}

// THE SHAPE THAT USED TO HANG. An agent shells out: the child's stdin and stdout
// are pipes, but it inherited the person's controlling terminal, so the machine's
// console is open for the asking. The gate must answer "unattended" anyway.
//
// The console is deliberately NOT stubbed here. On any machine that has one — a
// developer box, and this is where the bug was found — the real device opens, so
// leaving it alone reproduces the original conditions instead of simulating them.
//
// Before the gate, this reached runConfirmTUI and blocked on a bubbletea prompt
// rendered where the agent could not see it and nobody could answer: no refusal,
// no exit code, no output at all until the host's timeout killed it. So the
// assertion is twofold — the refusal is confirmation_required, AND the prompt was
// never started. The second half is the one that catches a regression: a prompt
// that renders is a hang, whatever it returns.
func TestConfirmForCLI_StdioIsPiped(t *testing.T) {
	origStdio, origRun := stdioIsTerminalFn, runConfirmTUIFn
	t.Cleanup(func() {
		stdioIsTerminalFn, runConfirmTUIFn = origStdio, origRun
	})

	stdioIsTerminalFn = func() bool { return false } // pipes: an agent holds them
	rendered := false
	runConfirmTUIFn = func(string, string, api.RiskTier) (bool, bool) {
		rendered = true
		return true, true // "confirmed" — so a gate that fell through would PASS the write
	}

	op := api.Operation{
		Name: "place", Access: api.Write, Summary: "Place a spot order",
		Params: []api.Param{{Name: "symbol"}},
	}
	_, err := confirmForCLI(nil, confirmaudit.Event{}, op, map[string]string{"symbol": "BTCUSDT"}, "", false, credential.ConfirmGrade{})

	if rendered {
		t.Error("the confirmation prompt was started for a caller whose stdio is piped — " +
			"that is the hang: it renders where the agent cannot see it and waits for input nobody sends")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeConfirmationRequired {
		t.Fatalf("want confirmation_required, got %v", err)
	}
	if errs.ExitCode(err) != errs.ExitConfirmationRequired {
		t.Errorf("exit code = %d, want %d — the agent routes on this", errs.ExitCode(err), errs.ExitConfirmationRequired)
	}
	// The refusal has to carry the pending request: this is the path an agent
	// actually takes, so it is the one whose payload has to be relayable.
	for _, want := range []string{"Place a spot order", "symbol", "BTCUSDT"} {
		if !strings.Contains(e.Message, want) {
			t.Errorf("confirmation_required message missing %q:\n%s", want, e.Message)
		}
	}
}

// The human's path must survive the gate: attended stdio still reaches the
// prompt. Without this, a gate wired backwards — or one that refuses
// unconditionally — would look correct to every other test here.
func TestConfirmForCLI_AttendedStdioStillReachesThePrompt(t *testing.T) {
	stubTTY(t, true, true)

	op := api.Operation{Name: "place", Access: api.Write, Summary: "Place a spot order"}
	ch, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "", false, credential.ConfirmGrade{})
	if err != nil {
		t.Fatalf("an attended caller who confirmed must proceed, got %v", err)
	}
	if ch != confirmaudit.ChannelTTY {
		t.Errorf("channel = %q, want %q", ch, confirmaudit.ChannelTTY)
	}
}

// A read normally never reaches the prompt. A grade that demands confirmation
// has to open the gate for it, and the refusal must not call the read a write.
func TestConfirmForCLI_GradedReadIsRefusedAsARead(t *testing.T) {
	prev := stdioIsTerminalFn
	stdioIsTerminalFn = func() bool { return false } // unattended: take the refusal branch
	t.Cleanup(func() { stdioIsTerminalFn = prev })

	op := api.Operation{Name: "list", Access: api.Read, Summary: "List things"}

	_, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "",
		false, credential.ConfirmGrade{Confirm: true, Reason: "graded: because"})
	if err == nil {
		t.Fatal("a graded read was allowed through without confirmation")
	}
	msg := err.Error()
	if !strings.Contains(msg, "this read needs") {
		t.Errorf("refusal does not call it a read:\n%s", msg)
	}
	if strings.Contains(msg, "this write needs") {
		t.Errorf("refusal calls a read a write:\n%s", msg)
	}
	if !strings.Contains(msg, "graded: because") {
		t.Errorf("refusal omits the grade reason:\n%s", msg)
	}
}

// An ungraded read is untouched: no prompt, no refusal, no audit record.
func TestConfirmForCLI_UngradedReadStillSkipsTheGate(t *testing.T) {
	op := api.Operation{Name: "list", Access: api.Read}

	ch, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "",
		false, credential.ConfirmGrade{})
	if err != nil || ch != "" {
		t.Fatalf("ungraded read got ch=%q err=%v, want no-op", ch, err)
	}
}

// A graded write is ONE prompt carrying both reasons, not two prompts.
func TestConfirmForCLI_GradedWriteMergesBothReasonsIntoOneRefusal(t *testing.T) {
	prev := stdioIsTerminalFn
	stdioIsTerminalFn = func() bool { return false }
	t.Cleanup(func() { stdioIsTerminalFn = prev })

	op := api.Operation{Name: "place", Access: api.Write, Summary: "Place an order"}

	_, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "",
		false, credential.ConfirmGrade{Confirm: true, Reason: "graded: because"})
	if err == nil {
		t.Fatal("a graded write was allowed through")
	}
	msg := err.Error()
	if !strings.Contains(msg, "this write needs") {
		t.Errorf("a write must still be called a write:\n%s", msg)
	}
	if !strings.Contains(msg, "graded: because") {
		t.Errorf("the grade reason is missing:\n%s", msg)
	}
	if !strings.Contains(msg, "Place an order") {
		t.Errorf("the pending request is missing:\n%s", msg)
	}
}

// --yes skips a graded confirmation exactly as it skips a write confirmation
// (design decision D7: the grade is a rule plus an audit trail on the one path
// that has no channel to ask a human, not a mechanism).
func TestConfirmForCLI_AssumeYesSkipsAGradedRead(t *testing.T) {
	op := api.Operation{Name: "list", Access: api.Read}

	ch, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "",
		true, credential.ConfirmGrade{Confirm: true, Reason: "graded: because"})
	if err != nil {
		t.Fatalf("--yes on a graded read errored: %v", err)
	}
	if ch != confirmaudit.ChannelAssumeYes {
		t.Errorf("channel = %q, want %q", ch, confirmaudit.ChannelAssumeYes)
	}
}

func TestConfirmModel_HeaderUsesTheNoun(t *testing.T) {
	read := newConfirmModel("the request", "read", api.TierNone)
	if got := read.View(); !strings.Contains(got, "Confirm read:") {
		t.Errorf("read view header:\n%s", got)
	}
	write := newConfirmModel("the request", "write", api.TierLight)
	if got := write.View(); !strings.Contains(got, "Confirm write:") {
		t.Errorf("write view header:\n%s", got)
	}
}

// A graded read is neutral: it is not irreversible, and its cursor starts on
// Confirm rather than defaulting to Cancel the way a heavy write does.
func TestConfirmModel_GradedReadIsNeutral(t *testing.T) {
	m := newConfirmModel("the request", "read", api.TierNone)
	if m.heavy {
		t.Error("a graded read must not be rendered as irreversible")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (Confirm)", m.cursor)
	}
	if strings.Contains(m.View(), "IRREVERSIBLE") {
		t.Error("a graded read must not show the irreversible banner")
	}
}

// gradingIdentityProvider is a Provider that grades, registered so the wiring
// from the gate through pkg/credential to a provider is exercised end to end —
// the shared repository has no real identity that grades.
type gradingIdentityProvider struct {
	grade  credential.ConfirmGrade
	called int
	// What the last call was asked about. Recorded because every argument exists
	// for a reason a caller could get wrong: the env decides WHICH deployment's
	// state answers, and the access decides which sentence a person is shown. Both
	// travelled untested until a mutation passed the wrong value and no test noticed.
	gotEnv    hosts.Env
	gotReq    *api.PermReq
	gotAccess api.Access
}

func (p *gradingIdentityProvider) Realm() credential.Realm { return credential.Realm("grade-wiring") }
func (p *gradingIdentityProvider) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{}, nil
}
func (p *gradingIdentityProvider) Inject(*api.RequestSpec, credential.Credentials) error { return nil }
func (p *gradingIdentityProvider) ConfirmGrade(env hosts.Env, req *api.PermReq, access api.Access) credential.ConfirmGrade {
	p.called++
	p.gotEnv, p.gotReq, p.gotAccess = env, req, access
	return p.grade
}

// CarriesGrants makes this fixture identity eligible to carry a Permission
// requirement at all: assembly refuses an operation that declares one against an
// identity whose provider has no grant map (see mount.go). Grading and carrying
// grants travel together in practice — a grade is a statement about a permission
// code — so a fixture that grades without carrying them could not be mounted.
func (p *gradingIdentityProvider) CarriesGrants() {}

func gradeFactory(dryRun bool) *Factory {
	return &Factory{
		Config: func() config.Config { return config.Config{Env: "prod"} },
		DryRun: func() bool { return dryRun },
		Env:    func(string) string { return "" },
		Out:    io.Discard,
		Err:    io.Discard,
	}
}

func TestGradeFor(t *testing.T) {
	p := &gradingIdentityProvider{grade: credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "because",
	}}
	credential.Register(p)
	identity := api.Identity(p.Realm())
	perm := &api.PermReq{System: "site-a", Codes: []string{"a:list"}}

	t.Run("a graded operation is graded, and both strings travel", func(t *testing.T) {
		p.called = 0
		got := gradeFor(gradeFactory(false), identity,
			api.Operation{Name: "list", Access: api.Read, Permission: perm})
		if !got.Confirm || got.Code != "X2" || got.Reason != "because" {
			t.Fatalf("grade = %+v, want {true X2 because}", got)
		}
		if p.called != 1 {
			t.Errorf("provider asked %d times, want 1", p.called)
		}
	})

	t.Run("the question reaches the identity intact", func(t *testing.T) {
		// The RESOLVED env, not the empty one: an identity holds per-env state, so
		// the wrong env answers about a different deployment — or, when that env has
		// no session at all, answers nothing and silently drops the grade.
		gradeFor(gradeFactory(false), identity,
			api.Operation{Name: "list", Access: api.Read, Permission: perm})
		if p.gotEnv != hosts.Env("prod") {
			t.Errorf("env = %q, want prod", p.gotEnv)
		}
		if p.gotReq != perm {
			t.Errorf("req = %+v, want the operation's own requirement", p.gotReq)
		}
		if p.gotAccess != api.Read {
			t.Errorf("access = %v, want Read", p.gotAccess)
		}
		// And a write is passed as a write: the access is the whole reason a grade
		// can word a read and a write differently.
		gradeFor(gradeFactory(false), identity,
			api.Operation{Name: "place", Access: api.Write, Permission: perm})
		if p.gotAccess != api.Write {
			t.Errorf("access = %v, want Write", p.gotAccess)
		}
	})

	t.Run("an operation with no permission requirement is not asked about", func(t *testing.T) {
		p.called = 0
		// nil Factory on purpose: nothing may be read off it on this path, which is
		// what keeps the common case from depending on a populated Factory.
		if got := gradeFor(nil, identity, api.Operation{Name: "list", Access: api.Read}); got.Confirm {
			t.Errorf("graded %+v, want the zero value", got)
		}
		if p.called != 0 {
			t.Errorf("provider asked %d times for an ungraded op, want 0", p.called)
		}
	})

	t.Run("a dry-run is not graded, so a preview never reads the credential store", func(t *testing.T) {
		p.called = 0
		got := gradeFor(gradeFactory(true), identity,
			api.Operation{Name: "list", Access: api.Read, Permission: perm})
		if got.Confirm {
			t.Errorf("dry-run graded %+v, want the zero value", got)
		}
		if p.called != 0 {
			t.Errorf("provider asked %d times under --dry-run, want 0", p.called)
		}
	})

	t.Run("an identity that does not grade grades nothing", func(t *testing.T) {
		got := gradeFor(gradeFactory(false), api.Identity("nobody-registered"),
			api.Operation{Name: "list", Access: api.Read, Permission: perm})
		if got.Confirm {
			t.Errorf("graded %+v, want the zero value", got)
		}
	})
}

// TestGradedReadConfirmation_CLIWiring drives a graded read through the whole
// command tree, exactly as TestWriteConfirmation_CLIWiring does for a write.
// Everything above hands confirmForCLI a grade already made; only this proves
// runOperation ASKS for one and lets it open the gate. Without it, the line that
// wires the feature into this face can be deleted and every other test still
// passes — which is how it stood until this test existed.
//
// The topology is local rather than the shared fixture's: a graded operation
// needs an identity that both grades and carries grants, and adding one to
// domains() would put it under every contract test that walks every op.
func TestGradedReadConfirmation_CLIWiring(t *testing.T) {
	unattended(t)

	p := &gradingIdentityProvider{grade: credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "graded: the identity demands it",
	}}
	credential.Register(p)

	ds := []api.DomainSpec{{
		Name: "sample", Short: "Fixture domain",
		Subs: []api.Subdomain{{
			Name: "graded", Short: "A read its own identity grades",
			Signed: api.SignNone, Identity: api.Identity(p.Realm()), HostKey: testHostKey,
			Ops: []api.Operation{{
				Name: "list", Method: "GET", Path: "/fixture/graded",
				Access: api.Read, Summary: "A graded read",
				Permission: &api.PermReq{System: "fixture", Codes: []string{"fixture:list"}},
			}},
		}},
	}}
	runGraded := func(args ...string) (string, int) {
		var out, errBuf bytes.Buffer
		app := NewApp("mexc-cli", "", ds, nil, nil, true,
			func(string) string { return "" }, &out, &errBuf)
		code := app.Run(args)
		return out.String() + errBuf.String(), code
	}

	// The gate opens for a read that never used to reach it, and the refusal
	// carries the identity's own sentence — which nothing but the provider could
	// have supplied.
	got, code := runGraded("sample", "graded", "list")
	if code == 0 {
		t.Fatalf("a graded read ran without confirmation:\n%s", got)
	}
	if !strings.Contains(got, "this read needs") {
		t.Errorf("the refusal does not call it a read:\n%s", got)
	}
	if !strings.Contains(got, "graded: the identity demands it") {
		t.Errorf("the identity's reason never reached the prompt:\n%s", got)
	}

	// A preview sends nothing, so there is nothing to confirm and nothing to ask
	// the identity about. This is the wiring-level guard for the dry-run exemption.
	got, code = runGraded("sample", "graded", "list", "--dry-run")
	if code != 0 {
		t.Fatalf("--dry-run on a graded read was refused (exit %d):\n%s", code, got)
	}
	if strings.Contains(got, "explicit confirmation") {
		t.Errorf("--dry-run asked for a confirmation:\n%s", got)
	}
}

// The cancel paths named everything a write too. A person who has just refused
// a read must not be told they refused a write.
func TestConfirmForCLI_CancelledGradedReadIsNamedARead(t *testing.T) {
	prevStdio, prevRun := stdioIsTerminalFn, runConfirmTUIFn
	stdioIsTerminalFn = func() bool { return true }
	runConfirmTUIFn = func(string, string, api.RiskTier) (bool, bool) { return false, true } // asked, refused
	t.Cleanup(func() { stdioIsTerminalFn, runConfirmTUIFn = prevStdio, prevRun })

	op := api.Operation{Name: "list", Access: api.Read, Summary: "List things"}
	_, err := confirmForCLI(nil, confirmaudit.Event{}, op, nil, "",
		false, credential.ConfirmGrade{Confirm: true, Code: "X2", Reason: "graded: because"})
	if err == nil {
		t.Fatal("a cancelled graded read was allowed through")
	}
	if msg := err.Error(); !strings.Contains(msg, `read "list" was cancelled`) {
		t.Errorf("cancellation does not name the read:\n%s", msg)
	}
}
