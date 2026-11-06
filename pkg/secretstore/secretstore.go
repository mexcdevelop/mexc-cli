// Package secretstore encrypts a small blob at rest under a key the operating
// system holds, so that the ciphertext can be copied somewhere the key is not.
//
// # WHAT IT IS FOR
//
// A credential file leaving the machine by ordinary means: a dotfile repository,
// a synced folder, a directory pasted into a bug report. Those carry the file
// and not the OS keyring, so the ciphertext arrives useless.
//
// # WHAT IT IS NOT FOR
//
// Stated because every obvious reading is wrong in a different way:
//
//   - It is NOT a boundary against a process running as the same user. That
//     process asks the keyring for the key exactly as this package does.
//   - It does NOT cover a whole-disk backup or a machine migration. On macOS,
//     Time Machine backs up ~/Library/Keychains and Migration Assistant carries
//     the login keychain, so there both halves travel together. What this
//     protects is copying the DATA DIRECTORY, not copying the machine.
//   - It is not a substitute for the file permissions, nor for requiring a
//     terminal to enter a secret. Three controls, three scopes, no overlap.
//
// # KEY RESOLUTION
//
// Two places can hold the wrapping key — a local file (see DowngradeToFile) and
// the OS keyring — and BOTH are tried when opening. Reading through only whichever
// is in front would mean that any event changing that order silently orphans
// everything sealed under the other.
package secretstore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
)

const (
	keyLen   = 32 // AES-256
	nonceLen = 12 // AES-GCM standard nonce
	tagLen   = 16 // AES-GCM authentication tag
	fileMode = 0o600
	dirMode  = 0o700

	// keyringTimeout bounds a single keyring call. A working backend answers in
	// 20-40ms; an unreachable login keychain takes ~24s to say so. Generous rather
	// than tight because the usual cause of a slow-but-fine answer is a permission
	// dialog the user has not clicked yet.
	keyringTimeout = 5 * time.Second
)

// The three states a key store can be in. Collapsing any two is how a caller
// ends up minting a key while the real one is merely out of reach.
var (
	// errNotInitialized: reachable, holds no key. The ONLY state in which
	// generating one is correct.
	errNotInitialized = errors.New("no key stored yet")
	// errBlocked: exists but would not answer — a sandbox, a locked keychain, an
	// unanswered prompt, a timeout. It may well be holding the key.
	errBlocked = errors.New("the key store did not answer")
	// errCorrupted: a key is there and is unusable.
	errCorrupted = errors.New("the stored key is malformed")
	// errUnwrapBlocked is errBlocked for the LOCAL KEY FILE: the file is there and
	// the OS refused to unwrap it (see keywrap_windows.go). Still three states, not
	// four — errors.Is reports it as errBlocked, and it means what errBlocked means:
	// the key may well be sitting right there.
	//
	// It is distinguished at all because the REMEDY is different, and describe's
	// whole job is that the remedy fits: "unlock your keyring" is useless advice on
	// a platform this code does not ask a keyring.
	errUnwrapBlocked = fmt.Errorf("the local key file could not be unwrapped: %w", errBlocked)
	// errForeignKey is errCorrupted for a key file this machine cannot unwrap
	// because it was written somewhere else — another platform, another account.
	// Still errCorrupted to every caller; it exists only so the REMEDY can differ.
	//
	// The plain corrupted remedy is "remove the key file and save again", and that
	// is safe when the file is local damage. It is destructive when the state
	// directory is SHARED — a synced folder, a dotfiles repo, the very thing this
	// package exists to make safe to copy. Deleting there propagates back and
	// strands every credential on the machine that could still read them.
	errForeignKey = fmt.Errorf("the local key file was written elsewhere: %w", errCorrupted)
)

