---
name: mexc-spot-marketv2
scope: spot.marketv2
signed: yes
identity: yes
visibility: public
description: 'MEXC spot CLI market data (V2) — grouped symbol lists, market tickers, coin introductions and risk notes, concept plates, precision-limited depth, recent deals, and ranking markets. Public market-data operations need no saved profile; account-aware operations require a saved API credential profile and signed CLI requests. Triggers: "symbol list / grouped symbols", "market ticker", "coin introduction / risk", "concept plate / sector", "precision depth", "recent deals", "meme coins", "new coin list", "timezone list".'
---

# spot · marketv2 (CLI market data, V2)

> CLI-gateway market data declarations. Most REST operations are public reads that need no saved API credential profile or signed request. `symbols`, `tickers`, `symbol-trade`, and `user-eftd-info` retain account-aware behavior and require a saved API credential profile. It targets `/api/cli/spot/market-v2`, which returns richer, display-oriented objects than a raw market feed: sectors, introductions, and rankings. On a structured credential error for a signed operation, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

## Core Concepts

- **symbol**: a trading pair code in BASE_QUOTE form, e.g. `BTC_USDT`. This subdomain's upstream indexes pairs with the underscore; the no-separator form (`BTCUSDT`), which `spot.market`'s OpenAPI endpoints use, is a silent lookup miss here — `--symbol` is pattern-validated client-side to catch it before the request is sent.
- **symbolId**: the numeric/string trading-pair id. Several operations accept **either** `--symbol` **or** `--symbolId` (pass one).
- **coinId** / **coinName**: identify a coin; `coin-introduce` accepts either, `coin-risk` needs `--coinId`.
- **plateId**: a concept-plate (sector) id.
- **openPriceMode**: on the ticker operations this is numeric — `1` = first trade of the period, `2` = previous period's close (default `1`). (The `spot.kline` subdomain uses a different, string-valued `openPriceMode`.)
- **Response field keys are abbreviated**: display VOs use short keys with the full name in parentheses in the API doc, e.g. `sbl(symbol)`, `cd(coinId)`, `ps(priceScale)`, `qs(quantityScale)`, `c(closePrice)`. Treat responses as opaque display objects unless you need a specific field.

## Resource Relationships

```
spot.marketv2
├── Symbols        symbols / api-available-symbols / latest-symbols
├── Tickers        tickers / ticker
├── Trade page     symbol-trade (fee rates / order types / order limits)
├── Coins          coin-introduce / coins-introduce / coin-risk / coin-delay / coin-new-list / meme-coins / st-coins
├── Concept plates concept-plates / concept-plate-introduce / concept-plate-amount
├── Order book     depth / deals
└── Rankings/misc  stock-ranking-markets / metal-ranking-markets / timezone-list / user-eftd-info
```

## Important Notes

- **Authentication varies by operation**: `symbols`, `tickers`, `symbol-trade`, and `user-eftd-info` require a saved API credential profile and signed request. Every other REST operation listed below is public and does not send an API-key identity or request signature.
- **Locale/timezone are request headers**, exposed as flags: `--language` (default `en-US`), `--convertLanguage`, `--timezone` (default `UTC+8`). They are declared as header params, so a value is sent on the wire as a header, not a query key.
- **`--symbol` or `--symbolId`**: `ticker`, `symbol-trade`, and (in `spot.kline`) the kline query accept either — pass exactly one. `depth` and `deals` key off `--symbolId`. `--symbol` must be BASE_QUOTE (e.g. `BTC_USDT`); `symbols` does not return this form pre-assembled — derive it as `vn` + `_` + the market group key (see Workflows).
- **`coins-introduce` takes a list**: pass `--coinIds` as a comma-separated list, e.g. `--coinIds 128,129`.
- **`ws-ticker`/`ws-depth`/`ws-trades` are public WebSocket subscriptions** against the exchange's spot v3 public gateway. Like the public REST operations, they need no saved credential profile; the four account-aware REST operations retain the subdomain default authentication behavior. See the `mexc-ws` skill for the shared `--listen-duration`/`--limit`/JSON-Lines execution model, and for `--spot-ws-url` (redirects the gateway these three dial for one invocation; `--futures-ws-url` has no effect here).
- **`symbols` and `tickers` are hidden-filtered**: the result is narrowed by locale, request IP and the calling account, so a pair missing from the list may be hidden rather than nonexistent. `symbol-trade` reports the same condition as `sst = 2` (NOT_EXIST) instead of omitting the pair.
- **One-of-two required pairs** (the CLI cannot enforce this, the upstream refuses the call): `ticker` and `symbol-trade` need `--symbol` **or** `--symbolId`; `coin-introduce` needs `--coinId` **or** `--coinName`. Passing neither returns a `500` envelope naming both fields. `depth` and `deals` key off `--symbolId` only.
- **`coins-introduce` takes a list**: pass `--coinIds` as a comma-separated list with no spaces, e.g. `--coinIds 128,129`.
- **Large result sets**: `tickers` returns every pair (order of hundreds) and `api-available-symbols` returns thousands. Filter or aggregate locally rather than pushing the whole payload into context.

