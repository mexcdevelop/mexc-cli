---
name: mexc-spot-grid
scope: spot.grid
signed: yes
identity: yes
visibility: public
description: 'MEXC spot grid trading (automated buy-low / sell-high within a price range). Query AI-suggested grid params, create/modify/stop grid strategies, inspect a grid''s orders / arbitrage / profit, and withdraw grid profit. Carries an API key; each request is signed. Triggers: "grid trading","create a grid strategy","AI grid params","my grid strategies","stop a grid","grid profit / withdraw grid profit","grid orders".'
---

> Auth: this subdomain carries an openapi API key (`identity: yes`) and the CLI signs each request (`signed: yes`) with the profile secret. A saved profile (API key + secret) is required. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · grid (grid trading)

> Signed spot subdomain: every operation carries the openapi identity (`identity: yes`, your API key and secret) and the CLI signs each request (`signed: yes`) with the profile secret. A saved profile is required. POST operations send a JSON body via `--data`; GET/DELETE use flags. scope marks only the resource (`spot.grid`); the action lives in the access column.

## Core Concepts

- **grid strategy**: an automated bot that places staggered buy/sell orders
  across a price range `[lowerPrice, upperPrice]`, profiting from oscillation.
- **gridUniqId**: the unique id of one grid strategy, returned by `list`. Every
  per-grid operation (detail / orders / modify / stop / withdraw) takes it.
- **symbolId / marketCurrencyId + currencyId**: the trading pair. `config` /
  `ai-params` take a `symbolId`; `place` takes the base (`currencyId`) and quote
  (`marketCurrencyId`) asset ids.
- **strategySource**: `1` = you supply the price range / grid count / amount
  manually; `2` = use AI-suggested params (then `aiParamPeriod` is required —
  fetch candidates with `ai-params`).
- **grid business state**: `0` creating · `1` pending start · `2` running ·
  `3` finished · `4` stopping. Operations are state-sensitive: `immediate-start`
  applies to a **pending** grid; `stop` applies to a **running** grid.
- **autoSell**: on stop, whether to sell the base asset back to quote (`1`) or
  keep it (`0`).
- **withdrawable profit**: realized grid profit you can pull to the main account
  (`asset-info` shows the amount; `withdraw-apply` moves it).

## Important Notes

- **API key + secret required**: every operation needs a saved
  profile (`mexc profile set <name>` in a terminal (it prompts for the key/secret, no flags),
  then `mexc profile use <name>`). The framework injects the API key and signs
  each request with the secret; you never pass a signature or memberId as a flag.
- **POST body via `--data`**: POST ops take a JSON body through one `--data`
  flag (inspect fields with `mexc schema`); GET and the DELETE `stop` use
  flags. All body/flag values are strings.
- **State matters**: check a grid's state (`detail`) before acting —
  `immediate-start` only works on a pending grid, `stop` only on a running one.

## Operations

