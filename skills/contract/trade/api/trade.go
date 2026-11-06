// Package tradeapi declares the operations of the contract.trade sub-domain
// (contract trading: orders, plan/stop/track orders, chase-limit, positions).
// Every operation is signed and carries the API-key identity; reads and writes
// coexist, so write semantics are set explicitly.
//
// Declare only Operation descriptors (name / path /
// access / params / body). The framework sends HTTP, signs, injects the
// credential, and mounts commands. scope is always contract.trade; the action
// lives in each Operation's Access. The user is identified by the API-key
// credential — never a caller parameter/body field.
package tradeapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// marginOperTypes: manual margin-adjust operations are declared inline via the
// change-margin body's enum tag (ADD/SUB).

// f returns a pointer to a numeric bound, for the Param MinVal/MaxVal fields.
func f(v float64) *float64 { return &v }

// ---- Request body prototypes ----

type cancelOrderReq struct {
	Symbol  string `json:"symbol" desc:"contract name, e.g. BTC_USDT"`
	OrderID int64  `json:"orderId" desc:"exchange order id"`
}

// placeOrderReq is the create/batch-order body (also the calc-liquidate-price input).
type placeOrderReq struct {
	Symbol           string  `json:"symbol" validate:"required" desc:"contract name, e.g. BTC_USDT"`
	PositionID       int64   `json:"positionId" desc:"position id (for reduce/close on an existing position)"`
	Price            float64 `json:"price" desc:"order price (limit orders)"`
	Vol              float64 `json:"vol" desc:"order volume (contracts)"`
	Leverage         int     `json:"leverage" desc:"leverage multiple"`
	Side             int     `json:"side" desc:"order side: 1 open-long, 2 close-short, 3 open-short, 4 close-long"`
	Type             int     `json:"type" desc:"order type: 1 limit, 2 post-only, 3 IOC, 4 FOK, 5 market, 6 convert-to-limit"`
	OpenType         int     `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	ExternalOid      string  `json:"externalOid" desc:"third-party (external) order id"`
	LossTrend        int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend      int     `json:"profitTrend" desc:"take-profit trigger price type"`
	MarketOrderLevel int     `json:"marketOrderLevel" desc:"market-order price level"`
	StopLossPrice    float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice  float64 `json:"takeProfitPrice" desc:"take-profit price"`
	PriceProtect     int     `json:"priceProtect" desc:"price-protect flag"`
	PositionMode     int     `json:"positionMode" desc:"position mode: 1 hedge, 2 one-way"`
	ReduceOnly       bool    `json:"reduceOnly" desc:"reduce-only (one-way mode only)"`
	MarketCeiling    bool    `json:"marketCeiling" desc:"open at 100% market"`
	ExternalModule   int     `json:"stpMode" desc:"self-trade-prevention mode"`
	CouponID         string  `json:"couponId" desc:"coupon id"`
	EnsureStopLoss   bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
	VolWay           int     `json:"volWay" desc:"volume unit: 1 contracts, 2 base amount"`
	FlashClose       bool    `json:"flashClose" desc:"flash-close flag"`
	Mtoken           string  `json:"mtoken" desc:"web device id"`
	BboTypeNum       int     `json:"bboTypeNum" desc:"BBO limit-order type number"`
	OrderID          int64   `json:"orderId" desc:"order id; used ONLY in the cross-margin scenario when calculating liquidation price — relevant to order-calc-liquidate-price"`
	FundingGap       float64 `json:"fundingGap" desc:"amount of spot/wealth-management funding to use, cross-margin mode only — relevant to order-calc-liquidate-price"`
}

type planOrderReq struct {
	Symbol           string  `json:"symbol" validate:"required" desc:"contract name, e.g. BTC_USDT"`
	Leverage         int     `json:"leverage" desc:"leverage multiple"`
	Side             int     `json:"side" desc:"order side"`
	TriggerPrice     float64 `json:"triggerPrice" validate:"required" desc:"trigger price"`
	Price            float64 `json:"price" desc:"execute price; 0/empty means market"`
	Vol              float64 `json:"vol" desc:"order volume"`
	OpenType         int     `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	TriggerType      int     `json:"triggerType" desc:"trigger type: 1 >=, 2 <="`
	ExecuteCycle     int     `json:"executeCycle" desc:"execute window: 1 24h, 2 7d"`
	Trend            int     `json:"trend" desc:"price type: 1 last, 2 fair, 3 index"`
	OrderType        int     `json:"orderType" desc:"order type"`
	MarketOrderLevel int     `json:"marketOrderLevel" desc:"market-order price level"`
	PriceProtect     int     `json:"priceProtect" desc:"price-protect flag"`
	PositionMode     int     `json:"positionMode" desc:"position mode: 1 hedge, 2 one-way"`
	LossTrend        int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend      int     `json:"profitTrend" desc:"take-profit trigger price type"`
	StopLossPrice    float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice  float64 `json:"takeProfitPrice" desc:"take-profit price"`
	ReduceOnly       bool    `json:"reduceOnly" desc:"reduce-only"`
	EnsureStopLoss   bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
	VolWay           int     `json:"volWay" desc:"volume unit"`
}

