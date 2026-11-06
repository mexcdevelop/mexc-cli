---
name: mexc-spot-convert
scope: spot.convert
signed: yes
identity: yes
visibility: public
description: 'MEXC spot flash convert — swap one asset directly into another at a quoted rate with no order book: list convertible currencies and their min/max amounts, quote by the amount you spend or the amount you receive, execute a quotation, place a limit convert that waits for your rate, cancel a limit convert that is still waiting, and query your convert orders. Quoting and querying are read; executing, placing and cancelling are write. Triggers: "convert","flash convert","swap USDT to BTC","exchange one coin for another","convert quote","conversion rate","limit convert","cancel limit convert","cancel my convert order","withdraw a resting convert","convert order history","how much BTC will I get for","min convert amount".'
---

> Auth: every operation needs credentials and every request is signed (`signed: yes`, `identity: yes`) — the convert flow moves your own assets, the CLI signs each request with the profile secret, and the gateway authenticates the whole surface, so there is no public call here and a saved profile needs **both** the api key and the secret. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# spot · convert (flash convert)

> Direct asset-for-asset conversion at a quoted rate — no order book, no `symbol`, no side. Reads quote and query; writes execute a conversion. scope marks only the resource (`spot.convert`); the action lives in the access column.

## Core Concepts

- **Convert is not trading.** There is no trading pair, no `BUY`/`SELL` and no order book. You name **two currencies** and an amount, and the backend answers with a rate. Nothing about `spot.trade` carries over.
- **Currencies are identified by `currencyId`, not by name.** These are opaque backend ids (not `BTC` / `USDT`), so the flow always starts at `currencies`, which maps names to ids. Passing a ticker symbol where a `currencyId` is expected fails.
- **from / to**: `fromCurrencyId` is what you **spend**, `toCurrencyId` is what you **receive**. Amount fields follow the same convention: `fromConvertQuantity` is the amount spent, `toConvertQuantity` the amount received.
- **Quote direction (`tradeType`)**: `1` = **forward**, you fix what you spend (`fromConvertQuantity`) and ask what you get. `2` = **reverse**, you fix what you want to receive (`toConvertQuantity`) and ask what it costs.
- **Quotation vs convert order — two different ids.** A quote returns a **quotation order id**; `place` consumes that id to produce a **convert order**. A quotation is short-lived: once it expires, `place` is rejected and you must re-quote.
- **A limit convert is addressed by its `quotationOrderId`, not its `id`.** `place-limit` returns the **quotation order id** (numeric), and that is the only handle `cancel-limit` accepts. The convert order's own `id` is a different, prefixed string; passing it to `cancel-limit` fails, because that parameter is parsed as a number.
- **Market convert is two steps** (`quote-*` → `place`); **limit convert is one step** (`place-limit`), carrying its own `convertRate` and resting until the market reaches it.
- **The two PLACEMENT ops carry three optional request headers.** `place` and `place-limit` take `--platform` (only `CLI` is accepted, since that is what this binary is), `--ip` and `--language`. These are headers, not body fields, so they are flags rather than `--data` keys. All three are optional — the backend accepts a placement without them — but **send `--platform CLI`**: it is the one value this product can truthfully report, and an omitted flag means an absent header rather than a defaulted one. **No other operation here takes them**: the quotes, `currencies`, `quantity-limit`, `orders` and `cancel-limit` reject the flags outright. Leave `--ip` off unless you have a real address to attribute the call to — the CLI cannot know its own public address, so a made-up value is worse than none, and the backend falls back to the address it observes.
- **Only a limit convert can be cancelled**, and only while it is still resting. A market convert executes immediately, so there is nothing to withdraw; `cancel-limit` is the exit for a limit convert that has not triggered yet.
- **`accountType`** is where the converted asset lands, and it travels as a **NAME, not a number**: `SPOT` (default) or `CONTRACT`. It is a backend enum; `"0"` / `"1"` are not accepted.
- **`expireType`** is a limit convert's lifetime, and it travels as a **NUMBER**: `1` = 1 hour, `2` = 1 day, `3` = 3 days, `4` = 1 week, `5` = 30 days (default). An unknown code is rejected as a bad request.
- **`convertRate` is "how much TO per 1 FROM"** — so a *higher* rate is better for you, and a limit convert asks for a rate the market has not reached yet. It must be **strictly higher than the live rate**; the three amount fields must also satisfy `fromConvertQuantity × convertRate ≥ toConvertQuantity`.
- **A limit convert has no direction choice.** Unlike the quotes, `place-limit` is always *forward*: you fix `fromConvertQuantity`, the amount you spend. There is no `tradeType` on it.

