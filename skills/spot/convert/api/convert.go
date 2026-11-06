// Package convertapi declares the operations of the spot.convert sub-domain —
// MEXC flash convert (swap one asset directly into another at a quoted rate,
// with no order book).
//
// Like the other subdomains this declares api.Operation descriptors only — name,
// path, read/write, params, request body. The framework (internal/cli +
// internal/transport) does HTTP, auth, rate limiting and command assembly.
//
// Two things about this subdomain's shape are worth knowing up front:
//
//   - The convert flow is TWO steps for a market convert: `quote-forward` /
//     `quote-reverse` returns a quotation carrying an id, and `place` then
//     executes that id. A quotation expires, so the two calls belong together.
//     `place-limit` is the one-step alternative: it carries its own rate and
//     rests until the market reaches it.
//
//   - These are gateway endpoints and every one of them is SIGNED by the client
//     (const Signed below): the profile secret signs the request, and the gateway
//     verifies that signature and resolves the caller from the API key the request
//     also carries — so a saved profile needs both halves, not just the key.
//     What the signature covers follows the request shape, and both shapes are
//     here. The GET quotes / queries and the DELETE cancel-limit put their params
//     in the query, and the canonical query is what is covered. place and
//     place-limit POST a JSON body, and the body is
//     covered too, by its hash, exactly as the bytes are sent. That the scheme can
//     cover a body is the precondition for declaring one at all — the assembly
//     layer refuses to pair a query-only scheme with a request body — which is
//     what TestOps_BodyOpsResolveToABodyCapableScheme guards. The three placement
//     headers below are OUTSIDE the signature — beyond the request line, query and
//     body it covers only its own protocol values (a timestamp, and an idempotency
//     key when one is sent), none of which is a declared param — so those three can
//     be sent or omitted per call without affecting it.
//
// Write semantics, and the two kinds differ: `place` and `place-limit` are
// non-destructive (they only add an order, unmaking nothing) but NON-idempotent —
// a repeat converts again — so the retry layer never auto-retries them.
// `cancel-limit` is the mirror image: it destroys a resting order, yet repeating
// it converges on the same state, so it is idempotent and may be retried.
// Everything else is a read.
package convertapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Shared enum vocabularies for the QUERY parameters. Upstream models these as
// integer-coded enums and the wire form is the CODE, not the name — hence the
// numeric strings.
//
// The two body enums (accountType / expireType) are deliberately NOT here: a
// struct tag cannot reference a Go variable, so their vocabulary lives in the tag
// that actually enforces it (see placeReq / placeLimitReq). Keeping a second,
// unenforced copy in this block is how the two drift.
var (
	// tradeTypeValues is the quote direction: 1 forward (you fix the amount you
	// SPEND), 2 reverse (you fix the amount you RECEIVE).
	tradeTypeValues = []string{"1", "2"}
	// businessTypeValues selects the convert business line; 0 (spot) is the
	// default and the only one this subdomain is scoped to.
	businessTypeValues = []string{"0", "1", "2", "3", "4"}
)

// placeReq is the JSON body of spot.convert.place: it executes a quotation that
// `quote-forward` / `quote-reverse` already returned. The rate, the currencies
// and the amounts all come from that quotation, so the only thing to send is its
// id — which is also why a quotation that has expired cannot be placed.
//
// Two upstream fields are deliberately not exposed: the caller's own account,
// resolved from the credential, and the request source, which the request's own
// headers carry. accountType is a NAMED enum upstream, not an integer code, so
// the wire form is the constant name (SPOT / CONTRACT). A JSON number would be
// read positionally, which happens to equal the code here, but that is a
// coincidence rather than a contract; the name is what upstream promises. Omit
// the field to get the default, SPOT.
type placeReq struct {
	ID          string `json:"id" validate:"required" desc:"Quotation order id returned by a quote call"`
	AccountType string `json:"accountType,omitempty" enum:"SPOT,CONTRACT" desc:"Where the converted asset lands: SPOT (default) or CONTRACT (futures)"`
}

