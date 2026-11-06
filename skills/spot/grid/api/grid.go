// Package gridapi declares the operations of the spot.grid subdomain (spot
// grid trading — automated buy-low / sell-high within a price range).
//
// Like the other spot subdomains it declares api.Operation descriptors only —
// name, method, path, read/write, params. The framework (internal/cli +
// internal/transport) handles HTTP, signing, rate limiting, and command
// assembly. scope is always spot.grid (a resource identifier, no action suffix);
// the action lives in each Operation's Access.
//
// Every operation carries the openapi identity (the API key in X-MEXC-APIKEY)
// and is signed client-side with the CLI gateway scheme (const Signed =
// signing.CliHMAC): the profile secret signs the external request and the
// gateway verifies it. This scheme can sign a JSON body, so POST operations send
// a JSON body (the backend reads only the body); GET and the DELETE stop use
// query params. The gateway strips the cli segment, rewriting /api/cli/spot/...
// to the backend /api/spot/... route, so /api/cli/spot/grid/... maps to
// /api/spot/grid/....
package gridapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// autoSellValues: 0 = do not auto-sell on stop, 1 = auto-sell. Used by the stop
// op, which is a DELETE carrying query params (a body is not allowed on DELETE).
var autoSellValues = []string{"0", "1"}

// The POST operations send a real JSON body to the backend. Each
// body struct below mirrors the client-supplied fields of the backend request
// DTO; server-side fields (memberId from the api key) are filled by the backend
// and never sent by the client. Every field is a string: the backend coerces it
// to its typed value (Long / Integer / BigDecimal / etc.), which mirrors how the
// same fields travel as query strings on the GET endpoints and sidesteps large-id
// JSON-number precision loss. The whole body is supplied on the CLI through one
// --data flag (inspect it with `mexc-cli schema`).

// listBody is the body of the list op.
// stateList filters by grid state; when omitted the backend defaults to the
// active states (0,1,2), so a stopped/finished grid drops off the list unless
// its state is asked for explicitly. Pass the codes to see stopped ones.
type listBody struct {
	StartDate string   `json:"startDate" desc:"Start time filter, Unix milliseconds"`
	EndDate   string   `json:"endDate" desc:"End time filter, Unix milliseconds"`
	StateList []string `json:"stateList,omitempty" desc:"Grid states to return: 0 init, 1 pending, 2 running, 3 finished, 4 stopping. Omit for active only (0,1,2); pass e.g. [\"2\",\"3\"] to include finished/stopped grids"`
	Page      string   `json:"page" desc:"Page number (default 1)"`
	PageSize  string   `json:"pageSize" desc:"Page size (default 20, max 200)"`
}

// arbitrageRecordsBody is the body of arbitrage-records.
type arbitrageRecordsBody struct {
	GridUniqId string `json:"gridUniqId" validate:"required" desc:"Grid strategy id (from list)"`
	Page       string `json:"page" desc:"Page number (default 1)"`
	PageSize   string `json:"pageSize" desc:"Page size (default 20, max 200)"`
}

// withdrawRecordsBody is the body of withdraw-records.
type withdrawRecordsBody struct {
	GridUniqId string `json:"gridUniqId" desc:"Grid strategy id (from list)"`
	State      string `json:"state" desc:"Withdrawal state filter"`
	Page       string `json:"page" desc:"Page number (default 1)"`
	PageSize   string `json:"pageSize" desc:"Page size (default 20, max 200)"`
}

