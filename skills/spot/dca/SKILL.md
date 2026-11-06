---
name: mexc-spot-dca
scope: spot.dca
signed: yes
identity: yes
visibility: public
description: 'MEXC spot DCA (dollar-cost-averaging / auto-invest): list DCA-eligible trading pairs with PnL, and manage the caller''s DCA strategies (list / detail / plans / pause-resume-stop). Carries an API key; each request is signed. Triggers: "DCA / auto-invest trading pairs","which coins support DCA","my DCA strategies","list my auto-invest plans","strategy detail","pause / resume / stop a DCA strategy","dollar-cost-averaging".'
---

> Auth: this subdomain carries an openapi API key (`identity: yes`) and the CLI signs each request (`signed: yes`) with the profile secret. A saved profile (API key + secret) is required. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · dca (dollar-cost-averaging / auto-invest)

> Signed spot subdomain: every operation carries the openapi identity (`identity: yes`, your API key and secret) and the CLI signs each request (`signed: yes`) with the profile secret. A saved profile is required. POST operations send a JSON body via `--data`; GET uses flags. scope marks only the resource (`spot.dca`); the action lives in the access column.

## Core Concepts

- **symbol**: a trading pair, e.g. `BTC_USDT` (this gateway returns the underscore form `<base>_<quote>`; note it differs from the spot v3 concatenated form like `BTCUSDT`).
- **currency**: the base asset of the pair, e.g. `BTC`.
- **market**: the quote asset, e.g. `USDT`.
- **pnlInfoList**: per-period profit-and-loss rates. Each entry has a `timeType`
  (`PERIOD_1_MONTH` / `PERIOD_3_MONTH` / `PERIOD_6_MONTH` / `PERIOD_1_YEAR` /
  `PERIOD_3_YEAR` / `PERIOD_5_YEAR`) and a `totalPnlRate` (string decimal). An
  empty list means no PnL history yet.
- **strategy**: a caller-owned DCA plan configuration, identified by
  `strategyId`. It holds the invest amount, schedule, and the chosen symbols.
- **strategyId**: the id of a strategy, produced by `strategy-list` and consumed
  by `strategy-detail`, `strategy-plan-list`, and `strategy-set-state`.
- **state**: a strategy's lifecycle state — `1` running, `2` paused, `3` offline
  (stopped). `strategy-set-state` moves a strategy between them.
- **plan**: one scheduled execution instance of a strategy (a single periodic
  buy). `strategy-plan-list` lists a strategy's plans.
- The list responses are paginated: `data.result` (or `data.records`) is the
  array, with `total` / `page` / `pageSize` / `totalPage` alongside it.

## Important Notes

- **API key + secret required**: every operation needs a saved
  profile (`mexc profile set <name>` in a terminal (it prompts for the key/secret, no flags),
  then `mexc profile use <name>`). The framework injects the API key and signs
  each request with the secret; you never pass a signature or memberId as a flag.
- **POST body via `--data`**: `symbols`, `strategy-list`, `strategy-plan-list`,
  `strategy-create`, and the `strategy-set-*` writes take a JSON body through one
  `--data` flag (inspect fields with `mexc schema`); GET ops use flags.
- **Every body value is a string — quote it, even numbers and flags**: fields
  that look numeric (`page`, `pageSize`, `investAmount`, `percent`, `state`,
  `triggerFlag`, `tplId`, `accountPayment`) must be sent as quoted strings, e.g.
  `{"page":"1"}` and `{"triggerFlag":"1"}`, not `{"page":1}`. An unquoted number
  is rejected client-side (`cannot unmarshal number ... into ... of type string`),
  so it fails fast rather than silently. This is deliberate: the backend coerces
  the strings to their typed values and it keeps large ids from losing precision
  as JSON numbers. When unsure of a field's type, run `mexc schema spot.dca.<op>`.
- **strategyId chaining**: `strategy-detail`, `strategy-plan-list`, and
  `strategy-set-state` all need a `strategyId` obtained from `strategy-list`
  first — call `strategy-list` to resolve it before those.
- **strategy-set-state is a write**: it changes a live strategy's state
  (`2` pause, `3` stop). Confirm with the user before calling; do not
  auto-approve it.

## Operations