// placeLimitReq is the JSON body of spot.convert.place-limit — a limit convert:
// instead of executing a live quotation it carries its own target rate and rests
// until the market reaches it (or the lifetime in expireType runs out).
//
// convertRate together with the two quantities describes the intended exchange,
// so all three are required; the backend does not derive one from the others. It
// does, however, cross-check them, and two of those checks are the ones a caller
// trips first:
//
//   - Self-consistency: fromConvertQuantity * convertRate must be >=
//     toConvertQuantity, and all three must be positive. Otherwise the order is
//     rejected as "params error" — the guard exists so a caller cannot book an
//     exchange that would settle at a loss.
//   - The rate must BEAT the market: the backend fetches the live convert rate and
//     refuses the order when the live rate is already >= convertRate (a limit
//     convert that the market has met is just a market convert). So quote first,
//     then set convertRate strictly above what quote-forward reports.
//
// There is no direction choice here: the trade type is pinned to forward
// upstream, so a limit convert always fixes the amount you SPEND.
type placeLimitReq struct {
	FromCurrencyID string `json:"fromCurrencyId" validate:"required" desc:"Currency id being spent"`
	ToCurrencyID   string `json:"toCurrencyId" validate:"required" desc:"Currency id being received"`

	FromConvertQuantity string `json:"fromConvertQuantity" validate:"required" desc:"Amount to spend, decimal string; within the pair's min/max and no more decimal places than its quantity precision"`
	ToConvertQuantity   string `json:"toConvertQuantity" validate:"required" desc:"Amount to receive, decimal string; must be <= fromConvertQuantity * convertRate"`
	ConvertRate         string `json:"convertRate" validate:"required" desc:"Target rate as amount of TO per 1 FROM, decimal string; must be strictly higher than the live rate, max 35 integer digits"`

	// expireType is a plain Integer code on the DTO (ExpireType.toEnum rejects an
	// unknown code with a bad-request), so it travels as a JSON NUMBER — not a
	// quoted string.
	ExpireType  int    `json:"expireType,omitempty" enum:"1,2,3,4,5" desc:"Lifetime code: 1=1h, 2=1d, 3=3d, 4=1w, 5=30d (default)"`
	AccountType string `json:"accountType,omitempty" enum:"SPOT,CONTRACT" desc:"Where the converted asset lands: SPOT (default) or CONTRACT (futures)"`
}

// placementHeaders declares the request headers the two convert PLACEMENT
// endpoints carry. Only place and place-limit take them — the quotes and the
// queries do not — so these are deliberately per-op params rather than something
// the identity or the host row injects into every request. Param.Name is used
// verbatim as the header name and also spells the CLI flag, so
// they surface as `--platform` / `--ip` / `--language`.
//
// All three are OPTIONAL: the backend does not demand any of them, so declaring
// one required would fail a call the backend would have accepted.
//
//   - platform carries a single-value Enum even though it is optional. The value
//     is fixed — this product is the CLI, so CLI is the only thing it can
//     truthfully report — so the Enum is not there to let a caller choose but to
//     reject anything else before the request is sent, the same way place-oco
//     pins orderType to one value. Being optional, it is only sent when a caller
//     passes `--platform CLI`; there is no framework mechanism for a constant
//     default, so an omitted flag means an absent header.
//   - ip is optional for a second, stronger reason: a CLI does not know its own
//     public address. It sees
//     only its local interfaces, and the address the backend cares about is
//     whatever its edge observed. Anything this binary filled in unasked would be
//     a guess presented as a fact, and finding out for real means calling some
//     third-party echo service on every placement. So it is passed through when a
//     caller has a reason to set it, and omitted otherwise — leaving the backend
//     to use the peer address it already has.
//   - language is OPTIONAL and defaults server-side to en-US, matching how the
//     marketv2 ops declare the same header.
func placementHeaders() []api.Param {
	return []api.Param{
		// Desc tags stay terse on purpose: each one is carried into the MCP tool
		// description, which is capped per tool (TestMCPToolTokenBudget). Why ip is
		// optional, and what a caller should do about it, is in SKILL.md.
		{
			Name: "platform",
			In:   api.InHeader,
			Enum: []string{"CLI"},
			Desc: "Always CLI",
		},
		{Name: "ip", In: api.InHeader, Desc: "Caller IP"},
		{Name: "language", In: api.InHeader, Desc: "Language, default en-US"},
	}
}

