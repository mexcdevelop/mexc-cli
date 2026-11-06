---
name: mexc-contract-trade
scope: contract.trade
signed: yes
identity: yes
visibility: public
description: >-
  MEXC contract trading — place/cancel/change orders, plan (trigger) orders,
  take-profit/stop-loss orders, trailing (track) orders, chase-limit orders,
  leverage & margin, position mode, query/close/reverse positions. Requires an
  API key (every call is signed). Triggers: "place contract order","cancel
  order","plan order","stop loss take profit","trailing order","chase limit",
  "change leverage","adjust margin","open positions","close position","reverse".
---

# contract · trade (contract trading)

> Private trading subdomain: every operation is signed and carries the API-key identity (`signed: yes`, `identity: yes`). A valid API key is required; the user is identified by the credential, never by a request parameter/body field. On a credential/login error, load the `mexc-auth` skill, then replay.

## Core Concepts

- **symbol**: a contract name, e.g. `BTC_USDT`.
- **side**: `1` open-long, `2` close-short, `3` open-short, `4` close-long.
- **openType** (margin mode): `1` isolated, `2` cross.
- **positionMode**: `1` hedge (two-way), `2` one-way. **positionType**: `1` long, `2` short.
- **order type**: `1` limit, `2` post-only (maker), `3` IOC, `4` FOK, `5` market, `6` convert-to-limit.
- **trend** (price type): `1` last price, `2` fair price, `3` index price.
- Order/position ids are int64; times are **Unix milliseconds**.
- **Response envelope**: shared contract envelope (`success` / business `code` / `data`); business error codes come from the downstream service and pass through unchanged.
- **Error codes**: see [`references/error-codes.md`](references/error-codes.md) for the business error codes (with English meanings) these operations may return.
- **Two-step take-profit/stop-loss & trailing orders** attach to a `positionId`; plan (trigger) orders fire when the market crosses `triggerPrice`.

## Important Notes

- **API key required**: every op is `signed: yes` / `identity: yes`. Missing/invalid credentials return a structured credential error — load `mexc-auth`.
- **Writes need confirmation**: all order placement/cancel/change, margin/leverage/mode changes, and position close/reverse are `write` — show a summary and confirm first.
- **Never auto-retry `idempotent=no` writes** (`create`, `chase-limit-order`, `chase-limit-place`, `chase-limit-stop`, `planorder-place-v2`, `stoporder-place-v2`, `trackorder-place`, `change-margin`, `close-all`, `reverse`): a lost response could mean the action already landed.
- **Scalar-array bodies**: `cancel`, `chase-limit-stop`, and `chase-limit-cancel` take the request body as a raw JSON array of order ids, e.g. `--data '[123,456]'`.
- **Bodies**: create/change/plan/stop/track/chase placement and position ops take a JSON body via `--data` (validated before send; inspect with `mexc schema contract.trade.<op>`).

## Operations

