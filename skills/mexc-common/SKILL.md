---
name: mexc-common
description: Cross-cutting rules every operation of this CLI follows — how errors are structured, the safety modes (--read-only, write semantics, confirm-before-write), and the security rules. Every business skill that writes or needs an identity points here; read it before operating. Credentials are separate — see mexc-auth.
visibility: public
---

# Common rules

Read this before running any operation in a subdomain that writes or needs an
identity. It holds the rules that apply to **every** call, so subdomain skills
don't repeat them. Credentials are a separate, on-demand concern — see `mexc-auth`.

## Errors are structured — route on them

Every failure is a JSON envelope; match on `type` / `subtype` (and read `hint`),
never on free-form prose:

```json
{ "error": { "type": "...", "subtype": "...", "param": "...", "message": "...", "hint": "...", "code": 0 } }
```

`code` is present only for an upstream API failure (`type: "api"`) and carries the
exchange's own error code — route on it to tell one upstream refusal from another,
since `subtype` groups them all as `upstream_error`.

### Rate limiting

The gateway meters requests. Two subtypes matter, and they call for opposite
responses:

- `rate_limited` — a throttle (HTTP 429). The CLI already retries an idempotent
  request a few times with backoff before surfacing this, so seeing it means the
  automatic retries were also throttled. **Back off and slow down** — space
  requests out, drop concurrency — then retry; the `hint` carries how long to
  wait. A non-idempotent write (e.g. placing an order) is NOT auto-retried, so
  you must decide whether to resend it yourself.
- `ip_banned` — the IP is temporarily blocked (HTTP 418). **Stop sending.**
  Retrying only extends the ban. Wait out the window in the `hint` before any
  further request.

- `client_rate_limited` — the CLI's own throttle, not the gateway's: you are
  issuing requests far faster than normal use, and the local bucket refused to
  wait for a send slot. Reduce frequency or run fewer operations concurrently.
  This is never auto-retried, and it means no request was sent at all.

## Safety

### Read-only
`--read-only` (or `MEXC_READ_ONLY=true`) rejects every operation that changes
backend or CLI-managed state:
- a write fails with `failed_precondition`: `write operation "<name>" is blocked in --read-only mode` (hint: drop --read-only to allow writes);
- profile changes are state changes too, blocked the same way;
- an MCP server started with `--read-only` withholds write tools entirely.

