---
name: mexc-contract-market
scope: contract.market
signed: no
identity: no
visibility: public
description: >-
  MEXC contract public market data — contract detail, index/fair/market price,
  ticker, order book depth, klines, funding rate, risk reserve, movers, rankings
  and platform big-data. Public data, no API key required. Triggers: "contract
  ticker","contract depth","contract klines","index price","fair price","funding
  rate","risk reserve","hot contracts","contract movers","long short ratio".
---

# contract · market (public market data)

> Public contract market-data subdomain: every operation is a read needing no signature or identity (`signed: no`, `identity: no`), so no credentials and no shared auth guide are involved.

## Core Concepts

- **symbol / contractName**: a contract name, e.g. `BTC_USDT` (base + `_` + quote). Some endpoints call the parameter `symbol`, others `contractName`; both mean the contract name.
- **symbols**: a comma-separated list of contract names, e.g. `BTC_USDT,ETH_USDT`.
- **interval (kline)**: one of `Min1` `Min5` `Min15` `Min30` `Min60` `Hour4` `Hour8` `Day1` `Week1` `Month1`.
- **interval (big data)**: one of `MIN5` `MIN15` `MIN30` `H1` `H2` `H4` `H6` `H8` `H12` `D1`.
- **scene (`detail-v3`)**: `0` common, `1` web1, `2` web2, `3` agencyCenter, `4` platformAssets, `5` seo, `6` operation, `7` otc, `8` ptp, `9` ios/android, `10` ios, `11` android, `12` web3, `13` ios/android-kline.
- Kline `start` / `end` are **Unix seconds**; most other timestamps in the response body are **Unix milliseconds**.
- **Response envelope**: responses follow the shared contract envelope (`success`, business `code`, `data`); the fields below describe the `data` body. Business error codes are defined by the downstream service and passed through by the gateway unchanged.
- **Error codes**: see [`references/error-codes.md`](references/error-codes.md) for the business error codes (with English meanings) these operations may return.
- **state (contract state)**: `0` ENABLED, `1` DELIVERY, `2` DELIVERY_CMP (delivery complete), `3` OFFLINE, `4` PAUSE (codes per the contract state enum).
- **positionOpenType (pot)**: `1` isolated, `2` cross, `3` both. **futureType (ft)**: `1` perpetual, `2` daily, `3` weekly, `4` monthly, `5` quarterly, `6` yearly.

### Return-field dictionary

Field meanings follow the endpoints' response bodies. One subdomain = one `Contract` resource; each op returns a different projection of it.

**Contract detail — `detail` (full names)**: `id`, `symbol`, `displayName` / `displayNameEn`, `baseCoin` / `quoteCoin`, `baseCoinName` / `quoteCoinName`, `settleCoin`, `futureType`, `positionOpenType`, `state`, `contractSize`, `priceScale`, `volScale`, `amountScale`, `priceUnit`, `volUnit`, `minLeverage` / `maxLeverage` (country-adjusted) / `regularMaxLeverage`, `tempMaxLeverage` / `tempMaxLeverageReason` / `tempMaxLeverageLimited`, `minVol` / `maxVol`, `maintenanceMarginRate` / `initialMarginRate`, `liquidationFeeRate`, `askLimitPriceRate` / `bidLimitPriceRate`, `riskBaseVol` / `riskIncrVol` / `riskIncrImr` / `riskIncrMmr` / `riskLevelLimit` / `riskLimitType` / `riskLimitMode` / `riskLimitCustom`, `takerFeeRate` / `makerFeeRate` / `feeRateMode` / `isZeroFeeRate` / `leverageFeeRates` / `tieredFeeRates`, `indexOrigin`, `depthStepList`, `maxNumOrders`, `apiAllowed`, `baseCoinIconUrl`, `conceptPlate` / `conceptPlateId`, `isHot` / `isNew` / `isHidden`, `openingTime` / `openingCountdownOption` / `showBeforeOpen`, `createTime`, `tagIdList`, `fn` (multi-language full name).