// ErrUndecryptable marks the ONE failure of Open that means the stored bytes are
// beyond help on this machine: a key was read, and the data does not open under it.
//
// It exists because a caller that owns the stored data has to decide whether to
// discard it, and no other part of the returned error answers that — every failure
// Open can return is the same type and subtype, differing only in prose, and
// matching on prose is the mistake this whole area already made once.
//
// ONE sentinel, not a pair, and its ABSENCE is the safe answer. Everything else Open
// can fail with — a keyring that will not answer, a malformed key, a key file with
// the wrong mode, no key stored yet — is a statement about reaching the KEY, not
// about the data, and in every one of those the data is very likely intact. So a
// caller writes:
//
//	if !errors.Is(err, secretstore.ErrUndecryptable) { /* keep the data */ }
//
// and a site that ever forgets to attach this errs toward keeping data rather than
// destroying it.
var ErrUndecryptable = errors.New("secretstore: the stored data opens under no key on this machine")

// Backend names where a wrapping key lives.
const (
	BackendKeychain = "keychain" // the OS keyring holds it: the intended state
	BackendFile     = "file"     // a local key file, protected only by its mode
	BackendNone     = "none"     // neither holds one yet
)

// Ref identifies one wrapping key. Namespace scopes a product's keys inside the
// OS keyring and MUST differ per product: two binaries sharing a namespace would
// read and overwrite each other's keys, the keyring twin of a shared state
// directory. Account names the key within that namespace.
type Ref struct {
	Namespace string
	Account   string
}

// keyFile is where a local key lives: inside the product's own state directory,
// the thing the product already owns and secures.
func (r Ref) keyFile() (string, error) {
	dir, err := config.StatePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, r.Account+".key"), nil
}

// withTimeout bounds a blocking keyring call. The goroutine is left to finish on
// its own — go-keyring shells out and offers no cancellation — so what this
// guarantees is that the CALLER stops waiting, not that the work stops. A write
// landing after its caller gave up is why keyring writes happen only under the
// creation lock; see withKeyLock.
func withTimeout[T any](call func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer func() { _ = recover() }()
		v, err := call()
		ch <- res{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(keyringTimeout):
		var zero T
		return zero, errBlocked
	}
}

func keyringRead(r Ref) ([]byte, error) {
	enc, err := withTimeout(func() (string, error) { return keyring.Get(r.Namespace, r.Account) })
	switch {
	case err == nil:
		key, derr := base64.StdEncoding.DecodeString(enc)
		if derr != nil || len(key) != keyLen {
			return nil, errCorrupted
		}
		return key, nil
	case errors.Is(err, keyring.ErrNotFound):
		return nil, errNotInitialized
	default:
		return nil, errBlocked
	}
}

// keyringUsable says whether this platform has an OS keyring worth asking. It
// starts at the platform's own answer — platformKeyringUsable, a constant set by
// keyring_darwin.go / keyring_other.go — and is a var only so a test can drive
// both branches on whatever machine it runs on.
//
// It is FALSE everywhere but macOS, and that is a decision, not a gap:
//
//   - On Linux go-keyring speaks to the freedesktop Secret Service over D-Bus.
//     The environments this product actually runs in — a server over ssh, a
//     container, an agent host, cron — have no session bus, so every install
//     would land on Seal's file fallback anyway. It would just pay for the trip
//     first: godbus autolaunches, so on a machine with dbus installed but no
//     session bus the first credential save execs `dbus-launch` and leaves an
//     orphan daemon behind, and a bus that answers while the Secret Service does
//     not costs keyringTimeout per call.
//   - Worse than the cost is the ambiguity. The three-state logic below exists
//     because macOS cannot tell "denying access" from "empty". Linux has no such
//     defect, so routing it through the keyring imports macOS's problem — and
//     with it every hint about locked keychains and sandboxes, which an agent
//     relays verbatim to a user who has neither.
//   - The state that actually bites: a key created in a desktop session lives in
//     the Secret Service with nothing on disk, so the same machine over ssh
//     cannot read what it just saved. A file key is reachable from both.
//
// Windows gets the same answer for a different reason: DPAPI is a wrap/unwrap
// primitive with no storage of its own, so it belongs around the key FILE rather
// than in front of it. That is a later step; today it lands on the file key too.
//
// The cost, stated plainly: on Linux the key file sits in the same directory as
// the ciphertext it protects, so copying that directory copies both. See the
// package doc — encryption at rest still defeats a stray `cat` or a pasted
// directory listing, but not a wholesale copy. macOS keeps the stronger property.
var keyringUsable = platformKeyringUsable