> scope is the resource identifier (always `contract.trade`); the action is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc contract trade liquidate-price` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/liquidate_price/{orderId}` | Estimated liquidation price for an order |
| `mexc contract trade deal-details` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/deal_details/{orderId}` | Order deal details |
| `mexc contract trade deal-details-by-position` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/deal_details/byPositionId/{symbol}/{positionId}` | Deal details by position |
| `mexc contract trade close-orders-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/list/close_orders` | Historical orders (closed) V2 |
| `mexc contract trade open-orders-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/list/open_orders` | Current open orders V2 |
| `mexc contract trade open-orders-v2-by-symbol` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/list/open_orders/{symbol}` | Current open orders V2 by symbol |
| `mexc contract trade history-orders-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/list/history_orders` | Historical limit orders V2 |
| `mexc contract trade order-config` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/order/order/config` | Order-query configuration |
| `mexc contract trade create` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/order/create` | Create (place) an order |
| `mexc contract trade cancel` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/order/cancel` | Cancel orders by ids |
| `mexc contract trade cancel-all` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/order/cancel_all` | Cancel all orders |
| `mexc contract trade change-limit-order` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/order/change_limit_order` | Change limit order price & volume |
| `mexc contract trade chase-limit-order` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/order/chase_limit_order` | Chase order (reprice to best) |
| `mexc contract trade chase-limit-place` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/order/chase_limit/place` | Place a chase-limit order |
| `mexc contract trade chase-limit-stop` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/order/chase_limit/stop` | Set TP/SL on chase-limit |
| `mexc contract trade chase-limit-cancel` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/order/chase_limit/cancel` | Cancel chase-limit orders |
| `mexc contract trade planorder-place-v2` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/planorder/place/v2` | Place a plan (trigger) order V2 |
| `mexc contract trade planorder-cancel` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/planorder/cancel` | Cancel plan orders |
| `mexc contract trade planorder-cancel-all` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/planorder/cancel_all` | Cancel all plan orders |
| `mexc contract trade planorder-orders-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/planorder/list/orders` | Plan order list V2 |
| `mexc contract trade planorder-change-price` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/planorder/change_price` | Change plan order price |
| `mexc contract trade planorder-change-stop-order` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/planorder/change_stop_order` | Change plan order TP/SL |
| `mexc contract trade stoporder-place-v2` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/stoporder/place/v2` | Place a TP/SL order V2 |
| `mexc contract trade stoporder-cancel` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/stoporder/cancel` | Cancel TP/SL orders |
| `mexc contract trade stoporder-change-price` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/stoporder/change_price` | Change TP/SL order price |
| `mexc contract trade stoporder-change-plan-price` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/stoporder/change_plan_price` | Change position TP/SL price |
| `mexc contract trade stoporder-change-plan-order` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/stoporder/change_plan_order` | Change plan TP/SL order |
| `mexc contract trade stoporder-cancel-all` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/stoporder/cancel_all` | Cancel all TP/SL orders |
| `mexc contract trade stoporder-orders-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/stoporder/list/orders` | TP/SL order list V2 |
| `mexc contract trade stoporder-open-orders` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/stoporder/open_orders` | Current TP/SL orders |
| `mexc contract trade trackorder-place` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/trackorder/place` | Place a trailing (track) order |
| `mexc contract trade trackorder-cancel` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/trackorder/cancel` | Cancel trailing (track) orders |
| `mexc contract trade trackorder-change-order` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/trackorder/change_order` | Change trailing (track) order |
| `mexc contract trade trackorder-list` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/trackorder/list/orders` | Trailing (track) order list |
| `mexc contract trade open-positions` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/open_positions` | Current open positions |
| `mexc contract trade history-positions-v2` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/list/history_positions` | Historical positions V2 |
| `mexc contract trade history-positions-liquidate` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/list/history_positions_liquidate` | Historical liquidated positions |
| `mexc contract trade funding-records` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/funding_records` | Funding-fee records |
| `mexc contract trade funding-records-summary` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/funding_records/summary` | Funding-fee summary |
| `mexc contract trade position-funding-detail` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/list/position_funding_detail` | Position funding-fee detail |
| `mexc contract trade margin-change-details` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/marginChangeDetails` | Margin-change details |
| `mexc contract trade open-position-leverage-history` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/open_position/leverage_history` | Position leverage-change history |
| `mexc contract trade leverage` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/leverage` | Leverage of a contract |
| `mexc contract trade position-mode` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/position_mode` | Position mode |
| `mexc contract trade calc-liquidate-price` | `contract.trade` | read | yes | yes | yes | `GET /api/v1/private/position/calc_liquidate_price` | Isolated→cross estimated liquidation price |
| `mexc contract trade order-calc-liquidate-price` | `contract.trade` | read | yes | yes | yes | `POST /api/v1/private/position/order/calc_liquidate_price` | Prospective-order liquidation price |
| `mexc contract trade order-calc-liquidate-price-v2` | `contract.trade` | read | yes | yes | yes | `POST /api/v1/private/position/order/calc_liquidate_price/v2` | Prospective-order liquidation price V2 |
| `mexc contract trade change-margin` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/position/change_margin` | Adjust position margin |
| `mexc contract trade change-auto-add-im` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/position/change_auto_add_im` | Toggle auto-add margin |
| `mexc contract trade auto-add-im-change-history` | `contract.trade` | read | yes | yes | yes | `POST /api/v1/private/position/autoAddImChange/history` | Auto-add-margin change history |
| `mexc contract trade change-leverage` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/position/change_leverage` | Adjust leverage |
| `mexc contract trade change-position-mode` | `contract.trade` | write | yes | yes | yes | `POST /api/v1/private/position/change_position_mode` | Switch position mode |
| `mexc contract trade close-all` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/position/close_all` | Close all positions |
| `mexc contract trade reverse` | `contract.trade` | write | yes | yes | no | `POST /api/v1/private/position/reverse` | Reverse a position |