Convert order fields returned by `orders` (and by `place`):

| Field | Meaning |
|---|---|
| `id` | Convert order id |
| `quotationOrderId` | The quotation this order was executed from |
| `state` | `0` resting limit convert (not yet converted), `1` converting, `2` succeeded, `3` failed, `4` cancelled, `5` expired |
| `fromCurrencyId` / `toCurrencyId` | Currency ids of the two sides |
| `fromCurrency` / `toCurrency` | Currency names, when the backend resolves them |
| `fromConvertQuantity` / `toConvertQuantity` | Amount spent / amount expected |
| `realConvertQuantity` | Amount actually received — this, not `toConvertQuantity`, is the settled result |
| `convertOrderPrice` | Rate the order was booked at |
| `convertFee` | Fee charged |
| `orderType` / `expireType` | Order kind and lifetime |
| `cancelType` | Why it was cancelled: `1` you cancelled it (`cancel-limit`), `2` cancelled by the system, `3` expired, `4` withdrawn by price protection |
| `createTime` / `updateTime` / `settlementTime` / `deadTime` | Created / last changed / settled / expiry timestamps |

## Resource Relationships

```
Currency config            currencies       (read)  names -> currencyId, plus your balance
      └── pair limits      quantity-limit   (read)  min/max convertible amount

Quotation                                   the short-lived priced offer
      ├── quote-forward    (read)  fix the amount you SPEND
      └── quote-reverse    (read)  fix the amount you RECEIVE
              │ returns a quotation order id
              ▼
Convert order
      ├── place            (write) execute that quotation      -> market convert
      ├── place-limit      (write) rest at your own rate       -> limit convert
      │       │ returns the quotation order id
      │       ▼
      ├── cancel-limit     (write) withdraw it while it rests  (orderId = that id)
      └── orders           (read)  query, paged
                                   isCurrent=true  -> resting limit converts
                                   isCurrent=false -> finished ones
```

## Important Notes

