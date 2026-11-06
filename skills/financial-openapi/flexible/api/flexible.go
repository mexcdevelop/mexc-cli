// Package flexibleapi declares the operations of the financial-openapi.flexible sub-domain
// (flexible/demand financial operations exposed via openapi gateway).
//
// These endpoints are exposed to clients under the /api/v3/financial/cli/flexible prefix.
// Auto-pledge settings are write operations; all others are read-only.
// All require openapi identity. The memberId is extracted from request header.
package flexibleapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Scope is the resource identifier for this sub-domain (domain.sub-domain, no action suffix).
const Scope = "financial-openapi.flexible"

// Signed is this subdomain's default signing scheme: every trade operation is
// signed with the MEXC spot HMAC scheme. Operations inherit it (they omit
// the Signed field); an operation overrides only with a concrete api.Sign*
// constant.
const Signed = signing.CliHMAC

// Identity is this subdomain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), not a login session.
const Identity = identity.OpenAPI

// Operations returns all operations of this sub-domain.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "auto-pledge-setting", Method: "GET", Path: "/api/v3/financial/cli/flexible/auto_pledge/setting",
			Access:  api.Write,
			Summary: "Set global auto-pledge switch",
			Params: []api.Param{
				{Name: "autoPledge", In: api.InQuery, Required: true, Enum: []string{"0", "1"}, Desc: "Auto-pledge switch: 0=disable, 1=enable"},
			},
		},
		{
			Name: "auto-pledge-setting-query", Method: "GET", Path: "/api/v3/financial/cli/flexible/auto_pledge/setting/query",
			Access:  api.Read,
			Summary: "Query global auto-pledge state",
		},
		{
			Name: "auto-pledge-financial-setting", Method: "POST", Path: "/api/v3/financial/cli/flexible/auto_pledge/financial/setting",
			Access:  api.Write,
			Summary: "Set auto-pledge per currency (batch)",
			Body: []struct {
				CurrencyID     string `json:"currencyId" validate:"required" desc:"Currency ID"`
				AutoPledgeFlag int    `json:"autoPledgeFlag" validate:"required" enum:"0,1" desc:"Auto-pledge switch: 0=disable, 1=enable"`
			}{},
		},
		{
			Name: "auto-pledge-financial-list", Method: "GET", Path: "/api/v3/financial/cli/flexible/auto_pledge/financial_list",
			Access:  api.Read,
			Summary: "Query auto-pledge config list",
			Params: []api.Param{
				{Name: "currency", In: api.InQuery, Desc: "Currency name for fuzzy search"},
				{Name: "showOpenedOnly", In: api.InQuery, Desc: "Show only enabled auto-pledge configs"},
			},
		},
		{
			Name: "member-pledge-orders-page", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/pledge_orders/page",
			Access:  api.Read,
			Summary: "Query paginated pledge orders",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "startTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-redeemable-list", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/redeemable/list",
			Access:  api.Read,
			Summary: "Query redeemable products",
		},
		{
			Name: "member-redeem-orders-page", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/redeem_orders/page",
			Access:  api.Read,
			Summary: "Query paginated redeem orders",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "startTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-profit-record-page", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/profit_record/page",
			Access:  api.Read,
			Summary: "Query paginated profit records",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "financialNo", In: api.InQuery, Desc: "Product number"},
				{Name: "startTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "apr-history", Method: "GET", Path: "/api/v3/financial/cli/flexible/apr/history",
			Access:  api.Read,
			Summary: "Query APR history",
			Params: []api.Param{
				{Name: "financialId", In: api.InQuery, Required: true, Desc: "Product ID"},
			},
		},
		{
			Name: "detail", Method: "GET", Path: "/api/v3/financial/cli/flexible/detail",
			Access:  api.Read,
			Summary: "Query product detail",
			Params: []api.Param{
				{Name: "financialId", In: api.InQuery, Required: true, Desc: "Product ID"},
			},
		},
		{
			Name: "member-pledge-currency", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/pledge_orders/currency",
			Access:  api.Read,
			Summary: "Query pledge currencies",
		},
		{
			Name: "member-redeem-currency", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/redeem_orders/currency",
			Access:  api.Read,
			Summary: "Query redeem currencies",
		},
		{
			Name: "member-profit-currency", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/profit_record/currency",
			Access:  api.Read,
			Summary: "Query profit currencies",
		},
		{
			Name: "auto-pledge-apr", Method: "GET", Path: "/api/v3/financial/cli/flexible/auto_pledge/apr",
			Access:  api.Read,
			Summary: "Query max auto-pledge APR",
		},
		{
			Name: "auto-pledge-kyc-level", Method: "GET", Path: "/api/v3/financial/cli/flexible/auto_pledge/kyc_level",
			Access:  api.Read,
			Summary: "Query user auto-pledge KYC level",
			Params: []api.Param{
				{Name: "financialId", In: api.InQuery, Desc: "Product ID"},
			},
		},
		{
			Name: "member-position-detail", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/position_detail",
			Access:  api.Read,
			Summary: "Query position detail",
			Params: []api.Param{
				{Name: "financialNo", In: api.InQuery, Required: true, Desc: "Product number"},
			},
		},
		{
			Name: "member-profit-record-summary", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/profit_record_summary",
			Access:  api.Read,
			Summary: "Query profit summary",
		},
		{
			Name: "member-interest-detail", Method: "GET", Path: "/api/v3/financial/cli/flexible/member/interest_detail",
			Access:  api.Read,
			Summary: "Query interest detail",
			Params: []api.Param{
				{Name: "profitRecordNo", In: api.InQuery, Required: true, Desc: "Profit record number"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
	}
}
