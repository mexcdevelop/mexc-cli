// Package tradeapi declares the operations of the spot.trade subdomain (placing
// and cancelling spot orders, plus order/trade queries).
//
// Like accountapi, this declares api.Operation descriptors only —
// name, path, read/write, signed-or-not, params. The framework (internal/cli + internal/transport)
// handles HTTP, signing, rate limiting, and command assembly. scope is always
// spot.trade (a resource identifier, no action suffix); the action lives in each
// Operation's Access.
//
// This subdomain is the one where reads and writes coexist, so the write
// semantics matter and are set explicitly:
//
//   - place / place-market / place-oco / place-trigger / place-market-trigger /
//     place-track: write, NON-idempotent (a repeat places another order and could
//     double-fill) and NON-destructive (it only adds an order, unmaking nothing).
//     Non-idempotent means the retry layer never auto-retries them — a lost
//     response could mean the order already landed. The two everyday paths split
//     by order kind: place is LIMIT (price + quantity, may carry TP/SL),
//     place-market is MARKET (no price; sized by quantity OR the quote-asset
//     amount).
//   - modify: write, NON-idempotent AND destructive — it is a cancel-and-replace,
//     so it removes the original order and yields a new order id (see the op).
//   - cancel: write, idempotent (cancelling an already-cancelled order converges
//     to the same state, safe to repeat) and destructive (it removes a resting
//     order). It is the ONLY cancel here, and it spans four scopes from one order
//     to the whole account, see its declaration.
//   - deal-detail / current-orders / history-orders / deals: read (idempotent,
//     non-destructive by default).
//
// A placement being non-idempotent does not mean a resend can never be made safe.
// It means the FRAMEWORK cannot make it safe, while the CALLER can: the backend
// dedupes a placement on clientOrderId + member, so a resend carrying the same
// clientOrderId is refused as a duplicate instead of placing a second order.
//
// The declaration stays non-idempotent anyway, and must, because the field is
// OPTIONAL. The transport cannot see whether a given call supplied one, and
// auto-retrying a placement that did not would double-fill — so a declaration of
// idempotent here would be true only for the calls that happened to carry the field
// and catastrophic for the rest. The retry layer therefore never resends a
// placement by itself; an agent that wants a resendable placement supplies a
// clientOrderId, resends deliberately, and reads a duplicate rejection as "the
// first attempt landed". place-trigger is the one placement that does not expose
// the field at all, so nothing can make it resendable.
//
// Every endpoint here is a gateway endpoint, and every one is signed: the client
// signs the request with the profile secret and the gateway resolves the member
// from the API key it carries (see Signed and Identity). So a saved profile needs
// both halves — an api key alone is not enough.
//
// What the signature covers follows the request shape, and both shapes are here.
// The four GET queries and the DELETE cancel put their params in the query, and
// the canonical query is what is covered. The seven POSTs — the six placements
// plus modify — send a JSON body, and the body is covered too, by its hash, over
// exactly the bytes that are sent. That the scheme can cover a body is the
// precondition for declaring one at all: the assembly layer refuses to pair a
// query-only scheme with a request body, which is what
// TestOps_BodyOpsResolveToABodyCapableScheme guards in this package.
//
// Two consequences of everything being a gateway op are worth knowing up front.
// First, every placement is body-based, so placing an order means building JSON
// for --data rather than passing flags, and the vocabulary is currency+market /
// tradeType, never symbol / side. Second, there is no BATCH placement and no
// equivalent to fall back on — N orders means N calls. Quote-amount sizing does
// exist, via place-market's `amount`, but only for market orders; a limit order is
// still sized in the base asset.
//
// Single-order lookup is deal-detail only, and it is keyed on orderId — no
// operation here accepts a client order id, so one has to be resolved against a
// current-orders / history-orders listing first.
package tradeapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

