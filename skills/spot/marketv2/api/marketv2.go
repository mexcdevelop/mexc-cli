// Package marketv2api declares the operations of the spot.marketv2 subdomain
// (CLI market data, V2: symbols, tickers, coin info, concept plates,
// order-book depth / deals, and ranking markets).
//
// Like marketapi, this declares api.Operation descriptors only — name, path,
// read/write, params. The framework (internal/cli + internal/transport) handles
// HTTP, rate limiting, and command assembly.
//
// These are CLI-gateway endpoints (path prefix /api/cli/spot/market-v2),
// reached through the same gateway host as the openapi endpoints
// (hosts.KeyOpenAPI). They are public market-data reads: the subdomain default
// is unsigned with no identity, and every operation inherits it except
// user-eftd-info, which reads the caller's own eligibility and overrides both.
// symbols, tickers and symbol-trade used to be signed so the backend could hide
// pairs by the caller's account; they are public now, which means the pair list
// is filtered by locale and request IP only. scope is always spot.marketv2 (a
// resource identifier, no action suffix); every operation is a read.
//
// Locale / timezone are request headers, so they are declared with
// In: api.InHeader and surface as --language / --convertLanguage / --timezone.
package marketv2api

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// openPriceModeValues are the allowed values for the numeric openPriceMode query
// param on the ticker operations: 1 = first trade of the period, 2 = the
// previous period's close. (The kline subdomain uses a different, string-valued
// openPriceMode — FIRST_DEAL / LAST_CLOSE.)
var openPriceModeValues = []string{"1", "2"}

// boolValues are the allowed values for a boolean-valued string flag. The
// framework binds only string flags, so a boolean toggle is expressed as a
// string-with-enum and passed through to the upstream unchanged.
var boolValues = []string{"true", "false"}

// depthCountValues are the only depth level counts the upstream accepts; any
// other value (1, 5, 10, 25, 50, 100 ...) is refused with error -1130.
var depthCountValues = []string{"20", "30", "40"}

// depthDecimalPattern constrains the depth price bucket to a power of ten, which
// is the first of two upstream rules on this parameter: a non-power-of-ten is
// refused with -1129, and a power of ten outside the pair's price-scale window
// with -1131. Only the first rule can be checked client-side, since the second
// needs the pair's priceScale; see the subdomain SKILL.md Edge Cases.
const depthDecimalPattern = `^(1|10*|0\.0*1)$`

