// WebSocket-backed operations of the spot.marketv2 subdomain: ws-ticker /
// ws-depth / ws-trades. Unlike the REST operations in marketv2.go, these are
// Kind: api.KindWSCollect (a long-lived subscription) rather than
// Method/Path: the execution model (bounded vs unbounded, JSON Lines
// streaming, no auto-reconnect) is documented once, generically, in the
// mexc-ws skill, not repeated per operation.
//
// All three are public channels (no credentials): the exchange's spot v3
// public WS gateway needs no signature or login at all, unlike this
// subdomain's REST operations (which sign against the CLI gateway).
// So every op here explicitly overrides Signed/Identity to none rather than
// inheriting marketv2's subdomain defaults (signing.CliHMAC /
// identity.OpenAPI) — an intentional, per-op opt-out, not an omission. Their WSCollect closures ignore the apiKey/secretKey
// arguments entirely for the same reason.
package marketv2api

import (
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// spotWSHostKey names the host key these three operations' WSConfig.HostKey
// routes by — the SAME key value editions/public/adapter registers as
// adapter.KeySpotWS. It is a literal here, not an import of that constant:
// skills/**/api is composed BY an edition (editions/public), so it must
// never import back into one (that would invert the six-layer dependency
// direction) — the same precedent
// skills/spot/account/api/ws.go's spotHostKeyOpenAPI already sets for
// adapter.KeyOpenAPI. Resolving this key to an actual URL — the registered
// default, a MEXC_SPOT_WS_BASE_URL env override, or a --spot-ws-url
// per-invocation override — happens entirely in internal/cli's WS execution
// path; this package never sees a literal URL at all.
const spotWSHostKey = "spot-ws"

// spotAggreInterval is the aggregation window used for every public channel
// this subdomain subscribes to. The non-aggregated channel variants are
// rejected by the server, so only the aggregated form is ever used.
const spotAggreInterval = "100ms"

// wsOperations returns the WS-backed operations of this subdomain. Called by
// Operations() alongside the REST ones, so both live under one subdomain
// (spot.marketv2): WS is just another way to reach the same resource, not a
// new scope.
func wsOperations() []api.Operation {
	return []api.Operation{
		{
			Name:     "ws-ticker",
			Kind:     api.KindWSCollect,
			Signed:   api.SignNone,
			Identity: api.IdentityNone,
			Access:   api.Read,
			Summary:  "Subscribe to real-time best bid/ask ticker pushes for a symbol (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Required: true, Desc: "Trading pair, e.g. BTCUSDT"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				symbol := values["symbol"]
				return &transport.WSConfig{
					HostKey:  spotWSHostKey,
					Channels: []string{"spot@public.aggre.bookTicker.v3.api.pb@" + spotAggreInterval + "@" + symbol},
					Codec:    transport.CodecSpotV3,
				}, nil
			},
		},
		{
			Name:     "ws-depth",
			Kind:     api.KindWSCollect,
			Signed:   api.SignNone,
			Identity: api.IdentityNone,
			Access:   api.Read,
			Summary:  "Subscribe to real-time incremental order-book depth pushes for a symbol (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Required: true, Desc: "Trading pair, e.g. BTCUSDT"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				symbol := values["symbol"]
				return &transport.WSConfig{
					HostKey:  spotWSHostKey,
					Channels: []string{"spot@public.aggre.depth.v3.api.pb@" + spotAggreInterval + "@" + symbol},
					Codec:    transport.CodecSpotV3,
				}, nil
			},
		},
		{
			Name:     "ws-trades",
			Kind:     api.KindWSCollect,
			Signed:   api.SignNone,
			Identity: api.IdentityNone,
			Access:   api.Read,
			Summary:  "Subscribe to real-time trade (new-deal) pushes for a symbol (WS, public, no auth)",
			Params: []api.Param{
				{Name: "symbol", Required: true, Desc: "Trading pair, e.g. BTCUSDT"},
			},
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				symbol := values["symbol"]
				return &transport.WSConfig{
					HostKey:  spotWSHostKey,
					Channels: []string{"spot@public.aggre.deals.v3.api.pb@" + spotAggreInterval + "@" + symbol},
					Codec:    transport.CodecSpotV3,
				}, nil
			},
		},
	}
}