var (
	// sideValues is the side by NAME, the form the body-based placements and the
	// `deals` query use.
	sideValues = []string{"BUY", "SELL"}
	// tradeTypeCodes is the SAME axis by integer code, which is what the order-list
	// queries and cancel's batch filter take instead. Two spellings of one concept,
	// kept as separate vars so a declaration cannot quietly use the wrong one.
	tradeTypeCodes = []string{"1", "2"}
	// boolValues is the wire spelling of a boolean query flag.
	boolValues = []string{"true", "false"}
	// orderTypeValues are the backend OrderType constant NAMES, as accepted by
	// deal-detail's orderType parameter. They are the enum's names rather than its
	// integer codes because that endpoint compares the parameter against
	// OrderType.name(); TRIGGER_ORDER is the one value that changes which store is
	// read (see the op).
	orderTypeValues = []string{
		"TRIGGER_ORDER", "LIMIT_ORDER", "POST_ONLY", "IMMEDIATE_OR_CANCEL",
		"FILL_OR_KILL", "MARKET_ORDER", "STOP_LIMIT_ORDER", "OCO_STOP_LIMIT_ORDER",
		"OCO_LIMIT_ORDER", "STOP_MARKET_ORDER", "TRACK_LIMIT_ORDER", "TRACK_MARKET_ORDER",
	}
)

// orderTypeCodeTable is the backend OrderType enum's code -> name mapping, in the
// enum's own declaration order.
//
// It exists as a constant because the SAME table has to appear in three places
// that must not drift: deal-detail's orderType help (below), the batch-filter
// documentation in SKILL.md, and this comment's reader. The translation is not
// optional knowledge — the listings return this enum as its integer code while
// deal-detail accepts only the name, so every "read the type off a row, then look
// the order up" flow needs it, and an agent that does not have it here has to
// find it in prose. TestOrderTypeCodeTable_MatchesEnumAndSkillDoc locks the three
// copies together.
const orderTypeCodeTable = "-1 TRIGGER_ORDER, 1 LIMIT_ORDER, 2 POST_ONLY, " +
	"3 IMMEDIATE_OR_CANCEL, 4 FILL_OR_KILL, 5 MARKET_ORDER, 100 STOP_LIMIT_ORDER, " +
	"101 OCO_STOP_LIMIT_ORDER, 102 OCO_LIMIT_ORDER, 103 STOP_MARKET_ORDER, " +
	"104 TRACK_LIMIT_ORDER, 105 TRACK_MARKET_ORDER"

// placeOcoReq is the JSON body of spot.trade.place-oco. An OCO
// ("one-cancels-the-other") placement submits two linked legs in one call — a
// resting limit order and a stop-limit order — and whichever one activates
// cancels the other. The framework reflects this struct into the schema and
// deep-validates --data against it before sending.
//
// Symbol identification accepts EITHER complete pair: the names
// (currency + market) or the ids (currencyId + marketCurrencyId). One full pair
// is required, but "either pair" is not expressible as a per-field constraint,
// so none of the four carries validate:"required"; supplying neither complete
// pair is refused by the backend as an invalid-parameter error.
// The desc tags are deliberately terse. This body has 13 fields, and every desc
// is carried into the MCP tool description, which is capped per tool
// (TestMCPToolTokenBudget) — so the tags name each field and nothing more. The
// business constraints that cannot be expressed as a type or an enum (each leg's
// required position relative to the latest traded price, the side-to-triggerType
// binding, the currency+market / currencyId+marketCurrencyId pairing, precision
// and eligibility limits) live in SKILL.md, which is where an agent reads them.
type placeOcoReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	Quantity    string `json:"quantity" validate:"required" desc:"Quantity, positive decimal"`
	TradeType   string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`
	OrderType   string `json:"orderType" validate:"required" enum:"OCO_STOP_LIMIT_ORDER"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`

	Price             string `json:"price" validate:"required" desc:"Limit-leg price"`
	TriggerType       string `json:"triggerType" validate:"required" enum:"GE,LE" desc:"Activation dir; BUY=GE, SELL=LE"`
	TriggerPrice      string `json:"triggerPrice" validate:"required" desc:"Stop-limit activation price"`
	TriggerLimitPrice string `json:"triggerLimitPrice" validate:"required" desc:"Stop-limit placement price"`

	ClientOrderID string `json:"clientOrderId,omitempty" desc:"Client order id, 1-32 chars. Optional dedupe key: a resend reusing it is refused as a duplicate; without it a resend places a second order"`
}

// placeTriggerReq is the JSON body of spot.trade.place-trigger — a stop-limit
// ("trigger") order: nothing rests on the book until the market reaches
// triggerPrice, at which point a limit order for quantity at price is placed.
//
// Two things the backend owns and a caller must therefore NOT send:
// the order type (fixed server-side to the stop-limit type) and memberId (taken
// from the authenticated caller). Both are omitted here on purpose.
//
// Decimal fields are declared as strings so a value passes through exactly as
// written; the backend parses them as arbitrary-precision decimals. Bounds and
// eligibility rules are in SKILL.md — see the note on placeOcoReq above for why
// the desc tags stay short.
type placeTriggerReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	TradeType   string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`
	TriggerType string `json:"triggerType" validate:"required" enum:"GE,LE" desc:"GE: activate when price rises to triggerPrice; LE: when it falls"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`

	TriggerPrice string `json:"triggerPrice" validate:"required" desc:"Activation price"`
	Price        string `json:"price" validate:"required" desc:"Limit price used once activated"`
	Quantity     string `json:"quantity" validate:"required" desc:"Quantity, positive decimal"`
}

