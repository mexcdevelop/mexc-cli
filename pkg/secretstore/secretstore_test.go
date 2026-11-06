package secretstore

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
)

// The whole package test binary runs against go-keyring's in-memory mock.
//
// It has to. A test isolates state by overriding HOME, and on macOS that is
// exactly what makes the login keychain unreachable — so a real keyring would be
// both unavailable and slow to say so. The mock stands in for the OS, not for
// our logic: every branch below still runs the shipping code.
//
// Before this, every test downgraded to a key file first, which meant the
// keyring branch — the DEFAULT every real user is on — had no coverage at all.
//
// keyringUsable is forced on for the same reason. It is false on every platform
// but macOS (see secretstore.go), so leaving it alone would mean the keyring
// branch loses its coverage again the moment CI runs on Linux — the exact
// regression the paragraph above describes, arriving through the back door. The
// logic under test ships on macOS; the mock lets every platform exercise it.
//
// What that trades away is coverage of the platform's own answer, so the two
// tests at the bottom of this file assert it directly: TestKeyringUsable_*
// pins the build-tag wiring, and the disabled-path tests drive the branch every
// non-macOS install actually takes.
func TestMain(m *testing.M) {
	keyring.MockInit()
	keyringUsable = true
	os.Exit(m.Run())
}

// blockedKeyring simulates the state that matters most: the keyring is there and
// will not answer. Not "empty" — a different state with a different correct
// response, and conflating the two is the bug these tests guard.
func blockedKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(errors.New("keychain access blocked"))
	t.Cleanup(keyring.MockInit)
}

// unreachableKeyring is what macOS ACTUALLY produces when Keychain access is
// denied: ErrNotFound, byte-identical to an empty keyring. blockedKeyring cannot
// stand in for it — a generic error maps to errBlocked, so every refusal keyed on
// errBlocked passes there while failing on a real Mac. Any invariant that depends on
// telling "out of reach" from "empty" needs a case here too.
func unreachableKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInitWithError(keyring.ErrNotFound)
	t.Cleanup(keyring.MockInit)
}

func isolated(t *testing.T) Ref {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	return Ref{Namespace: "mexctest", Account: "test-key"}
}

func mustSeal(t *testing.T, r Ref, plain string) []byte {
	t.Helper()
	blob, _, err := Seal(r, []byte(plain))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return blob
}

// plantKey writes bytes into the key file in the ON-DISK format this platform's
// reader expects: wrapped where the platform wraps the file, raw where the file
// IS the key.
//
// It exists because os.WriteFile with a raw key is not a portable fixture — it
// writes the Unix format on every platform. Where the file is wrapped,
// readKeyFile refuses those bytes at the unwrap, before reaching the behaviour
// under test, so the test fails for the shape of its own fixture rather than for
// anything the product did. Four tests here did exactly that.
//
// Write the file directly ONLY where the subject IS a file no wrap could have
// produced — empty, or bytes deliberately malformed. Two callers below still do,
// and say so.
func plantKey(t *testing.T, r Ref, path string, key []byte) {
	t.Helper()
	stored, werr := wrapKey(r, key)
	if werr != nil {
		t.Fatalf("wrap the planted key: %v", werr)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, stored, fileMode); err != nil {
		t.Fatal(err)
	}
}

// requireSymlinks skips a test that has to CREATE a symlink to set itself up.
//
// On Windows that needs SeCreateSymbolicLinkPrivilege, which an ordinary account
// does not hold unless Developer Mode is on: os.Symlink returns "A required
// privilege is not held by the client". Without this the test reports a missing
// privilege as a failure of the code under test. Where the privilege IS held it
// still runs — this asks the OS rather than the GOOS, because the answer differs
// between two machines running the same build.
func requireSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("this account cannot create a symlink (%v), so the planted-symlink fixture cannot be built here", err)
	}
}

func TestSealOpen_RoundTripViaKeyring(t *testing.T) {
	r := isolated(t)
	plain := "api-key=abc\napi-secret=def\n"

	blob, backend, err := Seal(r, []byte(plain))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if backend != BackendKeychain {
		t.Fatalf("backend = %q, want %q — a working keyring must be preferred", backend, BackendKeychain)
	}
	if bytes.Contains(blob, []byte("api-secret")) || bytes.Contains(blob, []byte("def")) {
		t.Fatal("the sealed blob still contains the plaintext")
	}
	got, err := Open(r, blob)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != plain {
		t.Fatalf("round trip = %q, want %q", got, plain)
	}
	if b := Backend(r); b != BackendKeychain {
		t.Errorf("Backend = %q, want %q", b, BackendKeychain)
	}
}

// THE property the two-key read exists for: a key file appearing in front must
// not orphan what the keyring key sealed. A front-key-only read did exactly that.
func TestOpen_FallsBackToTheKeyringWhenTheFileKeyIsDifferent(t *testing.T) {
	r := isolated(t)
	blob := mustSeal(t, r, "sealed-under-the-keyring")

	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	other, _ := randomKey()
	plantKey(t, r, path, other)
	if b := Backend(r); b != BackendFile {
		t.Fatalf("Backend = %q, want %q", b, BackendFile)
	}

	got, oerr := Open(r, blob)
	if oerr != nil {
		t.Fatalf("a blob sealed under the keyring key must still open: %v", oerr)
	}
	if string(got) != "sealed-under-the-keyring" {
		t.Fatalf("got %q", got)
	}
}