// keyringKey resolves the key the OS keyring holds, minting one only when
// allowCreate is set AND the keyring positively reports it has none.
//
// The distinction between "has none" and "will not say" is the entire point.
// Treating a refusal as an absence — which is what a bare `err != nil` does —
// mints a second key while the first is still there.
//
// The single gate for every caller: Backend, sealingKey, Open and
// DowngradeToFile all reach the keyring through here, and keyringRead is
// reachable only from here. Gating keyringRead instead would be wrong — it
// returns errNotInitialized, which is exactly the answer that sends this
// function on to CREATE a keyring entry.
func keyringKey(r Ref, allowCreate bool) ([]byte, error) {
	if !keyringUsable {
		return nil, errNotInitialized
	}
	key, err := keyringRead(r)
	if err == nil || !errors.Is(err, errNotInitialized) || !allowCreate {
		return key, err
	}
	return withKeyLock(r, func() ([]byte, error) {
		// Re-read inside the lock: whoever held it may have created the key, and a
		// second one would strand whatever they just sealed.
		if k, rerr := keyringRead(r); rerr == nil {
			return k, nil
		} else if !errors.Is(rerr, errNotInitialized) {
			return nil, rerr
		}
		fresh, gerr := randomKey()
		if gerr != nil {
			return nil, gerr
		}
		if _, serr := withTimeout(func() (struct{}, error) {
			return struct{}{}, keyring.Set(r.Namespace, r.Account, base64.StdEncoding.EncodeToString(fresh))
		}); serr != nil {
			return nil, errBlocked
		}
		return fresh, nil
	})
}

// fileKey resolves the key held in the local file, creating one only when
// allowCreate is set and none is there.
func fileKey(r Ref, allowCreate bool) ([]byte, error) {
	path, err := r.keyFile()
	if err != nil {
		return nil, err
	}
	switch key, rerr := readKeyFile(r, path); {
	case rerr == nil:
		return key, nil
	case errors.Is(rerr, errNotInitialized):
		// Absent; fall through.
	default:
		return nil, rerr
	}
	if !allowCreate {
		return nil, errNotInitialized
	}
	key, gerr := randomKey()
	if gerr != nil {
		return nil, gerr
	}
	// Sequenced deliberately. writeKeyFile adopts a concurrent creator's key by
	// writing it back through the pointer, and that is the whole point of the
	// pointer — a caller must never seal under a key that was discarded. Returning
	// `key, writeKeyFile(...)` in one statement left that to chance: Go orders the
	// function CALLS in a return statement, but when a plain variable operand is read
	// relative to them is unspecified, so the pre-adoption value could be returned.
	// It happens not to be, with the current compiler; the cost if that ever changes
	// is a blob sealed under a key that exists nowhere.
	if werr := writeKeyFile(r, path, key, &key); werr != nil {
		return nil, werr
	}
	return key, nil
}

