package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

// writeRaw drops a profile file straight onto disk at the given mode, bypassing
// Save (which would tighten the mode to 0600 — the very thing these tests need
// to violate).
func writeRaw(t *testing.T, name, mode string) string {
	t.Helper()
	pd, err := profilesDir()
	if err != nil {
		t.Fatalf("profilesDir: %v", err)
	}
	path := filepath.Join(pd, name)
	// Sealed, not plaintext: Load reads only the encrypted format, so a raw write
	// has to produce what Save would. Written directly anyway, because these tests
	// need modes and names that Save refuses to create.
	blob, _, serr := secretstore.Seal(keyRef, []byte("api-key=K\napi-secret=S\n"))
	if serr != nil {
		t.Fatalf("seal: %v", serr)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if mode == "wide" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
	}
	return path
}

func TestAudit_SplitsUsableFromRefused(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)

	writeRaw(t, "good", "")
	writeRaw(t, "leaky", "wide")
	// An invalid name never becomes a Profile, but it is very often someone's
	// credential file sitting in the directory under the wrong name.
	writeRaw(t, "Prod.bak", "")

	usable, rejected, err := Audit()
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if want := []string{"good"}; !reflect.DeepEqual(usable, want) {
		t.Fatalf("usable = %v, want %v", usable, want)
	}
	got := map[string]string{}
	for _, r := range rejected {
		got[r.Name] = r.Reason
	}
	if len(got) != 2 {
		t.Fatalf("rejected = %v, want exactly leaky + Prod.bak", rejected)
	}
	if !strings.Contains(got["leaky"], "insecure permissions") {
		t.Errorf("leaky reason must name the permission problem, got %q", got["leaky"])
	}
	if !strings.Contains(got["Prod.bak"], "not a valid profile name") {
		t.Errorf("Prod.bak reason must name the naming rule, got %q", got["Prod.bak"])
	}
}

// The rejection reason is shown verbatim by `doctor`, so it has to carry the
// remedy rather than merely state the diagnosis. This asserts the property the
// diagnostic depends on, not the exact sentence.
func TestAudit_PermissionReasonCarriesTheRemedy(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	path := writeRaw(t, "leaky", "wide")

	_, rejected, err := Audit()
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected = %v, want 1", rejected)
	}
	if !strings.Contains(rejected[0].Reason, "chmod 600") {
		t.Errorf("reason must tell the user how to fix it, got %q", rejected[0].Reason)
	}
	if !strings.Contains(rejected[0].Reason, path) {
		t.Errorf("reason must name the offending file, got %q", rejected[0].Reason)
	}
}

// List and Audit must agree on what "usable" means. They share one walk today;
// this pins the contract so a future split cannot silently reintroduce the drift
// where `list` advertises an entry `use` then refuses.
func TestList_MatchesAuditUsable(t *testing.T) {
	isolatedHome(t)

	writeRaw(t, "alpha", "")
	writeRaw(t, "beta", "")
	writeRaw(t, "leaky", "wide")

	listed, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	usable, rejected, err := Audit()
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !reflect.DeepEqual(listed, usable) {
		t.Fatalf("List = %v, Audit usable = %v — they must not diverge", listed, usable)
	}
	// The agreement above is the contract and holds on every platform. What follows
	// needs `leaky` to actually BE refused, which only happens where mode bits are
	// real — so it is guarded rather than skipping the whole test and losing the
	// contract's coverage there (see secfile.ModeBitsMeaningful).
	if !secfile.ModeBitsMeaningful {
		return
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(listed, want) {
		t.Fatalf("List = %v, want %v", listed, want)
	}
	// The refused entry is excluded from List but must still be reported, not lost.
	if len(rejected) != 1 || rejected[0].Name != "leaky" {
		t.Fatalf("rejected = %v, want just leaky", rejected)
	}
}

// A directory inside profiles/ is skipped silently, not reported as refused.
// This pins the pre-existing List() behaviour the Audit refactor promised to
// preserve: a directory is not a mislaid credential file, so naming it would be
// noise in doctor's report.
func TestAudit_DirectoryIsSkippedNotRefused(t *testing.T) {
	isolatedHome(t)
	writeRaw(t, "good", "")
	pd, err := profilesDir()
	if err != nil {
		t.Fatalf("profilesDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(pd, "somedir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	usable, rejected, err := Audit()
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if !reflect.DeepEqual(usable, []string{"good"}) {
		t.Fatalf("usable = %v, want [good]", usable)
	}
	if len(rejected) != 0 {
		t.Fatalf("a directory must be skipped, not refused: %v", rejected)
	}
}

func TestAudit_EmptyStoreIsNotAnError(t *testing.T) {
	isolatedHome(t)

	usable, rejected, err := Audit()
	if err != nil {
		t.Fatalf("Audit on an empty store: %v", err)
	}
	if len(usable) != 0 || len(rejected) != 0 {
		t.Fatalf("empty store must yield nothing, got usable=%v rejected=%v", usable, rejected)
	}
}
