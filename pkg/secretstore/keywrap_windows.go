//go:build windows

package secretstore

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// The key file is wrapped with DPAPI — CryptProtectData under the user's own
// logon secret — so the bytes on disk are useless to another account on the same
// machine and to any other machine.
//
// This is the confidentiality the mode-bit check used to provide and cannot here
// (see secfile.ModeBitsMeaningful): Go's mode bits say nothing about access, so
// the previous commit stopped consulting them. Rather than leave the key
// unprotected by anything this tree controls, the protection moves from "a mode
// nobody enforces" to "a wrap the OS enforces".
//
// WHAT IT DOES NOT CHANGE: not the threat model, only which OS mechanism carries
// it. A process running as this user can call CryptUnprotectData exactly as this
// code does, and the package doc has always said the same-user boundary is not
// one this package claims. What it does deliver is the property the package IS
// for: the state directory copied into a dotfile repo, a synced folder, or a bug
// report arrives with a key that unwraps nowhere.
//
// WHY NOT THE CREDENTIAL ITSELF: because then a DPAPI failure would be
// indistinguishable from "these bytes are not decryptable" — the one answer
// ErrUndecryptable exists to mark, and the one a caller is entitled to discard
// stored data on. Wrapping the KEY keeps the two apart: a refused unwrap is a
// statement about reaching the key, never about the data, so no failure here can
// authorize discarding a credential.

// keyFileWrapped: the bytes on disk are bound to this user account, so the key
// file is not a copy of the key — carrying it elsewhere carries nothing.
const keyFileWrapped = true

// keyEntropy binds a wrap to one Ref, so a key file cannot be renamed into
// another account's slot and silently unwrap. The NUL is a separator, not
// decoration: without it ("mexc"+"profile-key") and ("mexcprofile"+"-key") would
// produce identical entropy.
//
// It is not a secret and is not treated as one — it is compiled into a binary the
// same user can read. Domain separation is the whole claim.
func keyEntropy(r Ref) []byte {
	return []byte(r.Namespace + "\x00" + r.Account)
}

func wrapKey(r Ref, key []byte) ([]byte, error) {
	entropy := keyEntropy(r)
	var out windows.DataBlob
	// CRYPTPROTECT_UI_FORBIDDEN: never put up a dialog. A CLI may be running with
	// no interactive desktop at all (a service, a CI agent, an ssh session), where
	// a prompt does not fail — it hangs, holding the key-creation lock.
	err := windows.CryptProtectData(blobOf(key), nil, blobOf(entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		// No classification on the write path. Nothing is on disk yet, so there is no
		// data to be right or wrong about; the caller needs the reason, not a verdict.
		return nil, errs.Internal("the OS could not protect the encryption key: %v", err).WithCause(err)
	}
	return takeBlob(&out), nil
}

func unwrapKey(r Ref, blob []byte) ([]byte, error) {
	// An empty file never reaches DPAPI. blobOf maps empty to a nil DataBlob, which
	// is the right encoding for ABSENT ENTROPY and an illegal value for pDataIn —
	// so passing it through would ask the OS a malformed question and get back a
	// code this code would then read as "the OS would not answer", whose advice is
	// "change nothing". That wedges the install permanently on a file that is
	// simply truncated. A zero-length key file is local damage, and removing it is
	// the correct remedy, so it is classified here where that is still knowable.
	//
	// readKeyFile already refuses an empty file before calling here, so this is the
	// second of two guards. It stays because the hazard belongs to the DPAPI call
	// itself: anyone adding another caller must not be able to reintroduce a NULL
	// pDataIn by not knowing about the check upstream.
	if len(blob) == 0 {
		return nil, errCorrupted
	}
	entropy := keyEntropy(r)
	var out windows.DataBlob
	err := windows.CryptUnprotectData(blobOf(blob), nil, blobOf(entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err == nil {
		return takeBlob(&out), nil
	}
	// ERROR_INVALID_DATA is DPAPI's answer for a blob it cannot authenticate:
	// edited, or protected under a different user, machine or entropy. Unrecoverable
	// HERE — but "here" is the whole point. The same bytes are a working key on the
	// machine that wrote them, and a state directory reaching this one through a
	// synced folder or a dotfiles repository is the case this package is built for.
	//
	// So NOT plain errCorrupted, whose remedy is "remove it and save again". That
	// advice deletes the only copy of a key that still works somewhere else, and
	// the deletion syncs back. errForeignKey carries the same classification with a
	// remedy that names the shared-directory case first.
	if errors.Is(err, windows.ERROR_INVALID_DATA) {
		return nil, errForeignKey
	}
	// Everything else is treated as "the OS would not answer". This used to record
	// NTE_BAD_DATA as deliberately left out, on the grounds that it is plausibly a
	// corruption code but had not been confirmed to surface through GetLastError
	// here. MEASURED now, on Windows 11 build 26200, calling CryptUnprotectData
	// exactly as above:
	//
	//   valid blob, wrong entropy    ERROR_INVALID_DATA (13)
	//   valid blob, one byte flipped ERROR_INVALID_DATA (13)
	//   valid blob, truncated in half ERROR_INVALID_DATA (13)
	//   180 zero bytes               ERROR_INVALID_DATA (13)
	//   262 bytes of garbage         ERROR_INVALID_DATA (13)
	//   5 bytes                      ERROR_INVALID_PARAMETER (87)
	//   empty                        ERROR_INVALID_PARAMETER (87)
	//
	// NTE_BAD_DATA never appeared, so leaving it out cost nothing and the branch it
	// would have needed does not exist. Every blob big enough to be a DPAPI blob
	// and wrong in any way answers ERROR_INVALID_DATA, which is the case above.
	//
	// KNOWN INCONSISTENCY, left as it stands rather than guessed at. A file too
	// small to be a DPAPI blob answers ERROR_INVALID_PARAMETER and lands here, so a
	// truncated key file is told "the OS would not answer, change nothing" — while
	// a zero-length one is classified errCorrupted a few lines up, with the
	// opposite and more useful remedy, because both are equally local damage. No
	// valid key file is that small on any platform (32 bytes raw, 262 wrapped), so
	// the classification could be extended. It is not extended here because doing
	// so means encoding DPAPI's undocumented minimum blob size as a threshold, and
	// this comment's own rule applies: the blocked path costs a confusing message,
	// the corrupted path costs credentials, and guessing is only allowed to be
	// wrong the cheap way.
	return nil, errUnwrapBlocked
}

// blobOf points a DataBlob at b without copying it. Valid only while b is
// reachable, which is the duration of the syscall in both callers above.
//
// A nil DataBlob means "no entropy" to DPAPI, which is a different wrap than
// empty entropy — so an empty input has to produce nil rather than a zero-sized
// blob, or the two would disagree between protect and unprotect.
func blobOf(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return nil
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// takeBlob copies DPAPI's output into Go memory and releases the C allocation.
// CryptProtectData and CryptUnprotectData both allocate with LocalAlloc and
// document the caller as responsible for LocalFree; skipping it leaks the
// plaintext key into a buffer nothing will ever clear.
func takeBlob(b *windows.DataBlob) []byte {
	defer func() { _, _ = windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data))) }()
	out := make([]byte, b.Size)
	copy(out, unsafe.Slice(b.Data, b.Size))
	return out
}