**Contract detail V2/V3 — `detail-v2` / `detail-v3` (compact names)**: same data with abbreviated keys —
`id`, `symbol`, `dn`/`dne` (display name zh/en), `bc`/`qc` (base/quote coin), `bcn`/`qcn` (base/quote coin name), `sc` (settle coin), `ft` (future type), `pot` (position open type), `state`, `cs` (contract size), `ps`/`vs`/`as` (price/vol/amount scale), `pu`/`vu` (price/vol unit), `minL`/`maxL`/`rml` (min/max/regular-max leverage), `tml`/`tmlr`/`tmll` (temp max leverage / reason / limited), `ccMaxL` (country-config max leverage), `minV`/`maxV` (min/max vol), `mmr`/`imr` (maint/initial margin rate), `liqfr` (liquidation fee rate), `alpr`/`blpr` (ask/bid limit price rate), `rbv`/`riv`/`rii`/`rim`/`rll`/`rlt`/`rlm`/`rlcs` (risk-limit fields), `frm`/`tfr`/`mfr` (fee-rate mode / taker / maker), `izfr`/`izfs` (is-zero-fee), `levfrs`/`tiefrs` (leverage / tiered fee rates), `io` (index origin), `dsl`/`dsn` (depth step list / num), `mno` (max num orders), `aa` (api allowed), `bciu` (base coin icon url), `bcid`/`vid` (base coin id / vcoin id), `cp`/`cpi` (concept plate / id), `ih`/`in`/`ihd`/`ip` (is hot/new/hidden/pressure), `ot`/`oco`/`sbo` (opening time / countdown option / show-before-open), `ct` (create time), `iml` (max-leverage label), `type`/`tl`/`tls` (type / type label / tag list), `stopOnlyFair`, `rlr` (risk limit rate), `fn` (full name), `ctl` (translation). `detail-v3` returns only the fields the `scene` (and `--params`) select.

**Market price** — `market-price-v2`: `{ symbol, weight, list:[{name,price,weight}] }`.

**Ticker** — `ticker` / `tickers`: `contractId`, `symbol`, `lastPrice`, `bid1` / `ask1`, `volume24` / `amount24`, `holdVol`, `high24Price` / `lower24Price`, `riseFallRate` / `riseFallValue`, `indexPrice`, `fairPrice`, `fundingRate`, `maxBidPrice` / `minAskPrice`, `timestamp`, `riseFallRates` (per-timezone `{ zone, r, v, r7, r30, r90, r180, r365 }`). `spot-latest-price`: list of `{ currency, price }`.

**Depth** — `depth` / `depth-step`: `{ bids, asks, version, cts, timestamp }`; each level is `[price, vol, orderCount]`.

**Deals** — `deals`: `{ id, price, vol, timestamp, isBid (1=buy, 2=sell), open }`.

**Klines** — `kline*` (column arrays aligned by index): `time[]` (seconds), `open[]`, `close[]`, `high[]`, `low[]`, `vol[]`, `amount[]`, plus `realOpen[]` / `realClose[]` / `realHigh[]` / `realLow[]`.

**Funding rate** — `funding-rate`: `symbol`, `fundingRate`, `indexPrice` (`idxPrice`), `fairPrice`, `collectCycle`, `maxFundingRate` / `minFundingRate`, `nextSettleTime`, `timestamp`. History (`funding-rate-history` / `funding-rate-history-of-day`): `symbol`, `fundingRate`, `settleTime`, `collectCycle` (history is paged: `currentPage` / `pageSize` / `totalCount` / `resultList`).

**Risk reserve** — history (`risk-reverse-history` / `risk-reverse-history-of-days`): `{ symbol, currency, available, snapshotTime }` (`risk-reverse-history` is paged).

**Rankings / big data** — `rise-fall-rate-sort`: `contractId`, `symbol`, `lastPrice`, `riseFallRate`, `baseCoin` / `quoteCoin` / `baseCoinName` / `quoteCoinName`, `settleCoin`, `contractSize`, `priceScale`, `futureType`, `coinUrl` / `baseCoinIconUrl`, `type`, `stopOnlyFair`, `state`. `hot-contracts`: contract spec fields + `lastPrice`, `riseFallRate`, `maxLeverage`, `isMaxLeverage`, `isZeroFeeRate`. `market-changes-list`: market-movers list. `big-data-position` and `big-data-active-trade-amount` return the long/short ratio and active buy/sell amount series for the requested `interval`.

