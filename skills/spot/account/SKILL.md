---
name: mexc-spot-account
scope: spot.account
signed: yes
identity: yes
visibility: public
description: 'MEXC spot account — real-time push of the account''s own new trades over WebSocket. Requires an API key: the subscription authenticates by a signed one-shot exchange before it connects. Triggers: "my trades in real time","account trade stream","watch my fills","spot account WS".'
---

> Auth: this subdomain needs an openapi API key. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · account (spot account)

## Core Concepts

- **own trades**: the fills of the authenticated account itself, pushed as they happen — not the market-wide trade feed (that is `spot.marketv2 ws-trades`).
- **listenKey**: a short-lived subscription credential. The CLI obtains one with a signed request and dials with it; nothing about it is a flag you pass.

## Resource Relationships

```
Account
└── ws-deals    the account's own new trades, pushed (access: read)
```

## Important Notes

- **A subscription, not a request**: `ws-deals` streams JSON Lines until you stop it. `--listen-duration` and `--limit` are the two ways to bound it; with neither, it runs until interrupted. See the `mexc-ws` skill for their semantics.
- **`signed: no` at the operation level, `identity: yes`**: a WS-backed operation builds no signed request of its own, so the signing axis does not apply to it. It still needs the API key — the one-shot listenKey exchange the CLI sends on its behalf IS signed — so credentials are prepared exactly as for any signed operation.
- **`--env` and `--base-url` do not move the WS connection.** There is no test WebSocket gateway; the subscription reaches production by default. `--spot-ws-url` (or persistently, `MEXC_SPOT_WS_BASE_URL`) is the one way to redirect it for a single invocation (see the `mexc-ws` skill) — it governs only the WS connection, not the listenKey preflight, which is a genuine signed REST call and still follows `--base-url`/`MEXC_OPENAPI_BASE_URL` like any other signed operation. The two flags act on two different legs of this one command and can be set independently.
- **No parameters**: the stream is the account's own, so there is nothing to select.

## Operations

> scope is always the resource `spot.account`; the action (read/write) is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe. (visibility is a subdomain-level fact in the frontmatter, not a per-op column.)

| Command                           | scope | access | signed | identity | idempotent | Endpoint | Description |
|-----------------------------------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot account ws-deals`      | `spot.account` | read  | no  | yes | yes | `WS wss://wbs-api.mexc.com/ws#spot@private.deals` | Real-time push of the account's own new trades (WS; see the `mexc-ws` skill for `--listen-duration`/`--limit`) |

## Permissions

> Authorization = resource(scope) × action(access). scope is always `spot.account`.

| Operation | scope | access |
|-----------|-------|--------|
| ws-deals | `spot.account` | read |

## Usage Examples

```bash
# Watch this account's fills for 30 seconds
mexc spot account ws-deals --listen-duration 30s

# Take the next 5 fills and stop
mexc spot account ws-deals --limit 5

# Preview what would be subscribed, without connecting
mexc spot account ws-deals --dry-run
```

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op (idempotent=no).
- Unsure about parameters? run `mexc schema spot.account.<op>` instead of guessing.
- On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill (credentials) and the `mexc-common` skill (common error / safety / security rules), then replay the command; this skill does not describe login.

**Skill-specific notes**
- Implementation lives in `./api/` (descriptors only; the connection and signing are delegated to the `pkg/` foundation).
- Always bound the stream (`--listen-duration` or `--limit`) unless the user asked to watch indefinitely — an unbounded subscription does not return on its own.
- Never print the secret key in logs or output.