// placeBody is the body of the place op.
type placeBody struct {
	MarketCurrencyId string `json:"marketCurrencyId" validate:"required" desc:"Quote asset id (e.g. USDT)"`
	CurrencyId       string `json:"currencyId" validate:"required" desc:"Base asset id"`
	LowerPrice       string `json:"lowerPrice" validate:"required" desc:"Grid lower price bound"`
	UpperPrice       string `json:"upperPrice" validate:"required" desc:"Grid upper price bound"`
	GridQty          string `json:"gridQty" validate:"required" desc:"Number of grids"`
	InvestAmount     string `json:"investAmount" validate:"required" desc:"Total invest amount"`
	GridType         string `json:"gridType" desc:"Grid type (default 1)"`
	GridMode         string `json:"gridMode" desc:"Grid mode: arithmetic/geometric (default 1)"`
	StartPrice       string `json:"startPrice" desc:"Optional trigger start price"`
	LossPrice        string `json:"lossPrice" desc:"Optional stop-loss price"`
	ProfitPrice      string `json:"profitPrice" desc:"Optional take-profit price"`
	AutoSell         string `json:"autoSell" enum:"0,1" desc:"Auto-sell on stop: 0 no, 1 yes (default 0)"`
	StrategySource   string `json:"strategySource" enum:"1,2" desc:"1 manual, 2 AI params (default 1)"`
	AiParamPeriod    string `json:"aiParamPeriod" enum:"101,201,301" desc:"AI period: 101 short, 201 mid, 301 long (required when strategySource=2)"`
}

// modifyAutoSellBody is the body of modify-auto-sell.
type modifyAutoSellBody struct {
	GridUniqId string `json:"gridUniqId" validate:"required" desc:"Grid strategy id (from list)"`
	AutoSell   string `json:"autoSell" validate:"required" enum:"0,1" desc:"0 no, 1 yes"`
}

// modifyTriggerPriceBody is the body of modify-trigger-price.
type modifyTriggerPriceBody struct {
	GridUniqId     string `json:"gridUniqId" validate:"required" desc:"Grid strategy id (from list)"`
	ProfitPrice    string `json:"profitPrice" desc:"Take-profit price"`
	LossPrice      string `json:"lossPrice" desc:"Stop-loss price"`
	TriggerVersion string `json:"triggerVersion" desc:"Optimistic-lock version of the current trigger prices"`
}

// immediateStartBody is the body of immediate-start.
type immediateStartBody struct {
	GridUniqId string `json:"gridUniqId" validate:"required" desc:"Grid strategy id (from list)"`
}