## Resource Relationships

```
Contract (BTC_USDT)
├── Detail          contract spec (leverage / precision / fee / risk limits)
├── MarketPrice     index-source component prices
├── Ticker          24h stats / last price / rise-fall
├── Depth           order book depth (+ merged-precision steps)
├── Deals           recent trades
├── Kline           klines (contract / index-price / fair-price)
├── FundingRate     current + historical funding rate
└── RiskReserve     historical risk-reserve fund
```

## Important Notes

- **All public, no key required**: every operation in this subdomain is `signed: no` / `identity: no` and needs no `X-MEXC-APIKEY`.
- **Identity is not a parameter**: a logged-in caller's identity travels with the request's own credentials, never as a field — do not put it in the request data.
- `detail` / `detail-v2` / `detail-v3` / `ticker` / `funding-rate` return the whole market when no symbol is passed.
- `deals` returns 100 trades by default and is capped at 100 (`--limit`).
- `depth` returns the full cached depth when `--limit` is omitted; `--booksFull true` returns the full snapshot and is rate-limited.
- `funding-rate-history-of-day` caps `--day` at 14.
- `detail-v3` requires `--client` and `--scene`; `--params` appends extra fields on top of the scene's default field set.
- `rise-fall-rate-sort` requires the `timezone` header.
- Kline period names here are the **contract** period names (`Min1`…`Month1`), which differ from spot's `1m`…`1M`.

## Operations

> scope is the resource identifier (always `contract.market`); the action is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe.

| Command                                             | scope             | access | signed | identity | idempotent | Endpoint                                                 | Description                                    |
| --------------------------------------------------- | ----------------- | ------ | ------ | -------- | ---------- | -------------------------------------------------------- | ---------------------------------------------- |
| `mexc contract market detail`                       | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/detail`                            | Contract detail (single or all)                |
| `mexc contract market detail-v2`                    | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/detailV2`                          | Contract detail V2 (compact fields)            |
| `mexc contract market detail-v3`                    | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/detailV3`                          | Contract detail V3 (scene-tailored)            |
| `mexc contract market support-currencies`           | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/support_currencies`                | Supported margin currencies                    |
| `mexc contract market support-currencies-v2`        | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/support_currencies_v2`             | Supported margin currencies V2                 |
| `mexc contract market delivery-tips`                | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/delivery/tips`                     | Delivery tips for a delivery contract          |
| `mexc contract market market-price-v2`              | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/market_price_v2`                   | Latest index-source prices V2                  |
| `mexc contract market ticker`                       | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/ticker`                            | Ticker (single or all)                         |
| `mexc contract market tickers`                      | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/tickers`                           | Tickers for a set of contracts                 |
| `mexc contract market spot-latest-price`            | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/spot/latest_price`                 | Latest spot price for currencies               |
| `mexc contract market depth`                        | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/depth/{symbol}`                    | Order book depth                               |
| `mexc contract market depth-step-enable`            | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/depth_step/enable`                 | Depth-merge precision switch (legacy)          |
| `mexc contract market depth-step`                   | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/depth_step/{symbol}`               | Merged-precision depth at a step               |
| `mexc contract market deals`                        | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/deals/{symbol}`                    | Recent trades (`--limit` default/max 100)      |
| `mexc contract market kline`                        | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/{symbol}`                    | Klines                                         |
| `mexc contract market mini-klines`                  | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/miniKlines`                        | Mini klines for a set of contracts             |
| `mexc contract market index-price-kline`            | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/index_price/{symbol}`        | Index-price klines                             |
| `mexc contract market fair-price-kline`             | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/fair_price/{symbol}`         | Fair-price klines                              |
| `mexc contract market kline-recent`                 | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/{symbol}/recent`             | Most recent klines                             |
| `mexc contract market index-price-kline-recent`     | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/index_price/{symbol}/recent` | Most recent index-price klines                 |
| `mexc contract market fair-price-kline-recent`      | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/fair_price/{symbol}/recent`  | Most recent fair-price klines                  |
| `mexc contract market kline-next`                   | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/{symbol}/next`               | Next page of klines                            |
| `mexc contract market index-price-kline-next`       | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/index_price/{symbol}/next`   | Next page of index-price klines                |
| `mexc contract market fair-price-kline-next`        | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/kline/fair_price/{symbol}/next`    | Next page of fair-price klines                 |
| `mexc contract market funding-rate`                 | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate/{contractName}`       | Current funding rate of a contract             |
| `mexc contract market funding-rate-all`             | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate`                      | Current funding rate of all contracts          |
| `mexc contract market funding-rate-history`         | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate/history`              | Funding rate history (paged)                   |
| `mexc contract market funding-rate-history-of-day`  | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate/historyOfDay`         | Funding rate history over last N days (max 14) |
| `mexc contract market funding-rate-board-info`      | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate/board/info`           | Funding rate board summary info                |
| `mexc contract market funding-rate-board-list`      | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/funding_rate/board/list`           | Funding rate board list                        |
| `mexc contract market risk-reverse-history`         | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/risk_reverse/history`              | Risk-reserve fund history (paged)              |
| `mexc contract market risk-reverse-history-of-days` | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/risk_reverse/historyOfDays`        | Risk-reserve fund history by day               |
| `mexc contract market market-changes-list`          | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/market_changes/list`               | Contract movers list                           |
| `mexc contract market big-data-position`            | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/big_data/position`                 | Long/short position ratio                      |
| `mexc contract market big-data-active-trade-amount` | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/big_data/active_trade_amount`      | Platform active buy/sell amount                |
| `mexc contract market hot-contracts`                | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/hotContracts`                      | Hot contracts                                  |
| `mexc contract market rise-fall-rate-sort`          | `contract.market` | read   | no     | no       | yes        | `GET /api/v1/contract/rise_fall_rate/sort`               | 24h rolling rise/fall-rate ranking             |

