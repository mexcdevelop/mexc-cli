package cli

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// domains is the topology the engine's face and contract tests mount.
//
// Everything here is a FIXTURE. The names are deliberately ones no product
// uses — a domain called sample, paths under /fixture — because these
// declarations used to be borrowed from real subdomains, and when those were
// retired the engine's tests broke although the engine had not changed. Worse,
// the borrowed names outlived the subdomains and read, to anyone searching the
// tree afterwards, like a deletion that had not finished.
//
// So: shared machinery must not know which business subdomains a product ships
// this month, and must not borrow their vocabulary either.
//
// Each operation exists to give some engine test a shape to exercise — an
// unsigned GET with two parameters, a closed enum, a signed read, a write whose
// flag derives from access, a subdomain mixing reads and writes for --read-only.
// The shapes are the contract; the paths are never called.
//
// Production never reads this. The engine takes Factory.Domains, injected by an
// edition.
func domains() []api.DomainSpec {
	return []api.DomainSpec{
		{
			Name:  "sample",
			Short: "Fixture domain: request shapes the engine tests mount",
			Subs: []api.Subdomain{
				{
					Name: "public", Short: "Unsigned reads",
					Signed: api.SignNone, Identity: api.IdentityNone, HostKey: testHostKey,
					Ops: []api.Operation{
						{
							Name: "ticker", Method: "GET", Path: "/fixture/ticker",
							Access: api.Read, Summary: "One required parameter",
							Params: []api.Param{{Name: "symbol", Desc: "Instrument", Required: true}},
						},
						{
							Name: "depth", Method: "GET", Path: "/fixture/depth",
							Access: api.Read, Summary: "One required and one optional parameter",
							Params: []api.Param{
								{Name: "symbol", Desc: "Instrument", Required: true},
								{Name: "limit", Desc: "Row count"},
							},
						},
						{
							Name: "candles", Method: "GET", Path: "/fixture/candles",
							Access: api.Read, Summary: "A closed enum parameter",
							Params: []api.Param{
								{Name: "symbol", Desc: "Instrument", Required: true},
								// The enum deliberately omits 1h: a test passes exactly
								// that value to prove a rejection names the right param.
								{Name: "interval", Desc: "Period", Required: true,
									Enum: []string{"1m", "5m", "15m", "60m", "1d"}},
							},
						},
					},
				},
				{
					Name: "signed", Short: "Signed read and write",
					Signed: testQueryScheme, Identity: identity.OpenAPI, HostKey: testHostKey,
					Ops: []api.Operation{
						{
							Name: "fee", Method: "GET", Path: "/fixture/fee",
							Access: api.Read, Summary: "A signed read",
							Params: []api.Param{{Name: "symbol", Desc: "Instrument", Required: true}},
						},
						{
							Name: "toggle", Method: "POST", Path: "/fixture/toggle",
							Access: api.Write, Summary: "A write, so the derived write flag has a subject",
							Params: []api.Param{{Name: "enable", Desc: "true or false", Required: true}},
						},
					},
				},
				{
					Name: "orders", Short: "Signed subdomain mixing reads and writes",
					Signed: testQueryScheme, Identity: identity.OpenAPI, HostKey: testHostKey,
					Ops: []api.Operation{
						{
							Name: "list", Method: "GET", Path: "/fixture/orders",
							Access: api.Read, Summary: "A signed read beside a write",
							Params: []api.Param{{Name: "symbol", Desc: "Instrument", Required: true}},
						},
						{
							Name: "create", Method: "POST", Path: "/fixture/orders",
							Access: api.Write, Summary: "The write --read-only must refuse",
							Params: []api.Param{
								{Name: "symbol", Desc: "Instrument", Required: true},
								{Name: "side", Desc: "Direction", Required: true},
								{Name: "quantity", Desc: "Amount", Required: true},
							},
						},
					},
				},
				{
					Name: "stream", Short: "A WS-backed subscription beside signed reads",
					Signed: testQueryScheme, Identity: identity.OpenAPI, HostKey: testHostKey,
					Ops: []api.Operation{
						{
							Name: "ws-ticker",
							Kind: api.KindWSCollect,
							// Both axes overridden off the subdomain's defaults, which
							// is the shape a public channel under a signed subdomain
							// takes: the flag tests reach the WS path with no
							// credentials resolved.
							Signed: api.SignNone, Identity: api.IdentityNone,
							Access: api.Read, Summary: "A subscription, so the WS flags have a subject",
							Params: []api.Param{{Name: "symbol", Desc: "Instrument", Required: true}},
							// No Codec: every test that reaches here passes --dry-run,
							// which renders the plan and returns before dialling. A
							// fixture that could dial would be a fixture that can
							// reach the network. HostKey reuses testHostKey (the
							// same key the REST fixtures above register), resolved
							// via the ordinary pkg/hosts path — a WS fixture needs
							// no dedicated host key of its own since dry-run never
							// actually dials it.
							WSCollect: func(values map[string]string, _, _ string, _ bool) (any, error) {
								return &transport.WSConfig{
									HostKey:  testHostKey,
									Channels: []string{"fixture.ticker@" + values["symbol"]},
								}, nil
							},
						},
					},
				},
			},
		},
	}
}