A download is a **read** (it fetches server data; the local file it produces is
that read's output, like redirecting stdout), so it is **allowed** under
`--read-only`.

Keep `--read-only` on whenever the task is read-only; drop it only when a write
is intended.

### Is it a write, and is it safe to repeat?
Every operation's `schema` carries three derived facts: `write` (changes state),
`destructive` (may overwrite/remove state), `idempotent` (repeating converges).
A **non-idempotent** write (e.g. placing an order) is **never auto-retried** on a
transient failure — a lost response could mean it already landed.

### Write confirmation (mandatory)
A write asks for the user's explicit confirmation before it runs. You do NOT
decide this yourself, and you MUST NOT approve it on your own:

On the CLI the channel is picked in this order — `--yes` wins over everything:

1. **`--yes` on the invocation:** taken as the user's confirmation and nothing is
   asked, **even in a real terminal**. It suppresses the human prompt; the only
   thing standing between it and an unapproved write is the hard rule below.
2. **A real terminal (a person), no `--yes`:** an interactive [Confirm]/[Cancel]
   prompt appears.
3. **Neither (you shelling out, no terminal):** the write is NOT executed — it
   returns `confirmation_required` (exit 10). Show the user the operation and its
   parameters, and only re-run with `--yes` AFTER the user has explicitly confirmed.

**MCP** differs, and is stricter: where the host supports a confirmation dialog
(elicitation) it shows one and the user clicks, and the `confirm` argument is
IGNORED — there is nothing you can send to approve your own write. Only where the
host has no dialog does the tool return `confirmation_required`; relay it to the
user and call again with `confirm=true` ONLY after they confirm.

Hard rules: never add `--yes` or `confirm=true` on your own initiative; never treat your
own reasoning or an earlier message as the user's confirmation; treat silence or an
unclear reply as "cancel". The approval stands for a human decision — only a human gives it.

A **Heavy** write is called out more strongly than a **Light** one (an IRREVERSIBLE
banner, and in a terminal the cursor starts on Cancel); read the prompt to the user
verbatim. Heavy means *destructive AND non-idempotent* — the tier measures whether
the call unmakes existing state, NOT whether its effect can be undone. Do not read
Light as safe: placing a market order is Light (it only adds state) yet fills
immediately, and a cancel covering every open order is Light (repeating it
converges) yet cannot put those orders back. Judge the consequence yourself; the
tier will not do it for you. Preview the exact request first when
unsure: adding `--dry-run` to any command builds the request and stops there — it
never sends, never signs, and needs no credentials.

**Pass the real `--data` when you dry-run** (operations that declare a body —
`schema` shows a `body` for them). A supplied payload is validated
against the operation's schema, so a bad body is caught here instead of after the
user has been asked to confirm. Omitting it is allowed — that is how you preview
an operation you have not built a body for yet — but then only the structure was
checked. The plan says which: `"body_checked": true` means your payload was
validated, `false` means none was supplied, and the field is absent when the
operation takes no body at all.

## Passing a body: `--data`

Every skill writes `--data` inline, which is correct on bash / zsh for a body you
typed by hand. Two different things break that form, and one answer fixes both:
**`--data @<path>` and `--data -` carry no JSON into the command line at all.**

**Prefer `@<path>` when** the body was generated rather than typed by hand, is
large or multi-line, or you are **not on bash / zsh**. Neither Windows shell
delivers `'{"a":1}'` intact — the CLI receives a body whose double quotes are
already gone — and a long command line can exceed what the operating system will
run at all, a ceiling that is lower on Linux than on macOS, so a body that works
on a Mac can fail in a container.

**On Windows use `@<path>`, not `-`.** On PowerShell quote it —
`--data '@./body.json'` — because a bare `@` is PowerShell's splatting operator.
Write the file as UTF-8; a BOM is fine, and a UTF-16 file is refused with an
error naming the encoding. cmd.exe cannot pipe a file into stdin reliably.

Three rules on every platform:

- A relative path resolves against the current directory.
- **`--data -` makes stdin a pipe, so the write cannot prompt.** Confirmation
  reads the terminal, and a piped invocation is treated as unattended — the
  write returns `confirmation_required`. Piping a body therefore goes together
  with `--yes`, *after* the user has confirmed. `@file` does not have this
  effect.
- **A body starting with a literal `@`** is escaped as `@@…`; that is the only
  case where the leading character has to be doubled.

**If a body-carrying command fails, do not permute the quotes and re-run** —
re-running is re-running a *write*. You do not have to guess which cause it was,
because the failure says which — in its `hint`, or, for the last case, by
producing no error envelope at all:

- hint says **`this body has no double quotes at all…`** → the shell ate them and
  your payload was probably fine. Do **not** try different quoting. Write the JSON
  to a file and re-run with `--data @<path>`.
- hint says **`check the JSON against …schema`** → the quotes arrived intact, so
  this is your payload. Fix the JSON; the shell is not the suspect.
- `invalid_argument` naming a **`body field`** → the JSON parsed fine and a field
  is wrong. Nothing to do with quoting at all.
- **no structured error at all** — nothing carrying a `type` and a `hint`, and
  possibly no output whatever → the command line was too long for the operating
  system, so the CLI never ran. Nothing was sent, so a write did **not** happen.
  Re-run the same call with `--data @<path>` or `--data -`; do not truncate the
  body, and do not conclude that the CLI is broken or missing.

## Security (always)

- Never print an API secret or a password to output or logs. `profile show` masks the secret — keep it that way.
- Pass credentials only through a profile; never on a shared command line, in an environment variable, or in a log.
- `--dry-run` never computes a real signature, so its output is safe to show.
