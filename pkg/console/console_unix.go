//go:build !windows

package console

// One device. /dev/tty is readable and writable through a single handle, so
// InPath and OutPath naming the same path is how Open is told to open it once —
// two descriptors on one terminal would work and buy nothing.
const (
	InPath  = "/dev/tty"
	OutPath = "/dev/tty"
)
