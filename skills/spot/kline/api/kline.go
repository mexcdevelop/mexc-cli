// Package klineapi declares operation declarations for the spot.kline
// subdomain. The operations use the isolated /api/cli/spot/kline route family
// and are routed upstream to a handler of their own.
//
// Only the object-list kline query is exposed. The upstream also serves a
// parallel-array variant of the SAME series (/api/cli/kline), but the two
// disagree on nine points — input unit (seconds vs milliseconds), success code
// (0 vs 200), value type (string vs number), and, worst, the key "v", which
// means quote amount on one and base volume on the other. Publishing both would
// hand a caller two paths to the same data with no objective basis to choose and
// a different pitfall down each, so the array variant is deliberately not
// wrapped. Two further upstream endpoints (/api/cli/trend/query and
// /api/cli/coin/trend) answer 404, and are not wrapped either.
package klineapi

import "github.com/mexcdevelop/mexc-cli/pkg/api"

// intervalValues are the allowed kline periods. The upstream resolves the value
// with an enum lookup on the NAME, so the match is case-sensitive and these ten
// spellings are the whole legal set: a lowercase or shorthand form ("1m", "60m",
// "1d") is refused upstream with "interval value is error".
var intervalValues = []string{
	"Min1", "Min5", "Min15", "Min30", "Min60",
	"Hour4", "Hour8", "Day1", "Week1", "Month1",
}

// openPriceModeValues are the allowed open-price modes of a kline period. This
// subdomain's openPriceMode is STRING-valued; the spot.marketv2 ticker
// operations use a different, numeric one (1 / 2).
var openPriceModeValues = []string{"FIRST_DEAL", "LAST_CLOSE"}

// maxReasonableUnixSeconds bounds start/end to catch the most common unit
// mistake client-side: passing a 13-digit millisecond timestamp where the
// upstream wants seconds. The upstream silently multiplies by 1000 with no
// unit check of its own, so a millisecond value here inflates the requested
// window by ~1000x and the resulting error ("The number of points is more than
// <N>") points at the window size, not the actual unit mistake. This bound is
// deliberately far in the future (year 2100) so it never rejects a legitimate
// seconds-based timestamp; it exists only to catch an order-of-magnitude unit
// error before the request is sent, not to validate the timestamp's meaning.
var maxReasonableUnixSeconds = api.Ptr(4102444800.0)

// Operations returns every operation of this subdomain.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "query", Method: "GET", Path: "/api/cli/spot/kline/kline/query",
			Access:  api.Read,
			Summary: "Kline / candlestick series for one trading pair",
			Params: []api.Param{
				{Name: "symbolId", Desc: "Trading pair id, e.g. 2 (read it from the marketv2 symbols operation)", Required: true},
				{Name: "interval", Desc: "Kline period; case-sensitive, one of the listed values", Required: true, Enum: intervalValues},
				{Name: "start", Desc: "Window start as a Unix timestamp in SECONDS (not milliseconds) — a millisecond value will be rejected as out of range", Required: true, MaxVal: maxReasonableUnixSeconds},
				{Name: "end", Desc: "Window end as a Unix timestamp in SECONDS (not milliseconds) — a millisecond value will be rejected as out of range", Required: true, MaxVal: maxReasonableUnixSeconds},
				{Name: "openPriceMode", Desc: "Open price of each period: FIRST_DEAL (first trade) or LAST_CLOSE (previous period's close)", Required: true, Enum: openPriceModeValues},
			},
		},
		{
			Name:    "support-earliest",
			Method:  "GET",
			Path:    "/api/cli/spot/kline/support/earliest",
			Access:  api.Read,
			Summary: "Earliest queryable window start, per kline period",
		},
	}
}

const Scope = "spot.kline"

const Signed = api.SignNone

const Identity = api.IdentityNone
