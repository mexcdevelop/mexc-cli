package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/console"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// nonTerminal points both tty paths at a regular file, which is what the real
// opener sees when there is no controlling terminal. Used against the REAL
// openTTY so the refusal is proven by the shipping code, not by a stub.
//
// BOTH paths, and to the SAME file: that reproduces the one-device shape this
// platform actually has. The two-device shape gets its own test below.
func nonTerminal(t *testing.T) {
	t.Helper()
	redirectTTY(t, regularFile(t, "not-a-tty"), "")
}

func regularFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

// redirectTTY points the opener at the given paths. An empty out means "the same
// device as in", which is how Unix is shaped; two distinct paths is how Windows
// is shaped.
func redirectTTY(t *testing.T, in, out string) {
	t.Helper()
	if out == "" {
		out = in
	}
	prevIn, prevOut := ttyInPath, ttyOutPath
	ttyInPath, ttyOutPath = in, out
	t.Cleanup(func() { ttyInPath, ttyOutPath = prevIn, prevOut })
}

// The two-device shape, which is the one Windows takes, driven through the real
// openTTY on a platform that has one device. It proves the refusal survives the
// split — that nothing in openTTY assumed the two sides were the same handle,
// and that a caller still gets the relayable hint rather than a bare I/O error.
func TestOpenTTY_RefusesWhenTheDeviceIsTwoSeparatePaths(t *testing.T) {
	redirectTTY(t, regularFile(t, "in"), regularFile(t, "out"))

	_, err := openTTY("saving a profile", "profile set <name>")
	if err == nil {
		t.Fatal("two regular files must not be accepted as a console")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T", err)
	}
	if e.Subtype != errs.SubtypeInteractiveRequired {
		t.Errorf("subtype = %q, want interactive_required — the same refusal as the one-device shape", e.Subtype)
	}
	if !strings.Contains(e.Hint, "own terminal") {
		t.Errorf("hint must still be the one an agent relays, got %q", e.Hint)
	}
}

// THE control. Without a terminal the command refuses — and the refusal must
// carry a hint an agent can relay verbatim, because relaying is its only
// correct move.
func TestReadCredentials_RefusesWithoutATerminal(t *testing.T) {
	nonTerminal(t)

	_, _, err := readCredentials()
	if err == nil {
		t.Fatal("saving a profile without a terminal must be refused")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T", err)
	}
	// interactive_required, with its own exit code: an agent must be able to tell
	// "stand aside, the user acts, then retry unchanged" from "your arguments were
	// wrong" — reading this as the latter is what sends it hunting for a variation
	// that works.
	if e.Subtype != errs.SubtypeInteractiveRequired {
		t.Errorf("subtype = %q, want interactive_required", e.Subtype)
	}
	if got := errs.ExitCode(err); got != errs.ExitInteractiveRequired {
		t.Errorf("exit code = %d, want %d (not %d, the plain validation code)",
			got, errs.ExitInteractiveRequired, 2)
	}
	if !strings.Contains(e.Hint, "own terminal") {
		t.Errorf("hint must tell the agent to relay to the user, got %q", e.Hint)
	}
	// The relayed command must be the caller's own. A refusal that points at the
	// wrong command is worse than none: an agent passes it on verbatim, and the
	// user runs something that does not address what just failed.
	if !strings.Contains(e.Hint, "profile set <name>") {
		t.Errorf("hint must name the command that was refused, got %q", e.Hint)
	}
	// The hint must close off the channels an agent would otherwise go hunting
	// for, and must NOT overclaim: it says no such channel exists, not that an
	// agent is unable to store a credential (it can drive its own pty — see the
	// rationale in tty.go).
	if !strings.Contains(e.Hint, "no flag, stdin or environment variable") {
		t.Errorf("hint must foreclose the workarounds, got %q", e.Hint)
	}
	for _, overclaim := range []string{"cannot reach", "an agent cannot", "never reaches"} {
		if strings.Contains(e.Hint, overclaim) {
			t.Errorf("hint must not claim a barrier it does not have (%q): %q", overclaim, e.Hint)
		}
	}
}

// The regression guard for the removal. `profile set` must declare NO flag that
// could carry a credential: a flag is the one input channel that writes a secret
// into a command line, and a command line is the artefact that gets recorded in
// an agent transcript and shell history. The terminal requirement does not cover
// for this — a process can manufacture a terminal — so the absence of the flags
// is the part that actually holds.
func TestProfileSetDeclaresNoCredentialFlags(t *testing.T) {
	cmd := newProfileSetCmd(&stubDeps{})
	// Both spellings: the prompts now say Access Key / Secret Key, so those are
	// the names a future flag would most plausibly be given.
	for _, name := range []string{
		"access-key", "secret-key",
		"api-key", "api-secret",
		"key", "secret", "password", "token",
	} {
		if f := cmd.Flags().Lookup(name); f != nil {
			t.Errorf("`profile set` must not accept --%s: a credential in argv is recorded", name)
		}
	}
	if n := cmd.Flags().NFlag(); n != 0 {
		t.Errorf("unexpected flags on `profile set` (%d)", n)
	}
}