// symbolPattern constrains --symbol to the BASE_QUOTE form this subdomain's
// upstream actually indexes on (e.g. BTC_USDT), unlike spot.market's OpenAPI
// endpoints, which index the same pair as BTCUSDT (no separator). Passing the
// no-separator form here is a silent cache miss, not a rejection: ticker throws
// "symbol not exists" (30014) while symbol-trade instead returns a success
// envelope with sst=2 (NOT_EXIST) — two different failure shapes for the same
// root cause, which is exactly what this pattern heads off client-side before
// either path is reached.
const symbolPattern = `^[A-Za-z0-9]+_[A-Za-z0-9]+$`

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	ops := []api.Operation{
		{
			Name: "symbols", Method: "GET", Path: "/api/cli/spot/market-v2/symbolsV2",
			Access:  api.Read,
			Summary: "All spot trading pairs, grouped by market (V2); hidden pairs are filtered out by locale / IP / account",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
			},
		},
		{
			Name: "tickers", Method: "GET", Path: "/api/cli/spot/market-v2/tickers",
			Access:  api.Read,
			Summary: "24h ticker list for all trading pairs; hidden pairs are filtered out by locale / IP / account",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "timezone", Desc: "Timezone, e.g. UTC+8 (default UTC+8)", In: api.InHeader},
				{Name: "openPriceMode", Desc: "Open-price mode: 1 first trade, 2 previous close (default 1)", Enum: openPriceModeValues},
			},
		},
		{
			Name:    "ticker",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/symbol/ticker",
			Access:  api.Read,
			Summary: "24h ticker detail for a single trading pair",
			Params: []api.Param{
				{Name: "timezone", Desc: "Timezone, e.g. UTC+8 (default UTC+8)", In: api.InHeader},
				{Name: "symbol", Desc: "Trading pair in BASE_QUOTE form, e.g. BTC_USDT — required unless symbolId is given", Pattern: symbolPattern},
				{Name: "symbolId", Desc: "Trading pair id — required unless symbol is given"},
				{Name: "openPriceMode", Desc: "Open-price mode: 1 first trade, 2 previous close (default 1)", Enum: openPriceModeValues},
			},
		},
		{
			Name: "symbol-trade", Method: "GET", Path: "/api/cli/spot/market-v2/symbol/trade",
			Access:  api.Read,
			Summary: "Trade-page info for a pair: fee rates, order types, order limits",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
				{Name: "symbol", Desc: "Trading pair in BASE_QUOTE form, e.g. BTC_USDT — required unless symbolId is given", Pattern: symbolPattern},
				{Name: "symbolId", Desc: "Trading pair id — required unless symbol is given"},
				{Name: "symbolFee", Desc: "Whether to include fee-rate info (default true); false drops tfr / mfr / omf / otf but still returns bfr", Enum: boolValues},
			},
		},
		{
			Name:    "coin-delay",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/coin/delay",
			Access:  api.Read,
			Summary: "Coins shown with a delay notice, and their delay-announcement links",
		},
		{
			Name:    "coin-introduce",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/coin/introduce",
			Access:  api.Read,
			Summary: "Detailed introduction for a single coin",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
				{Name: "coinId", Desc: "Coin id — required unless coinName is given"},
				{Name: "coinName", Desc: "Coin name — required unless coinId is given"},
			},
		},
		{
			Name:    "coins-introduce",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/coins/introduce",
			Access:  api.Read,
			Summary: "Brief introduction for multiple coins in one call",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
				{Name: "coinIds", Desc: "Comma-separated coin ids, e.g. 128,129", Required: true},
			},
		},
		{
			Name:    "coin-risk",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/coin/risk",
			Access:  api.Read,
			Summary: "Risk-warning message for a coin (status + text)",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
				{Name: "coinId", Desc: "Coin id", Required: true},
			},
		},
		{
			Name:    "coin-new-list",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/coin/new/list",
			Access:  api.Read,
			Summary: "New-coin ranking list",
		},
		{
			Name:    "st-coins",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/stCoins",
			Access:  api.Read,
			Summary: "ST-label coins and the markets each trades in",
		},
		{
			Name:    "api-available-symbols",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/api/available/symbols",
			Access:  api.Read,
			Summary: "Set of trading pairs available for API trading",
		},
		{
			Name:    "latest-symbols",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/latest/symbols",
			Access:  api.Read,
			Summary: "Most recently listed spot trading pairs",
		},
		{
			Name:    "depth",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/depth/v4",
			Access:  api.Read,
			Summary: "Order-book depth at a given price precision and level count",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id", Required: true},
				{Name: "decimal", Desc: "Price bucket size; must be a power of ten (1, 10, 100 ... or 0.1, 0.01, 0.001 ...) AND lie within the pair's price-scale window — read the pair's ps (priceScale) from the symbols operation first", Required: true, Pattern: depthDecimalPattern},
				{Name: "count", Desc: "Number of depth levels; only 20, 30 and 40 are accepted", Required: true, Enum: depthCountValues},
			},
		},
		{
			Name:    "deals",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/deals/v2",
			Access:  api.Read,
			Summary: "Most recent trades for a trading pair",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id", Required: true},
			},
		},
		{
			Name:    "concept-plates",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/concept/platesV2",
			Access:  api.Read,
			Summary: "Concept-plate (sector) list (V2)",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
			},
		},
		{
			Name:    "concept-plate-introduce",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/concept/plate/introduce",
			Access:  api.Read,
			Summary: "Introduction for a single concept plate",
			Params: []api.Param{
				{Name: "language", Desc: "Language, e.g. en-US (default en-US)", In: api.InHeader},
				{Name: "convertLanguage", Desc: "Conversion / display language", In: api.InHeader},
				{Name: "plateId", Desc: "Concept plate id", Required: true},
			},
		},
		{
			Name:    "concept-plate-amount",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/concept/plate/amount",
			Access:  api.Read,
			Summary: "24h turnover of each symbol in a concept plate",
			Params: []api.Param{
				{Name: "plateId", Desc: "Concept plate id", Required: true},
			},
		},
		{
			Name:    "meme-coins",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/meme/coins",
			Access:  api.Read,
			Summary: "Meme-coin list (with chain info)",
		},
		{
			Name:    "timezone-list",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/timezone/list",
			Access:  api.Read,
			Summary: "Timezone identifiers the system supports",
		},
		{
			Name: "user-eftd-info", Method: "GET", Path: "/api/cli/spot/market-v2/user/eftd_info",
			// The one operation here that is about the caller rather than the market:
			// it reads the caller's own first-deposit eligibility, so it is signed and
			// carries the API key while everything else in this subdomain is public.
			// Unsigned, the backend has no member to look up and answers an empty
			// object for everyone.
			Signed:   signing.CliHMAC,
			Identity: identity.OpenAPI,
			Access:   api.Read,
			Summary:  "Current user's EFTD (first-deposit benefit) eligibility info",
		},
		{
			Name:    "stock-ranking-markets",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/stockRankingMarkets",
			Access:  api.Read,
			Summary: "Markets supported by the US-stock ranking",
		},
		{
			Name:    "metal-ranking-markets",
			Method:  "GET",
			Path:    "/api/cli/spot/market-v2/metalRankingMarkets",
			Access:  api.Read,
			Summary: "Markets supported by the metal ranking",
		},
	}
	return append(ops, wsOperations()...)
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.marketv2"

// Signed is this subdomain's default: unsigned. Market data needs no
// credentials; user-eftd-info overrides it.
const Signed = api.SignNone

// Identity is this subdomain's default: no identity. user-eftd-info overrides
// it with the API-key identity.
const Identity = api.IdentityNone