func TestOpen_UsesTheFileKeyWhenItIsThere(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "seed") // a keyring key to copy; the downgrade never mints one
	if _, err := DowngradeToFile(r); err != nil {
		t.Fatal(err)
	}
	blob, backend, err := Seal(r, []byte("sealed-under-the-file"))
	if err != nil {
		t.Fatal(err)
	}
	if backend != BackendFile {
		t.Fatalf("backend = %q, want %q — a present key file must win", backend, BackendFile)
	}
	got, err := Open(r, blob)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(got) != "sealed-under-the-file" {
		t.Fatalf("got %q", got)
	}
}

// A blocked keyring must never be read as "empty". Minting a key here is what
// permanently orphaned data in the first version of this package.
func TestDowngrade_RefusesWhenTheKeyringWillNotAnswer(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "precious") // the key now lives in the keyring
	blockedKeyring(t)

	_, err := DowngradeToFile(r)
	if err == nil {
		t.Fatal("a blocked keyring must not be treated as an empty one")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T", err)
	}
	if !strings.Contains(e.Hint, "Do NOT") {
		t.Errorf("the hint must warn against re-saving while the data is recoverable, got %q", e.Hint)
	}
	path, _ := r.keyFile()
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("a refused downgrade must not leave a key file behind")
	}
}

func TestDowngrade_CopiesTheKeyringKeySoDataStaysReadable(t *testing.T) {
	r := isolated(t)
	blob := mustSeal(t, r, "precious")

	res, err := DowngradeToFile(r)
	if err != nil {
		t.Fatalf("DowngradeToFile: %v", err)
	}
	if res != DowngradeCopiedFromKeyring {
		t.Fatalf("result = %v, want DowngradeCopiedFromKeyring", res)
	}
	got, oerr := Open(r, blob)
	if oerr != nil || string(got) != "precious" {
		t.Fatalf("data sealed before the downgrade must stay readable: %q %v", got, oerr)
	}
}

// THE REGRESSION TEST. A key is in the keyring, which then stops answering in the
// one way macOS reports as absence. An invented key would take precedence in Open's
// order, so the data would become unreadable wherever the keyring is out of reach —
// which is the only context the downgrade exists for.
func TestDowngrade_RefusesWhenAnUnreachableKeyringLooksEmpty(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "precious") // the key now lives in the keyring
	unreachableKeyring(t)

	res, err := DowngradeToFile(r)
	if err == nil {
		t.Fatalf("a keyring reporting absence because it cannot answer must not be treated "+
			"as an empty one (got result %v)", res)
	}
	path, _ := r.keyFile()
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("a refused downgrade must not leave a key file behind")
	}
}

// Nothing sealed anywhere means no key to copy, and copying is all this does.
// Refusing costs nothing: Seal already falls back to a local key by itself where
// no keyring is usable, so saving a credential is what brings one into existence.
func TestDowngrade_RefusesWhenThereIsNothingToCopy(t *testing.T) {
	r := isolated(t)

	if res, err := DowngradeToFile(r); err == nil {
		t.Fatalf("with no key to copy the downgrade must refuse, got result %v", res)
	}
	if b := Backend(r); b != BackendNone {
		t.Fatalf("a refused downgrade must leave no key anywhere: Backend = %q, want %q", b, BackendNone)
	}
	path, _ := r.keyFile()
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("a refused downgrade must not leave a key file behind")
	}
}

// The refusal has to survive being read by an agent, which acts on the hint and
// not on the prose around it: it must not suggest re-saving (that seals under a
// second key) and it must name where the command can actually work.
func TestDowngrade_RefusalHintDoesNotSendYouToReSave(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "precious")
	unreachableKeyring(t)

	_, err := DowngradeToFile(r)
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T (%v)", err, err)
	}
	if !strings.Contains(e.Hint, "do NOT save the credential again") {
		t.Errorf("hint must warn against re-saving, got %q", e.Hint)
	}
	if !strings.Contains(e.Hint, "terminal where the keyring answers") {
		t.Errorf("hint must name where the keyring answers, got %q", e.Hint)
	}
}

func TestDowngrade_IsIdempotent(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "precious") // a keyring key for the first call to copy
	if _, err := DowngradeToFile(r); err != nil {
		t.Fatal(err)
	}
	res, err := DowngradeToFile(r)
	if err != nil {
		t.Fatalf("second downgrade: %v", err)
	}
	if res != DowngradeAlreadyDone {
		t.Fatalf("result = %v, want DowngradeAlreadyDone", res)
	}
}

// Seal must not refuse where no keyring exists: a control every user of a whole
// platform has to step around is an install step, not a control. It downgrades
// automatically AND reports which backend it used, so the caller can say so.
func TestSeal_FallsBackToAFileKeyWhenTheKeyringIsUnusable(t *testing.T) {
	r := isolated(t)
	blockedKeyring(t)

	blob, backend, err := Seal(r, []byte("still-works"))
	if err != nil {
		t.Fatalf("Seal must not refuse without a keyring: %v", err)
	}
	if backend != BackendFile {
		t.Fatalf("backend = %q, want %q so the caller can warn", backend, BackendFile)
	}
	got, oerr := Open(r, blob)
	if oerr != nil || string(got) != "still-works" {
		t.Fatalf("round trip on the fallback key: %q %v", got, oerr)
	}
}

