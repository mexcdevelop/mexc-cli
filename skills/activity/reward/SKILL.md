---
name: mexc-activity-reward
scope: activity.reward
signed: yes
identity: yes
visibility: public
description: >-
  Query the logged-in user's unified reward center — reward summary across trial/token/coupon categories, paginated reward detail per category, coupon list, and the reward-center activity-type list. Triggers: "my rewards","reward summary","reward detail","my coupons","reward center","coupon list","reward activity types".
---

> Auth: all four operations are openapi endpoints fronted by the API gateway — they need an openapi API key; the caller is authenticated by that key and their own account is resolved upstream. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# activity · reward

Read a logged-in user's rewards across the unified reward center. Authorization model = resource(scope) × action(access).

## Core Concepts

The subdomain resource is `activity.reward`. The **member is taken from the login session** and language from the request header — neither is a request-body field.

Reward categories (`rewardCategory`, drives the detail tab): `CONTRACT_REWARD` (trial funds / bonus / airdrop position), `ASSET_REWARD` (token), `COUPON_REWARD` (coupon).

Summary response — one summary block per category (trial / token / coupon): totals and counts per reward category.

Reward detail / coupon fields (returned by `reward-detail` / `coupon-list`):

- `userCouponId` (string): user coupon id (coupon rows).
- `couponType` (string): coupon type.
- `displayState` / `usageState` (string): display / usage status — `UNUSED`, `USED`, `EXPIRED` (`IN_USE` may also appear for display).
- `amount` / `remainAmount` (decimal string): face value / remaining.
- `activityName` / `activityType` (string): source activity name / type (i18n).
- `startTime` / `endTime` / `grantTime` (timestamp): validity / claim time.

## Important Notes