// placeMarketTriggerReq is the JSON body of spot.trade.place-market-trigger — a
// stop-MARKET order: once the market reaches triggerPrice, a market order is
// submitted (so there is no limit price, unlike place-trigger).
//
// quantity and amount are an exclusive pair: exactly one must be supplied
// (quantity sells/buys a base-asset amount, amount spends/receives a quote-asset
// amount). Neither carries validate:"required" because "exactly one of" is not a
// per-field constraint; the backend rejects both-blank and both-set alike.
//
// The DTO's `nonce` field is deliberately NOT exposed, and this is a correction:
// this struct used to carry it, described as a "client dedupe nonce". Nothing
// dedupes on it, established from both ends — the same nonce sent twice came back
// as two successful placements with two different orderIds, and the backend's
// placement path for this endpoint never reads the field. So the description
// promised the one property an agent acts on after a timeout and delivered none of
// it, which made it the most dangerous field in this file: a field named nonce is
// reached for at exactly the wrong moment, and an agent that believes a placement
// is deduped treats a lost response as safe to resend — which for a stop-market
// placement is a second live order.
//
// The dedupe key this operation does have is clientOrderId, which the backend pairs
// with the member — see the package comment for why that does not make the op
// idempotent. With `nonce` gone, a body carrying it is refused locally as an unknown
// field instead of being accepted and silently ignored.
type placeMarketTriggerReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	TradeType   string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`
	OrderType   string `json:"orderType" validate:"required" enum:"STOP_MARKET_ORDER"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`
	TriggerType string `json:"triggerType" validate:"required" enum:"GE,LE" desc:"GE: activate on rise to triggerPrice; LE: on fall"`

	TriggerPrice string `json:"triggerPrice" validate:"required" desc:"Activation price"`
	Quantity     string `json:"quantity,omitempty" desc:"Base-asset amount; exclusive with amount"`
	Amount       string `json:"amount,omitempty" desc:"Quote-asset amount; exclusive with quantity"`

	ClientOrderID string `json:"clientOrderId,omitempty" desc:"Client order id, 1-32 chars. Optional dedupe key: a resend reusing it is refused as a duplicate; without it a resend places a second order"`
}

// placeTrackReq is the JSON body of spot.trade.place-track — a trailing
// ("track") order: it follows the market by trackAmplitude and submits an order
// when the price retraces by that fraction from its best excursion.
//
// Which price/size fields are required depends on orderType, so none of them is
// marked required here (the framework cannot express a conditional):
//   - TRACK_LIMIT_ORDER  needs price AND quantity (amount is unused)
//   - TRACK_MARKET_ORDER needs exactly one of quantity or amount (price unused)
type placeTrackReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	TradeType   string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`
	OrderType   string `json:"orderType" validate:"required" enum:"TRACK_LIMIT_ORDER,TRACK_MARKET_ORDER" desc:"Trailing limit or trailing market"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`

	// A FRACTION, and the field most likely to be filled in ten times wrong: 5%
	// is 0.05, so 0.5 is 50%. The pattern below is the (0,1]-at-4-decimals rule
	// the backend enforces, expressed locally so a percent-shaped value (5, 50)
	// is refused before a round trip. It cannot help with 0.5, which is a LEGAL
	// value the backend accepts: nothing here can tell a mistaken 0.5 from a
	// deliberate one, so the desc carries the worked conversion and the caller has
	// to read it — the confirmation prompt echoes the raw fraction back unchanged.
	//
	// Written with character classes ([.] / [0-9] rather than \. / \d) to keep
	// backslashes out of a struct tag, whose value is unquoted before use. The
	// branches enumerate "at least one non-zero decimal, at most four", which is
	// how the range is expressed without a negative lookahead (RE2 has none).
	TrackAmplitude   string `json:"trackAmplitude" validate:"required" pattern:"^(1([.]0{1,4})?|0[.][1-9][0-9]{0,3}|0[.]0[1-9][0-9]{0,2}|0[.]00[1-9][0-9]?|0[.]000[1-9])$" desc:"Trail FRACTION within (0,1] at 4 decimals: 0.05 = 5%, 0.5 = 50%"`
	TrackActivePrice string `json:"trackActivePrice,omitempty" desc:"Optional activation price before trailing starts"`

	Price    string `json:"price,omitempty" desc:"Limit price; TRACK_LIMIT_ORDER only"`
	Quantity string `json:"quantity,omitempty" desc:"Base-asset amount"`
	Amount   string `json:"amount,omitempty" desc:"Quote-asset amount; TRACK_MARKET_ORDER only"`

	ClientOrderID string `json:"clientOrderId,omitempty" desc:"Client order id, 1-32 chars. Optional dedupe key: a resend reusing it is refused as a duplicate; without it a resend places a second order"`
}

