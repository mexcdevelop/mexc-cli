---
name: mexc-activity-participate
scope: activity.participate
signed: yes
identity: yes
visibility: public
description: >-
  Register the logged-in user for an activity. Triggers: "register for activity","join activity","activity sign up","participate in activity".
---

> Auth: `register` is an openapi endpoint fronted by the API gateway and performs a write — it needs an openapi API key; the gateway authenticates the caller, signs the request, and forwards the member. On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill to prepare credentials (`--read-only` / common safety rules → load `mexc-common`), then replay; this skill does not describe login.

# activity · participate

Register the logged-in user for an activity. Authorization model = resource(scope) × action(access).

## Core Concepts

The subdomain resource is `activity.participate`. The **member is taken from the login session**, so the only caller input is `linkFlag` — the activity link identifier to register for.

## Important Notes

- **The account is resolved upstream**, not a body field.
- **`linkFlag` is required** and identifies which activity to register for.
- **Write, non-idempotent**: registering repeatedly is not a safe no-op — do not auto-retry on a lost response.
- **Gateway prefix**: this path carries the `/api/activity/openapi` routing prefix (full URL: `https://<host>/api/activity/openapi/lego/register`).

## Operations

| Command | scope | access | signed | identity | idempotent | Endpoint | Description |
|---------|-------|--------|--------|----------|------------|--------------|-------------|
| `mexc activity participate register` | `activity.participate` | write | yes | yes | no | `POST /api/activity/openapi/lego/register` | Register the logged-in user for an activity |

## Permissions

| Operation | scope | access |
|-----------|-------|--------|
| register | `activity.participate` | write |

## Usage Examples

```bash
# Register for the activity identified by linkFlag
mexc activity participate register --data '{"linkFlag":"<activity-link-flag>"}'
```

## Workflows

### Find a running activity and register for it

> User: "register me for one of the activities that are running now"

1. `mexc activity online list --data '{"limit":15,"activityStatus":"UNDERWAY"}'` → the activities visible to the user. Every record carries `activityLink` (the activity's relative landing path) and `appliedActivity` (whether the user already registered). For a generic landing-page activity `activityLink` looks like `/campaigns/<linkFlag>`, and **its last segment is the `linkFlag` `register` takes**; `register` only accepts activities of that kind.
   - Paging: take the next page's `lastRowNumber` from the response's `nextRowNumber`, falling back to the last record's `rowNumber`; `noMoreData=true` means the last page. A single response may return fewer than `limit` records, so keep requesting with the cursor until you have enough or `noMoreData=true` — a short page is not "no more activities".
2. Check that activity's `appliedActivity` before registering: `true` means it is already registered, so **skip step 3** — `register` is not idempotent, and calling it again is not a safe no-op.
3. `mexc activity participate register --data '{"linkFlag":"<linkFlag from step 1>"}'` → register the logged-in user. `linkFlag` is the only input; the account is resolved upstream, so never pass a member id in `--data`.
   ↓ step 3 is a write: show the user a summary and wait for explicit confirmation before running.
   - Code `211008` means the registration was refused, and `data.mutexRecord.activityType` names the activity type this one is mutually exclusive with. This is not retryable: pick another activity, or settle the conflicting one first.
   - A successful response may carry `data.showNewUserMutexTips=true`, meaning the registration landed but hit the new-user mutual-exclusion rule — relay that to the user.
4. `mexc activity online list --data '{"limit":15,"limitApplied":true}'` → re-query the joined activities to confirm the target appears. `list` is a signed openapi read whose account comes from the API key, so `limitApplied` and `appliedActivity` reflect the current account.

## Notes for Agents

**General rules**
- Never auto-retry this operation (idempotent=no): a lost response could mean the registration already landed.
- Show a summary and wait for explicit user confirmation before running this write.
- Unsure about parameters? run `mexc schema activity.participate.register` instead of guessing.
- This op is a signed openapi write (identity=openapi). On a structured `failed_precondition` (e.g. `requires API credentials`), load the `mexc-auth` skill (credentials) and the `mexc-common` skill (`--read-only` / common error / safety rules), then replay; this skill does not describe login.

**Skill-specific notes**
- The account is resolved upstream — never pass a member id in `--data`.
- `register` is a signed openapi write; use `--dry-run` to inspect the request before sending, and confirm with the user first.
