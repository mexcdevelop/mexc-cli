//go:build !windows

package command

// checkPowerShellPolicy is a Windows-only concern: the .ps1 shim and the
// execution policy that governs it exist nowhere else. Returning "does not
// apply" keeps the row out of the report entirely rather than emitting a pass
// nobody on this platform needs to read — see the Windows implementation for
// what the check actually decides.
func checkPowerShellPolicy() (checkResult, bool) {
	return checkResult{}, false
}
