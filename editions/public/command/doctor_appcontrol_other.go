//go:build !windows

package command

// checkAppControl is a Windows-only concern: Smart App Control and the
// code-integrity policy it enforces exist nowhere else. Returning "does not
// apply" keeps the row out of the report entirely rather than emitting a pass
// nobody on this platform needs to read — see the Windows implementation for what
// the check actually decides.
func checkAppControl() (checkResult, bool) {
	return checkResult{}, false
}
