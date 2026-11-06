---
name: mexc-futures-market
scope: futures.market
signed: no
identity: no
visibility: public
description: >-
  Futures (contract) public market data via WebSocket — real-time ticker,
  order-book depth, and trade pushes. Public data, no API key required.
  Triggers: "futures ticker","futures depth","futures trades","contract
  market data".
---

# futures · market (public market data via WS)

> This subdomain carries **no REST operations**: every capability here is a WebSocket subscription. There is no one-shot equivalent — to get a single current snapshot, subscribe with `--limit 1` (e.g. `mexc futures market ws-ticker --limit 1`) rather than looking for a `ticker-price`-style command.
>
> Public subdomain: every operation is a read needing no signature or identity (`signed: no`, `identity: no`). See the `mexc-ws` skill for the execution model every WS operation in this product shares (`--listen-duration`/`--limit`, JSON Lines output, no auto-reconnect, `--dry-run` preview).

## Core Concepts

- **symbol**: a contract symbol, e.g. `BTC_USDT` (base and quote separated by an underscore — unlike spot's `BTCUSDT`).
- `ws-ticker`'s `--symbol` is optional: omit it for a whole-market ticker push (every contract, one push.tickers frame per interval), or pass it for one contract's ticker only. `ws-depth`/`ws-trades` are always per-contract and require `--symbol`.

## Resource Relationships

```
Futures contract market
├── ws-ticker   ticker pushes: whole-market if --symbol omitted, one contract if given
├── ws-depth    order-book depth pushes for one contract (--symbol required)
└── ws-trades   trade (new-deal) pushes for one contract (--symbol required)
```

## Operations

> scope is the resource identifier (always `futures.market`); the action is in the access column. The Endpoint cell names the WS URL and channel, not an HTTP method/path — this subdomain has none.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc futures market ws-ticker` | `futures.market` | read | no | no | yes | `WS wss://contract.mexc.com/edge#sub.ticker(s)` | Real-time ticker pushes: whole market if `--symbol` omitted, one contract if given (e.g. `BTC_USDT`) |
| `mexc futures market ws-depth`  | `futures.market` | read | no | no | yes | `WS wss://contract.mexc.com/edge#sub.depth`   | Real-time order-book depth pushes (`--symbol` required, e.g. `BTC_USDT`) |
| `mexc futures market ws-trades` | `futures.market` | read | no | no | yes | `WS wss://contract.mexc.com/edge#sub.deal`    | Real-time trade (new-deal) pushes (`--symbol` required, e.g. `BTC_USDT`) |

## Permissions

> Authorization = resource(scope) × action(access). Every market operation is `read`.

| Operation | scope | access |
|-----------|-------|--------|
| ws-ticker / ws-depth / ws-trades | `futures.market` | read |

## Notes for Agents

- Implementation lives in `./api/` (descriptors only; connection execution is delegated to the framework).
- `--env`/`--base-url` have **no effect** on any operation here: MEXC publishes no testnet WS gateway, so every WS operation talks to the production endpoint by default regardless of the active profile's env. `--dry-run` surfaces this as `envIgnored: true` in its preview. `--futures-ws-url` (or persistently, `MEXC_FUTURES_WS_BASE_URL`) is the one flag that redirects the gateway these three dial, for one invocation (`--spot-ws-url` has no effect here); see the `mexc-ws` skill.
- See the `mexc-ws` skill before calling any operation here for the first time — it documents `--listen-duration`/`--limit`, the JSON Lines output shape, and the no-auto-reconnect behavior common to every WS operation in this product.