- **Signed, and not public.** Every request here is signed (`signed: yes`): the CLI signs it with the profile secret and the gateway verifies it, resolving the member from the api key the request also carries. So a profile needs both halves — a key alone is not enough. Credentials are required for EVERY operation, with no exception: the gateway authenticates the whole `/api/cli/*` surface, so a request carrying no key is refused with `api key required` (HTTP 400) before any handler runs.
- **What the signature covers: the query on a GET/DELETE, the JSON body on a POST.** The quotes, `orders` and `cancel-limit` travel in the query string, which is what gets signed; `place` and `place-limit` have their `--data` covered as well, byte for byte as sent — so build the body once and send it as is rather than editing it in flight. The three placement headers (`--platform` / `--ip` / `--language`) sit outside the signature, which is why sending or omitting them per call is free.
- **Start from `currencies`.** Every other operation takes `currencyId` values, and there is no name-based alternative. `currencies` also returns your **balance per currency**, so it doubles as the "what can I convert and how much do I have" call.
- **Respect `quantity-limit` before quoting.** Each pair has a min and max convertible amount (`fromConvertQuantityMin` / `fromConvertQuantityMax`); an amount outside that range is rejected.
- **`tradeType` is what actually picks the direction — not the command you chose.** The backend serves `quote-forward` and `quote-reverse` with the *same* handler; the URL is not what decides. `tradeType` defaults to `2` (reverse), so `quote-forward` requires it explicitly (this skill marks it required for that reason): omit it and you silently get a reverse quote that then demands `toConvertQuantity`.
- **Send the amount that matches the direction**: forward (`tradeType: 1`) requires `fromConvertQuantity`; reverse (`tradeType: 2`) requires `toConvertQuantity`. Sending the wrong one is rejected.
- **A quotation expires.** `place` on an expired quotation fails with a "quotation order expired" error — that is not a retryable transport failure, it means re-quote and place again. Do not re-send the same id.
- **`place-limit`'s rate must beat the market, or it is refused.** The backend fetches the live rate and rejects the order when that rate already meets your `convertRate` ("normal convert rate greater than limit convert rate"). A limit convert is only for a rate the market has *not* reached — if you want the current rate, use `quote-*` + `place` instead. So always quote first and set `convertRate` strictly above what the quote reports.
- **`place-limit`'s three amounts are cross-checked.** `fromConvertQuantity × convertRate` must be `>= toConvertQuantity` and all three must be positive, otherwise the order comes back as `params error`. Deriving `toConvertQuantity` as `fromConvertQuantity × convertRate` always satisfies it.
- **`place-limit` rejects excess precision rather than rounding it.** If `fromConvertQuantity` carries more decimal places than the pair's quantity precision, the order fails with `params error` — it is not silently truncated. The pair's min/max from `quantity-limit` applies here too, and your balance is checked at placing time.
- **Placing a limit convert FREEZES the funds.** `fromConvertQuantity` of `fromCurrencyId` is held from the moment it rests until it triggers, expires, or you cancel it — so a resting limit convert reduces the balance available to other converts. `cancel-limit` releases it.
- **Some `place-limit` rejections are account or market state, not bad input**: the convert switch being closed, an unknown `fromCurrencyId` / `toCurrencyId` ("fromCurrency not exist"), a pair hidden for your account ("You can't place orders for this pair for now"), or an account-level restriction. None of these is retryable — report them rather than re-sending.
- **`cancel-limit` only works on a limit convert that is still resting.** The backend looks for a pending order and, finding none, answers `limit convert order is expire` — which is what you also get for an order that already triggered, expired, or was cancelled before. Read it as "no longer cancellable", not as a bad id: confirm the current state with `orders`.
- **`cancel-limit` is deduped for ~3 seconds per order id.** A second cancel inside that window returns "the cancel request was sent too quickly, please try again after 3 seconds". It means the first one is being processed — wait and verify with `orders`, do not hammer it.
- **Cancelling releases the funds the limit convert froze** — the `fromConvertQuantity` of `fromCurrencyId`, held since `place-limit`. The order then reads `state` `4` with `cancelType` `1`.
- **Only `orderId` identifies the cancel; there is no cancel-all.** Each resting limit convert is withdrawn individually, so cancelling several means one call per `quotationOrderId` from `orders --isCurrent true`.
- **Concurrent converts are rejected, not queued.** A duplicate in-flight convert comes back as "flash orders are in progress, please wait patiently"; wait and re-query rather than hammering `place`.
- **`realConvertQuantity` is the settled amount.** `toConvertQuantity` is the expectation; read `realConvertQuantity` from `orders` to see what actually arrived.
- **`orders` paging and filters are all optional** — with no time range it returns the default page (`pageNum` 1, `pageSize` 20). `isCurrent` is the switch between resting limit converts (`true`) and finished orders (`false`/omitted); the state set behind it is chosen by the backend, not by you.
- **Times are epoch milliseconds** on input (`startTime` / `endTime`).
- **`businessType` stays `0`.** This subdomain is scoped to spot convert; other values address other convert business lines and are not what these commands are for.

## Operations

> scope is always the resource `spot.convert`; the action (read/write) is in the access column. signed/identity flag whether a signature/credential is required; idempotent flags whether a repeat is safe — the two placing writes are NOT, `cancel-limit` is.

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc spot convert currencies` | `spot.convert` | read | yes | yes | yes | `GET /api/cli/spot/convert/currency/list` | List convertible currencies (name → currencyId) with your balance per currency |
| `mexc spot convert quantity-limit` | `spot.convert` | read | yes | yes | yes | `GET /api/cli/spot/convert/quantity/limit/get` | Min/max convertible amount for a currency pair |
| `mexc spot convert quote-forward` | `spot.convert` | read | yes | yes | yes | `GET /api/cli/spot/convert/calculate/quantity/forward` | Quote by the amount you SPEND (pass `tradeType 1`) |
| `mexc spot convert quote-reverse` | `spot.convert` | read | yes | yes | yes | `GET /api/cli/spot/convert/calculate/quantity/reverse` | Quote by the amount you RECEIVE (`tradeType` defaults to 2) |
| `mexc spot convert place` | `spot.convert` | write | yes | yes | no | `POST /api/cli/spot/convert/order/place` | Execute a quotation by its id — market convert (body on `--data`; send the `--platform CLI` header) |
| `mexc spot convert place-limit` | `spot.convert` | write | yes | yes | no | `POST /api/cli/spot/convert/limit/order/place` | Place a limit convert that rests until your rate is reached (body on `--data`; send the `--platform CLI` header) |
| `mexc spot convert cancel-limit` | `spot.convert` | write | yes | yes | yes | `DELETE /api/cli/spot/convert/limit/order/cancel` | Cancel a resting limit convert by its quotation order id, releasing the frozen funds |
| `mexc spot convert orders` | `spot.convert` | read | yes | yes | yes | `GET /api/cli/spot/convert/order/query` | Query your convert orders, paged |

## Permissions

> Authorization = resource(scope) × action(access). scope is always `spot.convert`; actions split into read/write.

| Operation | scope | access |
|-----------|-------|--------|
| place / place-limit / cancel-limit | `spot.convert` | write |
| currencies / quantity-limit / quote-forward / quote-reverse / orders | `spot.convert` | read |

## Usage Examples

```bash
# 1. find the currencyId of the assets you want to convert (also shows your balance)
mexc spot convert currencies

