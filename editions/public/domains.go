package public

import (
	"github.com/mexcdevelop/mexc-cli/editions/public/adapter"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	onlineapi "github.com/mexcdevelop/mexc-cli/skills/activity/online/api"
	participateapi "github.com/mexcdevelop/mexc-cli/skills/activity/participate/api"
	rewardapi "github.com/mexcdevelop/mexc-cli/skills/activity/reward/api"
	contractaccountapi "github.com/mexcdevelop/mexc-cli/skills/contract/account/api"
	contractmarketapi "github.com/mexcdevelop/mexc-cli/skills/contract/market/api"
	contracttradeapi "github.com/mexcdevelop/mexc-cli/skills/contract/trade/api"
	commonapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/common/api"
	contractholdapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/contract-hold/api"
	fixedapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/fixed/api"
	flexibleapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/flexible/api"
	futuresaccountapi "github.com/mexcdevelop/mexc-cli/skills/futures/account/api"
	futuresmarketapi "github.com/mexcdevelop/mexc-cli/skills/futures/market/api"
	accountapi "github.com/mexcdevelop/mexc-cli/skills/spot/account/api"
	assetapi "github.com/mexcdevelop/mexc-cli/skills/spot/asset/api"
	convertapi "github.com/mexcdevelop/mexc-cli/skills/spot/convert/api"
	dcaapi "github.com/mexcdevelop/mexc-cli/skills/spot/dca/api"
	feeapi "github.com/mexcdevelop/mexc-cli/skills/spot/fee/api"
	gridapi "github.com/mexcdevelop/mexc-cli/skills/spot/grid/api"
	klineapi "github.com/mexcdevelop/mexc-cli/skills/spot/kline/api"
	marketsearchapi "github.com/mexcdevelop/mexc-cli/skills/spot/market-search/api"
	marketv2api "github.com/mexcdevelop/mexc-cli/skills/spot/marketv2/api"
	tradeapi "github.com/mexcdevelop/mexc-cli/skills/spot/trade/api"
)