> scope is the resource identifier (always `spot.grid`); the action is in the access column. signed/identity flag whether a signature/API key is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot grid config` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/config` | Grid config for a pair (`--symbolId` required) |
| `mexc spot grid ai-params` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/ai/params` | AI-suggested grid params per period (`--symbolId` required) |
| `mexc spot grid list` | `spot.grid` | read | yes | yes | yes | `POST /api/cli/spot/grid/strategy/queryRecord` | List the caller's grid strategies. JSON body via `--data`, all optional: `{"stateList","startDate","endDate","page","pageSize"}`. `stateList` filters by grid state (`0` init / `1` pending / `2` running / `3` finished / `4` stopping); omit it for active grids only (0,1,2), pass e.g. `["2","3"]` to include finished/stopped ones |
| `mexc spot grid detail` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/detail` | One grid's detail + stats (`--gridUniqId` required) |
| `mexc spot grid delegate-orders` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/queryDelegateOrder` | A grid's open orders (`--gridUniqId` required) |
| `mexc spot grid arbitrage-records` | `spot.grid` | read | yes | yes | yes | `POST /api/cli/spot/grid/strategy/queryArbitrageRecord` | A grid's arbitrage records. JSON body via `--data`: `{"gridUniqId"(required),"page","pageSize"}` |
| `mexc spot grid stop-reason` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/getStopReason` | Why a grid stopped (`--gridUniqId` required) |
| `mexc spot grid member-switch` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/memberGridSwitch` | Whether the caller may use grid trading |
| `mexc spot grid asset-info` | `spot.grid` | read | yes | yes | yes | `GET /api/cli/spot/grid/strategy/withdraw/assetInfo` | A grid's assets / withdrawable profit (`--gridUniqId` required) |
| `mexc spot grid withdraw-records` | `spot.grid` | read | yes | yes | yes | `POST /api/cli/spot/grid/strategy/withdraw/queryRecord` | Grid profit-withdrawal records. JSON body via `--data`, all optional: `{"gridUniqId","state","page","pageSize"}` |
| `mexc spot grid place` | `spot.grid` | write | yes | yes | no | `POST /api/cli/spot/grid/strategy/place` | Create a grid. JSON body via `--data`; required: `{"marketCurrencyId","currencyId","lowerPrice","upperPrice","gridQty","investAmount"}`; optional: `gridType,gridMode,startPrice,lossPrice,profitPrice,autoSell,strategySource,aiParamPeriod` |
| `mexc spot grid modify-auto-sell` | `spot.grid` | write | yes | yes | yes | `POST /api/cli/spot/grid/strategy/modify/autoSell` | Change auto-sell-on-stop. JSON body via `--data`, both required: `{"gridUniqId","autoSell":"0\|1"}` |
| `mexc spot grid modify-trigger-price` | `spot.grid` | write | yes | yes | yes | `POST /api/cli/spot/grid/strategy/modify/triggerPrice` | Change TP/SL prices. JSON body via `--data`: `{"gridUniqId"(required),"profitPrice","lossPrice","triggerVersion"}` |
| `mexc spot grid immediate-start` | `spot.grid` | write | yes | yes | no | `POST /api/cli/spot/grid/strategy/modify/immediateStart` | Start a pending grid now. JSON body via `--data`, required: `{"gridUniqId"}` |
| `mexc spot grid stop` | `spot.grid` | write | yes | yes | yes | `DELETE /api/cli/spot/grid/strategy/stop` | Stop a running grid (`--gridUniqId` required, `--autoSell 0\|1`) |
| `mexc spot grid withdraw-apply` | `spot.grid` | write | yes | yes | no | `POST /api/cli/spot/grid/strategy/withdraw/apply` | Withdraw grid profit to main account. JSON body via `--data`, both required: `{"gridUniqId","withdrawAmount"}` |

## Permissions

> Authorization = resource(scope) × action(access).

| Operation | scope | access |
|-----------|-------|--------|
| config / ai-params / list / detail / delegate-orders / arbitrage-records / stop-reason / member-switch / asset-info / withdraw-records | `spot.grid` | read |
| place / modify-auto-sell / modify-trigger-price / immediate-start / stop / withdraw-apply | `spot.grid` | write |

## Usage Examples

```bash
# Discover + create
mexc spot grid member-switch
mexc spot grid ai-params --symbolId <symbolId>
mexc spot grid place --data '{"marketCurrencyId":"<quoteId>","currencyId":"<baseId>","lowerPrice":"2500","upperPrice":"3500","gridQty":"20","investAmount":"500"}'

# Inspect
mexc spot grid list --data '{"page":"1","pageSize":"20"}'
mexc spot grid detail --gridUniqId <id>
mexc spot grid delegate-orders --gridUniqId <id>

