---
name: mexc-activity-online
scope: activity.online
signed: yes
identity: yes
visibility: public
description: >-
  Browse the online activity center — list user-visible online activities, header banner activities, and activity category tabs. Requires an openapi API key. Triggers: "online activities","activity center list","activity banners","list visible activities","banner activities","activity category tabs".
---

> Auth: these operations are openapi endpoints fronted by the API gateway — they need an openapi API key; the gateway authenticates the caller, signs the request, and forwards the member. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (common error/safety rules → load `mexc-common`), then replay; this skill does not describe login.

# activity · online

Read the online activity center as the logged-in viewer sees it. Authorization model = resource(scope) × action(access).

## Core Concepts

The subdomain resource is `activity.online`. Both operations return online activity entities the current viewer is allowed to see, using row-number cursor pagination.

Common activity fields (returned by the list operations):

- `activityId` (string): activity id.
- `activityName` (string): activity name.
- `activityType` (string): activity type.
- `activityStatus` (string): status enum — `PREHEATING` (not started), `UNDERWAY` (in progress), `SETTLEMENT`, `FINISHED`, `INVALID`.
- `activityLink` (string): the activity's page link.
- `startTime` / `endTime` (timestamp): activity window.
- `rowNumber` (number): the row's cursor value; pass the last row's value as `lastRowNumber` to fetch the next page.

## Important Notes

- **Requires an openapi API key.** The requests are signed and the account comes from that key; `memberId` / `language` / `ip` are resolved upstream and are not caller inputs.
- **Cursor pagination**, not page/offset: omit `lastRowNumber` for the first page, then pass the previous page's last `rowNumber` to advance.
- **`limit` bounds**: `list` allows 1–15, `banner-list` allows 1–10. Out-of-range is rejected locally as a structured `invalid_argument` before the request is sent, so the bound is visible without a round trip.
- `recommendTap` and `activityStatus` are enum filters (see Core Concepts / schema).
- **Gateway prefix**: these paths carry the `/api/activity/openapi` routing prefix (e.g. the full URL is `https://<host>/api/activity/openapi/activity_center_online/category_list`).
- **Locale is header-driven**: the response language comes from the `Language` request header (lowercase `language` also accepted), defaulting to `en-US` when absent. Valid codes include `en-US` / `zh-CN` / `ko-KR` / `ja-JP` (`zh-HK` may fall back to English). The CLI cannot set custom headers yet, so mexc calls return the default `en-US`; to check another language today, send the header with curl, e.g. `curl -H "Language: zh-CN" "<full URL>"`.

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc activity online list`        | `activity.online` | read | yes | yes | yes | `POST /api/activity/openapi/activity_center_online/list` | List user-visible online activities (cursor paginated) |
| `mexc activity online banner-list` | `activity.online` | read | yes | yes | yes | `POST /api/activity/openapi/activity_center_online/banner_list` | List header banner activities (cursor paginated) |
| `mexc activity online category-list` | `activity.online` | read | yes | yes | yes | `GET /api/activity/openapi/activity_center_online/category_list` | List activity category tabs |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| list / banner-list / category-list | `activity.online` | read |

## Usage Examples

```bash
# First page of visible activities (max 15)
mexc activity online list --data '{"limit":15}'
# Next page: pass the previous page's last rowNumber
mexc activity online list --data '{"limit":15,"lastRowNumber":15}'
# Only in-progress activities in a category
mexc activity online list --data '{"limit":10,"recommendTap":"CONTRACT_TAP","activityStatus":"UNDERWAY"}'
# Only the activities the viewer has joined (My Activities)
mexc activity online list --data '{"limit":10,"limitApplied":true}'
# Header banners (max 10)
mexc activity online banner-list --data '{"limit":10}'
# Activity category tabs (no params; language from header)
mexc activity online category-list
```

## Workflows

### Open the activity center: category tabs first, then that tab's list

> User: "show me what's on in the activity center"

1. `mexc activity online category-list` → the category tabs (this op takes no params). Order them by `sortOrder` ascending; the **default category is the one with the lowest `sortOrder`**. If the category the caller asked for is not in the returned list, fall back to that default instead of querying with it.
2. `mexc activity online list --data '{"limit":15,"recommendTap":"<code of the category picked in step 1>","activityStatus":"UNDERWAY"}'` → the first page of activities in that category.
   - Precondition: `recommendTap` comes from a category's `code` in step 1 — do not compose it yourself. A `code` outside this op's `recommendTap` enum is rejected by validation; run `mexc schema activity.online.list` to check the accepted values.
   - Precondition: default `activityStatus` to `UNDERWAY`, and **reset it back to `UNDERWAY` whenever the category changes** — do not carry the previous category's status filter over.
   - Omit `lastRowNumber` on the first page.
3. `mexc activity online banner-list --data '{"limit":10}'` → the header banner activities. It has no dependency on step 2 and can be issued in parallel; it is only needed for the activity center entry view.

### Page through with the cursor / top up a full page

> User: "give me more activities"

1. `mexc activity online list --data '{"limit":15,"recommendTap":"<code>","activityStatus":"UNDERWAY"}'` → the first page, plus the cursor for the next one.
2. `mexc activity online list --data '{"limit":15,"lastRowNumber":<cursor from step 1>,...same filters}'` → the next page; repeat this step until a stop condition hits.
   - Cursor value: **prefer `nextRowNumber` from the response**; when that field is absent, fall back to the last row's `rowNumber`.
   - Stop conditions: `noMoreData` is true, or neither source yields a cursor — that is the end, do not send another request.
   - A single response may return fewer rows than `limit` (the server filters by visibility). To top up a full page, keep requesting with the new cursor, but **cap the number of rounds yourself** (5 rounds in practice) so a narrow filter cannot page forever.
   - `limit` accepts 1–15; out-of-range is rejected.
3. Whenever a filter changes (`recommendTap` / `activityStatus` / `limitApplied`), **drop the cursor and restart without `lastRowNumber`**. Reusing the old cursor resumes from the old position and returns an incomplete result.

### Fill the header banner slots

> User: "what's being promoted at the top of the activity center"

1. `mexc activity online banner-list --data '{"limit":10}'` → the first batch of banner activities plus the next cursor. `limit` accepts 1–10; 10 is this op's maximum.
2. `mexc activity online banner-list --data '{"limit":10,"lastRowNumber":<cursor from step 1>}'` → keep pulling; repeat until 10 rows have accumulated or `noMoreData` is true.
   - Same cursor rule as `list`: prefer `nextRowNumber`, otherwise the last row's `rowNumber`.
   - Cap the rounds the same way (5 in practice), and truncate to 10 rows once full.

## Notes for Agents

**General rules**
- Retry only idempotent ops (idempotent=yes); never auto-retry a non-idempotent op.
- Unsure about parameters? run `mexc schema activity.online.<op>` instead of guessing.
- On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill (credentials) and the `mexc-common` skill (common error / safety rules), then replay; this skill does not describe login.

**Skill-specific notes**
- This is a signed openapi subdomain (identity=openapi) — a saved API-key profile is required.
- `category-list` is a plain GET with no params. `list` / `banner-list` send a JSON request body (`--data`); all three are callable today. Use `--dry-run` to preview a request before sending it.
