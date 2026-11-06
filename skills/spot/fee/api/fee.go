// Package feeapi declares the operations of the spot.fee subdomain (spot fee
// rate center — a member's trading fee rates by pair / overall / channel / with
// MX deduction / lowest, their zero-fee pairs, plus country zero-fee pairs and a
// pair's default rule fee).
//
// Like the other spot subdomains it declares api.Operation descriptors only —
// name, method, path, read/write, params. The framework (internal/cli +
// internal/transport) handles HTTP, signing, rate limiting, and command
// assembly. scope is always spot.fee (a resource identifier, no action suffix);
// every operation here is a read.
//
// Every operation carries the openapi identity (the API key in X-MEXC-APIKEY);
// the gateway signs each request server-side, so no client-side signature is
// sent (const Signed = none). All ops are GET reads, so parameters travel in the
// query string. The fee-center CLI gateway route matches /api/cli/spot/fee/** and
// rewrites /api/cli/spot/fee/<rest> -> /fee/<rest>, so each op's Path is
// /api/cli/spot/fee/ followed by the backend path after the /fee/ prefix.
//
// Member-context ops (the caller's own fees) read memberId from the login
// context (not a flag), so the caller never passes a memberId. Those paths carry
// a member/all marker to avoid colliding with the explicit-memberId endpoints
// (e.g. backend /fee/symbol/member/query). The two member-independent ops
// (rule-symbol-default, zero-fee-country) hit reference-data endpoints directly.
package feeapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// sourceValues are the allowed values for symbol-fee-by-source --source: the
// order channel the fee is quoted for (defaults to WEB when omitted).
var sourceValues = []string{"WEB", "APP", "API"}

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it. The set here
// mirrors exactly the paths the fee-center CLI gateway route exposes.
func Operations() []api.Operation {
	return []api.Operation{
		// --- caller's own fees (member from login context) ---
		{
			Name: "symbol-fee", Method: "GET", Path: "/api/cli/spot/fee/symbol/member/query",
			Access:  api.Read,
			Summary: "The caller's fee rate for one trading pair",
			Params: []api.Param{
				{Name: "symbol", Desc: "Trading pair name, e.g. BTCUSDT (provide symbol or symbolId)"},
				{Name: "symbolId", Desc: "Trading pair id (provide symbol or symbolId)"},
			},
		},
		{
			Name: "symbol-fee-mx", Method: "GET", Path: "/api/cli/spot/fee/symbol/member/withMxDiscount",
			Access:  api.Read,
			Summary: "The caller's fee rate for a pair including MX deduction",
			Params: []api.Param{
				{Name: "symbol", Desc: "Trading pair name, e.g. BTCUSDT (provide symbol or symbolId)"},
				{Name: "symbolId", Desc: "Trading pair id (provide symbol or symbolId)"},
			},
		},
		{
			Name: "symbol-fee-by-source", Method: "GET", Path: "/api/cli/spot/fee/symbol/member/queryBySource",
			Access:  api.Read,
			Summary: "The caller's fee rate for a pair by order channel",
			Params: []api.Param{
				{Name: "symbol", Desc: "Trading pair name, e.g. BTCUSDT (provide symbol or symbolId)"},
				{Name: "symbolId", Desc: "Trading pair id (provide symbol or symbolId)"},
				{Name: "source", Desc: "Order channel: WEB / APP / API (default WEB)", Enum: sourceValues},
			},
		},
		{
			Name: "symbol-low", Method: "GET", Path: "/api/cli/spot/fee/symbol/member/low",
			Access:  api.Read,
			Summary: "The caller's lowest-fee trading pairs",
		},
		{
			Name: "member-fee", Method: "GET", Path: "/api/cli/spot/fee/member/all/query",
			Access:  api.Read,
			Summary: "The caller's overall fee rate",
		},
		{
			Name: "zero-fee-mine", Method: "GET", Path: "/api/cli/spot/fee/all/zero/query",
			Access:  api.Read,
			Summary: "The caller's zero-fee trading pairs",
		},

		// --- member-independent reference data ---
		{
			Name: "zero-fee-country", Method: "GET", Path: "/api/cli/spot/fee/zero/country/query",
			Access:  api.Read,
			Summary: "Zero-fee trading pairs for a country",
			Params: []api.Param{
				{Name: "countryCode", Desc: "Country code (default DEFAULT)"},
			},
		},
		{
			Name: "rule-symbol-default", Method: "GET", Path: "/api/cli/spot/fee/rule/symbol/default",
			Access:  api.Read,
			Summary: "A trading pair's default rule fee rate (no discount or deduction)",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id"},
			},
		},
		{
			Name: "symbol-strategy-default", Method: "GET", Path: "/api/cli/spot/fee/symbol/strategy/default",
			Access:  api.Read,
			Summary: "A trading pair's base fee rate (the default strategy rate, before member discounts/deductions)",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id", Required: true},
			},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.fee"

// Signed is this subdomain's default signing scheme: the CLI gateway scheme.
// The client signs the external (pre-rewrite) request — method, path, and
// canonical query — with the profile secret and sends the signature in a header;
// the gateway verifies it against the API key it carries. Every op here
// is a GET, so only the query is covered. Operations inherit this.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key in X-MEXC-APIKEY, which identifies the caller and
// sign server-side.
const Identity = identity.OpenAPI
