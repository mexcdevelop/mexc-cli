package transport

import (
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// identitiesStubProvider is a provider that records nothing and resolves nothing;
// the tests below care only about WHICH provider the client reached.
type identitiesStubProvider struct {
	realm credential.Realm
	token string
}

func (p identitiesStubProvider) Realm() credential.Realm { return p.realm }
func (p identitiesStubProvider) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{Token: p.token}, nil
}
func (identitiesStubProvider) Inject(*api.RequestSpec, credential.Credentials) error { return nil }

// countingLookup records how many times it was asked, so a test can prove the
// client consulted it rather than the package-level registry.
type countingLookup struct {
	p     credential.Provider
	realm credential.Realm
	asked int
}

func (l *countingLookup) Provider(r credential.Realm) (credential.Provider, bool) {
	l.asked++
	if r == l.realm {
		return l.p, true
	}
	return nil, false
}

// TestIdentitiesZeroValueIsTheProcessRegistry is the regression that protects
// every existing face. A Client that nobody configured must reach exactly the
// provider Register() put in the process registry — this is the mechanism by which
// the local CLI's behaviour is unchanged by the whole Lookup change.
func TestIdentitiesZeroValueIsTheProcessRegistry(t *testing.T) {
	const realm = credential.Realm("transport-identities-zero")
	registered := identitiesStubProvider{realm: realm, token: "from-registry"}
	credential.Register(registered)

	c := New(config.Config{})
	if c.Identities != nil {
		t.Fatal("New() set Identities; the zero value must mean the process registry")
	}

	got, ok := c.identities().Provider(realm)
	if !ok {
		t.Fatal("identities() could not reach the registered provider")
	}
	if got != credential.Provider(registered) {
		t.Fatalf("identities() returned %v, want the registered provider", got)
	}
}

// TestIdentitiesPerRequestOverridesTheRegistry proves the per-request path is real:
// with a Lookup set, the client must reach THAT provider even though a different
// one is registered for the same realm under the process registry.
//
// This is the assertion that would fail if any call site went back to reading the
// package-level map — which is the mistake the whole change exists to prevent.
func TestIdentitiesPerRequestOverridesTheRegistry(t *testing.T) {
	const realm = credential.Realm("transport-identities-perrequest")
	credential.Register(identitiesStubProvider{realm: realm, token: "from-registry"})

	perRequest := identitiesStubProvider{realm: realm, token: "from-caller"}
	lk := &countingLookup{p: perRequest, realm: realm}

	c := New(config.Config{})
	c.Identities = lk

	got, ok := c.identities().Provider(realm)
	if !ok {
		t.Fatal("identities() could not reach the per-request provider")
	}
	if got != credential.Provider(perRequest) {
		t.Fatalf("identities() returned %v, want the per-request provider", got)
	}
	if lk.asked != 1 {
		t.Fatalf("the per-request Lookup was asked %d times, want 1", lk.asked)
	}

	// And the registry still holds the other one, so the test above is not passing
	// because the registration was overwritten.
	fromRegistry, _ := credential.Get(realm)
	if fromRegistry == credential.Provider(perRequest) {
		t.Fatal("the per-request provider leaked into the process registry")
	}
}

// TestReservedHeadersFollowTheSameLookup pins the call site that is easiest to get
// wrong: the redirect policy's reserved-header list must come from the same
// provider that injected the credentials.
//
// It goes through Client.reservedHeadersFor — the code the send path actually uses —
// rather than through credential.ReservedHeadersVia directly. That difference is
// load-bearing: an earlier version of this test called the helper, and so it kept
// passing when the call site was changed back to reading the process registry.
//
// The consequence of that mistake is not a wrong list, it is an EMPTY one: on a
// per-caller face the registered provider is a refusing placeholder that reserves
// nothing. An empty list reads as "this identity reserves no headers" rather than
// as a bug, and it means a cross-host redirect forwards the injected credential.
func TestReservedHeadersFollowTheSameLookup(t *testing.T) {
	const realm = credential.Realm("transport-identities-reserved")
	// The registry's provider reserves NOTHING, so the two sources are
	// distinguishable and the test cannot pass by accident.
	credential.Register(identitiesStubProvider{realm: realm})
	if viaRegistry := credential.ReservedHeaders(realm); len(viaRegistry) != 0 {
		t.Fatalf("the registry reserved %v; the test cannot distinguish the two sources", viaRegistry)
	}

	perRequest := reservingStubProvider{identitiesStubProvider{realm: realm}}
	c := New(config.Config{})
	c.Identities = &countingLookup{p: perRequest, realm: realm}

	spec := api.RequestSpec{Identity: api.Identity(realm), Signed: api.SignNone}
	got := c.reservedHeadersFor(spec)
	if len(got) != 1 || got[0] != "X-Reserved-By-Caller" {
		t.Fatalf("reservedHeadersFor = %v, want [X-Reserved-By-Caller]", got)
	}
}

// TestReservedHeadersForIncludesSigningScheme keeps the other half of that list
// from being dropped by a refactor: the signer's names must survive alongside the
// identity's, because both would otherwise leak across a redirect.
func TestReservedHeadersForIncludesSigningScheme(t *testing.T) {
	const realm = credential.Realm("transport-identities-reserved-signed")
	credential.Register(identitiesStubProvider{realm: realm})

	perRequest := reservingStubProvider{identitiesStubProvider{realm: realm}}
	c := New(config.Config{})
	c.Identities = &countingLookup{p: perRequest, realm: realm}

	// The unsigned case plus one registered scheme if this build has any. There is
	// no exported way to enumerate the registry, so the signed half is driven off
	// whichever scheme the transport's own tests already register; when none is
	// present the identity half is still asserted rather than passing vacuously.
	schemes := []api.Signing{api.SignNone, testReservedScheme}
	for _, scheme := range schemes {
		got := c.reservedHeadersFor(api.RequestSpec{Identity: api.Identity(realm), Signed: scheme})

		if !containsString(got, "X-Reserved-By-Caller") {
			t.Errorf("scheme %q: reservedHeadersFor lost the identity's header: %v", scheme, got)
		}
		for _, want := range signer.ReservedHeaders(scheme) {
			if !containsString(got, want) {
				t.Errorf("scheme %q: reservedHeadersFor lost the signing header %q: %v", scheme, want, got)
			}
		}
	}
}

// testReservedScheme is a signing scheme registered by this test file so the
// signed half of reservedHeadersFor has something real to lose.
const testReservedScheme api.Signing = "identities-test-scheme"

type reservedHeaderSigner struct{}

func (reservedHeaderSigner) Scheme() api.Signing { return testReservedScheme }
func (reservedHeaderSigner) Sign(*api.RequestSpec, credential.Credentials, time.Time) error {
	return nil
}

// ReservedHeaders is the optional capability the transport asks about; it is the
// whole reason this stub exists.
func (reservedHeaderSigner) ReservedHeaders() []string { return []string{"X-Reserved-By-Signer"} }

func init() { signer.Register(reservedHeaderSigner{}) }

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

type reservingStubProvider struct{ identitiesStubProvider }

func (reservingStubProvider) ReservedHeaders() []string { return []string{"X-Reserved-By-Caller"} }