// writeKeyFile creates the key file, or adopts the winner's key if another
// process created it first. out is overwritten with whatever key ends up
// authoritative, so the caller never seals under one that was discarded.
//
// r is needed for more than the path: it binds the platform wrap to this key (see
// wrapKey), so what lands on disk is not necessarily the 32 bytes handed in.
func writeKeyFile(r Ref, path string, key []byte, out *[]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return errs.Internal("cannot create the state directory: %v", err).WithCause(err)
	}
	// Wrapped before the file is created, not after: a failure here must leave no
	// file at all, or the next read would find an empty one and call it corrupt.
	stored, kerr := wrapKey(r, key)
	if kerr != nil {
		return kerr
	}
	// O_EXCL so a concurrent creator cannot be overwritten mid-flight; O_NOFOLLOW
	// so a planted symlink cannot redirect the key.
	f, oerr := secfile.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if oerr != nil {
		if errors.Is(oerr, os.ErrExist) {
			// Someone won the race; their key is authoritative. They may not have
			// finished writing it, so give the write a moment to land rather than
			// declaring corruption on a partial file.
			for i := 0; i < 3; i++ {
				if k, rerr := readKeyFile(r, path); rerr == nil {
					*out = k
					return nil
				}
				time.Sleep(5 * time.Millisecond)
			}
			return errCorrupted
		}
		return errs.Internal("cannot write the key file: %v", oerr).WithCause(oerr)
	}
	if _, werr := f.Write(stored); werr != nil {
		_ = f.Close()
		_ = os.Remove(path) // never leave a half-written key behind
		return errs.Internal("cannot write the key file: %v", werr).WithCause(werr)
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(path)
		return errs.Internal("cannot finalize the key file: %v", cerr).WithCause(cerr)
	}
	return nil
}

// readKeyFile reads the local key, refusing a path that is not a regular file or
// whose mode lets anyone else read it — the profile reader refuses both, and
// guarding the key less than the thing it protects is the wrong way round.
//
// The mode half is skipped where the OS synthesises mode bits, which keeps that
// symmetry: the profile reader skips it there too.
func readKeyFile(r Ref, path string) ([]byte, error) {
	info, serr := os.Lstat(path)
	if errors.Is(serr, os.ErrNotExist) {
		return nil, errNotInitialized
	}
	if serr != nil {
		return nil, errs.Internal("cannot stat the key file: %v", serr).WithCause(serr)
	}
	if !info.Mode().IsRegular() {
		return nil, errs.Validation(errs.SubtypeFailedPrecondition,
			"the encryption key path %s is not a regular file", path).
			WithHint("something replaced it, possibly with a symlink; remove it and save the credential again")
	}
	// Only where the bits are real. Where they are synthesised this mask matches
	// every writable file, so keeping it would refuse to read a key this very
	// process just wrote — see secfile.ModeBitsMeaningful.
	if secfile.ModeBitsMeaningful && info.Mode().Perm()&0o077 != 0 {
		return nil, errs.Validation(errs.SubtypeInsecurePermission,
			"the encryption key file %s has insecure permissions %04o", path, info.Mode().Perm()).
			WithHint("run: chmod 600 %s", path)
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, errs.Internal("cannot read the key file: %v", rerr).WithCause(rerr)
	}
	// Empty is LOCAL damage on every platform — no format anywhere is zero bytes —
	// and it is what a kill between this file's create and write leaves behind. Kept
	// ahead of the unwrap so both platforms classify it the same way: removing it is
	// the correct remedy, which is the opposite of the foreign-file case below.
	if len(b) == 0 {
		return nil, errCorrupted
	}
	// A file of exactly keyLen bytes, where this platform WRAPS, is the other kind
	// of platform's format — the one where the file simply is the key. A valid
	// wrapped blob is never that small: measured, wrapping a 32-byte key produces
	// 262 bytes, and DPAPI will not parse anything under 52 at all.
	//
	// So this is a key file that travelled here from a machine that does not wrap,
	// which is the most likely way a state directory arrives from elsewhere — a
	// synced folder, a dotfiles repository, a copied home directory.
	//
	// Classified HERE, before the unwrap, because the unwrap cannot classify it:
	// DPAPI answers ERROR_INVALID_PARAMETER for a blob this small, which maps to
	// "the OS would not answer, change nothing" — advice for a different situation
	// entirely, and it leaves the caller with no way forward. The post-unwrap
	// length check below was written for exactly this file and could never reach
	// it, because these bytes never survive the unwrap. See keywrap_windows.go for
	// the measurements.
	if keyFileWrapped && len(b) == keyLen {
		return nil, errForeignKey
	}
	// Unwrapped BEFORE the length check, because on a platform that wraps, the bytes
	// on disk are not key-sized and the check would call every valid file corrupt.
	key, uerr := keyUnwrap(r, b)
	if uerr != nil {
		return nil, uerr
	}
	if len(key) != keyLen {
		// Wrong length after unwrapping is what a key file from the OTHER kind of
		// platform looks like: one wraps and one stores raw, so the formats differ in
		// size. Local damage produces the same shape, and the two cannot be told
		// apart — so the wording covers the case whose wrong answer destroys data.
		return nil, errForeignKey
	}
	return key, nil
}

