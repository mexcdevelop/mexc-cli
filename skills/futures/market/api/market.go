// Package marketapi declares the operations of the futures.market subdomain
// (public contract market data via WebSocket).
//
// Unlike spot.market, this subdomain carries NO REST operations: futures
// REST (its signature rule is entirely different from spot's — accessKey +
// timestamp + params via request headers, not query-string HMAC) is a
// separate, larger topic and out of scope here. Every operation is Kind:
// api.KindWSCollect (a long-lived subscription), not Method/Path.
package marketapi

import (
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// futuresWSHostKey names the host key every operation in this file's
// WSConfig.HostKey routes by — the SAME key value editions/public/adapter
// registers as adapter.KeyFuturesWS. It is a literal here, not an import of
// that constant — skills/**/api must never import editions/ (the same
// precedent spotHostKeyOpenAPI sets for adapter.KeyOpenAPI). Resolving this key to an actual URL — the registered
// default, a MEXC_FUTURES_WS_BASE_URL env override, or a --futures-ws-url
// per-invocation override — happens entirely in internal/cli's WS execution
// path; this package never sees a literal URL at all.
const futuresWSHostKey = "futures-ws"

// Operations returns every operation of this subdomain. The framework
// assembles commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "ws-ticker",
			Kind: api.KindWSCollect,
			// --symbol is optional here, unlike ws-depth/ws-trades below:
			// omitting it subscribes to sub.tickers (plural, whole-market,
			// no param — one push.tickers frame with an array of every
			// contract, ~2s cadence); passing it subscribes to sub.ticker
			// (singular, one contract — a push.ticker frame with a single
			// object, ~1s cadence). The exchange docs list these as two
			// separate pages ("Tickers" vs "Get a single ticker"); this op
			// picks between them by presence of --symbol rather than
			// forcing a second command, since both push the same ticker
			// fields and differ only in scope + cardinality.
			Access:  api.Read,
			Summary: "Subscribe to real-time ticker pushes: whole-market if --symbol is omitted, one contract if given (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract symbol, e.g. BTC_USDT; omit to subscribe to the whole market"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				channel := "sub.tickers"
				if symbol := values["symbol"]; symbol != "" {
					channel = "sub.ticker:" + symbol
				}
				return &transport.WSConfig{
					HostKey:  futuresWSHostKey,
					Channels: []string{channel},
					Codec:    transport.CodecFuturesEdge,
				}, nil
			},
		},
		{
			Name:    "ws-depth",
			Kind:    api.KindWSCollect,
			Access:  api.Read,
			Summary: "Subscribe to real-time order-book depth pushes for one contract (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Required: true, Desc: "Contract symbol, e.g. BTC_USDT"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				return &transport.WSConfig{
					HostKey:  futuresWSHostKey,
					Channels: []string{"sub.depth:" + values["symbol"]},
					Codec:    transport.CodecFuturesEdge,
				}, nil
			},
		},
		{
			Name:    "ws-trades",
			Kind:    api.KindWSCollect,
			Access:  api.Read,
			Summary: "Subscribe to real-time trade (new-deal) pushes for one contract (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Required: true, Desc: "Contract symbol, e.g. BTC_USDT"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				return &transport.WSConfig{
					HostKey:  futuresWSHostKey,
					Channels: []string{"sub.deal:" + values["symbol"]},
					Codec:    transport.CodecFuturesEdge,
				}, nil
			},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command
// assembly and the contract test (must match the SKILL.md frontmatter).
const Scope = "futures.market"

// Signed is this subdomain's default signing scheme: futures market data is
// public, entirely unsigned. Operations inherit it (they omit the Signed
// field); an operation overrides only with a concrete api.Sign* constant.
const Signed = api.SignNone

// Identity is this subdomain's default login identity: none — public data
// needs no session. Operations inherit it the same way as Signed.
const Identity = api.IdentityNone