// A malformed keyring entry is NOT "no keyring": downgrading past it would hide
// a fixable problem behind a permanent weaker state.
func TestSeal_RefusesACorruptKeyringEntry(t *testing.T) {
	r := isolated(t)
	if err := keyring.Set(r.Namespace, r.Account, "not-base64-of-32-bytes"); err != nil {
		t.Fatal(err)
	}
	_, _, err := Seal(r, []byte("x"))
	if err == nil {
		t.Fatal("a corrupt keyring entry must be reported, not silently bypassed")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error must name the real problem, got %v", err)
	}
	path, _ := r.keyFile()
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Error("it must not have downgraded around the corruption")
	}
}

func TestSeal_NonceIsFresh(t *testing.T) {
	r := isolated(t)
	if bytes.Equal(mustSeal(t, r, "same"), mustSeal(t, r, "same")) {
		t.Fatal("two seals of the same plaintext are identical — the nonce is not random")
	}
}

func TestOpen_RejectsTamperedBlob(t *testing.T) {
	r := isolated(t)
	blob := mustSeal(t, r, "api-secret=real\n")
	blob[len(blob)-1] ^= 0xff
	if _, err := Open(r, blob); err == nil {
		t.Fatal("a tampered blob must not decrypt")
	}
}

// Too short to be a sealed value at all. Reported as truncation, not key loss:
// the remedies differ, and "your key is gone" sends the user to re-issue a
// credential they still have.
func TestOpen_RejectsTruncatedBlob(t *testing.T) {
	r := isolated(t)
	for _, n := range []int{0, 1, nonceLen, nonceLen + tagLen - 1} {
		_, err := Open(r, make([]byte, n))
		if err == nil {
			t.Fatalf("%d bytes must be refused", n)
		}
		if !strings.Contains(err.Error(), "truncated") {
			t.Errorf("%d bytes: want a truncation error, got %v", n, err)
		}
	}
}

func TestOpen_NoKeyAnywhere(t *testing.T) {
	r := isolated(t)
	blob := mustSeal(t, r, "x")
	if err := keyring.Delete(r.Namespace, r.Account); err != nil {
		t.Fatal(err)
	}
	_, err := Open(r, blob)
	if err == nil {
		t.Fatal("with no key anywhere the blob must not open")
	}
	if !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Errorf("got %v", err)
	}
}

// A blocked keyring during a READ must say so, and must not advise re-saving —
// the data is still recoverable once the keyring answers.
func TestOpen_BlockedKeyringIsDistinctFromKeyLoss(t *testing.T) {
	r := isolated(t)
	blob := mustSeal(t, r, "x")
	blockedKeyring(t)

	_, err := Open(r, blob)
	if err == nil {
		t.Fatal("expected a failure")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %T", err)
	}
	if !strings.Contains(e.Message, "did not answer") {
		t.Errorf("message must name the blocked keyring, got %q", e.Message)
	}
	if !strings.Contains(e.Hint, "Do NOT") {
		t.Errorf("hint must warn against re-saving, got %q", e.Hint)
	}
}

func TestReadKeyFile_Rejections(t *testing.T) {
	r := isolated(t)
	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}

	// plantKey, so the LENGTH check is what this exercises. A raw short write is
	// not a short key on a wrapping platform, it is a blob that cannot be unwrapped
	// — a different rejection, reached earlier, whose class is errBlocked rather
	// than errCorrupted. The wrong-length branch would then never run anywhere.
	plantKey(t, r, path, []byte("short"))
	if _, rerr := readKeyFile(r, path); !errors.Is(rerr, errCorrupted) {
		t.Errorf("a wrong-length key must be errCorrupted, got %v", rerr)
	}

	// Wide mode — the key is more sensitive than the profile it protects, and the
	// profile reader already refuses this. Chmod explicitly: WriteFile applies its
	// perm only when creating, and the file above already exists.
	//
	// Only where the bits are real: readKeyFile skips this check where the OS
	// synthesises them, because there the mask matches every writable file (see
	// secfile.ModeBitsMeaningful). The rest of this test is platform-independent,
	// so the block is guarded rather than the whole function skipped.
	if secfile.ModeBitsMeaningful {
		if err := os.WriteFile(path, make([]byte, keyLen), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, rerr := readKeyFile(r, path); rerr == nil || !strings.Contains(rerr.Error(), "insecure permissions") {
			t.Errorf("a world-readable key must be refused, got %v", rerr)
		}
	}
}

// A symlink, which is how a planted key gets read. Split out of
// TestReadKeyFile_Rejections rather than guarded inside it: creating the link is
// a privilege an account either holds or does not (see requireSymlinks), and a
// skip in the middle of that test would have reported its passing assertions as
// skipped too.
func TestReadKeyFile_RejectsASymlinkedPath(t *testing.T) {
	requireSymlinks(t)
	r := isolated(t)
	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "planted")
	if err := os.WriteFile(target, make([]byte, keyLen), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	// Refused on the Lstat, before any unwrap, so the target's format does not
	// matter and this asserts the same thing on every platform.
	if _, rerr := readKeyFile(r, path); rerr == nil || !strings.Contains(rerr.Error(), "not a regular file") {
		t.Errorf("a symlinked key path must be refused, got %v", rerr)
	}
}