type planOrderChangePriceReq struct {
	Symbol       string  `json:"symbol" validate:"required" desc:"contract name"`
	OrderID      int64   `json:"orderId" validate:"required" desc:"plan-order id"`
	TriggerPrice float64 `json:"triggerPrice" desc:"trigger price"`
	Price        float64 `json:"price" desc:"execute price"`
	OrderType    int     `json:"orderType" validate:"required" desc:"order type"`
	TriggerType  int     `json:"triggerType" desc:"trigger type"`
	Trend        int     `json:"trend" desc:"price type"`
	Vol          float64 `json:"vol" desc:"order volume"`
	From         int     `json:"from" desc:"source flag"`
}

type planOrderChangeStopOrderReq struct {
	Symbol          string  `json:"symbol" validate:"required" desc:"contract name"`
	OrderID         int64   `json:"orderId" validate:"required" desc:"plan-order id"`
	LossTrend       int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend     int     `json:"profitTrend" desc:"take-profit trigger price type"`
	StopLossPrice   float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice float64 `json:"takeProfitPrice" desc:"take-profit price"`
	EnsureStopLoss  bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
}

type placeStopPlanOrderReq struct {
	LossTrend            int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend          int     `json:"profitTrend" desc:"take-profit trigger price type"`
	StopLossPrice        float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice      float64 `json:"takeProfitPrice" desc:"take-profit price"`
	Vol                  float64 `json:"vol" desc:"volume"`
	PositionID           int64   `json:"positionId" validate:"required" desc:"position id"`
	PriceProtect         int     `json:"priceProtect" desc:"price-protect flag"`
	ProfitLossVolType    string  `json:"profitLossVolType" desc:"volume mode: SAME / SEPARATE"`
	TakeProfitVol        float64 `json:"takeProfitVol" desc:"take-profit volume"`
	StopLossVol          float64 `json:"stopLossVol" desc:"stop-loss volume"`
	VolType              int     `json:"volType" desc:"volume type"`
	TakeProfitReverse    int     `json:"takeProfitReverse" desc:"take-profit reverse: 1 yes, 2 no"`
	StopLossReverse      int     `json:"stopLossReverse" desc:"stop-loss reverse: 1 yes, 2 no"`
	TakeProfitType       int     `json:"takeProfitType" desc:"take-profit order type"`
	TakeProfitOrderPrice float64 `json:"takeProfitOrderPrice" desc:"take-profit order price"`
	StopLossType         int     `json:"stopLossType" desc:"stop-loss order type"`
	StopLossOrderPrice   float64 `json:"stopLossOrderPrice" desc:"stop-loss order price"`
	EnsureStopLoss       bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
}

