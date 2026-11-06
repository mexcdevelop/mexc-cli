package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	xport "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// domainsWithHeaderParam: one operation, one header param, given wire name.
func domainsWithHeaderParam(name string) []api.DomainSpec {
	return []api.DomainSpec{{
		Name: "sample", Short: "Fixture domain",
		Subs: []api.Subdomain{{
			Name: "public", Short: "Unsigned reads",
			Signed: api.SignNone, Identity: api.IdentityNone, HostKey: "fixture",
			Ops: []api.Operation{{
				Name: "ticker", Method: "GET", Path: "/fixture/ticker",
				Access: api.Read, Summary: "One header parameter",
				Params: []api.Param{{Name: name, Desc: "Fixture", In: api.InHeader}},
			}},
		}},
	}}
}

// Opting out validates clean even where header params exist.
func TestValidateBuildVersionHeader_EmptyNameIsFine(t *testing.T) {
	if err := validateBuildVersionHeader(domainsWithHeaderParam("language"), ""); err != nil {
		t.Fatalf("an unnamed build-version header must validate clean, got: %v", err)
	}
}

func TestValidateBuildVersionHeader_UncontestedNameIsFine(t *testing.T) {
	if err := validateBuildVersionHeader(domainsWithHeaderParam("language"), "X-MEXC-CLI-BUILD-VERSION"); err != nil {
		t.Fatalf("no collision here, got: %v", err)
	}
}

// The collision this guard exists for: the param is set later and wins.
func TestValidateBuildVersionHeader_RejectsACollidingParam(t *testing.T) {
	err := validateBuildVersionHeader(domainsWithHeaderParam("X-MEXC-CLI-BUILD-VERSION"), "X-MEXC-CLI-BUILD-VERSION")
	if err == nil {
		t.Fatal("a header param named the same as the build-version header must be refused")
	}
	if !strings.Contains(err.Error(), "sample.public.ticker") {
		t.Errorf("the error must name the offending operation, got: %v", err)
	}
}

// Header names are case-insensitive, so a recased param collides just as hard.
func TestValidateBuildVersionHeader_ComparesCaseInsensitively(t *testing.T) {
	if err := validateBuildVersionHeader(domainsWithHeaderParam("x-mexc-cli-build-version"), "X-MEXC-CLI-BUILD-VERSION"); err == nil {
		t.Fatal("a case-differing param name still collides on the wire and must be refused")
	}
}

// A query param of the same name is a different wire slot.
func TestValidateBuildVersionHeader_IgnoresAQueryParamOfTheSameName(t *testing.T) {
	ds := domainsWithHeaderParam("X-MEXC-CLI-BUILD-VERSION")
	ds[0].Subs[0].Ops[0].Params[0].In = api.InQuery
	if err := validateBuildVersionHeader(ds, "X-MEXC-CLI-BUILD-VERSION"); err != nil {
		t.Fatalf("a query param of the same name occupies a different wire slot, got: %v", err)
	}
}

// SetRateLimit must actually reach the Client the factory builds. This guards the
// wiring order: the shared limiter has to be created from the policy the
// composition root injects AFTER NewApp returns, not from the nil value present
// while NewApp runs.
func TestSetRateLimit_ReachesTheClient(t *testing.T) {
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(string) string { return "" }, nil, nil)
	app.SetRateLimit(&xport.RateLimitPolicy{RequestsPerSecond: 50, Burst: 50, MaxWait: 30 * time.Second})
	if app.f.Client(context.Background()).Limiter == nil {
		t.Fatal("SetRateLimit did not reach the Client: the limiter is nil, so the throttle never runs")
	}
}

// Saying nothing gets the shared DEFAULT, not no throttle.
//
// This assertion is the reverse of what it was before the two rate-limit designs
// were merged. The old shape read a nil policy as "this product does no
// client-side throttling"; the merged rule is that only an explicit opt-out means
// that, because "I forgot to state a policy" must not be spelled the same way as
// "I want no ceiling". Only one of those is a decision.
func TestNoRateLimit_FallsBackToTheDefaultPolicy(t *testing.T) {
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(string) string { return "" }, nil, nil)
	if app.f.Client(context.Background()).Limiter == nil {
		t.Fatal("a product that stated no policy got no limiter; silence must resolve to " +
			"the shared default, or a forgotten field removes the ceiling")
	}
}

// And the opt-out has its own spelling, which is the whole point of the rule above.
func TestUnlimitedRateLimit_LeavesClientUnthrottled(t *testing.T) {
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(string) string { return "" }, nil, nil)
	app.SetRateLimit(xport.Unlimited)
	if app.f.Client(context.Background()).Limiter != nil {
		t.Fatal("transport.Unlimited must leave the Client with no limiter")
	}
}

// THE property the design rests on, and the one that moved during the merge: it
// used to be guaranteed by a package-level singleton in internal/transport, and is
// now guaranteed by the invocation-scoped OnceValue in the Client factory. Either
// way every Client must share ONE limiter.
//
// A per-Client limiter resets the bucket on each call. The server face builds a
// Client per request, so that mistake makes the ceiling a no-op — and the symptom
// is not a failing test, it is an upstream ban.
func TestEveryClientSharesOneLimiter(t *testing.T) {
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(string) string { return "" }, nil, nil)
	app.SetRateLimit(&xport.RateLimitPolicy{RequestsPerSecond: 50, Burst: 50, MaxWait: time.Second})

	a := app.f.Client(context.Background()).Limiter
	b := app.f.Client(context.Background()).Limiter
	if a == nil || b == nil {
		t.Fatal("the policy did not reach the clients")
	}
	if a != b {
		t.Fatal("two Clients hold different limiters; the budget is a process one, and a " +
			"limiter per Client resets the bucket on every call")
	}
}
