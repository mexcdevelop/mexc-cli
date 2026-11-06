package secfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The build-tag wiring is the whole mechanism behind ModeBitsMeaningful, and a
// wrong tag is silent: the constant would simply hold the other platform's answer
// and every caller would quietly do the opposite of what it should. Asserting the
// mapping against runtime.GOOS is the only way that shows up as a failure.
//
// Written as a table so that adding a platform to the product forces a decision
// here rather than inheriting one.
func TestModeBitsMeaningful_MatchesThePlatform(t *testing.T) {
	want := map[string]bool{
		"darwin":  true,
		"linux":   true,
		"windows": false,
	}
	expected, known := want[runtime.GOOS]
	if !known {
		t.Skipf("no expectation recorded for GOOS=%s; add one when the product ships there", runtime.GOOS)
	}
	if ModeBitsMeaningful != expected {
		t.Errorf("ModeBitsMeaningful = %v on %s, want %v", ModeBitsMeaningful, runtime.GOOS, expected)
	}
}

// A mode this product treats as private must read back as private where the bits
// are real. This is the property the callers' gates depend on; it also documents
// that os.WriteFile's perm is what lands, with no umask surprise, for 0600.
func TestModeBitsMeaningful_PrivateModeReadsBackPrivate(t *testing.T) {
	if !ModeBitsMeaningful {
		t.Skip("this platform synthesises mode bits; there is nothing to read back")
	}
	path := filepath.Join(t.TempDir(), "k")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("a 0600 file reads back as %04o, which the product's own gate would refuse", got)
	}
}

// Lock's contract is exclusion, so the only way to see it is to have a second
// holder try. Both halves are asserted because they fail differently: a Lock that
// did nothing would let the second holder in immediately, and a release that did
// nothing would keep it out forever. A test that checked only one of those would
// pass against half a lock.
//
// TWO SEPARATE OPENS, deliberately. On Unix a flock is associated with the open
// file description, so locking twice through the SAME *os.File is a re-lock that
// succeeds and proves nothing. Windows would notice (LockFileEx is per-handle),
// which is exactly the direction a mistake here would be invisible in: it would
// look fine on the platform holding the only reachable caller.
//
// The shape mirrors what the product actually locks — an empty ".lock" file
// opened O_RDWR|O_CREATE, the file that exists only to be locked.
//
// The negative half is timing-based and therefore the weaker of the two: it says
// "still blocked after a while", not "blocked forever". The positive half is what
// pins the behaviour, so the window is generous rather than tight.
func TestLock_ExcludesASecondHolderUntilRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")

	first, err := OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open the first handle: %v", err)
	}
	defer func() { _ = first.Close() }()

	release, err := Lock(first)
	if err != nil {
		t.Fatalf("lock the first handle: %v", err)
	}

	// The second holder OWNS its handle and the test body must never close it.
	//
	// MEASURED, by stubbing out release: closing a handle that still has a blocking
	// LockFileEx pending on it does not return until that call does, so a deferred
	// Close here held the test past `go test -timeout` instead of letting the
	// 10-second branch below report the failure. A test whose job is to catch a
	// lock that never releases must not itself hang when it finds one.
	//
	// Leaving the goroutine blocked in that case is unavoidable — nothing cancels a
	// blocking lock — but a leaked goroutine does not stop the binary from exiting,
	// and a deferred close on that handle does.
	acquired := make(chan error, 1)
	go func() {
		h, oerr := OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
		if oerr != nil {
			acquired <- oerr
			return
		}
		rel, lerr := Lock(h)
		if lerr == nil {
			rel()
		}
		_ = h.Close()
		acquired <- lerr
	}()

	select {
	case lerr := <-acquired:
		// Release before failing, so the goroutine cannot be left blocked for the
		// rest of the run.
		release()
		t.Fatalf("the second holder acquired while the lock was held (err=%v): Lock does not exclude", lerr)
	case <-time.After(250 * time.Millisecond):
		// Still blocked, which is the state under test.
	}

	release()

	select {
	case lerr := <-acquired:
		if lerr != nil {
			t.Errorf("the second holder could not acquire after release: %v", lerr)
		}
	case <-time.After(10 * time.Second):
		t.Error("the second holder never acquired after release: release did not unlock")
	}
}