// Operations returns every operation of this sub-domain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "currencies", Method: "GET", Path: "/api/cli/spot/convert/currency/list",
			Access:  api.Read,
			Summary: "List the currencies available for convert (includes your balance per currency)",
			Params: []api.Param{
				{Name: "state", Desc: "Config state filter; 0 (default) is the normally available set"},
			},
		},
		{
			Name: "quantity-limit", Method: "GET", Path: "/api/cli/spot/convert/quantity/limit/get",
			Access: api.Read,
			// Identity is INHERITED (openapi), like every other op here. This once
			// declared IdentityNone, reasoned from the backend service: that path is
			// excluded from its member interceptor and its handler never reads a
			// member, so the service itself needs no caller. That reasoning missed a
			// layer. The gateway in front of the service authenticates every
			// /api/cli/* path, so a request carrying no key is refused before the
			// handler is reached — verified against the gateway, which answers
			// `api key required` (HTTP 400) with or without the CLI in the picture.
			// What the service does with a member is not what decides this; whether
			// the gateway lets the request through is.
			Summary: "Get the min/max convertible amount for a currency pair",
			Params: []api.Param{
				{Name: "fromCurrencyId", Desc: "Currency id being spent", Required: true},
				{Name: "toCurrencyId", Desc: "Currency id being received", Required: true},
			},
		},
		{
			Name: "quote-forward", Method: "GET", Path: "/api/cli/spot/convert/calculate/quantity/forward",
			Access: api.Read,
			// tradeType is Required here even though the backend defaults it,
			// because its default is REVERSE: omitting it on the forward path
			// silently yields a reverse quote (and then demands toConvertQuantity).
			// Making it explicit is the difference between a correct quote and a
			// confusing rejection.
			Summary: "Quote a convert by the amount you SPEND (forward: you fix fromConvertQuantity)",
			Params: []api.Param{
				{Name: "fromCurrencyId", Desc: "Currency id being spent", Required: true},
				{Name: "toCurrencyId", Desc: "Currency id being received", Required: true},
				{Name: "fromConvertQuantity", Desc: "Amount to spend, decimal string", Required: true},
				{Name: "tradeType", Desc: "Quote direction; pass 1 (forward) for this operation", Required: true, Enum: tradeTypeValues},
				{Name: "businessType", Desc: "Convert business line; 0 spot (default)", Enum: businessTypeValues},
				{Name: "isQuotation", Desc: "true to return a bindable quotation rather than an indicative rate"},
			},
		},
		{
			Name: "quote-reverse", Method: "GET", Path: "/api/cli/spot/convert/calculate/quantity/reverse",
			Access:  api.Read,
			Summary: "Quote a convert by the amount you RECEIVE (reverse: you fix toConvertQuantity)",
			Params: []api.Param{
				{Name: "fromCurrencyId", Desc: "Currency id being spent", Required: true},
				{Name: "toCurrencyId", Desc: "Currency id being received", Required: true},
				{Name: "toConvertQuantity", Desc: "Amount to receive, decimal string", Required: true},
				{Name: "tradeType", Desc: "Quote direction; defaults to 2 (reverse), which is this operation", Enum: tradeTypeValues},
				{Name: "businessType", Desc: "Convert business line; 0 spot (default)", Enum: businessTypeValues},
				{Name: "isQuotation", Desc: "true to return a bindable quotation rather than an indicative rate"},
			},
		},
		{
			Name: "place", Method: "POST", Path: "/api/cli/spot/convert/order/place",
			Access: api.Write,
			// Executing a quotation adds a convert order without unmaking anything
			// (non-destructive), but a repeat converts a second time, so Idempotent
			// stays false (the write default) and it is never auto-retried.
			Destructive: api.Ptr(false),
			Summary:     "Execute a quotation returned by a quote call (market convert)",
			Body:        placeReq{},
			Params:      placementHeaders(),
		},
		{
			Name: "place-limit", Method: "POST", Path: "/api/cli/spot/convert/limit/order/place",
			Access: api.Write,
			// Like place, this only adds an order — but it also FREEZES the
			// fromConvertQuantity until the order triggers, expires, or is
			// cancelled, which is why cancel-limit is its counterpart.
			Destructive: api.Ptr(false),
			// Returns the quotation order id — the handle cancel-limit takes.
			Summary: "Place a limit convert that rests until the market reaches your rate; returns its quotation order id",
			Body:    placeLimitReq{},
			Params:  placementHeaders(),
		},
		{
			Name: "cancel-limit", Method: "DELETE", Path: "/api/cli/spot/convert/limit/order/cancel",
			Access: api.Write,
			// Idempotent, unlike the two place ops: the backend only cancels an
			// order that is still resting, and re-cancelling one that is already
			// gone changes nothing (it is refused, not double-cancelled), so a
			// repeat converges. Destructive keeps the write default of true — this
			// unmakes a resting order and releases its frozen funds.
			//
			// Caveat worth knowing when a retry does fire: the backend dedupes by
			// order id for ~3s, so a prompt retry comes back as "the cancel request
			// was sent too quickly" rather than a second cancel. That is a business
			// state, not a transport failure.
			Idempotent: api.Ptr(true),
			Summary:    "Cancel a resting limit convert by its quotation order id",
			Params: []api.Param{
				// The id here is the QUOTATION order id — the value place-limit
				// returned, surfaced as quotationOrderId by `orders`. The convert
				// order's own `id` is a different, prefixed string and is rejected:
				// the backend parses this parameter as a number.
				{Name: "orderId", Desc: "Quotation order id of the resting limit convert (numeric): what place-limit returned, i.e. quotationOrderId in `orders` — NOT the convert order id", Required: true},
			},
		},
		{
			Name: "orders", Method: "GET", Path: "/api/cli/spot/convert/order/query",
			Access:  api.Read,
			Summary: "Query your convert orders, paged (isCurrent=true for resting limit converts)",
			Params: []api.Param{
				{Name: "isCurrent", Desc: "true = only resting (not yet converted) limit orders; false/omitted = finished ones"},
				{Name: "id", Desc: "Filter by convert order id"},
				{Name: "currencyId", Desc: "Filter by either side's currency id"},
				{Name: "fromCurrencyId", Desc: "Filter by the currency spent"},
				{Name: "toCurrencyId", Desc: "Filter by the currency received"},
				{Name: "startTime", Desc: "Range start, epoch milliseconds"},
				{Name: "endTime", Desc: "Range end, epoch milliseconds"},
				{Name: "pageNum", Desc: "Page number, default 1"},
				{Name: "pageSize", Desc: "Page size, default 20"},
			},
		},
	}
}