// domains returns the public edition's business topology. It is the single
// source of truth shared by command assembly and the `schema`/`mcp`/`explore`
// faces (the engine derives them from Edition.Domains), so they never drift. To
// add a PUBLIC subdomain, add one entry here — its command tree and schema entry
// follow automatically. Signed/Identity wire the subdomain's auth defaults from
// its api package constants; an Operation inherits them unless it overrides with
// a concrete value (see engine mountSubdomain).
func domains() []api.DomainSpec {
	return []api.DomainSpec{
		{
			Name:  "spot",
			Short: "Spot: market data / account / trading",
			Subs: []api.Subdomain{
				{
					Name:     "marketv2",
					Short:    "Spot market data V2: public REST and WS data; selected reads require credentials",
					Signed:   marketv2api.Signed,
					Identity: marketv2api.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      marketv2api.Operations(),
				},
				{
					Name:     "kline",
					Short:    "Spot public kline data",
					Signed:   klineapi.Signed,
					Identity: klineapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      klineapi.Operations(),
				},
				{
					Name:     "market-search",
					Short:    "Spot CLI gateway market search (signed)",
					Signed:   marketsearchapi.Signed,
					Identity: marketsearchapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      marketsearchapi.Operations(),
				},
				{
					Name:     "account",
					Short:    "Spot account: the account's own new-trade stream (WS, signed preflight)",
					Signed:   accountapi.Signed,
					Identity: accountapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      accountapi.Operations(),
				},
				{
					Name:     "trade",
					Short:    "Spot trading: place (incl. TP/SL, trigger, trailing, OCO) / modify / cancel orders, query orders & trades (signed)",
					Signed:   tradeapi.Signed,
					Identity: tradeapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      tradeapi.Operations(),
				},
				{
					Name:     "dca",
					Short:    "Spot DCA / auto-invest: eligible pairs, the caller's strategies & plans (gateway-signed)",
					Signed:   dcaapi.Signed,
					Identity: dcaapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      dcaapi.Operations(),
				},
				{
					Name:     "convert",
					Short:    "Spot flash convert: quote, execute, limit convert, cancel, order query (gateway-signed)",
					Signed:   convertapi.Signed,
					Identity: convertapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      convertapi.Operations(),
				},
				{
					Name:     "grid",
					Short:    "Spot grid trading: AI params, create / modify / stop grids, profit withdrawal (gateway-signed)",
					Signed:   gridapi.Signed,
					Identity: gridapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      gridapi.Operations(),
				},
				{
					Name:     "fee",
					Short:    "Spot fee rate center: the caller's fees by pair / channel / overall, zero-fee pairs (gateway-signed)",
					Signed:   feeapi.Signed,
					Identity: feeapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      feeapi.Operations(),
				},
				{
					Name:     "asset",
					Short:    "Spot asset: overview / balances / deposit-withdraw & transfer history / transfers / bills / prices / pnl (signed)",
					Signed:   assetapi.Signed,
					Identity: assetapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      assetapi.Operations(),
				},
			},
		},
		{
			Name:  "activity",
			Short: "Activity: online discovery / reward center / participation",
			Subs: []api.Subdomain{
				{
					Name:     "online",
					Short:    "Online activity discovery: list / banner list (public, no login)",
					Signed:   onlineapi.Signed,
					Identity: onlineapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      onlineapi.Operations(),
				},
				{
					Name:     "reward",
					Short:    "Unified reward center for the logged-in user (signed)",
					Signed:   rewardapi.Signed,
					Identity: rewardapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      rewardapi.Operations(),
				},
				{
					Name:     "participate",
					Short:    "Activity registration for the logged-in user (signed)",
					Signed:   participateapi.Signed,
					Identity: participateapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      participateapi.Operations(),
				},
			},
		},
		{
			Name:  "contract",
			Short: "Contract: public market data",
			Subs: []api.Subdomain{
				{
					Name:     "market",
					Short:    "Contract public market data: detail / price / ticker / depth / kline / funding / risk (read, unsigned)",
					Signed:   contractmarketapi.Signed,
					Identity: contractmarketapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      contractmarketapi.Operations(),
				},
				{
					Name:     "account",
					Short:    "Contract private account: assets / records / multi-asset / fee / risk / preferences (signed)",
					Signed:   contractaccountapi.Signed,
					Identity: contractaccountapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      contractaccountapi.Operations(),
				},
				{
					Name:     "trade",
					Short:    "Contract trading: orders / plan / stop / track / chase-limit / leverage / margin / positions (signed)",
					Signed:   contracttradeapi.Signed,
					Identity: contracttradeapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      contracttradeapi.Operations(),
				},
			},
		},
		{
			Name:  "futures",
			Short: "Futures (contract): public market data / account push, via WebSocket",
			Subs: []api.Subdomain{
				{
					Name:     "market",
					Short:    "Futures public market data via WS (read, unsigned)",
					Signed:   futuresmarketapi.Signed,
					Identity: futuresmarketapi.Identity,
					// HostKey is mandatory on every subdomain, but every operation
					// here is Kind: api.KindWSCollect and carries its own hardcoded
					// WS URL — it never resolves a host via pkg/hosts. Reusing the
					// openapi key keeps this subdomain from needing a host row of
					// its own that nothing would ever look up.
					HostKey: adapter.KeyOpenAPI,
					Ops:     futuresmarketapi.Operations(),
				},
				{
					Name:     "account",
					Short:    "Futures account's own new-trade push via WS (in-connection login)",
					Signed:   futuresaccountapi.Signed,
					Identity: futuresaccountapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      futuresaccountapi.Operations(),
				},
			},
		},
		{
			Name:  "financial-openapi",
			Short: "Financial products: flexible, fixed, and contract-hold earning",
			Subs: []api.Subdomain{
				{
					Name:     "common",
					Short:    "Common financial product, asset, position, and order queries (signed)",
					Signed:   commonapi.Signed,
					Identity: commonapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      commonapi.Operations(),
				},
				{
					Name:     "contract-hold",
					Short:    "Contract-hold earning products, positions, and profit records (signed)",
					Signed:   contractholdapi.Signed,
					Identity: contractholdapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      contractholdapi.Operations(),
				},
				{
					Name:     "fixed",
					Short:    "Fixed-term financial products and orders (signed)",
					Signed:   fixedapi.Signed,
					Identity: fixedapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      fixedapi.Operations(),
				},
				{
					Name:     "flexible",
					Short:    "Flexible financial products and auto-pledge settings (signed)",
					Signed:   flexibleapi.Signed,
					Identity: flexibleapi.Identity,
					HostKey:  adapter.KeyOpenAPI,
					Ops:      flexibleapi.Operations(),
				},
			},
		},
	}
}