## Permissions

> Authorization = resource(scope) × action(access). Every market operation is `read`.

| Operation                        | scope             | access |
| -------------------------------- | ----------------- | ------ |
| all operations in this subdomain | `contract.market` | read   |

## Usage Examples

```bash
# --- Contract detail / currencies / delivery ---
mexc contract market detail --symbol BTC_USDT
mexc contract market detail-v2 --symbol BTC_USDT,ETH_USDT
mexc contract market detail-v3 --symbol BTC_USDT --client web --scene 0
mexc contract market support-currencies --includePoint false
mexc contract market support-currencies-v2 --includePoint false
mexc contract market delivery-tips --symbol BTC_USDT --language en-US

# --- Prices ---
mexc contract market market-price-v2 --symbol BTC_USDT
mexc contract market ticker --symbol BTC_USDT
mexc contract market tickers --symbols BTC_USDT,ETH_USDT
mexc contract market spot-latest-price --currencies BTC,ETH

# --- Order book & trades ---
mexc contract market depth --symbol BTC_USDT --limit 20
mexc contract market depth-step-enable
mexc contract market depth-step --symbol BTC_USDT
mexc contract market deals --symbol BTC_USDT --limit 50

# --- Klines (last / index / fair price) ---
mexc contract market kline --symbol BTC_USDT --interval Min15 --limit 100
mexc contract market kline-recent --symbol BTC_USDT --interval Min15 --end 1730000000
mexc contract market kline-next --symbol BTC_USDT --interval Min15 --start 1730000000
mexc contract market index-price-kline --symbol BTC_USDT --interval Min15 --limit 100
mexc contract market index-price-kline-recent --symbol BTC_USDT --interval Min15 --end 1730000000
mexc contract market index-price-kline-next --symbol BTC_USDT --interval Min15 --start 1730000000
mexc contract market fair-price-kline --symbol BTC_USDT --interval Min15 --limit 100
mexc contract market fair-price-kline-recent --symbol BTC_USDT --interval Min15 --end 1730000000
mexc contract market fair-price-kline-next --symbol BTC_USDT --interval Min15 --start 1730000000
mexc contract market mini-klines --symbols BTC_USDT,ETH_USDT --limit 10

# --- Funding rate ---
mexc contract market funding-rate --contractName BTC_USDT
mexc contract market funding-rate-all
mexc contract market funding-rate-history --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract market funding-rate-history-of-day --symbol BTC_USDT --day 7
mexc contract market funding-rate-board-info
mexc contract market funding-rate-board-list

# --- Risk reserve ---
mexc contract market risk-reverse-history --symbol BTC_USDT --page_num 1 --page_size 20
mexc contract market risk-reverse-history-of-days --symbol BTC_USDT

# --- Rankings, market changes & big data ---
mexc contract market market-changes-list
mexc contract market hot-contracts --limit 10
mexc contract market rise-fall-rate-sort --riseSort true --limit 40 --timezone UTC+8
mexc contract market big-data-position --currency USDT --symbol BTC_USDT --type PLATFORM_TOTAL_USERS --interval H1
mexc contract market big-data-active-trade-amount --currency USDT --symbol BTC_USDT --interval H1
```