- **The account is resolved upstream.** `memberId` and `language` (request header) are not caller inputs — do not send them in `--data`.
- **`rewardCategory` is required** on `reward-detail` and selects which tab of data is returned (one category per call).
- **`usageState`** on `reward-detail` is only meaningful for `CONTRACT_REWARD` / `COUPON_REWARD`.
- **Both time bounds are required, always, on `summary` / `reward-detail` / `coupon-list`.** There is no one-sided window and no implicit default: send `startTime` and `endTime` together, or the call is rejected locally naming the missing one. To get the "last 30 days" view, compute the two timestamps and pass them — an explicit window is also what makes a summary and its drill-downs reconcile, since the response never echoes the window back.
- **Time format**: `startTime` / `endTime` are timestamps **in milliseconds, passed as an unquoted JSON number** (`1767225600000`); a quoted string is rejected by the body schema. Mind the unit: seconds are not caught anywhere — the value is read as millis, lands in 1970, and silently turns the query into a ~56-year scan (16s in practice, against sub-second for a valid window).
- `reward-detail` / `coupon-list` are paginated via `page` / `pageSize`. Both are optional and the backend defaults them to page 1 / size 20; when supplied, `page` must be >= 1 and `pageSize` must be 1–100 (the backend imposes no upper bound of its own, so the cap is enforced locally to stop an unbounded pull).
- **All four operations require a logged-in member and are signed openapi calls** (identity=openapi) — see the Auth note. `reward-activity-type` lists the activity types that have joined the reward center.
- **Gateway prefix**: these paths carry the `/api/activity/openapi` routing prefix (e.g. the full URL is `https://<host>/api/activity/openapi/reward_center/summary`).
- **Locale is header-driven**: the response language comes from the `Language` request header (lowercase `language` also accepted), defaulting to `en-US` when absent. Valid codes include `en-US` / `zh-CN` / `ko-KR` / `ja-JP` (`zh-HK` may fall back to English). The CLI cannot set custom headers yet, so mexc calls return the default `en-US`; to check another language today (e.g. for `reward-activity-type`), send the header with curl, e.g. `curl -H "Language: ja-JP" "<full URL>"`.

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc activity reward summary`              | `activity.reward` | read | yes | yes | yes | `POST /api/activity/openapi/reward_center/summary` | User reward summary across categories |
| `mexc activity reward reward-detail`        | `activity.reward` | read | yes | yes | yes | `POST /api/activity/openapi/reward_center/reward_detail` | Paginated reward detail for one category |
| `mexc activity reward coupon-list`          | `activity.reward` | read | yes | yes | yes | `POST /api/activity/openapi/reward_center/coupon_list` | Paginated user coupon list |
| `mexc activity reward reward-activity-type` | `activity.reward` | read | yes | yes | yes | `GET /api/activity/openapi/reward_center/reward_activity_type` | Reward-center activity type list |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| summary / reward-detail / coupon-list / reward-activity-type | `activity.reward` | read |

## Usage Examples

```bash
# Reward summary over an explicit window (both bounds always required)
mexc activity reward summary --data '{"startTime":1767225600000,"endTime":1769904000000}'
# Trial-fund reward detail, unused only, filtered by activity types
mexc activity reward reward-detail --data '{"rewardCategory":"CONTRACT_REWARD","usageState":"UNUSED","activityTypes":["lego","wf"],"page":1,"pageSize":20,"startTime":1767225600000,"endTime":1769904000000}'
# Token reward detail, fuzzy activity name search
mexc activity reward reward-detail --data '{"rewardCategory":"ASSET_REWARD","activityName":"Airdrop","page":1,"pageSize":20,"startTime":1767225600000,"endTime":1769904000000}'
# Coupon reward detail
mexc activity reward reward-detail --data '{"rewardCategory":"COUPON_REWARD","page":1,"pageSize":20,"startTime":1767225600000,"endTime":1769904000000}'
# Unused coupons; or coupons of given types with a name search
mexc activity reward coupon-list --data '{"displayState":"UNUSED","page":1,"pageSize":20,"startTime":1767225600000,"endTime":1769904000000}'
mexc activity reward coupon-list --data '{"couponTypes":["TOKEN","INTEREST"],"activityName":"Airdrop","page":1,"pageSize":20,"startTime":1767225600000,"endTime":1769904000000}'
# Reward-center activity types (no params)
mexc activity reward reward-activity-type
```

## Workflows

### Review the reward summary, then drill into a category

> User: "What rewards do I have, and show my unused coupons"

1. `mexc activity reward summary --data '{"startTime":<window start>,"endTime":<window end>}'` → per-category totals and counts (trial / token / coupon). **Both bounds are required** — pick the window here (for a "last 30 days" view, compute `now-30d` and `now` in ms) and keep it for every later step.
2. Drill into whichever category the summary reports data for, **reusing the exact window from step 1** — a drill-down over a different window will not reconcile with the summary numbers:
   - trial funds → `mexc activity reward reward-detail --data '{"rewardCategory":"CONTRACT_REWARD","page":1,"pageSize":20,"startTime":<from step 1>,"endTime":<from step 1>}'`
   - token → `mexc activity reward reward-detail --data '{"rewardCategory":"ASSET_REWARD","page":1,"pageSize":20,"startTime":<from step 1>,"endTime":<from step 1>}'`
   - coupons → `mexc activity reward coupon-list --data '{"displayState":"UNUSED","page":1,"pageSize":20,"startTime":<from step 1>,"endTime":<from step 1>}'` — the coupon category is served by `coupon-list`, not by `reward-detail` with `COUPON_REWARD`.
3. A category whose summary total / count is zero has no rows: skip its drill-down instead of paging an empty list.

### Filter reward detail by activity type

> User: "Show the unused trial funds granted by one activity"

1. `mexc activity reward reward-activity-type` → the set of legal `activityType` values.
2. `mexc activity reward reward-detail --data '{"rewardCategory":"CONTRACT_REWARD","activityTypes":["<value from step 1>"],"usageState":"UNUSED","page":1,"pageSize":20,"startTime":<window start>,"endTime":<window end>}'`
   - `activityTypes` entries **must come from step 1**; a value outside that set matches nothing.
   - `usageState` is only meaningful for `CONTRACT_REWARD` / `COUPON_REWARD` — omit it when querying `ASSET_REWARD` (token).
   - One call covers one `rewardCategory`; repeat step 2 per category.
   - Restart paging at `page:1` whenever any filter changes, otherwise a carried-over page number skips rows or lands past the end.

### Audit coupons per state, then reconcile with the summary

> User: "How many of my coupons are unused, used and expired"

1. `mexc activity reward coupon-list --data '{"displayState":"UNUSED","page":1,"pageSize":20,"startTime":<window start>,"endTime":<window end>}'` → one state per call, so run it once each for `UNUSED`, `USED` and `EXPIRED` to cover everything. `couponTypes` takes the reward-center list codes (e.g. `TOKEN`, `INTEREST`); that type catalogue is not part of this sub-domain.
2. After a coupon changes state (for example one gets consumed), re-run `coupon-list` **and** `summary` — refreshing only the list leaves the summary counts stale.
3. `mexc activity reward summary --data '{"startTime":<window start>,"endTime":<window end>}'` → re-query to confirm the coupon count matches the states listed. Reuse the **same** window as the `coupon-list` calls, or the two will not add up.

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op.
- Unsure about parameters? run `mexc schema activity.reward.<op>` instead of guessing.
- On a structured `failed_precondition` (e.g. `requires API credentials`) from a signed op, load the `mexc-auth` skill (credentials) and the `mexc-common` skill (common error / safety rules), then replay; this skill does not describe login.

**Skill-specific notes**
- The member and language are gateway/header-driven — never pass `memberId` / `language` in `--data`.
- Every windowed op needs **both** `startTime` and `endTime`; there is no one-sided window to fall back on. On `required body field "endTime" is missing`, add the other bound rather than retrying.
- `summary` / `reward-detail` / `coupon-list` / `reward-activity-type` are all signed openapi calls (identity=openapi). Use `--dry-run` to inspect any request before sending.