> scope is the resource identifier (always `spot.dca`); the action is in the access column. signed/identity flag whether a signature/API key is required; idempotent flags whether a repeat is safe.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot dca symbols` | `spot.dca` | read | yes | yes | yes | `POST /api/cli/spot/finance/dca/symbol/queryList` | List DCA-eligible trading pairs with per-period PnL rates. JSON body via `--data`, all optional: `{"sortDirection":"ASC\|DESC"}` |
| `mexc spot dca strategy-list` | `spot.dca` | read | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/list` | List the caller's DCA strategies. JSON body via `--data`, all optional: `{"type":"0\|1\|2","startDate","endDate","page","pageSize"}` |
| `mexc spot dca strategy-detail` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/strategy/detail` | One strategy's detail (`--strategyId` required) |
| `mexc spot dca strategy-plan-list` | `spot.dca` | read | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/plan/list` | Scheduled plans of a strategy. JSON body via `--data`: `{"strategyId"(required),"page","pageSize"}` |
| `mexc spot dca strategy-set-state` | `spot.dca` | write | yes | yes | yes | `GET /api/cli/spot/finance/dca/strategy/changeState` | Change a strategy's state (`--strategyId` required, `--state 1` resume / `2` pause / `3` offline) |
| `mexc spot dca symbols-recommended` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/symbol/recommendedList` | Recommended DCA trading pairs |
| `mexc spot dca symbols-basic` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/symbol/basic/list` | Brief info of DCA-eligible pairs |
| `mexc spot dca top-pick` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/symbol/topPick/strategy` | Curated top-pick DCA strategy |
| `mexc spot dca symbol-pnl-chart` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/symbol/pnl/chartData` | A pair's PnL chart data (`--symbolId`, `--interval` required) |
| `mexc spot dca strategy-precheck` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/strategy/preCheck` | Pre-check whether a new strategy may be created |
| `mexc spot dca strategy-stats` | `spot.dca` | read | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/stats/overview` | The caller's DCA statistics overview |
| `mexc spot dca strategy-chart` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/strategy/chartData` | A strategy's PnL chart data (`--strategyId` required) |
| `mexc spot dca plan-order-list` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/strategy/plan/order/list` | Orders of one plan (`--strategyId`, `--planId` required) |
| `mexc spot dca global-config` | `spot.dca` | read | yes | yes | yes | `GET /api/cli/spot/finance/dca/common/global/configs` | DCA global configuration (limits / switches) |
| `mexc spot dca strategy-set-name` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeStrategyName` | Rename a strategy. JSON body via `--data`, both required: `{"strategyId","strategyName"}` |
| `mexc spot dca strategy-set-amount` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeInvestAmount` | Change per-period invest amount. JSON body via `--data`, both required: `{"strategyId","investAmount"}` |
| `mexc spot dca strategy-set-schedule` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeSchedule` | Change schedule. JSON body via `--data`, all required: `{"strategyId","scheduleRule","timeZone"}` |
| `mexc spot dca strategy-set-account` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeAccountPayment` | Change payment account. JSON body via `--data`, both required: `{"strategyId","accountPayment":"0\|1"}` |
| `mexc spot dca strategy-create` | `spot.dca` | write | yes | yes | no | `POST /api/cli/spot/finance/dca/strategy/create` | Create a DCA strategy. JSON body via `--data`; required: `{"strategyName","scheduleRule","timeZone","investAmount","symbolList":[{"symbolId","percent"}]}`; optional: `accountPayment`, per-symbol `minPrice/maxPrice`, `scheduleKind`, `tplId`, `triggerFlag` |
| `mexc spot dca strategy-set-symbol` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeSymbol` | Replace a strategy's pair config. JSON body via `--data`, required: `{"strategyId","symbolList":[{"symbolId","percent"}]}` (optional per-symbol `minPrice/maxPrice`) |
| `mexc spot dca strategy-set-detail` | `spot.dca` | write | yes | yes | yes | `POST /api/cli/spot/finance/dca/strategy/changeDetail` | Whole-strategy update in one call. JSON body via `--data`, required: `{"strategyId","strategyName","investAmount","scheduleRule","timeZone","symbolList":[{"symbolId","percent"}]}`; optional: `accountPayment/scheduleKind/triggerFlag` |

## Permissions

> Authorization = resource(scope) × action(access). Reads query DCA data; the writes change a strategy's state or configuration.

| Operation | scope | access |
|-----------|-------|--------|
| symbols / symbols-recommended / symbols-basic / top-pick / symbol-pnl-chart / strategy-list / strategy-detail / strategy-plan-list / strategy-chart / strategy-stats / strategy-precheck / plan-order-list / global-config | `spot.dca` | read |
| strategy-create / strategy-set-state / strategy-set-name / strategy-set-amount / strategy-set-schedule / strategy-set-account / strategy-set-symbol / strategy-set-detail | `spot.dca` | write |

## Usage Examples

```bash
# List DCA-eligible pairs, highest PnL first (POST body)
mexc spot dca symbols --data '{"sortDirection":"DESC"}'

