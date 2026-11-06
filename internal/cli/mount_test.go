package cli

import (
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// testPingOp is a minimal but well-formed Operation: just enough for
// ValidateDecl to pass (Name/Method/Path/Access/Summary set, no params, no
// body), so these tests exercise only the hostKey rule in validateRegistry,
// not the unrelated declaration checks.
func testPingOp() api.Operation {
	return api.Operation{
		Name:    "ping",
		Method:  "GET",
		Path:    "/api/v3/ping",
		Access:  api.Read,
		Summary: "ping",
	}
}

// TestValidateRegistry_MissingHostKey asserts that a subdomain with no
// concrete host key — neither a subdomain default nor an op-level override —
// fails assembly-time validation with a structured error naming the gap. This
// is the mandatory-hostKey rule added alongside effectiveHostKey.
func TestValidateRegistry_MissingHostKey(t *testing.T) {
	ds := []api.DomainSpec{
		{
			Name: "testdomain",
			Subs: []api.Subdomain{
				{
					Name:     "testsub",
					Signed:   api.SignNone,
					Identity: api.IdentityNone,
					// HostKey deliberately left empty.
					Ops: []api.Operation{testPingOp()},
				},
			},
		},
	}
	err := validateRegistry(ds)
	if err == nil {
		t.Fatal("expected an error for a subdomain with no host key, got nil")
	}
	if !strings.Contains(err.Error(), "host key") {
		t.Fatalf("expected error to mention host key, got: %v", err)
	}
}

// TestValidateRegistry_SignedBodyAllowed pins that assembly does NOT judge the
// pairing of a signing scheme with a request body. It once did: a scheme signing
// the query only was refused a body here, which forced an op that carries one to
// be declared unsigned until a body-covering scheme existed. How much of a
// request a signature covers is now the scheme's own business, decided inside
// Sign, so an op can be declared in its final shape today. Whether the far side
// accepts that signature is answered by the far side.
func TestValidateRegistry_SignedBodyAllowed(t *testing.T) {
	type reqBody struct {
		Note string `json:"note"`
	}
	op := api.Operation{
		Name: "place", Method: "POST", Path: "/api/v3/order",
		Access: api.Write, Summary: "place order", Body: reqBody{},
	}
	ds := []api.DomainSpec{{
		Name: "spot",
		Subs: []api.Subdomain{{
			Name:     "trade",
			Signed:   testQueryScheme,
			Identity: identity.OpenAPI,
			HostKey:  testHostKey,
			Ops:      []api.Operation{op},
		}},
	}}
	if err := validateRegistry(ds); err != nil {
		t.Fatalf("a signed op carrying a body must assemble, got: %v", err)
	}
}

// TestValidateRegistry_UnsignedBodyAllowed is the counterpart: the same body op
// under an UNSIGNED subdomain is well-formed too. It guards the other direction
// — that dropping the signed+body rule did not leave a rule keyed on the body
// alone.
func TestValidateRegistry_UnsignedBodyAllowed(t *testing.T) {
	type reqBody struct {
		Note string `json:"note"`
	}
	op := api.Operation{
		Name: "save", Method: "POST", Path: "/api/save",
		Access: api.Write, Summary: "save", Body: reqBody{},
	}
	ds := []api.DomainSpec{{
		Name: "example",
		Subs: []api.Subdomain{{
			Name:     "tag",
			Signed:   api.SignNone,
			Identity: api.IdentityNone,
			HostKey:  testHostKey,
			Ops:      []api.Operation{op},
		}},
	}}
	if err := validateRegistry(ds); err != nil {
		t.Fatalf("an unsigned body op must be allowed, got: %v", err)
	}
}

// regWith wraps one op in a single-subdomain registry with the given auth axes.
func regWith(op api.Operation, signed api.Signing, identity api.Identity) []api.DomainSpec {
	return []api.DomainSpec{{
		Name: "d",
		Subs: []api.Subdomain{{
			Name: "s", Signed: signed, Identity: identity, HostKey: "openapi",
			Ops: []api.Operation{op},
		}},
	}}
}

// TestValidateRegistry_ReservedSigningParamRejected: a query param colliding with
// a name the signer injects (signature) is silently overwritten at signing, so
// it must be rejected at assembly time.
func TestValidateRegistry_ReservedSigningParamRejected(t *testing.T) {
	op := api.Operation{
		Name: "q", Method: "GET", Path: "/api/v3/q", Access: api.Read, Summary: "q",
		Params: []api.Param{{Name: "signature", Desc: "x"}},
	}
	err := validateRegistry(regWith(op, testQueryScheme, identity.OpenAPI))
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("a param colliding with an injected signing param must be rejected, got: %v", err)
	}
}

// TestValidateRegistry_ReservedRecvWindowAllowed: recvWindow is deliberately
// user-overridable (the signer only fills it when absent), so declaring it must
// NOT be rejected — it is not a reserved name.
func TestValidateRegistry_ReservedRecvWindowAllowed(t *testing.T) {
	op := api.Operation{
		Name: "q", Method: "GET", Path: "/api/v3/q", Access: api.Read, Summary: "q",
		Params: []api.Param{{Name: "recvWindow", Desc: "validity window"}},
	}
	if err := validateRegistry(regWith(op, testQueryScheme, identity.OpenAPI)); err != nil {
		t.Fatalf("recvWindow is user-overridable and must be allowed, got: %v", err)
	}
}

