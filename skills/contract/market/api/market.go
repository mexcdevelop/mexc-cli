// Package marketapi declares the operations of the contract.market sub-domain
// (public contract market data, read, unsigned).
//
// Declare only Operation descriptors (op name / path / access / params). Signing and login identity
// default at the subdomain level (const Signed / const Identity below); an op
// omits them to inherit, or sets a concrete signing.* / identity.* constant to
// override. The framework sends HTTP, signs, and mounts commands -- not this file.
//
// scope is always contract.market (a resource identifier, no action suffix); the
// action lives in each Operation's Access. Every operation here is a public read
// needing no signature or credential — the user identity, when present, is
// injected by the API gateway into request headers and is never a caller param.
package marketapi

import "github.com/mexcdevelop/mexc-cli/pkg/api"

// klineIntervals are the kline periods accepted by the contract kline endpoints
// (the upstream Interval enum names), kept in sync with the SKILL.md table.
var klineIntervals = []string{"Min1", "Min5", "Min15", "Min30", "Min60", "Hour4", "Hour8", "Day1", "Week1", "Month1"}

// bigDataIntervals are the statistic sampling periods accepted by the big-data
// endpoints (the upstream statistic Interval enum names).
var bigDataIntervals = []string{"MIN5", "MIN15", "MIN30", "H1", "H2", "H4", "H6", "H8", "H12", "D1"}

// bigDataPositionTypes are the platform statistic dimensions accepted by
// big-data-position (the upstream StatisticDataType enum names).
var bigDataPositionTypes = []string{"PLATFORM_TOTAL_AMOUNT", "PLATFORM_TOTAL_USERS", "PLATFORM_ELITE_TOTAL_AMOUNT", "PLATFORM_ELITE_TOTAL_USERS"}

// f returns a pointer to a numeric bound, for the Param MinVal/MaxVal fields.
func f(v float64) *float64 { return &v }