// testOpenAPI is a minimal openapi identity double (the real one lives in
// editions/public). It mirrors the api-key header injection + reserved header so
// the engine's assembly-time validation and dry-run reachability behave as in
// production.
// testOpenAPI is the engine's stand-in for an api-key identity. It holds its own
// credentials rather than reading them off Config, mirroring the production
// shape: a provider resolves credentials from wherever ITS product keeps them,
// and Config carries none. Tests flip testOpenAPICredentials to move between
// "no credentials" and "credentials present".
type testOpenAPI struct{}

// testOpenAPICredentials is what testOpenAPI resolves; empty means the identity
// has none, which the write-confirmation precheck must treat as a hard stop.
var testOpenAPICredentials credential.Credentials

const testAPIKeyHeader = "X-MEXC-APIKEY"

func (testOpenAPI) Realm() credential.Realm { return credential.Realm(identity.OpenAPI) }
func (testOpenAPI) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	// Mirror the real provider: no api credentials → failed_precondition, so the
	// write-confirmation precheck (precheckWriteCredentials) behaves as in production.
	if testOpenAPICredentials.APIKey == "" || testOpenAPICredentials.Secret == "" {
		return credential.Credentials{}, errs.Validation(errs.SubtypeFailedPrecondition,
			"this operation requires API credentials")
	}
	return testOpenAPICredentials, nil
}
func (testOpenAPI) ReservedHeaders() []string { return []string{testAPIKeyHeader} }
func (testOpenAPI) Inject(spec *api.RequestSpec, cred credential.Credentials) error {
	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header[testAPIKeyHeader] = cred.APIKey
	return nil
}

// testQueryScheme is a scheme name this package invents for its own signer
// double below, never a product's.
const testQueryScheme api.Signing = "test-query-hmac"

// testQuerySigner is a minimal query-signing double (a real one lives in
// editions/public). It reserves the same query params and performs a real HMAC so
// the engine's validation and any non-dry-run path behave as in production.
type testQuerySigner struct{}

func (testQuerySigner) Scheme() api.Signing { return testQueryScheme }

// testHMACHex mirrors what a query-signing scheme does, so this package can
// exercise the signing hook without importing a product's scheme.
func testHMACHex(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (testQuerySigner) ReservedParams() []string { return []string{"timestamp", "signature"} }
func (testQuerySigner) Sign(spec *api.RequestSpec, cred credential.Credentials, now time.Time) error {
	if spec.Query == nil {
		spec.Query = map[string]string{}
	}
	spec.Query["timestamp"] = strconv.FormatInt(now.UnixMilli(), 10)
	spec.Query["signature"] = testHMACHex(cred.Secret, api.CanonicalQuery(spec.Query))
	return nil
}

// stubWSCollector stands in for the WS runner; every WS case here is a dry-run,
// which returns before the collector is called.
type stubWSCollector struct{}

func (stubWSCollector) Collect(context.Context, transport.WSConfig) error { return nil }
