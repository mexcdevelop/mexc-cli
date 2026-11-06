//go:build windows

package command

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// checkAppControl answers one question: is this machine running a Windows
// code-integrity policy that can refuse to start the binary behind this command?
//
// WHY THIS EXISTS. Smart App Control is a kernel-level code-integrity policy. In
// enforcement mode it requires an executable image to be either signed by a
// trusted publisher or judged "verified and reputable" by Microsoft's cloud, and
// it refuses everything else at CreateProcess time. The npm launcher spawns the
// platform binary through Node, and libuv has no mapping for the failure such a
// refusal returns — so the user is handed `spawn UNKNOWN`, a message that names
// no cause and suggests a broken install.
//
// Measured on Windows 11 with Smart App Control enforcing: the first run of a
// freshly installed, unsigned binary failed with `cannot start platform binary:
// spawn UNKNOWN`; the CodeIntegrity operational log recorded event 3077 naming
// the exact file and the policy id, alongside a 3118 "Smart App Control Block";
// and the same command succeeded a minute later, once the verdict was cached.
// Nothing in the CLI's own output connected the two.
//
// The check does not inspect this binary's signature. Whether the shipped
// binaries are signed is a property of the release, not of the machine, and a
// claim about it here would go stale the day signing lands. What the machine can
// be asked is its policy state, and that plus the symptom is enough for the
// reader to recognise the failure when it happens.
//
// The bool reports whether the check APPLIES. It is skipped when the policy is
// off — the overwhelmingly common case, since Smart App Control only ever turns
// itself on after a clean install of Windows 11 — and when the state could not be
// read, because reporting an unknown state as either verdict would be inventing a
// finding.
func checkAppControl() (checkResult, bool) {
	state, err := smartAppControlState()
	if err != nil {
		return checkResult{}, false
	}
	return appControlResult(state)
}

// Smart App Control's three states, as the policy key reports them.
const (
	sacOff        = 0 // never turned on, or turned off by the user
	sacEnforcing  = 1 // blocking untrusted images
	sacEvaluating = 2 // watching, blocking nothing, may switch itself on later
)

// appControlResult maps a policy state to the verdict. Split from the read so the
// mapping can be tested without a machine in each state: Smart App Control cannot
// be switched per-test, and until the March 2026 update turning it off was a
// one-way door that needed a clean install to undo.
func appControlResult(state int) (checkResult, bool) {
	const nm = "app_control"

	switch state {
	case sacEnforcing:
		return warn(nm,
			"Smart App Control is enforcing, so Windows can refuse to start an unsigned binary",
			"a refusal surfaces as `cannot start platform binary: spawn UNKNOWN`, which names no cause. "+
				"Run the command again first — the verdict is cached, and a second attempt usually "+
				"succeeds. To confirm a block, check `Get-WinEvent -LogName "+
				"Microsoft-Windows-CodeIntegrity/Operational -MaxEvents 20` for a 3077 or 3118 event "+
				"naming the file. Smart App Control can also be turned off under Windows Security > "+
				"App & browser control"), true
	case sacEvaluating:
		return pass(nm,
			"Smart App Control is in evaluation mode; it blocks nothing yet, but Windows may switch it on later"), true
	default:
		// Off, or a value this build does not know. Either way there is nothing to
		// report: the row exists to explain a failure mode, not to enumerate
		// settings.
		return checkResult{}, false
	}
}

// smartAppControlState reads the policy state Windows publishes for Smart App
// Control.
//
// It asks PowerShell rather than opening the key directly. That costs a
// subprocess, which this package otherwise avoids, and the trade is the same one
// effectiveExecutionPolicy already makes and documents: a registry read from Go
// needs either a vendored registry package this module does not carry or a hand
// written syscall with unsafe pointer arithmetic, and neither is worth adding for
// one DWORD in a diagnostic command. The call is local, read-only, takes no user
// input and is bounded by a timeout.
//
// A missing key is not an error state to report — it is how a machine that never
// had Smart App Control looks — so it comes back as sacOff rather than as a
// failure.
func smartAppControlState() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The value is absent on machines that never had the feature, and
	// -ErrorAction SilentlyContinue keeps that from becoming a non-zero exit; the
	// empty output is then read as "off" below.
	const script = `(Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\CI\Policy' ` +
		`-Name VerifiedAndReputablePolicyState -ErrorAction SilentlyContinue).VerifiedAndReputablePolicyState`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return sacOff, nil
	}
	return strconv.Atoi(text)
}
