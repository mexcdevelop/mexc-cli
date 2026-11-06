package public

// Futures-specific WS flag boundary tests. These live here (not
// internal/cli) because futures is assembled by THIS edition
// (editions/public/domains.go) — internal/cli's own contract tests mount
// only a spot test fixture and have no futures commands to exercise. See
// internal/cli/wsflags_test.go for the shared --limit/--listen-duration
// boundary cases (spot, but the validation logic is identical for every
// Kind: api.KindWSCollect operation regardless of domain).
import (
	"strings"
	"testing"
)

// TestWSFlags_FuturesTickerSymbolOptional confirms futures ws-ticker's
// documented optional --symbol: omitted, it
// subscribes to the whole-market sub.tickers channel; an explicit empty
// string behaves the same as omitting it (both take the values["symbol"] ==
// "" branch in the WSCollect closure).
func TestWSFlags_FuturesTickerSymbolOptional(t *testing.T) {
	for _, args := range [][]string{
		{"futures", "market", "ws-ticker", "--dry-run"},
		{"futures", "market", "ws-ticker", "--symbol", "", "--dry-run"},
	} {
		stdout, stderr, code := run(nil, args...)
		if code != 0 {
			t.Fatalf("args=%v: want exit 0, got %d (stderr=%s)", args, code, stderr)
		}
		if !strings.Contains(stdout, `"sub.tickers"`) {
			t.Errorf("args=%v: stdout = %s, want channel sub.tickers (whole-market mode)", args, stdout)
		}
	}
}

// TestWSFlags_FuturesRequiredSymbolMissingRejected confirms futures
// ws-depth/ws-trades do NOT share ws-ticker's optional-symbol carve-out —
// --symbol stays a normal required Param for them.
func TestWSFlags_FuturesRequiredSymbolMissingRejected(t *testing.T) {
	for _, args := range [][]string{
		{"futures", "market", "ws-depth", "--dry-run"},
		{"futures", "market", "ws-trades", "--dry-run"},
	} {
		_, stderr, code := run(nil, args...)
		if code == 0 {
			t.Fatalf("args=%v: want non-zero exit for a missing required --symbol, got 0 (stderr=%s)", args, stderr)
		}
	}
}

// TestWSFlags_FuturesListenDurationNegativeRejected re-asserts the
// --listen-duration negative-value fix (internal/cli/wsflags_test.go) against
// a futures operation, since runWSOperation is shared code but futures'
// commands are only reachable through this edition's assembled tree.
func TestWSFlags_FuturesListenDurationNegativeRejected(t *testing.T) {
	_, stderr, code := run(nil, "futures", "market", "ws-ticker",
		"--symbol", "BTC_USDT", "--listen-duration", "-5s", "--dry-run")
	if code == 0 {
		t.Fatalf("want non-zero exit for a negative --listen-duration, got 0 (stderr=%s)", stderr)
	}
	if !strings.Contains(stderr, `"--listen-duration"`) {
		t.Errorf("stderr = %s, want it to name --listen-duration", stderr)
	}
}

// TestWSFlags_SpotMarketv2WSTickerReachableAndUnsigned confirms
// ws-ticker/ws-depth/ws-trades were migrated to spot.marketv2 (alongside
// this subdomain's signed CLI-gateway REST ops) and that each one still
// dry-runs as unsigned, per-op overriding the subdomain's signed default
// (signing.CliHMAC) to api.SignNone — the same public, unauthenticated
// WS channel these operations always were under spot.market.
func TestWSFlags_SpotMarketv2WSTickerReachableAndUnsigned(t *testing.T) {
	stdout, stderr, code := run(nil, "spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT", "--dry-run")
	if code != 0 {
		t.Fatalf("spot marketv2 ws-ticker not reachable: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"signed": false`) {
		t.Errorf("stdout = %s, want signed: false (per-op override), even though spot.marketv2 defaults to signed", stdout)
	}
}

// TestWSHostOverride_DefaultURLs confirms every WS operation resolves to its
// documented production default when
// no override is present — the registered adapter.SpotWSRows()/
// FuturesWSRows() rows, reached through the real assembled edition rather
// than a test fixture.
func TestWSHostOverride_DefaultURLs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT", "--dry-run"}, "wss://wbs-api.mexc.com/ws"},
		{[]string{"spot", "account", "ws-deals", "--dry-run"}, "wss://wbs-api.mexc.com/ws"},
		{[]string{"futures", "market", "ws-ticker", "--dry-run"}, "wss://contract.mexc.com/edge"},
		{[]string{"futures", "account", "ws-deals", "--dry-run"}, "wss://contract.mexc.com/edge"},
	}
	for _, c := range cases {
		stdout, stderr, code := run(nil, c.args...)
		if code != 0 {
			t.Fatalf("args=%v: want exit 0, got %d (stderr=%s)", c.args, code, stderr)
		}
		if !strings.Contains(stdout, c.want) {
			t.Errorf("args=%v: stdout = %s, want it to contain %q", c.args, stdout, c.want)
		}
	}
}

