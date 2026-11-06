// Package assetapi declares the operations of the spot.asset subdomain
// (the asset surface: account overview & balances, per-account asset detail,
// deposit/withdraw history, internal transfers, bill logs, quote prices, and
// spot PnL analysis).
//
// Like the other spot subdomains, this declares api.Operation descriptors only
// — name, path, read/write, params, request body. The framework (internal/cli +
// internal/transport) handles HTTP, signing, credential injection, and command
// assembly. scope is always spot.asset (a resource identifier, no action
// suffix); the action lives in each Operation's Access.
//
// Every operation here is signed with the MEXC spot HMAC scheme and carries the
// openapi API-key identity (see const Signed / const Identity below), so each op
// omits those fields and inherits the subdomain defaults. The only writes are
// the two fund-moving transfers (transfer / sub-transfer); everything else is a
// read, including the POST bill queries whose filters travel in a JSON body.
//
// Path is the external / gateway-side path the CLI calls (the "openapi" path).
// The gateway rewrites it to the internal service path and injects the resolved
// member identity as a request header, so the caller never passes a member id —
// it is not declared as a param.
package assetapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// subAccountTypes are the allowed values for sub-account-asset's --accountType
// flag. Only the spot account is queryable today.
var subAccountTypes = []string{"SPOT"}

// spotFuturesTypes are the account types a sub-account universal transfer moves
// between (--fromAccountType / --toAccountType).
var spotFuturesTypes = []string{"SPOT", "FUTURES"}

// spotFuturesPredictTypes are the account types an in-station transfer and its
// history query accept.
var spotFuturesPredictTypes = []string{"SPOT", "FUTURES", "PREDICT"}

// SpotBillQuery is the JSON request body of the spot bill log / summary queries
// (POST). mainType and the time range are required; the rest narrow the query.
// The member id is resolved from the API key and is not part of the body.
type SpotBillQuery struct {
	MainType    string   `json:"mainType" validate:"required" desc:"bill business main type (query the type list first)"`
	StartTime   int64    `json:"startTime" validate:"required" desc:"range start, Unix milliseconds"`
	EndTime     int64    `json:"endTime" validate:"required" desc:"range end, Unix milliseconds"`
	Currency    string   `json:"currency,omitempty" desc:"filter by asset, e.g. USDT"`
	VCoinID     string   `json:"vCoinId,omitempty" desc:"filter by internal coin id"`
	TransType   int      `json:"transType,omitempty" desc:"transaction direction filter"`
	SubTypeList []string `json:"subTypeList,omitempty" desc:"filter by bill sub types"`
	PageNum     int      `json:"pageNum,omitempty" desc:"page number, starts at 1"`
	PageSize    int      `json:"pageSize,omitempty" desc:"rows per page"`
}

// FuturesBillQuery is the JSON request body of the futures bill log / summary
// queries (POST). mainType and the time range are required.
type FuturesBillQuery struct {
	MainType  string `json:"mainType" validate:"required" desc:"bill business main type (query the type list first)"`
	StartTime int64  `json:"startTime" validate:"required" desc:"range start, Unix milliseconds"`
	EndTime   int64  `json:"endTime" validate:"required" desc:"range end, Unix milliseconds"`
	Currency  string `json:"currency,omitempty" desc:"filter by asset, e.g. USDT"`
	InOutType string `json:"inOutType,omitempty" desc:"income/expense direction filter"`
	SubType   string `json:"subType,omitempty" desc:"filter by bill sub type"`
	PageNum   int    `json:"pageNum,omitempty" desc:"page number, starts at 1"`
	PageSize  int    `json:"pageSize,omitempty" desc:"rows per page"`
}

