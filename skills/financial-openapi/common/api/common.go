// Package commonapi declares the operations of the financial-openapi.common sub-domain
// (common financial operations exposed via openapi gateway).
//
// These endpoints are exposed to clients under the /api/v3/financial/cli/common prefix.
// All operations are read-only and require openapi identity (api-key auth). The memberId is
// extracted from request header, not passed as parameter.
package commonapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Scope is the resource identifier for this sub-domain (domain.sub-domain, no action suffix).
const Scope = "financial-openapi.common"

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
			Name: "products-list-v2", Method: "GET", Path: "/api/v3/financial/cli/common/products/list/V2",
			Access:  api.Read,
			Summary: "Query product list (V2, non-paginated)",
			Params: []api.Param{
				{Name: "currency", In: api.InQuery, Desc: "Currency name for fuzzy search"},
				{Name: "investPeriodType", In: api.InQuery, Desc: "Investment period type: FLEXIBLE/FIXED"},
				{Name: "matchMemberAssets", In: api.InQuery, Desc: "Whether to match member's assets"},
				{Name: "sortField", In: api.InQuery, Desc: "Sort field: 1=estimated APR"},
				{Name: "asc", In: api.InQuery, Desc: "Sort direction: true=ascending"},
			},
		},
		{
			Name: "products-max-apr", Method: "GET", Path: "/api/v3/financial/cli/common/products/max_apr",
			Access:  api.Read,
			Summary: "Query maximum APR across products",
		},
		{
			Name: "products-by-currency", Method: "GET", Path: "/api/v3/financial/cli/common/products/by_currency",
			Access:  api.Read,
			Summary: "Query products by currency ID (for pledge popup)",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Required: true, Desc: "Currency ID (required)"},
			},
		},
		{
			Name: "member-assets-v2", Method: "GET", Path: "/api/v3/financial/cli/common/member/assets/v2",
			Access:  api.Read,
			Summary: "Query member assets (V2, excludes on-chain earn)",
		},
		{
			Name: "member-positions", Method: "GET", Path: "/api/v3/financial/cli/common/member/positions",
			Access:  api.Read,
			Summary: "Query member positions by currency",
			Params: []api.Param{
				{Name: "currency", In: api.InQuery, Desc: "Currency name for fuzzy search"},
				{Name: "hideSmall", In: api.InQuery, Desc: "Hide small assets (<1 USD)"},
			},
		},
		{
			Name: "member-positions-by-product-v2", Method: "GET", Path: "/api/v3/financial/cli/common/member/positions_by_product/v2",
			Access:  api.Read,
			Summary: "Query positions by product (V2, batch)",
			Params: []api.Param{
				{Name: "financialTypes", In: api.InQuery, Desc: "Financial types: FLEXIBLE/FIXED/BLC_EARN"},
			},
		},
		{
			Name: "member-recommend", Method: "GET", Path: "/api/v3/financial/cli/common/member/recommend",
			Access:  api.Read,
			Summary: "Query recommended products for member",
			Params: []api.Param{
				{Name: "financialType", In: api.InQuery, Desc: "Financial type: FLEXIBLE/FIXED/BLC_EARN"},
			},
		},
		{
			Name: "member-pledge-orders", Method: "GET", Path: "/api/v3/financial/cli/common/member/pledge_orders",
			Access:  api.Read,
			Summary: "Query member pledge orders",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "financialType", In: api.InQuery, Desc: "Financial type"},
				{Name: "startTime", In: api.InQuery, Required: true, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Required: true, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
			},
		},
		{
			Name: "member-redeem-orders", Method: "GET", Path: "/api/v3/financial/cli/common/member/redeem_orders",
			Access:  api.Read,
			Summary: "Query member redeem orders",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Currency ID"},
				{Name: "financialType", In: api.InQuery, Desc: "Financial type"},
				{Name: "startTime", In: api.InQuery, Required: true, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Required: true, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
			},
		},
		{
			Name: "member-pledge-currency", Method: "GET", Path: "/api/v3/financial/cli/common/member/pledge_orders/currency",
			Access:  api.Read,
			Summary: "Query pledge order currencies",
		},
		{
			Name: "member-redeem-currency", Method: "GET", Path: "/api/v3/financial/cli/common/member/redeem_orders/currency",
			Access:  api.Read,
			Summary: "Query redeem order currencies",
		},
	}
}
