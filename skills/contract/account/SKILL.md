---
name: mexc-contract-account
scope: contract.account
signed: yes
identity: yes
visibility: public
description: >-
  MEXC contract private account — assets & snapshots, funds/transfer records,
  deal-history statistics, unified (multi-asset) margin mode & debt, fee rates &
  discounts, risk limit/level, KYC limits and leverage-mode preference. Requires
  an API key (every call is signed). Triggers: "contract account assets","funds
  records","transfer records","multi-asset margin mode",
  "repay debt","tiered fee rate","fee discount","risk limit","leverage mode".
---

# contract · account (private account data)

> Private account subdomain: every operation is signed and carries the API-key identity (`signed: yes`, `identity: yes`). A valid API key is required; the logged-in user is identified by the credential, never by a request parameter. On a credential/login error, load the `mexc-auth` skill to prepare the API key, then replay.

## Core Concepts

- **currency**: an asset currency, e.g. `USDT`, `BTC`, `MX`.
- **symbol**: a contract name, e.g. `BTC_USDT`.
- Timestamps in parameters and bodies are **Unix milliseconds** unless noted.
- **Response envelope**: responses follow the shared contract envelope (`success` / business `code` / `data`); business error codes are defined by the downstream service and passed through by the gateway unchanged.
- **Error codes**: see [`references/error-codes.md`](references/error-codes.md) for the business error codes (with English meanings) these operations may return.
- **Pagination differs by endpoint family**: `asset_book/v2` uses `currentPage` + `pageSize` (default 1 / 50, max 500); the unified-margin page queries use `pageIndex` + `pageSize` (default 1 / 20, max 200).
- **Unified (multi-asset) margin**: `getMultiAssetMode` reads the mode; `changeMultiAssetMode/{isMultiAssetMode}` toggles it; `getMultiAssets` reads USDT-valued assets under the mode; interest/debt records and pledge/borrow currency configs describe borrowing under it.
- **Debt repayment is a two-step flow**: `pre-pay-debt` previews and returns a `businessId`; `pay-debt/{businessId}` executes the repayment for that id.

### Enum semantics

- **change-leverage-mode `leverageMode`**: `1` advanced, `2` simple. **leverage-mode / getMultiAssetMode** responses: mode is `OPEN` / `NOT_OPEN` / `FUNCTION_NOT_ALLOWED` for multi-asset; leverage mode is advanced/simple as above.

### Return-field dictionary

Field meanings follow the endpoints' response bodies.

- **assets / asset**: `currency`, `displayCurrency`, `equity` (total), `availableBalance`, `cashBalance`, `frozenBalance`, `positionMargin`, `unrealized` (unrealized PnL), `availableCash`, `availableOpen`, `bonus` / `bonusExpireTime` / `recentExpireAmount` / `recentExpireTime` (experience bonus), `debtAmount`, `contributeMarginAmount`, `vcoinId`.
- **get-multi-assets**: per currency — `currency`, `equity`, `adjEquity` (adjusted equity), `im` / `imr` (initial margin / rate), `mmr` (maintenance margin rate), `availableBalance`.
- **asset-book-v2**: `id`, `currency` / `displayCurrency`, `amount`, `type` (record type), `direction` (in/out), `symbol`, `dateTime`, `title`, `remark`, `mark`, `isBonusDeduct` / `bonusDeductAmount`.
- **fee detail V2 (`tiered-fee-rate-v2`)**: taker/maker fee rates, fee-rate mode, tiered/leverage fee ladders, discount and deduction fields; `tiered-fee-rate-v2-next-level` returns the next tier after pass-through.
- **risk-limit / risk-level**: per-contract, per-`positionType` (1 long / 2 short) risk-limit tiers and the user's current level.

## Important Notes

- **API key required**: every operation is `signed: yes` / `identity: yes`. Missing/invalid credentials return a structured credential error — load `mexc-auth`.
- **Writes need confirmation**: `change-multi-asset-mode`, `change-leverage-mode`, `pre-pay-debt`, `pay-debt` are `write` — show a summary and confirm before running.
- **`pay-debt` is destructive and non-idempotent**: it moves funds to repay debt; never auto-retry it (a lost response could mean the repayment already landed). Always call `pre-pay-debt` first and pass its `businessId`.
- **User identity is not a parameter**: uid / account are taken from the API-key credential and gateway headers; do not put them in query or body.
- **Bodies**: `pre-pay-debt` and `change-leverage-mode` take a JSON body — supply it via `--data` (validated against the schema before sending; inspect with `mexc schema contract.account.<op>`).

## Operations

> scope is the resource identifier (always `contract.account`); the action is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe.

