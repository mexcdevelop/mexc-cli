---
name: mexc-spot-kline
scope: spot.kline
signed: no
identity: no
visibility: public
description: 'MEXC spot CLI kline data — public candlestick series for one trading pair, plus the earliest queryable window per period. No saved credential profile or signed request is required. Triggers: "kline / candlesticks", "chart data", "OHLC", "price history", "earliest kline time".'
---

# spot · kline (CLI kline data)

> CLI-gateway kline declarations. Both operations are public reads: a real call needs no saved API credential profile or signed request. The `/api/cli/spot/kline` routes are distinct from OpenAPI `/api/v3/klines` and return a chart-oriented series. Common error and safety rules → load the `mexc-common` skill.

## Core Concepts

- **symbolId**: the trading-pair id, e.g. `2`. `query` keys off it only — there is no `--symbol` here. Resolve it first with `mexc spot marketv2 symbols`.
- **interval**: the candle period. Case-sensitive, exactly ten legal values: `Min1` `Min5` `Min15` `Min30` `Min60` `Hour4` `Hour8` `Day1` `Week1` `Month1`. Shorthand like `1m` / `60m` / `1d` is **not** accepted.
- **start / end**: the query window, as Unix timestamps in **SECONDS**. A 13-digit millisecond value is rejected client-side (out of range) before the request is sent — the upstream has no unit check of its own and would otherwise silently multiply it by 1000, inflating the window ~1000x and returning a misleading "number of points" error instead of a unit error.
- **openPriceMode**: required. `FIRST_DEAL` = first trade of the period; `LAST_CLOSE` = previous period's close. (The `spot.marketv2` ticker operations use a different, numeric `openPriceMode` — `1` / `2`.)

## Resource Relationships

```
spot.kline
├── Series  query            (candlesticks for one symbolId over a window)
└── Meta    support-earliest (earliest queryable window start, per period)
```

## Response Shape

`query` returns a list of objects. **Every numeric value is a STRING**, and the keys are abbreviated:

| key | meaning |
|-----|---------|
| `t` | window start, Unix **seconds** |
| `o` | open price |
| `c` | close price |
| `h` | high price |
| `l` | low price |
| `v` | **base-asset volume** |
| `a` | **quote-asset amount** (turnover) |

## Important Notes

- **Public market data**: a real call does not need a saved API credential profile. The CLI does not attach an API-key identity or request signature.
- **All five `query` params are required** — `symbolId`, `interval`, `start`, `end`, `openPriceMode`. Omitting any is refused before the request is sent.
- **Single-query point cap**: the upstream limits `(end - start) / interval-step` to a server-side configured maximum. Exceeding it returns `The number of points is more than <N>` with the actual cap in the message — read `N` from the error rather than assuming a value.
- **A window older than what the cache holds returns an empty series, not an error.** Call `support-earliest` first and clamp `start` to the value it reports for your `interval`.
- **An unknown `symbolId` also returns an empty series with a success envelope** — an empty result does not prove the pair is wrong. Verify the id through `mexc spot marketv2 symbols`.
- The upstream's parallel-array kline variant is deliberately **not** exposed: it returns the same series but takes milliseconds, answers `code:200`, and uses `v` for quote amount instead of base volume. Use `query` for all kline access.

## Operations

> scope is the resource identifier (always `spot.kline`); the action is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot kline query`            | `spot.kline` | read | no | no | yes | `GET /api/cli/spot/kline/kline/query`      | Kline / candlestick series for one trading pair |
| `mexc spot kline support-earliest` | `spot.kline` | read | no | no | yes | `GET /api/cli/spot/kline/support/earliest` | Earliest queryable window start, per kline period |

## Permissions

> Authorization = resource(scope) × action(access). Every kline operation is `read`.

| Operation | scope | access |
|-----------|-------|--------|
| query / support-earliest | `spot.kline` | read |

## Usage Examples

```bash
# Earliest queryable window start for every period
mexc spot kline support-earliest

# 15-minute candles over a one-day window (SECOND-level timestamps)
mexc spot kline query --symbolId 2 --interval Min15 \
  --start 1786320000 --end 1786406400 --openPriceMode LAST_CLOSE

# Hourly candles, open price taken from the first trade of each period
mexc spot kline query --symbolId 2 --interval Min60 \
  --start 1786320000 --end 1786406400 --openPriceMode FIRST_DEAL
```

## Workflows

### Query a historical kline series by trading-pair name

> User: "Show me the 15-minute candles for BTC_USDT over the last day"

1. `mexc spot marketv2 symbol-trade --symbol BTC_USDT` → the pair's `id`, which is the `symbolId` the next step needs. The user gives a pair name, but `query` keys off `symbolId` only, so resolve it first. If the response carries no `id`, stop there — do not retry the next step with the name instead.
2. `mexc spot kline query --symbolId <id from step 1> --interval Min15 --start <seconds> --end <seconds> --openPriceMode LAST_CLOSE` → the kline series. For "the last N candles" take `start = end - N * interval-step`; split a wide range into several calls (for the single-query point cap and what an empty series means, see Important Notes).
3. Only when the earliest history is asked for, or step 2 came back empty and you need to tell whether the window was out of range: `mexc spot kline support-earliest` → the earliest queryable start per period (**milliseconds** — convert to seconds, then redo step 2).

## Notes for Agents

- Implementation lives in `./api/` (one descriptor per operation; the request is delegated to the `pkg/` foundation — no HTTP/signing here).
- This subdomain is public market data and does not require a saved API credential profile.
- Unsure about parameters? run `mexc schema spot.kline.<op>` instead of guessing.
- Both operations are reads and idempotent — safe to retry.