# 2. check the pair's min/max convertible amount
mexc spot convert quantity-limit \
  --fromCurrencyId 128f589271cb4951b03e71e6323eb7be \
  --toCurrencyId 2fb942154ef44a4ab2ef98c8afb6a4a7

# 3a. forward quote: "I want to spend 100 USDT, how much BTC do I get?"
mexc spot convert quote-forward \
  --fromCurrencyId 128f589271cb4951b03e71e6323eb7be \
  --toCurrencyId 2fb942154ef44a4ab2ef98c8afb6a4a7 \
  --fromConvertQuantity 100 --tradeType 1

# 3b. reverse quote: "I want to end up with 0.001 BTC, what does it cost?"
mexc spot convert quote-reverse \
  --fromCurrencyId 128f589271cb4951b03e71e6323eb7be \
  --toCurrencyId 2fb942154ef44a4ab2ef98c8afb6a4a7 \
  --toConvertQuantity 0.001

# 4. execute the quotation the quote returned (id comes from step 3)
# --platform is a header, not a body field. All three headers are optional, but
# send --platform CLI: an omitted flag means no header at all. --ip and --language
# are best left off unless you have a reason to set them.
mexc spot convert place --platform CLI \
  --data '{"id":"<quotationOrderId>","accountType":"SPOT"}'

# limit convert instead: rest until the rate is reached, expire after 1 day.
# convertRate must be STRICTLY higher than the live rate from step 3a, and
# toConvertQuantity <= fromConvertQuantity * convertRate (here: 100 * 0.00001 = 0.001).
# Note the wire forms: expireType is a NUMBER, accountType is a NAME.
mexc spot convert place-limit --platform CLI --data '{
  "fromCurrencyId":"128f589271cb4951b03e71e6323eb7be",
  "toCurrencyId":"2fb942154ef44a4ab2ef98c8afb6a4a7",
  "fromConvertQuantity":"100","toConvertQuantity":"0.001",
  "convertRate":"0.00001","expireType":2,"accountType":"SPOT"
}'

# resting limit converts, then finished ones
mexc spot convert orders --isCurrent true
mexc spot convert orders --pageNum 1 --pageSize 20