// TestValidateRegistry_ReservedAPIKeyHeaderRejected: a header param colliding
// with the identity-injected api-key header must be rejected (case-insensitive).
func TestValidateRegistry_ReservedAPIKeyHeaderRejected(t *testing.T) {
	op := api.Operation{
		Name: "q", Method: "GET", Path: "/api/v3/q", Access: api.Read, Summary: "q",
		Params: []api.Param{{Name: "x-mexc-apikey", In: api.InHeader, Desc: "x"}},
	}
	err := validateRegistry(regWith(op, api.SignNone, identity.OpenAPI))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "apikey") {
		t.Fatalf("a header param colliding with the api-key header must be rejected, got: %v", err)
	}
}

// TestValidateRegistry_ReservedContentTypeRejected: Content-Type is framework-
// owned (set from the body kind), so a business header param must not claim it.
func TestValidateRegistry_ReservedContentTypeRejected(t *testing.T) {
	op := api.Operation{
		Name: "q", Method: "GET", Path: "/api/v3/q", Access: api.Read, Summary: "q",
		Params: []api.Param{{Name: "Content-Type", In: api.InHeader, Desc: "x"}},
	}
	err := validateRegistry(regWith(op, api.SignNone, api.IdentityNone))
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Fatalf("a Content-Type header param must be rejected, got: %v", err)
	}
}

// TestValidateRegistry_HostKeySet asserts the positive case: the same
// subdomain, with a concrete hostKey wired, passes validation.
func TestValidateRegistry_HostKeySet(t *testing.T) {
	ds := []api.DomainSpec{
		{
			Name: "testdomain",
			Subs: []api.Subdomain{
				{
					Name:     "testsub",
					Signed:   api.SignNone,
					Identity: api.IdentityNone,
					HostKey:  testHostKey,
					Ops:      []api.Operation{testPingOp()},
				},
			},
		},
	}
	if err := validateRegistry(ds); err != nil {
		t.Fatalf("expected nil error with hostKey set, got: %v", err)
	}
}

// --- Permission requires an identity whose provider carries RBAC grants ---
//
// The rule is deliberately expressed as a provider CAPABILITY, not a comparison
// against a known identity value: this layer must not enumerate the identities
// that exist, because only the edition injecting one does.

type capProvider struct {
	realm credential.Realm
}

func (p capProvider) Realm() credential.Realm { return p.realm }
func (capProvider) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{}, nil
}
func (capProvider) Inject(*api.RequestSpec, credential.Credentials) error { return nil }

// withGrants is the same provider plus the GrantCarrier marker. Two types rather
// than a field, because the capability is carried by the TYPE — that is what
// credential.CarriesGrants asserts.
type withGrants struct{ capProvider }

func (withGrants) CarriesGrants() {}

// TestCarriesGrants covers the capability lookup itself over the three states a
// realm can be in. Synthetic realms keep it independent of which identities the
// enum happens to define.
func TestCarriesGrants(t *testing.T) {
	credential.Register(withGrants{capProvider{realm: "cap-yes"}})
	credential.Register(capProvider{realm: "cap-no"})

	if !credential.CarriesGrants("cap-yes") {
		t.Error("a provider implementing GrantCarrier must report true")
	}
	if credential.CarriesGrants("cap-no") {
		t.Error("a provider without the capability must report false")
	}
	// No provider means nothing can carry grants — the rule must reject rather
	// than assume, so an unregistered realm is false, not an error or a default.
	if credential.CarriesGrants("cap-unregistered") {
		t.Error("an unregistered realm must report false")
	}
}

// TestValidateRegistry_PermissionNeedsGrantCarrier is the integration half: an
// operation declaring Permission is refused at assembly time when its effective
// identity's provider carries no grant map. The openapi identity registered by
// TestMain is exactly such a provider, so it doubles as the negative case.
//
// The positive case is not asserted here: no identity in THIS product carries
// grants, which is the point — a product that has one proves the branch, and
// TestCarriesGrants above pins the predicate this rule is built on.
func TestValidateRegistry_PermissionNeedsGrantCarrier(t *testing.T) {
	op := testPingOp()
	op.Permission = &api.PermReq{System: "example-admin", Codes: []string{"a:b"}}

	err := validateRegistry(regWith(op, api.SignNone, identity.OpenAPI))
	if err == nil {
		t.Fatal("Permission on an identity that carries no grants must be rejected")
	}
	if !strings.Contains(err.Error(), "does not carry RBAC grants") {
		t.Fatalf("error should name the missing capability, got: %v", err)
	}
}

// TestValidateRegistry_IdentityNeedsProvider is where the identity-validity
// check now lives. pkg/api used to carry a closed list of identity values and
// reject anything outside it at declaration time; the list is gone, because the
// shared core must not catalogue the identities products define.
//
// The replacement is stricter, not weaker: a value in a list could still have no
// provider behind it and would fail only at the first real call, whereas this
// refuses to assemble at all. IdentityNone stays exempt — "no identity" has
// nothing to resolve.
func TestValidateRegistry_IdentityNeedsProvider(t *testing.T) {
	err := validateRegistry(regWith(testPingOp(), api.SignNone, "no-such-identity"))
	if err == nil {
		t.Fatal("an identity with no registered provider must be rejected")
	}
	if !strings.Contains(err.Error(), "no provider is registered") {
		t.Fatalf("error should say the provider is missing, got: %v", err)
	}

	// IdentityNone needs no provider.
	if err := validateRegistry(regWith(testPingOp(), api.SignNone, api.IdentityNone)); err != nil {
		t.Fatalf("IdentityNone must not require a provider, got: %v", err)
	}
}