# List my running strategies (POST body)
mexc spot dca strategy-list --data '{"type":"0"}'

# One strategy's detail (GET) / its scheduled plans (POST body)
mexc spot dca strategy-detail --strategyId 12345
mexc spot dca strategy-plan-list --data '{"strategyId":"12345"}'

# Pause a strategy (write — dry-run first to confirm the request)
mexc spot dca strategy-set-state --strategyId 12345 --state 2 --dry-run
```

## Workflows

Multi-step DCA playbooks. A strategy is addressed by `strategyId` (from `strategy-list`), a trading
pair by `symbolId` (from `symbols` / `symbols-basic`), and one scheduled period by `planId` (from
`strategy-plan-list`). **All three have to be resolved by a query first — never assemble them from a
name the user spoke.**

### Pick symbols, evaluate them, then create a strategy

> User: "Set up a weekly auto-invest: 60% BTC, 40% ETH, 100 USDT a week."

1. `mexc spot dca global-config` → the global limits and switches. The cap on how many strategies one
   account may hold is the hard boundary of this playbook; never advise past it.
2. `mexc spot dca symbols --data '{"sortDirection":"DESC"}'` → DCA-eligible pairs with their
   per-period PnL rates, and **this is also the step that resolves the coin the user named into a
   `symbolId`**. Match on `currency` (e.g. `BTC`) or `symbol` (e.g. `BTC_USDT`), then take that row's
   **`symbolId`**.
   ⚠️ The same row also carries `currencyId` / `marketCurrencyId` / `marketId`. They look identical in
   shape but **none of them is accepted here**, and passing the wrong one does not raise an error.
   When only the amount bounds are needed, use `mexc spot dca symbols-basic`: it adds each pair's
   minimum and maximum per-period amount, which is what the amount in step 5 must satisfy.
3. `mexc spot dca symbol-pnl-chart --symbolId <id from step 2> --interval <period>` → that pair's
   historical PnL curve, to back the choice.
   ⚠️ A wrong id returns a **success response with an empty curve**, not an error. So an empty curve
   means "suspect the id first" — do not tell the user the pair has no PnL history.
4. `mexc spot dca strategy-precheck` → whether a new strategy may still be created. It returns a
   truthy value while a slot is free; **once the cap is reached it returns a business error saying the
   maximum has been reached, rather than a falsy value**. Handle that as the error branch and tell the
   user an existing strategy has to be stopped first (see the pause / resume / stop playbook).
5. `mexc spot dca strategy-create --data '{"strategyName":"<name>","scheduleRule":"<cron>","timeZone":"+08:00","investAmount":"<per-period amount>","symbolList":[{"symbolId":"<idA>","percent":"0.6"},{"symbolId":"<idB>","percent":"0.4"}]}'`
   - The `percent` values in `symbolList` are **decimals, not percentages**: `1` = 100%, `0.6` = 60%
     (sending `100` is rejected as `invalid percent: 100.00`). They **must add up to exactly 1** (100%),
     or the whole request is rejected. Mind the silent trap — for "put everything into one pair" the
     value is `1`, not `0.1`; `0.1` would quietly invest only 10%. When confirming with the user,
     restate each share as a percentage (e.g. `0.6` → 60%) so a wrong magnitude is caught.
   - The per-period amount must fall inside the min / max bounds of every chosen pair (step 2).
   - For how `scheduleRule` branches per frequency, see step 2 of the configuration playbook; the
     **interval between two investments must exceed one hour**, otherwise it is rejected.
   - This write is **not idempotent**: running it again creates a second strategy, so never resend it
     just because the call timed out.
     ↓ Before running, summarise the name, per-period amount, schedule, symbols with their split and
     the funding account, and wait for explicit confirmation.
6. `mexc spot dca strategy-list --data '{"type":"0"}'` → confirm the strategy exists and is running;
   to check the full configuration, follow with
   `mexc spot dca strategy-detail --strategyId <the new strategyId>`.

### Create a strategy from a curated template

> User: "Just give me a ready-made auto-invest plan."

1. `mexc spot dca top-pick` → a set of **ready-made strategy templates** (not a symbol list). Each
   template carries a name, a description, a schedule, and its symbols with their split, plus an
   adoption count and an annualised PnL rate to choose by. A template's split already adds up, which
   saves picking symbols and allocating percentages by hand.
2. `mexc spot dca strategy-precheck` → as above, confirm a slot is free.
3. `mexc spot dca strategy-create --data '{"strategyName":"<name>","scheduleRule":"<template schedule>","timeZone":"+08:00","investAmount":"<per-period amount>","symbolList":<template symbols and split>,"tplId":"<template id>"}'`
   - Reuse the symbols, split and schedule the template returned; **the per-period amount is still the
     user's decision** and must satisfy the bounds of every symbol in the template.
   - `tplId` passes the template id along as the interface intends. Its effect
     cannot be observed from this sub-domain's responses (the strategy detail does not echo it back
     and the template's adoption count does not move), so never promise the user the strategy is
     "linked to that template" — what actually takes effect is the reused symbols, split and schedule.
   - If the user tweaks the template, the split must still add up to exactly 100%.
     ↓ Before running, summarise the template name, its symbols with their split, the schedule and the
     per-period amount, and wait for explicit confirmation.
4. `mexc spot dca strategy-list --data '{"type":"0"}'` → confirm it was created.

### Review a strategy and drill down to one period's orders

> User: "How is my auto-invest plan doing?"

1. `mexc spot dca strategy-list --data '{"type":"2","page":"1","pageSize":"20"}'` → the strategy list;
   take the target `strategyId`. `type` reads as: `0` is **both running and paused**, `1` is stopped,
   `2` is everything. Pagination values are passed as strings.
   ⚠️ Because `0` covers running and paused alike, **never infer state from "which list it showed up
   in" — read each row's state field.** That field has four values: initialising, running, paused and
   stopped (initialising is a brief transient right after creation, and can still be read).
2. `mexc spot dca strategy-detail --strategyId <id>` → that strategy's full configuration and current
   state.
   ⚠️ An id that does not exist returns a **success response with no data body**, and passing a
   non-numeric value (a strategy name, say) yields an internal error. Neither reads as a clean
   "not found", so the id has to come from step 1.
3. `mexc spot dca strategy-plan-list --data '{"strategyId":"<id>","page":"1","pageSize":"20"}'` → that
   strategy's scheduled plans; take the target `planId`.
   ⚠️ **An empty result is normal**: before the first funding period there are no scheduled plans at
   all, and that says nothing about the strategy being broken. A stopped strategy still keeps its past
   plans, so history stays queryable after it goes offline.
4. `mexc spot dca plan-order-list --strategyId <id> --planId <planId from step 3>` → the orders that
   period actually produced. **Both ids are required**; omitting either is refused locally.
   ⚠️ Read placement and fill apart: a non-zero order amount does not mean anything was bought — check
   the filled quantity and filled amount. A period can place successfully and fill nothing.
5. When overall performance is wanted, add `mexc spot dca strategy-chart --strategyId <id>` for that
   strategy's PnL curve and `mexc spot dca strategy-stats` for the account-level summary.
   ⚠️ The two are scoped differently, so never mix them in one answer: total PnL, PnL rate, total
   invested and current portfolio value in the strategy detail are **per strategy**, while the
   cumulative PnL, invested amount and running-strategy count in the account summary cover the
   **whole account**.

### Change an existing strategy's configuration

> User: "Change how much my auto-invest puts in each period."

1. `mexc spot dca strategy-list --data '{"type":"2"}'` → locate the strategy and **read its current
   state at the same time** — the state decides whether this change can land. When going for the
   whole-strategy update, also run `mexc spot dca strategy-detail --strategyId <id>` to read every
   current value for carrying forward.
   ⚠️ State precondition: **a paused strategy can still be reconfigured**; **a stopped strategy
   accepts no configuration change at all** and is refused with a message that it has gone offline.
2. Pick one path:
   - **Change a single field** (each its own command, carrying only the new value):
     - per-period amount → `mexc spot dca strategy-set-amount --data '{"strategyId":"<id>","investAmount":"<amount>"}'`
     - schedule → `mexc spot dca strategy-set-schedule --data '{"strategyId":"<id>","scheduleRule":"<cron>","timeZone":"+08:00"}'`
     - name → `mexc spot dca strategy-set-name --data '{"strategyId":"<id>","strategyName":"<name>"}'`
     - funding account → `mexc spot dca strategy-set-account --data '{"strategyId":"<id>","accountPayment":"<0 spot only / 1 spot + flexible savings>"}'`
   - **Change several at once** → `mexc spot dca strategy-set-detail --data '{"strategyId":"<id>","strategyName":"<name>","investAmount":"<amount>","scheduleRule":"<cron>","timeZone":"+08:00","symbolList":[...]}'`
     ⚠️ Its name, per-period amount, schedule, time zone and symbol list are **all required**: carry
     the current value forward for every field you are not changing, or it will be overwritten with
     whatever you send. Optional fields left out are not cleared. The symbol list is replaced
     **wholesale** and its split must still add up to exactly 100%; when validation fails the whole
     request is rejected and nothing lands partially.

   **The schedule branches per frequency, and each branch requires different pieces**
   (`scheduleRule` is a six-field cron: second minute hour day month weekday):
   - Every N hours (N in 1–24): only the interval, no time of day.
   - Every N days (N in 1–30): needs a time of day.
   - Weekly: needs **a weekday** plus a time of day.
   - Monthly: needs **a day of month** plus a time of day. Keep to 1–28;
     ⚠️ a larger day (30, say) **is not rejected**, but it will not fire in months that lack it —
     point that consequence out to the user.
   - Whichever branch, the **interval between two investments must exceed one hour**, or it is
     rejected.
   - `timeZone` takes a signed UTC offset such as `+08:00`.
     ↓ These are writes: state which fields change and from which value to which, then wait for
     explicit confirmation.
3. `mexc spot dca strategy-detail --strategyId <id>` → re-query to confirm the change landed.

> Changing only the symbols and their split maps to
> `mexc spot dca strategy-set-symbol --data '{"strategyId":"<id>","symbolList":[...]}'`.
> That command currently answers "not found" and cannot be used. Until it works, use the
> whole-strategy update above and carry every field you are not changing forward at its
> current value.

### Pause, resume or stop a strategy

> User: "Put my auto-invest on hold for now."

1. `mexc spot dca strategy-list --data '{"type":"2"}'` → take the target `strategyId` **and its
   current state**. **Read the current state before choosing the target state**, or you will send
   "pause" to a strategy that is already stopped.
2. `mexc spot dca strategy-set-state --strategyId <id> --state <1 resume / 2 pause / 3 stop>` → move
   it between states.
   - Running and paused **convert back and forth**, and repeating the same target state converges.
   - **Stopped is terminal**: a stopped strategy can neither be resumed nor paused again, and none of
     its configuration can be changed.
   - Stopping **frees one strategy slot**. Since an account holds very few, "switch to another
     auto-invest plan" really means stopping the old strategy and creating a new one — so make sure
     the user understands this step cannot be undone.
     ↓ This is a write: state which target state it moves to and whether that is irreversible, then
     wait for explicit confirmation.
3. `mexc spot dca strategy-list --data '{"type":"2"}'` → re-query to confirm the state changed.
   ⚠️ The `type` used to re-query has to cover the target state: after stopping, `0` will not find it —
   use `1` or `2`.

Only the parameters that distinguish the intent and make a command runnable are shown above; run
`mexc schema spot.dca.<op>` for the full set.

## Notes for Agents

- Implementation lives in `./api/` (one file declaring the operations; the actual
  request — signing, host resolution, sending — is delegated to the `pkg/`
  foundation).
- Inspect the exact request shape before calling with
  `mexc schema spot.dca.<operation>` (e.g. `spot.dca.strategy-list`).
- Read operations are safe to auto-approve; the writes (`strategy-set-*`) change
  a strategy's state/config — confirm with the user first.
- The caller is resolved from the API key, so never ask the user for a
  memberId — it is not a parameter.
- On a structured `failed_precondition` (e.g. `requires API credentials`), load
  the `mexc-auth` skill (credentials) and `mexc-common` (common error / safety
  rules), then replay; this skill does not describe login.