// keyUnwrap is the seam for the one property worth testing off Windows: that a
// refused unwrap does NOT lead to minting a replacement key. Nothing else in this
// package may reassign it.
var keyUnwrap = unwrapKey

func randomKey() ([]byte, error) {
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, errs.Internal("cannot generate an encryption key: %v", err).WithCause(err)
	}
	return key, nil
}

// KeyringAvailable reports whether this platform consults an OS keyring at all.
//
// It exists so a caller can say the right thing about BackendFile, which means
// two different situations. On macOS a file key means the keyring was bypassed —
// a real downgrade, and the weaker of two available options. Everywhere else it
// is the ONLY key store there is, and reporting it as a downgrade would be a
// permanent warning about a state the user cannot change and did not cause.
//
// Not exposed as a fourth Backend constant on purpose: where the key lives and
// what the platform offers are separate facts, and folding them together would
// make every existing `== BackendFile` comparison silently platform-dependent.
func KeyringAvailable() bool { return keyringUsable }

// KeyFileWrappedByOS reports whether the local key file is protected by an OS
// mechanism tied to the current user account, rather than being the key itself.
//
// It exists for one reason: the sentence "a backup or synced folder that copies
// the profile copies the key too" is the whole point of this package on a platform
// where the file is the key, and a falsehood on one where it is not. A caller that
// warns about co-location must ask this first.
//
// It does NOT mean the file is safe from this user. Same-user access is outside
// what this package defends against, wrap or no wrap — see the package doc.
func KeyFileWrappedByOS() bool { return keyFileWrapped }

// Backend reports where this namespace's key lives, WITHOUT creating one — so a
// diagnostic can describe the installation without changing it. It answers about
// what EXISTS: a keyring that will not answer reports BackendNone, because "the
// keyring is usable" would be a guess and this is meant to be a fact.
func Backend(r Ref) string {
	if _, err := fileKey(r, false); err == nil {
		return BackendFile
	}
	if _, err := keyringKey(r, false); err == nil {
		return BackendKeychain
	}
	return BackendNone
}

