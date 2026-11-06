//go:build windows

package secfile

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// FILE_FLAG_OPEN_REPARSE_POINT opens the reparse point itself instead of
// following it. Passed through the high 12 bits of os.OpenFile's flag, which
// syscall.Open hands to CreateFile verbatim after checking them against the set
// of flags it supports — see the fileFlagsMask branch in syscall_windows.go.
//
// From the standard library, not golang.org/x/sys/windows, because the constant is
// all that is needed and syscall already has it.
const noFollow = syscall.FILE_FLAG_OPEN_REPARSE_POINT

// False because on Windows os.FileInfo.Mode() reports bits that were never on
// disk. Go derives them from a single attribute —
// FILE_ATTRIBUTE_READONLY — in fileStat.mode() (os/types_windows.go): 0666 when
// the attribute is clear, 0444 when it is set, plus ModeDir|0111 for a
// directory. Nothing in that derivation consults the ACL, which is what actually
// governs who may open the file.
//
// So `Perm()&0o077 != 0` is not a weaker test here, it is a different one that
// happens to have a constant answer: every writable file reads as 0666 and every
// directory as 0777, both of which trip the mask. A gate written for Unix
// therefore refuses every correctly-protected file on Windows.
const modeBitsMeaningful = false

// lock uses LockFileEx, the Windows counterpart of flock. Without
// LOCKFILE_FAIL_IMMEDIATELY it blocks until the range is available, matching
// LOCK_EX.
//
// One byte is locked rather than the whole file: the lock is a rendezvous, not a
// guard over content, and the file it is taken on exists only to be locked. A
// zero-length range would be a no-op, so the range has to be non-empty.
func lock(f *os.File) (func(), error) {
	handle := windows.Handle(f.Fd())
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped)); err != nil {
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, new(windows.Overlapped))
	}, nil
}
