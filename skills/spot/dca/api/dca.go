// Package dcaapi declares the operations of the spot.dca subdomain (spot
// dollar-cost-averaging / auto-invest).
//
// Like the other spot subdomains it declares api.Operation descriptors only —
// name, method, path, read/write, params. The framework (internal/cli +
// internal/transport) handles HTTP, signing, rate limiting, and command
// assembly. scope is always spot.dca (a resource identifier, no action suffix);
// the action lives in each Operation's Access.
//
// Every operation carries the openapi identity (the API key in X-MEXC-APIKEY).
// The gateway signs each request server-side from that key, so no client-side
// signature is sent. These endpoints live on the CLI gateway host; the subdomain
// wires that via its registry hostKey.
package dcaapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// strategyStateValues are the allowed values for the strategy-set-state --state
// flag: 1 = running (resume), 2 = paused, 3 = offline (stop). strategy-set-state
// is a GET op whose params travel in the query, so it keeps a string-with-enum
// param (the framework only binds string flags).
var strategyStateValues = []string{"1", "2", "3"}

// The POST operations send a real JSON body to the backend. Each
// body struct below mirrors the client-supplied fields of the backend request
// DTO; server-side fields (memberId from the api key, ip / language from request
// headers) are filled by the backend and never sent by the client. Every field
// is a string: the backend coerces it to its typed value (Long / Integer /
// BigDecimal), which mirrors how the same fields travel as query strings on the
// GET endpoints and sidesteps large-id JSON-number precision loss. The whole
// body is supplied on the CLI through one --data flag (inspect it with
// `mexc-cli schema`).

// symbolsBody is the body of the symbols op.
type symbolsBody struct {
	SortDirection string `json:"sortDirection" enum:"ASC,DESC" desc:"Sort by PnL rate: ASC or DESC (default ASC)"`
}

// strategyListBody is the body of the strategy-list op.
type strategyListBody struct {
	Type      string `json:"type" enum:"0,1,2" desc:"Strategy filter: 0 running, 1 stopped, 2 all (default all)"`
	StartDate string `json:"startDate" desc:"Start time filter, Unix milliseconds"`
	EndDate   string `json:"endDate" desc:"End time filter, Unix milliseconds"`
	Page      string `json:"page" desc:"Page number (default 1)"`
	PageSize  string `json:"pageSize" desc:"Page size (default 20)"`
}

// strategyPlanListBody is the body of the strategy-plan-list op.
type strategyPlanListBody struct {
	StrategyId string `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	Page       string `json:"page" desc:"Page number (default 1)"`
	PageSize   string `json:"pageSize" desc:"Page size (default 20)"`
}

// strategySetNameBody is the body of the strategy-set-name op.
type strategySetNameBody struct {
	StrategyId   string `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	StrategyName string `json:"strategyName" validate:"required" desc:"New strategy name (max 40 chars)"`
}

// strategySetAmountBody is the body of the strategy-set-amount op.
type strategySetAmountBody struct {
	StrategyId   string `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	InvestAmount string `json:"investAmount" validate:"required" desc:"New per-period invest amount (positive)"`
}

// strategySetScheduleBody is the body of the strategy-set-schedule op.
type strategySetScheduleBody struct {
	StrategyId   string `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	ScheduleRule string `json:"scheduleRule" validate:"required" desc:"Cron expression; interval must exceed one hour"`
	TimeZone     string `json:"timeZone" validate:"required" desc:"Time zone offset, e.g. +08:00"`
}

