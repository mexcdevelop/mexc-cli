---
name: mexc-spot-market-search
scope: spot.market-search
signed: yes
identity: yes
visibility: public
description: Search spot-market coins, trading pairs, and related entities through the CLI gateway route declaration. Requires a saved API credential profile and signed CLI requests. Use it when an agent needs to resolve a user query before choosing a market-data operation.
---

# spot · market-search

> The market-search subdomain reaches the market-support search endpoint through the isolated `/api/cli/spot/**` gateway namespace. Its operation requires a saved API credential profile and signed CLI request. On a structured credential error, load the `mexc-auth` skill; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login.

## Core Concepts

- **keyword**: the text to resolve into coins, symbols, or related market entities. Required, at most 100 characters.
- **currency** and **coinId**: optional filters that narrow the result. They drive the `finances` and `convertTokens` categories, which `keyword` does not.
- **language** and **X-Source**: optional HTTP headers; use `mexc schema` to inspect their locations before calling.

## Response Shape

`global-search` returns 12 result categories in one object:

`guides` · `notices` · `activities` · `contractCopies` · `finances` · `convertTokens` · `labels` · `coinLabel` · `labelRelatedProducts` · `stocks` · `tradfiSymbols` · `predicts`

Category presence is conditional, and an **absent** key is not the same as an empty one:

- `labelRelatedProducts` is computed only when `--labels true` **and** `--coinLabel true`; otherwise the key is absent.
- `predicts` requires `--predict true` **and** a logged-in account; otherwise it is empty.
- `labels` / `coinLabel` are empty unless the matching flag is set.
- `finances` and `convertTokens` are driven by `--coinId` / `--currency`, not by `--keyword`, so they can be populated for an empty keyword and empty for a matching one.
- A category whose backing service failed is **omitted entirely** rather than returned empty — absent means unknown, not "none".

## Edge Cases

- A keyword longer than 100 characters is refused by the CLI before the request is sent. The upstream would otherwise return a success envelope with everything empty and no explanation.
- A no-match keyword and an empty keyword look identical at the envelope level: `code:0` with every category empty. There is no "nothing found" signal.

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---|---|---|---|---|---|---|---|
| `mexc spot market-search global-search` | `spot.market-search` | read | yes | yes | yes | `GET /api/cli/spot/market-support/global/search` | Global search across coins, trading pairs, and supported market entities |

## Permissions

| operation | scope | access |
|---|---|---|
| global-search | `spot.market-search` | read |

## Usage Example

```bash
mexc spot market-search global-search --keyword BTC --currency USDT --language en-US
```

## Notes for Agents

- This subdomain requires a saved API credential profile. Configure and select a profile before a real call; the framework then applies the required authentication and signing for the gateway route.
- Inspect the operation schema before calling when a filter or header is uncertain: `mexc schema spot.market-search.global-search`.
- The operation is read-only and idempotent.
