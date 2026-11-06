package transport

import (
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/config"

	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// Property 14: a face that states no policy keeps a throttle anyway.
//
// This is the executable form of the constraint that survived the merge of two
// competing rate-limit designs. The mechanism changed — an invocation-scoped
// limiter built from a policy, rather than package-level buckets swapped by
// ConfigureRate — but the property did not: silence must not mean "no ceiling".
//
// _Validates: R13.2_
func TestPropertyNilPolicyKeepsTheSharedDefault(t *testing.T) {
	got := NewSharedLimiter(nil)
	if got == nil {
		t.Fatal("a nil policy produced no limiter; silence must resolve to the shared " +
			"default, or forgetting to state a policy silently removes the ceiling")
	}
}

// The other spelling, and the reason the two must differ. "I want no ceiling" is a
// decision and has its own word; "I said nothing" is an omission and gets the
// default. Collapsing them is what turns a forgotten field into an upstream ban.
func TestUnlimitedIsTheOnlyWayToTurnTheThrottleOff(t *testing.T) {
	if NewSharedLimiter(port.Unlimited) != nil {
		t.Error("transport.Unlimited must yield no limiter")
	}
	if NewSharedLimiter(&port.RateLimitPolicy{Unlimited: true}) != nil {
		t.Error("an inline Unlimited policy must yield no limiter")
	}
	// A zero-valued policy is NOT unlimited: it is a policy whose numbers the
	// limiter floors to something safe. Only the explicit flag disables.
	if NewSharedLimiter(&port.RateLimitPolicy{}) == nil {
		t.Error("a zero-valued policy must still produce a limiter, not silently disable")
	}
}

// The mechanism must actually apply what it is given, or the tests above would
// pass against an implementation that ignores the policy entirely.
func TestAStatedPolicyIsApplied(t *testing.T) {
	slow := NewSharedLimiter(&port.RateLimitPolicy{
		RequestsPerSecond: 1, Burst: 1, MaxWait: time.Millisecond,
	})
	if slow == nil {
		t.Fatal("a stated policy produced no limiter")
	}
	fast := NewSharedLimiter(&port.RateLimitPolicy{
		RequestsPerSecond: 1000, Burst: 1000, MaxWait: time.Second,
	})
	if fast == slow {
		t.Fatal("two different policies produced the same limiter instance")
	}
}

// The default is the MEASURED policy, not the pair of unsourced numbers the old
// design carried. Asserted so a future edit cannot quietly reinstate a 10/s bucket
// or a second, wider bucket for order paths — the one that was inverted against
// what the backend actually does.
func TestDefaultPolicyIsTheMeasuredOne(t *testing.T) {
	d := port.DefaultRateLimit
	if d == nil {
		t.Fatal("there must be a default policy for a nil Edition.RateLimit to resolve to")
	}
	if d.Unlimited {
		t.Fatal("the default must not be unlimited")
	}
	if d.RequestsPerSecond < 18 {
		t.Errorf("RequestsPerSecond = %v; the measured steady state on the lightest "+
			"endpoint is ~18-31/s, so a lower ceiling throttles legitimate use",
			d.RequestsPerSecond)
	}
	if d.MaxWait <= 0 {
		t.Error("MaxWait must be positive: a drained bucket has to give up rather than block forever")
	}
}

// A Client is born with no limiter; the composition root assigns the shared one.
// Pinned because the alternative — New() reaching for a package-level singleton —
// is exactly the process-global state this design replaced, and it would come back
// looking like a convenience.
func TestNewClientDoesNotInventItsOwnLimiter(t *testing.T) {
	if c := New(config.Config{}); c.Limiter != nil {
		t.Fatal("New() supplied a limiter of its own; the invocation owns that decision, " +
			"and a per-Client limiter resets the bucket on every call")
	}
}