## Edge Cases

- `depth --decimal` has **two** independent rules and two different errors: `-1129` = not a power of ten (`2`, `0.5`, `1.0`, `0.10` all fail); `-1131` = a power of ten but outside the pair's price-scale window. The window is roughly `priceScale - 6 .. priceScale + 1` for a high-precision pair and `priceScale - 4 .. priceScale + 1` otherwise, so a value that works for one pair fails for another. Read `ps` (priceScale) from `symbols` first. The CLI pre-checks the power-of-ten rule only.
- `depth` at the **edge** of that window returns empty `bids` / `asks` with a success envelope rather than an error — an empty book is not proof the market is empty; step one bucket inward and retry.
- `depth --count` accepts only `20` / `30` / `40`. `1`, `5`, `10`, `50`, `100` are all refused (`-1130`).
- `tickers --openPriceMode` accepts only `1` / `2`; any other value returns a `500` envelope with `wrong openPriceMode value!`.
- **Unknown ids do not error.** `coin-introduce`, `coins-introduce`, `coin-risk` and `concept-plate-introduce` all answer with a success envelope and empty data for an id that does not exist, so an empty result cannot be distinguished from a typo. Re-check the id against `symbols` / `coin-new-list` before concluding the data is absent.
- **An empty result may also mean a lookup failed.** A success envelope can come back with a field missing instead of an error — most visibly on `symbol-trade`, whose fee fields (`omf`, `otf`, `tfr`, `mfr`) can disappear entirely, still under `code:0`. Treat "field absent" as unknown, never as zero.
- `symbol-trade --symbolFee false` drops `tfr` / `mfr` / `omf` / `otf` on purpose; `bfr` (buy fee rate) is still returned.
- **`openPriceMode` is not the same parameter across subdomains**: numeric (`1` / `2`) here, string (`FIRST_DEAL` / `LAST_CLOSE`) in `spot.kline`.

## Operations

