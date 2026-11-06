package profile

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

// TestMain runs the package against go-keyring's in-memory mock: overriding HOME is
// what makes the login keychain unreachable on macOS, and without the mock the
// provisioning below would attempt a write against the developer's own keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

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
	if _, _, err := secretstore.Seal(keyRef, []byte("provision")); err != nil {
		t.Fatalf("provision test encryption key: %v", err)
	}
	if _, err := secretstore.DowngradeToFile(keyRef); err != nil {
		t.Fatalf("pin the test encryption key to a file: %v", err)
	}
	return home
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
