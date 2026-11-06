package profile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/config"
)

// TestProfilesDir_FollowsStateDir pins profiles to the product's state directory
// rather than a second, independent computation of it. Profiles hold plaintext
// credentials, so a stale path here would keep writing them where another
// product can read them.
func TestProfilesDir_FollowsStateDir(t *testing.T) {
	isolatedHome(t)
	config.SetStateDirName(".mexc-profiletest")
	t.Cleanup(func() { config.SetStateDirName(".mexc") })

	root, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	pd, err := profilesDir()
	if err != nil {
		t.Fatalf("profilesDir: %v", err)
	}
	if want := filepath.Join(root, "profiles"); pd != want {
		t.Fatalf("profilesDir = %q, want %q", pd, want)
	}
	if !strings.Contains(pd, ".mexc-profiletest") {
		t.Fatalf("profiles must follow the product state dir, got %q", pd)
	}
}
