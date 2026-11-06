package console

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The build-tag wiring is the whole mechanism, and a wrong tag is silent: the
// constants would simply hold the other platform's answer. What matters is not
// the literal strings so much as WHETHER THEY DIFFER, because that is what makes
// Open take the one-handle or the two-handle path.
func TestPaths_MatchThePlatform(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		if InPath == OutPath {
			t.Errorf("the Windows console is two devices; one path means prompts go to the input buffer (%q)", InPath)
		}
	default:
		if InPath != OutPath {
			t.Errorf("/dev/tty is one device; two paths would open it twice (%q, %q)", InPath, OutPath)
		}
		if InPath != "/dev/tty" {
			t.Errorf("InPath = %q, want /dev/tty", InPath)
		}
	}
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

// The refusal every caller depends on: a path that opens fine but is not a
// terminal. This is what a redirected caller has, and it is the ONE branch of
// this package that a machine with no console can reach.
func TestOpen_RefusesAPathThatIsNotATerminal(t *testing.T) {
	p := regularFile(t, "not-a-tty")
	if _, err := Open(p, p); err == nil {
		t.Fatal("a regular file must not be accepted as a terminal")
	} else if !strings.Contains(err.Error(), p) {
		t.Errorf("the error must name the path, got %v", err)
	}
}

// The two-handle shape — what Windows takes — driven on a platform that has one.
// Two distinct paths make Open open twice and close both on refusal; running it
// here is the only way that arithmetic gets executed off Windows.
//
// It can only prove the INPUT side's check fires: reaching the output side's
// check needs a real terminal on the input side, which needs a pty this test
// suite does not have. Stated rather than left as an apparent gap.
func TestOpen_TwoDistinctPathsAreBothOpenedAndRefused(t *testing.T) {
	in := regularFile(t, "in")
	out := regularFile(t, "out")
	if _, err := Open(in, out); err == nil {
		t.Fatal("two regular files must not be accepted as a console")
	} else if !strings.Contains(err.Error(), in) {
		t.Errorf("the error must name the side that failed, got %v", err)
	}
}

// A missing path is a DIFFERENT answer from a path that is not a terminal, and
// callers turn both into the same refusal — but only because they see an error
// at all. Returning nil for either would let a caller prompt into nothing.
func TestOpen_MissingPathIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := Open(missing, missing); err == nil {
		t.Fatal("a path that does not exist must not open")
	}
	// And the second side missing must not leak the first.
	if _, err := Open(regularFile(t, "in"), missing); err == nil {
		t.Fatal("a missing output side must fail the whole open")
	}
}

// Close must not close a shared handle twice. Constructed directly because Open
// cannot hand back a live Pair without a terminal — and the double close is
// exactly the bug that only shows up on the platform where In == Out.
func TestClose_SharedHandleIsClosedOnce(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "shared"))
	if err != nil {
		t.Fatal(err)
	}
	if err := (Pair{In: f, Out: f}).Close(); err != nil {
		t.Errorf("closing a shared handle reported %v — the second close found it already closed", err)
	}
}

func TestClose_DistinctHandlesAreBothClosed(t *testing.T) {
	dir := t.TempDir()
	a, err := os.Create(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Create(filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := (Pair{In: a, Out: b}).Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Closing again must report both as already closed; if Out had been skipped,
	// this second close would succeed.
	if b.Close() == nil {
		t.Error("the output side was left open")
	}
}

// A caller defers Close before it knows whether Open succeeded, so the zero
// value has to be safe.
func TestClose_ZeroPairIsSafe(t *testing.T) {
	if err := (Pair{}).Close(); err != nil {
		t.Errorf("closing a zero Pair returned %v", err)
	}
}
