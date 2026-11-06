// Package accountapi declares the operations of the contract.account sub-domain
// (private account data: assets, deal history, multi-asset mode, fee rates, user
// preferences/profile). Every operation is signed and carries the API-key
// identity — reads and writes coexist, so write semantics are set explicitly.
//
// Declare only Operation descriptors (name / path /
// access / params / body). The framework sends HTTP, signs, injects the
// credential, and mounts commands. scope is always contract.account; the action
// lives in each Operation's Access. The logged-in user is identified by the
// API-key credential (and, at the gateway, by injected headers) — never a caller
// parameter, and never placed in a request body.
package accountapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// ---- Request body prototypes (POST JSON bodies) ----

// multiAssetPayDebtBody is the body of pre-pay-debt.
type multiAssetPayDebtBody struct {
	Currency    string `json:"currency" validate:"required" desc:"borrowed currency to repay"`
	PayCurrency string `json:"payCurrency" validate:"required" desc:"currency used to repay"`
}

// leverageModeChangeBody is the body of change-leverage-mode.
type leverageModeChangeBody struct {
	LeverageMode int `json:"leverageMode" validate:"required" enum:"1,2" desc:"leverage mode: 1 advanced, 2 simple"`
}

// Operations returns every operation of this sub-domain. The framework assembles
// commands from it, and the contract test self-checks against it. Signed/Identity
// are omitted on every op — all inherit the subdomain defaults (signed, openapi).
func Operations() []api.Operation {
	return []api.Operation{
		// ---- Assets ----
		{
			Name: "assets", Method: "GET", Path: "/api/v1/private/account/assets",
			Access:  api.Read,
			Summary: "All account assets",
		},
		{
			Name: "asset", Method: "GET", Path: "/api/v1/private/account/asset/{currency}",
			Access:  api.Read,
			Summary: "Asset of a specific currency",
			Params: []api.Param{
				{Name: "currency", Desc: "Currency, e.g. USDT", Required: true, In: api.InPath},
				{Name: "pointEnabled", Desc: "Include MX point; default false"},
			},
		},
		{
			Name: "asset-book-v2", Method: "GET", Path: "/api/v1/private/account/asset_book/v2",
			Access:  api.Read,
			Summary: "Funds flow records V2 (paged, id cursor)",
			Params: []api.Param{
				{Name: "currency", Desc: "Currency filter"},
				{Name: "beginTime", Desc: "Start time (Unix milliseconds)"},
				{Name: "endTime", Desc: "End time (Unix milliseconds)"},
				{Name: "type", Desc: "Record type filter"},
				{Name: "symbol", Desc: "Contract name filter, e.g. BTC_USDT"},
				{Name: "inOutType", Desc: "In/out type filter"},
				{Name: "startId", Desc: "Cursor: start record id for the next page"},
				{Name: "currentPage", Desc: "Page number, starting at 1; default 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 50, max 500", MinVal: f(1), MaxVal: f(500)},
			},
		},
		{
			Name: "asset-book-order-deal-fee-total", Method: "GET", Path: "/api/v1/private/account/asset_book/order_deal_fee/total",
			Access:  api.Read,
			Summary: "Total trading fee over the last 30 days",
		},

		// ---- Multi-asset (unified margin) mode ----
		{
			Name: "get-multi-asset-mode", Method: "GET", Path: "/api/v1/private/multiAssets/getMultiAssetMode",
			Access:  api.Read,
			Summary: "Current unified (multi-asset) margin mode",
		},
		{
			Name: "change-multi-asset-mode", Method: "POST", Path: "/api/v1/private/multiAssets/changeMultiAssetMode/{isMultiAssetMode}",
			Access:      api.Write,
			Idempotent:  api.Ptr(true),
			Destructive: api.Ptr(false),
			Summary:     "Switch unified (multi-asset) margin mode on/off",
			Params: []api.Param{
				{Name: "isMultiAssetMode", Desc: "true to enable unified margin mode, false to disable", Required: true, In: api.InPath, Enum: boolValues},
			},
		},
		{
			Name: "get-multi-assets", Method: "GET", Path: "/api/v1/private/multiAssets/getMultiAssets",
			Access:  api.Read,
			Summary: "Asset info under unified (multi-asset) margin mode (USDT-valued)",
		},
		{
			Name: "page-multi-assets-interest-record", Method: "GET", Path: "/api/v1/private/multiAssets/pageMultiAssetsInterestRecord",
			Access:  api.Read,
			Summary: "Unified-margin interest settlement records (paged)",
			Params: []api.Param{
				{Name: "startTime", Desc: "Start time (Unix milliseconds)"},
				{Name: "endTime", Desc: "End time (Unix milliseconds)"},
				{Name: "currency", Desc: "Currency filter"},
				{Name: "pageIndex", Desc: "Page number, starting at 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 20, max 200", MinVal: f(1), MaxVal: f(200)},
			},
		},
		{
			Name: "page-multi-assets-debt-repayment-record", Method: "GET", Path: "/api/v1/private/multiAssets/pageMultiAssetsDebtRepaymentRecord",
			Access:  api.Read,
			Summary: "Unified-margin debt repayment records (paged)",
			Params: []api.Param{
				{Name: "startTime", Desc: "Start time (Unix milliseconds)"},
				{Name: "endTime", Desc: "End time (Unix milliseconds)"},
				{Name: "pageIndex", Desc: "Page number, starting at 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 20, max 200", MinVal: f(1), MaxVal: f(200)},
			},
		},
		{
			Name: "list-debt-repayment-record-detail-by-record-id", Method: "GET", Path: "/api/v1/private/multiAssets/listDebtRepaymentRecordDetailByRecordId",
			Access:  api.Read,
			Summary: "Debt repayment detail by repayment record id",
			Params: []api.Param{
				{Name: "debtRepaymentRecordId", Desc: "Debt repayment record id", Required: true},
			},
		},
		{
			Name: "pre-pay-debt", Method: "POST", Path: "/api/v1/private/multiAssets/prePayDebt",
			Access:      api.Write,
			Idempotent:  api.Ptr(true),
			Destructive: api.Ptr(false),
			Summary:     "Preview a unified-margin debt repayment",
			Body:        multiAssetPayDebtBody{},
		},
		{
			Name: "pay-debt", Method: "POST", Path: "/api/v1/private/multiAssets/payDebt/{businessId}",
			Access:  api.Write,
			Summary: "Repay unified-margin debt for a prepared business id",
			Params: []api.Param{
				{Name: "businessId", Desc: "Business id returned by pre-pay-debt", Required: true, In: api.InPath},
			},
		},
		{
			Name: "list-all-pledge-currency-config", Method: "GET", Path: "/api/v1/private/multiAssets/listAllPledgeCurrencyConfig",
			Access:  api.Read,
			Summary: "All pledge (collateral) currency configs",
		},
		{
			Name: "list-all-debt-currency-config", Method: "GET", Path: "/api/v1/private/multiAssets/listAllDebtCurrencyConfig",
			Access:  api.Read,
			Summary: "All borrowable currency configs",
		},

		// ---- Deal history ----
		{
			Name: "statistics-deal-amount", Method: "GET", Path: "/api/v1/private/deal_history/statistics_deal_amount",
			Access:  api.Read,
			Summary: "Traded-amount statistics",
			Params: []api.Param{
				{Name: "startTime", Desc: "Start time (Unix milliseconds)"},
				{Name: "endTime", Desc: "End time (Unix milliseconds)"},
				{Name: "symbol", Desc: "Contract name filter, e.g. BTC_USDT"},
			},
		},

		// ---- Fee rate / discount ----
		{
			Name: "tiered-fee-rate-v2", Method: "GET", Path: "/api/v1/private/account/tiered_fee_rate/v2",
			Access:  api.Read,
			Summary: "Fee detail V2 (discount / deduction / fee)",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT"},
			},
		},
		{
			Name: "tiered-fee-rate-v2-next-level", Method: "GET", Path: "/api/v1/private/account/tiered_fee_rate/v2/next_level",
			Access:  api.Read,
			Summary: "Next-level fee rate after pass-through",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT"},
			},
		},
		{
			Name: "contract-fee-rate", Method: "GET", Path: "/api/v1/private/account/contract/fee_rate",
			Access:  api.Read,
			Summary: "Fee detail of a specific contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT"},
			},
		},
		{
			Name: "contract-fee-rate-range", Method: "GET", Path: "/api/v1/private/account/contract/fee_rate_range",
			Access:  api.Read,
			Summary: "Contract fee-rate range",
		},
		{
			Name: "fee-deduct-configs", Method: "GET", Path: "/api/v1/private/account/feeDeductConfigs",
			Access:  api.Read,
			Summary: "Fee-deduction configs",
		},
		{
			Name: "contract-fee-discount-config", Method: "GET", Path: "/api/v1/private/account/config/contractFeeDiscountConfig",
			Access:  api.Read,
			Summary: "Spot-holding fee-discount configs",
		},

		// ---- Risk / profile / preferences ----
		{
			Name: "risk-limit", Method: "GET", Path: "/api/v1/private/account/risk_limit",
			Access:  api.Read,
			Summary: "Risk limit",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT"},
			},
		},
		{
			Name: "risk-level", Method: "GET", Path: "/api/v1/private/profile/risk_level",
			Access:  api.Read,
			Summary: "User risk level",
			Params: []api.Param{
				{Name: "contractId", Desc: "Contract id (optional)"},
			},
		},
		{
			Name: "kyc-trade-limit", Method: "GET", Path: "/api/v1/private/profile/kyc/trade_limit",
			Access:  api.Read,
			Summary: "KYC trade limit",
		},
		{
			Name: "leverage-mode", Method: "GET", Path: "/api/v1/private/user_pref/leverage_mode",
			Access:  api.Read,
			Summary: "Leverage mode preference",
		},
		{
			Name: "change-leverage-mode", Method: "POST", Path: "/api/v1/private/user_pref/change_leverage_mode",
			Access:      api.Write,
			Idempotent:  api.Ptr(true),
			Destructive: api.Ptr(false),
			Summary:     "Change the leverage mode preference",
			Body:        leverageModeChangeBody{},
		},
	}
}

// boolValues constrains a boolean path/query parameter.
var boolValues = []string{"true", "false"}

// f returns a pointer to a numeric bound, for the Param MinVal/MaxVal fields.
func f(v float64) *float64 { return &v }

// Scope is this sub-domain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "contract.account"

// Signed is this sub-domain's default signing scheme: every account operation is
// signed with the MEXC spot HMAC scheme. Operations inherit it (they omit the
// Signed field); an operation overrides only with a concrete api.Sign* constant.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), not a login session.
const Identity = identity.OpenAPI
