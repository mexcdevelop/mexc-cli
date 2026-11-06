package command

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

func TestMask(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "(empty)"},
		{"abc", "****"},              // very short
		{"shortsecret", "****"},      // 11 chars, still fully hidden (< 12)
		{"abcdefghijkl", "****ijkl"}, // 12 chars, tail revealed
		{"a-long-real-secret-value", "****alue"},
	}
	for _, c := range cases {
		if got := mask(c.in); got != c.want {
			t.Errorf("mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Saving under the weaker backend must SAY so. Seal falls back to a local key
// rather than refusing — which keeps the product usable where no keyring exists
// — and silence there would leave the user believing in protection they do not
// have. The warning is the whole justification for allowing the fallback.
func TestProfileSet_WarnsWhenTheKeyLandsInALocalFile(t *testing.T) {
	requireKeyring(t)
	isolatedHome(t) // provisions a file-backed key, i.e. the weaker backend
	withStubTTY(t, func(*os.File) (string, string, error) { return "K", "S", nil })

	d := &stubDeps{}
	cmd := newProfileSetCmd(d)
	cmd.SetArgs([]string{"prod"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("save: %v (%v)", err, d.lastErr)
	}
	got := d.errOut.String()
	if !strings.Contains(got, "local file") || !strings.Contains(got, "not in the OS keyring") {
		t.Errorf("stderr must name the weaker backend, got %q", got)
	}
	if !strings.Contains(got, "copies both") {
		t.Errorf("stderr must say why it matters, got %q", got)
	}
	// It must not claim the machine has no keyring: the usual reason for landing here
	// is one that is present and out of reach, and a reader told they have none stops
	// looking for the one they have.
	if strings.Contains(got, "no OS keyring on this machine") {
		t.Errorf("stderr must not deny the keyring exists, got %q", got)
	}
}

// Pinning the key to a local file lowers the protection of stored credentials,
// so it takes an explicit --yes — the same say-so every other write in this tree
// requires, and routable by its own exit code.
func TestProfileKeychainDowngrade_RequiresExplicitConfirmation(t *testing.T) {
	requireKeyring(t)
	isolatedHome(t)
	withStubTTY(t, func(*os.File) (string, string, error) { return "", "", nil })

	d := &stubDeps{}
	cmd := newProfileKeychainDowngradeCmd(d)
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("downgrading without --yes must be refused")
	}
	var e *errs.Error
	if !errors.As(d.lastErr, &e) {
		t.Fatalf("want a structured error, got %v", d.lastErr)
	}
	if e.Subtype != errs.SubtypeConfirmationRequired {
		t.Errorf("subtype = %q, want confirmation_required", e.Subtype)
	}
	if errs.ExitCode(e) != errs.ExitConfirmationRequired {
		t.Errorf("exit = %d, want %d", errs.ExitCode(e), errs.ExitConfirmationRequired)
	}

	// With --yes it proceeds.
	d2 := &stubDeps{assumeYes: true}
	cmd2 := newProfileKeychainDowngradeCmd(d2)
	cmd2.SetArgs(nil)
	cmd2.SetOut(io.Discard)
	cmd2.SetErr(io.Discard)
	if err := cmd2.Execute(); err != nil {
		t.Fatalf("with --yes it must proceed: %v (%v)", err, d2.lastErr)
	}
	if !strings.Contains(d2.out.String(), "backend") {
		t.Errorf("it must report the resulting backend, got %q", d2.out.String())
	}
}

// The counterpart of the two tests above, and the only coverage the no-keyring
// wording has: where a key file is the only key store, saving must still say
// where the key is, but as a NOTE about a normal state rather than a warning
// about a degraded one.
func TestProfileSet_NotesTheKeyStoreWhereThereIsNoKeyring(t *testing.T) {
	requireNoKeyring(t)
	isolatedHome(t)
	withStubTTY(t, func(*os.File) (string, string, error) { return "K", "S", nil })

	d := &stubDeps{}
	cmd := newProfileSetCmd(d)
	cmd.SetArgs([]string{"prod"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("save: %v (%v)", err, d.lastErr)
	}
	got := d.errOut.String()
	if !strings.Contains(got, "note:") {
		t.Errorf("a normal state must not be reported as a warning, got %q", got)
	}
	if !strings.Contains(got, "local file") || !strings.Contains(got, "only") {
		t.Errorf("it must still say where the key is and that it is the only store, got %q", got)
	}
	// The one instruction that matters on every platform: the file is the only way
	// back to the data whether or not the OS wraps it.
	if !strings.Contains(got, "do not delete it") {
		t.Errorf("it must warn against deleting the key file, got %q", got)
	}
	// Whether a copied folder is dangerous depends on the wrap, and claiming the
	// wrong one either scares the reader or lies about the protection.
	if secretstore.KeyFileWrappedByOS() {
		if strings.Contains(got, "copies both") {
			t.Errorf("the OS wraps the key here; a copy carries nothing: %q", got)
		}
	} else if !strings.Contains(got, "copies both") {
		t.Errorf("the file is the key here; the caveat must be stated: %q", got)
	}
}

// keychain-downgrade is registered everywhere so `--help` reads the same, and it
// copies a key OUT of a keyring — so where there is no keyring it must refuse up
// front rather than reach DowngradeToFile, whose "no key to copy" reads as a
// recoverable problem and sends the reader hunting for a keyring to unlock.
//
// --yes is passed deliberately: a platform refusal must not be bypassable by the
// flag that bypasses the confirmation.
func TestProfileKeychainDowngrade_RefusesWhereThereIsNoKeyring(t *testing.T) {
	requireNoKeyring(t)
	isolatedHome(t)

	d := &stubDeps{assumeYes: true}
	cmd := newProfileKeychainDowngradeCmd(d)
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("it must refuse where there is no keyring to copy from, even with --yes")
	}
	var e *errs.Error
	if !errors.As(d.lastErr, &e) {
		t.Fatalf("want a structured error, got %v", d.lastErr)
	}
	if strings.Contains(e.Message, "holds no key to copy") {
		t.Errorf("that wording reads as a recoverable keyring problem, got %q", e.Message)
	}
	if !strings.Contains(e.Message, "this platform") {
		t.Errorf("the refusal must name the reason as the platform, got %q", e.Message)
	}
	// It must not leave the reader thinking their storage is degraded.
	if !strings.Contains(e.Hint, "not a downgraded one") {
		t.Errorf("hint must say the local file is normal here, got %q", e.Hint)
	}
}
