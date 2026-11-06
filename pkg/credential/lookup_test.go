package credential

import (
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// lookupStubProvider is a minimal Provider for the lookup tests. It deliberately
// does NOT implement HeaderReserver; the one that does is below.
type lookupStubProvider struct{ realm Realm }

func (p lookupStubProvider) Realm() Realm { return p.realm }
func (lookupStubProvider) Resolve(config.Config, hosts.Env) (Credentials, error) {
	return Credentials{}, nil
}
func (lookupStubProvider) Inject(*api.RequestSpec, Credentials) error { return nil }

type lookupReservingProvider struct{ lookupStubProvider }

func (lookupReservingProvider) ReservedHeaders() []string { return []string{"X-Stub-Key"} }

// fixedLookup answers with one provider for one realm and nothing else — the
// shape a per-request Lookup has.
type fixedLookup struct {
	realm Realm
	p     Provider
}

func (l fixedLookup) Provider(r Realm) (Provider, bool) {
	if r == l.realm {
		return l.p, true
	}
	return nil, false
}

// TestRegistryLookupMatchesGet pins the equivalence the whole change rests on:
// the default Lookup and the package-level Get must be the same answer, so a
// zero-value Client behaves exactly as it did before Lookup existed.
func TestRegistryLookupMatchesGet(t *testing.T) {
	const realm = Realm("lookup-test-registry")
	Register(lookupStubProvider{realm: realm})

	fromGet, okGet := Get(realm)
	fromLookup, okLookup := Registry().Provider(realm)
	if okGet != okLookup || fromGet != fromLookup {
		t.Fatalf("Registry().Provider disagreed with Get: (%v,%v) vs (%v,%v)",
			fromGet, okGet, fromLookup, okLookup)
	}
}

// TestLookupMissReportsSameSemantics is the substitutability requirement: a miss
// must look identical whichever Lookup answered it. If one returned a zero
// provider with ok=true, or panicked, the two would stop being interchangeable.
func TestLookupMissReportsSameSemantics(t *testing.T) {
	const absent = Realm("lookup-test-absent")

	for name, l := range map[string]Lookup{
		"process": Registry(),
		"fixed":   fixedLookup{realm: "something-else", p: lookupStubProvider{}},
	} {
		t.Run(name, func(t *testing.T) {
			p, ok := l.Provider(absent)
			if ok {
				t.Fatal("a missing realm reported ok")
			}
			if p != nil {
				t.Fatalf("a missing realm returned a non-nil provider: %v", p)
			}
		})
	}
}

// TestReservedHeadersViaAsksTheGivenLookup is the reason ReservedHeadersVia
// exists. Asking the process registry while a per-request Lookup is in force
// would answer from the wrong provider — and on a face whose registered provider
// is a refusing placeholder, that wrong answer is an empty list, which looks like
// "this identity reserves nothing" rather than like a bug.
func TestReservedHeadersViaAsksTheGivenLookup(t *testing.T) {
	const realm = Realm("lookup-test-reserved")
	// The process registry holds a provider that reserves NOTHING for this realm.
	Register(lookupStubProvider{realm: realm})

	if got := ReservedHeaders(realm); len(got) != 0 {
		t.Fatalf("process registry unexpectedly reserved %v", got)
	}

	perRequest := fixedLookup{realm: realm, p: lookupReservingProvider{lookupStubProvider{realm: realm}}}
	got := ReservedHeadersVia(perRequest, realm)
	if len(got) != 1 || got[0] != "X-Stub-Key" {
		t.Fatalf("ReservedHeadersVia returned %v, want [X-Stub-Key]", got)
	}
}

// TestReservedHeadersViaNilLookupIsTheRegistry keeps the zero value meaning
// "process-wide", matching every other nil-Lookup path in this module.
func TestReservedHeadersViaNilLookupIsTheRegistry(t *testing.T) {
	const realm = Realm("lookup-test-nil")
	Register(lookupReservingProvider{lookupStubProvider{realm: realm}})

	got := ReservedHeadersVia(nil, realm)
	want := ReservedHeaders(realm)
	if len(got) != len(want) || (len(got) == 1 && got[0] != want[0]) {
		t.Fatalf("nil Lookup returned %v, want the registry's %v", got, want)
	}
}