| Command                                                                | scope              | access | signed | identity | idempotent | Endpoint                                                                   | Description                       |
| ---------------------------------------------------------------------- | ------------------ | ------ | ------ | -------- | ---------- | -------------------------------------------------------------------------- | --------------------------------- |
| `mexc contract account assets`                                         | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/assets`                                       | All account assets                |
| `mexc contract account asset`                                          | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/asset/{currency}`                             | Asset of a currency               |
| `mexc contract account asset-book-v2`                                  | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/asset_book/v2`                                | Funds flow records V2 (paged)     |
| `mexc contract account asset-book-order-deal-fee-total`                | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/asset_book/order_deal_fee/total`              | Total trading fee (30d)           |
| `mexc contract account get-multi-asset-mode`                           | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/getMultiAssetMode`                        | Unified margin mode               |
| `mexc contract account change-multi-asset-mode`                        | `contract.account` | write  | yes    | yes      | yes        | `POST /api/v1/private/multiAssets/changeMultiAssetMode/{isMultiAssetMode}` | Switch unified margin mode        |
| `mexc contract account get-multi-assets`                               | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/getMultiAssets`                           | Unified-margin assets (USDT)      |
| `mexc contract account page-multi-assets-interest-record`              | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/pageMultiAssetsInterestRecord`            | Unified-margin interest records   |
| `mexc contract account page-multi-assets-debt-repayment-record`        | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/pageMultiAssetsDebtRepaymentRecord`       | Unified-margin repayment records  |
| `mexc contract account list-debt-repayment-record-detail-by-record-id` | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/listDebtRepaymentRecordDetailByRecordId`  | Repayment detail by record id     |
| `mexc contract account pre-pay-debt`                                   | `contract.account` | write  | yes    | yes      | yes        | `POST /api/v1/private/multiAssets/prePayDebt`                              | Preview a debt repayment          |
| `mexc contract account pay-debt`                                       | `contract.account` | write  | yes    | yes      | no         | `POST /api/v1/private/multiAssets/payDebt/{businessId}`                    | Repay debt for a business id      |
| `mexc contract account list-all-pledge-currency-config`                | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/listAllPledgeCurrencyConfig`              | Pledge currency configs           |
| `mexc contract account list-all-debt-currency-config`                  | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/multiAssets/listAllDebtCurrencyConfig`                | Borrowable currency configs       |
| `mexc contract account statistics-deal-amount`                         | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/deal_history/statistics_deal_amount`                  | Traded-amount statistics          |
| `mexc contract account tiered-fee-rate-v2`                             | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/tiered_fee_rate/v2`                           | Fee detail V2                     |
| `mexc contract account tiered-fee-rate-v2-next-level`                  | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/tiered_fee_rate/v2/next_level`                | Next-level fee rate               |
| `mexc contract account contract-fee-rate`                              | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/contract/fee_rate`                            | Fee detail of a contract          |
| `mexc contract account contract-fee-rate-range`                        | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/contract/fee_rate_range`                      | Contract fee-rate range           |
| `mexc contract account fee-deduct-configs`                             | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/feeDeductConfigs`                             | Fee-deduction configs             |
| `mexc contract account contract-fee-discount-config`                   | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/config/contractFeeDiscountConfig`             | Spot-holding fee-discount configs |
| `mexc contract account risk-limit`                                     | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/account/risk_limit`                                   | Risk limit                        |
| `mexc contract account risk-level`                                     | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/profile/risk_level`                                   | User risk level                   |
| `mexc contract account kyc-trade-limit`                                | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/profile/kyc/trade_limit`                              | KYC trade limit                   |
| `mexc contract account leverage-mode`                                  | `contract.account` | read   | yes    | yes      | yes        | `GET /api/v1/private/user_pref/leverage_mode`                              | Leverage mode                     |
| `mexc contract account change-leverage-mode`                           | `contract.account` | write  | yes    | yes      | yes        | `POST /api/v1/private/user_pref/change_leverage_mode`                      | Change leverage mode              |

## Permissions

> Authorization = resource(scope) × action(access).

| Operation                                                                | scope              | access |
| ------------------------------------------------------------------------ | ------------------ | ------ |
| all query operations (assets / records / fee / risk / config reads)      | `contract.account` | read   |
| change-multi-asset-mode / pre-pay-debt / pay-debt / change-leverage-mode | `contract.account` | write  |

## Usage Examples

```bash
# --- Assets & funds records ---
mexc contract account assets
mexc contract account asset --currency USDT --pointEnabled false
mexc contract account asset-book-v2 --currency USDT --startId 0 --currentPage 1 --pageSize 50
mexc contract account asset-book-order-deal-fee-total