> scope is the resource identifier (always `spot.marketv2`); the action is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot marketv2 symbols`                 | `spot.marketv2` | read | yes | yes | yes | `GET /api/cli/spot/market-v2/symbolsV2`               | All spot trading pairs, grouped by market (V2); hidden pairs are filtered out by locale / IP / account |
| `mexc spot marketv2 tickers`                 | `spot.marketv2` | read | yes | yes | yes | `GET /api/cli/spot/market-v2/tickers`                 | 24h ticker list for all trading pairs; hidden pairs are filtered out by locale / IP / account |
| `mexc spot marketv2 ticker`                  | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/symbol/ticker`           | 24h ticker detail for a single trading pair |
| `mexc spot marketv2 symbol-trade`            | `spot.marketv2` | read | yes | yes | yes | `GET /api/cli/spot/market-v2/symbol/trade`            | Trade-page info for a pair: fee rates, order types, order limits |
| `mexc spot marketv2 coin-delay`              | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/coin/delay`              | Coins shown with a delay notice, and their delay-announcement links |
| `mexc spot marketv2 coin-introduce`          | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/coin/introduce`          | Detailed introduction for a single coin |
| `mexc spot marketv2 coins-introduce`         | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/coins/introduce`         | Brief introduction for multiple coins in one call |
| `mexc spot marketv2 coin-risk`               | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/coin/risk`               | Risk-warning message for a coin (status + text) |
| `mexc spot marketv2 coin-new-list`           | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/coin/new/list`           | New-coin ranking list |
| `mexc spot marketv2 st-coins`                | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/stCoins`                 | ST-label coins and the markets each trades in |
| `mexc spot marketv2 api-available-symbols`   | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/api/available/symbols`   | Set of trading pairs available for API trading |
| `mexc spot marketv2 latest-symbols`          | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/latest/symbols`          | Most recently listed spot trading pairs |
| `mexc spot marketv2 depth`                   | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/depth/v4`                | Order-book depth at a given price precision and level count |
| `mexc spot marketv2 deals`                   | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/deals/v2`                | Most recent trades for a trading pair |
| `mexc spot marketv2 concept-plates`          | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/concept/platesV2`        | Concept-plate (sector) list (V2) |
| `mexc spot marketv2 concept-plate-introduce` | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/concept/plate/introduce` | Introduction for a single concept plate |
| `mexc spot marketv2 concept-plate-amount`    | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/concept/plate/amount`    | 24h turnover of each symbol in a concept plate |
| `mexc spot marketv2 meme-coins`              | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/meme/coins`              | Meme-coin list (with chain info) |
| `mexc spot marketv2 timezone-list`           | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/timezone/list`           | Timezone identifiers the system supports |
| `mexc spot marketv2 user-eftd-info`          | `spot.marketv2` | read | yes | yes | yes | `GET /api/cli/spot/market-v2/user/eftd_info`          | Current user's EFTD (first-deposit benefit) eligibility info |
| `mexc spot marketv2 stock-ranking-markets`   | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/stockRankingMarkets`     | Markets supported by the US-stock ranking |
| `mexc spot marketv2 metal-ranking-markets`   | `spot.marketv2` | read | no | no | yes | `GET /api/cli/spot/market-v2/metalRankingMarkets`     | Markets supported by the metal ranking |
| `mexc spot marketv2 ws-ticker` | `spot.marketv2` | read | no | no | yes | `WS wss://wbs-api.mexc.com/ws#spot@public.aggre.bookTicker` | Real-time best bid/ask ticker pushes (WS, public, `--symbol` required; see the `mexc-ws` skill for `--listen-duration`/`--limit`) |
| `mexc spot marketv2 ws-depth`  | `spot.marketv2` | read | no | no | yes | `WS wss://wbs-api.mexc.com/ws#spot@public.aggre.depth`      | Real-time incremental order-book depth pushes (WS, public, `--symbol` required) |
| `mexc spot marketv2 ws-trades` | `spot.marketv2` | read | no | no | yes | `WS wss://wbs-api.mexc.com/ws#spot@public.aggre.deals`      | Real-time trade (new-deal) pushes for a symbol (WS, public, `--symbol` required) |

## Permissions

> Authorization = resource(scope) × action(access). Every marketv2 operation is `read`.

| Operation | scope | access |
|-----------|-------|--------|
| symbols / tickers / ticker / symbol-trade / coin-delay / coin-introduce / coins-introduce / coin-risk / coin-new-list / st-coins / api-available-symbols / latest-symbols / depth / deals / concept-plates / concept-plate-introduce / concept-plate-amount / meme-coins / timezone-list / user-eftd-info / stock-ranking-markets / metal-ranking-markets | `spot.marketv2` | read |
| ws-ticker / ws-depth / ws-trades | `spot.marketv2` | read |

## Usage Examples