type cancelStopPlanOrderItem struct {
	Symbol          string `json:"symbol" desc:"contract name"`
	StopPlanOrderID int64  `json:"stopPlanOrderId" desc:"stop-plan order id"`
	PositionID      int64  `json:"positionId" desc:"position id"`
}

type changePriceReq struct {
	LossTrend         int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend       int     `json:"profitTrend" desc:"take-profit trigger price type"`
	StopLossPrice     float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice   float64 `json:"takeProfitPrice" desc:"take-profit price"`
	OrderID           int64   `json:"orderId" desc:"stop order id"`
	StopPlanOrderID   int64   `json:"stopPlanOrderId" desc:"stop-plan order id"`
	TakeProfitReverse int     `json:"takeProfitReverse" desc:"take-profit reverse: 1 yes, 2 no"`
	StopLossReverse   int     `json:"stopLossReverse" desc:"stop-loss reverse: 1 yes, 2 no"`
	EnsureStopLoss    bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
}

type changeStopPlanOrderReq struct {
	OrderID              int64   `json:"orderId" validate:"required" desc:"stop-plan order id"`
	LossTrend            int     `json:"lossTrend" desc:"stop-loss trigger price type"`
	ProfitTrend          int     `json:"profitTrend" desc:"take-profit trigger price type"`
	StopLossPrice        float64 `json:"stopLossPrice" desc:"stop-loss price"`
	TakeProfitPrice      float64 `json:"takeProfitPrice" desc:"take-profit price"`
	StopLossVolume       float64 `json:"stopLossVolume" desc:"stop-loss volume"`
	TakeProfitVolume     float64 `json:"takeProfitVolume" desc:"take-profit volume"`
	TakeProfitReverse    int     `json:"takeProfitReverse" desc:"take-profit reverse: 1 yes, 2 no"`
	StopLossReverse      int     `json:"stopLossReverse" desc:"stop-loss reverse: 1 yes, 2 no"`
	TakeProfitType       int     `json:"takeProfitType" desc:"take-profit order type"`
	TakeProfitOrderPrice float64 `json:"takeProfitOrderPrice" desc:"take-profit order price"`
	StopLossType         int     `json:"stopLossType" desc:"stop-loss order type"`
	StopLossOrderPrice   float64 `json:"stopLossOrderPrice" desc:"stop-loss order price"`
	EnsureStopLoss       bool    `json:"ensureStopLoss" desc:"guaranteed stop-loss"`
}

type placeTrackOrderReq struct {
	Symbol       string  `json:"symbol" validate:"required" desc:"contract name"`
	Leverage     int     `json:"leverage" desc:"leverage multiple"`
	Side         int     `json:"side" desc:"order side"`
	Vol          float64 `json:"vol" desc:"order volume"`
	OpenType     int     `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	Trend        int     `json:"trend" desc:"reference price type"`
	ActivePrice  float64 `json:"activePrice" desc:"activation price"`
	BackType     int     `json:"backType" desc:"callback type: 1 rate, 2 amount"`
	BackValue    float64 `json:"backValue" desc:"callback value"`
	PositionMode int     `json:"positionMode" desc:"position mode: 1 hedge, 2 one-way"`
	ReduceOnly   bool    `json:"reduceOnly" desc:"reduce-only"`
	VolWay       int     `json:"volWay" desc:"volume unit"`
}

type cancelTrackOrderReq struct {
	Symbol       string `json:"symbol" desc:"contract name"`
	TrackOrderID int64  `json:"trackOrderId" desc:"track-order id"`
}

type trackOrderChangeReq struct {
	Symbol       string  `json:"symbol" validate:"required" desc:"contract name"`
	TrackOrderID int64   `json:"trackOrderId" validate:"required" desc:"track-order id"`
	Trend        int     `json:"trend" desc:"reference price type"`
	ActivePrice  float64 `json:"activePrice" desc:"activation price"`
	BackType     int     `json:"backType" desc:"callback type"`
	BackValue    float64 `json:"backValue" desc:"callback value"`
	Vol          float64 `json:"vol" desc:"order volume"`
}

type chaseLimitOrderPlaceReq struct {
	Symbol           string  `json:"symbol" validate:"required" desc:"contract name"`
	Vol              float64 `json:"vol" desc:"order volume"`
	Leverage         int     `json:"leverage" desc:"leverage multiple"`
	Side             int     `json:"side" desc:"order side"`
	OpenType         int     `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	ChaseType        int     `json:"chaseType" desc:"chase type; default 1"`
	DistanceType     int     `json:"distanceType" desc:"distance type; default 0"`
	DistanceValue    float64 `json:"distanceValue" desc:"distance value"`
	MaxDistanceType  int     `json:"maxDistanceType" desc:"max-distance type; default 2"`
	MaxDistancePrice float64 `json:"maxDistancePrice" desc:"max-distance price"`
	CouponID         string  `json:"couponId" desc:"coupon id"`
	VolWay           int     `json:"volWay" desc:"volume unit"`
}