# Manage
mexc spot grid modify-trigger-price --data '{"gridUniqId":"<id>","profitPrice":"3600","lossPrice":"2400"}'
mexc spot grid asset-info --gridUniqId <id>
mexc spot grid withdraw-apply --data '{"gridUniqId":"<id>","withdrawAmount":"12.5"}'
mexc spot grid stop --gridUniqId <id> --autoSell 1
```

## Workflows

Multi-step grid playbooks. A strategy is addressed by its `gridUniqId`; a trading
pair by its `symbolId`.

**Three hard constraints that apply to every playbook below:**

- **`list` defaults to active strategies only.** With no `stateList`, the body defaults server-side to the active states (`0` init / `1` pending / `2` running), so a strategy drops off the list the moment it enters the stop flow — "not in the default `list`" means neither "stopped" nor "does not exist". To see stopped/finished ones, pass `stateList` with the wanted codes (e.g. `{"stateList":["3","4"]}` for finished + stopping, or `["0","1","2","3","4"]` for everything). Even so, to read one specific strategy's current state prefer `detail` with its `gridUniqId`, and it is good practice to **capture the `gridUniqId` when you create or act on a strategy** rather than relying on re-listing.
- **`withdrawAmount` means two different things.** In a `detail` response it is the **cumulative amount already withdrawn**; in a `withdraw-apply` request body it is the **amount to withdraw now**. Never carry the former into the latter. `asset-info` names the two separately, so prefer it when reading the withdrawable amount.
- **On a trigger-price change, whichever price you omit is cleared.** See the third playbook.

### Create a grid on a trading pair

> User: "Run a grid strategy on this pair for me"

1. `mexc spot grid member-switch` → whether the caller is allowed to use grid trading. If not, stop here.
   It already accounts for every condition that gates grid trading — do not try to infer it yourself.
2. `mexc spot marketv2 symbols` → the pair catalogue; **use it to turn what the user said ("BTC/USDT") into the identifiers the later steps need**.
   The catalogue is grouped by **quote asset**; within a group `vn` is the **trading code** (e.g. `BTC`) and `fn` is the **full coin name** (e.g. `Bitcoin`) —
   ⚠️ **match on `vn`, not `fn`**; and the same `vn` recurs under different quote-asset groups, so
   **locate by quote-asset group plus `vn` together** — matching on `vn` alone yields several candidates.
   From the match take `id` (the pair id used below), `cd` / `mcd` (base and quote asset ids, needed to create the strategy),
   and `ps` / `qs` (price and quantity scale, needed in step 6 to check the per-grid price step).
3. `mexc spot grid config --symbolId <id from step 2>` → the pair's grid rules: the allowed grid-count range
   (`minGridQty` / `maxGridQty`), whether grid trading is open on the pair (`state`), and whether AI params are available
   (`aiParamState`). **Do this before choosing any number**: a manual grid count outside the range is rejected at creation.
4. `mexc spot grid ai-params --symbolId <pair id>` → AI-suggested price range, grid count and estimated profit, returned per period
   (short / mid / long). **When the previous step reports AI params unavailable, an empty result here is the normal outcome** — it means the pair has no AI params and the strategy parameters must be supplied manually; it is not a failure.
5. `mexc spot marketv2 symbol-trade --symbolId <pair id>`, `mexc spot marketv2 ticker --symbolId <pair id>`
   and `mexc spot fee symbol-strategy-default --symbolId <pair id>`
   → read the pair's **minimum limit-order amount** `li`, its **last traded price** `c`, and the **strategy fee basis** `takerFeeRate`.
   Together with the price bounds, the grid count and the scales, these are the inputs the minimum invest amount is derived from; the last price moves, so read it close to the moment of creation rather than reusing an older value.
   `symbol-trade` and `ticker` accept either the pair name or `symbolId` and return the same payload, but **a name is ambiguous across quote-asset groups, so pass `symbolId`**.
   ⚠️ Note which fee this is: `symbol-strategy-default` is the **strategy** rate that grid creation is priced against. It is not interchangeable with `rule-symbol-default`, nor with the `tfr` / `mfr` carried by `symbol-trade`.
   ⚠️ **The minimum invest amount is still enforced server-side — do not promise the user a figure you computed yourself.** Read these inputs to sanity-check the order of magnitude, then let the server decide. The invest amount must also stay within the available balance. Falling below the floor or exceeding the balance is rejected at creation.
6. Create the strategy, choosing one of two routes. Supply the pair through `cd` / `mcd` from step 2:
   - Manual route →
     `mexc spot grid place --data '{"marketCurrencyId":"<mcd>","currencyId":"<cd>","lowerPrice":"<lower bound>","upperPrice":"<upper bound>","gridQty":"<grid count>","investAmount":"<invest amount>"}'`
     → returns the new strategy's `gridUniqId`
   - AI route →
     `mexc spot grid place --data '{"marketCurrencyId":"<mcd>","currencyId":"<cd>","lowerPrice":"<AI lower bound>","upperPrice":"<AI upper bound>","gridQty":"<AI grid count>","investAmount":"<invest amount>","strategySource":"2","aiParamPeriod":"<101 short / 201 mid / 301 long>"}'`
     → returns the new strategy's `gridUniqId`

   `lowerPrice` / `upperPrice` / `gridQty` / `investAmount` are **always required** —
   ⚠️ **the AI route cannot supply the period alone**; carry the suggested values of the period chosen in step 4 into those four fields.
   On the manual route also check: upper bound above lower bound; `gridQty` a positive integer inside the range from step 3;
   and **the per-grid price step must not round to 0 under the price scale from step 2** (a narrow range combined with a high grid count triggers this, independently of the grid-count range).
   Supply the remaining optional fields (`gridType` / `gridMode` / `startPrice` / `profitPrice` / `lossPrice` / `autoSell`) as needed.
       ↓ step 6 is a write that commits funds: show the user a summary of price range, grid count and invest amount, and wait for explicit confirmation before running.
7. **Take `gridUniqId` from the `place` response** → the response carries the new strategy's id, so **there is no need to call `list` to find it**.
   Keep it: once the strategy stops it no longer appears in `list` and can only be reached through this id.
8. `mexc spot grid detail --gridUniqId <id from step 7>` → confirm the strategy runs with the intended parameters.
   After creation the state advances through creating → pending start / running, so an intermediate state on the first read is expected; re-read shortly after if needed.

### Inspect how a grid is doing

> User: "Show me how my grid is doing"

1. `mexc spot grid list --data '{"page":"1","pageSize":"20"}'` → list the running strategies and take the target's `gridUniqId`.
   Only active states are returned; a stopped strategy is absent, so go straight to step 2 with a `gridUniqId` you already hold.
2. `mexc spot grid detail --gridUniqId <id>` and `mexc spot grid delegate-orders --gridUniqId <id>`
   → **read these two as a pair** for the full picture: `detail` gives parameters, state and profit totals;
   `delegate-orders` gives the currently open buy-side and sell-side orders plus the per-grid quantity.
   While the strategy is not stopped both keep changing, so repeat them only while following along; once stopped there is nothing to poll.
3. `mexc spot grid arbitrage-records --data '{"gridUniqId":"<id>","page":"1","pageSize":"20"}'`
   → completed arbitrage (matched buy-sell) records, paginated; alongside the page the response also carries arbitrage profit and arbitrage count totals.
4. Once the strategy is stopped, `mexc spot grid stop-reason --gridUniqId <id>` → the stop reason.
   ⚠️ **While the strategy is still running the stop reason read back is a placeholder and reflects nothing real**; it is only meaningful after the strategy has stopped.
   `detail` returns the same field and is trustworthy only after the stop as well.

> **Three different profit bases — do not mix them**: arbitrage profit is realized profit from matched grids and **is also the base for the withdrawable amount**; position value is "quote balance + base quantity x last price"; invest amount is the principal. They mean different things — do not add them together or substitute one for another.

### Adjust a running grid

> User: "Change the take-profit price on my grid"

1. `mexc spot grid list --data '{"page":"1","pageSize":"20"}'` → take the target strategy's `gridUniqId`.
2. `mexc spot grid detail --gridUniqId <id>` → read the **current state** and the **current take-profit and stop-loss prices**.
   The state decides which action applies: an immediate start only makes sense for a strategy pending start, while trigger-price and auto-sell changes target a running one.
   Do not skip this step — step 3 needs both prices read here.
3. Choose one based on the intent:
   - Change the take-profit / stop-loss prices →
     `mexc spot grid modify-trigger-price --data '{"gridUniqId":"<id>","profitPrice":"<take-profit>","lossPrice":"<stop-loss>","triggerVersion":"<current epoch millis>"}'`
     → the new prices are visible in the `detail` of the next step
     ⚠️ **Two rules that cause real damage if missed**:
     (a) **Whichever price you omit is cleared.** To change only the take-profit price you must still **pass the stop-loss price read in step 2 at its current value**,
     otherwise the stop-loss the user had set is silently removed; the reverse holds too. So this command **always carries both prices**.
     (b) **`triggerVersion` must be sent**, set to the current epoch milliseconds, and it orders concurrent modifications.
     Omitting it is rejected server-side as a missing required parameter (even though the field is not marked required at the interface layer);
     the value is **not returned by any query**, so generate it yourself.
     The take-profit price must also sit above the highest of (last price, upper bound, trigger start price), and the stop-loss price below the lowest of the three.
   - Change whether the base asset is sold back to quote on stop →
     `mexc spot grid modify-auto-sell --data '{"gridUniqId":"<id>","autoSell":"<0 or 1>"}'`
     → the new value is visible in the `detail` of the next step
   - Start a strategy that has not triggered yet →
     `mexc spot grid immediate-start --data '{"gridUniqId":"<id>"}'`
     → moves the strategy from pending start into running; **calling it on an already running strategy is rejected**, which is why step 2 reads the state first
       ↓ all three are writes: state which setting changes and from what to what, and wait for explicit confirmation before running.
4. `mexc spot grid detail --gridUniqId <id>` → re-query to confirm the change took effect.
   **Use `detail`, not `list`**: `list` does not return the current trigger prices or auto-sell setting.

### Withdraw a grid's profit

> User: "Withdraw what my grid has earned"

1. `mexc spot grid list --data '{"page":"1","pageSize":"20"}'` → take the target strategy's `gridUniqId`.
2. `mexc spot grid asset-info --gridUniqId <id>` → read the **remaining withdrawable amount**, along with arbitrage profit, the amount already withdrawn and the invested funds.
   The amount to withdraw now must be greater than 0 and no more than the remaining withdrawable amount.
   **Use this command rather than subtracting fields from `detail` yourself**: it names "already withdrawn" and "withdrawable" separately, so neither is confused with the input of the next step.
3. `mexc spot grid withdraw-apply --data '{"gridUniqId":"<id>","withdrawAmount":"<amount to withdraw now>"}'`
   → moves the profit to the main account.
   ⚠️ `withdrawAmount` here is the **amount to withdraw now**, **not** the identically named cumulative amount in `detail`.
       ↓ step 3 is a write that moves funds, and **repeating it withdraws twice**: show a summary and wait for explicit confirmation before running.
       Never auto-retry on failure — re-query with step 4 first to see whether it already took effect.
4. `mexc spot grid asset-info --gridUniqId <id>` → re-query: the remaining withdrawable amount should have dropped and the amount already withdrawn should have risen.
   Add `mexc spot grid withdraw-records --data '{"gridUniqId":"<id>","page":"1","pageSize":"20"}'` when the individual records are wanted.

### Stop a grid

> User: "Stop my grid"

1. `mexc spot grid list --data '{"page":"1","pageSize":"20"}'` → take the target strategy's `gridUniqId`.
   **Keep this id**: after the stop it no longer appears in `list` and is the only way to query the strategy.
2. `mexc spot grid detail --gridUniqId <id>` → confirm the strategy really is running, and read its position and profit for the summary in step 3.
3. `mexc spot grid stop --gridUniqId <id> --autoSell <0 or 1>` → end the strategy.
   **`autoSell` decides whether the base asset is sold back to quote on exit**; the two outcomes affect the position completely differently, so state which one is being used before running.
       ↓ step 3 is a write that disposes of a position: wait for explicit confirmation before running.
4. `mexc spot grid detail --gridUniqId <id>` → re-query. **Two things to note**:
   first, right after the stop you **may** read stopping rather than stopped (when it settles depends on how fast clearing runs) and the position may still be held,
   so **on stopping, re-read shortly after until the state is stopped before reporting the outcome to the user**;
   second, **do not re-query with `list`** — the strategy already left `list` while stopping, so "not in the list" does not prove it has fully unwound.
   Once the stop has completed, call `mexc spot grid stop-reason --gridUniqId <id>` if the reason is wanted.

Only the parameters that disambiguate intent and make a command runnable are shown above; run `mexc schema spot.grid.<op>` for the full parameter set.

## Notes for Agents

- Implementation lives in `./api/` (one file declaring the operations; the actual
  request — signing, host resolution, sending — is delegated to the `pkg/`
  foundation).
- Inspect the exact request shape before calling with
  `mexc schema spot.grid.<operation>` (e.g. `spot.grid.place`).
- Reads are safe to auto-approve. Writes (place / modify-* / immediate-start /
  stop / withdraw-apply) change state or move funds — confirm with the user
  first; `withdraw-apply` is non-idempotent (never auto-retry).
- The caller is resolved from the API key, so never ask the user for a
  memberId — it is not a parameter.
- On a structured `failed_precondition` (e.g. `requires API credentials`), load
  the `mexc-auth` skill (credentials) and `mexc-common` (common error / safety
  rules), then replay; this skill does not describe login.