// withStubTTY replaces the opener (a temp file stands in for the terminal, and
// is never read because the prompter is stubbed too) and records whether the
// prompter ran.
func withStubTTY(t *testing.T, prompt func(*os.File) (string, string, error)) *bool {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "fake-tty"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	prevOpen, prevPrompt := ttyOpener, credentialPrompter
	called := false
	// One handle for both sides, matching the shape the stub stands in for; the
	// prompter is stubbed too, so neither side is actually used.
	ttyOpener = func(string, string) (console.Pair, error) {
		return console.Pair{In: f, Out: f}, nil
	}
	credentialPrompter = func(tty console.Pair) (string, string, error) {
		called = true
		return prompt(tty.In)
	}
	t.Cleanup(func() { ttyOpener, credentialPrompter = prevOpen, prevPrompt })
	return &called
}

// The prompt is the only source. There is nothing to supply, so there is nothing
// to prefer over it.
func TestReadCredentials_TakesBothFromThePrompt(t *testing.T) {
	called := withStubTTY(t, func(*os.File) (string, string, error) {
		return "typed-key", "typed-secret", nil
	})

	key, secret, err := readCredentials()
	if err != nil {
		t.Fatalf("readCredentials: %v", err)
	}
	if !*called {
		t.Fatal("the prompt must run; it is the only input channel")
	}
	if key != "typed-key" || secret != "typed-secret" {
		t.Errorf("got (%q, %q), want what was typed", key, secret)
	}
}

// An empty answer is a validation error, not a profile saved with blank
// credentials — which would look configured and resolve to nothing, the exact
// state doctor had to grow a check for.
func TestReadCredentials_RefusesAnEmptyAnswer(t *testing.T) {
	for _, c := range []struct {
		name           string
		gotKey, gotSec string
	}{
		{"empty secret", "K", ""},
		{"empty key", "", "S"},
		{"both empty", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			withStubTTY(t, func(*os.File) (string, string, error) {
				return c.gotKey, c.gotSec, nil
			})
			_, _, err := readCredentials()
			if err == nil {
				t.Fatal("a blank half must be refused")
			}
			var e *errs.Error
			if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
				t.Fatalf("want invalid_argument, got %v", err)
			}
		})
	}
}

