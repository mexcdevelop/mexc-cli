// Package accountapi declares the operations of the spot.account subdomain:
// the account's own new-trade stream (ws-deals).
//
// Like every skills/**/api package, this declares api.Operation descriptors
// only — name, access, signed-or-not, params. The framework (internal/cli +
// internal/transport) handles the connection, signing, rate limiting, and
// command assembly. scope is always spot.account (a resource identifier, no
// action suffix); the action lives in each Operation's Access.
package accountapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return wsOperations()
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.account"

// Signed is this subdomain's default signing scheme: the CLI gateway HMAC
// scheme. An operation inherits it by omitting the Signed field; an operation
// overrides only with a concrete api.Sign* constant.
//
// Every operation here is a KindWSCollect (WS-backed) op, which overrides
// Signed to none because it builds no request of its own. The default is still
// this scheme rather than none: ws-deals' listenKey preflight is a real signed
// request sent on its behalf, so the subdomain does sign — and an operation
// added here without an explicit override must sign too.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), not a login session. A
// KindWSCollect op inherits it as well: the api key and secret its preflight
// signs with come from the same place.
const Identity = identity.OpenAPI
