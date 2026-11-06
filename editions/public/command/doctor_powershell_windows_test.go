package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The verdict must split on whether an UNSIGNED, LOCAL script runs, because that
// is what npm's generated shim is. Getting this wrong in either direction has a
// cost: a false warn tells a working install to change a security setting it did
// not need to change, and a false pass leaves the user with a PowerShell
// SecurityException and nothing connecting it to this product.
func TestPowerShellPolicyResult(t *testing.T) {
	const shim = `C:\Users\x\AppData\Roaming\npm\mexc.ps1`

	tests := []struct {
		policy string
		want   string
	}{
		{"Restricted", statusWarn},   // runs no script at all — the Windows client default
		{"AllSigned", statusWarn},    // runs only signed scripts; the shim is unsigned
		{"RemoteSigned", statusPass}, // local unsigned scripts run — the recommended fix
		{"Unrestricted", statusPass},
		{"Bypass", statusPass},
		// Get-ExecutionPolicy prints the effective value; casing comes from
		// PowerShell, so the match must not depend on it.
		{"restricted", statusWarn},
		{"remotesigned", statusPass},
	}
	for _, tt := range tests {
		t.Run(tt.policy, func(t *testing.T) {
			got := powerShellPolicyResult("mexc", tt.policy, shim)
			if got.Status != tt.want {
				t.Fatalf("policy %q = %s, want %s (%s)", tt.policy, got.Status, tt.want, got.Message)
			}
			if !strings.Contains(got.Message, tt.policy) {
				t.Fatalf("message must name the policy it read, got: %s", got.Message)
			}
			if tt.want == statusWarn {
				// The hint has to carry both ways out, and the .cmd one matters most:
				// it needs no security change and works immediately.
				for _, w := range []string{"Set-ExecutionPolicy", "CurrentUser", "RemoteSigned", "mexc.cmd"} {
					if !strings.Contains(got.Hint, w) {
						t.Fatalf("hint must mention %q, got: %s", w, got.Hint)
					}
				}
				if !strings.Contains(got.Message, shim) {
					t.Fatalf("message must name the blocked shim, got: %s", got.Message)
				}
			}
		})
	}
}

// The Process-scope override must not reach the child, or the check answers a
// question nobody asked: whether a process launched from THIS session could run
// the shim, rather than whether the user's own terminal can. Those differ exactly
// where the check earns its keep — inside an agent host that was launched with
// -ExecutionPolicy Bypass, which is where a run is most likely to happen and
// least likely to be representative.
func TestWithoutProcessPolicy(t *testing.T) {
	in := []string{
		"PATH=C:\\Windows",
		"PSExecutionPolicyPreference=Bypass",
		"PSModulePath=C:\\Modules",
		// Windows environment names are case-insensitive; an inherited override
		// in any casing is the same override.
		"psexecutionpolicypreference=Unrestricted",
	}
	got := withoutProcessPolicy(in)
	for _, entry := range got {
		if strings.HasPrefix(strings.ToLower(entry), "psexecutionpolicypreference=") {
			t.Fatalf("the process-scope override survived: %q", entry)
		}
	}
	// Everything else is passed through untouched — including PSModulePath, whose
	// prefix is close enough to matter if the match were sloppy.
	want := []string{"PATH=C:\\Windows", "PSModulePath=C:\\Modules"}
	if len(got) != len(want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The check applies only to an npm install, and the shim's presence on PATH is
// how that is decided. exec.LookPath cannot answer it — .PS1 is not in PATHEXT —
// so this walks PATH itself, and that walk is what the test pins.
func TestFindPowerShellShim(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mexc.ps1"), []byte("# shim"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A .cmd alongside it must not be mistaken for the .ps1: only the PowerShell
	// shim is governed by the execution policy.
	if err := os.WriteFile(filepath.Join(other, "mexc.cmd"), []byte("@echo off"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", strings.Join([]string{other, dir}, string(os.PathListSeparator)))
	if got := findPowerShellShim("mexc"); got != filepath.Join(dir, "mexc.ps1") {
		t.Fatalf("findPowerShellShim = %q, want the .ps1 in %q", got, dir)
	}

	// A binary installed some other way has no .ps1 anywhere, and then there is
	// nothing this check could be about.
	t.Setenv("PATH", other)
	if got := findPowerShellShim("mexc"); got != "" {
		t.Fatalf("with no .ps1 on PATH the check must not apply, got %q", got)
	}

	// The name is the command's, not a fixed literal: another product built on
	// this framework ships under its own.
	t.Setenv("PATH", dir)
	if got := findPowerShellShim("other-cli"); got != "" {
		t.Fatalf("a different command name must not match mexc.ps1, got %q", got)
	}
}