# --- Unified (multi-asset) margin & debt ---
mexc contract account get-multi-asset-mode
mexc contract account change-multi-asset-mode --isMultiAssetMode true
mexc contract account get-multi-assets
mexc contract account page-multi-assets-interest-record --currency USDT --pageIndex 1 --pageSize 20
mexc contract account page-multi-assets-debt-repayment-record --pageIndex 1 --pageSize 20
mexc contract account list-debt-repayment-record-detail-by-record-id --debtRepaymentRecordId 123456
mexc contract account pre-pay-debt --data '{"currency":"USDT","payCurrency":"BTC"}'
mexc contract account pay-debt --businessId 123456
mexc contract account list-all-pledge-currency-config
mexc contract account list-all-debt-currency-config

# --- Deal history ---
mexc contract account statistics-deal-amount --startTime 1730000000000 --endTime 1732592000000 --symbol BTC_USDT

# --- Fee rate & discount ---
mexc contract account tiered-fee-rate-v2 --symbol BTC_USDT
mexc contract account tiered-fee-rate-v2-next-level --symbol BTC_USDT
mexc contract account contract-fee-rate --symbol BTC_USDT
mexc contract account contract-fee-rate-range
mexc contract account fee-deduct-configs
mexc contract account contract-fee-discount-config

# --- Risk, profile & preferences ---
mexc contract account risk-limit --symbol BTC_USDT
mexc contract account risk-level --contractId 10
mexc contract account kyc-trade-limit
mexc contract account leverage-mode
mexc contract account change-leverage-mode --data '{"leverageMode":1}'
```

## Workflows

### Repay unified-margin debt

> User: "I want to repay my USDT debt"

1. `mexc contract account list-all-pledge-currency-config` → the currencies eligible to be used as the repayment currency.
2. `mexc contract account assets` → per-currency balances; combine with step 1 to build the candidate lists: currencies with `debtAmount > 0` (liability candidates) and currencies with a positive available balance that are pledge-eligible (repayment candidates).
3. `mexc contract account pre-pay-debt --data '{"currency":"<liability currency>","payCurrency":"<repayment currency>"}'` → returns a `businessId`, the exchange price, the estimated pay/repay amounts, and a quote expiry (`effectiveTime`).
4. The quote expires: if it lapses before confirming, re-run step 3 to get a fresh `businessId` — do not execute step 5 with an expired quote.
   ↓ step 5 is a write: show the user a summary and wait for explicit confirmation before running.
5. `mexc contract account pay-debt --businessId <businessId from step 3>` → executes the repayment. Non-idempotent — never auto-retry on a timeout or lost response; re-query step 6 first to check whether it already landed.
6. `mexc contract account assets` → re-query to confirm the liability currency's debt decreased and the repayment currency's balance decreased.

### Tiered fee-rate penetration check

> User: "What's my fee rate for this symbol, and am I close to the next tier?"

1. `mexc contract account tiered-fee-rate-v2 --symbol BTC_USDT` → the current tiered fee table, the tier the account currently sits in, and whether next-tier penetration is enabled for this account (`useNextLevelConfig`).
2. Only continue to step 3 if **both** hold: penetration is enabled, and the current tier is **not** already the last one. Otherwise stop here — there is no next tier to check.
3. `mexc contract account tiered-fee-rate-v2-next-level --symbol BTC_USDT` → the next tier's fee detail. If its fee-rate mode is `NORMAL`, present it merged into the last row of the tiered table from step 1 rather than as a separate tier.

### MX fee-deduction eligibility check

> User: "Can I use MX to deduct my trading fees?"

1. `mexc contract account fee-deduct-configs` → a list of deduction configs, each with `deductCoin`, `settleCoin`, and `discountRatio`.
2. Find the entry where `deductCoin=MX` and `settleCoin=USDT`. Only continue to step 3 if such an entry exists — otherwise MX deduction is unavailable and there is no need to check the balance.
3. `mexc contract account asset --currency MX` → the MX available balance. If it is greater than 0, MX deduction can be activated, using the `discountRatio` found in step 2.

## Notes for Agents

**General rules**

- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op — `pay-debt` is `idempotent=no`.
- Show a summary and wait for explicit user confirmation before any write operation.
- Unsure about parameters or a body shape? run `mexc schema contract.account.<op>` instead of guessing.
- On a structured credential/login error, load the `mexc-auth` skill to prepare the API key; that skill owns the exact subtype/message/recovery. Common error/safety rules → load the `mexc-common` skill. Then replay; this skill does not describe login. (These are separate skills installed alongside the CLI — load by name.)

**Skill-specific notes**

- `pay-debt` requires a `businessId` from `pre-pay-debt`; treat the pair as one flow and never retry `pay-debt` blindly.
- Pagination parameter names differ by endpoint family (see Core Concepts) — do not assume a single scheme.
