// Tests for the WS host-key override mechanism's negative space: this
// package's own fixture ("sample stream ws-ticker") sets WSConfig.HostKey to
// testHostKey ("public-example"), which matches neither of the two literal
// keys mount.go's hostOverrides map uses ("spot-ws" / "futures-ws") — so
// --spot-ws-url/--futures-ws-url are necessarily no-ops against it. That is
// exactly the behavior worth asserting here: the override lookup is
// exact-key (hostOverrides[cfg.HostKey]), never "any override present
// wins", so passing the wrong flag for an operation's actual protocol has
// no effect — not merely "is ignored by convention" the way the business
// closure itself used to have to opt out, but structurally unreachable.
//
// The positive case — an override DOES apply when its key matches the
// operation's real HostKey — is asserted in editions/public/wsflags_test.go
// against spot.marketv2's real "spot-ws" key, since this package's fixture
// cannot exercise a match by construction.
package cli

import (
	"strings"
	"testing"
)

// TestWSHostOverride_DefaultUnchangedWhenOmitted confirms that omitting both
// override flags leaves the dry-run plan's URL exactly at whatever
// pkg/hosts.Resolve(cfg.HostKey, env) returns for the fixture's registered
// host key (testHostKey's blank-env row, "https://example.test").
func TestWSHostOverride_DefaultUnchangedWhenOmitted(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.test"`) {
		t.Errorf("stdout = %s, want the registered default URL with no override flags", stdout)
	}
}

// TestWSHostOverride_SpotFlagHasNoEffectOnUnrelatedHostKey confirms
// --spot-ws-url has NO effect on this fixture, whose HostKey (testHostKey)
// is not "spot-ws" — the wrong-key case the hostOverrides map lookup makes
// structurally unreachable, not merely conventionally ignored.
func TestWSHostOverride_SpotFlagHasNoEffectOnUnrelatedHostKey(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--spot-ws-url", "wss://should-not-apply.test/ws", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if strings.Contains(stdout, "should-not-apply") {
		t.Errorf("stdout = %s, want --spot-ws-url to have NO effect on a fixture whose HostKey is %q, not \"spot-ws\"", stdout, testHostKey)
	}
	if !strings.Contains(stdout, `"url": "https://example.test"`) {
		t.Errorf("stdout = %s, want the registered default URL unchanged", stdout)
	}
}

// TestWSHostOverride_FuturesFlagAlsoHasNoEffect is --futures-ws-url's
// counterpart to the above.
func TestWSHostOverride_FuturesFlagAlsoHasNoEffect(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT", "--futures-ws-url", "wss://should-not-apply.test/edge", "--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if strings.Contains(stdout, "should-not-apply") {
		t.Errorf("stdout = %s, want --futures-ws-url to have NO effect on a fixture whose HostKey is %q", stdout, testHostKey)
	}
}

// TestWSHostOverride_BothFlagsPassedNeitherApplies confirms passing BOTH
// override flags at once still leaves an unrelated HostKey's resolution
// untouched — the lookup is exact-key, not "any override present wins".
func TestWSHostOverride_BothFlagsPassedNeitherApplies(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "stream", "ws-ticker",
		"--symbol", "BTCUSDT",
		"--spot-ws-url", "wss://spot-should-not-apply.test/ws",
		"--futures-ws-url", "wss://futures-should-not-apply.test/edge",
		"--dry-run")
	if code != 0 {
		t.Fatalf("want exit 0, got %d (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stdout, `"url": "https://example.test"`) {
		t.Errorf("stdout = %s, want the registered default URL unchanged even with both flags passed", stdout)
	}
}
