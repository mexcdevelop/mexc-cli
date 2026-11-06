package command

import (
	"os"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

// isolatedHome points HOME at a fresh directory AND provisions a file-backed
// encryption key inside it, returning that directory.
//
// Both halves are needed: profiles are sealed at rest (pkg/secretstore), so without
// a key nothing can be saved or loaded. Two steps, because the downgrade only ever
// COPIES a key: Seal mints one in the mocked keyring, then the downgrade pins it to
// a local file.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, _, err := secretstore.Seal(profile.KeyRef(), []byte("provision")); err != nil {
		t.Fatalf("provision test encryption key: %v", err)
	}
	if _, err := profile.DowngradeKeyToFile(); err != nil {
		t.Fatalf("pin the test encryption key to a file: %v", err)
	}
	return home
}

// writeSealed puts content on disk in the format Load expects. Tests that need a
// profile Save would refuse to create — an empty secret, say — still have to
// produce a sealed file, because the on-disk format is encrypted-only.
func writeSealed(t *testing.T, path, content string) {
	t.Helper()
	blob, _, err := secretstore.Seal(profile.KeyRef(), []byte(content))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// stubKeyBackend fixes what doctor reports about at-rest protection for the
// duration of one test. Without it every credentials assertion would inherit the
// harness's own state: isolatedHome downgrades to a key file, so the real
// backend is always "file" under test, and assertions about anything else would
// be reading the fixture rather than the code.
func stubKeyBackend(t *testing.T, backend string) {
	t.Helper()
	prev := keyBackend
	keyBackend = func() string { return backend }
	t.Cleanup(func() { keyBackend = prev })
}

// requireRealModeBits skips a test whose subject is a file-permission check.
// Where the OS synthesises mode bits, this product deliberately does not check
// them, so asserting that a wide mode gets refused would assert the opposite of
// the intended behaviour — see secfile.ModeBitsMeaningful. Skipping states that;
// leaving the test to fail would read as a port that is broken rather than one
// that made a decision.
func requireRealModeBits(t *testing.T) {
	t.Helper()
	if !secfile.ModeBitsMeaningful {
		t.Skip("this platform synthesises file mode bits; the check under test does not run here")
	}
}

// requireKeyring / requireNoKeyring split the tests whose subject only exists on
// one side of the platform gate. Several behaviours here are macOS-shaped by
// nature — a "downgrade" needs something to downgrade FROM — and their
// counterparts are the wording a platform with only a key file gets instead.
//
// Written as skips rather than one test with a branch, because the two sides
// assert different things about different code and a reader should see which one
// ran.
func requireKeyring(t *testing.T) {
	t.Helper()
	if !secretstore.KeyringAvailable() {
		t.Skip("this platform consults no OS keyring; the behaviour under test does not exist here")
	}
}

func requireNoKeyring(t *testing.T) {
	t.Helper()
	if secretstore.KeyringAvailable() {
		t.Skip("this platform has an OS keyring; the no-keyring wording is not what it produces")
	}
}
