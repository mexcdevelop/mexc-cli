// Package marketsearchapi declares signed operations for spot market support.
package marketsearchapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

var booleanValues = []string{"true", "false"}

// Operations returns the market support operations exposed through the isolated
// CLI gateway route family.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "global-search", Method: "GET", Path: "/api/cli/spot/market-support/global/search",
			Access:  api.Read,
			Summary: "Global search across coins, trading pairs, and supported market entities",
			Params: []api.Param{
				{Name: "language", Desc: "Response language, e.g. en-US", In: api.InHeader},
				{Name: "X-Source", Desc: "Optional source identifier", In: api.InHeader},
				{Name: "currency", Desc: "Quote currency filter, e.g. USDT"},
				{Name: "coinId", Desc: "Coin identifier filter"},
				{Name: "keyword", Desc: "Search keyword; at most 100 characters (a longer one returns an empty result upstream without an error)", Required: true, MaxLen: 100},
				{Name: "labels", Desc: "Include labels in the result (default false); labelRelatedProducts is computed only when this and coinLabel are both true", Enum: booleanValues},
				{Name: "coinLabel", Desc: "Include coin labels in the result (default false)", Enum: booleanValues},
				{Name: "predict", Desc: "Include prediction-market results (default false); requires a logged-in account, otherwise this category is always empty", Enum: booleanValues},
			},
		},
	}
}

const Scope = "spot.market-search"

const Signed = signing.CliHMAC

const Identity = identity.OpenAPI
