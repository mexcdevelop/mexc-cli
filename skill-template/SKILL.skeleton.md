---
name: mexc-<domain>-<sub>
scope: <domain>.<sub>
signed: no
identity: no
visibility: public
description: >-
  <domain> <sub> — placeholder: replace with what this subdomain does and when to
  use it. Triggers: "<English phrase 1>","<English phrase 2>". The >- block keeps
  it valid YAML so colons and "quotes" in your text never break parsing.
---

# <domain> · <sub>

<!-- one line: what this subdomain is for. Authorization = resource(scope) × action(access). -->

## Core Concepts

<!-- Core entities + ID / value formats, e.g. symbol looks like BTCUSDT. -->

## Resource Relationships

<!-- ASCII tree of entity relationships; delete this section if there is no clear hierarchy. -->

## Important Notes

<!-- Business FACTS only: defaults/limits, ambiguous params, expected failures, precision limits. -->

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc <domain> <sub> example` | `<domain>.<sub>` | read | no | no | yes | `GET /api/v3/example` | placeholder op, replace me |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| example | `<domain>.<sub>` | read |

## Usage Examples

```bash
# one runnable call per command, with real values
mexc <domain> <sub> example --symbol BTCUSDT
```

## Workflows

<!-- Multi-step playbooks (>=2 commands where step N feeds step N+1); delete this section if none. -->

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op (idempotent=no).
- Show a summary and wait for explicit user confirmation before any write operation.
- Unsure about parameters? run `mexc schema <domain>.<sub>.<op>` instead of guessing.
- On a structured credential/login error, load the `<auth-skill>` skill to prepare credentials/login; that skill owns the exact subtype/message/recovery. Common error/safety rules→ load the `mexc-common` skill. Then replay; this skill does not describe login. (These are separate skills installed alongside the CLI — load by name.)

**Skill-specific notes**
<!-- Only agent notes unique to this skill; delete this block if none. -->