## Workflows

### Contract detail page (parallel reads keyed by symbol)

> User: "Open the BTC_USDT contract detail page"

1. `mexc contract market detail-v2 --symbol BTC_USDT` → contract spec (leverage / precision / risk limits / `depthStepList`, the merged-precision steps this contract supports)
2. In parallel, fetch the live data panel:
   - `mexc contract market ticker --symbol BTC_USDT` → last price, rise/fall, index price, fair price, funding rate
   - `mexc contract market deals --symbol BTC_USDT --limit 50` → recent trades
   - `mexc contract market funding-rate --contractName BTC_USDT` → current funding rate
3. (Optional) `mexc contract market ticker` (no symbol) → the whole-market ticker list, for cross-contract comparison panels

### Enrich the contract list with scene-tailored fields (detail-v2 + detail-v3)

> User: "List contracts including zero-fee-rate flag and multi-language full name"

1. `mexc contract market detail-v2` → base contract list (all, or by symbol)
2. `mexc contract market detail-v3 --client web --scene <scene> --params izfr,liqfr,fn` → scene-tailored extra fields (e.g. `izfr` zero-fee flag, `liqfr` liquidation fee rate, `fn` full name) not present in `detail-v2`
3. Merge step 2's result into step 1's list by `symbol` — `detail-v2` alone does not carry these extra fields.

### Funding rate comparison board (gated by config)

> User: "Show the cross-exchange funding rate comparison"

1. `mexc contract market funding-rate-board-info` → the exchange list and interval list this board supports
2. Only when **both** lists from step 1 are non-empty, continue:
   `mexc contract market funding-rate-board-list` → funding rate data for the whole set of contracts (typically polled at a fixed interval while the view is open)

### Switch merged-depth precision (valid steps come from contract detail)

> User: "Switch BTC_USDT's merged depth to a coarser step"

1. `mexc contract market detail-v2 --symbol BTC_USDT` → `depthStepList` is the set of merge-precision steps this contract supports; the `step` value used next must come from this list, not an arbitrary number
2. `mexc contract market depth-step --symbol BTC_USDT --step <step from step 1>` → merged depth at the selected precision

## Notes for Agents

**General rules**

- Retry only idempotent ops (idempotent=yes); every op here is idempotent.
- Unsure about parameters? run `mexc schema contract.market.<op>` instead of guessing.
- Common error/safety rules → load the `mexc-common` skill. This subdomain is public and needs no credential or login, so no auth skill is involved.

**Skill-specific notes**

- `symbol` vs `contractName`: some ops name the path/query parameter `symbol`, others `contractName`; both are the contract name (e.g. `BTC_USDT`).
- Kline interval names are the contract set (`Min1`…`Month1`), distinct from spot's `1m`…`1M`; kline `start`/`end` are Unix seconds.
- The `timezone` header (optional on most ranking/ticker ops, required on `rise-fall-rate-sort`) selects the timezone of the rise/fall figures.