// Seal encrypts plain and reports which backend held the key, so the caller can
// tell the user when the protection is the weaker one.
//
// Order: the file key, then the keyring, then a NEW file key. That last step is
// an automatic downgrade, and it is deliberate. Refusing instead would make the
// product unusable wherever no keyring exists — a server over ssh, a container —
// and a control that every user of an entire platform must step around is not a
// control, it is an install step. The honesty moves to where it belongs: the
// caller learns which backend it got, and says so.
func Seal(r Ref, plain []byte) (blob []byte, backend string, err error) {
	key, backend, err := sealingKey(r)
	if err != nil {
		return nil, "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", errs.Internal("cannot generate nonce: %v", err).WithCause(err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), backend, nil
}

func sealingKey(r Ref) ([]byte, string, error) {
	key, err := fileKey(r, false)
	if err == nil {
		return key, BackendFile, nil
	}
	if !errors.Is(err, errNotInitialized) {
		return nil, "", describe(r, err)
	}
	key, kerr := keyringKey(r, true)
	if kerr == nil {
		return key, BackendKeychain, nil
	}
	if errors.Is(kerr, errCorrupted) {
		// A malformed keyring entry is not "no keyring": generating a file key
		// would hide a fixable problem behind a permanent downgrade.
		return nil, "", describe(r, kerr)
	}
	key, ferr := fileKey(r, true)
	if ferr != nil {
		return nil, "", describe(r, ferr)
	}
	return key, BackendFile, nil
}

// Open decrypts a blob, trying BOTH key locations — see the package doc: reading
// through only the front key is what turns any key change into silent data loss.
func Open(r Ref, blob []byte) ([]byte, error) {
	if len(blob) < nonceLen+tagLen {
		return nil, errs.Validation(errs.SubtypeFailedPrecondition,
			"stored data is truncated (%d bytes; a sealed value is at least %d)",
			len(blob), nonceLen+tagLen).
			WithHint("the file was cut short; save the credential again").
			WithCause(ErrUndecryptable)
	}
	var firstErr error
	// readAKey records that a key was obtained and tried, which is what separates
	// "the data matches nothing here" from "nothing here could be read" — the two
	// share the ending below, and only the first justifies a caller discarding data.
	readAKey := false
	for _, from := range []func() ([]byte, error){
		func() ([]byte, error) { return fileKey(r, false) },
		func() ([]byte, error) { return keyringKey(r, false) },
	} {
		key, err := from()
		if err != nil {
			if firstErr == nil || errors.Is(firstErr, errNotInitialized) {
				firstErr = err
			}
			continue
		}
		readAKey = true
		gcm, gerr := newGCM(key)
		if gerr != nil {
			return nil, gerr
		}
		if out, oerr := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil); oerr == nil {
			return out, nil
		}
	}
	// Every describe state is about reaching a key, never about the data, so none of
	// them carries ErrUndecryptable — see the note on it.
	if firstErr != nil && !errors.Is(firstErr, errNotInitialized) {
		return nil, describe(r, firstErr)
	}
	// The hint splits on whether this platform has a keyring at all. Telling someone
	// to unlock a keychain that does not exist on their OS is advice they cannot act
	// on, and an agent relays it verbatim and loops. Same reason nameTheEscapeHatch
	// guards on KeyringAvailable.
	hint := "no key has been stored here yet, or this data came from another machine. " +
		"Saving the credential again creates a key and is the fix if this machine is " +
		"where it belongs; it will NOT recover data written elsewhere."
	if keyringUsable {
		hint = "if the OS keyring exists here but cannot be reached — a sandbox, CI, a locked " +
			"keychain — the key is probably still in it and saving the credential again would NOT " +
			"recover this data: it seals a second key and leaves the first behind. Either reach " +
			"the keyring (unlock it, or run outside the sandbox), or pin the key to a local file " +
			"if this CLI offers a command for that. Only if the keyring answers and holds no key " +
			"was this written on another machine, and then saving it again is the fix."
	}
	e := errs.Validation(errs.SubtypeFailedPrecondition,
		"stored data cannot be decrypted with any key on this machine").
		WithHint("%s", hint)
	if readAKey {
		e = e.WithCause(ErrUndecryptable)
	}
	return nil, e
}