# cancel a limit convert that is still resting — orderId is the quotationOrderId
# from `orders --isCurrent true` (the same id place-limit returned)
mexc spot convert cancel-limit --orderId 570123456789012345
```

## Workflows

Multi-step convert playbooks. Both sides of a convert are addressed by `currencyId`, and that value can only be resolved from the currency list.

### Market convert: execute at the quoted rate

> User: "Swap my A for B"

1. `mexc spot convert currencies` → every convertible currency. **This operation cannot filter by name** (its only parameter is `state`), so fetch the whole list and resolve locally: compare the name the user gave against each record's **`currency`** field, case-normalised and matched **exactly**, then take `currencyId` from that same record.
   - **Do not match on `vcoinNameFull`**: it frequently disagrees with the ticker code, so matching on it returns zero rows.
   - **The numeric `id` in a record is not the `currencyId`**; sending it is rejected (`56013`).
   - The same record already carries `balanceAmount`, `scale` (quantity precision) and `supportAccountType`, so a balance check needs no extra call.
2. `mexc spot convert quantity-limit --fromCurrencyId <A> --toCurrencyId <B>` → the min/max convertible amount for a single convert.
   **The limit is keyed on the currency being spent** (the payload carries only `fromCurrencyId`); an amount outside it is rejected at the next step (`56009`).
3. Quote on whichever side the user fixed. **Both forms must carry `--isQuotation true`**:
   - fixing what you SPEND → `mexc spot convert quote-forward --fromCurrencyId <A> --toCurrencyId <B> --fromConvertQuantity <amount> --tradeType 1 --isQuotation true`
   - fixing what you RECEIVE → `mexc spot convert quote-reverse --fromCurrencyId <A> --toCurrencyId <B> --toConvertQuantity <amount> --isQuotation true`
   → returns the rate, the lifetime `quotationExpire`, and **the quotation order id**, which is the only input the next step takes.
   - **`--isQuotation true` is not optional.** Omitting it, or passing `false`, yields an indicative rate only. That response **also carries an id**, but executing it is rejected as "quotation order expired" — and an indicative quote **does not check the limit at all**, so it succeeds even for an amount above the maximum.
   - When fixing what you RECEIVE, the limit is applied to the amount the backend **derives for the spend side**: the user supplied the receive amount, so the derived spend amount may fall outside the limit. Work back from the limit in step 2 and the rate to a feasible receive range, or adjust on `56009`.
   - **`tradeType` is what actually picks the direction, not the command you chose**: the forward path must pass `1` explicitly, because the backend defaults to reverse.
4. Show the user the rate, both amounts and the fee, and **wait for explicit confirmation**. A quotation is short-lived; if the user deliberates, go back to step 3 and re-quote.
5. `mexc spot convert place --data '{"id":"<quotation order id from step 3>"}'` (`--platform CLI` may be added; the header is optional) → execute at that quotation.
   **Send only the quotation order id — do not resend the currencies or the amounts**; the terms are already locked into the quotation.
       ↓ step 5 moves funds: run it only after the confirmation from step 4.
   - If it returns "quotation order expired", the quotation was refused and nothing was consumed: go back to step 3 and re-quote. **Do not resend the same id.**
   - A valid quotation can still fail to fill on the market side, which comes back as `convert fail` (`56019`). `place` is **not idempotent**, so do not auto-retry it and do not resend the same id — the attempt already reached the market. **Go to step 6 first and confirm from `orders` whether anything settled**; only once you know it did not may you re-quote, and that is a fresh write needing its own confirmation.
6. `mexc spot convert orders` → confirm the outcome and read `realConvertQuantity` (what actually arrived), not `toConvertQuantity` (the expectation).
   - After a fill `state` may sit at "converting" rather than "succeeded", so **do not judge success by `state`** — go by the amount actually received.
   - To pin down one row, add `--id <convert order id>`. Note this takes the **convert order id**, which is a different value from the quotation order id used to cancel.

### Limit convert: rest at a target rate until it triggers

> User: "Convert it for me once the rate reaches X"

1. `mexc spot convert currencies` and `mexc spot convert quantity-limit` → as above, for both ids, the limit and the precision, and confirm the balance covers it — placing **freezes** the amount to be spent.
2. `mexc spot convert quote-forward --fromCurrencyId <A> --toCurrencyId <B> --fromConvertQuantity <amount> --tradeType 1` → the live rate, as a reference. **This step is not optional**: `convertRate` must be **strictly higher** than the live rate or the order is refused (`30046`); a rate the market has already reached belongs to a market convert.
3. `mexc spot convert place-limit --data '{...}'` (`--platform CLI` optional) → rest a limit convert. **The value it returns is the quotation order id itself** (a bare value, not an object), and that is the only handle a cancel accepts.
   - The three amounts are cross-checked: `fromConvertQuantity × convertRate` must be `>= toConvertQuantity`, and all three must be positive; otherwise it comes back as `params error` (`56015`). Deriving `toConvertQuantity` from `fromConvertQuantity × convertRate` always satisfies it.
   - The spend amount is bound by the same limit from step 2 (outside it → `56009`).
       ↓ step 3 is a write that freezes funds: show the user the target rate, both amounts and the lifetime, and wait for confirmation.
   - `place-limit` is **not idempotent**, and each call freezes funds of its own. If the outcome is ever unclear, **do not resend** — go to step 4 and check the resting list first, or you may end up with two orders freezing twice the amount.
4. `mexc spot convert orders --isCurrent true` → confirm it is resting and keep its **quotation order id**. The resting row also carries the target rate and the expiry time.
   A resting order has three possible ends: it triggers when the market reaches the rate, it expires untriggered, or the user cancels it. The first two need no further command — read the result with step 5; for cancelling, see the next playbook.
5. `mexc spot convert orders` (without `--isCurrent`) → the finished set carries the final state and the amount actually received.
   **Leaving the resting list does not mean it filled** — it may have expired or been cancelled; the final state is only readable from the finished set.

### Cancel a resting limit convert

> User: "Cancel that limit convert I have sitting there"

1. `mexc spot convert orders --isCurrent true` → this is the full set of cancellable orders; take the **quotation order id** from each row. Only a still-resting limit convert can be cancelled; filled or expired ones are not in this list.
2. Show the user which order (or orders) is about to be withdrawn and **wait for explicit confirmation** — this cancels real orders and changes what funds are tied up.
       ↓ the next step is a write.
3. `mexc spot convert cancel-limit --orderId <quotation order id>` → withdraw it and release the funds frozen at placing time.
   **This takes the quotation order id, not the convert order id**; they differ, and the wrong one will not cancel anything.
   - **There is no cancel-all**: to withdraw several, walk the list from step 1 one call at a time.
   - **Repeating the same id within ~3s is refused** (`30048`, "sent too quickly"), which means the first call is still being processed — verify with step 4 instead of hammering it. Different ids are not affected.
4. Re-query **both sets**: `mexc spot convert orders --isCurrent true` to confirm it is no longer resting, then without `--isCurrent` to confirm the final state (a successful cancel reads as cancelled, with the cancel reason "cancelled by you").
   **Leaving the resting list neither proves the cancel worked nor rules out a fill** — read both sets. The state is written asynchronously, so allow a short delay before re-querying.
   - A cancel refused as "limit convert order is expire" (`30050`) means it is **no longer cancellable** — it may have filled, expired, or already been cancelled. That is not a bad id: check the finished set for what actually became of it.

Only the parameters that distinguish the intent and make a command runnable are shown above; run `mexc schema spot.convert.<op>` for the full set.

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op (idempotent=no) — a lost `place` response could mean the conversion already happened, so query `orders` instead of re-sending.
- Show a summary and wait for explicit user confirmation before any write operation (executing, placing, or cancelling a convert).
- Unsure about parameters? run `mexc schema spot.convert.<op>` instead of guessing.
- On a structured credential error (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials, and the `mexc-common` skill for common error / safety / security rules, then replay; this skill does not describe login. (These are separate skills installed alongside the CLI — load by name.)

**Skill-specific notes**
- Implementation lives in `./api/convert.go` (descriptors only; the request is delegated to the framework).
- **Never invent a `currencyId`.** They are opaque backend ids with no relation to ticker symbols — always resolve them via `currencies` first, in the same session.
- **A quotation is not a price you can hold on to.** Quote → confirm → place should happen promptly; if the user deliberates, re-quote rather than placing a stale id.
- `place` and `place-limit` take a JSON body **plus** the `--platform CLI` header flag — build the body from `mexc schema spot.convert.<op>`, never from guesswork, and read the same schema for the header params (`platform` / `ip` / `language`, all optional). `cancel-limit` is the opposite: a single `--orderId` flag, no body, no headers.
- **Body field types are load-bearing, so read them off `schema`**: `expireType` is an integer (`2`, not `"2"`) and `accountType` is a name (`"SPOT"`, not `"0"`). The CLI validates `--data` against the declared shape on a real call and rejects a wrong type, unknown field, or bad enum value before anything is sent.
- **`--dry-run` checks the body you hand it.** A `--data` passed with it is validated on the same required / enum / unknown-field rules a real call applies, so a malformed payload fails in the preview rather than looking fine there; the plan's `body_checked` reports `true` when that happened and `false` when no payload was given (omitting `--data` is not an error). Still read `schema` for field shapes — and note a dry-run cannot judge business state, so it will not tell you a quotation has expired or a rate is below the market.
- **Cancelling needs the right id.** `cancel-limit --orderId` takes the `quotationOrderId` (what `place-limit` returned), not the convert order `id`. If a cancel is rejected outright, check which of the two ids you passed before assuming the order is gone.
- In `--read-only` mode the write ops (`place` / `place-limit` / `cancel-limit`) are refused; the quotes and queries still work.
- Do not treat "quotation order expired", "flash orders are in progress", "limit convert order is expire", or "the cancel request was sent too quickly" as transport failures — all four are business states with specific next steps (re-quote; wait and re-query; check the order's real state; wait ~3s and verify).
