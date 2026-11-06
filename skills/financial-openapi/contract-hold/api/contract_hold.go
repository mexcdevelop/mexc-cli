// Package contractholdapi declares the operations of the financial-openapi.contract-hold sub-domain
// (contract-hold financial operations exposed via openapi gateway).
//
// These endpoints are exposed to clients under the /api/v3/financial/cli/contract_hold prefix.
// Most operations are read-only except register (write). All require openapi identity (api-key auth).
// The memberId is extracted from request header, not passed as parameter.
package contractholdapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Scope is the resource identifier for this sub-domain (domain.sub-domain, no action suffix).
const Scope = "financial-openapi.contract-hold"

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
			Name: "products-list", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/products/list",
			Access:  api.Read,
			Summary: "Query product list",
		},
		{
			Name: "products-max-apr", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/products/max_apr",
			Access:  api.Read,
			Summary: "Query max APR",
		},
		{
			Name: "products-max-base-apr", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/products/max_base_apr",
			Access:  api.Read,
			Summary: "Query max base APR",
		},
		{
			Name: "member-asset", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/member/asset",
			Access:  api.Read,
			Summary: "Query member asset summary",
		},
		{
			Name: "register", Method: "POST", Path: "/api/v3/financial/cli/contract_hold/register",
			Access:  api.Write,
			Summary: "Modify auto-registration switch",
			Body: struct {
				SwitchFlag bool `json:"switchFlag" validate:"required" desc:"Auto-registration switch: true=enable, false=disable"`
			}{},
		},
		{
			Name: "register-query", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/register/query",
			Access:  api.Read,
			Summary: "Query registration state",
		},
		{
			Name: "member-profit-record", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/member/profit_record",
			Access:  api.Read,
			Summary: "Query paginated profit records",
			Params: []api.Param{
				{Name: "currencyId", In: api.InQuery, Desc: "Profit currency ID"},
				{Name: "startTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "endTime", In: api.InQuery, Pattern: "[0-9]{13}", Desc: "UTC Unix timestamp in milliseconds (13 digits)"},
				{Name: "page", In: api.InQuery, Desc: "Page number"},
				{Name: "pageSize", In: api.InQuery, Desc: "Page size"},
			},
		},
		{
			Name: "member-profit-currency", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/member/profit_record/currency",
			Access:  api.Read,
			Summary: "Query profit record currencies",
		},
		{
			Name: "member-positions", Method: "GET", Path: "/api/v3/financial/cli/contract_hold/member/positions",
			Access:  api.Read,
			Summary: "Query member positions",
		},
		{
			Name: "profit-calculator", Method: "POST", Path: "/api/v3/financial/cli/contract_hold/profit_calculator/calculate",
			Access:  api.Read,
			Summary: "Calculate estimated profit",
			Body: struct {
				FinancialNo       string  `json:"financialNo" validate:"required" minlen:"1" desc:"Non-blank target number"`
				PrincipalQuantity float64 `json:"principalQuantity" validate:"required" desc:"Principal amount; non-positive values produce a zero result"`
				PositionQuantity  float64 `json:"positionQuantity,omitempty" desc:"Optional position value; negative values are treated as zero by the backend"`
				InvestDays        int     `json:"investDays" validate:"required" minval:"0" maxval:"1100" desc:"Investment days from 0 to 1100"`
			}{},
		},
	}
}
