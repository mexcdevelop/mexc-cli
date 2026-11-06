# Subdomain Registry

> The directory main axis is `skills/<domain>/<sub>/`, **one level of sub-domain, no common/biz split**. To add a sub-domain, **register it here first, then create the directory** — this prevents naming drift.

## Rules

- Keep a domain's sub-domain count to **3–6**. More than that usually means the domain was cut wrong and should be split.
- Sub-domain names: singular, lowercase, hyphenate multi-word names (`open-order`), no domain prefix.
- Every sub-domain must appear in this registry.

## Classification Axes

The directory expresses only "domain/sub-domain". Authorization uses the **resource(scope) × action(access)** combination (AWS-IAM style). The five dimensions are fully orthogonal:

| Dimension | Form | Values | Purpose |
|------|------|------|------|
| `scope` | frontmatter, **resource identifier only** | `<domain>.<sub>` (e.g. `spot.trade`) | Marks "which resource", no action |
| `access` | operation-level tag (table column) | `read` / `write` / (future `admin` / `cancel-only` / `history-only`) | The action dimension, an **open enum**, leaving room to extend |
| `signed` | frontmatter tag (sub-domain **default**) | `yes` / `no` | `yes` iff the sub-domain's default signing scheme (`const Signed` in its api package) is not `none`. The scheme is a **closed enum**: this product's schemes are declared in `signing/` (`signing.CliHMAC`), and an operation may override the default with one of those constants (omit = inherit) |
| `identity` | frontmatter tag (sub-domain **default**) | `yes` / `no` | `yes` iff the sub-domain carries a login identity (`const Identity` in its api package is not `none`). Which identity is a **closed enum**: this product's identities are declared in `identity/` (`identity.OpenAPI`), carried by the const (never exposed as an algorithm) |
| `visibility` | frontmatter tag (**sub-domain-level**) | `public` | Every sub-domain in this repository is public; `check-boundary` Invariant 8 rejects anything else. The tag is kept because it is the axis sub-domain registries share. It is a sub-domain fact, so an operation has no visibility of its own |

- **Whether an operation writes is read from the `access` column** (`access != 'read'`), not parsed from the scope string, and not stored as a separate `write` boolean.
- **The action is an open enum**: today read/write, tomorrow it may grow `admin`/`cancel-only`/`history-only`, etc. — the structure stays the same, only enum values are added. **Signing schemes and identities are the opposite — closed enums**: a value exists only where it is declared, and business code references that constant, never a string literal (CI Invariant 6). Three files declare them all, and `scripts/check-boundary.py` reads exactly those as its source of truth (`ENUM_SOURCES`):

  | Declared in | Import path | Holds |
  |---|---|---|
  | `pkg/api/operation.go` | `github.com/mexcdevelop/mexc-cli/pkg/api` | the param locations (`api.In*`) and the `none` value of each auth axis (`api.SignNone`, `api.IdentityNone`) |
  | `signing/signing.go` | `github.com/mexcdevelop/mexc-cli/signing` | this product's signing schemes — `signing.CliHMAC` |
  | `identity/identity.go` | `github.com/mexcdevelop/mexc-cli/identity` | this product's identities — `identity.OpenAPI` |

  `signing/` and `identity/` are at the **repository root**, not under `pkg/`. The shared core deliberately does not catalogue a product's concrete values — see the comment above `Identity` in `pkg/api/operation.go`. There is no `api.SignCliHMAC` or `api.IdentityOpenAPI`; naming one does not compile.
- Every sub-domain in **this** repository is `visibility: public`; the tag stays in the frontmatter so every sub-domain states it explicitly, and Invariant 8 rejects any other value. Nothing is pruned at build time — a product mounts a sub-domain by listing it in its `domains.go`.

## Relationship Between scope/access and Underlying API Permissions

The skill layer's `(scope, access)` is **not bound directly to the exchange's permission enum** — it is mexc's own authorization naming system. The base layer maintains a mapping that translates `(scope, access)` into the underlying API-key permissions needed to call the real endpoints (e.g. a write operation on some resource requires the corresponding exchange permission). This lets the skill layer's classification axes evolve independently, without being held hostage by the exchange's permission naming.

**`Operation.Permission` is not usable in this product.** The field exists in the shared `pkg/api` because the taxonomy is shared with a product whose identity does carry grants. Here it is inert: `Permission` names an RBAC bucket plus the codes an operation needs, and checking them requires credentials that carry a grant map (`credential.GrantCarrier`). This product's only identity — `identity.OpenAPI`, an api key — carries none, because an api key's permissions live on the exchange side and cannot be read from the credential.