// Operations returns every operation of this subdomain. The framework assembles
// commands from it, and the contract test self-checks against it.
func Operations() []api.Operation {
	return []api.Operation{
		// ---------------- account overview & info ----------------
		{
			Name: "account", Method: "GET", Path: "/api/v3/account",
			Access:  api.Read,
			Summary: "Current account info (balances and account state)",
		},
		{
			Name: "overview", Method: "GET", Path: "/api/v3/account/overview",
			Access:  api.Read,
			Summary: "Converted asset overview across all accounts",
		},
		{
			Name: "sub-account-asset", Method: "GET", Path: "/api/v3/sub-account/asset",
			Access:  api.Read,
			Summary: "A master account's view of a sub-account's asset",
			Params: []api.Param{
				{Name: "subAccount", Desc: "sub-account login account", Required: true},
				{Name: "accountType", Desc: "account type to query", Required: true, Enum: subAccountTypes},
			},
		},

		// ---------------- per-account asset detail ----------------
		{
			Name: "assets-spot", Method: "GET", Path: "/api/v3/account/assets/spot",
			Access:  api.Read,
			Summary: "Spot account converted asset detail",
		},
		{
			Name: "assets-futures", Method: "GET", Path: "/api/v3/account/assets/futures",
			Access:  api.Read,
			Summary: "Futures account converted asset detail",
		},
		{
			Name: "assets-otc", Method: "GET", Path: "/api/v3/account/assets/otc",
			Access:  api.Read,
			Summary: "Fiat (OTC) account converted asset detail",
		},
		{
			Name: "assets-strategy", Method: "GET", Path: "/api/v3/account/assets/strategy",
			Access:  api.Read,
			Summary: "Strategy-trading account converted asset detail",
		},
		{
			Name: "assets-robot", Method: "GET", Path: "/api/v3/account/assets/robot",
			Access:  api.Read,
			Summary: "Trading-bot account converted asset detail",
		},
		{
			Name: "assets-alpha", Method: "GET", Path: "/api/v3/account/assets/alpha",
			Access:  api.Read,
			Summary: "Alpha account converted asset detail",
		},
		{
			Name: "assets-predict", Method: "GET", Path: "/api/v3/account/assets/predict",
			Access:  api.Read,
			Summary: "Prediction-market account converted asset detail",
		},
		{
			Name: "assets-stock", Method: "GET", Path: "/api/v3/account/assets/stock",
			Access:  api.Read,
			Summary: "Stock account asset (--currency defaults to USD)",
			Params: []api.Param{
				{Name: "currency", Desc: "quote currency, defaults to USD"},
			},
		},
		{
			Name: "assets-financial", Method: "GET", Path: "/api/v3/account/assets/financial",
			Access:  api.Read,
			Summary: "Financial (earn) account converted asset detail",
		},

		// ---------------- deposit / withdraw history ----------------
		{
			Name: "deposit-history", Method: "GET", Path: "/api/v3/capital/deposit/hisrec",
			Access:  api.Read,
			Summary: "Deposit history (--limit default 1000, max 1000)",
			Params: []api.Param{
				{Name: "coin", Desc: "asset name, e.g. USDT; required when network is set"},
				{Name: "network", Desc: "chain/network, e.g. TRX; requires coin"},
				{Name: "status", Desc: "deposit status filter, e.g. 5=SUCCESS"},
				{Name: "startTime", Desc: "range start, Unix milliseconds"},
				{Name: "endTime", Desc: "range end, Unix milliseconds"},
				{Name: "limit", Desc: "rows per page, default 1000, max 1000"},
			},
		},
		{
			Name: "withdraw-history", Method: "GET", Path: "/api/v3/capital/withdraw/history",
			Access:  api.Read,
			Summary: "Withdrawal history (--limit default 1000, max 1000)",
			Params: []api.Param{
				{Name: "coin", Desc: "asset name, e.g. USDT; required when network is set"},
				{Name: "network", Desc: "chain/network; requires coin"},
				{Name: "status", Desc: "withdrawal status filter, 0=pending 1=success"},
				{Name: "startTime", Desc: "range start, Unix milliseconds"},
				{Name: "endTime", Desc: "range end, Unix milliseconds"},
				{Name: "limit", Desc: "rows per page, default 1000, max 1000"},
			},
		},

		// ---------------- deposit networks & addresses ----------------
		// deposit-chains lists a coin's supported deposit networks; each row
		// carries the walletType needed by deposit-address's --netWork. Query
		// the chains first, then feed the chosen walletType into the address
		// lookup (see the SKILL.md "Get a deposit address" workflow).
		//
		// Path is the gateway path (mexc-client -> gateway -> open-api); the
		// gateway rewrites it to the open-api /api/v3/open/... path. Query params
		// are forwarded verbatim, so the flag name IS the backend wire key.
		{
			Name: "deposit-chains", Method: "GET", Path: "/api/v3/capital/deposit/chain/list",
			Access:  api.Read,
			Summary: "List a coin's supported deposit networks (chains)",
			Params: []api.Param{
				{Name: "coin", Desc: "asset name, same as currency, e.g. USDT", Required: true},
			},
		},
		{
			Name: "deposit-address", Method: "GET", Path: "/api/v3/capital/deposit/address/v2",
			Access: api.Read,
			Summary: "Query a coin's deposit addresses (optionally filtered by " +
				"--network chain label or --netWork walletType)",
			Params: []api.Param{
				{Name: "coin", Desc: "asset name, e.g. USDT", Required: true},
				{Name: "network", Desc: "chain label filter, e.g. TRC20 (the chainName; optional)"},
				// netWork differs from network only by capitalization: it is the
				// walletType (virtual-coin series) taken from a deposit-chains row,
				// NOT the chain label. The name mirrors the backend wire param, which
				// the framework sends verbatim (flag name == query key).
				{Name: "netWork", Desc: "walletType filter from a deposit-chains row (not the chain label; optional)"},
			},
		},

		// ---------------- transfers ----------------
		{
			Name: "transfer", Method: "POST", Path: "/api/v3/capital/transfer",
			Access: api.Write,
			// A fund transfer moves balance irreversibly and repeating it would
			// transfer again, so it is destructive AND non-idempotent (RiskTier
			// Heavy: strongest confirmation, never auto-retried).
			Destructive: api.Ptr(true), Idempotent: api.Ptr(false),
			Summary: "Transfer between own accounts (spot/futures/predict)",
			Params: []api.Param{
				{Name: "fromAccountType", Desc: "source account type", Required: true, Enum: spotFuturesPredictTypes},
				{Name: "toAccountType", Desc: "target account type", Required: true, Enum: spotFuturesPredictTypes},
				{Name: "asset", Desc: "asset to transfer, e.g. USDT", Required: true},
				{Name: "amount", Desc: "amount to transfer", Required: true},
				{Name: "symbol", Desc: "trading pair, for futures/predict scenarios"},
			},
		},
		{
			Name: "sub-transfer", Method: "POST", Path: "/api/v3/capital/sub-account/universalTransfer",
			Access:      api.Write,
			Destructive: api.Ptr(true), Idempotent: api.Ptr(false),
			Summary: "Universal transfer between master and sub-accounts",
			Params: []api.Param{
				{Name: "fromAccountType", Desc: "source account type", Required: true, Enum: spotFuturesTypes},
				{Name: "toAccountType", Desc: "target account type", Required: true, Enum: spotFuturesTypes},
				{Name: "fromAccount", Desc: "source account; fromAccount or toAccount must be set"},
				{Name: "toAccount", Desc: "target account; fromAccount or toAccount must be set"},
				{Name: "asset", Desc: "asset to transfer, e.g. USDT", Required: true},
				{Name: "amount", Desc: "amount to transfer", Required: true},
				{Name: "symbol", Desc: "trading pair, for futures scenarios"},
				{Name: "clientTranId", Desc: "client idempotency id (optional)"},
			},
		},
		{
			Name: "transfer-history", Method: "GET", Path: "/api/v3/capital/transfer",
			Access:  api.Read,
			Summary: "In-station transfer history",
			Params: []api.Param{
				{Name: "fromAccountType", Desc: "source account type", Required: true, Enum: spotFuturesPredictTypes},
				{Name: "toAccountType", Desc: "target account type", Required: true, Enum: spotFuturesPredictTypes},
				{Name: "asset", Desc: "filter by asset, e.g. USDT"},
				{Name: "symbol", Desc: "filter by trading pair, e.g. BTCUSDT"},
				{Name: "startTime", Desc: "range start, Unix milliseconds"},
				{Name: "endTime", Desc: "range end, Unix milliseconds"},
				{Name: "page", Desc: "page number, starts at 1"},
				{Name: "size", Desc: "rows per page, default 10"},
			},
		},
		{
			Name: "sub-transfer-history", Method: "GET", Path: "/api/v3/capital/sub-account/universalTransfer",
			Access:  api.Read,
			Summary: "Master/sub-account universal transfer history",
			Params: []api.Param{
				{Name: "fromAccountType", Desc: "source account type", Required: true, Enum: spotFuturesTypes},
				{Name: "toAccountType", Desc: "target account type", Required: true, Enum: spotFuturesTypes},
				{Name: "fromAccount", Desc: "filter by source account"},
				{Name: "toAccount", Desc: "filter by target account"},
				{Name: "startTime", Desc: "range start, Unix milliseconds"},
				{Name: "endTime", Desc: "range end, Unix milliseconds"},
				{Name: "page", Desc: "page number, starts at 1"},
				{Name: "limit", Desc: "rows per page, default 10"},
			},
		},
		{
			Name: "transfer-detail", Method: "GET", Path: "/api/v3/capital/transfer/tranId",
			Access:  api.Read,
			Summary: "A single in-station transfer record by tranId",
			Params: []api.Param{
				{Name: "tranId", Desc: "transfer serial number", Required: true},
			},
		},
		{
			Name: "transfer-internal-history", Method: "GET", Path: "/api/v3/capital/transfer/internal",
			Access:  api.Read,
			Summary: "User-to-user internal transfer history",
			Params: []api.Param{
				{Name: "tranId", Desc: "filter by transfer serial number"},
				{Name: "startTime", Desc: "range start, Unix milliseconds"},
				{Name: "endTime", Desc: "range end, Unix milliseconds; window with startTime <= 7 days"},
				{Name: "page", Desc: "page number, starts at 1"},
				{Name: "limit", Desc: "rows per page, default 10"},
			},
		},

		// ---------------- bill ----------------
		{
			Name: "bill-spot-types", Method: "GET", Path: "/api/v3/account/bill/spot/types",
			Access:  api.Read,
			Summary: "Spot bill query types (main types)",
		},
		{
			Name: "bill-spot-logs", Method: "POST", Path: "/api/v3/account/bill/spot/logs",
			Access:  api.Read,
			Summary: "Spot bill log list (filters in --data)",
			Body:    SpotBillQuery{},
		},
		{
			Name: "bill-spot-summary", Method: "POST", Path: "/api/v3/account/bill/spot/summary",
			Access:  api.Read,
			Summary: "Spot bill summary (filters in --data)",
			Body:    SpotBillQuery{},
		},
		{
			Name: "bill-futures-types", Method: "GET", Path: "/api/v3/account/bill/futures/types",
			Access:  api.Read,
			Summary: "Futures bill query types (main types)",
		},
		{
			Name: "bill-futures-logs", Method: "POST", Path: "/api/v3/account/bill/futures/logs",
			Access:  api.Read,
			Summary: "Futures bill log list (filters in --data)",
			Body:    FuturesBillQuery{},
		},
		{
			Name: "bill-futures-summary", Method: "POST", Path: "/api/v3/account/bill/futures/summary",
			Access:  api.Read,
			Summary: "Futures bill summary (filters in --data)",
			Body:    FuturesBillQuery{},
		},

		// ---------------- quote prices ----------------
		{
			Name: "usdt-main-prices", Method: "GET", Path: "/api/v3/common/usdt/main/prices",
			Access:  api.Read,
			Summary: "USDT-converted prices for main quote coins",
		},
		{
			Name: "usdt-all-prices", Method: "GET", Path: "/api/v3/common/usdt/all/prices",
			Access:  api.Read,
			Summary: "USDT-converted prices for all coins",
		},
		{
			Name: "cny-prices", Method: "GET", Path: "/api/v3/common/cny/prices",
			Access:  api.Read,
			Summary: "CNY prices for all coins",
		},
		{
			Name: "exchange-rate", Method: "GET", Path: "/api/v3/common/currency/exchange/rate",
			Access:  api.Read,
			Summary: "Fiat currency exchange rates",
		},

		// ---------------- spot PnL analysis ----------------
		{
			Name: "pnl-overview", Method: "GET", Path: "/api/v3/account/pnl/spot/overview",
			Access:  api.Read,
			Summary: "Spot profit-and-loss overview",
		},
		{
			Name: "pnl-detail", Method: "GET", Path: "/api/v3/account/pnl/spot/detail",
			Access:  api.Read,
			Summary: "Spot profit-and-loss daily detail (range <= 180 days)",
			Params: []api.Param{
				{Name: "from", Desc: "range start, Unix milliseconds", Required: true},
				{Name: "to", Desc: "range end, Unix milliseconds", Required: true},
			},
		},
	}
}

// Scope is this subdomain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "spot.asset"

// Signed is this subdomain's default signing scheme: every asset operation is
// signed with the MEXC spot HMAC scheme (X-MEXC-APIKEY + HMAC-SHA256 over the
// query). Operations inherit it (they omit the Signed field); an operation
// overrides only with a concrete signing.* constant.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), not a login session.
const Identity = identity.OpenAPI
