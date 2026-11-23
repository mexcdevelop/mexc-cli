---
name: mexc-spot-market-search
scope: spot.market-search
signed: no
identity: no
visibility: public
description: Search spot-market coins, trading pairs, and related entities through the CLI gateway route declaration. Public, needs no saved profile. Use it when an agent needs to resolve a user query before choosing a market-data operation.
---

# spot · market-search

> The market-search subdomain reaches the market-support search endpoint through the isolated `/api/cli/spot/**` gateway namespace. Its operation is a public read: it needs no saved API credential profile and sends no API key or signature, so the search runs anonymously. Common error rules → load the `mexc-common` skill.

## Core Concepts

- **keyword**: the text to resolve into coins, symbols, or related market entities. Required, at most 100 characters.
- **currency** and **coinId**: optional filters that narrow the result. They drive the `finances` and `convertTokens` categories, which `keyword` does not.
- **language** and **X-Source**: optional HTTP headers; use `mexc schema` to inspect their locations before calling.

## Response Shape

`global-search` returns 12 result categories in one object:

`guides` · `notices` · `activities` · `contractCopies` · `finances` · `convertTokens` · `labels` · `coinLabel` · `labelRelatedProducts` · `stocks` · `tradfiSymbols` · `predicts`

Category presence is conditional, and an **absent** key is not the same as an empty one:

- `labelRelatedProducts` is computed only when `--labels true` **and** `--coinLabel true`; otherwise the key is absent.
- `predicts` needs a logged-in account, and this search is anonymous, so it is always empty here — `--predict true` does not change that. Do not read an empty `predicts` as "no prediction market matches".
- Categories that are personalised upstream (activities, contract copies, finances and the like) are the anonymous view, not the caller's own.
- `labels` / `coinLabel` are empty unless the matching flag is set.
- `finances` and `convertTokens` are driven by `--coinId` / `--currency`, not by `--keyword`, so they can be populated for an empty keyword and empty for a matching one.
- A category whose backing service failed is **omitted entirely** rather than returned empty — absent means unknown, not "none".

## Edge Cases

- A keyword longer than 100 characters is refused by the CLI before the request is sent. The upstream would otherwise return a success envelope with everything empty and no explanation.
- A no-match keyword and an empty keyword look identical at the envelope level: `code:0` with every category empty. There is no "nothing found" signal.

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---|---|---|---|---|---|---|---|
| `mexc spot market-search global-search` | `spot.market-search` | read | no | no | yes | `GET /api/cli/spot/market-support/global/search` | Global search across coins, trading pairs, and supported market entities |

## Permissions

| operation | scope | access |
|---|---|---|
| global-search | `spot.market-search` | read |

## Usage Example

```bash
mexc spot market-search global-search --keyword BTC --currency USDT --language en-US
```

## Notes for Agents

- This subdomain is public: no saved profile is needed, and a saved one is not used.
- Inspect the operation schema before calling when a filter or header is uncertain: `mexc schema spot.market-search.global-search`.
- The operation is read-only and idempotent.