// strategySetAccountBody is the body of the strategy-set-account op.
type strategySetAccountBody struct {
	StrategyId     string `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	AccountPayment string `json:"accountPayment" validate:"required" enum:"0,1" desc:"Payment account: 0 spot, 1 spot + finance"`
}

// dcaSymbolAllocation is one entry of strategy-create's symbolList: which pair to
// buy and its share of each period's invest amount. symbolId + percent are
// required; the price bounds are optional. Values are strings, coerced by the
// backend (BigDecimal), like the rest of this subdomain.
type dcaSymbolAllocation struct {
	SymbolId string `json:"symbolId" validate:"required" desc:"Trading pair id (from symbols)"`
	Percent  string `json:"percent" validate:"required" desc:"Decimal share, NOT a percent: 1 = 100%, 0.2 = 20% (sending 100 is rejected); shares across symbolList should sum to 1"`
	MinPrice string `json:"minPrice,omitempty" desc:"Optional lower price bound"`
	MaxPrice string `json:"maxPrice,omitempty" desc:"Optional upper price bound"`
}

// strategyCreateBody is the body of the strategy-create op. symbolList is a
// percent-weighted set of pairs; the
// percents are the split of investAmount across pairs each period. Server-side
// fields (memberId) are filled from the api key and never sent.
type strategyCreateBody struct {
	StrategyName string                `json:"strategyName" validate:"required" desc:"Strategy name (max 40 chars)"`
	ScheduleRule string                `json:"scheduleRule" validate:"required" desc:"Schedule expression (cron); interval must exceed one hour"`
	TimeZone     string                `json:"timeZone" validate:"required" desc:"Time zone offset, e.g. +08:00"`
	InvestAmount string                `json:"investAmount" validate:"required" desc:"Invest amount per period (positive)"`
	SymbolList   []dcaSymbolAllocation `json:"symbolList" validate:"required" desc:"Pairs to buy and their percent split (1..N)"`

	// Optional (the backend does not require these). accountPayment
	// defaults spot-only server-side; scheduleKind defaults CRON; triggerFlag
	// defaults 0 (wait for the schedule).
	AccountPayment string `json:"accountPayment,omitempty" enum:"0,1" desc:"Payment account: 0 spot, 1 spot + finance"`
	ScheduleKind   string `json:"scheduleKind,omitempty" enum:"CRON,RRULE" desc:"Schedule rule type (default CRON)"`
	TplId          string `json:"tplId,omitempty" desc:"Template id; 0 or omit if not from a template"`
	TriggerFlag    string `json:"triggerFlag,omitempty" enum:"0,1" desc:"0 wait for schedule, 1 also invest once immediately (default 0)"`
}

// strategySetSymbolBody is the body of strategy-set-symbol: replace a
// strategy's pair configuration (the pair list is validated server-side).
type strategySetSymbolBody struct {
	StrategyId string                `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	SymbolList []dcaSymbolAllocation `json:"symbolList" validate:"required" desc:"New pairs + percent split (replaces the current set)"`
}

