package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
)

// TestStateDir_ProductScoped pins the property the whole split depends on: two
// products naming different directories never see each other's state. Before
// this, both wrote $HOME/.mexc, so one product's `env` (whose legal values are
// only the envs ITS hosts serve) made every command of the other fail host
// resolution.
func TestStateDir_ProductScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	orig := stateDirName
	t.Cleanup(func() { stateDirName = orig })

	stateDirName = ".mexc"
	a, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	SetStateDirName(".mexc-other")
	b, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if a == b {
		t.Fatalf("two products must not share a state dir, both got %q", a)
	}
	if !strings.HasPrefix(a, home) || !strings.HasPrefix(b, home) {
		t.Fatalf("state dirs must sit under $HOME: %q %q", a, b)
	}

	// 0700: profiles hold plaintext credentials, so no other local user may
	// traverse in — where the bits mean that. Where the OS synthesises them the
	// mode this asserts is unreachable: os.Mkdir passes no security attributes, the
	// directory inherits the parent ACL, and Mode() reports a constant 0777. See
	// stateDirPerm, which says the same thing at the site that sets it, and
	// secfile.ModeBitsMeaningful, which every reader of a mode consults.
	//
	// Guarded rather than skipped: the product-scoping this test is named for is
	// platform-independent and must keep its coverage everywhere.
	if secfile.ModeBitsMeaningful {
		for _, d := range []string{a, b} {
			fi, err := os.Stat(d)
			if err != nil {
				t.Fatalf("stat %s: %v", d, err)
			}
			if perm := fi.Mode().Perm(); perm != 0o700 {
				t.Errorf("%s has mode %04o, want 0700", d, perm)
			}
		}
	}

	// Everything derived follows the switch — a second, independent computation
	// of the root would silently keep writing to the old place.
	sp, err := settingsPath()
	if err != nil {
		t.Fatalf("settingsPath: %v", err)
	}
	if want := filepath.Join(b, "config"); sp != want {
		t.Errorf("settingsPath = %q, want %q", sp, want)
	}
}

// SetStateDirName ignores an empty name so a product with no reason to move can
// leave Edition.StateDir unset.
func TestSetStateDirName_EmptyKeepsDefault(t *testing.T) {
	orig := stateDirName
	t.Cleanup(func() { stateDirName = orig })
	SetStateDirName(".mexc-x")
	SetStateDirName("")
	if got := StateDirName(); got != ".mexc-x" {
		t.Fatalf("empty name must not reset the directory, got %q", got)
	}
}
