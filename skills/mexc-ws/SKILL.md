---
name: mexc-ws
description: Cross-cutting execution model every WebSocket-backed operation in mexc follows — --listen-duration/--limit, the JSON Lines output shape, error handling, no auto-reconnect, and --dry-run's preview. Every WS subdomain skill (spot.market, spot.account, futures.market, futures.account) points here instead of repeating it. Read this before running any `ws-*` command for the first time.
visibility: public
---

# mexc · WebSocket execution model

A `ws-*` command (`spot marketv2 ws-ticker`, `spot account ws-deals`, `futures market ws-depth`, `futures account ws-deals`, ...) is a **long-lived subscription**, not a one-shot request: it opens a connection, subscribes to a channel, and prints every pushed message as it arrives. This page documents what every one of them has in common, so the subdomain-specific skills only need to say what the operation is, not how it runs.

## `--listen-duration` / `--limit`: bounding a call

Two optional flags, usable together or separately:

- `--listen-duration <Go duration>` — stop after this long, e.g. `--listen-duration 10s`, `--listen-duration 1.5m`.
- `--limit <N>` — stop after collecting this many messages.

**Neither flag changes what a message looks like** — only when the command stops:

- **Bounded** (either flag set): the command exits with code `0` on its own once the duration elapses or the count is reached.
- **Unbounded** (both omitted, the default): the command runs until you interrupt it (Ctrl+C / SIGTERM, exit `0`) or the connection ends unexpectedly (exit non-zero — see below).

For an agent invoking this non-interactively, **always pass at least one of these** — an unbounded call blocks forever from the caller's perspective. `--limit 1` is often enough to answer "what does this look like right now".

## Output: JSON Lines, one message per line

Every message — bounded or unbounded — is written as one compact JSON object per line (not the indented, multi-line JSON an ordinary operation prints):

```json
{"channel":"push.tickers","symbol":"BTCUSDT","body":{"...":"..."}}
{"channel":"push.tickers","symbol":"BTCUSDT","body":{"...":"..."}}
```

Pipe it through `jq -c` or read it line by line; do not expect one buffered JSON document for the whole call.

## Errors: also one line, distinguished by an `error` key

A subscription rejection, a login rejection, or an ack timeout is reported as one line carrying an `error` object instead of a data message, then the command exits non-zero:

```json
{"error":{"type":"api","subtype":"subscription_rejected","channel":"push.tickers","message":"Reason: Blocked!"}}
```

Route on `type`/`subtype` exactly as for any other mexc error (see `mexc-common`):

| `type` | `subtype` | Meaning |
|---|---|---|
| `network` | `network_transport` | Could not connect, or the connection ended unexpectedly |
| `api` | `subscription_rejected` | The server explicitly refused the subscription (or a futures login) |
| `api` | `subscription_ack_timeout` | The server never acknowledged the subscription |
| `validation` | `failed_precondition` | Missing credentials, **or** (futures `ws-deals` only) the API key itself was rejected during login — check credentials, not parameters |

A rejection or timeout is reported **immediately**, independent of `--listen-duration`/`--limit` — the command does not wait out your requested duration/count if the server has already said the subscription will never produce data.

## No auto-reconnect — by design

If the connection drops mid-stream (network issue, the upstream's own session limit, ...), the command does **not** silently reconnect. It reports the failure and exits non-zero. A `ws-*` command here is for on-demand observation, not a durable background service — if you need continuous coverage, supervise and restart the command yourself.

## `--dry-run`: preview without connecting

Every `ws-*` command supports `--dry-run`: it never opens a connection, and prints what it would connect to instead:

```json
{
  "operation": "ws-ticker",
  "url": "wss://wbs-api.mexc.com/ws",
  "channels": ["spot@public.aggre.bookTicker.v3.api.pb@100ms@BTCUSDT"],
  "signed": false,
  "bounded": false,
  "envIgnored": true
}
```

`envIgnored: true` is not specific to this call — it is **always true** for every WS operation in this product: MEXC publishes no testnet WebSocket gateway, so `--env`/`--base-url` never change where a `ws-*` command connects; it always talks to the production endpoint by default (see `--spot-ws-url`/`--futures-ws-url` below for the one way to redirect it). A private channel's dry-run preview never performs its real credential exchange (Spot's listenKey fetch, Futures' login handshake) and works with no saved profile, exactly like an ordinary signed operation's dry-run.

## `--spot-ws-url` / `--futures-ws-url`: redirecting the gateway for one call

`--base-url` has no effect on a `ws-*` command (see above) — a WS operation needs its own override, because there are two independent WS gateways (spot and futures), not one REST host. Two flags, each pinning a different protocol's gateway for this invocation only:

- `--spot-ws-url <url>` — overrides the address every spot `ws-*` command dials (`spot marketv2 ws-ticker`/`ws-depth`/`ws-trades`, `spot account ws-deals`). Has no effect on a futures operation.
- `--futures-ws-url <url>` — overrides the address every futures `ws-*` command dials (`futures market ws-ticker`/`ws-depth`/`ws-trades`, `futures account ws-deals`). Has no effect on a spot operation.

Both flags are registered on every `ws-*` command regardless of which protocol it actually speaks (there is no per-subdomain gating at this layer), so passing the wrong one for a given command is silently a no-op rather than an error — `spot marketv2 ws-ticker --futures-ws-url ...` still dials the default (or `--spot-ws-url`-overridden) spot gateway. Omit either flag to use the production default. `--dry-run`'s `url` field reflects whichever address (default or overridden) that invocation would actually dial, so an override is visible in the preview without connecting.

`MEXC_SPOT_WS_BASE_URL` / `MEXC_FUTURES_WS_BASE_URL` persist the same override across invocations (a shell profile, a CI env), the WS counterpart of `MEXC_OPENAPI_BASE_URL` for the REST host — same precedence order too: the matching `--*-ws-url` flag wins over the env var, which wins over the production default.

For Spot's private channel (`ws-deals`), the override applies to the WS connection itself, not to the listenKey preflight: that preflight is a genuine signed REST call and follows `--base-url`/`MEXC_OPENAPI_BASE_URL` exactly like any other signed operation — `--spot-ws-url` and `--base-url` govern two different legs of that one command and can be set independently.

## Not exposed over MCP

A `ws-*` operation is a CLI-only capability today; it is withheld from the `mcp` command's tool set the same way a download or a multipart upload is — an MCP tool call is a single request-response, with no protocol convention yet for "this call streams a series of messages before returning".
