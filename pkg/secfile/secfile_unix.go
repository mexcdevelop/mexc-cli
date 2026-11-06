//go:build !windows

package secfile

import (
	"os"
	"syscall"
)

// O_NOFOLLOW makes the open fail outright when the final path component is a
// symbolic link, which is the strongest of the two behaviours documented on
// OpenFile.
const noFollow = syscall.O_NOFOLLOW

// On a Unix filesystem the permission bits ARE the access control, so reading
// them answers the question the caller is asking.
const modeBitsMeaningful = true

func lock(f *os.File) (func(), error) {
	fd := int(f.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return nil, err
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }, nil
}