Two things stop it being written: `check-boundary` Invariant 14 rejects the declaration in CI, and `validateRegistry` refuses it at assembly. Leave `Permission` unset; no sub-domain here sets it.

## Registry

### spot

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `marketv2` | Spot CLI gateway market data V2 (grouped symbols / market tickers / coin info / concept plates / depth / deals) | `spot.marketv2` | read | public | yes | yes |
| `kline`    | Spot CLI public kline data (candlestick series / earliest queryable window) | `spot.kline` | read | public | no | no |
| `market-search` | Spot CLI gateway market support search | `spot.market-search` | read | public | yes | yes |
| `account`  | Spot account's own new-trade stream (WS) | `spot.account`  | read | public | yes | yes |
| `trade`     | Spot trading (place incl. TP/SL, trigger, trailing & OCO order types / modify / cancel / query / fills) | `spot.trade`     | read/write | public | yes | yes |
| `dca`       | Spot DCA / auto-invest (eligible pairs, caller's strategies & plans) | `spot.dca` | read/write | public | yes | yes |
| `convert`   | Spot flash convert (currencies / limits / quote / execute / limit convert / cancel limit convert / order query) | `spot.convert` | read/write | public | yes | yes |
| `grid`      | Spot grid trading (AI params, create / modify / stop grids, profit withdrawal) | `spot.grid` | read/write | public | yes | yes |
| `fee`       | Spot fee rate center (caller's fees by pair / channel / overall, zero-fee pairs) | `spot.fee` | read | public | yes | yes |

> `dca` / `convert` / `grid` / `fee` carry an API key (`identity: yes`) and sign each request client-side (`signed: yes`) with the profile secret; the gateway verifies the signature. Public market data is the exception: `kline` has no identity or signing default, and selected `marketv2` operations override the subdomain defaults to no identity and no signing. `convert`'s `quantity-limit` used to be declared `identity: none` on the reasoning that its backend reads no member and was corrected because it is not a public market-data operation.
| `trade`     | Spot trading (place / cancel / query / fills)       | `spot.trade`     | read/write | public | yes | yes |
| `asset`     | Spot asset surface (overview / balances / deposit-withdraw & transfer history / transfers / bills / prices / pnl) | `spot.asset` | read/write | public | yes | yes |

> Planned (not yet implemented — not in `domains()`): `transfer` (`spot.transfer`, read/write, signed). Register it in the table above when its `skills/spot/transfer/` is added.

### futures

> Futures (contract) capabilities: today, WebSocket-only public market data and the account's own private push. Futures REST (its signature rule is entirely different from spot's) is a separate, larger topic and out of scope for now — neither subdomain below carries a REST operation.

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `market`  | Futures public market data via WS (ticker / depth / trades) | `futures.market`  | read | public | no | no |
| `account` | Futures account's own new-trade push via WS | `futures.account` | read | public | no | yes |

### financial-openapi

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `common` | Common financial operations via openapi gateway (query products, member assets, positions, orders, currencies across flexible/fixed/contract-hold) | `financial-openapi.common` | read | public | yes | yes |
| `contract-hold` | Contract-hold financial operations via openapi gateway (query products, member assets, positions, profit records, register config, profit calculator) | `financial-openapi.contract-hold` | read/write | public | yes | yes |
| `fixed` | Fixed (term) financial operations via openapi gateway (query products, orders, profit records, positions) | `financial-openapi.fixed` | read | public | yes | yes |
| `flexible` | Flexible (demand) financial operations via openapi gateway (auto-pledge management, query products, orders, profit records, positions) | `financial-openapi.flexible` | read/write | public | yes | yes |


### activity

> Activity capabilities: what an end user can discover, claim and join.

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `online` | Online activity discovery (list / banner list / category tabs) | `activity.online` | read | public | yes | yes |
| `reward` | Unified reward center (summary / reward detail / coupon list) | `activity.reward` | read | public | yes | yes |
| `participate` | Activity registration | `activity.participate` | write | public | yes | yes |

### contract

> Contract capabilities: public market data.

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `market` | Contract public market data (detail / price / ticker / depth / kline / funding / risk / rankings) | `contract.market` | read | public | no | no |
| `account` | Contract private account (assets / records / export / multi-asset margin / fee / risk / preferences) | `contract.account` | read/write | public | yes | yes |
| `trade` | Contract trading (orders / plan / stop / track / chase-limit / leverage / margin / positions / close / reverse) | `contract.trade` | read/write | public | yes | yes |

<!--
When adding a new domain, copy this block:

### <domain-name>

| subdomain | meaning | scope | access | visibility | signed | identity |
|------|------|-------|--------|------------|:------:|:--------:|
| `xxx` | ... | `<domain>.xxx` | read / write / … | public / internal | yes / no | yes / no |
-->