// Pasted input can carry escape sequences; storing them would let a credential
// file replay control codes into a terminal, and would silently corrupt the
// value besides.
func TestStripTerminalControls(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"ab\x1b[31mcd", "abcd"},  // CSI colour sequence
		{"ab\x1bOPcd", "abcd"},    // SS3
		{"a\x00b\x07c", "abc"},    // stray C0 bytes
		{"key\x1b", "key"},        // lone trailing ESC
		{"\x1b[2Jwiped", "wiped"}, // clear-screen
		{"tab\there", "tabhere"},  // tab is a control byte
		{"ünïcödé", "ünïcödé"},    // multi-byte text survives
	}
	for _, c := range cases {
		if got := stripTerminalControls(c.in); got != c.want {
			t.Errorf("stripTerminalControls(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// stubDeps is a minimal framework.Deps for building a command in isolation and
// running it without a real edition behind it. Only what these tests touch is
// implemented; Client is never called on the credential paths.
type stubDeps struct {
	out, errOut bytes.Buffer
	readOnly    bool
	assumeYes   bool
	lastErr     error
}

func (s *stubDeps) Config() config.Config  { return config.Config{ReadOnly: s.readOnly} }
func (*stubDeps) DryRun() bool             { return false }
func (s *stubDeps) AssumeYes() bool        { return s.assumeYes }
func (*stubDeps) Client() transport.Sender { return nil }

// ClientFor mirrors Client: these tests exercise the confirmation prompt, which
// never sends. It exists because framework.Deps requires it — a face that serves
// remote callers needs a per-caller Sender, and this stub stands in for the whole
// interface.
func (*stubDeps) ClientFor(context.Context) transport.Sender { return nil }
func (s *stubDeps) Out() io.Writer                           { return &s.out }
func (s *stubDeps) Err() io.Writer                           { return &s.errOut }
func (s *stubDeps) WriteJSON(v any) error                    { return json.NewEncoder(&s.out).Encode(v) }
func (s *stubDeps) WriteError(err error)                     { s.lastErr = err }

// The end-to-end refusal, driven through the real cobra command so flag parsing
// and RunE are exercised.
//
// It lives HERE, not in editions/public with the other end-to-end tests, because
// only this package can redirect the tty paths. Driving it from there read the
// DEVELOPER's terminal and hung forever — and `scripts/install-hooks.sh` runs
// `go test ./...` from a pre-commit hook, so it hung their `git commit` too.
func TestProfileSetCmd_RefusedWithoutATerminal(t *testing.T) {
	isolatedHome(t)
	nonTerminal(t)

	d := &stubDeps{}
	cmd := newProfileSetCmd(d)
	cmd.SetArgs([]string{"prod"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("`profile set` must fail without a terminal")
	}
	var e *errs.Error
	if !errors.As(d.lastErr, &e) {
		t.Fatalf("want a structured error on the deps, got %v", d.lastErr)
	}
	if e.Subtype != errs.SubtypeInteractiveRequired {
		t.Errorf("subtype = %q, want interactive_required", e.Subtype)
	}
	if !strings.Contains(e.Hint, "own terminal") {
		t.Errorf("hint must be relayable to the user, got %q", e.Hint)
	}
	// Nothing half-written: a refused save must leave the store untouched.
	names, lerr := profile.List()
	if lerr != nil {
		t.Fatalf("List: %v", lerr)
	}
	if len(names) != 0 {
		t.Errorf("a refused save must store nothing, got %v", names)
	}
}

// Each caller relays ITS OWN command. openTTY takes the relay text as a
// parameter for exactly this reason — it was hard-coded to `profile set`, so the
// downgrade command told users to run something unrelated to what they tried.
func TestOpenTTY_RelaysTheCallersOwnCommand(t *testing.T) {
	nonTerminal(t)
	for _, c := range []struct{ what, relay string }{
		{"saving a profile", "profile set <name>"},
		{"downgrading credential storage", "profile keychain-downgrade"},
	} {
		_, err := openTTY(c.what, c.relay)
		if err == nil {
			t.Fatalf("%s: expected a refusal", c.what)
		}
		var e *errs.Error
		if !errors.As(err, &e) {
			t.Fatalf("%s: want a structured error, got %T", c.what, err)
		}
		if !strings.Contains(e.Message, c.what) {
			t.Errorf("message must name the action, got %q", e.Message)
		}
		if !strings.Contains(e.Hint, c.relay) {
			t.Errorf("hint must name %q, got %q", c.relay, e.Hint)
		}
	}
}

// The shape that used to hang `profile set`: this process's streams are pipes
// (an agent holds them) while the controlling terminal it inherited opens fine.
// The paths are left at the REAL device on purpose — pointing them at a regular
// file would test console.Open's refusal, which already worked; what needs
// proving is that openTTY refuses before it ever asks the device.
//
// On a machine with a console this is exactly the pre-fix hang: openTTY returned
// a usable Pair and the caller prompted into it. On a machine without one the
// device would refuse anyway, so the assertion is written to hold either way —
// what it pins is the SUBTYPE and that no prompt is possible.
func TestOpenTTY_RefusesWhenStdioIsPipedEvenIfTheDeviceOpens(t *testing.T) {
	orig := stdioIsTerminal
	stdioIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdioIsTerminal = orig })

	p, err := openTTY("saving a profile", "profile set <name>")
	if err == nil {
		_ = p.Close()
		t.Fatal("openTTY must refuse a caller whose own stdio is piped — returning the inherited " +
			"terminal is what made this command prompt where nobody could answer, and hang")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T: %v", err, err)
	}
	if e.Subtype != errs.SubtypeInteractiveRequired {
		t.Errorf("subtype = %q, want interactive_required — the agent must be told to stand aside, "+
			"not to retry with different arguments", e.Subtype)
	}
	if errs.ExitCode(err) != errs.ExitInteractiveRequired {
		t.Errorf("exit code = %d, want %d", errs.ExitCode(err), errs.ExitInteractiveRequired)
	}
	if !strings.Contains(e.Hint, "profile set <name>") {
		t.Errorf("hint must relay the command the USER should run, got %q", e.Hint)
	}
}

// The gate must not swallow the attended case: with stdio reporting a terminal,
// openTTY goes on to ask the device exactly as before. Driven with the paths at a
// regular file so the outcome is deterministic on a machine with or without a
// console — what is asserted is that the DEVICE got to answer, which its own
// refusal subtype proves.
func TestOpenTTY_AttendedStdioStillAsksTheDevice(t *testing.T) {
	origStdio := stdioIsTerminal
	stdioIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdioIsTerminal = origStdio })

	dir := t.TempDir()
	p := filepath.Join(dir, "not-a-tty")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	redirectTTY(t, p, p)

	if _, err := openTTY("saving a profile", "profile set <name>"); err == nil {
		t.Fatal("a path that is not a terminal must still be refused by the device check")
	}
}