// placeReq is the JSON body of spot.trade.place — THE general placement operation
// of this subdomain. Omit profitTriggerPrice and loseTriggerPrice and it places a
// plain order; supply either or both to attach a take-profit and/or stop-loss to
// the order being placed.
//
// orderType is optional and defaults to a limit order server-side, but it only
// accepts the non-conditional types: the conditional families (stop-limit,
// stop-market, OCO, trailing) are refused here and have their own operations.
//
// price and quantity are BOTH required — the backend refuses a request without
// either, including for MARKET_ORDER. There is no
// quote-amount field on THIS operation, so a limit order can only be sized in the
// base asset — "spend N of the quote asset" is expressible only through
// place-market's `amount`.
type placeReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	Price     string `json:"price" validate:"required" desc:"Order price"`
	Quantity  string `json:"quantity" validate:"required" desc:"Quantity, positive decimal"`
	TradeType string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`

	OrderType   string `json:"orderType,omitempty" enum:"LIMIT_ORDER,MARKET_ORDER,IMMEDIATE_OR_CANCEL,POST_ONLY,FILL_OR_KILL" desc:"Defaults to LIMIT_ORDER"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`

	ProfitTriggerPrice string `json:"profitTriggerPrice,omitempty" desc:"Take-profit trigger; BUY: above price, SELL: below"`
	LoseTriggerPrice   string `json:"loseTriggerPrice,omitempty" desc:"Stop-loss trigger; BUY: below price, SELL: above"`

	ClientOrderID string `json:"clientOrderId,omitempty" desc:"Client order id, 1-32 chars. Optional dedupe key: a resend reusing it is refused as a duplicate; without it a resend places a second order"`
}

// placeMarketReq is the JSON body of spot.trade.place-market — the v4 placement
// endpoint, which the backend restricts to MARKET orders: its service layer
// refuses anything whose orderType is not MARKET_ORDER with a 400 "invalid order
// type" (PlaceOrderProxyImpl.doPlaceMarketOrderMarForId), hence the single-value
// enum below.
//
// It exists alongside place because the two cover different ground. place (the
// /api/cli/spot/order/place DTO) requires BOTH price and quantity and can attach
// a take-profit/stop-loss, so it is the limit-order path. This one needs no price
// at all and adds `amount`, a QUOTE-asset size — the only way in this subdomain to
// express "spend N USDT" rather than "buy N BTC".
//
// quantity and amount are an exclusive pair, and neither carries
// validate:"required" because "exactly one of" is not a per-field constraint:
// quantity sizes the order in the base asset (natural for a market SELL), amount
// in the quote asset (natural for a market BUY).
//
// The underlying DTO also carries a `price` field, inherited from the shared v4
// request type. It is deliberately NOT exposed: this endpoint only accepts market
// orders, where the fill price comes from the book, so offering a price here would
// only invite the caller to think it caps anything.
type placeMarketReq struct {
	Currency         string `json:"currency,omitempty" desc:"Base asset, e.g. BTC"`
	Market           string `json:"market,omitempty" desc:"Quote asset, e.g. USDT"`
	CurrencyID       string `json:"currencyId,omitempty" desc:"Base asset id (alt)"`
	MarketCurrencyID string `json:"marketCurrencyId,omitempty" desc:"Quote asset id (alt)"`

	TradeType   string `json:"tradeType" validate:"required" enum:"BUY,SELL" desc:"Order side"`
	OrderType   string `json:"orderType" validate:"required" enum:"MARKET_ORDER" desc:"Market orders only"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`

	Quantity string `json:"quantity,omitempty" desc:"Base-asset amount, e.g. sell 0.001 BTC; exclusive with amount"`
	Amount   string `json:"amount,omitempty" desc:"Quote-asset amount, e.g. spend 100 USDT; exclusive with quantity"`

	ClientOrderID string `json:"clientOrderId,omitempty" pattern:"^[0-9a-zA-Z_-]{1,32}$" desc:"Client order id, 1-32 chars of [0-9a-zA-Z_-]. Optional dedupe key: a resend reusing it is refused as a duplicate; without it a resend places a second order"`
}

// modifyReq is the JSON body of spot.trade.modify. The operation is a
// cancel-and-replace, not an in-place edit: the original order is cancelled and a
// fresh one is placed, so the response carries a NEW order id. Only price and
// quantity can change — side, symbol and type are inherited from the original.
type modifyReq struct {
	OrderID     string `json:"orderId" validate:"required" desc:"Id of the resting limit order to modify"`
	Price       string `json:"price" validate:"required" desc:"New price, positive decimal"`
	Quantity    string `json:"quantity" validate:"required" desc:"New quantity, positive decimal"`
	OrderSource string `json:"orderSource" validate:"required" enum:"CLI" desc:"Order origin; always CLI"`
}

// orderListParams builds the parameter set shared by current-orders and
// history-orders, which the backend serves from one handler shape: the two differ
// only in which order states they will look at. kind names that set for the
// state param's help text ("open" / "finished").
//
// Every parameter is optional. Two things about the filters are worth knowing and
// are spelled out in SKILL.md:
//   - orderTypes / states / tradeType all travel as INTEGER CODES here, unlike
//     the body-based placements (names) and unlike `deals` (tradeType by name).
//   - the state filter is CLAMPED, not validated: the backend intersects it with
//     the states the endpoint owns, and an empty intersection silently falls back
//     to all of them rather than erroring or returning nothing.
func orderListParams(kind string) []api.Param {
	return []api.Param{
		{Name: "currency", Desc: "Base asset, e.g. BTC (pair by name)"},
		{Name: "market", Desc: "Quote asset, e.g. USDT (pair by name)"},
		{Name: "currencyId", Desc: "Base asset id (pair by id)"},
		{Name: "marketCurrencyId", Desc: "Quote asset id (pair by id)"},
		{Name: "orderTypes", Desc: "Comma-separated OrderType CODES (not names), e.g. 1,5"},
		// No Enum here even though the values are a closed set: this is a
		// comma-separated multi-value param (Set<Integer> on the backend), and a
		// single-value Enum would reject every list. Same reason as orderTypes.
		{Name: "states", Desc: "Comma-separated OrderState CODES (0 INACTIVE, 1 NEW, 2 FILLED, 3 PARTIALLY_FILLED, 4 CANCELED, 5 PARTIALLY_CANCELED); silently clamped to this endpoint's " + kind + " states"},
		{Name: "tradeType", Desc: "Side as a CODE: 1 BUY, 2 SELL; omit for both", Enum: tradeTypeCodes},
		{Name: "startTime", Desc: "Range start, epoch milliseconds"},
		{Name: "endTime", Desc: "Range end, epoch milliseconds"},
		{Name: "pageNum", Desc: "Page number, default 1"},
		{Name: "pageSize", Desc: "Page size, default 20"},
		{Name: "needPage", Desc: "false skips the total-count query (faster, no total)", Enum: boolValues},
	}
}

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "place-oco", Method: "POST", Path: "/api/cli/spot/v4/order/place/oco",
			Access: api.Write,
			// Signing is inherited, like everywhere else here: the subdomain scheme
			// signs the request and the gateway verifies it. What matters for a
			// body-carrying op is that the scheme can cover a JSON body — the assembly
			// layer refuses to pair a query-only scheme with a request body
			// (internal/cli.validateRegistry, via signer.CanSignBody), which is what
			// TestOps_BodyOpsResolveToABodyCapableScheme guards in this package.
			// Same write semantics as place: an OCO submission adds two linked
			// orders without unmaking anything (non-destructive), and repeating it
			// would place a second pair, so Idempotent stays false (the write
			// default) and the retry layer never auto-retries it.
			Destructive: api.Ptr(false),
			Summary:     "Place an OCO pair (limit leg + stop-limit leg; one activating cancels the other)",
			Body:        placeOcoReq{},
		},
		{
			Name: "place-trigger", Method: "POST", Path: "/api/cli/spot/order/place/trigger/v2",
			Access: api.Write,
			// Signing and identity are inherited, like every op here: the request is
			// signed with the profile secret and carries the API key.
			// Adds a resting trigger without unmaking anything, and a repeat would
			// place a second trigger — so non-destructive, non-idempotent (the write
			// default), never auto-retried.
			Destructive: api.Ptr(false),
			Summary:     "Place a stop-limit (trigger) order: a limit order submitted only once the market reaches triggerPrice",
			Body:        placeTriggerReq{},
		},
		{
			Name: "place-market-trigger", Method: "POST", Path: "/api/cli/spot/v4/order/place/market/trigger",
			Access: api.Write,
			// Signing and identity inherited, as everywhere here; the scheme covers the
			// JSON body this op carries.
			Destructive: api.Ptr(false),
			Summary:     "Place a stop-market (trigger) order: a market order submitted once the market reaches triggerPrice",
			Body:        placeMarketTriggerReq{},
		},
		{
			Name: "place-track", Method: "POST", Path: "/api/cli/spot/v4/order/place/track",
			Access:      api.Write,
			Destructive: api.Ptr(false),
			Summary:     "Place a trailing (track) order that follows the market by trackAmplitude",
			Body:        placeTrackReq{},
		},
		{
			Name: "place", Method: "POST", Path: "/api/cli/spot/order/place",
			Access:      api.Write,
			Destructive: api.Ptr(false),
			Summary:     "Place a LIMIT order (price + quantity required); optionally attach a take-profit and/or stop-loss",
			Body:        placeReq{},
		},
		{
			Name: "place-market", Method: "POST", Path: "/api/cli/spot/v4/order/place",
			Access: api.Write,
			// Signed and credentialed by inheritance, like its siblings.
			// Same write semantics as the other placements: it only adds an order
			// (non-destructive), and a repeat would place a second one, so Idempotent
			// stays false (the write default) and it is never auto-retried. That
			// matters more here than elsewhere — a market order fills immediately, so
			// a duplicate is a real position, not a resting order you can cancel.
			Destructive: api.Ptr(false),
			Summary:     "Place a MARKET order, sized either in the base asset (quantity) or the quote asset (amount)",
			Body:        placeMarketReq{},
		},
		{
			Name: "modify", Method: "POST", Path: "/api/cli/spot/order/modify",
			Access: api.Write,
			// NOTE the write semantics here differ from cancel, and from the generic
			// advice that "amending an order is idempotent". This amend is a
			// cancel-and-replace: it DESTROYS the original resting order (so
			// Destructive keeps the write default of true) and it is NOT safe to
			// repeat (so Idempotent keeps the write default of false, and the retry
			// layer never auto-retries it). A lost response is genuinely ambiguous —
			// the original may already be cancelled and replaced under a new id, and
			// a blind repeat would fail on a now-nonexistent order or act on stale
			// state. Both fields are therefore left unset on purpose.
			Summary: "Modify a resting limit order's price/quantity (cancel-and-replace; returns a new order id)",
			Body:    modifyReq{},
		},
		{
			Name: "cancel", Method: "DELETE", Path: "/api/cli/spot/order/cancel/v2",
			Access: api.Write,
			// Signing and identity are inherited, like every op here. The backend
			// refuses an order that is not the caller's ("Only can cancel self
			// order").
			// Cancelling converges on a fixed state, so a repeat is safe (Destructive
			// keeps the write default of true — it unmakes resting orders). The
			// backend dedupes per order for ~3s and answers CANCEL_ORDER_TOO_QUICKLY
			// inside that window; that is a business state, not a transport failure.
			Idempotent: api.Ptr(true),
			// FOUR modes, selected by WHICH params are present rather than by an
			// explicit mode flag: one plan order (triggerId), one order (orderId),
			// one pair (symbol/symbolId + optional filters), or the whole account
			// (nothing at all — cancelBatch with a null symbol, the same call the
			// backend's own bulk path uses). All four are intended behaviour, so
			// every param is correctly optional; the precedence and the scope-widens-
			// when-a-param-is-missing consequence live in SKILL.md. The summary spells
			// the account-wide form out because it is the widest one and the CLI/MCP
			// faces show only the summary.
			//
			// Two things this cannot do: cancel by origClientOrderId (the endpoint takes
			// no client-order-id param at all, so a client id has to be resolved against
			// a current-orders listing first), and cancel several symbols in one call
			// (symbol is a single pair, so N pairs means N calls).
			Summary: "Cancel one order (triggerId/orderId), one pair (symbol, with optional side/type filters), or — with no parameters at all — every open order in the account",
			Params: []api.Param{
				// Single-cancel mode. triggerId wins over orderId when both are sent,
				// but orderId is the one to reach for: it cancels every kind of order
				// here, plan orders included, and a value matching no spot order is
				// retried as a triggerId anyway. triggerId is usable only while the
				// caller still holds what a trigger placement returned, because NO
				// listing exposes it — current-orders carries orderId and no triggerId,
				// so an order discovered rather than just placed can only be cancelled
				// by orderId. Hence the descs point at orderId as the default route.
				{Name: "triggerId", Desc: "Cancel one plan order by the triggerId its placement returned; takes precedence over orderId"},
				{Name: "orderId", Desc: "Cancel any one order by its orderId, plan orders included — the normal route"},
				// Batch mode, used only when neither id above is present. symbolId
				// wins; a symbol is resolved to its symbolId server-side.
				// Spelled as the order listings return it (BTC_USDT), NOT as the
				// public market endpoints take it (BTCUSDT) — the gateway refuses
				// the joined form with "Invalid market name".
				{Name: "symbol", Desc: "Batch mode: cancel this pair's open orders, spelled as current-orders returns it, e.g. BTC_USDT (used only when no id is given)"},
				{Name: "symbolId", Desc: "Batch mode: same as symbol but by pair id; takes precedence over symbol"},
				// Both filters are INTEGER codes on the wire, unlike deal-detail's
				// orderType which is a NAME — an easy pair to mix up.
				{Name: "tradeType", Desc: "Batch filter, side as a code: 1 BUY, 2 SELL; omit for both", Enum: tradeTypeCodes},
				{Name: "orderTypes", Desc: "Batch filter, comma-separated OrderType CODES (not names), e.g. 100,103 for stop-limit+stop-market; omit for all"},
			},
		},
		{
			Name: "current-orders", Method: "GET", Path: "/api/cli/spot/order/current/orders/v2",
			Access: api.Read,
			// Signed and credentialed by inheritance, like its siblings.
			Summary: "List the caller's OPEN orders, paged, with type/side/state/pair/time filters",
			Params:  orderListParams("open"),
		},
		{
			Name: "history-orders", Method: "GET", Path: "/api/cli/spot/order/history/orders/v2",
			Access:  api.Read,
			Summary: "List the caller's FINISHED orders, paged, with the same filters as current-orders",
			Params:  orderListParams("finished"),
		},
		{
			Name: "deals", Method: "GET", Path: "/api/cli/spot/deal/deals",
			Access:  api.Read,
			Summary: "List the caller's fills (trade history), paged, filtered by pair / side / time",
			Params: []api.Param{
				{Name: "currency", Desc: "Base asset, e.g. BTC (pair by name)"},
				{Name: "market", Desc: "Quote asset, e.g. USDT (pair by name)"},
				{Name: "currencyId", Desc: "Base asset id (pair by id)"},
				{Name: "marketCurrencyId", Desc: "Quote asset id (pair by id)"},
				// NOTE the wire form differs from the two order-list endpoints: this
				// one takes tradeType by NAME, whereas current-orders /
				// history-orders take the integer CODE.
				// Same concept, two spellings — an easy and silent mistake.
				{Name: "tradeType", Desc: "Side by NAME (not the 1/2 code the order lists use)", Enum: sideValues},
				{Name: "startTime", Desc: "Range start, epoch milliseconds"},
				{Name: "endTime", Desc: "Range end, epoch milliseconds"},
				{Name: "pageNum", Desc: "Page number, default 1"},
				{Name: "pageSize", Desc: "Page size, default 20"},
				{Name: "needPage", Desc: "false skips the total-count query (faster, no total)", Enum: boolValues},
			},
		},
		{
			Name: "deal-detail", Method: "GET", Path: "/api/cli/spot/order/deal/detail",
			Access: api.Read,
			// Signing and identity are inherited, like everything else here; the
			// backend scopes the lookup to the resolved member.
			Summary: "Query one order's detail plus its fills; the only operation that can look an order up by triggerId",
			Params: []api.Param{
				// Which id this is depends on orderType (see below): the trigger
				// record's own id for TRIGGER_ORDER, the spot order id otherwise.
				{Name: "orderId", Desc: "With TRIGGER_ORDER: the triggerId a place-trigger/-market-trigger/-track call returned. Otherwise: the spot order id. Must belong to the caller", Required: true},
				// Required, and load-bearing rather than cosmetic: the backend
				// branches on it to choose WHICH store to read, which in turn decides
				// what orderId means. TRIGGER_ORDER selects the plan/trigger-order
				// table, matched on t_trigger_order.id (the triggerId — NOT
				// execute_order_id, the eventual spot order id); every other value
				// selects the spot order table. A mismatch is not an error — it
				// silently looks in the wrong table and comes back empty.
				// Takes the NAME, and the listings return the CODE — so the desc has to
				// say so here. current-orders / history-orders spell orderType as an
				// integer (1, 5, 104), and passing that straight through is the natural
				// next step after "read the type off a row"; it is refused every time.
				// The code table is in SKILL.md under cancel.
				{Name: "orderType", Desc: "Order type by NAME, NOT the integer code a current-orders/history-orders row carries. Row code -> name: " + orderTypeCodeTable + ". Also selects the store AND the meaning of orderId: TRIGGER_ORDER = plan order by triggerId, any other value = spot order by order id", Required: true, Enum: orderTypeValues},
			},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.trade"

// Signed is this subdomain's default signing scheme: the CLI gateway scheme, and
// every operation inherits it (none declares a Signed of its own).
//
// The client signs the external (pre-rewrite) request — method, path, canonical
// query, and a hash of the JSON body — with the profile secret and sends the
// signature in a header; the gateway verifies it and resolves the member from the
// API key. That the scheme covers a JSON body is what makes it usable here at all:
// every placement in this subdomain is body-based, and the assembly layer refuses
// to pair a query-only scheme with a request body (see the contract test).
//
// The history is worth keeping, because this constant has now held three values.
// It was the MEXC spot HMAC scheme while this subdomain wrapped the OpenAPI
// endpoints. When those gave way to gateway endpoints it became none, on the
// understanding that the gateway identified the caller by session alone. It is now
// the gateway scheme: those same endpoints verify a client signature, so none had
// become the misleading value — the frontmatter derives `signed` from here, and
// would have claimed `no` for requests that are in fact signed.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries the
// API key in X-MEXC-APIKEY, which identifies the caller, and the
// same profile's secret signs the request (see Signed). Operations inherit this.
const Identity = identity.OpenAPI