// TestWSHostOverride_SpotFlagAppliesToSpotOperations confirms --spot-ws-url
// redirects EVERY spot WS operation's dry-run URL (public marketv2 channels
// and the private account channel alike — both resolve through the same
// "spot-ws" HostKey), replacing the registered default.
func TestWSHostOverride_SpotFlagAppliesToSpotOperations(t *testing.T) {
	for _, args := range [][]string{
		{"spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT", "--spot-ws-url", "wss://custom-spot.test/ws", "--dry-run"},
		{"spot", "account", "ws-deals", "--spot-ws-url", "wss://custom-spot.test/ws", "--dry-run"},
	} {
		stdout, stderr, code := run(nil, args...)
		if code != 0 {
			t.Fatalf("args=%v: want exit 0, got %d (stderr=%s)", args, code, stderr)
		}
		if !strings.Contains(stdout, "wss://custom-spot.test/ws") {
			t.Errorf("args=%v: stdout = %s, want the --spot-ws-url override applied", args, stdout)
		}
	}
}

// TestWSHostOverride_FuturesFlagAppliesToFuturesOperations is
// --futures-ws-url's counterpart, across futures' public market channel and
// its login-based private channel (both resolve through "futures-ws").
func TestWSHostOverride_FuturesFlagAppliesToFuturesOperations(t *testing.T) {
	for _, args := range [][]string{
		{"futures", "market", "ws-ticker", "--futures-ws-url", "wss://custom-futures.test/edge", "--dry-run"},
		{"futures", "account", "ws-deals", "--futures-ws-url", "wss://custom-futures.test/edge", "--dry-run"},
	} {
		stdout, stderr, code := run(nil, args...)
		if code != 0 {
			t.Fatalf("args=%v: want exit 0, got %d (stderr=%s)", args, code, stderr)
		}
		if !strings.Contains(stdout, "wss://custom-futures.test/edge") {
			t.Errorf("args=%v: stdout = %s, want the --futures-ws-url override applied", args, stdout)
		}
	}
}

// TestWSHostOverride_WrongFlagHasNoEffect confirms --futures-ws-url passed to
// a spot operation (and vice versa) is structurally never consulted — the
// operation's own registered default still shows in the plan, not the
// unrelated flag's value. This is the cross-check for the "generic,
// exact-HostKey-keyed lookup" design: mount.go registers both flags on
// every KindWSCollect operation uniformly, and it is this lookup — not a
// business closure choosing to ignore the wrong parameter — that makes the
// wrong flag inert.
func TestWSHostOverride_WrongFlagHasNoEffect(t *testing.T) {
	stdout, stderr, code := run(nil, "spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT",
		"--futures-ws-url", "wss://should-not-apply.test/edge", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if strings.Contains(stdout, "should-not-apply") {
		t.Errorf("stdout = %s, want --futures-ws-url to have no effect on a spot operation", stdout)
	}
	if !strings.Contains(stdout, "wss://wbs-api.mexc.com/ws") {
		t.Errorf("stdout = %s, want the spot default URL unchanged", stdout)
	}
}

// TestWSHostOverride_EnvVarOverride confirms MEXC_SPOT_WS_BASE_URL /
// MEXC_FUTURES_WS_BASE_URL redirect the default URL with NO new code beyond
// registering "spot-ws"/"futures-ws" into pkg/hosts (adapter.SpotWSRows() /
// FuturesWSRows()) — pkg/hosts.Resolve's existing per-key env-var convention
// (envVarName) grants this automatically, the same way MEXC_OPENAPI_BASE_URL
// already works for the REST host. Uses t.Setenv (a real process env var),
// not run's injected env map: pkg/hosts.Resolve calls os.Getenv directly
// (see hosts.go), the same reason every other MEXC_OPENAPI_BASE_URL test in
// this repo (e.g. doctor_test.go) uses t.Setenv rather than run's env map.
func TestWSHostOverride_EnvVarOverride(t *testing.T) {
	t.Setenv("MEXC_SPOT_WS_BASE_URL", "wss://env-spot.test/ws")
	stdout, stderr, code := run(nil, "spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, "wss://env-spot.test/ws") {
		t.Errorf("stdout = %s, want MEXC_SPOT_WS_BASE_URL to redirect the URL", stdout)
	}
	t.Setenv("MEXC_SPOT_WS_BASE_URL", "")

	t.Setenv("MEXC_FUTURES_WS_BASE_URL", "wss://env-futures.test/edge")
	stdout, stderr, code = run(nil, "futures", "market", "ws-ticker", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, "wss://env-futures.test/edge") {
		t.Errorf("stdout = %s, want MEXC_FUTURES_WS_BASE_URL to redirect the URL", stdout)
	}
}

// TestWSHostOverride_FlagWinsOverEnvVar confirms --spot-ws-url takes
// precedence over MEXC_SPOT_WS_BASE_URL when both are present for the same
// invocation — the same override-chain order (flag > env var > registered
// default) internal/transport/send.go's resolveBase already documents for a
// REST RequestSpec.HostKey (--base-url > MEXC_<KEY>_BASE_URL > builtin).
func TestWSHostOverride_FlagWinsOverEnvVar(t *testing.T) {
	t.Setenv("MEXC_SPOT_WS_BASE_URL", "wss://env-spot.test/ws")
	stdout, stderr, code := run(nil,
		"spot", "marketv2", "ws-ticker", "--symbol", "BTCUSDT",
		"--spot-ws-url", "wss://flag-wins.test/ws", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, "wss://flag-wins.test/ws") {
		t.Errorf("stdout = %s, want --spot-ws-url to win over MEXC_SPOT_WS_BASE_URL", stdout)
	}
	if strings.Contains(stdout, "env-spot.test") {
		t.Errorf("stdout = %s, want the env var's value NOT to appear once the flag wins", stdout)
	}
}