// withdrawApplyBody is the body of withdraw-apply.
type withdrawApplyBody struct {
	GridUniqId     string `json:"gridUniqId" validate:"required" desc:"Grid strategy id (from list)"`
	WithdrawAmount string `json:"withdrawAmount" validate:"required" desc:"Profit amount to withdraw (positive)"`
}

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		// --- queries (read) ---
		{
			Name: "config", Method: "GET", Path: "/api/cli/spot/grid/strategy/config",
			Access:  api.Read,
			Summary: "Grid config for a trading pair (price/qty limits, defaults)",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id", Required: true},
			},
		},
		{
			Name: "ai-params", Method: "GET", Path: "/api/cli/spot/grid/ai/params",
			Access:  api.Read,
			Summary: "AI-suggested grid params per period (price range, grid count, est. profit)",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id", Required: true},
			},
		},
		{
			Name: "list", Method: "POST", Path: "/api/cli/spot/grid/strategy/queryRecord",
			Access:  api.Read,
			Summary: "List the caller's grid strategies (defaults to active ones)",
			Body:    listBody{},
		},
		{
			Name: "detail", Method: "GET", Path: "/api/cli/spot/grid/strategy/detail",
			Access:  api.Read,
			Summary: "One grid strategy's detail and statistics",
			Params: []api.Param{
				{Name: "gridUniqId", Desc: "Grid strategy id (from list)", Required: true},
			},
		},
		{
			Name: "delegate-orders", Method: "GET", Path: "/api/cli/spot/grid/strategy/queryDelegateOrder",
			Access:  api.Read,
			Summary: "The grid's currently open (delegated) orders",
			Params: []api.Param{
				{Name: "gridUniqId", Desc: "Grid strategy id (from list)", Required: true},
			},
		},
		{
			Name: "arbitrage-records", Method: "POST", Path: "/api/cli/spot/grid/strategy/queryArbitrageRecord",
			Access:  api.Read,
			Summary: "A grid's arbitrage (matched buy-sell) records (paginated)",
			Body:    arbitrageRecordsBody{},
		},
		{
			Name: "stop-reason", Method: "GET", Path: "/api/cli/spot/grid/strategy/getStopReason",
			Access:  api.Read,
			Summary: "Why a grid strategy stopped",
			Params: []api.Param{
				{Name: "gridUniqId", Desc: "Grid strategy id (from list)", Required: true},
			},
		},
		{
			Name: "member-switch", Method: "GET", Path: "/api/cli/spot/grid/strategy/memberGridSwitch",
			Access:  api.Read,
			Summary: "Whether the caller is allowed to use grid trading",
		},
		{
			Name: "asset-info", Method: "GET", Path: "/api/cli/spot/grid/strategy/withdraw/assetInfo",
			Access:  api.Read,
			Summary: "A grid's asset / withdrawable profit info",
			Params: []api.Param{
				{Name: "gridUniqId", Desc: "Grid strategy id (from list)", Required: true},
			},
		},
		{
			Name: "withdraw-records", Method: "POST", Path: "/api/cli/spot/grid/strategy/withdraw/queryRecord",
			Access:  api.Read,
			Summary: "The caller's grid profit-withdrawal records (paginated)",
			Body:    withdrawRecordsBody{},
		},

		// --- create / modify / stop (write) ---
		{
			Name: "place", Method: "POST", Path: "/api/cli/spot/grid/strategy/place",
			Access:      api.Write,
			Destructive: api.Ptr(false), // creates a new grid; adds state, never auto-retried
			Summary:     "Create a grid strategy (manual params or strategySource=2 for AI params)",
			Body:        placeBody{},
		},
		{
			Name: "modify-auto-sell", Method: "POST", Path: "/api/cli/spot/grid/strategy/modify/autoSell",
			Access:     api.Write,
			Idempotent: api.Ptr(true), // converges to the target toggle
			Summary:    "Change a grid's auto-sell-on-stop setting",
			Body:       modifyAutoSellBody{},
		},
		{
			Name: "modify-trigger-price", Method: "POST", Path: "/api/cli/spot/grid/strategy/modify/triggerPrice",
			Access:     api.Write,
			Idempotent: api.Ptr(true), // converges to the target prices
			Summary:    "Change a grid's take-profit / stop-loss trigger prices",
			Body:       modifyTriggerPriceBody{},
		},
		{
			Name: "immediate-start", Method: "POST", Path: "/api/cli/spot/grid/strategy/modify/immediateStart",
			Access:  api.Write,
			Summary: "Start a pending grid immediately (skip the trigger wait)",
			Body:    immediateStartBody{},
		},
		{
			Name: "stop", Method: "DELETE", Path: "/api/cli/spot/grid/strategy/stop",
			Access:     api.Write,
			Idempotent: api.Ptr(true), // stopping converges to a stopped state
			Summary:    "Stop a running grid strategy",
			Params: []api.Param{
				{Name: "gridUniqId", Desc: "Grid strategy id (from list)", Required: true},
				{Name: "autoSell", Desc: "Sell base asset back to quote on stop: 0 no, 1 yes", Enum: autoSellValues},
			},
		},
		{
			Name: "withdraw-apply", Method: "POST", Path: "/api/cli/spot/grid/strategy/withdraw/apply",
			Access: api.Write,
			// Moving funds is neither idempotent (a repeat is a second withdrawal)
			// nor safe to auto-retry, so it keeps the write defaults.
			Summary: "Withdraw a grid's profit to the main account",
			Body:    withdrawApplyBody{},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.grid"

// Signed is this subdomain's default signing scheme: the CLI gateway scheme.
// The client signs the external (pre-rewrite) request — method, path, canonical
// query, and a hash of the JSON body — with the profile secret and sends the
// signature in a header; the gateway verifies it and resolves the member from
// the API key. This scheme can sign a JSON body, so the POST operations here are
// covered. Operations inherit this.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key in X-MEXC-APIKEY, which identifies the caller and
// sign server-side.
const Identity = identity.OpenAPI