```bash
# All trading pairs, grouped by market
mexc spot marketv2 symbols

# 24h ticker detail for one pair (previous-close open price)
mexc spot marketv2 ticker --symbol BTC_USDT --openPriceMode 2

# Trade-page info (fees / order types / limits), Korean locale
mexc spot marketv2 symbol-trade --symbol BTC_USDT --language ko-KR

# Brief intro for several coins at once
mexc spot marketv2 coins-introduce --coinIds 128,129

# Precision-limited order book. --decimal must be a power of ten inside the
# pair's price-scale window, so read ps from `symbols` first; --count is 20/30/40.
mexc spot marketv2 symbols            # -> pick a symbolId, note its ps
mexc spot marketv2 depth --symbolId 2 --decimal 0.01 --count 20

# 24h turnover of each symbol in a concept plate
mexc spot marketv2 concept-plate-amount --plateId 12
```

## Workflows

### View the order book or the recent trades by trading-pair name

> User: "Show me the order book for BTC_USDT"

1. `mexc spot marketv2 symbol-trade --symbol BTC_USDT` → the pair's `id` plus its price scale `ps`. `depth` and `deals` key off `symbolId` only, so resolve it first; `ps` decides which price bucket the next step may ask for. If the response carries no `id`, stop there.
2. Pick one according to what the user asked for (both depend on step 1 only and have no order between them — you do not need to run both):
   - `mexc spot marketv2 depth --symbolId <id from step 1> --decimal <a power of ten allowed by ps> --count 20` → the order book (for the `--decimal` / `--count` rules see Edge Cases)
   - `mexc spot marketv2 deals --symbolId <id from step 1>` → the most recent trades

### View a concept plate's introduction or its 24h turnover by plate name

> User: "How does turnover look for the precious metals plate?"

1. `mexc spot marketv2 concept-plates` → the plate list; locate the target plate's `id` by name and carry it as `plateId`. The user names a plate, but the next step keys off `plateId` only.
2. Pick one according to what the user asked for (both depend on step 1 only and have no order between them — you do not need to run both):
   - `mexc spot marketv2 concept-plate-introduce --plateId <id from step 1>` → the plate introduction
   - `mexc spot marketv2 concept-plate-amount --plateId <id from step 1>` → the 24h turnover

### Check a coin's risk warning by coin name

> User: "Does BTC have a risk warning?"

1. `mexc spot marketv2 coin-introduce --coinName BTC` → the coin record; take its `id` as the `coinId` for the next step. That response already carries the coin introduction, so there is no need to fetch it again. If what you hold is a trading pair rather than a coin name, use `cd` (the coin) from `mexc spot marketv2 symbol-trade --symbol BTC_USDT` — not `id`, which is the pair.
2. `mexc spot marketv2 coin-risk --coinId <id from step 1>` → the risk status. `coin-risk` keys off `coinId` only, and a wrong identifier still answers with a success envelope (see the unknown-id entry under Edge Cases), so pass the value resolved in step 1 — never a coin name or a pair id.

**Assembling `--symbol` when you only have the `symbols` output**: the pairs sit under `data.symbols`, keyed by market group — `{ data: { symbols: { <group>: [ { vn, cd, ... } ] } } }`. `vn` is the base currency (e.g. `ETH`), not the full pair. Assemble `--symbol` yourself as `vn` + `"_"` + the group key (e.g. group `USDT` + `vn` `ETH` -> `ETH_USDT`). The same `vn` recurs under different groups, so the group key is what decides the quote side (`ETH` under `BTC` -> `ETH_BTC`). Prefer `--symbolId` when you already have it; it needs no assembly.

## Notes for Agents

- Implementation lives in `./api/` (one descriptor per operation; the request is delegated to the `pkg/` foundation — no HTTP/signing here).
- Only `symbols`, `tickers`, `symbol-trade`, and `user-eftd-info` require a saved API credential profile. The other REST operations and all WebSocket operations are public.
- Unsure about parameters? run `mexc schema spot.marketv2.<op>` instead of guessing (it shows each param's `in` location, so you can see which values travel as headers).
- All operations are reads and idempotent — safe to retry.