// describe turns an internal key-store state into the user-facing error. Three
// states, three sentences, on purpose: they call for different actions, and the
// one that must never be advised is "save it again" while the data is still
// recoverable.
//
// Every branch names the sandbox unconditionally. A keyring behind a sandbox cannot
// be told apart from an absent one by any read, so a condition here would be a
// guess; one extra sentence for a reader whose keyring is fine costs less than an
// agent looping on advice that cannot work.
func describe(r Ref, err error) error {
	var typed *errs.Error
	if errors.As(err, &typed) {
		return err // already user-facing (permissions, IO)
	}
	path, _ := r.keyFile()
	switch {
	case errors.Is(err, errUnwrapBlocked):
		// Both possible causes are named because a read cannot tell them apart, and
		// only one of them is fixable. Naming just the fixable one sends someone whose
		// key came from elsewhere into an endless environment hunt; naming just the
		// other invites deleting a key that would work fine after a normal logon.
		return errs.Validation(errs.SubtypeFailedPrecondition,
			"the operating system would not unwrap the local encryption key").
			WithHint("do NOT delete %s or save the credential again yet: the key is "+
				"probably intact and a new one would leave what is stored unreadable. This "+
				"usually means the account's own protection is unavailable — a service or "+
				"scheduled task running without a loaded user profile, or a different account "+
				"than the one that saved it. Run it as the account that saved the credential, "+
				"from an ordinary interactive session. If it keeps happening and this file was "+
				"copied here from another machine or user, then it cannot be unwrapped here at "+
				"all, and removing it and saving the credential again is the only way forward.",
				path)
	case errors.Is(err, errBlocked):
		return errs.Validation(errs.SubtypeFailedPrecondition,
			"the OS keyring did not answer, so stored credentials cannot be reached").
			WithHint("it is usually locked, or blocked by a sandbox or CI. Do NOT save the " +
				"credential again until it answers: the key is probably still there, and a new one " +
				"would leave this data unreadable here. Either unlock it and retry unchanged, or " +
				"pin the key to a local file so this context stops needing the keyring, if this " +
				"CLI offers a command for that.")
	case errors.Is(err, errForeignKey):
		// Deliberately does NOT lead with "delete it". The same bytes are a working
		// key on the machine that wrote them, and this directory may be synced back
		// there — so the destructive step is named last, and only for the case where
		// nothing else holds the data.
		return errs.Validation(errs.SubtypeFailedPrecondition,
			"the encryption key file here was written by a different platform or user account").
			WithHint("if %s is in a synced folder, a dotfiles repository, or was copied from "+
				"another machine, do NOT delete it: it is still the working key over there, and "+
				"deleting it here propagates back and makes those credentials unreadable. Use "+
				"this machine's own profiles instead, or keep the two state directories "+
				"separate. Only if this file has no other home is removing it and saving the "+
				"credential again the way forward.", path)
	case errors.Is(err, errCorrupted):
		return errs.Validation(errs.SubtypeFailedPrecondition,
			"the encryption key on this machine is malformed").
			WithHint("remove %s if present, then save the credential again", path)
	default:
		// errNotInitialized. On a platform with a keyring this is ambiguous — a
		// sandboxed macOS run reports itself exactly this way — and the ambiguity has
		// to be stated. Where no keyring is consulted there is no ambiguity, and
		// repeating the keyring sentence would send the reader after a component their
		// OS does not have.
		if !keyringUsable {
			return errs.Validation(errs.SubtypeFailedPrecondition,
				"no encryption key could be read on this machine").
				WithHint("nothing has been saved here yet: saving a credential creates the key.")
		}
		return errs.Validation(errs.SubtypeFailedPrecondition,
			"no encryption key could be read on this machine").
			WithHint("if nothing has been saved here yet, saving a credential creates the key. But " +
				"a keyring that cannot be reached — a sandbox, CI, a locked keychain — reports " +
				"itself exactly this way, and there the key already exists and saving again will " +
				"not reach it.")
	}
}

// DowngradeResult says what DowngradeToFile did.
type DowngradeResult int

const (
	// DowngradeAlreadyDone means a valid key file was already present.
	DowngradeAlreadyDone DowngradeResult = iota
	// DowngradeCopiedFromKeyring means the keyring's key was copied verbatim, so
	// everything already sealed stays readable.
	DowngradeCopiedFromKeyring
)

