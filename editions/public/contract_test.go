package public

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/adapter"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// The public edition must reach an UNSIGNED op end-to-end and report it as such.
// Its companion below covers the signed path; between them the two auth outcomes
// a plan can report are both pinned.
//
// contract.market is the product's only unsigned REST subdomain — every spot
// subdomain signs, and activity became signed when its operations started
// requiring an API key — so this is the one REST operation that can prove
// signed=false reaches the plan at all. A WS operation cannot stand in: its
// dry-run prints a different plan shape, with no method.
func TestPublic_UnsignedOpReachable(t *testing.T) {
	stdout, stderr, code := run(nil, "contract", "market", "funding-rate-all", "--dry-run")
	if code != 0 {
		t.Fatalf("contract market funding-rate-all not reachable: code=%d stderr=%s", code, stderr)
	}
	var plan struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Signed bool   `json:"signed"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatalf("plan not JSON: %v\n%s", err, stdout)
	}
	if plan.Method != "GET" || !strings.Contains(plan.URL, "/api/v1/contract/funding_rate") {
		t.Fatalf("wrong shape: %+v", plan)
	}
	if plan.Signed {
		t.Fatalf("an unsigned op must report signed=false")
	}
}

// A credentialed spot op still produces a dry-run plan (no real credentials/signature).
func TestPublic_SignedSpotDryRun(t *testing.T) {
	stdout, stderr, code := run(nil, "spot", "trade", "current-orders", "--dry-run")
	if code != 0 {
		t.Fatalf("credentialed spot dry-run failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "/api/cli/spot/order/current/orders/v2") {
		t.Fatalf("plan missing path: %s", stdout)
	}
}

// The profile builtin ships in the public edition.
func TestPublic_ProfileBuiltinPresent(t *testing.T) {
	_, stderr, code := run(nil, "profile", "list")
	if code != 0 {
		t.Fatalf("profile list should succeed in the public edition: code=%d stderr=%s", code, stderr)
	}
}

// This product serves one env, so it offers no env axis: there is nothing to
// select and nothing to persist. The three entry points that would express a
// choice are absent — `config` (whose only settable key is env), --env, and
// MEXC_ENV. Each is asserted separately: they are dropped by different
// mechanisms and could regress independently.
//
// The shared machinery is unchanged and still covered for a product that DOES
// serve several envs (internal/builtin/config_test.go, internal/cli).
func TestNoEnvAxis_ProductOffersNoEnvSelection(t *testing.T) {
	isolatedHome(t)

	if got := len(hosts.Envs()); got != 1 {
		t.Fatalf("this product registers %d envs (%s); the assertions below assume one",
			got, hosts.KnownEnvs())
	}

	t.Run("config is not mounted", func(t *testing.T) {
		_, e, code := run(nil, "config", "list")
		if code == 0 {
			t.Fatal("`config` ran; a product with no settable key must not mount it")
		}
		if !strings.Contains(e, "unknown command") {
			t.Fatalf("expected an unknown-command envelope, got: %s", e)
		}
	})

	t.Run("--env is not registered", func(t *testing.T) {
		_, e, code := run(nil, "--env", "prod", "activity", "online", "category-list", "--dry-run")
		if code == 0 {
			t.Fatal("--env was accepted; it must not exist without an env axis")
		}
		if !strings.Contains(e, "unknown flag") {
			t.Fatalf("expected an unknown-flag envelope, got: %s", e)
		}
	})

	// The variable matters most: MEXC_ENV left exported in a shell — set for
	// whatever reason, by whatever tool — would otherwise make every command here
	// fail with "host has no endpoint for env …".
	t.Run("MEXC_ENV is not read", func(t *testing.T) {
		_, e, code := run(map[string]string{"MEXC_ENV": "no-such-env"},
			"activity", "online", "category-list", "--dry-run")
		if code != 0 {
			t.Fatalf("MEXC_ENV changed the outcome: exit %d, stderr %s", code, e)
		}
	})
}

// The endpoint table carries production only. A non-production host registered
// here would be published in the source AND named in the "no endpoint for env"
// hint, which lists every env a host serves.
func TestHostRows_ProductionOnly(t *testing.T) {
	rows := adapter.OpenAPIRows().Rows
	if len(rows) != 1 {
		t.Fatalf("openapi registers %d rows, want exactly one (prod)", len(rows))
	}
	ep, ok := rows["prod"]
	if !ok {
		t.Fatalf("the single row is not named prod: %v", rows)
	}
	if ep.URL != adapter.DefaultBaseURL {
		t.Fatalf("prod URL = %q, want %q", ep.URL, adapter.DefaultBaseURL)
	}
}

// The two `config` cases that used to live here — an unknown key, and a write
// blocked by --read-only — are gone with the command. They would still have
// PASSED: both only asserted exit 2, and "unknown command" is also exit 2, so
// they asserted nothing while reading as coverage. The behaviours themselves are
// covered where the command lives, against a product that mounts it:
// internal/builtin/config_test.go.

// SpotWSRows and FuturesWSRows are the WS counterpart of OpenAPIRows: each
// registers exactly one "prod" row, matching this product's public WS
// gateways — the same single-env shape OpenAPIRows takes, for the identical
// reason (MEXC publishes no testnet WS
// gateway at all, so there is not even a hypothetical second row to add).
func TestSpotWSRows_ProductionOnly(t *testing.T) {
	rows := adapter.SpotWSRows().Rows
	if len(rows) != 1 {
		t.Fatalf("spot-ws registers %d rows, want exactly one (prod)", len(rows))
	}
	ep, ok := rows["prod"]
	if !ok {
		t.Fatalf("the single row is not named prod: %v", rows)
	}
	if ep.URL != adapter.DefaultSpotWSURL {
		t.Fatalf("prod URL = %q, want %q", ep.URL, adapter.DefaultSpotWSURL)
	}
}

func TestFuturesWSRows_ProductionOnly(t *testing.T) {
	rows := adapter.FuturesWSRows().Rows
	if len(rows) != 1 {
		t.Fatalf("futures-ws registers %d rows, want exactly one (prod)", len(rows))
	}
	ep, ok := rows["prod"]
	if !ok {
		t.Fatalf("the single row is not named prod: %v", rows)
	}
	if ep.URL != adapter.DefaultFuturesWSURL {
		t.Fatalf("prod URL = %q, want %q", ep.URL, adapter.DefaultFuturesWSURL)
	}
}

// The command surface is exactly what the edition declares: a domain this
// product does not mount must not be reachable, so a capability cannot appear
// by accident (a stray import, a copied file) without being wired in.
func TestPublic_UndeclaredCommandsAreUnreachable(t *testing.T) {
	for _, args := range [][]string{
		{"nosuchdomain"},
		{"nosuchdomain", "sub", "get", "--dry-run"},
		{"spot", "nosuchsub", "get"},
	} {
		_, _, code := run(nil, args...)
		if code == 0 {
			t.Fatalf("public edition must not expose %v (got exit 0)", args)
		}
	}
}

// Everything OTHER than `set` stays non-interactive, with no terminal in sight.
// The split is the whole design: the human saves the credential once at a
// keyboard, the agent uses it freely thereafter. The refusal on `set` itself is
// asserted in editions/public/command, where the terminal can be stubbed —
// driving it from here would read the DEVELOPER's terminal and hang.
func TestProfileReadPathsStayAgentUsable(t *testing.T) {
	isolatedHome(t)

	if _, e, code := run(nil, "profile", "list"); code != 0 {
		t.Fatalf("`profile list` must work without a terminal: exit=%d %s", code, e)
	}
}

// The build-version header must not be the signer's protocol-version header:
// that one is constant across releases and absent from unsigned operations.
func TestPublic_NamesABuildVersionHeaderDistinctFromTheProtocolOne(t *testing.T) {
	ed := Edition()
	if ed.BuildVersionHeader == "" {
		t.Fatal("the public edition must name a build-version header")
	}
	if strings.EqualFold(ed.BuildVersionHeader, "X-MEXC-CLI-VERSION") {
		t.Fatalf("build-version header %q is the signing protocol header; they answer different questions",
			ed.BuildVersionHeader)
	}
}

// The public product declares a client-side throttle. The exact value is a
// product decision, but its shape must hold: a positive rate and burst, and a
// wait cap so a drained bucket never hangs a caller.
func TestPublic_DeclaresARateLimit(t *testing.T) {
	ed := Edition()
	if ed.RateLimit == nil {
		t.Fatal("the public edition must declare a client-side rate limit")
	}
	if ed.RateLimit.RequestsPerSecond <= 0 || ed.RateLimit.Burst <= 0 {
		t.Fatalf("rate and burst must be positive, got %+v", ed.RateLimit)
	}
	if ed.RateLimit.MaxWait <= 0 {
		t.Fatal("MaxWait must be set so a drained bucket cannot hang indefinitely")
	}
}