// OpenFile ORs a platform flag into whatever the caller passed, and on Windows
// that flag travels through syscall.Open's high-bit mask. If the plumbing were
// wrong the symptom would not be a followed link — it would be ordinary opens
// failing, or a flag the caller passed being dropped. So the baseline is pinned
// before any of the link behaviour, and O_TRUNC is exercised specifically because
// silently losing it would leave stale bytes behind a shorter credential.
func TestOpenFile_PlainFileRoundTripsAndHonoursTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")

	write := func(content string) {
		f, err := OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatalf("open %q for write: %v", path, err)
		}
		if _, err := f.WriteString(content); err != nil {
			_ = f.Close()
			t.Fatalf("write %q: %v", content, err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close after writing %q: %v", content, err)
		}
	}

	write("the-longer-first-content")
	write("short")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "short" {
		t.Errorf("file holds %q, want %q: O_TRUNC did not survive the added flag", got, "short")
	}
}

// The security property: a link planted AT the path must not be written through.
//
// The two platforms deliver it differently and this test says so rather than
// papering over it (see the doc on OpenFile). Unix refuses the open outright;
// Windows opens the reparse point itself, and the write is INTENDED not to reach
// the target. Asserting one shape on both would let the test pass for the wrong
// reason on one of them.
//
// The flags are O_WRONLY|O_CREATE|O_TRUNC on purpose. That is the atomic-replace
// shape editions/public/profile uses, and it is the exact case Go's own
// reparse-point handling does NOT cover: it adds the flag by itself only when
// O_CREATE and O_EXCL are both set. Written with O_EXCL this test would pass
// without the function under test contributing anything.
//
// The write's own error is logged rather than failed. What this test is entitled
// to assert is that the target did not change; whether the handle also refuses
// the write is Windows' business, and a refusal is the safe direction.
//
// NOT COVERED: a link at a PARENT component. OpenFile's guarantee is about the
// final component only, and a caller needing the whole path resolved safely has a
// different problem than this function solves.
func TestOpenFile_DoesNotWriteThroughALinkAtThePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	const original = "target-content"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		// A file symlink on Windows needs SeCreateSymbolicLinkPrivilege or Developer
		// Mode, and an ordinary developer box and an unelevated CI job both lack it.
		// Skipping keeps this from being permanently red there: a red test nobody can
		// act on is a red test nobody reads.
		//
		// The Windows half HAS been run, once, on Windows 11 with Developer Mode on,
		// and passed — with a control proving the same open without the flag does
		// overwrite the target. OpenFile's doc carries the numbers.
		//
		// What the skip costs is standing coverage, not the one-time answer: wherever
		// the privilege is absent this reports success without asserting anything, and
		// a SKIP does not turn a pipeline red. So a regression in the Windows path
		// would go unnoticed until someone re-runs this WITH the privilege. Turning
		// Developer Mode on for the Windows job is what would fix that; until then,
		// treat a green run here as silence rather than as evidence.
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create a symlink here, so the behaviour cannot be observed: %v", err)
		}
		t.Fatalf("create the symlink: %v", err)
	}

	f, err := OpenFile(link, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("Windows is expected to open the reparse point rather than refuse: %v", err)
		}
	} else if err == nil {
		_ = f.Close()
		t.Fatal("the open succeeded on a symlinked path: O_NOFOLLOW is not in effect")
	}

	if err == nil {
		if _, werr := f.WriteString("planted"); werr != nil {
			t.Logf("the reparse-point handle refused the write, which is the safe direction: %v", werr)
		}
		if cerr := f.Close(); cerr != nil {
			t.Logf("close: %v", cerr)
		}
	}

	got, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != original {
		t.Errorf("the write reached the link target: it holds %q, want %q", got, original)
	}
}
