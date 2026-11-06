package adapter

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
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
	if _, _, err := secretstore.Seal(profile.KeyRef(), []byte("provision")); err != nil {
		t.Fatalf("provision test encryption key: %v", err)
	}
	if _, err := profile.DowngradeKeyToFile(); err != nil {
		t.Fatalf("pin the test encryption key to a file: %v", err)
	}
	return home
}
