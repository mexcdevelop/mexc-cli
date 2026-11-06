//go:build windows

package console

// Two devices. Windows exposes the console to CreateFile under two reserved
// names: CONIN$ is the input buffer, CONOUT$ the active screen buffer. Neither
// is bidirectional — a prompt written to CONIN$ does not appear — which is the
// whole reason Open takes a path for each side.
//
// These are the counterpart of /dev/tty, NOT of stdin and stdout. Both open even
// when the standard streams are redirected, and both fail when the process has
// no console at all: a service, a scheduled task, a detached process. That
// failure is the refusal the callers are looking for.
const (
	InPath  = "CONIN$"
	OutPath = "CONOUT$"
)

// ONE EDGE THAT TURNED OUT NOT TO EXIST. Kept as a record, because the reasoning
// that predicted it is sound and only one fact retires it.
//
// The prediction: os.OpenFile runs the name through fixLongPath
// (os/path_windows.go), which prepends \\?\ once the path is long enough — and
// for a RELATIVE name like these two, "long enough" is measured against the
// working directory, not the name. The device-path exemption in
// addExtendedPrefix is computed on the original relative name and only covers
// names beginning //./, so it does not apply here. A deep enough working
// directory would therefore rewrite these into something that is not a console
// device, and `profile set` would refuse in a nested directory and work one level
// up.
//
// MEASURED, and it does not happen. From a 253-character working directory
// (len(cwd)+1+len(name) = 260, well past the 248 threshold), os.OpenFile("CONIN$")
// and os.OpenFile("CONOUT$") both open normally. Windows 11 build 26200, Go
// 1.27.0, with HKLM\SYSTEM\CurrentControlSet\Control\FileSystem\LongPathsEnabled
// set to 0 — that is, with the registry opt-in OFF, which is the configuration
// most likely to show the fault.
//
// WHY: the Go runtime opts every Go binary into long-path awareness at startup.
// initLongPathSupport (runtime/os_windows.go) sets the undocumented
// IsLongPathAwareProcess bit in the PEB on Windows >= 10.0.15063 and sets
// canUseLongPaths, and fixLongPath returns the path untouched whenever that is
// true. addExtendedPrefix is therefore unreachable for any Go program on any
// Windows this product supports, at any path length, whatever the registry says.
//
// The prediction was otherwise correct, which is why it is worth keeping: with
// addExtendedPrefix forced to run, GetFullPathName("CONIN$") answers \\.\CONIN$
// — Windows resolves the reserved name to its device form — and the \\?\ prefix
// is then copied over the first four units, producing \\?\\\.\CONIN$. That does
// not open, and neither does a plain \\?\CONIN$; both were checked directly. So
// the failure mode is real and only the trigger is gone.
//
// Reproduce by chdir-ing into a >248-character directory and calling
// os.OpenFile("CONIN$", os.O_RDWR, 0) from a process attached to a console. It
// needs a console: with stdio redirected the open still succeeds, and it is
// term.IsTerminal in Open that does the refusing.
