---
name: mexc-spot-asset
scope: spot.asset
signed: yes
identity: yes
visibility: public
description: >-
  MEXC spot asset surface — account overview & balances, per-account asset detail
  (spot/futures/otc/strategy/robot/alpha/predict/stock/financial), deposit &
  withdrawal history, internal transfers and their history, spot/futures bill
  logs & summaries, quote prices (USDT/CNY/FX rate), and spot PnL analysis. Every
  operation needs an API key signature. Triggers: "asset overview","account
  balances","asset detail","deposit history","withdrawal history","transfer
  funds","transfer history","bill logs","coin prices","spot pnl".
---

> Auth: this subdomain needs an openapi API key and performs fund-moving writes (`transfer`, `sub-transfer`). On a structured credential error (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · asset

Asset surface: query converted balances and per-account asset detail, review deposit/withdraw and transfer history, move funds between your own accounts, read bill logs, look up quote prices, and inspect spot profit-and-loss. Authorization = resource(scope) × action(access).

## Core Concepts

- **account type**: the wallet an asset lives in — `SPOT`, `FUTURES`, `PREDICT` (and others surfaced as their own `assets-*` reads). Transfers move funds between two account types.
- **converted asset**: a balance detail already converted to a reference value (the `overview` and `assets-*` reads return converted views).
- **tranId**: an internal transfer serial number. It comes from two places — the result of a `transfer` write (`POST /api/v3/capital/transfer`), or a row of `transfer-history` — and is the key `transfer-detail` uses to look up a single transfer's status.
- **mainType**: a bill business category. Pass `ALL` to query every category at once; to filter by a specific category, take its enum value from the matching type list (`bill-spot-types` for spot bills, `bill-futures-types` for futures bills) rather than guessing.
- **time range**: start/end are Unix milliseconds. History windows are capped (see Important Notes).
- **deposit network vs walletType**: a coin can be deposited over several networks. `deposit-chains` returns one row per supported network, and each row exposes two distinct identifiers: `network` (the chain label shown to users, e.g. `TRC20` — the `chainName`) and `netWork` (the internal `walletType` / virtual-coin series). To query a deposit address for a specific network, take that row's `netWork` (walletType) and pass it to `deposit-address --netWork`. The two look almost identical but are **not** interchangeable — see the note in Important Notes.
- **deposit-chains → deposit-address relationship**: these two ops are a pair. `deposit-chains --coin <X>` first tells you which networks coin X supports and their `netWork` (walletType); passing that walletType as `deposit-address --coin <X> --netWork <walletType>` then returns the on-chain deposit address for exactly that network (the response is filtered to the matching walletType). You normally call `deposit-chains` before `deposit-address` so you know which walletType to request (see the "Get a deposit address" workflow).

## Capability Boundary (read before acting on a "send funds out" request)

This subdomain moves funds **only between accounts the API key already owns**. It does **not** provide any way to send funds out of the user's control. If a request is one of the following, stop and tell the user this surface cannot do it — do **not** substitute `transfer`/`sub-transfer`:

- **Withdraw to an external wallet / on-chain address** — not supported. `withdraw-history` is a read-only history of past withdrawals; there is no withdraw-execution op here. A request like "withdraw to my wallet" cannot be fulfilled.
- **Send to another user / a friend** — not supported. `transfer-internal-history` is a read-only history of user-to-user transfers; there is no user-to-user transfer-execution op. A request like "send to my friend" cannot be fulfilled.
- **What `transfer` / `sub-transfer` actually do**: `transfer` moves balance between the **same user's own** account types (e.g. SPOT ↔ FUTURES); `sub-transfer` moves balance between a master account and **its own** sub-accounts. Neither sends funds off-platform or to an unrelated user. Do not offer them as a stand-in for a withdrawal or a peer transfer — the user could believe funds left their control when they only moved between their own wallets.

## Important Notes

- **All signed**: every operation requires `X-MEXC-APIKEY` + a signature (`signed: yes`), including reads and the quote-price lookups.
- **Writes are fund transfers**: `transfer` and `sub-transfer` (`access: write`) move balance. They are graded as strong writes — destructive and non-idempotent — so they need explicit confirmation and must never be auto-retried.
- **Your own account is not a parameter**: you never pass a member/account id for your own account; it is resolved from the API key. `sub-account-asset` and the transfer commands take the counterpart account (`subAccount` / `fromAccount` / `toAccount`), not your own id.
- **Account-type enums**: `transfer` and its history accept `SPOT`/`FUTURES`/`PREDICT`; `sub-transfer` and its history accept `SPOT`/`FUTURES` only.
- **History caps**: deposit/withdraw `limit` defaults to 1000 (max 1000); deposit/withdraw ranges cap at the service window; `transfer-internal-history` window between start and end cannot exceed 7 days; `pnl-detail` range cannot exceed 180 days.
- **Deposit address lookup takes walletType, not the chain label**: `deposit-address` has two look-alike optional filters that differ **only by capitalization**. `--network` (lowercase w) filters by the chain label (`chainName`, e.g. `TRC20`); `--netWork` (capital W) filters by the internal `walletType` and is the value you take from a `deposit-chains` row. The canonical flow is `deposit-chains` → read a row's `netWork` → `deposit-address --coin <X> --netWork <that walletType>`, which returns only the address for that walletType (the result is filtered by walletType). Do not pass the chain label into `--netWork` or the walletType into `--network`. If unsure which value you hold, run `deposit-chains` first — its rows show both fields side by side. Both filters are optional: with neither, `deposit-address` returns every network's address for the coin.
- **Bill queries are POST reads**: `bill-*-logs` / `bill-*-summary` are reads whose filters travel as a JSON body on `--data`; `mainType`, `startTime`, `endTime` are required. Set `mainType` to `ALL` to query every category at once; to filter one category, take the enum from the matching types op (`bill-spot-types` for the spot bills, `bill-futures-types` for the futures bills) rather than guessing a value.
- **Transfer status lookup**: a successful `transfer` returns a `tranId`; pass it to `transfer-detail --tranId <id>` to check that transfer's status. A `tranId` can also be taken from a `transfer-history` row. A master/sub `sub-transfer` has **no** by-id detail op — confirm its record's status via `sub-transfer-history` instead.
- **Quote prices are signed reads**: `usdt-main-prices` / `usdt-all-prices` / `cny-prices` / `exchange-rate` still require an API key signature like every other op here.
- **"How much of coin X do I have" → `overview` `currencyAssets`**: this is the canonical balance answer. `mexc spot asset overview` returns `currencyAssets`, where each entry is one coin's real holdings aggregated across all subsystems (spot/futures/etc) — read the coin's amount there. Use `account` (`balances[coin].free`) only when you specifically want the **spot-only** balance.
- **Two-block payload — do not read a balance off the valuation block**: `overview` and every `assets-*` read (spot/futures/otc/strategy/robot/alpha/predict) return two blocks at completely different scales. **Per-coin real holdings** live in `currencyAssets` (overview) / `assets` (all `assets-*`) — `total` is that coin's amount. **Total net worth re-priced in each main coin** (BTC/ETH/MX/USDC/USDT) lives in `overviews` (overview) / `balances` (every `assets-*`) — each row is the *same account total* re-priced into that coin, **not that coin's balance**. Two traps: (1) `assets-*.balances` has the identical name to `account.balances` but the opposite meaning — `account.balances` is the **only** place `balances` means a per-coin balance; in every `assets-*` read `balances` is a valuation block; (2) reading a coin off the valuation block returns a number that can be many thousands of times too large. **Sanity check**: if a single-coin "balance" is orders of magnitude larger than the user expects (e.g. billions), you almost certainly read the valuation block — switch to `overview.currencyAssets`. For any real balance use `currencyAssets` / `assets` (or `account`); use `overviews` / `balances` only for "what is my whole account worth priced in coin Y". (`assets-stock` and `assets-financial` have neither block: stock holdings are in `cashDtoList`, financial holdings in `positions`.)

## Operations

> scope is always the resource `spot.asset`; the action (read/write) is in the access column. signed/identity flag whether a signature/login is required; idempotent flags whether a repeat is safe. (visibility is a subdomain-level fact in the frontmatter, not a per-op column.)

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot asset account`                   | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account`                                | Current account info; `balances[coin].free`/`locked` = that coin's **spot-only** real balance (per-coin), not converted |
| `mexc spot asset overview`                  | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/overview`                       | Converted asset overview. **For "how much of coin X do I have" use `currencyAssets`** — each entry is a coin's real holdings across all subsystems (spot/futures/etc). `overviews` = total net worth re-priced in each main coin, **NOT a per-coin balance** (do not read a coin's amount off it) |
| `mexc spot asset sub-account-asset`         | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/sub-account/asset`                      | Sub-account asset (`--subAccount` `--accountType`) |
| `mexc spot asset assets-spot`               | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/spot`                    | Spot asset detail. `assets` = per-coin holdings (`total` is that coin's amount). `balances` = total spot net worth re-priced in each main coin, **NOT a per-coin balance** — same name as `account.balances` but opposite meaning; never read a coin's amount off `assets-spot.balances` |
| `mexc spot asset assets-futures`            | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/futures`                 | Futures asset detail. `assets` = per-coin holdings. `balances` = total futures net worth re-priced in each main coin, **NOT a per-coin balance** (same trap as `assets-spot.balances`) |
| `mexc spot asset assets-otc`                | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/otc`                     | Fiat (OTC) asset detail. `assets` = per-coin holdings. `balances` = total OTC net worth re-priced in each main coin, **NOT a per-coin balance** |
| `mexc spot asset assets-strategy`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/strategy`                | Strategy-trading asset detail. `assets` = per-strategy holdings. `balances` = total strategy net worth re-priced in each main coin, **NOT a per-coin balance** |
| `mexc spot asset assets-robot`              | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/robot`                   | Trading-bot asset detail. `assets` = per-bot holdings. `balances` = total bot net worth re-priced in each main coin, **NOT a per-coin balance** |
| `mexc spot asset assets-alpha`              | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/alpha`                   | Alpha asset detail. `assets` = per-coin holdings. `balances` = total alpha net worth re-priced in each main coin, **NOT a per-coin balance** |
| `mexc spot asset assets-predict`            | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/predict`                 | Prediction-market asset detail. `assets` = per-coin holdings. `balances` = total predict net worth re-priced in each main coin, **NOT a per-coin balance** |
| `mexc spot asset assets-stock`              | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/stock`                   | Stock asset (`--currency` default USD). No `balances` block; holdings are in `cashDtoList` with `totalCash`/`totalAvailableCash` scalars |
| `mexc spot asset assets-financial`          | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/assets/financial`               | Financial (earn) asset detail. No `balances` block; per-coin holdings are in `positions` (`positionQuantity` = amount, `positionUsdtQuantity` = USDT value) |
| `mexc spot asset deposit-history`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/deposit/hisrec`                 | Deposit history |
| `mexc spot asset withdraw-history`          | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/withdraw/history`               | Withdrawal history |
| `mexc spot asset deposit-chains`            | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/deposit/chain/list`             | List a coin's supported deposit networks; each row's `netWork` (walletType) feeds `deposit-address --netWork` |
| `mexc spot asset deposit-address`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/deposit/address/v2`             | Query a coin's deposit addresses; filter by `--netWork` (walletType from `deposit-chains`) or `--network` (chain label) |
| `mexc spot asset transfer`                  | `spot.asset` | write | yes | yes | no  | `POST /api/v3/capital/transfer`                      | Transfer between own accounts; returns a `tranId` — look up that transfer's status with `transfer-detail --tranId <id>` |
| `mexc spot asset sub-transfer`              | `spot.asset` | write | yes | yes | no  | `POST /api/v3/capital/sub-account/universalTransfer` | Master/sub universal transfer; no by-id detail op — confirm the record's status via `sub-transfer-history` |
| `mexc spot asset transfer-history`          | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/transfer`                       | In-station transfer history; each row carries a `tranId` usable with `transfer-detail` |
| `mexc spot asset sub-transfer-history`      | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/sub-account/universalTransfer`  | Master/sub transfer history; use to confirm a `sub-transfer` record's status |
| `mexc spot asset transfer-detail`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/transfer/tranId`                | Single transfer status by `--tranId`; the id comes from a `transfer` result or a `transfer-history` row |
| `mexc spot asset transfer-internal-history` | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/capital/transfer/internal`              | User-to-user transfer history |
| `mexc spot asset bill-spot-types`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/bill/spot/types`                | Spot bill `mainType` enum values — use one to filter `bill-spot-logs`/`bill-spot-summary`, or pass `mainType=ALL` there to query all types |
| `mexc spot asset bill-spot-logs`            | `spot.asset` | read  | yes | yes | yes | `POST /api/v3/account/bill/spot/logs`                | Spot bill logs (filters in `--data`); `mainType=ALL` returns all types, otherwise use an enum from `bill-spot-types` |
| `mexc spot asset bill-spot-summary`         | `spot.asset` | read  | yes | yes | yes | `POST /api/v3/account/bill/spot/summary`             | Spot bill summary (filters in `--data`); `mainType=ALL` returns all types, otherwise use an enum from `bill-spot-types` |
| `mexc spot asset bill-futures-types`        | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/bill/futures/types`             | Futures bill `mainType` enum values — use one to filter `bill-futures-logs`/`bill-futures-summary`, or pass `mainType=ALL` there to query all types |
| `mexc spot asset bill-futures-logs`         | `spot.asset` | read  | yes | yes | yes | `POST /api/v3/account/bill/futures/logs`             | Futures bill logs (filters in `--data`); `mainType=ALL` returns all types, otherwise use an enum from `bill-futures-types` |
| `mexc spot asset bill-futures-summary`      | `spot.asset` | read  | yes | yes | yes | `POST /api/v3/account/bill/futures/summary`          | Futures bill summary (filters in `--data`); `mainType=ALL` returns all types, otherwise use an enum from `bill-futures-types` |
| `mexc spot asset usdt-main-prices`          | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/common/usdt/main/prices`                | USDT prices for main quote coins |
| `mexc spot asset usdt-all-prices`           | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/common/usdt/all/prices`                 | USDT prices for all coins |
| `mexc spot asset cny-prices`                | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/common/cny/prices`                      | CNY prices for all coins |
| `mexc spot asset exchange-rate`             | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/common/currency/exchange/rate`          | Fiat exchange rates |
| `mexc spot asset pnl-overview`              | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/pnl/spot/overview`              | Spot PnL overview |
| `mexc spot asset pnl-detail`                | `spot.asset` | read  | yes | yes | yes | `GET /api/v3/account/pnl/spot/detail`                | Spot PnL daily detail (`--from` `--to`) |

## Permissions

> Authorization = resource(scope) × action(access). scope is always `spot.asset`.

| Operation | scope | access |
|-----------|-------|--------|
| account / overview / sub-account-asset / assets-* / deposit-history / withdraw-history / deposit-chains / deposit-address / transfer-history / sub-transfer-history / transfer-detail / transfer-internal-history / bill-* / usdt-main-prices / usdt-all-prices / cny-prices / exchange-rate / pnl-overview / pnl-detail | `spot.asset` | read |
| transfer / sub-transfer | `spot.asset` | write |

## Usage Examples

```bash
# "How much of coin X do I have" — read the coin's entry in `currencyAssets`
# (real holdings across all subsystems). Do NOT read the amount off `overviews`,
# which is total net worth re-priced per main coin, not a per-coin balance.
mexc spot asset overview

# Spot-only per-coin holdings (read `assets`; `balances` is total spot net worth
# re-priced in each main coin, NOT a per-coin balance despite the name)
mexc spot asset assets-spot

# Deposit history for USDT on TRX in a time range
mexc spot asset deposit-history --coin USDT --network TRX --startTime 1719763200000 --endTime 1722355200000

# Get a deposit address: first list the coin's networks, then request the
# address for a chosen network's walletType (the `netWork` field from a chains row).
mexc spot asset deposit-chains --coin USDT
mexc spot asset deposit-address --coin USDT --netWork <walletType-from-a-deposit-chains-row>

# Transfer 100 USDT from spot to futures (a write — confirm before running).
# The response carries a tranId; use it to check the transfer's status:
mexc spot asset transfer --fromAccountType SPOT --toAccountType FUTURES --asset USDT --amount 100
mexc spot asset transfer-detail --tranId <tranId-from-the-transfer-result>

# Spot bill logs: filters travel as a JSON body on --data.
# mainType=ALL queries every category; to filter one category use an enum from
# bill-spot-types (e.g. SPOT_TRADING) — "TRADE" is not valid.
mexc spot asset bill-spot-logs --data '{"mainType":"ALL","startTime":1719763200000,"endTime":1722355200000,"pageNum":1,"pageSize":20}'
mexc spot asset bill-spot-logs --data '{"mainType":"SPOT_TRADING","startTime":1719763200000,"endTime":1722355200000,"pageNum":1,"pageSize":20}'

# Spot PnL detail for a range (<= 180 days)
mexc spot asset pnl-detail --from 1719763200000 --to 1722355200000
```

## Workflows

### Answer "how much of coin X do I have"
> User: "how much USDT do I have?"
1. `mexc spot asset overview` → in the returned `currencyAssets`, find the entry for the coin (e.g. USDT); its holdings amount is that coin's real balance aggregated across all subsystems (spot/futures/etc). **This is the field to read.**
2. Report that amount. Do **not** read the number off `overviews` (overview) or `balances` (assets-spot) — those blocks are the whole account's total net worth re-priced into each main coin, not a per-coin balance, and can be thousands of times larger.
- **Sanity check before answering**: if the number you are about to report is far larger than the user could plausibly hold (e.g. billions of USDT), you read the valuation block by mistake — go back to `currencyAssets`.
- Want only the **spot wallet** balance (not aggregated)? use `mexc spot asset account` → `balances[USDT].free` (+`locked`); do not use `assets-spot.balances`, which is a valuation block despite the name.

### Transfer funds and confirm they landed
> User: "move 100 USDT from spot to futures and check it went through"
1. `mexc spot asset assets-spot` → in the returned `assets`, check that USDT's `available` covers 100 (`balances` is total net worth priced in each main coin, not a per-coin amount — do not read balance off it)
2. `mexc spot asset transfer --fromAccountType SPOT --toAccountType FUTURES --asset USDT --amount 100` → the response carries a `tranId`
       ↓ step 2 is a write (`idempotent=no`, never auto-retry): show the user a summary and wait for explicit confirmation first
3. `mexc spot asset transfer-detail --tranId <tranId from step 2>` → confirm the transfer's `status` directly by id. (No `tranId` at hand — e.g. checking an older transfer? list `mexc spot asset transfer-history --fromAccountType SPOT --toAccountType FUTURES --asset USDT`, locate the record, and read its `tranId`/`status` from there.)

### Move funds between own and sub-account
> User: "transfer 50 USDT from spot to sub-account subA and confirm it landed"
1. `mexc spot asset sub-account-asset --subAccount subA --accountType SPOT` → per-coin holdings on the receiving sub-account, used to sanity-check the destination before writing (the op currently supports only `accountType=SPOT`)
2. `mexc spot asset sub-transfer --fromAccountType SPOT --toAccountType SPOT --toAccount subA --asset USDT --amount 50`
       ↓ step 2 is a write (`idempotent=no`, never auto-retry): show the user a summary and wait for explicit confirmation first; `--clientTranId` may be set as a server-side idempotency key (not a client-side retry mechanism)
3. `mexc spot asset sub-transfer-history --fromAccountType SPOT --toAccountType SPOT` → locate the record and confirm `status` (sub-transfers have no by-id detail op, so this list is the only way to check the record's status)

### Read spot bill: types first, then logs and summary together
> User: "show my spot trading fees for last month, both line items and net"
1. `mexc spot asset bill-spot-types` → pick the right `mainType` (spot trading is `SPOT_TRADING`; fees are subType `SPOT_TRADING_FEES`); the `mainType` in `--data` for steps 2 and 3 must come from this list. (To pull every category at once instead of filtering, pass `mainType=ALL` in steps 2 and 3 and skip this step.)
2. `mexc spot asset bill-spot-logs --data '{"mainType":"SPOT_TRADING","subTypeList":["SPOT_TRADING_FEES"],"startTime":<start>,"endTime":<end>,"pageNum":1,"pageSize":20}'` → paginated line items
3. `mexc spot asset bill-spot-summary --data '{"mainType":"SPOT_TRADING","subTypeList":["SPOT_TRADING_FEES"],"startTime":<start>,"endTime":<end>}'` → net-in / net-out / net totals for the same filter; steps 2 and 3 share the filter, answer different questions (rows vs totals), and are typically fetched together

### Read futures bill: types first, then logs and summary together
> User: "show my futures funding and PnL bills for last month, both entries and totals"
1. `mexc spot asset bill-futures-types` → valid `mainType` list. (To pull every category at once instead of filtering, pass `mainType=ALL` in steps 2 and 3 and skip this step.)
2. `mexc spot asset bill-futures-logs --data '{"mainType":"<mainType>","startTime":<start>,"endTime":<end>,"pageNum":1,"pageSize":20}'` → paginated entries
3. `mexc spot asset bill-futures-summary --data '{"mainType":"<mainType>","startTime":<start>,"endTime":<end>}'` → totals for the same filter (typically run alongside step 2)

### Spot PnL for a window: totals and daily breakdown
> User: "show my spot PnL for the last 90 days"
1. `mexc spot asset pnl-overview` → headline totals (cumulative PnL, average daily rate)
2. `mexc spot asset pnl-detail --from <t0> --to <t1>` → per-day PnL series over the same window (`--from` / `--to` are Unix ms; range ≤ 180 days — the API rejects longer)
- Both steps use the same `t0`–`t1` window and answer different questions (aggregate vs distribution); they are typically fetched in parallel, not one-after-the-other.

### Get a deposit address (chains first, then address)
> User: "give me my USDT deposit address on TRC20"
1. `mexc spot asset deposit-chains --coin USDT` → the list of networks USDT supports. Each row carries `network` (the chain label, e.g. `TRC20`) and `netWork` (the internal `walletType`). Find the row for the network the user wants and read **that row's `netWork`** (walletType).
2. `mexc spot asset deposit-address --coin USDT --netWork <walletType from step 1>` → the on-chain deposit address(es) for that network. When `--netWork` is set, the response is filtered to only the address whose walletType matches it, so you get exactly that network's address. If the coin uses a shared address, the response also carries a `memo` — the user must send it alongside the address.
- **Pass the walletType, not the chain label**: step 2's `--netWork` takes the `netWork`/walletType from step 1, not the `TRC20`-style label. `--network` (lowercase) is a separate chain-label filter; the two differ only by capitalization — do not swap them.
- Skipping step 1 is possible (`deposit-address --coin USDT` with no filter returns every network's address), but calling `deposit-chains` first is the reliable way to learn the exact walletType and confirm the network is open for deposits (`enableDeposit`).

### Convert holdings to a fiat display
> User: "value my spot holdings in EUR"
1. `mexc spot asset usdt-main-prices` → USDT-conversion prices for the main quote coins (BTC / ETH / USDC / USDT / MX)
2. `mexc spot asset exchange-rate` → USDT → target fiat (EUR / JPY / ...) rates
3. Compute client-side: `value = holdings × coin_USDT_price × USDT_to_fiat_rate`
- Neither op is useful on its own for a fiat display; the two are always combined and step 3 is composition, not a further call.

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op (idempotent=no) — this includes both transfers.
- Show a summary and wait for explicit user confirmation before any write operation (`transfer`, `sub-transfer`).
- Unsure about parameters? run `mexc schema spot.asset.<op>` instead of guessing — the POST bill queries carry their filter fields on `--data`, inspectable there.
- On a structured credential error (e.g. `requires API credentials`), load the `mexc-auth` skill (credentials) and the `mexc-common` skill (common error / safety / security rules), then replay the command; this skill does not describe login.

**Skill-specific notes**
- **Balance queries**: "how much of coin X do I have" → `overview` → read the coin's entry in `currencyAssets` (holdings across all subsystems). Never read a coin's amount off a valuation block (`overview.overviews`, or the `balances` block of **any** `assets-*` read) — those are the whole account's total net worth re-priced per main coin and can be thousands of times larger. The `balances` block appears in every `assets-*` read (spot/futures/otc/strategy/robot/alpha/predict) and is always a valuation block; `account.balances` is the **only** `balances` that is a per-coin balance. If a reported balance looks implausibly large (billions), you read the valuation block — switch to `currencyAssets`.
- Capability boundary (see the section above): this surface has **no** withdraw-to-external-wallet op and **no** user-to-user transfer op. If the user asks to "withdraw to my wallet" or "send to a friend", say it is unsupported here — never stand in with `transfer`/`sub-transfer`, which only move funds between the user's own accounts.
- **Deposit address**: to get a deposit address, run `deposit-chains --coin <X>` first, read a network row's `netWork` (walletType), then `deposit-address --coin <X> --netWork <that walletType>`. `deposit-address` has two look-alike filters differing only by case — `--netWork` (walletType, from a `deposit-chains` row) and `--network` (chain label like `TRC20`); pass the walletType into `--netWork`, never the chain label. Both are optional (no filter returns every network's address); check a chains row's `enableDeposit` before treating a network as usable, and surface any `memo` the address response returns.
- `sub-account-asset` currently supports only `accountType=SPOT`.
- `transfer` accepts `SPOT`/`FUTURES`/`PREDICT`; `sub-transfer` accepts `SPOT`/`FUTURES` only.
- **Transfer status**: `transfer` returns a `tranId` → check that transfer with `transfer-detail --tranId <id>`; the same `tranId` also appears on each `transfer-history` row. `sub-transfer` has no by-id lookup — confirm its status through `sub-transfer-history`.
- **Bill filtering**: pass `mainType=ALL` to `bill-*-logs`/`bill-*-summary` to query all categories; to filter one category, take its enum from `bill-spot-types` (spot) or `bill-futures-types` (futures) — never guess the value.
- History windows are capped: `transfer-internal-history` start→end ≤ 7 days; `pnl-detail` range ≤ 180 days; deposit/withdraw `limit` ≤ 1000.
- Never print the secret key in logs or output.
