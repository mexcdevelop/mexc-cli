// Boundary tests for the --limit / --listen-duration flags shared by every
// Kind: api.KindWSCollect operation. All cases
// run under --dry-run: validation happens before any dial, so a dry-run and a
// real invocation must fail identically on a bad flag value (verified
// manually against a real WS connection during the same investigation that
// added this file).
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWSFlags_ListenDurationNegativeRejected guards the bug this file exists
// to prevent regressing: --listen-duration accepted any string
// time.ParseDuration itself accepts, including a negative one ("-5s"). A
// negative Duration fails runLoop's "> 0" bounded/deadline checks the same
// way a zero Duration does, so the read loop silently ran UNBOUNDED instead
// of rejecting the flag — the same class of silent misinterpretation
// --limit's own negative check exists to prevent (see the sibling case
// below). Confirmed against a real connection: before the fix, this command
// streamed indefinitely; after the fix, it is rejected before any dial.
func TestWSFlags_ListenDurationNegativeRejected(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--listen-duration", "-5s", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for a negative --listen-duration, got 0 (stderr=%s)", stderr)
	}
	env := mustErrEnvelope(t, stderr)
	if env.Error.Type != "validation" || env.Error.Subtype != "invalid_argument" {
		t.Errorf("type/subtype = %s/%s, want validation/invalid_argument", env.Error.Type, env.Error.Subtype)
	}
	if env.Error.Param != "--listen-duration" {
		t.Errorf("param = %q, want --listen-duration", env.Error.Param)
	}
}

// TestWSFlags_LimitNegativeRejected is --limit's pre-existing counterpart to
// the above, kept alongside it so the two flags' negative-value handling is
// asserted symmetrically in one place.
func TestWSFlags_LimitNegativeRejected(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--limit", "-1", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for a negative --limit, got 0 (stderr=%s)", stderr)
	}
	env := mustErrEnvelope(t, stderr)
	if env.Error.Type != "validation" || env.Error.Subtype != "invalid_argument" {
		t.Errorf("type/subtype = %s/%s, want validation/invalid_argument", env.Error.Type, env.Error.Subtype)
	}
	if env.Error.Param != "--limit" {
		t.Errorf("param = %q, want --limit", env.Error.Param)
	}
}

// TestWSFlags_BothNegative_ListenDurationReportedFirst locks in the
// evaluation order inside runWSOperation: --listen-duration is parsed and
// checked before --limit, so when both are invalid the caller sees the
// --listen-duration error, not --limit's. This is an implementation detail
// worth pinning down explicitly — otherwise a future reordering could flip
// which flag an agent is told to fix, with no test failing to flag it.
func TestWSFlags_BothNegative_ListenDurationReportedFirst(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--limit", "-1", "--listen-duration", "-5s", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit, got 0 (stderr=%s)", stderr)
	}
	env := mustErrEnvelope(t, stderr)
	if env.Error.Param != "--listen-duration" {
		t.Errorf("param = %q, want --listen-duration (checked first in runWSOperation)", env.Error.Param)
	}
}

// TestWSFlags_ZeroIsNotBounded confirms 0 is legal for both flags and both
// mean "no bound" (not "bound at zero") — verified via the dry-run plan's
// "bounded" field, which mirrors runLoop's own bounded := Duration>0 ||
// Limit>0.
func TestWSFlags_ZeroIsNotBounded(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--limit", "0", "--listen-duration", "0s", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, `"bounded": false`) {
		t.Errorf("stdout = %s, want bounded: false for --limit 0 --listen-duration 0s", stdout)
	}
}

// TestWSFlags_ListenDurationMissingUnitRejected guards time.ParseDuration's
// own format rule (a bare number with no unit, e.g. "5", is invalid Go
// duration syntax) surfacing as a structured validation error rather than a
// raw Go error string leaking through unformatted.
func TestWSFlags_ListenDurationMissingUnitRejected(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--listen-duration", "5", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for a unit-less --listen-duration, got 0 (stderr=%s)", stderr)
	}
	env := mustErrEnvelope(t, stderr)
	if env.Error.Param != "--listen-duration" {
		t.Errorf("param = %q, want --listen-duration", env.Error.Param)
	}
}

// TestWSFlags_ListenDurationOverflowRejected guards a duration string whose
// magnitude overflows time.Duration's own int64 range — time.ParseDuration
// itself refuses it, and that refusal must still surface as our structured
// validation envelope, not a bare Go error.
func TestWSFlags_ListenDurationOverflowRejected(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--listen-duration", "99999999999999999999h", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for an overflowing --listen-duration, got 0 (stderr=%s)", stderr)
	}
	env := mustErrEnvelope(t, stderr)
	if env.Error.Param != "--listen-duration" {
		t.Errorf("param = %q, want --listen-duration", env.Error.Param)
	}
}

// TestWSFlags_EmptyListenDurationMeansUnset confirms an explicit empty string
// is treated identically to the flag never being passed (unbounded), not as
// an invalid duration.
func TestWSFlags_EmptyListenDurationMeansUnset(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--listen-duration", "", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, `"bounded": false`) {
		t.Errorf("stdout = %s, want bounded: false for an empty --listen-duration", stdout)
	}
}

// TestWSFlags_RequiredSymbolMissingRejected confirms a required --symbol
// (spot ws-ticker) is enforced as a normal required Param. futures'
// ws-depth/ws-trades (which do NOT share ws-ticker's optional-symbol
// carve-out) are asserted alongside futures' optional-symbol case in
// editions/public (this package's test fixture only mounts spot; futures is
// assembled by the public edition, see editions/public/wsflags_test.go).
func TestWSFlags_RequiredSymbolMissingRejected(t *testing.T) {
	_, stderr, code := run(nil, "sample", "stream", "ws-ticker", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for a missing required --symbol, got 0 (stderr=%s)", stderr)
	}
}

// mustErrEnvelope decodes stderr into errEnvelope (declared in
// contract_test.go) or fails the test.
func mustErrEnvelope(t *testing.T, stderr string) errEnvelope {
	t.Helper()
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr is not a valid error envelope: %v (stderr=%s)", err, stderr)
	}
	return env
}
