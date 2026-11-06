package adapter

import "github.com/mexcdevelop/mexc-cli/pkg/hosts"

// KeyOpenAPI is this product's OpenAPI host key: the name its operations route
// by and the key its rows register under. It lives here, with the rows, for the
// same reason the scheme and identity constants live in signing/ and identity/ —
// a host key is a product's vocabulary, and pkg/hosts carries the resolution
// logic without knowing any product's names.
const KeyOpenAPI = "openapi"

// DefaultBaseURL is the production endpoint of the MEXC spot OpenAPI. It lives
// with the host row that uses it rather than in the shared config: another
// product's endpoints are its own, and the shared layer resolves a host through
// the table, never by name.
const DefaultBaseURL = "https://gateway-cli.mexc.com"

// KeySpotWS is this product's spot WebSocket host key — the name every spot
// WS operation's WSConfig.HostKey routes by (public channels: ws-ticker /
// ws-depth / ws-trades; the private channel, ws-deals, additionally appends
// "?listenKey=..." to whatever this key resolves to, once its listenKey
// preflight succeeds; see skills/spot/account/api/ws.go). It lives here,
// with the row that uses it, for the same reason KeyOpenAPI does: a host key
// is this product's vocabulary, and pkg/hosts carries the resolution logic
// without knowing any product's names.
const KeySpotWS = "spot-ws"

// KeyFuturesWS is this product's futures (contract) WebSocket host key —
// shared by every futures WS operation, public and private alike (unlike
// spot, futures' private channel authenticates by an in-connection login
// frame sent after dial, never a URL query parameter; see
// skills/futures/account/api/account.go).
const KeyFuturesWS = "futures-ws"

// DefaultSpotWSURL is the production Spot WebSocket gateway's endpoint — the
// value SpotWSRows registers KeySpotWS's "prod" row with.
const DefaultSpotWSURL = "wss://wbs-api.mexc.com/ws"

// DefaultFuturesWSURL is the production Futures WebSocket gateway's
// endpoint — the value FuturesWSRows registers KeyFuturesWS's "prod" row
// with.
const DefaultFuturesWSURL = "wss://contract.mexc.com/edge"

// SpotWSRows and FuturesWSRows are the public WS gateways, injected into the
// SAME host table OpenAPIRows uses, by the same public edition (edition.go).
// A WS-backed Operation's WSCollect closure never sets a literal URL — only
// WSConfig.HostKey (KeySpotWS / KeyFuturesWS) — and internal/cli's WS
// execution path resolves it through pkg/hosts.Resolve exactly like a REST
// RequestSpec.HostKey, which is what makes these two rows behave like
// OpenAPIRows' in every way that matters to a caller: a single "prod" row
// (see OpenAPIRows' doc comment for why one env, deliberately — the same
// reasoning applies verbatim here, and additionally MEXC publishes no
// testnet WS gateway at all, so there is not even a hypothetical second row
// to add), --base-url style per-invocation pinning (this product's is
// --spot-ws-url / --futures-ws-url, since --base-url itself is a single,
// unkeyed override and two WS gateways coexist in the registry), and a
// MEXC_<KEY>_BASE_URL persistent override (MEXC_SPOT_WS_BASE_URL /
// MEXC_FUTURES_WS_BASE_URL) for free from pkg/hosts.Resolve's existing
// per-key env-var convention.
func SpotWSRows() hosts.Registration {
	return hosts.Registration{
		Key:  KeySpotWS,
		Rows: map[hosts.Env]hosts.Endpoint{"prod": {URL: DefaultSpotWSURL}},
	}
}

func FuturesWSRows() hosts.Registration {
	return hosts.Registration{
		Key:  KeyFuturesWS,
		Rows: map[hosts.Env]hosts.Endpoint{"prod": {URL: DefaultFuturesWSURL}},
	}
}

// OpenAPIRows is the public OpenAPI host, injected into the host table by
// the public edition (edition.go) rather than living as a built-in row in
// pkg/hosts — so the shared host package ships no concrete host and the public
// binary is the only one that carries this endpoint.
//
// ONE env, deliberately. A non-production endpoint registered here would publish
// a hostname that is not part of the public product, and it would not stay in the
// source: "host %q has no endpoint for env %q" lists every env the host serves,
// so the name reached anyone who mistyped --env.
//
// With a single env there is nothing to select, and the composition root derives
// that from this table — framework.assemble drops --env, MEXC_ENV and the `env`
// setting when a product serves only one. Adding a second row brings all three
// back with no other wiring, but it is not a free edit: two contract tests assert
// this shape on purpose (TestHostRows_ProductionOnly is the security gate that
// keeps a non-production hostname out of a published binary), and both must be
// answered rather than deleted.
//
// Pointing the CLI elsewhere during development is --base-url for one invocation
// or MEXC_OPENAPI_BASE_URL to persist it. Those are the right tool regardless:
// env chooses among the deployments a product ships, while these two say "ignore
// the table, go here".
//
// The row is named "prod" rather than left blank so production is stated rather
// than implied — Resolve substitutes Edition.DefaultEnv for an unselected env, and
// an agent reading the table sees what it will actually hit.
func OpenAPIRows() hosts.Registration {
	return hosts.Registration{
		Key:  KeyOpenAPI,
		Rows: map[hosts.Env]hosts.Endpoint{"prod": {URL: DefaultBaseURL}},
	}
}
