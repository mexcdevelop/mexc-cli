<!--
SKILL.md template — copy to skills/<domain>/<sub>/SKILL.md, fill in the prompts,
and delete these comments plus every <placeholder> before committing.
Contract: ../AGENTS.md §4. Classification axes: ../SUBDOMAINS.md.

Enforced by check-boundary (CI):
  - all six frontmatter fields present (name/scope/signed/identity/visibility/description);
  - name == mexc-<domain>-<sub> (the namespaced install id);
  - scope == <domain>.<sub> (resource only, no action suffix);
  - signed/identity ∈ {yes,no}; visibility ∈ {public,internal};
  - a Permissions table carrying an access column.
signed/identity are SUB-DOMAIN defaults and must agree, as facts, with the
`const Signed` / `const Identity` at the end of api/*.go — the doc records
"is a signature required" and "is a login identity required" as yes/no, the Go
records which concrete scheme/identity. A single Operation may override either.

Sections: REQUIRED = frontmatter / Operations / Permissions.
          RECOMMENDED = Core Concepts / Resource Relationships / Important Notes /
                        Usage Examples / Workflows / Notes for Agents.
Everything in a shipped SKILL.md is English.
-->
---
name: mexc-<domain>-<sub>          # e.g. mexc-spot-market, must equal mexc-<domain>-<sub> (namespaced install id)
scope: <domain>.<sub>              # resource identifier only, e.g. spot.kline (no .read/.write action suffix)
signed: <yes or no>                # sub-domain default: does a call need a signature? matches the Operations table's signed column; a per-operation value wins
identity: <yes or no>              # sub-domain default: does a call need a login identity? matches the Operations table's identity column; a per-operation value wins
visibility: <public or internal>   # which product mounts this sub-domain
description: >-                     # block scalar: the value goes on the indented lines below, so colons and "quotes" inside need no escaping
  <what it does> — <sub-capabilities>. Triggers: "<English phrase 1>","<English phrase 2>".
  [optional] Do not use for <scenario> (use <domain>.<other-sub>).
# description must be valid YAML. Prefer the `>-` block scalar above, or quote the
# whole value (use single quotes when it contains "..."). Never leave a bare
# `: ` (colon+space) in an unquoted value — it fails to parse, `npx skills`
# silently drops the whole skill, and check-boundary rejects it in CI.
---

<!-- If this sub-domain needs an identity (identity: yes) or performs ANY write, add a ONE-LINE Auth banner here, e.g.:
     > Auth: this subdomain needs an API key[ and performs writes]. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.
     A read-only public sub-domain (identity: no, no writes) does not need this line. -->

# <domain> · <sub>

<!-- One line stating this subdomain's purpose. Authorization model = resource(scope) × action(access). -->

## Core Concepts

<!-- Core entities + ID / value formats. Example: symbol looks like BTCUSDT; orderId is a numeric string. -->
<!-- A placeholder subdomain may omit this section for now and fill it in when wiring up real operations. -->

## Resource Relationships

<!-- ASCII tree of entity relationships. Delete this section if there is no clear hierarchy.
Symbol (BTCUSDT)
├── Ticker
├── Depth (order book)
└── Kline
-->

## Important Notes
<!-- Business FACTS only. Operating discipline and generic safety rules belong in Notes for
     Agents; idempotency belongs in the Operations table. The goal is to teach the agent this
     business and keep it out of traps. Typical kinds (delete the lines that do not apply):
     - Defaults and caps: parameter defaults, max rows returned, max time range
       (e.g. depth defaults to 100, caps at 5000; history ≤ 7 days).
     - Ambiguous parameters: how to choose between two easily confused ones (e.g. "quantity"
       vs "amount"); a bare unitless number should be clarified before acting.
     - Expected failures: which errors are normal rather than retryable (e.g. a clock skew
       breaking the signature, or --read-only refusing a write).
     - Credential requirements: a signed operation needs an active profile
       (mexc profile use); a requires-credentials error without one is expected.
     - Precision and limits: order precision, LOT_SIZE, minimum notional, pagination and
       batch caps — the rules that get a request rejected. -->

## Operations
<!-- scope is always the resource; the action lives in the access column. signed / identity /
     idempotent are marked per operation, all as yes/no.
       signed   = is a request signature required (the key/secret scheme)? Unrelated to whether
                  a human must log in.
       identity = is a login identity required? yes = a real person must authenticate first, and
                  the CLI maintains that session; no = public, or a signature alone suffices.
                  signed and identity are orthogonal: one is a key signature, the other a login.
                  WHICH identity is decided by `const Identity` in Go — the table says only
                  yes/no, and the credential flow itself belongs to the shared `mexc-auth`
                  skill (generic error/safety rules: `mexc-common`), not to this file.
       idempotent = does repeating the call converge to the same state with no extra side
                  effect? (Operation.IdempotentHint; defaults read=yes / write=no, overridable.) -->

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc <domain> <sub> <operation>` | `<domain>.<sub>` | read | no | no | yes | `<METHOD> <path>` | <one-line description> |

## Permissions

<!-- Required (enforced by check-boundary): scope must equal the resource <domain>.<sub>; the action goes in the access column. -->

| Operation | scope | access |
|-----------|-------|--------|
| <operation> | `<domain>.<sub>` | read |

## Usage Examples
<!-- RECOMMENDED. One runnable invocation per command (or per meaningful variant, e.g. LIMIT vs
     MARKET for place), with intent-level parameters and real values. Single commands only —
     multi-step business flows belong in Workflows. For the full parameter set point the reader
     at `mexc schema`; do not restate the Operations table. -->
```bash
# Format example only (taken from spot trade) — replace with this sub-domain's real commands.
# Market-buy BTC with 100 USDT (quoteOrderQty = how much USDT to spend)
mexc spot trade place --symbol BTCUSDT --side BUY --type MARKET --quoteOrderQty 100
# Market-sell 0.001 BTC (quantity = how much BTC to sell)
mexc spot trade place --symbol BTCUSDT --side SELL --type MARKET --quantity 0.001
```

## Workflows
<!-- Turn "one sentence from the user" into "which commands to run, in order". DELETE THIS WHOLE
     SECTION if this skill has no multi-step task.

  When to write one: a task needs ≥2 commands AND one step's output feeds the next. A scenario a
  single command handles belongs in Usage Examples.

  How to write one: a short block per scenario = heading + the user's own words + numbered steps
  (note each step's output; confirm before a write; re-query at the end). For example:

    ### Market buy, then confirm the fill
    > User: "buy BTC with 100 USDT at market"
    1. mexc spot marketv2 ticker --symbol BTCUSDT         → current price, to estimate size
    2. mexc spot trade place --symbol BTCUSDT --side BUY --type MARKET --quoteOrderQty 100
           ↓ step 2 is a write: show the user a summary and wait for confirmation first
    3. mexc spot trade get --symbol BTCUSDT --orderId <id from step 2>   → confirm the fill

  Parameters: include only what distinguishes the intent and makes the command runnable; use
  <...> for the rest and let the agent run `mexc schema` for the full set.

  One sub-domain or several: a playbook may use only this skill's commands, or span sub-domains
  (the example above uses market + trade). A cross-sub-domain playbook depends on the other
  skill existing, so it can be added later.

  Too many scenarios (>5): move the detailed playbooks into references/workflows-*.md and leave
  a one-line index here.
    ⚠️ Path convention (only when you split into references/): reference a sub-file as
       `{baseDir}/references/xxx.md`, where {baseDir} is the absolute directory this SKILL.md
       lives in, and add a note under the title:
         > Path convention: {baseDir} = the directory this SKILL.md lives in; read {baseDir}/...
         > with an absolute path, never a bare filename.
       Do NOT use a bare relative path (references/xxx.md or ./references/xxx.md) — the agent
       will fail to read it and start guessing. For example:
    - Open a position (market / limit / with TP-SL) → {baseDir}/references/workflows-open.md
    - Close a position (full / partial / reverse)   → {baseDir}/references/workflows-close.md
    Split further by business action as the file grows. -->

## Notes for Agents
<!-- Two blocks: (1) generic rules, (2) notes specific to this skill. Business facts belong in
     Important Notes. -->

**General rules (example set)**
- Retry is decided by idempotency alone: an idempotent operation (idempotent=yes) may be retried on a timeout or lost response; a non-idempotent one (idempotent=no) **must never be retried automatically**.
- Before a write, show the user a summary and **wait for explicit confirmation** ("just do it" is not confirmation).
- Unsure about parameters? Run `mexc schema <domain>.<sub>.<op>` instead of guessing.
- Credentials live in a shared skill, not here: on a structured credential error, load the `mexc-auth` skill — it owns the exact subtype/message/recovery. For `--read-only`, write confirmation and the error envelope, load the `mexc-common` skill (both apply to any sub-domain that writes or needs an identity). Prepare the credential, then replay the original command. (These are separate skills installed alongside the CLI; load them by name.)

**Notes specific to this skill**
<!-- Only agent-facing notes unique to this skill; delete this block if there are none. -->
- <placeholder: agent-facing notes unique to this skill (business facts go in Important Notes)>
