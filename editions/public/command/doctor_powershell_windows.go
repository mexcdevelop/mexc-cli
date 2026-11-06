package command

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
)

// checkPowerShellPolicy answers one question: will PowerShell refuse to run the
// .ps1 shim npm installed for this command?
//
// WHY THIS EXISTS. `npm i -g` writes three shims next to each other — mexc,
// mexc.cmd and mexc.ps1 — and PowerShell picks the .ps1. Windows client editions
// default the execution policy to Restricted, which refuses to run ANY script
// file, so the very first command a user types after a successful install fails
// with a PowerShell SecurityException and a docs link:
//
//	mexc : File ...\npm\mexc.ps1 cannot be loaded because running scripts is
//	disabled on this system.
//
// (The wording arrives in the console's own language, so do not match on it.)
//
// Nothing in that message mentions this product, and nothing in this product's
// output mentions the policy, so it reads as "the tool is broken" when the tool
// was never reached. Measured on the machine this was written for: CurrentUser
// and LocalMachine both Undefined, so a fresh terminal falls back to the default
// and blocks — while an agent host that launches PowerShell with
// -ExecutionPolicy Bypass never sees it, which is exactly how it stayed hidden.
//
// The bool result reports whether this check APPLIES, and a false is not a pass.
// It is skipped when nothing could go wrong (no .ps1 shim on PATH, so this
// install was not made by npm) and when the answer could not be established.
// Reporting an undetermined policy as either verdict would be inventing a
// finding; the row is simply absent, which leaves the reader no worse off than
// before the check existed.
func checkPowerShellPolicy() (checkResult, bool) {
	shim := findPowerShellShim(cliname.Name())
	if shim == "" {
		return checkResult{}, false
	}
	policy, err := effectiveExecutionPolicy()
	if err != nil || policy == "" {
		return checkResult{}, false
	}
	return powerShellPolicyResult(cliname.Name(), policy, shim), true
}

// powerShellPolicyResult turns a policy name into the verdict. Split from the
// check above so the mapping can be tested without a machine in each state:
// setting a real execution policy is a per-user system change, and a test that
// made one would be changing the developer's environment to assert a string.
func powerShellPolicyResult(command, policy, shim string) checkResult {
	const nm = "powershell_policy"

	// Restricted runs no script at all; AllSigned runs only signed ones, and npm's
	// generated shim carries no signature. Every other policy — RemoteSigned,
	// Unrestricted, Bypass — runs a local unsigned script, which is what the shim
	// is. Get-ExecutionPolicy reports the EFFECTIVE value, so "Undefined" never
	// reaches here: it has already been resolved to whatever it falls back to.
	switch strings.ToLower(policy) {
	case "restricted", "allsigned":
		return warn(nm,
			"PowerShell's execution policy is "+policy+", which blocks the shim npm installed at "+shim,
			"run `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` in PowerShell (no administrator needed), "+
				"or call `"+command+".cmd` instead, which the policy does not govern")
	default:
		return pass(nm, "PowerShell execution policy is "+policy+"; the "+filepath.Base(shim)+" shim npm installed can run")
	}
}

// findPowerShellShim looks for "<command>.ps1" on PATH and returns its full path.
//
// exec.LookPath cannot answer this: on Windows it searches the extensions in
// PATHEXT, and .PS1 is not one of them by default — PowerShell adds scripts to
// its own command discovery rather than the process-wide list. So the directories
// are walked directly.
func findPowerShellShim(command string) string {
	name := command + ".ps1"
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}

// effectiveExecutionPolicy asks PowerShell what it would do, rather than deriving
// it from the registry.
//
// The registry holds one value per scope and the effective policy is the result
// of resolving six of them in order — two Group Policy scopes, the process, the
// user, the machine, and a per-edition default that differs between Windows
// client and Server. Reimplementing that here would be a second, drifting copy of
// a rule PowerShell already applies; asking the shell is the same question the
// user's terminal will ask.
//
// This is the only place `doctor` runs a subprocess, and it is worth stating why
// that does not contradict the "sends nothing of its own" rule at the top of this
// package: that rule is about the network and about this product's endpoints. This
// starts a local, read-only shell builtin with no profile and no interaction, and
// it is bounded — a machine where powershell.exe is missing, disabled or hung
// yields no row rather than a hung `doctor`.
func effectiveExecutionPolicy() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// -NoProfile so a slow or broken user profile cannot affect the answer or the
	// timing; -NonInteractive so nothing can prompt. Get-ExecutionPolicy is a
	// cmdlet, not a script file, so the policy being checked cannot block the
	// check itself.
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Get-ExecutionPolicy")
	cmd.Env = withoutProcessPolicy(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// withoutProcessPolicy drops PSExecutionPolicyPreference from the child's
// environment.
//
// That variable is how a Process-scope policy propagates: `powershell
// -ExecutionPolicy Bypass` sets it, and every descendant inherits it. Leaving it
// in place makes this check answer "what would a process launched from HERE be
// allowed to do", when the question is "what will happen in the user's own
// terminal" — and the two differ precisely where it matters. Measured: run from
// an agent host that launched PowerShell with -ExecutionPolicy Bypass, doctor
// reported Bypass and passed; run from a plain shell on the same machine seconds
// later, Restricted and warned. The user's terminal is the second one.
//
// Reporting pass from inside an exempted session would reproduce the exact blind
// spot this check exists to close: every command works for the agent, and the
// person who installed it cannot run one.
func withoutProcessPolicy(env []string) []string {
	const name = "PSExecutionPolicyPreference="
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		// Environment names are case-insensitive on Windows, so the comparison
		// has to be too: an inherited "psexecutionpolicypreference=" is the same
		// override wearing different casing.
		if len(entry) >= len(name) && strings.EqualFold(entry[:len(name)], name) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