// Operations returns every operation of this sub-domain. The framework assembles
// commands from it, and the contract test self-checks against it. Every op
// inherits the subdomain defaults (Signed / Identity omitted) — all public reads.
func Operations() []api.Operation {
	return []api.Operation{
		// ---- Contract detail ----
		{
			Name: "detail", Method: "GET", Path: "/api/v1/contract/detail",
			Access:  api.Read,
			Summary: "Contract detail (a single contract, or the whole list when symbol is omitted)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT; omit to list all contracts"},
				{Name: "type", Desc: "Pass 'all' to include offline contracts in the full list"},
				{Name: "coin", Desc: "Filter the full list by quote coin, e.g. USDT"},
			},
		},
		{
			Name: "detail-v2", Method: "GET", Path: "/api/v1/contract/detailV2",
			Access:  api.Read,
			Summary: "Contract detail V2 (compact field names; symbol accepts a comma-separated list)",
			Params: []api.Param{
				{Name: "symbol", Desc: "One or more contract names, comma-separated, e.g. BTC_USDT,ETH_USDT; omit for all"},
				{Name: "client", Desc: "Client tag shaping the returned fields, e.g. web / ios / android"},
			},
		},
		{
			Name: "detail-v3", Method: "GET", Path: "/api/v1/contract/detailV3",
			Access:  api.Read,
			Summary: "Contract detail V3 (scene-tailored field set)",
			Params: []api.Param{
				{Name: "symbol", Desc: "One or more contract names, comma-separated; omit for all"},
				{Name: "client", Desc: "Client tag, e.g. web / ios / android", Required: true},
				{Name: "scene", Desc: "Scene code selecting the returned field set: 0=common,1=web1,2=web2,3=agencyCenter,4=platformAssets,5=seo,6=operation,7=otc,8=ptp,9=ios/android,10=ios,11=android,12=web3,13=ios/android-kline", Required: true, MinVal: f(0), MaxVal: f(13)},
				{Name: "params", Desc: "Comma-separated extra field keys to append to the scene result"},
			},
		},

		// ---- Margin / stable currencies ----
		{
			Name: "support-currencies", Method: "GET", Path: "/api/v1/contract/support_currencies",
			Access:  api.Read,
			Summary: "Supported margin currencies (name set)",
			Params: []api.Param{
				{Name: "includePoint", Desc: "Include MXPOINT in the result; default false"},
			},
		},
		{
			Name: "support-currencies-v2", Method: "GET", Path: "/api/v1/contract/support_currencies_v2",
			Access:  api.Read,
			Summary: "Supported margin currencies V2 (with per-currency detail)",
			Params: []api.Param{
				{Name: "includePoint", Desc: "Include MXPOINT in the result; default false"},
			},
		},
		{
			Name: "delivery-tips", Method: "GET", Path: "/api/v1/contract/delivery/tips",
			Access:  api.Read,
			Summary: "Delivery tips for a delivery contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "language", Desc: "Language tag for the tip text, e.g. en-US; defaults to the main language"},
			},
		},

		// ---- Prices ----
		{
			Name: "market-price-v2", Method: "GET", Path: "/api/v1/contract/market_price_v2",
			Access:  api.Read,
			Summary: "Latest index-source market prices of a contract V2 (with index weight)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
			},
		},

		// ---- Tickers ----
		{
			Name: "ticker", Method: "GET", Path: "/api/v1/contract/ticker",
			Access:  api.Read,
			Summary: "Ticker for one contract, or all contracts when symbol is omitted",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT; omit for all contracts"},
				{Name: "limit", Desc: "Max number of tickers to return when listing all"},
				{Name: "timezone", Desc: "Timezone id for the rise/fall figures, e.g. UTC+8", In: api.InHeader},
			},
		},
		{
			Name: "tickers", Method: "GET", Path: "/api/v1/contract/tickers",
			Access:  api.Read,
			Summary: "Tickers for a specified set of contracts",
			Params: []api.Param{
				{Name: "symbols", Desc: "Contract names, comma-separated, e.g. BTC_USDT,ETH_USDT", Required: true},
				{Name: "timezone", Desc: "Timezone id for the rise/fall figures, e.g. UTC+8", In: api.InHeader},
			},
		},
		{
			Name: "spot-latest-price", Method: "GET", Path: "/api/v1/contract/spot/latest_price",
			Access:  api.Read,
			Summary: "Latest spot price for the given currencies",
			Params: []api.Param{
				{Name: "currencies", Desc: "Currencies, comma-separated, e.g. BTC,ETH", Required: true},
			},
		},

		// ---- Order book depth ----
		{
			Name: "depth", Method: "GET", Path: "/api/v1/contract/depth/{symbol}",
			Access:  api.Read,
			Summary: "Order book depth of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "limit", Desc: "Number of levels to return; omit for the full cached depth"},
				{Name: "booksFull", Desc: "Return the full depth snapshot (rate-limited); default false"},
			},
		},
		{
			Name: "depth-step-enable", Method: "GET", Path: "/api/v1/contract/depth_step/enable",
			Access:  api.Read,
			Summary: "Whether the depth-merge precision switch is enabled (legacy, always true)",
		},
		{
			Name: "depth-step", Method: "GET", Path: "/api/v1/contract/depth_step/{symbol}",
			Access:  api.Read,
			Summary: "Merged-precision depth of a contract at a given step",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "step", Desc: "Merge-precision step; omit to use the contract's first configured step"},
			},
		},

		// ---- Trades ----
		{
			Name: "deals", Method: "GET", Path: "/api/v1/contract/deals/{symbol}",
			Access:  api.Read,
			Summary: "Recent trades of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "limit", Desc: "Number of trades; default 100, max 100", MaxVal: f(100)},
			},
		},

		// ---- Klines ----
		{
			Name: "kline", Method: "GET", Path: "/api/v1/contract/kline/{symbol}",
			Access:  api.Read,
			Summary: "Klines of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)"},
				{Name: "end", Desc: "End time (Unix seconds)"},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "mini-klines", Method: "GET", Path: "/api/v1/contract/miniKlines",
			Access:  api.Read,
			Summary: "Mini klines for a set of contracts",
			Params: []api.Param{
				{Name: "symbols", Desc: "Contract names, comma-separated, e.g. BTC_USDT,ETH_USDT", Required: true},
				{Name: "limit", Desc: "Max number of klines per contract"},
			},
		},
		{
			Name: "index-price-kline", Method: "GET", Path: "/api/v1/contract/kline/index_price/{symbol}",
			Access:  api.Read,
			Summary: "Index-price klines of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)"},
				{Name: "end", Desc: "End time (Unix seconds)"},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "fair-price-kline", Method: "GET", Path: "/api/v1/contract/kline/fair_price/{symbol}",
			Access:  api.Read,
			Summary: "Fair-price klines of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)"},
				{Name: "end", Desc: "End time (Unix seconds)"},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "kline-recent", Method: "GET", Path: "/api/v1/contract/kline/{symbol}/recent",
			Access:  api.Read,
			Summary: "Most recent klines of a contract ending at a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "end", Desc: "End time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "index-price-kline-recent", Method: "GET", Path: "/api/v1/contract/kline/index_price/{symbol}/recent",
			Access:  api.Read,
			Summary: "Most recent index-price klines of a contract ending at a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "end", Desc: "End time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "fair-price-kline-recent", Method: "GET", Path: "/api/v1/contract/kline/fair_price/{symbol}/recent",
			Access:  api.Read,
			Summary: "Most recent fair-price klines of a contract ending at a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "end", Desc: "End time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "kline-next", Method: "GET", Path: "/api/v1/contract/kline/{symbol}/next",
			Access:  api.Read,
			Summary: "Next page of klines of a contract starting from a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "index-price-kline-next", Method: "GET", Path: "/api/v1/contract/kline/index_price/{symbol}/next",
			Access:  api.Read,
			Summary: "Next page of index-price klines of a contract starting from a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},
		{
			Name: "fair-price-kline-next", Method: "GET", Path: "/api/v1/contract/kline/fair_price/{symbol}/next",
			Access:  api.Read,
			Summary: "Next page of fair-price klines of a contract starting from a given time",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "interval", Desc: "Kline period", Required: true, Enum: klineIntervals},
				{Name: "start", Desc: "Start time (Unix seconds)", Required: true},
				{Name: "limit", Desc: "Max number of klines"},
			},
		},

		// ---- Funding rate ----
		{
			Name: "funding-rate", Method: "GET", Path: "/api/v1/contract/funding_rate/{contractName}",
			Access:  api.Read,
			Summary: "Current funding rate of a contract",
			Params: []api.Param{
				{Name: "contractName", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
			},
		},
		{
			Name: "funding-rate-all", Method: "GET", Path: "/api/v1/contract/funding_rate",
			Access:  api.Read,
			Summary: "Current funding rate of all contracts",
		},
		{
			Name: "funding-rate-history", Method: "GET", Path: "/api/v1/contract/funding_rate/history",
			Access:  api.Read,
			Summary: "Funding rate history of a contract (paged)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "page_num", Desc: "Page number, starting at 1; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "funding-rate-history-of-day", Method: "GET", Path: "/api/v1/contract/funding_rate/historyOfDay",
			Access:  api.Read,
			Summary: "Funding rate history of a contract over the last N days",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "day", Desc: "Number of days to look back; max 14", Required: true, MinVal: f(1), MaxVal: f(14)},
			},
		},
		{
			Name: "funding-rate-board-info", Method: "GET", Path: "/api/v1/contract/funding_rate/board/info",
			Access:  api.Read,
			Summary: "Funding rate board summary info",
		},
		{
			Name: "funding-rate-board-list", Method: "GET", Path: "/api/v1/contract/funding_rate/board/list",
			Access:  api.Read,
			Summary: "Funding rate board list",
		},

		// ---- Risk reserve ----
		{
			Name: "risk-reverse-history", Method: "GET", Path: "/api/v1/contract/risk_reverse/history",
			Access:  api.Read,
			Summary: "Risk-reserve fund history of a contract (paged)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "page_num", Desc: "Page number, starting at 1; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "risk-reverse-history-of-days", Method: "GET", Path: "/api/v1/contract/risk_reverse/historyOfDays",
			Access:  api.Read,
			Summary: "Risk-reserve fund history of a contract by day",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
			},
		},

		// ---- Market changes / rankings / big data ----
		{
			Name: "market-changes-list", Method: "GET", Path: "/api/v1/contract/market_changes/list",
			Access:  api.Read,
			Summary: "Contract market-change (movers) list",
			Params: []api.Param{
				{Name: "category", Desc: "Market-change category filter"},
			},
		},
		{
			Name: "big-data-position", Method: "GET", Path: "/api/v1/contract/big_data/position",
			Access:  api.Read,
			Summary: "Long/short position ratio (platform big data)",
			Params: []api.Param{
				{Name: "currency", Desc: "Settlement currency, e.g. USDT", Required: true},
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "type", Desc: "Statistic dimension", Required: true, Enum: bigDataPositionTypes},
				{Name: "interval", Desc: "Sampling period", Required: true, Enum: bigDataIntervals},
			},
		},
		{
			Name: "big-data-active-trade-amount", Method: "GET", Path: "/api/v1/contract/big_data/active_trade_amount",
			Access:  api.Read,
			Summary: "Platform active buy/sell amount (platform big data)",
			Params: []api.Param{
				{Name: "currency", Desc: "Settlement currency, e.g. USDT", Required: true},
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "interval", Desc: "Sampling period", Required: true, Enum: bigDataIntervals},
			},
		},
		{
			Name: "hot-contracts", Method: "GET", Path: "/api/v1/contract/hotContracts",
			Access:  api.Read,
			Summary: "Hot contracts",
			Params: []api.Param{
				{Name: "limit", Desc: "Number of contracts; default 10"},
				{Name: "fullMsg", Desc: "Return full contract+ticker detail; default true. false returns symbols only"},
				{Name: "timezone", Desc: "Timezone id for the rise/fall figures, e.g. UTC+8", In: api.InHeader},
			},
		},
		{
			Name: "rise-fall-rate-sort", Method: "GET", Path: "/api/v1/contract/rise_fall_rate/sort",
			Access:  api.Read,
			Summary: "24h rolling rise/fall-rate ranking (gainers or losers)",
			Params: []api.Param{
				{Name: "riseSort", Desc: "true = gainers (descending), false = losers (ascending)", Required: true},
				{Name: "limit", Desc: "Number of entries; default 40"},
				{Name: "timezone", Desc: "Timezone id for the rise/fall figures, e.g. UTC+8", Required: true, In: api.InHeader},
			},
		},
	}
}

// Scope is this sub-domain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "contract.market"

// Signed is this sub-domain's default signing scheme: contract market is public
// data, entirely unsigned. Operations inherit it (they omit the Signed field).
const Signed = api.SignNone

// Identity is this sub-domain's default login identity: none — public market
// data needs no credential. Operations inherit it the same way as Signed.
const Identity = api.IdentityNone