// Scope is the resource identifier of this sub-domain (matches SKILL.md frontmatter).
const Scope = "spot.convert"

// Signed is this sub-domain's default signing scheme: the CLI gateway scheme, and
// every operation inherits it.
//
// The client signs the external (pre-rewrite) request — method, path, canonical
// query, and a hash of the JSON body — with the profile secret and sends the
// signature in a header; the gateway verifies it and resolves the member from the
// API key the request also carries.
//
// This was none until the gateway began verifying a client signature on these
// endpoints. The old comment argued a scheme could not be declared here because
// the spot scheme signs the query only and the assembly layer refuses to pair a
// query-only scheme with a request body — true of that scheme, and the reason this
// subdomain could not simply adopt it. The gateway scheme covers a JSON body, so
// place and place-limit are fine under it; the guard that enforces the pairing is
// in the contract test.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default: a credential IS required. The convert
// flow is per-member (it moves your assets and returns your balances), and the
// framework's closed Identity enum has no "gateway session" value, so openapi —
// the public credential identity — is the closest true statement: a caller must be
// authenticated. EVERY operation here inherits it, with no exception: the gateway
// authenticates the whole /api/cli/* surface, so there is no public call to carve
// out (see quantity-limit, which used to try).
const Identity = identity.OpenAPI