type changeOrderPriceReq struct {
	OrderID int64   `json:"orderId" validate:"required" desc:"exchange order id"`
	Price   float64 `json:"price" validate:"required" desc:"new order price"`
	Vol     float64 `json:"vol" desc:"new order volume (change-limit-order only)"`
}

type chaseOrderReq struct {
	OrderID string `json:"orderId" validate:"required" desc:"exchange order id"`
}

type changeMarginReq struct {
	PositionID int64   `json:"positionId" validate:"required" desc:"position id"`
	Amount     float64 `json:"amount" validate:"required" desc:"margin amount to add/reduce"`
	Type       string  `json:"type" validate:"required" enum:"ADD,SUB" desc:"operation: ADD (increase) / SUB (decrease)"`
}

type changeAutoAddImReq struct {
	PositionID int64 `json:"positionId" validate:"required" desc:"position id"`
	IsEnabled  bool  `json:"isEnabled" desc:"enable auto-add initial margin"`
}

type changeLeverageReq struct {
	PositionID       int64  `json:"positionId" desc:"position id (when adjusting an existing position)"`
	OpenType         int    `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	Leverage         int    `json:"leverage" validate:"required" desc:"target leverage multiple"`
	Symbol           string `json:"symbol" desc:"contract name"`
	PositionType     int    `json:"positionType" desc:"position type: 1 long, 2 short"`
	LeverageMode     int    `json:"leverageMode" desc:"leverage mode"`
	MarginSelected   bool   `json:"marginSelected" desc:"margin selected flag"`
	LeverageSelected bool   `json:"leverageSelected" desc:"leverage selected flag"`
}

type positionModeChangeReq struct {
	PositionMode int `json:"positionMode" validate:"required" desc:"position mode: 1 hedge, 2 one-way"`
}

// differentiationParam is one side's order parameters (long or short) for the
// V2 liquidation-price calc; mirrors the upstream request shape.
type differentiationParam struct {
	Leverage int     `json:"leverage" desc:"leverage multiple"`
	OpenType int     `json:"openType" desc:"margin mode: 1 isolated, 2 cross"`
	Vol      float64 `json:"vol" desc:"order volume (contracts)"`
	Price    float64 `json:"price" desc:"order price (limit orders; 0/empty means market)"`
}

type calcLiquidatePriceReq struct {
	Symbol       string               `json:"symbol" validate:"required" desc:"contract name"`
	Type         int                  `json:"type" desc:"calculation type"`
	PositionMode int                  `json:"positionMode" desc:"position mode: 1 hedge, 2 one-way"`
	LongParam    differentiationParam `json:"longParam" desc:"long-side order params (leverage/openType/vol/price) used to calc the long liquidation price"`
	ShortParam   differentiationParam `json:"shortParam" desc:"short-side order params (leverage/openType/vol/price) used to calc the short liquidation price"`
}

type autoAddImChangeHistoryReq struct {
	PositionID int64 `json:"positionId" validate:"required" desc:"position id"`
	RecordID   int64 `json:"recordId" desc:"cursor: last record id"`
	PageSize   int   `json:"pageSize" desc:"page size; default 5"`
}

type positionReverseReq struct {
	Symbol     string  `json:"symbol" desc:"contract name"`
	PositionID int64   `json:"positionId" validate:"required" desc:"position id"`
	Vol        float64 `json:"vol" validate:"required" desc:"volume to reverse"`
}

// Operations returns every operation of this sub-domain. Signed/Identity are
// omitted on every op — all inherit the subdomain defaults (signed, openapi).
func Operations() []api.Operation {
	return []api.Operation{
		// ---- Order query ----
		{
			Name: "liquidate-price", Method: "GET", Path: "/api/v1/private/order/liquidate_price/{orderId}",
			Access: api.Read, Summary: "Estimated liquidation price for an order",
			Params: []api.Param{{Name: "orderId", Desc: "Exchange order id", Required: true, In: api.InPath}},
		},
		{
			Name: "deal-details", Method: "GET", Path: "/api/v1/private/order/deal_details/{orderId}",
			Access: api.Read, Summary: "Deal (fill) details of an order",
			Params: []api.Param{{Name: "orderId", Desc: "Exchange order id", Required: true, In: api.InPath}},
		},
		{
			Name: "deal-details-by-position", Method: "GET", Path: "/api/v1/private/order/deal_details/byPositionId/{symbol}/{positionId}",
			Access: api.Read, Summary: "Deal (fill) details by contract and position id",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "positionId", Desc: "Position id", Required: true, In: api.InPath},
			},
		},
		{
			Name: "close-orders-v2", Method: "GET", Path: "/api/v1/private/order/list/close_orders",
			Access: api.Read, Summary: "Historical orders (closed) V2",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "category", Desc: "Category filter(s), comma-separated"},
				{Name: "side", Desc: "Order side filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "open-orders-v2", Method: "GET", Path: "/api/v1/private/order/list/open_orders",
			Access: api.Read, Summary: "Current open orders V2 (all contracts)",
			Params: []api.Param{
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "open-orders-v2-by-symbol", Method: "GET", Path: "/api/v1/private/order/list/open_orders/{symbol}",
			Access: api.Read, Summary: "Current open orders V2 for a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true, In: api.InPath},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "history-orders-v2", Method: "GET", Path: "/api/v1/private/order/list/history_orders",
			Access: api.Read, Summary: "Historical orders (limit) V2",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "category", Desc: "Category filter(s), comma-separated"},
				{Name: "type", Desc: "Order type filter(s), comma-separated"},
				{Name: "states", Desc: "Order state filter(s), comma-separated"},
				{Name: "side", Desc: "Order side filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
				{Name: "orderId", Desc: "Exchange order id filter"},
			},
		},
		{
			Name: "order-config", Method: "GET", Path: "/api/v1/private/order/order/config",
			Access: api.Read, Summary: "Order-query configuration",
		},

		// ---- Order create / cancel / change ----
		{
			Name: "create", Method: "POST", Path: "/api/v1/private/order/create",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Create (place) an order",
			Body:    placeOrderReq{},
		},
		{
			Name: "cancel", Method: "POST", Path: "/api/v1/private/order/cancel",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel orders by exchange order ids",
			Body:    []int64{},
		},
		{
			Name: "cancel-all", Method: "POST", Path: "/api/v1/private/order/cancel_all",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel all orders (optionally for a symbol)",
			Body:    cancelOrderReq{},
		},
		{
			Name: "change-limit-order", Method: "POST", Path: "/api/v1/private/order/change_limit_order",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a limit order's price and volume",
			Body:    changeOrderPriceReq{},
		},
		{
			Name: "chase-limit-order", Method: "POST", Path: "/api/v1/private/order/chase_limit_order",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Chase an order (reprice to the opposite best level)",
			Body:    chaseOrderReq{},
		},

		// ---- Chase-limit orders ----
		{
			Name: "chase-limit-place", Method: "POST", Path: "/api/v1/private/order/chase_limit/place",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Place a chase-limit order",
			Body:    chaseLimitOrderPlaceReq{},
		},
		{
			Name: "chase-limit-stop", Method: "POST", Path: "/api/v1/private/order/chase_limit/stop",
			Access:  api.Write,
			Summary: "Set stop-profit/loss on chase-limit orders",
			Body:    []int64{},
		},
		{
			Name: "chase-limit-cancel", Method: "POST", Path: "/api/v1/private/order/chase_limit/cancel",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel chase-limit orders",
			Body:    []int64{},
		},

		// ---- Plan orders ----
		{
			Name: "planorder-place-v2", Method: "POST", Path: "/api/v1/private/planorder/place/v2",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Place a plan (trigger) order V2",
			Body:    planOrderReq{},
		},
		{
			Name: "planorder-cancel", Method: "POST", Path: "/api/v1/private/planorder/cancel",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel plan orders",
			Body:    []cancelOrderReq{},
		},
		{
			Name: "planorder-cancel-all", Method: "POST", Path: "/api/v1/private/planorder/cancel_all",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel all plan orders (optionally for a symbol)",
			Body:    cancelOrderReq{},
		},
		{
			Name: "planorder-orders-v2", Method: "GET", Path: "/api/v1/private/planorder/list/orders",
			Access: api.Read, Summary: "Plan order list V2",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "states", Desc: "State filter(s), comma-separated"},
				{Name: "side", Desc: "Order side filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "planorder-change-price", Method: "POST", Path: "/api/v1/private/planorder/change_price",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a plan order's price",
			Body:    planOrderChangePriceReq{},
		},
		{
			Name: "planorder-change-stop-order", Method: "POST", Path: "/api/v1/private/planorder/change_stop_order",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a plan order's stop-profit/loss",
			Body:    planOrderChangeStopOrderReq{},
		},

		// ---- Stop (take-profit / stop-loss) orders ----
		{
			Name: "stoporder-place-v2", Method: "POST", Path: "/api/v1/private/stoporder/place/v2",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Place a take-profit/stop-loss order V2",
			Body:    placeStopPlanOrderReq{},
		},
		{
			Name: "stoporder-cancel", Method: "POST", Path: "/api/v1/private/stoporder/cancel",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel take-profit/stop-loss orders",
			Body:    []cancelStopPlanOrderItem{},
		},
		{
			Name: "stoporder-change-price", Method: "POST", Path: "/api/v1/private/stoporder/change_price",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a take-profit/stop-loss order's price",
			Body:    changePriceReq{},
		},
		{
			Name: "stoporder-change-plan-price", Method: "POST", Path: "/api/v1/private/stoporder/change_plan_price",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a position's take-profit/stop-loss price",
			Body:    changePriceReq{},
		},
		{
			Name: "stoporder-change-plan-order", Method: "POST", Path: "/api/v1/private/stoporder/change_plan_order",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a plan take-profit/stop-loss order",
			Body:    changeStopPlanOrderReq{},
		},
		{
			Name: "stoporder-cancel-all", Method: "POST", Path: "/api/v1/private/stoporder/cancel_all",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel all take-profit/stop-loss orders",
			Body:    cancelStopPlanOrderItem{},
		},
		{
			Name: "stoporder-orders-v2", Method: "GET", Path: "/api/v1/private/stoporder/list/orders",
			Access: api.Read, Summary: "Take-profit/stop-loss order list V2",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "is_finished", Desc: "Finished filter: 0 unfinished, 1 finished"},
				{Name: "state", Desc: "State filter"},
				{Name: "type", Desc: "Type filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "stoporder-open-orders", Method: "GET", Path: "/api/v1/private/stoporder/open_orders",
			Access: api.Read, Summary: "Current take-profit/stop-loss orders",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "includeLimit", Desc: "Include limit orders; default false"},
			},
		},

		// ---- Track orders ----
		{
			Name: "trackorder-place", Method: "POST", Path: "/api/v1/private/trackorder/place",
			Access: api.Write, Destructive: api.Ptr(false),
			Summary: "Place a trailing (track) order",
			Body:    placeTrackOrderReq{},
		},
		{
			Name: "trackorder-cancel", Method: "POST", Path: "/api/v1/private/trackorder/cancel",
			Access: api.Write, Idempotent: api.Ptr(true),
			Summary: "Cancel trailing (track) orders",
			Body:    cancelTrackOrderReq{},
		},
		{
			Name: "trackorder-change-order", Method: "POST", Path: "/api/v1/private/trackorder/change_order",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Change a trailing (track) order",
			Body:    trackOrderChangeReq{},
		},
		{
			Name: "trackorder-list", Method: "GET", Path: "/api/v1/private/trackorder/list/orders",
			Access: api.Read, Summary: "Trailing (track) order list",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "states", Desc: "State filter(s), comma-separated", Required: true},
				{Name: "side", Desc: "Order side filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "pageIndex", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},

		// ---- Positions ----
		{
			Name: "open-positions", Method: "GET", Path: "/api/v1/private/position/open_positions",
			Access: api.Read, Summary: "Current open positions",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "positionId", Desc: "Position id filter"},
			},
		},
		{
			Name: "history-positions-v2", Method: "GET", Path: "/api/v1/private/position/list/history_positions",
			Access: api.Read, Summary: "Historical positions V2",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "type", Desc: "Open type filter"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "position_type", Desc: "Position type: 1 long, 2 short"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
				{Name: "sortType", Desc: "Sort type; default CLOSE_TIME_DESC"},
			},
		},
		{
			Name: "history-positions-liquidate", Method: "GET", Path: "/api/v1/private/position/list/history_positions_liquidate",
			Access: api.Read, Summary: "Historical liquidated positions",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "positionId", Desc: "Position id", Required: true},
			},
		},
		{
			Name: "funding-records", Method: "GET", Path: "/api/v1/private/position/funding_records",
			Access: api.Read, Summary: "Funding-fee records",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "position_id", Desc: "Position id filter"},
				{Name: "position_type", Desc: "Position type: 1 long, 2 short"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
				{Name: "page_num", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "page_size", Desc: "Page size; default 20", MinVal: f(1)},
			},
		},
		{
			Name: "funding-records-summary", Method: "GET", Path: "/api/v1/private/position/funding_records/summary",
			Access: api.Read, Summary: "Funding-fee summary",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "position_id", Desc: "Position id filter"},
				{Name: "position_type", Desc: "Position type: 1 long, 2 short"},
				{Name: "start_time", Desc: "Start time (Unix milliseconds)"},
				{Name: "end_time", Desc: "End time (Unix milliseconds)"},
			},
		},
		{
			Name: "position-funding-detail", Method: "GET", Path: "/api/v1/private/position/list/position_funding_detail",
			Access: api.Read, Summary: "Position funding-fee detail",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name filter"},
				{Name: "pageIndex", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 15", MinVal: f(1)},
			},
		},
		{
			Name: "margin-change-details", Method: "GET", Path: "/api/v1/private/position/marginChangeDetails",
			Access: api.Read, Summary: "Margin-change details by position id",
			Params: []api.Param{
				{Name: "positionId", Desc: "Position id", Required: true},
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "pageIndex", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "pageSize", Desc: "Page size; default 5", MinVal: f(1)},
			},
		},
		{
			Name: "open-position-leverage-history", Method: "GET", Path: "/api/v1/private/position/open_position/leverage_history",
			Access: api.Read, Summary: "Leverage-change history of an open position",
			Params: []api.Param{
				{Name: "positionId", Desc: "Position id", Required: true},
				{Name: "index", Desc: "Page number; default 1", MinVal: f(1)},
				{Name: "limit", Desc: "Page size; default 10", MinVal: f(1)},
			},
		},
		{
			Name: "leverage", Method: "GET", Path: "/api/v1/private/position/leverage",
			Access: api.Read, Summary: "Leverage of a contract",
			Params: []api.Param{
				{Name: "symbol", Desc: "Contract name, e.g. BTC_USDT", Required: true},
				{Name: "needMaxLeverage", Desc: "Include the max leverage; default false"},
			},
		},
		{
			Name: "position-mode", Method: "GET", Path: "/api/v1/private/position/position_mode",
			Access: api.Read, Summary: "Position mode (hedge / one-way)",
		},
		{
			Name: "calc-liquidate-price", Method: "GET", Path: "/api/v1/private/position/calc_liquidate_price",
			Access: api.Read, Summary: "Estimated liquidation price when switching isolated to cross",
			Params: []api.Param{
				{Name: "id", Desc: "Position id", Required: true},
				{Name: "lever", Desc: "Leverage multiple"},
			},
		},
		{
			Name: "order-calc-liquidate-price", Method: "POST", Path: "/api/v1/private/position/order/calc_liquidate_price",
			Access: api.Read, Summary: "Estimated liquidation price for a prospective order",
			Body: placeOrderReq{},
		},
		{
			Name: "order-calc-liquidate-price-v2", Method: "POST", Path: "/api/v1/private/position/order/calc_liquidate_price/v2",
			Access: api.Read, Summary: "Estimated liquidation price for a prospective order V2 (both sides)",
			Body: calcLiquidatePriceReq{},
		},
		{
			Name: "change-margin", Method: "POST", Path: "/api/v1/private/position/change_margin",
			Access: api.Write, Summary: "Adjust a position's margin",
			Body: changeMarginReq{},
		},
		{
			Name: "change-auto-add-im", Method: "POST", Path: "/api/v1/private/position/change_auto_add_im",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Toggle auto-add initial margin",
			Body:    changeAutoAddImReq{},
		},
		{
			Name: "auto-add-im-change-history", Method: "POST", Path: "/api/v1/private/position/autoAddImChange/history",
			Access: api.Read, Summary: "Auto-add-margin change history",
			Body: autoAddImChangeHistoryReq{},
		},
		{
			Name: "change-leverage", Method: "POST", Path: "/api/v1/private/position/change_leverage",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Adjust leverage",
			Body:    changeLeverageReq{},
		},
		{
			Name: "change-position-mode", Method: "POST", Path: "/api/v1/private/position/change_position_mode",
			Access: api.Write, Idempotent: api.Ptr(true), Destructive: api.Ptr(false),
			Summary: "Switch position mode (hedge / one-way)",
			Body:    positionModeChangeReq{},
		},
		{
			Name: "close-all", Method: "POST", Path: "/api/v1/private/position/close_all",
			Access:  api.Write,
			Summary: "Close all open positions",
		},
		{
			Name: "reverse", Method: "POST", Path: "/api/v1/private/position/reverse",
			Access:  api.Write,
			Summary: "Reverse a position (close and open the opposite side)",
			Body:    positionReverseReq{},
		},
	}
}

// Scope is this sub-domain's resource identifier, referenced by command assembly
// and the contract test (must match the SKILL.md frontmatter).
const Scope = "contract.trade"

// Signed is this sub-domain's default signing scheme: every trade operation is
// signed with the MEXC spot HMAC scheme. Operations inherit it.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), not a login session.
const Identity = identity.OpenAPI