// DowngradeToFile pins the wrapping key into a local file so later reads take
// that path first. It is the deliberate way to say "keep the key here" — for a
// machine whose keyring an agent host cannot reach, or where another local
// process has to read the same store.
//
// It only ever READS the keyring: a downgrade must not create a keyring entry as
// a side effect. An existing entry is left in place and stays usable, because
// Open tries both keys.
//
// IT COPIES AND NEVER MINTS. On macOS a keyring that is denying access reports
// ErrNotFound, the same as an empty one, so "the keyring holds no key" is not a
// fact this code can establish — and minting on it strands the real key. No key to
// copy means refuse. Nothing is lost by refusing: where no keyring is usable, Seal
// falls back to a local key on its own, so saving a credential is what brings one
// into existence.
func DowngradeToFile(r Ref) (DowngradeResult, error) {
	if _, err := fileKey(r, false); err == nil {
		return DowngradeAlreadyDone, nil
	} else if !errors.Is(err, errNotInitialized) {
		return 0, describe(r, err)
	}

	key, kerr := keyringKey(r, false)
	if kerr != nil {
		if errors.Is(kerr, errNotInitialized) {
			return 0, errNothingToCopy()
		}
		return 0, describe(r, kerr)
	}

	path, err := r.keyFile()
	if err != nil {
		return 0, err
	}
	adopted := key
	if werr := writeKeyFile(r, path, key, &adopted); werr != nil {
		if errors.Is(werr, errCorrupted) {
			return 0, describe(r, werr)
		}
		return 0, werr
	}
	// writeKeyFile adopts a concurrent creator's key instead of overwriting it.
	// Where that happened this call moved nothing, and reporting "copied" would
	// claim a key movement that did not occur: the file holds the other writer's
	// key, which is the same state as finding one already there.
	if !bytes.Equal(adopted, key) {
		return DowngradeAlreadyDone, nil
	}
	return DowngradeCopiedFromKeyring, nil
}

// errNothingToCopy is DowngradeToFile's refusal. The wording keeps the ambiguity
// rather than hiding it: a keyring that will not answer looks exactly like an empty
// one, and naming only the empty case would send someone whose key is out of reach
// looking for a way to force past it.
func errNothingToCopy() error {
	return errs.Validation(errs.SubtypeFailedPrecondition,
		"the OS keyring holds no key to copy, so there is nothing to pin to a local file").
		WithHint("if nothing has been saved here yet, this is not the route to a local key: " +
			"saving a credential already keeps the key in a local file where no keyring is " +
			"usable. If you expected a key to be here, it is out of reach rather than absent — " +
			"a sandbox, CI, a locked keychain all report it this way — so run this from a " +
			"terminal where the keyring answers, and do NOT save the credential again first.")
}

// withKeyLock serializes key CREATION across processes.
//
// The window is narrow and the damage is not: two first-time saves racing each
// other both find no key, both generate, and both store — the loser's blob was
// sealed under a key that no longer exists. (Open's two-key fallback does not
// rescue that one: both keys lived in the same place.)
//
// Advisory (flock), which suffices — the only writers are this program. The lock
// file is separate from the key file so acquiring it never creates or truncates
// what it protects. Where flock is unavailable this proceeds unserialized rather
// than refusing to work.
func withKeyLock(r Ref, fn func() ([]byte, error)) ([]byte, error) {
	path, err := r.keyFile()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return nil, errs.Internal("cannot create the state directory: %v", err).WithCause(err)
	}
	lf, err := secfile.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return fn()
	}
	defer func() { _ = lf.Close() }()
	release, lerr := secfile.Lock(lf)
	if lerr != nil {
		return fn()
	}
	defer release()
	return fn()
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errs.Internal("cannot init cipher: %v", err).WithCause(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errs.Internal("cannot init GCM: %v", err).WithCause(err)
	}
	return gcm, nil
}

// The PLATFORM NOTE that used to sit here — "on Linux every install would land on
// the file fallback; revisit when a Linux artifact ships" — has been acted on. A
// Linux artifact ships, and the answer is keyringUsable: those platforms do not
// consult the keyring at all, rather than consulting it and always failing over.
// See that var for the reasoning, and BackendFile below for what callers report.