// strategySetDetailBody is the body of strategy-set-detail: a whole-strategy
// update (name, amount, schedule and pairs are validated server-side).
// strategyName, investAmount, scheduleRule, timeZone and symbolList are all
// required; accountPayment / scheduleKind / triggerFlag are optional.
type strategySetDetailBody struct {
	StrategyId   string                `json:"strategyId" validate:"required" desc:"Strategy id (from strategy-list)"`
	StrategyName string                `json:"strategyName" validate:"required" desc:"Strategy name (max 40 chars)"`
	InvestAmount string                `json:"investAmount" validate:"required" desc:"Invest amount per period (positive)"`
	ScheduleRule string                `json:"scheduleRule" validate:"required" desc:"Schedule expression (cron); interval must exceed one hour"`
	TimeZone     string                `json:"timeZone" validate:"required" desc:"Time zone offset, e.g. +08:00"`
	SymbolList   []dcaSymbolAllocation `json:"symbolList" validate:"required" desc:"Pairs to buy and their percent split (replaces the current set)"`

	AccountPayment string `json:"accountPayment,omitempty" enum:"0,1" desc:"Payment account: 0 spot, 1 spot + finance"`
	ScheduleKind   string `json:"scheduleKind,omitempty" enum:"CRON,RRULE" desc:"Schedule rule type (default CRON)"`
	TriggerFlag    string `json:"triggerFlag,omitempty" enum:"0,1" desc:"0 wait for schedule, 1 also invest once immediately (default 0)"`
}

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
//
// POST operations send a JSON body (the backend reads only the body, and query
// params alone are rejected with 400); GET operations use query params.
// Since the gateway signs server-side, ops are unsigned (const Signed = none),
// which is also what lets a POST carry a body (the framework refuses a body
// under a client-side signature). The gateway strips the cli segment, rewriting
// /api/cli/spot/... to the backend /api/spot/... route, so
// /api/cli/spot/finance/dca/... maps to /api/spot/finance/dca/....
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "symbols", Method: "POST", Path: "/api/cli/spot/finance/dca/symbol/queryList",
			Access:  api.Read,
			Summary: "List DCA-eligible spot trading pairs with per-period PnL rates",
			Body:    symbolsBody{},
		},
		{
			Name: "strategy-list", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/list",
			Access:  api.Read,
			Summary: "List the caller's DCA strategies (paginated)",
			Body:    strategyListBody{},
		},
		{
			Name: "strategy-detail", Method: "GET", Path: "/api/cli/spot/finance/dca/strategy/detail",
			Access:  api.Read,
			Summary: "Get one DCA strategy's detail by strategyId",
			Params: []api.Param{
				{Name: "strategyId", Desc: "Strategy id (from strategy-list)", Required: true},
			},
		},
		{
			Name: "strategy-plan-list", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/plan/list",
			Access:  api.Read,
			Summary: "List the scheduled plans of one DCA strategy (paginated)",
			Body:    strategyPlanListBody{},
		},
		{
			Name: "strategy-set-state", Method: "GET", Path: "/api/cli/spot/finance/dca/strategy/changeState",
			Access: api.Write,
			// Setting a strategy to a target state converges (setting paused twice
			// is a no-op), so it is idempotent. It is left destructive by default
			// (the write default) because pausing/stopping halts an active strategy —
			// an impactful change worth flagging in the MCP safety hints.
			Idempotent: api.Ptr(true),
			Summary:    "Change a strategy's state: --state 1 resume, 2 pause, 3 offline",
			Params: []api.Param{
				{Name: "strategyId", Desc: "Strategy id (from strategy-list)", Required: true},
				{Name: "state", Desc: "Target state: 1 running (resume), 2 paused, 3 offline (stop)", Required: true, Enum: strategyStateValues},
			},
		},
		{
			Name: "strategy-create", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/create",
			Access: api.Write,
			// Creates a new strategy: a repeat creates a second one, so it keeps the
			// write defaults (non-idempotent, destructive) and is never auto-retried.
			Summary: "Create a DCA strategy (schedule + per-period invest amount + a percent-weighted symbolList)",
			Body:    strategyCreateBody{},
		},

		// --- symbol data (read) ---
		{
			Name: "symbols-recommended", Method: "GET", Path: "/api/cli/spot/finance/dca/symbol/recommendedList",
			Access:  api.Read,
			Summary: "List the recommended DCA trading pairs",
		},
		{
			Name: "symbols-basic", Method: "GET", Path: "/api/cli/spot/finance/dca/symbol/basic/list",
			Access:  api.Read,
			Summary: "List brief info of DCA-eligible trading pairs",
		},
		{
			Name: "top-pick", Method: "GET", Path: "/api/cli/spot/finance/dca/symbol/topPick/strategy",
			Access:  api.Read,
			Summary: "Get the curated top-pick DCA strategy",
		},
		{
			Name: "symbol-pnl-chart", Method: "GET", Path: "/api/cli/spot/finance/dca/symbol/pnl/chartData",
			Access:  api.Read,
			Summary: "PnL chart data for one trading pair over an interval",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id (from symbols)", Required: true},
				{Name: "interval", Desc: "Chart interval period, e.g. PERIOD_1_MONTH / PERIOD_1_YEAR", Required: true},
			},
		},

		// --- strategy queries (read) ---
		{
			Name: "strategy-precheck", Method: "GET", Path: "/api/cli/spot/finance/dca/strategy/preCheck",
			Access:  api.Read,
			Summary: "Pre-check whether the caller may create a new strategy",
		},
		{
			Name: "strategy-stats", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/stats/overview",
			Access:  api.Read,
			Summary: "The caller's DCA statistics overview",
		},
		{
			Name: "strategy-chart", Method: "GET", Path: "/api/cli/spot/finance/dca/strategy/chartData",
			Access:  api.Read,
			Summary: "PnL chart data for one strategy",
			Params: []api.Param{
				{Name: "strategyId", Desc: "Strategy id (from strategy-list)", Required: true},
			},
		},
		{
			Name: "plan-order-list", Method: "GET", Path: "/api/cli/spot/finance/dca/strategy/plan/order/list",
			Access:  api.Read,
			Summary: "List the orders of one plan of a strategy (paginated)",
			Params: []api.Param{
				{Name: "strategyId", Desc: "Strategy id (from strategy-list)", Required: true},
				{Name: "planId", Desc: "Plan id (from strategy-plan-list)", Required: true},
				{Name: "page", Desc: "Page number (default 1)"},
				{Name: "pageSize", Desc: "Page size (default 20)"},
			},
		},

		// --- common config (read) ---
		{
			Name: "global-config", Method: "GET", Path: "/api/cli/spot/finance/dca/common/global/configs",
			Access:  api.Read,
			Summary: "DCA global configuration (limits / switches)",
		},

		// --- strategy config changes (write; flat params, no nested symbolList) ---
		{
			Name: "strategy-set-name", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeStrategyName",
			Access:     api.Write,
			Idempotent: api.Ptr(true),
			Summary:    "Rename a strategy",
			Body:       strategySetNameBody{},
		},
		{
			Name: "strategy-set-amount", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeInvestAmount",
			Access:     api.Write,
			Idempotent: api.Ptr(true),
			Summary:    "Change a strategy's per-period invest amount",
			Body:       strategySetAmountBody{},
		},
		{
			Name: "strategy-set-schedule", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeSchedule",
			Access:     api.Write,
			Idempotent: api.Ptr(true),
			Summary:    "Change a strategy's schedule (cron rule + time zone)",
			Body:       strategySetScheduleBody{},
		},
		{
			Name: "strategy-set-account", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeAccountPayment",
			Access:     api.Write,
			Idempotent: api.Ptr(true),
			Summary:    "Change a strategy's payment account: 0 spot, 1 spot+finance",
			Body:       strategySetAccountBody{},
		},

		// --- whole-strategy / symbol updates (write; nested symbolList) ---
		{
			Name: "strategy-set-symbol", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeSymbol",
			Access:     api.Write,
			Idempotent: api.Ptr(true), // replaces the pair set; re-applying the same set converges
			Summary:    "Replace a strategy's pair configuration (symbolList + percent split)",
			Body:       strategySetSymbolBody{},
		},
		{
			Name: "strategy-set-detail", Method: "POST", Path: "/api/cli/spot/finance/dca/strategy/changeDetail",
			Access:     api.Write,
			Idempotent: api.Ptr(true), // whole-strategy update; re-applying the same config converges
			Summary:    "Update a whole strategy at once (name + invest amount + schedule + symbolList)",
			Body:       strategySetDetailBody{},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.dca"

// Signed is this subdomain's default signing scheme: the CLI gateway scheme.
// The client signs the external (pre-rewrite) request — method, path, canonical
// query, and a hash of the JSON body — with the profile secret and sends the
// signature in a header; the gateway verifies it and resolves the member from
// the API key. This scheme can sign a JSON body, so the POST operations here are
// covered. Operations inherit this.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key in X-MEXC-APIKEY, which identifies the caller and
// sign server-side; there is no client-side signature or login session.
const Identity = identity.OpenAPI
