// Package accountapi declares the operations of the futures.account
// subdomain (the account's own private pushes via WebSocket).
//
// Unlike spot.account, this subdomain carries NO REST operations — futures
// REST is out of scope (see futures/market/api's doc comment). Its one
// operation, ws-deals, authenticates with an IN-CONNECTION login handshake
// rather than spot's "REST-sign to fetch a listenKey" — a fundamentally
// different auth timing, which is why this operation sets LoginFrame (spot
// never does).
package accountapi

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// futuresWSHostKey matches futures/market/api's constant of the same name
// and value — duplicated rather than shared across subdomain packages, the
// same way spot's REST HostKey literals are two separate constants across
// subdomain packages: a subdomain's api package is meant to stand alone.
// Resolving this key to an actual URL — the registered default, a
// MEXC_FUTURES_WS_BASE_URL env override, or a --futures-ws-url
// per-invocation override — happens entirely in internal/cli's WS execution
// path, so the in-connection login below authenticates against whichever
// endpoint that resolution lands on (production by default).
//
// This is a literal duplicate of editions/public/adapter.KeyFuturesWS, not
// an import of it — skills/**/api must never import editions/, which would
// invert the dependency direction.
const futuresWSHostKey = "futures-ws"

// Operations returns every operation of this subdomain. The framework
// assembles commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "ws-deals",
			Kind: api.KindWSCollect,
			// Signed is explicitly none, the same rationale as spot's ws-deals:
			// this op never builds a RequestSpec. Its login signature
			// (accessKey+reqTime, computed inline below) is a third, WS-only
			// signing rule, distinct from both cli-hmac and a hypothetical
			// futures REST scheme — it is never registered as a signer.Signer,
			// so it has no home in the Signed axis at all.
			Signed:  api.SignNone,
			Access:  api.Read,
			Summary: "Subscribe to the account's own new-trade pushes (WS, private, WS in-band login, requires an API key with futures trade permission)",
			WSCollect: func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error) {
				if dryRun {
					// A --dry-run preview must work with no saved profile and
					// never perform the real login handshake. Login itself is a
					// connection-time step (not a separate network call the way
					// spot's listenKey exchange is), so there is nothing to skip
					// here beyond not requiring credentials to be present.
					return &transport.WSConfig{
						HostKey:  futuresWSHostKey,
						Channels: []string{"order.deal"},
						Codec:    transport.CodecFuturesEdge,
					}, nil
				}
				if apiKey == "" || secretKey == "" {
					return nil, errs.Validation(errs.SubtypeFailedPrecondition,
						"this operation requires API credentials").
						WithHint("ask the user to run `%s profile set <name>` in their own terminal (it prompts; "+
							"no flag, stdin or environment variable supplies a secret instead), then `%s profile use <name>`",
							cliname.Name(), cliname.Name())
				}
				reqTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
				// The WS login target string is accessKey+reqTime — deliberately
				// NO request params, unlike a hypothetical futures REST scheme's
				// accessKey+timestamp+params.
				signature := signer.HMACSHA256Hex(secretKey, apiKey+reqTime)
				loginFrame, err := json.Marshal(map[string]any{
					"method": "login",
					"param":  map[string]any{"apiKey": apiKey, "reqTime": reqTime, "signature": signature},
				})
				if err != nil {
					return nil, errs.Internal("failed to build login frame: %v", err).WithCause(err)
				}
				return &transport.WSConfig{
					HostKey:    futuresWSHostKey,
					Channels:   []string{"order.deal"},
					Codec:      transport.CodecFuturesEdge,
					LoginFrame: loginFrame,
				}, nil
			},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command
// assembly and the contract test (must match the SKILL.md frontmatter).
const Scope = "futures.account"

// Signed is this subdomain's default signing scheme. There is no REST
// operation in this subdomain, so this constant exists only to satisfy the
// registry's mandatory-default rule (validateRegistry); ws-deals already
// carries the same value regardless (Signed: api.SignNone above).
const Signed = api.SignNone

// Identity is this subdomain's default login identity: openapi, the same
// api-key/secret source ws-deals's WSCollect closure resolves credentials
// from. futures shares one API key/secret system with spot (an account only
// needs futures trading permission enabled on it), so this reuses
// identity.OpenAPI rather than declaring a new identity.
const Identity = identity.OpenAPI
