package public

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
)

// report is the doctor payload as a caller parses it. Declared here rather than
// exported from the command package because that is the contract under test: an
// agent reads this shape off stdout, and it must not change silently.
type report struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Checks  []struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"checks"`
}

func parseReport(t *testing.T, stdout string) report {
	t.Helper()
	var r report
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout must be the JSON report, got %q: %v", stdout, err)
	}
	if len(r.Checks) == 0 {
		t.Fatalf("report carries no checks: %q", stdout)
	}
	return r
}

func statusOf(t *testing.T, r report, name string) string {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	t.Fatalf("no check named %q in %+v", name, r.Checks)
	return ""
}

// doctor is mounted by this edition, and on a machine with nothing configured it
// reports a healthy installation: not being set up yet is not a fault.
func TestDoctor_MountedAndHealthyOnAFreshMachine(t *testing.T) {
	isolatedHome(t)

	stdout, _, code := run(nil, "doctor")
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", code, stdout)
	}
	r := parseReport(t, stdout)
	if !r.OK {
		t.Fatalf("ok = false on a fresh machine: %+v", r.Checks)
	}
	// The build string is a field, not a check: it is context and cannot fail.
	if r.Version == "" {
		t.Errorf("report must carry the build version: %+v", r)
	}
	// The command must actually be wired, not merely absent-and-silent: cobra
	// answers an unknown subcommand with exit 2, which parseReport would have
	// caught, but pinning the check set guards the mounting too.
	for _, want := range []string{"state_dir", "endpoint", "credentials", "skills_docs", "read_only"} {
		statusOf(t, r, want)
	}
}

// A buffer is not a terminal, so the whole test suite exercises the JSON view —
// which is also what an agent and a script get, since their stdout is a pipe.
// This pins that default explicitly rather than leaving it implied by the other
// tests happening to parse JSON.
func TestDoctor_NonTerminalOutputStaysJSON(t *testing.T) {
	isolatedHome(t)

	stdout, _, code := run(nil, "doctor")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("a piped run must emit JSON, got %q", stdout)
	}
	// --json at a non-terminal is a no-op, not an error.
	stdout, _, code = run(nil, "doctor", "--json")
	if code != 0 || !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("--json exit = %d, stdout = %q", code, stdout)
	}
}

// An active profile the loader refuses is the case doctor exists for: every
// business command reports "requires API credentials", which is what a machine
// with NO profile says, so the user is told to save one they already have.
func TestDoctor_UnusableActiveProfileFailsWithExitCode(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)

	if err := profile.Save(profile.Profile{Name: "prod", APIKey: "K", SecretKey: "S"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := profile.SetActive("prod"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "profiles", "prod"), 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	stdout, stderr, code := run(nil, "doctor")
	// Exit 2 is this tree's validation/failed-precondition code — doctor reuses the
	// product's one exit-code contract rather than inventing a code of its own.
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stdout=%s stderr=%s", code, stdout, stderr)
	}
	r := parseReport(t, stdout)
	if r.OK {
		t.Fatalf("ok must be false when a check failed: %+v", r.Checks)
	}
	if got := statusOf(t, r, "credentials"); got != "fail" {
		t.Fatalf("credentials = %q, want fail: %+v", got, r.Checks)
	}
	if !strings.Contains(stdout, "chmod 600") {
		t.Errorf("the report must carry the remedy, got %s", stdout)
	}
	// stdout stays data, stderr carries the envelope.
	//
	// Assert the envelope DOCTOR wrote, not merely that one exists. `App.Run`
	// wraps any RunE error that reached it without a recorded exit code into a
	// generic validation envelope with exit 2 — so "code == 2" and "stderr
	// mentions error" are both satisfied when doctor writes no envelope at all.
	// The first version of this test checked only those two and stayed green with
	// `d.WriteError(err)` deleted.
	env := parseEnvelope(t, stderr)
	if env.Error.Subtype != "failed_precondition" {
		t.Errorf("subtype = %q, want failed_precondition (cobra's fallback says invalid_argument)", env.Error.Subtype)
	}
	if !strings.Contains(env.Error.Hint, "failing checks on stdout") {
		t.Errorf("hint must point the reader at the report, got %q", env.Error.Hint)
	}
}

// envelope is the error shape written to stderr.
type envelope struct {
	Error struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
}

func parseEnvelope(t *testing.T, stderr string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal([]byte(stderr), &e); err != nil {
		t.Fatalf("stderr must be a JSON error envelope, got %q: %v", stderr, err)
	}
	return e
}

// A failing run still prints the full report on stdout. The per-check detail is
// the answer the caller asked for; the envelope only carries the exit code.
func TestDoctor_ReportIsWrittenEvenWhenAnyCheckFails(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	stdout, _, code := run(nil, "doctor")
	if code == 0 {
		t.Fatalf("a world-writable state dir must fail the run; stdout=%s", stdout)
	}
	r := parseReport(t, stdout)
	if got := statusOf(t, r, "state_dir"); got != "fail" {
		t.Fatalf("state_dir = %q, want fail: %+v", got, r.Checks)
	}
}

// doctor must survive a broken installation, since that is when it is wanted: it
// overrides the root's config-resolving PersistentPreRunE the way `profile` and
// `help` do. An endpoint redirected by the environment is reported, not fatal.
func TestDoctor_ReportsAnOverriddenEndpoint(t *testing.T) {
	isolatedHome(t)
	// Read by hosts.Resolve through os.Getenv, not the injected reader.
	t.Setenv("MEXC_OPENAPI_BASE_URL", "https://example.test")

	stdout, _, code := run(nil, "doctor")
	if code != 0 {
		t.Fatalf("an override is a warning, not a failure: exit = %d, stdout=%s", code, stdout)
	}
	r := parseReport(t, stdout)
	if got := statusOf(t, r, "endpoint"); got != "warn" {
		t.Fatalf("endpoint = %q, want warn: %+v", got, r.Checks)
	}
	if !strings.Contains(stdout, "example.test") {
		t.Errorf("the report must name where requests actually go, got %s", stdout)
	}
}
