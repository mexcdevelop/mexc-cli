package profile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

func TestProfileRoundTrip(t *testing.T) {
	isolatedHome(t)

	in := Profile{Name: "prod", APIKey: "ak", SecretKey: "sk"}
	if err := Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load("prod")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != in {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, in)
	}
}

// Save persists only api-key / api-secret; the writer must never emit an
// env= line (env is a global setting now, see settings.go) nor a base-url= line
// (the endpoint is chosen by env x the host table, never a URL in the profile).
func TestSaveProfile_WriterOmitsEnvAndBaseURL(t *testing.T) {
	isolatedHome(t)
	if err := Save(Profile{Name: "p", APIKey: "a", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	// Assert on the DECRYPTED payload, not the file. Reading the file was the
	// original test and it went silently vacuous the moment profiles were sealed:
	// grepping ciphertext for "env=" can never match, so the assertion held while
	// the writer was free to persist anything. Proven by mutation — adding an
	// `env=` line back to Save left the suite green.
	raw, err := os.ReadFile(filepath.Join(mustProfilesDir(t), "p"))
	if err != nil {
		t.Fatal(err)
	}
	plain, oerr := secretstore.Open(keyRef, raw)
	if oerr != nil {
		t.Fatalf("stored profile must decrypt: %v", oerr)
	}
	content := string(plain)
	if !strings.Contains(content, "api-key=a") || !strings.Contains(content, "api-secret=s") {
		t.Fatalf("the payload must carry the credential, got:\n%s", content)
	}
	// env and the endpoint are global settings, never profile fields.
	if strings.Contains(content, "env=") {
		t.Fatalf("a profile must not persist an env= line, got:\n%s", content)
	}
	if strings.Contains(content, "base-url=") {
		t.Fatalf("a profile must not persist a base-url= line, got:\n%s", content)
	}
}

func mustProfilesDir(t *testing.T) string {
	t.Helper()
	pd, err := profilesDir()
	if err != nil {
		t.Fatal(err)
	}
	return pd
}

// This is a pre-release tool with no published profiles to stay compatible
// with, so the retired base-url= / testnet= settings are simply ignored on
// load: a profile carrying them still loads (its api-key/secret parse normally),
// the retired keys have no effect (including a retired env= line, now that env is
// a global setting), and loading is silent — no warning, no error.
func TestLoadProfile_RetiredKeysIgnored(t *testing.T) {
	home := isolatedHome(t)
	pd := filepath.Join(home, ".mexc", "profiles")
	if err := os.MkdirAll(pd, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "api-key=a\napi-secret=s\nbase-url=https://old.example\ntestnet=true\nenv=testnet\n"
	// Sealed: the retired keys are ignored INSIDE the decrypted payload, which is
	// the property under test. The on-disk format itself is encrypted-only.
	blob, _, serr := secretstore.Seal(keyRef, []byte(body))
	if serr != nil {
		t.Fatalf("seal: %v", serr)
	}
	if err := os.WriteFile(filepath.Join(pd, "legacy"), blob, 0o600); err != nil {
		t.Fatal(err)
	}

	var got Profile
	stderr := captureStderr(t, func() {
		var err error
		got, err = Load("legacy")
		if err != nil {
			t.Fatalf("a profile with retired keys must still load: %v", err)
		}
	})
	if got.APIKey != "a" || got.SecretKey != "s" {
		t.Fatalf("unexpected profile fields: %+v", got)
	}
	if stderr != "" {
		t.Fatalf("expected no warning for retired keys, got: %q", stderr)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = orig
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestSaveProfile_Writes0600(t *testing.T) {
	home := isolatedHome(t)
	if err := Save(Profile{Name: "p", APIKey: "a", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, ".mexc", "profiles", "p"))
	if err != nil {
		t.Fatal(err)
	}
	// Only the mode half is platform-dependent. That the save lands at this exact
	// path is the contract every platform keeps, so the stat above stays
	// unguarded — skipping the whole test would drop that coverage too.
	requireRealModeBits(t)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadProfile_RejectsInsecurePerms(t *testing.T) {
	requireRealModeBits(t)
	home := isolatedHome(t)
	if err := Save(Profile{Name: "p", APIKey: "a", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".mexc", "profiles", "p")
	if err := os.Chmod(path, 0o644); err != nil { // group/world readable
		t.Fatal(err)
	}
	if _, err := Load("p"); err == nil {
		t.Fatal("expected Load to reject a world-readable profile")
	}
}

func TestLoadProfile_RejectsInsecureDirPerms(t *testing.T) {
	requireRealModeBits(t)
	home := isolatedHome(t)
	if err := Save(Profile{Name: "p", APIKey: "a", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	// File stays 0600, but widen the containing dir — another local user could
	// then traverse in and read the 0600 file.
	pd := filepath.Join(home, ".mexc", "profiles")
	if err := os.Chmod(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("p"); err == nil {
		t.Fatal("expected Load to reject a world-traversable profiles dir")
	}
}

func TestSaveProfile_Validation(t *testing.T) {
	isolatedHome(t)
	if err := Save(Profile{Name: "../escape", APIKey: "a", SecretKey: "s"}); err == nil {
		t.Error("path-traversal name must be rejected")
	}
	if err := Save(Profile{Name: "ok", APIKey: "a"}); err == nil {
		t.Error("a profile without a secret must be rejected")
	}
	// A newline in any value would be re-parsed on load as an extra key=value
	// line — silently truncating the secret and letting an injected base-url
	// redirect signed requests. Save must reject it up front.
	if err := Save(Profile{Name: "inject", APIKey: "a", SecretKey: "s\nbase-url=https://evil.example"}); err == nil {
		t.Error("a secret containing a newline must be rejected")
	}
	if err := Save(Profile{Name: "inject", APIKey: "a\nx=y", SecretKey: "s"}); err == nil {
		t.Error("an api key containing a newline must be rejected")
	}
}

func TestActiveProfile_RoundTripAndClear(t *testing.T) {
	isolatedHome(t)

	if a, _ := Active(); a != "" {
		t.Fatalf("fresh home should have no active profile, got %q", a)
	}
	if err := SetActive("prod"); err != nil {
		t.Fatal(err)
	}
	if a, _ := Active(); a != "prod" {
		t.Fatalf("active = %q, want prod", a)
	}
	if err := SetActive(""); err != nil {
		t.Fatal(err)
	}
	if a, _ := Active(); a != "" {
		t.Fatalf("active not cleared, got %q", a)
	}
}

func TestRemoveProfile_ClearsActive(t *testing.T) {
	isolatedHome(t)
	if err := Save(Profile{Name: "p", APIKey: "a", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := SetActive("p"); err != nil {
		t.Fatal(err)
	}
	if err := Remove("p"); err != nil {
		t.Fatal(err)
	}
	if a, _ := Active(); a != "" {
		t.Fatalf("removing the active profile must clear the active pointer, got %q", a)
	}
}

// The shared layer deliberately names no command — it is compiled into every product
// built on the module, and only this one has `profile keychain-downgrade`. This
// product puts the name back, because a hint an agent relays verbatim has to be
// runnable. Both directions matter: the remedy sentence must gain the command, and
// an unrelated error must be left completely alone.
func TestNameTheEscapeHatch(t *testing.T) {
	requireKeyring(t)
	// The two shared-layer hints that offer the local-file remedy both contain this
	// clause; the wrapper keys on it.
	const generic = "Either unlock it and retry unchanged, or pin the key to a local file so " +
		"this context stops needing the keyring, if this CLI offers a command for that."

	in := errs.Validation(errs.SubtypeFailedPrecondition, "the OS keyring did not answer").
		WithHint("%s", generic).
		WithCause(secretstore.ErrUndecryptable)
	out := nameTheEscapeHatch(in)

	var e *errs.Error
	if !errors.As(out, &e) {
		t.Fatalf("want a structured error, got %T", out)
	}
	if !strings.Contains(e.Hint, "profile keychain-downgrade --yes") {
		t.Errorf("hint must name the command this product has, got %q", e.Hint)
	}
	if !strings.Contains(e.Hint, generic) {
		t.Errorf("the shared layer's own sentence must survive underneath, got %q", e.Hint)
	}
	// Only the hint is touched, so a caller can still route on the cause.
	if !errors.Is(out, secretstore.ErrUndecryptable) {
		t.Error("the cause must still traverse: a caller deciding whether data is dead reads it")
	}
	if e.Message != "the OS keyring did not answer" || e.Subtype != errs.SubtypeFailedPrecondition {
		t.Errorf("message and subtype must be untouched, got %q / %q", e.Message, e.Subtype)
	}

	// An error that offers no such remedy is not this function's business. Asserted on
	// the hint rather than on identity: the hint is the observable requirement, and it
	// is also the only thing this function is allowed to touch.
	untouched := errs.Validation(errs.SubtypeNotFound, "profile %q does not exist", "x").
		WithHint("create it first")
	_ = nameTheEscapeHatch(untouched)
	if untouched.Hint != "create it first" {
		t.Errorf("an unrelated hint must not be rewritten, got %q", untouched.Hint)
	}
}

// The counterpart of TestNameTheEscapeHatch. Where no keyring is consulted,
// `profile keychain-downgrade` refuses, so naming it would hand an agent a
// command that cannot work — the exact failure nameTheEscapeHatch exists to
// prevent, arriving from the other direction. The shared layer's own conditional
// sentence is left to stand alone.
func TestNameTheEscapeHatch_LeavesTheHintAloneWhereThereIsNoKeyring(t *testing.T) {
	requireNoKeyring(t)
	const generic = "Either unlock it and retry unchanged, or pin the key to a local file so " +
		"this context stops needing the keyring, if this CLI offers a command for that."

	in := errs.Validation(errs.SubtypeFailedPrecondition, "the OS keyring did not answer").
		WithHint("%s", generic).
		WithCause(secretstore.ErrUndecryptable)
	out := nameTheEscapeHatch(in)

	var e *errs.Error
	if !errors.As(out, &e) {
		t.Fatalf("want a structured error, got %T", out)
	}
	if strings.Contains(e.Hint, "keychain-downgrade") {
		t.Errorf("the hint names a command that refuses on this platform, got %q", e.Hint)
	}
	if e.Hint != generic {
		t.Errorf("the hint must be passed through untouched, got %q", e.Hint)
	}
	if !errors.Is(out, secretstore.ErrUndecryptable) {
		t.Error("the cause must still traverse")
	}
}