## Permissions

> Authorization = resource(scope) × action(access).

| Operation | scope | access |
|-----------|-------|--------|
| all query operations (order/plan/stop/track/position reads + liquidation-price calculators) | `contract.trade` | read |
| all placement / cancel / change / margin / leverage / mode / close / reverse operations | `contract.trade` | write |

## Usage Examples

```bash
# --- Order query ---
mexc contract trade liquidate-price --orderId 123456
mexc contract trade deal-details --orderId 123456
mexc contract trade deal-details-by-position --symbol BTC_USDT --positionId 111
mexc contract trade close-orders-v2 --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade open-orders-v2 --page_num 1 --page_size 20
mexc contract trade open-orders-v2-by-symbol --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade history-orders-v2 --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade order-config

# --- Order place / cancel / change ---
mexc contract trade create --data '{"symbol":"BTC_USDT","side":1,"type":1,"openType":1,"vol":1,"price":90000,"leverage":20}'
mexc contract trade cancel --data '[123456,123457]'
mexc contract trade cancel-all --data '{"symbol":"BTC_USDT"}'
mexc contract trade change-limit-order --data '{"orderId":123456,"price":91000,"vol":2}'
mexc contract trade chase-limit-order --data '{"orderId":"123456"}'

# --- Chase-limit orders ---
mexc contract trade chase-limit-place --data '{"symbol":"BTC_USDT","side":1,"vol":1,"leverage":20,"openType":1}'
mexc contract trade chase-limit-stop --data '[123456,123457]'
mexc contract trade chase-limit-cancel --data '[123456,123457]'

# --- Plan (trigger) orders ---
mexc contract trade planorder-place-v2 --data '{"symbol":"BTC_USDT","side":1,"triggerPrice":95000,"vol":1,"openType":1,"triggerType":1,"trend":1,"orderType":5,"executeCycle":1,"leverage":10}'
mexc contract trade planorder-cancel --data '[{"symbol":"BTC_USDT","orderId":123456}]'
mexc contract trade planorder-cancel-all --data '{"symbol":"BTC_USDT"}'
mexc contract trade planorder-orders-v2 --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade planorder-change-price --data '{"symbol":"BTC_USDT","orderId":123456,"triggerPrice":96000,"price":96000,"orderType":1}'
mexc contract trade planorder-change-stop-order --data '{"symbol":"BTC_USDT","orderId":123456,"stopLossPrice":80000,"takeProfitPrice":110000}'

# --- Take-profit / stop-loss orders ---
mexc contract trade stoporder-place-v2 --data '{"positionId":111,"takeProfitPrice":100000,"stopLossPrice":80000}'
mexc contract trade stoporder-cancel --data '[{"symbol":"BTC_USDT","stopPlanOrderId":222}]'
mexc contract trade stoporder-change-price --data '{"orderId":222,"stopLossPrice":80000,"takeProfitPrice":100000}'
mexc contract trade stoporder-change-plan-price --data '{"stopPlanOrderId":222,"stopLossPrice":80000,"takeProfitPrice":100000}'
mexc contract trade stoporder-change-plan-order --data '{"orderId":222,"stopLossPrice":80000,"takeProfitPrice":100000}'
mexc contract trade stoporder-cancel-all --data '{"symbol":"BTC_USDT"}'
mexc contract trade stoporder-orders-v2 --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade stoporder-open-orders --symbol BTC_USDT --includeLimit false

# --- Trailing (track) orders ---
mexc contract trade trackorder-place --data '{"symbol":"BTC_USDT","side":1,"vol":1,"openType":1,"backType":1,"backValue":0.5}'
mexc contract trade trackorder-cancel --data '{"symbol":"BTC_USDT","trackOrderId":333}'
mexc contract trade trackorder-change-order --data '{"symbol":"BTC_USDT","trackOrderId":333,"backType":1,"backValue":0.8}'
mexc contract trade trackorder-list --symbol BTC_USDT --states 1 --pageIndex 1 --pageSize 20

# --- Positions query ---
mexc contract trade open-positions --symbol BTC_USDT
mexc contract trade history-positions-v2 --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade history-positions-liquidate --symbol BTC_USDT --positionId 111
mexc contract trade funding-records --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract trade funding-records-summary --symbol BTC_USDT
mexc contract trade position-funding-detail --symbol BTC_USDT --pageIndex 1 --pageSize 15
mexc contract trade margin-change-details --positionId 111 --symbol BTC_USDT --pageIndex 1 --pageSize 5
mexc contract trade open-position-leverage-history --positionId 111 --index 1 --limit 10
mexc contract trade leverage --symbol BTC_USDT --needMaxLeverage false
mexc contract trade position-mode

# --- Liquidation-price calculators ---
mexc contract trade calc-liquidate-price --id 111 --lever 20
mexc contract trade order-calc-liquidate-price --data '{"symbol":"BTC_USDT","side":1,"type":1,"openType":1,"vol":1,"price":90000,"leverage":20}'
mexc contract trade order-calc-liquidate-price-v2 --data '{"symbol":"BTC_USDT","type":1,"positionMode":1}'

# --- Position management (write) ---
mexc contract trade change-margin --data '{"positionId":111,"amount":10,"type":"ADD"}'
mexc contract trade change-auto-add-im --data '{"positionId":111,"isEnabled":true}'
mexc contract trade auto-add-im-change-history --data '{"positionId":111,"pageSize":5}'
mexc contract trade change-leverage --data '{"symbol":"BTC_USDT","positionType":1,"openType":1,"leverage":20}'
mexc contract trade change-position-mode --data '{"positionMode":1}'
mexc contract trade close-all
mexc contract trade reverse --data '{"symbol":"BTC_USDT","positionId":111,"vol":1}'
```

## Workflows

**Open a position with attached TP/SL**
1. `create` with `takeProfitPrice`/`stopLossPrice` in the body, or place the order then
2. `stoporder-place-v2` against the resulting `positionId`.

**Adjust and close a position**
1. `open-positions` → find the `positionId`.
2. `change-leverage` / `change-margin` as needed (confirm each write).
3. `reverse` to flip the side, or `close-all` to flatten every open position (confirm first).

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry an `idempotent=no` op (placements, margin change, close/reverse).
- Show a summary and wait for explicit user confirmation before any write operation.
- Unsure about parameters or a body shape? run `mexc schema contract.trade.<op>` instead of guessing.
- On a structured credential/login error, load the `mexc-auth` skill to prepare the API key; common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

**Skill-specific notes**
- `cancel`, `chase-limit-stop`, `chase-limit-cancel` take a raw JSON array of ids through `--data`, not an object body.
- Order `side` and `type` are numeric codes (see Core Concepts); pass the integer, not a label.
