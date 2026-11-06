// Package secfile holds the two file primitives whose SAFETY, not just their
// spelling, depends on the operating system.
//
// It exists because three places in this product write a file that must not be
// redirected by a planted link, and must not be written by two processes at once:
// the wrapping key and its creation lock (pkg/secretstore), and the credential
// ciphertext (editions/public/profile). Each used syscall.O_NOFOLLOW and
// syscall.Flock directly, which are Unix-only identifiers — so the same three-line
// pattern had to be ported three times, or once here.
//
// It lives under pkg/ rather than internal/ because an edition needs it, and an
// edition may not import internal/ — the invariant that keeps this module's
// machinery private and replaceable. .golangci.yml's depguard asserts it.
//
// What it does NOT do: it is not a general filesystem wrapper. Two functions, both
// about a specific attack or race, and one constant stating whether a file's mode
// bits mean anything here — which the same three places have to know before they
// judge a mode. Anything that os already spells portably stays on os.
package secfile

import (
	"os"
)

// ModeBitsMeaningful reports whether os.FileInfo.Mode()'s permission bits on this
// platform describe who may actually open the file.
//
// It exists because "is this file private?" has no portable answer. Where it is
// false, a caller must not read the bits AND must not report the result as
// though it had: the bits are synthesised, so testing them yields a verdict that
// was never about access. The per-platform files say how each one gets there.
//
// A caller that guards a check with this owes the user the distinction between
// "checked, and it is fine" and "not checked here" — silently passing claims an
// answer nobody computed. Only `doctor` reports to a human, so only doctor has to
// carry the second wording; the loaders simply stop refusing.
//
// What this does NOT say is that the file is unprotected where it is false.
// Windows protects it by ACL, which this package does not read. The honest
// summary is that the question moved somewhere Mode() cannot see.
const ModeBitsMeaningful = modeBitsMeaningful

// OpenFile is os.OpenFile plus one guarantee: a symbolic link — or, on Windows,
// any reparse point — AT path is not followed.
//
// The guarantee is worth stating carefully because the two platforms deliver it
// differently, and the difference reaches the caller.
//
// On Unix, O_NOFOLLOW makes the open FAIL when the final component is a link, so
// the caller aborts and nothing is written anywhere.
//
// On Windows, FILE_FLAG_OPEN_REPARSE_POINT makes the open return a handle to the
// link OBJECT rather than to its target — so the open SUCCEEDS and the caller
// carries on to write. The write does not reach the link's target. A caller that
// needs the Unix behaviour — refuse rather than proceed — has to check the handle
// itself, because a successful open and a successful write here mean neither that
// the path was ordinary nor that the bytes landed anywhere a reader will find.
//
// MEASURED on Windows 11 with Developer Mode on, against a symlink at the path,
// opened O_WRONLY|O_CREATE|O_TRUNC exactly as writeAtomic does:
//
//	with the flag:     open ok, write ok (7 bytes), target UNCHANGED
//	without the flag:  open ok, write ok,           target OVERWRITTEN
//
// The second line is the control, and it is the one that justifies this function
// existing: the attack is real on Windows, not theoretical. The first line is the
// guarantee. Both are reproduced by TestOpenFile_DoesNotWriteThroughALinkAtThePath,
// which needs the privilege to run and SKIPS without it — so this paragraph, not
// a green pipeline, is the record that it was ever exercised.
//
// The 7 bytes land on the link object itself: after the write the link is still a
// symlink and still resolves, Lstat reports its size as 7, and reading through it
// yields the target's original content. Not relied on, recorded because it is
// surprising and someone will otherwise rediscover it as a bug.
//
// So: same flag family, same purpose, two different failure modes, both now
// verified — Unix by a refused open, Windows by a write that goes nowhere.
//
// On Windows this is not redundant with what os already does. Go adds the reparse
// flag by itself only when O_CREATE and O_EXCL are BOTH set (syscall_windows.go,
// the CREATE_NEW branch); an O_CREATE|O_TRUNC open — which is exactly how a
// temp file for an atomic replace is opened — gets OPEN_ALWAYS with no such flag
// and follows the link. That asymmetry is invisible at the call site, which is
// the main reason this function exists rather than a per-caller flag.
//
// That paragraph was read out of the standard library before it was measured. It
// is now the control above: the same open without this function's flag put the
// planted bytes in the target.
//
// Callers should keep any Lstat pre-check they already have. This closes the
// window; the pre-check gives a better error message, and neither is a substitute
// for the other.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|noFollow, perm)
}

// Lock takes an exclusive lock on f, blocking until it is held, and returns the
// release. It is advisory on Unix and mandatory on Windows, which does not matter
// here: the only writers are this program, and nothing reads the locked file's
// contents — it exists to be locked.
//
// A caller that cannot get the lock should decide for itself whether to proceed
// unserialised or to fail — this returns the error and imposes neither.
func Lock(f *os.File) (release func(), err error) {
	return lock(f)
}
