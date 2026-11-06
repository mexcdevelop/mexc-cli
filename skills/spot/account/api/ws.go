// WebSocket-backed operation of the spot.account subdomain: ws-deals, the
// account's own new-trade push. It lives under account, not trade: it is a
// read of "my account's own new-trade stream" — an account-scoped read, not a
// trading action.
//
// Its auth is "REST-sign to fetch a listenKey, then dial already
// authenticated" — a pre-dial exchange, not an in-connection handshake (see
// futures/account/api's ws-deals for the opposite timing). The exchange
// itself is described as data (PreflightRequest + ApplyPreflightResult) for
// internal/cli's WS execution path to actually send, using the SAME
// *internal/transport.Client every REST operation shares — this package
// never dials a raw HTTP request itself.
package accountapi

import (
	"net/url"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// spotHostKeyOpenAPI names the host key ws-deals' listenKey preflight signs
// against and sends to — the SAME key value editions/public/adapter registers
// as adapter.KeyOpenAPI. It is a literal here, not an import of that
// constant: skills/**/api is composed BY an edition (editions/public), so it
// must never import back into one (that would invert the six-layer
// dependency direction). A declared Operation never sets HostKey at all and
// inherits the subdomain default from domains.go, which is registered with
// that exact same key; a hand-built RequestSpec — a one-shot preflight call,
// not a declared Operation — has no subdomain to inherit from, so it must
// name the key directly.
const spotHostKeyOpenAPI = "openapi"

// spotWSHostKey names the host key ws-deals' WS connection routes by — the
// SAME key value editions/public/adapter registers as adapter.KeySpotWS. It
// is a literal here, not an import of that constant, for the identical
// layering reason spotHostKeyOpenAPI is a literal just above (skills/**/api
// must never import editions/). Resolving this key to an actual URL — the
// registered default, a MEXC_SPOT_WS_BASE_URL env override, or a
// --spot-ws-url per-invocation override — happens entirely in internal/cli's
// WS execution path, one layer up: this package never sees a literal WS URL
// at all, only this key name and (below) the listenKey suffix to append to
// whatever that resolution lands on.
//
// This is a DIFFERENT host key than spotHostKeyOpenAPI just above: that one
// names the REST host the listenKey preflight signs against and sends to
// (gateway-cli.mexc.com); this one names the WS gateway the connection itself
// dials (wss://wbs-api.mexc.com/ws) — two distinct legs of this one
// operation, resolved independently (--base-url/MEXC_OPENAPI_BASE_URL
// govern the former; --spot-ws-url/MEXC_SPOT_WS_BASE_URL govern the latter).
const spotWSHostKey = "spot-ws"

// listenKeyRecvWindow is the recvWindow this one-shot preflight signs with.
const listenKeyRecvWindow = "5000"

// wsOperations returns the WS-backed operations of this subdomain, which is
// everything Operations() returns.
func wsOperations() []api.Operation {
	return []api.Operation{
		{
			Name: "ws-deals",
			Kind: api.KindWSCollect,
			// Signed is explicitly none: this op never builds a RequestSpec of
			// its own (its Kind is KindWSCollect, not KindREST), so the signing
			// axis does not apply to IT — the listenKey preflight below is a
			// separate, real RequestSpec that the cli-hmac signer signs exactly
			// as it would any other operation's request; it is just sent by the
			// WS execution path instead of by this op's own command.
			Signed:  api.SignNone,
			Access:  api.Read,
			Summary: "Subscribe to the account's own new-trade pushes (WS, private, requires an API key with SPOT_ACCOUNT_READ)",
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				// --dry-run must work with no saved profile and must never make a
				// real signed call — the listenKey exchange is a genuine network
				// request, so it is skipped entirely here. WSConfig.URL is a
				// SUFFIX appended to HostKey's resolved base (never a full URL a
				// closure builds alone — see pkg/transport.WSConfig's doc
				// comment), so the plan shown here is "<resolved base>?listenKey=
				// <resolved-at-call-time>" once internal/cli's WS execution path
				// appends it.
				if dryRun {
					return &transport.WSConfig{
						HostKey:  spotWSHostKey,
						URL:      "?listenKey=<resolved-at-call-time>",
						Channels: []string{"spot@private.deals.v3.api.pb"},
						Codec:    transport.CodecSpotV3,
					}, nil
				}
				if apiKey == "" || secretKey == "" {
					return nil, errs.Validation(errs.SubtypeFailedPrecondition,
						"this operation requires API credentials").
						WithHint("ask the user to run `%s profile set <name>` in their own terminal (it prompts; "+
							"no flag, stdin or environment variable supplies a secret instead), then `%s profile use <name>`",
							cliname.Name(), cliname.Name())
				}
				spec := api.RequestSpec{
					Method:   "POST",
					Path:     "/api/v3/listen-key/userDataStream",
					Query:    map[string]string{"recvWindow": listenKeyRecvWindow},
					HostKey:  spotHostKeyOpenAPI,
					Signed:   signing.CliHMAC,
					Identity: identity.OpenAPI,
				}
				return &transport.WSConfig{
					HostKey:          spotWSHostKey,
					Channels:         []string{"spot@private.deals.v3.api.pb"},
					Codec:            transport.CodecSpotV3,
					PreflightRequest: &spec,
					ApplyPreflightResult: func(dst any) (string, error) {
						m, ok := dst.(map[string]any)
						if !ok {
							return "", errs.Internal("listenKey response has unexpected shape %T", dst)
						}
						listenKey, _ := m["listenKey"].(string)
						if listenKey == "" {
							return "", errs.API(0, errs.SubtypeUpstreamError, "listenKey response carried no listenKey")
						}
						// A SUFFIX only — internal/cli's WS execution path appends
						// this to HostKey's resolved base; this closure never
						// learns that base itself (see WSConfig's doc comment).
						return "?listenKey=" + url.QueryEscape(listenKey), nil
					},
				}, nil
			},
		},
	}
}
