// Package fixedapi declares the operations of the financial-openapi.fixed sub-domain
// (fixed/term financial operations exposed via openapi gateway).
//
// These endpoints are exposed to clients under the /api/v3/financial/cli/fixed prefix.
// All operations are read-only and require openapi identity.
// The memberId is extracted from request header, not passed as parameter.
package fixedapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Scope is the resource identifier for this sub-domain (domain.sub-domain, no action suffix).
const Scope = "financial-openapi.fixed"

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
			Name: "detail", Method: "GET", Path: "/api/v3/financial/cli/fixed/detail",
			Access:  api.Read,
			Summary: "Query product detail",
			Params: []api.Param{
				{Name: "financialId", In: api.InQuery, Required: true, Desc: "Product ID"},
			},
		},
		{
			Name: "redeem-detail", Method: "GET", Path: "/api/v3/financial/cli/fixed/redeem/detail",
			Access:  api.Read,
			Summary: "Query redeem detail",
			Params: []api.Param{
				{Name: "financialNo", In: api.InQuery, Required: true, Desc: "Product number"},
				{Name: "orderNo", In: api.InQuery, Required: true, Desc: "Order number"},
			},
		},
		{
			Name: "member-pledge-orders-page", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/pledge_orders/page",
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
			Name: "member-redeem-orders-page", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/redeem_orders/page",
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
			Name: "member-profit-record-page", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/profit_record/page",
			Access:  api.Read,
			Summary: "Query paginated profit records",
			Params: []api.Param{
				{Name: "financialNo", In: api.InQuery, Desc: "Product number"},
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "startTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-pledge-currency", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/pledge_orders/currency",
			Access:  api.Read,
			Summary: "Query pledge currencies",
		},
		{
			Name: "member-redeem-currency", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/redeem_orders/currency",
			Access:  api.Read,
			Summary: "Query redeem currencies",
		},
		{
			Name: "member-profit-currency", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/profit_record/currency",
			Access:  api.Read,
			Summary: "Query profit currencies",
		},
		{
			Name: "member-position-detail", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/position_detail",
			Access:  api.Read,
			Summary: "Query position detail",
			Params: []api.Param{
				{Name: "financialNo", In: api.InQuery, Required: true, Desc: "Product number"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-profit-record-summary", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/profit_record_summary",
			Access:  api.Read,
			Summary: "Query profit summary",
		},
		{
			Name: "member-interest-detail", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/interest_detail",
			Access:  api.Read,
			Summary: "Query interest detail",
			Params: []api.Param{
				{Name: "profitRecordNo", In: api.InQuery, Required: true, Desc: "Profit record number"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-redeemable-list", Method: "GET", Path: "/api/v3/financial/cli/fixed/member/redeemable/list",
			Access:  api.Read,
			Summary: "Query redeemable products",
		},
	}
}