func TestDowngrade_WritesA0600KeyOfTheRightLength(t *testing.T) {
	r := isolated(t)
	mustSeal(t, r, "precious") // a keyring key to copy; the downgrade never mints one
	if _, err := DowngradeToFile(r); err != nil {
		t.Fatal(err)
	}
	path, _ := r.keyFile()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if secfile.ModeBitsMeaningful && info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %04o, want 0600", info.Mode().Perm())
	}
	// "The right length" is a property of the KEY, and only where the file is the
	// key is it also a property of the file. Where the platform wraps, the bytes on
	// disk are a blob a few hundred long, and asserting keyLen against them asserts
	// the other platform's format.
	//
	// So the invariant is read back through the reader instead, which holds
	// everywhere and is the thing callers depend on: what the downgrade left behind
	// opens as a key of exactly keyLen.
	key, rerr := readKeyFile(r, path)
	if rerr != nil {
		t.Fatalf("the downgraded key file must read back: %v", rerr)
	}
	if len(key) != keyLen {
		t.Errorf("key length = %d, want %d", len(key), keyLen)
	}
	if !keyFileWrapped && info.Size() != keyLen {
		t.Errorf("an unwrapped key file must be exactly the key: size = %d, want %d", info.Size(), keyLen)
	}
	if keyFileWrapped && info.Size() <= keyLen {
		t.Errorf("a wrapped key file must be larger than the key it holds: size = %d", info.Size())
	}
}

// An unusable keyring must not hang the caller: macOS takes ~24s per call to
// report failure when the login keychain is unreachable.
func TestKeyring_TimeoutBoundsAHangingBackend(t *testing.T) {
	t0 := time.Now()
	_, err := withTimeout(func() (string, error) {
		time.Sleep(2 * keyringTimeout)
		return "", nil
	})
	elapsed := time.Since(t0)

	if !errors.Is(err, errBlocked) {
		t.Fatalf("a hanging call must report errBlocked, got %v", err)
	}
	if elapsed >= 2*keyringTimeout {
		t.Fatalf("waited %v — the timeout is not bounding the call", elapsed)
	}
}

