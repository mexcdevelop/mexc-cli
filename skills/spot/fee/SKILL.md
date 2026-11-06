---
name: mexc-spot-fee
scope: spot.fee
signed: yes
identity: yes
visibility: public
description: 'MEXC spot fee rate center: the caller''s trading fee rates (per pair, overall, by channel, with MX deduction, lowest), their zero-fee pairs, plus country zero-fee pairs and a pair''s default rule fee. Carries an API key; each request is signed. Triggers: "my trading fee / fee rate","fee for this pair","maker taker fee","fee with MX discount","my lowest fee","zero-fee pairs","default fee for a pair".'
---

> Auth: this subdomain carries an openapi API key (`identity: yes`) and the CLI signs each request (`signed: yes`) with the profile secret. A saved profile (API key + secret) is required. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · fee (fee rate center)

> Read-only spot subdomain: every operation is `access: read`. scope marks only the resource (`spot.fee`); the action lives in the access column. The caller is resolved from the API key — you never pass a memberId.

## Core Concepts

- **fee rate**: the maker / taker commission you pay to trade. The fee center
  returns the effective rate after your tier, discounts, and deductions.
- **symbol / symbolId**: the trading pair. The per-symbol fee queries accept
  either the pair name (`symbol`, e.g. `BTCUSDT`) or its `symbolId` — provide one.
- **MX deduction**: paying fees with MX at a discount. `symbol-fee-mx` returns
  the rate with the discount applied.
- **source / channel**: WEB / APP / API — the same pair can have a different
  fee per order channel. `symbol-fee-by-source` queries a specific channel.
- **zero-fee pairs**: pairs that trade with no fee — `zero-fee-mine` returns the
  caller's, `zero-fee-country` returns a country's.
- **rule fee**: a pair's default fee before any discount/deduction
  (`rule-symbol-default`), independent of the caller.

## Important Notes

- **API key + secret required**: every operation needs a saved
  profile (`mexc profile set <name>` in a terminal (it prompts for the key/secret, no flags),
  then `mexc profile use <name>`). The framework injects the API key and signs
  each request with the secret; you never pass a signature or a memberId as flags.
- **symbol or symbolId**: `symbol-fee` / `symbol-fee-mx` / `symbol-fee-by-source`
  need one of `--symbol` or `--symbolId`; passing neither is rejected.
- All operations here are reads — none change any state.

## Operations

> scope is the resource identifier (always `spot.fee`); the action is in the access column. signed/identity flag whether a signature/API key is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot fee symbol-fee` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/symbol/member/query` | The caller's fee rate for one pair (`--symbol` or `--symbolId`) |
| `mexc spot fee symbol-fee-mx` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/symbol/member/withMxDiscount` | The caller's fee for a pair including MX deduction (`--symbol` or `--symbolId`) |
| `mexc spot fee symbol-fee-by-source` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/symbol/member/queryBySource` | The caller's fee for a pair by channel (`--symbol`/`--symbolId`, `--source WEB\|APP\|API`) |
| `mexc spot fee symbol-low` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/symbol/member/low` | The caller's lowest-fee trading pairs |
| `mexc spot fee member-fee` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/member/all/query` | The caller's overall fee rate |
| `mexc spot fee zero-fee-mine` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/all/zero/query` | The caller's zero-fee trading pairs |
| `mexc spot fee zero-fee-country` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/zero/country/query` | Zero-fee pairs for a country (`--countryCode`) |
| `mexc spot fee rule-symbol-default` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/rule/symbol/default` | A pair's default rule fee, no discount/deduction (`--symbolId`) |
| `mexc spot fee symbol-strategy-default` | `spot.fee` | read | yes | yes | yes | `GET /api/cli/spot/fee/symbol/strategy/default` | A pair's base fee rate (default strategy rate, before member discounts/deductions) (`--symbolId` required) |

## Permissions

> Authorization = resource(scope) × action(access). Every fee operation is `read`.

| Operation | scope | access |
|-----------|-------|--------|
| symbol-fee / symbol-fee-mx / symbol-fee-by-source / symbol-low / member-fee / zero-fee-mine / zero-fee-country / rule-symbol-default / symbol-strategy-default | `spot.fee` | read |

## Usage Examples

```bash
# My fee rate for a pair (and with MX deduction)
mexc spot fee symbol-fee --symbol BTCUSDT
mexc spot fee symbol-fee-mx --symbol BTCUSDT

# My fee on the API channel; my overall fee; my lowest-fee pairs
mexc spot fee symbol-fee-by-source --symbol BTCUSDT --source API
mexc spot fee member-fee
mexc spot fee symbol-low

# My zero-fee pairs / a country's zero-fee pairs / a pair's default rule fee
mexc spot fee zero-fee-mine
mexc spot fee zero-fee-country --countryCode DEFAULT
mexc spot fee rule-symbol-default --symbolId <symbolId>
```

## Notes for Agents

- Implementation lives in `./api/` (one file declaring the operations; the actual
  request — signing, host resolution, sending — is delegated to the `pkg/`
  foundation).
- Inspect the exact request shape before calling with
  `mexc schema spot.fee.<operation>` (e.g. `spot.fee.symbol-fee`).
- All operations are read-only and safe to auto-approve.
- The caller is resolved from the API key, so never ask the user for a
  memberId — it is not a parameter.
- On a structured `failed_precondition` (e.g. `requires API credentials`), load
  the `mexc-auth` skill (credentials) and `mexc-common` (common error / safety
  rules), then replay; this skill does not describe login.
