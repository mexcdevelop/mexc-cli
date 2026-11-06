---
name: mexc-futures-account
scope: futures.account
signed: no
identity: yes
visibility: public
description: >-
  Futures (contract) account's own new-trade push via WebSocket. Requires an
  API key with futures trading permission enabled. Triggers: "futures own
  trades","futures fills","futures order deal push".
---

> Auth: this subdomain needs an openapi API key with futures trading permission enabled. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# futures · account (private push via WS)

> This subdomain carries **no REST operations**: its one capability is a WebSocket subscription. There is no one-shot equivalent for it.
>
> `signed: no` does **not** mean "no credentials needed" — it means this operation never builds a signed REST request (it authenticates with an in-connection WS login handshake, not a query-string signature), which is why `identity: yes` still applies: the same API key every other identity-carrying operation in this product uses. See the `mexc-ws` skill for the execution model every WS operation shares (`--listen-duration`/`--limit`, JSON Lines output, no auto-reconnect, `--dry-run` preview).

## Core Concepts

- **ws-deals**: pushes the account's own new trades (order fills) as they happen — the futures counterpart of `spot account ws-deals`.
- Authentication happens **after** the connection is open: a `login` frame carrying the API key and a signed timestamp is sent first, and the server must reply `rs.login` before the subscription proceeds. This is the opposite timing from Spot's `ws-deals`, which authenticates by fetching a `listenKey` **before** dialing.

## Resource Relationships

```
Futures account
└── ws-deals   the account's own new-trade push (access: read)
```

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc futures account ws-deals` | `futures.account` | read | no | yes | yes | `WS wss://contract.mexc.com/edge#push.personal.order.deal` | Real-time push of the account's own new trades (WS, in-connection login) |

## Permissions

> Authorization = resource(scope) × action(access).

| Operation | scope | access |
|-----------|-------|--------|
| ws-deals | `futures.account` | read |

## Notes for Agents

- Implementation lives in `./api/` (descriptors only; the login frame is constructed here as data, but the connection execution — dial, sending it, waiting for `rs.login`/`rs.error` — is delegated to the framework).
- A login rejection (invalid or expired API key) is reported as a `validation`/`failed_precondition` error, distinct from a subscription rejection (`api`/`subscription_rejected`) — the two share the same wire frame shape but mean different things, and this product's error classification keeps them apart so the hint points at the right fix (check credentials vs check parameters).
- `--env`/`--base-url` have **no effect**: MEXC publishes no testnet futures WS gateway, so the login and subscription target production by default regardless of the active profile's env. `--futures-ws-url` (or persistently, `MEXC_FUTURES_WS_BASE_URL`) is the one flag that redirects it for one invocation (`--spot-ws-url` has no effect here); see the `mexc-ws` skill.
- See the `mexc-ws` skill before calling this operation for the first time.