func TestWithKeyLock_SerializesCallers(t *testing.T) {
	r := isolated(t)

	// Counted with atomics rather than a plain variable: flock gives mutual
	// exclusion but no happens-before edge the Go race detector can see, so the
	// obvious unsynchronized counter reports a data race while the lock works.
	var inside, peak atomic.Int32
	const n = 32
	done := make(chan []byte, n)
	for i := 0; i < n; i++ {
		go func() {
			out, err := withKeyLock(r, func() ([]byte, error) {
				cur := inside.Add(1)
				for {
					old := peak.Load()
					if cur <= old || peak.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inside.Add(-1)
				return []byte{1}, nil
			})
			if err != nil {
				t.Errorf("withKeyLock: %v", err)
			}
			done <- out
		}()
	}
	for i := 0; i < n; i++ {
		if v := <-done; len(v) != 1 || v[0] != 1 {
			t.Fatalf("the inner result must be returned, got %v", v)
		}
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("%d callers were inside the lock at once, want 1", got)
	}
}

func TestWithKeyLock_UnlockableStillRuns(t *testing.T) {
	r := isolated(t)
	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}
	// A directory where the lock file belongs: no lockable handle, the shape of
	// any environment that will not lock.
	if err := os.Mkdir(path+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	ran := false
	if _, err := withKeyLock(r, func() ([]byte, error) { ran = true; return nil, nil }); err != nil {
		t.Fatalf("withKeyLock: %v", err)
	}
	if !ran {
		t.Fatal("the callback must run even when the lock cannot be acquired")
	}
}

// The one thing a caller that OWNS the stored data has to be able to decide: is this
// blob dead, or is a key merely out of reach? Both sides are asserted, because the
// value of the sentinel is entirely in its being absent on the second.
func TestErrUndecryptable_MarksOnlyDeadData(t *testing.T) {
	t.Run("a key was read and the data does not fit", func(t *testing.T) {
		r := isolated(t)
		blob := mustSeal(t, r, "precious")
		// Swap the keyring's key: one IS readable now, and it is the wrong one.
		other, _ := randomKey()
		if err := keyring.Set(r.Namespace, r.Account, base64.StdEncoding.EncodeToString(other)); err != nil {
			t.Fatal(err)
		}
		if _, oerr := Open(r, blob); !errors.Is(oerr, ErrUndecryptable) {
			t.Fatalf("a readable key that does not fit must mark the data dead, got %v", oerr)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		r := isolated(t)
		if _, err := Open(r, make([]byte, 4)); !errors.Is(err, ErrUndecryptable) {
			t.Fatal("a truncated blob is dead and must say so")
		}
	})

	t.Run("no key could be read", func(t *testing.T) {
		r := isolated(t)
		blob := mustSeal(t, r, "precious")
		if err := keyring.Delete(r.Namespace, r.Account); err != nil {
			t.Fatal(err)
		}
		_, err := Open(r, blob)
		if err == nil {
			t.Fatal("expected a failure")
		}
		if errors.Is(err, ErrUndecryptable) {
			t.Error("no key was readable, so nothing is known about the data: marking it dead " +
				"invites a caller to destroy something a reachable keyring would open")
		}
	})

	t.Run("the keyring will not answer", func(t *testing.T) {
		r := isolated(t)
		blob := mustSeal(t, r, "precious")
		blockedKeyring(t)
		_, err := Open(r, blob)
		if err == nil {
			t.Fatal("expected a failure")
		}
		if errors.Is(err, ErrUndecryptable) {
			t.Error("a keyring that would not answer says nothing about the data")
		}
	})

	t.Run("ErrNotFound, as denied Keychain access reports itself on macOS", func(t *testing.T) {
		r := isolated(t)
		blob := mustSeal(t, r, "precious")
		unreachableKeyring(t)
		_, err := Open(r, blob)
		if err == nil {
			t.Fatal("expected a failure")
		}
		if errors.Is(err, ErrUndecryptable) {
			t.Error("this is the state a sandbox produces; marking it dead is how stored data " +
				"gets destroyed on a machine whose key is right there")
		}
	})
}

// fileKey returns the key that is actually ON DISK, which is the loser's whole
// safeguard in a first-time creation race: writeKeyFile adopts the winner's key by
// writing it back through the pointer, and a caller that sealed under its own
// discarded key would produce a blob no key opens.
//
// WHAT THIS TEST CANNOT DO, stated so nobody trusts it further than it goes. The
// shape it guards against was `return key, writeKeyFile(path, key, &key)`, which
// leaves the read of `key` unordered against the call that mutates it — and the
// current compiler happens to read it AFTER, so this test passes against that
// version too (checked). It catches a writeKeyFile that stops writing back, or a
// fileKey that returns the pre-adoption value outright; it cannot catch a compiler
// changing its mind. That is why the sequencing in fileKey is explicit rather than
// left to a test.
func TestFileKey_ReturnsTheAdoptedKeyNotTheDiscardedOne(t *testing.T) {
	r := isolated(t)
	path, _ := r.keyFile()

	// The winner got there first.
	winner, _ := randomKey()
	adopted := winner
	if err := writeKeyFile(r, path, winner, &adopted); err != nil {
		t.Fatal(err)
	}

	// The loser now asks to create one. It must come back with the winner's key.
	got, err := fileKey(r, true)
	if err != nil {
		t.Fatalf("fileKey: %v", err)
	}
	if !bytes.Equal(got, winner) {
		t.Fatal("fileKey returned a key that is not the one on disk — anything sealed under it " +
			"would be unopenable by any key that exists")
	}
}

// withoutKeyring drives the branch every non-macOS install takes. It restores the
// suite-wide override (see TestMain) so one test cannot leak into the next.
func withoutKeyring(t *testing.T) {
	t.Helper()
	keyringUsable = false
	t.Cleanup(func() { keyringUsable = true })
}

// TestKeyringUsable_MatchesThePlatform pins the build-tag wiring in
// keyring_darwin.go / keyring_other.go. Without it a mistake there — a wrong tag,
// a file that stops being compiled — is invisible: TestMain forces the flag on,
// so every other test in this file would keep passing while the shipped binary
// took the opposite branch.
//
// Reads the CONSTANT, not the var: TestMain overrides the var, so asserting on it
// would only prove the override happened.
func TestKeyringUsable_MatchesThePlatform(t *testing.T) {
	want := runtime.GOOS == "darwin"
	if platformKeyringUsable != want {
		t.Fatalf("platformKeyringUsable is %v on %s, want %v — check the build tags in keyring_darwin.go / keyring_other.go",
			platformKeyringUsable, runtime.GOOS, want)
	}
}

// With no keyring, Seal must still work: it falls through to a local key file and
// reports the weaker backend rather than failing. This is the DEFAULT on Linux and
// Windows, so a regression here breaks those platforms entirely while every
// keyring test above stays green.
func TestNoKeyring_SealUsesAFileKeyAndSaysSo(t *testing.T) {
	r := isolated(t)
	withoutKeyring(t)

	blob, backend, err := Seal(r, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if backend != BackendFile {
		t.Fatalf("backend = %q, want %q", backend, BackendFile)
	}
	if got := Backend(r); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
	out, err := Open(r, blob)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(out) != "secret" {
		t.Fatalf("Open returned %q", out)
	}
}

// The keyring must not be touched at all — not read, not written. A keyring that
// errors on every call proves it: if any code path still reached it, the generic
// error would map to errBlocked and surface instead of the file fallback.
func TestNoKeyring_NeverAsksTheKeyring(t *testing.T) {
	r := isolated(t)
	withoutKeyring(t)
	blockedKeyring(t)

	if _, _, err := Seal(r, []byte("secret")); err != nil {
		t.Fatalf("Seal must not consult the keyring, got: %v", err)
	}
	// A blocked keyring reports errBlocked, which Backend turns into BackendNone.
	// Seeing BackendFile is the evidence that the keyring was skipped.
	if got := Backend(r); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q — the keyring was consulted", got, BackendFile)
	}
}

// Open must not attach ErrUndecryptable when there is no key to be had. The
// sentinel authorises a caller to DESTROY stored data (the oss session store acts
// on it), so "no keyring and no key file yet" has to stay a plain failure.
func TestNoKeyring_MissingKeyIsNotUndecryptable(t *testing.T) {
	r := isolated(t)
	withoutKeyring(t)

	// A well-formed blob no key on this machine can open.
	sealed := make([]byte, nonceLen+tagLen+8)
	_, err := Open(r, sealed)
	if err == nil {
		t.Fatal("Open must fail when no key exists")
	}
	if errors.Is(err, ErrUndecryptable) {
		t.Fatal("no key was ever read, so the data is not proven dead — ErrUndecryptable must not be attached")
	}
}

// DowngradeToFile is macOS's escape hatch: it copies the keyring's key into a
// file. With no keyring there is nothing to copy, and it must refuse rather than
// mint — minting is what strands a real key.
func TestNoKeyring_DowngradeRefusesInsteadOfMinting(t *testing.T) {
	r := isolated(t)
	withoutKeyring(t)

	if _, err := DowngradeToFile(r); err == nil {
		t.Fatal("DowngradeToFile must refuse when no keyring can be read")
	}
	path, perr := r.keyFile()
	if perr != nil {
		t.Fatalf("keyFile: %v", perr)
	}
	if _, serr := os.Stat(path); serr == nil {
		t.Fatal("DowngradeToFile minted a key file; it must only ever copy one")
	}
}

// blockedUnwrap makes the platform key-wrap refuse the way DPAPI does when the
// account's own protection is unavailable — a service with no loaded user
// profile, or a different account than the one that saved the key.
//
// This is the state that decides whether the port is safe, and it is
// unreachable off Windows without a seam: the identity wrap cannot fail. The
// tests below are the reason keyUnwrap is a var.
func blockedUnwrap(t *testing.T) {
	t.Helper()
	keyUnwrap = func(Ref, []byte) ([]byte, error) { return nil, errUnwrapBlocked }
	t.Cleanup(func() { keyUnwrap = unwrapKey })
}

// fileKeyOnly puts the wrapping key in a local file and nowhere else, which is
// the only arrangement in which the wrap matters at all.
func fileKeyOnly(t *testing.T) (Ref, string) {
	t.Helper()
	r := isolated(t)
	withoutKeyring(t)
	if _, backend, err := Seal(r, []byte("provision")); err != nil {
		t.Fatalf("Seal: %v", err)
	} else if backend != BackendFile {
		t.Fatalf("backend = %q, want %q — the rest of this test would prove nothing", backend, BackendFile)
	}
	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	return r, path
}

// The three-state model in this file says a key store is not-initialized,
// blocked, or corrupted. A refused unwrap is the middle one — the key is very
// likely right there — and this pins that it reports as such rather than becoming
// a fourth state nothing else knows how to treat.
func TestUnwrapBlocked_IsAKindOfBlocked(t *testing.T) {
	if !errors.Is(errUnwrapBlocked, errBlocked) {
		t.Error("a refused unwrap must report as blocked, or callers keyed on errBlocked stop seeing it")
	}
	if errors.Is(errUnwrapBlocked, errNotInitialized) {
		t.Error("reporting as not-initialized is what would authorise minting a second key")
	}
	if errors.Is(errUnwrapBlocked, errCorrupted) {
		t.Error("reporting as corrupted is what would advise deleting a key that is probably fine")
	}
}

// The contract both platform implementations owe, asserted in the form that makes
// it worth running on Windows: the round trip must hold, and where the wrap is not
// the identity, the bytes on disk must be bound to the Ref that wrote them.
func TestKeyWrap_RoundTripsAndIsBoundToItsRef(t *testing.T) {
	r := Ref{Namespace: "ns", Account: "acct"}
	key, err := randomKey()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := wrapKey(r, key)
	if err != nil {
		t.Fatalf("wrapKey: %v", err)
	}
	got, err := unwrapKey(r, stored)
	if err != nil {
		t.Fatalf("unwrapKey: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Fatal("the key did not survive a wrap/unwrap round trip — everything sealed under it is lost")
	}
	if bytes.Equal(stored, key) {
		// The identity wrap. Nothing is bound, and the platform file says why.
		return
	}
	// Same account name, different product namespace: a key file moved between the
	// two must not open. The NUL separator in keyEntropy is what makes the pair
	// ("mexc","profile-key") distinguishable from ("mexcprofile","-key").
	if _, err := unwrapKey(Ref{Namespace: "ns2", Account: "acct"}, stored); err == nil {
		t.Error("a key wrapped for one Ref unwrapped under another — the wrap is not bound")
	}
}

// THE property this whole design turns on. When the OS will not unwrap the key
// file, the key is probably intact and merely out of reach, so generating a
// replacement would strand every credential already sealed. It must refuse.
func TestFileKey_ARefusedUnwrapNeverMintsAReplacement(t *testing.T) {
	r, path := fileKeyOnly(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// allowCreate=true is the whole point: the caller is ASKING for a key to be
	// minted if none exists, and the answer must still be no, because one does.
	blockedUnwrap(t)
	_, ferr := fileKey(r, true)
	if ferr == nil {
		t.Fatal("fileKey minted a key while the existing one was merely unreadable")
	}
	// The classification matters as much as the refusal, because it picks the advice.
	// O_EXCL means even a misclassified refusal leaves the FILE alone — but routed
	// through errCorrupted the user is told to remove the key and save again, which
	// finishes the job by hand.
	if errors.Is(ferr, errCorrupted) {
		t.Errorf("reported as corrupted: the remedy for that is deleting the key file (%v)", ferr)
	}
	if !errors.Is(ferr, errBlocked) {
		t.Errorf("must report as blocked so the advice is 'change nothing yet', got %v", ferr)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the key file is gone: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the key file was rewritten while the OS could not read it — anything sealed " +
			"under the old key is now unopenable")
	}
}

// Seal takes the same refusal one level up: it must not quietly seal the next
// credential under a second key, which would leave a store whose entries need two
// different keys and one of them lost.
func TestSeal_RefusesWhileTheKeyFileCannotBeUnwrapped(t *testing.T) {
	r, path := fileKeyOnly(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	blockedUnwrap(t)
	if _, _, err := Seal(r, []byte("secret")); err == nil {
		t.Fatal("Seal succeeded while the key file could not be unwrapped")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a failed Seal replaced the key file")
	}
}

// The reason the wrap covers the KEY and not the credential. A caller deletes its
// stored data when Open reports ErrUndecryptable — that is what the sentinel is
// for — so a refused unwrap reaching it would turn an unavailable account
// protection into deleted credentials. Open must say "I could not reach a key",
// never "these bytes are beyond help".
func TestOpen_ARefusedUnwrapIsNotUndecryptable(t *testing.T) {
	r, _ := fileKeyOnly(t)
	blob, _, err := Seal(r, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	blockedUnwrap(t)
	_, oerr := Open(r, blob)
	if oerr == nil {
		t.Fatal("Open succeeded with no readable key")
	}
	if errors.Is(oerr, ErrUndecryptable) {
		t.Fatalf("a refused unwrap must never be reported as undecryptable data — a caller "+
			"deletes its store on this: %v", oerr)
	}
}

// The remedy has to fit the cause, and here the wrong remedy destroys
// credentials. Asserted as properties rather than a sentence: the warning must be
// present, the path must be named so the user knows what NOT to touch, and the
// macOS advice must not leak into a platform with no keyring to unlock.
func TestDescribe_UnwrapBlockedWarnsAgainstDeletingTheKey(t *testing.T) {
	r := isolated(t)
	path, _ := r.keyFile()

	var e *errs.Error
	if !errors.As(describe(r, errUnwrapBlocked), &e) {
		t.Fatal("describe must return a user-facing error")
	}
	if !strings.Contains(e.Hint, "do NOT delete") {
		t.Errorf("hint must warn against deleting the key, got %q", e.Hint)
	}
	if !strings.Contains(e.Hint, path) {
		t.Errorf("hint must name the file the user must not touch, got %q", e.Hint)
	}
	if strings.Contains(e.Message, "keyring") || strings.Contains(e.Hint, "unlock") {
		t.Errorf("this state has no keyring to unlock; that advice cannot be acted on: %q / %q",
			e.Message, e.Hint)
	}
}

// A key file carried in from another platform, and the exact boundary of what
// that is safe from.
//
// TWO things have to hold, and the first version of this test only checked one.
// It asserted that ErrUndecryptable is not attached — which stops the AUTOMATIC
// deletion — and concluded the risk was closed. It was not: the error a human
// reads still said "the encryption key on this machine is malformed / remove it
// and save the credential again". In a synced folder that advice deletes the
// working key on the machine it came from, and the deletion syncs back. The
// machine-facing half was safe and the human-facing half was the loss.
//
// The safety of the first half rests on ONE property, worth naming because it is
// not obvious and not permanent: the two platforms' key files differ in LENGTH.
// One wraps the key, the other stores it raw, so a foreign file is not key-sized
// and no key is obtained at all. ErrUndecryptable is reachable only when a key WAS
// read.
func TestForeignKeyFile_IsNeitherDeletedNorAdvisedAway(t *testing.T) {
	r, path := fileKeyOnly(t)
	blob, _, err := Seal(r, []byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Shaped like the other platform's file: a DPAPI blob is a few hundred bytes,
	// never the raw key length.
	//
	// Written directly, not through plantKey, BECAUSE the subject is a file no wrap
	// on this platform could have produced. Where the platform wraps, these bytes
	// are a blob DPAPI will not authenticate; where it does not, they are a key of
	// the wrong length. Both are the foreign-file case, and both must reach the
	// remedy asserted below.
	if err := os.WriteFile(path, make([]byte, 180), fileMode); err != nil {
		t.Fatal(err)
	}
	_, oerr := Open(r, blob)
	if oerr == nil {
		t.Fatal("a key file of the wrong shape must not open anything")
	}
	if errors.Is(oerr, ErrUndecryptable) {
		t.Fatalf("reported as dead data — a caller deletes its store on this: %v", oerr)
	}

	// The half that was missing. The remedy a person is given must not be "delete
	// it", because the file may be a working key somewhere this directory syncs to.
	var e *errs.Error
	if !errors.As(oerr, &e) {
		t.Fatalf("want a structured error, got %T", oerr)
	}
	if !strings.Contains(e.Hint, "do NOT delete") {
		t.Errorf("the remedy must warn against deleting a key that works elsewhere, got %q", e.Hint)
	}
	if !strings.Contains(e.Hint, "synced folder") {
		t.Errorf("the remedy must name the shared-directory case, got %q", e.Hint)
	}
	if !strings.Contains(e.Hint, path) {
		t.Errorf("the remedy must name the file, got %q", e.Hint)
	}

	// The boundary. A DIFFERENT key of the right length is read, tried, and fails —
	// and that legitimately means "this data opens under no key here". Asserted so
	// the protection above is not mistaken for a general guarantee about carrying a
	// state directory between platforms. If a future change makes the two formats
	// the same length, THIS is what still passes while the product has quietly
	// become destructive.
	other, err := randomKey()
	if err != nil {
		t.Fatal(err)
	}
	// plantKey, not a direct write: this case is the one that must be a VALID key
	// file holding the WRONG key, which on a wrapping platform means wrapped. A raw
	// write here failed at the unwrap instead, reporting the foreign-file remedy —
	// and so passed the assertion above twice over while never reaching this one.
	plantKey(t, r, path, other)
	if _, oerr := Open(r, blob); !errors.Is(oerr, ErrUndecryptable) {
		t.Fatalf("a wrong key of the right length is genuinely dead data, got %v", oerr)
	}
}

// errForeignKey must stay a KIND of errCorrupted, not a fourth state: it changes
// the wording, never the classification every other caller branches on.
func TestForeignKey_IsAKindOfCorrupted(t *testing.T) {
	if !errors.Is(errForeignKey, errCorrupted) {
		t.Error("callers keyed on errCorrupted must still see it")
	}
	if errors.Is(errForeignKey, errNotInitialized) {
		t.Error("reporting as not-initialized would authorise minting a second key")
	}
	if errors.Is(errForeignKey, errBlocked) {
		t.Error("blocked means the key may be fine here; a foreign key is not fine here")
	}
}

// A truncated key file is LOCAL damage, and there "remove it and save again" is
// the correct remedy — the opposite call from the foreign case. Zero length is the
// state a kill between writeKeyFile's create and write leaves behind.
func TestEmptyKeyFile_IsLocalDamageWithARecoverableRemedy(t *testing.T) {
	r, path := fileKeyOnly(t)
	if err := os.WriteFile(path, nil, fileMode); err != nil {
		t.Fatal(err)
	}

	_, ferr := fileKey(r, false)
	if !errors.Is(ferr, errCorrupted) {
		t.Fatalf("an empty key file must be corrupted, got %v", ferr)
	}
	if errors.Is(ferr, errBlocked) {
		t.Fatal("blocked would tell the user to change nothing, wedging a truncated file forever")
	}
	var e *errs.Error
	if !errors.As(describe(r, ferr), &e) {
		t.Fatal("describe must return a user-facing error")
	}
	if !strings.Contains(e.Hint, "remove "+path) {
		t.Errorf("local damage must be recoverable by removing the file, got %q", e.Hint)
	}
}

// A key file that travelled here from a platform which does NOT wrap is the most
// likely way a foreign key arrives: 32 raw bytes, the format where the file simply
// is the key. It is also the file the post-unwrap length check was written for and
// could never reach — these bytes never survive the unwrap, so before this the case
// landed on "the OS would not answer, change nothing", which is advice for a
// different situation and leaves no way forward.
//
// Wrapping platforms only, by nature: where the file IS the key, 32 raw bytes is
// this platform's own valid format, not a foreign one.
func TestReadKeyFile_AnUnwrappedKeyFileIsForeignNotBlocked(t *testing.T) {
	if !keyFileWrapped {
		t.Skip("this platform stores the key unwrapped, so keyLen raw bytes is its own valid format")
	}
	r := isolated(t)
	path, err := r.keyFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		t.Fatal(err)
	}
	// Written raw ON PURPOSE, not through plantKey: the other platform's format is
	// precisely what plantKey cannot produce, because plantKey wraps.
	unwrapped, err := randomKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, unwrapped, fileMode); err != nil {
		t.Fatal(err)
	}

	_, rerr := readKeyFile(r, path)
	if !errors.Is(rerr, errForeignKey) {
		t.Fatalf("a keyLen unwrapped key file must be errForeignKey, got %v", rerr)
	}
	// It must NOT be classified as the store refusing to answer, which is what the
	// unwrap alone reports for a blob this small.
	if errors.Is(rerr, errBlocked) {
		t.Error("classified as blocked: that advice is 'change nothing', which wedges this file forever")
	}
	// And the remedy a person is given must be the non-destructive one: the file is
	// still the working key on the machine that wrote it.
	var e *errs.Error
	if !errors.As(describe(r, rerr), &e) {
		t.Fatal("describe must return a user-facing error")
	}
	if !strings.Contains(e.Hint, "do NOT delete") {
		t.Errorf("the remedy must warn against deleting a key that works elsewhere, got %q", e.Hint)
	}
}
