# AGENTS.md — The Contract Hub for Writing Skills / Code for AI Agents

> **Required reading before** you write any skill or api wrapper. This document states the **hard contracts** (the boundaries and formats you must follow). For how to add a directory, see [CONTRIBUTING.md](./CONTRIBUTING.md).

## 0. English-only (Read This First, Non-Negotiable)

**Everything written into this repository is English-only — no exceptions.** This applies to Go identifiers, **comments**, string literals, and log messages; commit messages and MR/PR descriptions (Conventional Commits in English); and every skill file and doc. No CJK characters anywhere in code or commit messages.

This is enforced by CI, not left to good intent: `scripts/check-english.py` scans the Go lines a merge request changes and the commit message, and any CJK character turns the pipeline red. The pointer files `CLAUDE.md`, `.cursorrules` / `.cursor/rules/`, and `.kiro/steering/` restate this rule so every AI tool sees it on entry. Write English — do not work around the check.

The **only** CJK allowed in the repo is functional, not prose: the CJK code points a script must name to do its job — the character-class ranges in `check-english.py`, and the two benchmark tokens that `check-boundary.py` forbids (`飞书`, `对标`). These are a tool defining what CJK it acts on, not Chinese written into the repo. Everything else — comments, docstrings, config, docs — is English. <!-- self-contained-ok: this line defines the forbidden-token rule -->

---

## 1. Output Is Written to Be Read by Machines (Highest-Priority Mindset)

This CLI's primary consumer is AI Agents (Claude Code, Cursor, etc.). Your code is read by machines — error messages, output format, and flag design all directly affect an Agent's success rate.

### 1.1 Errors Are Written to Be Parsed by AI

**One rule to internalize: every error message you write will be parsed by an AI to decide its next action.** So errors must be **structured, actionable, and specific**.

Errors are emitted as a JSON envelope (language-agnostic — it constrains "what the binary emits", not how it's implemented). Four fields:

| Field | Meaning | Who fills it |
|------|------|------|
| `type` | Top category: `validation` / `network` / `internal` / `api` | Required |
| `subtype` | Sub-category, for precise Agent routing: e.g. `invalid_argument`, `failed_precondition`, `network_transport`, `file_io` | Required |
| `param` | Points **only** to the specific user input that actually failed (e.g. `--symbol`); omit it when there is no concrete input | Optional |
| `hint` | Recovery guidance for the Agent (plain language, telling it how to fix the next step) | Recommended |

Example (malformed request structure):

```json
{
  "error": {
    "type": "validation",
    "subtype": "invalid_argument",
    "param": "--symbol",
    "message": "symbol \"BTC\" is not a valid trading pair",
    "hint": "use a full pair like BTCUSDT; list valid pairs with `mexc spot marketv2 symbols`"
  }
}
```

Anti-patterns (do not do this):

- ❌ A bare string error / only a single `message` — the Agent cannot route on it and is left guessing.
- ❌ Stuffing recovery guidance into `message` — `hint` is its own field, which is easier for a machine to read.
- ❌ Filling `param` with a field the caller never passed, or with a system-side guess — `param` names only the specific user input that actually failed.

### 1.2 stdout Is Data, stderr Is Everything Else

- **Program output (the JSON envelope) → stdout.**
- **Progress, warnings, prompts → stderr.**

Mixing them pollutes the pipe chain (an Agent that parses stderr progress text as data will break).

---

## 2. Agent Invocation Protocol

```bash
mexc <domain> <sub> <operation> [flags]    # Call a wrapped operation
mexc schema <domain>.<sub>.<operation>     # Inspect an operation's parameter structure (JSON) before calling
mexc schema                                # List every inspectable operation id
mexc <domain> --help                       # Explore what capabilities a domain offers
```

Every capability reachable from this binary is a wrapped operation. There is no
raw-endpoint escape hatch: an endpoint that has no wrapper cannot be called.

- **Run `schema` before a call when unsure of the parameters**: `mexc schema spot.kline.query` emits the operation's method / path / access / whether it is `signed` (a boolean — the scheme name stays internal) / its `identity` (`none` or `openapi`) / params (each with its `in` location) / for a body operation a `body` section listing every `--data` field (name, type, required, enum, nested fields) / for a multipart operation a `multipart` section listing the `--file` fields and any `--data` text fields — all as JSON, so do not guess the field format of `--data` / `--file`. Run `mexc schema` with no argument to list every inspectable id. `schema` is a pure local lookup over the declared Operations (no network), and reads from the same registry that mounts the commands, so the two never drift.
- **Credentials**: a signed operation needs a saved profile. `mexc profile set <name>` takes **no credential flags** and prompts on a terminal, so a secret cannot be passed on a command line and cannot be recorded by one — an agent that hits this should relay the command to the user rather than look for another channel, because there is none. (This is not a barrier against a process that already holds the secret and allocates its own pty; see editions/public/command/tty.go.) Once saved, everything else is non-interactive — select with `mexc profile use <name>` or per-call with `--profile <name>`. There is no credential environment variable. Profiles are encrypted at rest under a key in the OS keyring; where no keyring is usable the CLI falls back to a local key file and says so, and `mexc profile keychain-downgrade --yes` pins that choice deliberately. Both keys are tried when reading, so changing which one is in front never orphans a stored credential. See §2.1.

### 2.1 Credentials & Security

Credentials come **solely from a saved profile** — there is no credential environment variable. This is a local, human-operated tool, so credentials live in a named profile under `~/.mexc/profiles/<name>`: the one chosen by `--profile <name>`, else the active profile (`mexc profile use <name>`). (`MEXC_READ_ONLY` remains environment-driven — it is configuration, not a credential.)

This product serves ONE endpoint, so it has no env axis and no `config` command: `--env`, `MEXC_ENV` and `config set env` are not registered, because a selector whose only legal value is the one already in effect is a question with one answer. `framework.assemble` derives that from the host table — registering a second env row brings all three back. To point the CLI elsewhere while developing, use `--base-url` for one invocation or `MEXC_OPENAPI_BASE_URL` to persist it; those override host resolution outright, which is the right tool for "go here instead" as distinct from "pick a deployment".

Resolution: an explicitly named `--profile` that cannot be loaded is a hard error. A broken **active** profile is not fatal — it degrades to "no credentials", so credential-free public commands still run and only a signed command then fails with the normal "requires credentials" error.

Security stance for profiles:

- A profile stores the api key and secret **in plaintext**, protected only by `0600` file permissions inside a `0700` directory. This is a deliberate trade-off for multi-account convenience on a local machine.
- `mexc profile show` **masks the secret** (a short secret is fully hidden; only a long one shows its last 4 chars) so it never reaches stdout / the agent context. Credentials never enter logs, stdout, or `--dry-run` output (a dry-run never computes a real signature).
- Loading a profile whose **file** (more permissive than `0600`) **or containing directory** (more permissive than `0700`) is over-permissive is **refused** with a structured error — a wider mode means the secret may be group/world readable, defeating the only protection plaintext has.
- Writes never follow a symlink at the profile path, so a planted link cannot redirect the plaintext secret elsewhere.
- `profile set` / `use` / `remove` change on-disk state and are blocked under `--read-only`.
- Do not commit `~/.mexc`, and be aware that disk backups / sync (iCloud, Time Machine, dotfile repos) will capture plaintext profiles.
- Profiles are sealed under a key held by the **OS keyring**, or by a local file (`~/.mexc/profile-key.key`, 0600) that `Seal` creates where no keyring is usable. **Both are tried when reading**, so changing which one is in front never orphans a stored credential.
- **A read cannot tell "the keyring is denying you" from "the keyring is empty"** — the OS reports both the same way — so three things are easy to get backwards and all three were: `profile keychain-downgrade` **copies an existing key and never invents one** (inventing it on a false "empty" strands the real key, so it refuses instead); **saving a credential again never recovers an unreadable one** (it seals a second key and leaves the first behind); and **deleting the key file can be unrecoverable** (where the CLI created it by itself it is the only copy, and `profile show` masks the secret so nothing can be read back).

---

## 3. The Contract for Writing an api Wrapper (Thin Wrapper)

The `api/` layer is a **thin business wrapper** — it wraps one internal endpoint operation into a single command:

- **One operation, one file** (e.g. `tickers`, `depth`, `klines`).
- Responsible only for: **parameter validation / mapping parameters to the internal endpoint / shaping the output**.
- **Never sends HTTP itself** — sending requests, auth, signing, retries, timeouts, and error classification are all delegated to the shared `pkg/` base layer.
- **Command, filename, and (scope, access) are isomorphic**: `mexc spot trade place` ↔ `api/.../place` ↔ resource `spot.trade` + action `write`.

Landing checklist (per operation added):

1. In the sub-domain `SKILL.md`, append the operation's description + one operation-table row + one permission-table row (including access).
2. Add the operation's wrapper file under `api/`, calling the `pkg/` base layer.
3. Keep the sub-domain's frontmatter `visibility: public` — every sub-domain in this repository is, and Invariant 8 rejects anything else. Visibility is a sub-domain fact, not a per-operation one.

### 3.1 The Operation Declaration Contract

An `api.Operation` is the single source of truth its faces derive from. The
rules below are machine-enforced (L1/L2 contract tests, assembly-time
validation, and `check-boundary.py` Invariant 6 in CI):

**Closed enums — constants only, never string literals.** `Signed`, `Identity`,
and `Param.In` are closed enums whose values are defined only in `pkg/api` by
the CLI platform (param locations, plus the `none` value of each auth axis) or by this product (its schemes in `signing/`, its identities in `identity/`). Reference them as `signing.*` / `identity.*` / `api.In*`
constants; a quoted string on these fields turns CI red (Invariant 6a), and an
unknown constant name is caught in CI (6b) and refused at assembly and send
time (fail-closed). Need a new signing algorithm or identity? Ask the CLI team
to define it — do not invent a value.

**Subdomain defaults + tri-state inheritance.** Each api package declares its
defaults once — `const Signed = api.Sign<X>` and `const Identity =
api.Identity<Y>` — and the registry wires them in. On an operation:

- omit the field (zero value) → inherit the subdomain default — **the normal
  case**; do not restate defaults per op;
- a concrete value → override for this op only;
- `api.SignNone` on an op in a signing subdomain is a legal opt-out but a
  smell: it usually means the operation belongs in its own subdomain. Two
  different non-none schemes inside one subdomain almost certainly mean the
  subdomain is cut wrong.

**Params: one name, two uses.** `Param.Name` is the wire name — the exact
query key / header name / form field the backend expects — and is used
verbatim as the CLI flag `--<name>`. There is no second name field. A name may
not collide with the reserved framework flags (`base-url`, `read-only`,
`dry-run`, `no-retry`, `profile`, `env`, `help`, `yes`, `data`, `file`). (`version` is
not reserved: cobra's `--version` is root-only, so a leaf op may use it.)

**Sensitive values.** A write is confirmed before it runs, and the prompt shows
the request's params/body so the user can verify the exact action. If a rare
param or body field carries a secret (e.g. a token), mask its value in the
prompt: set `Sensitive: true` on the `Param`, or tag the body field
`sensitive:"true"`. The default shows the value (so the user can verify it);
credentials and signing never appear in the prompt — they are framework-resolved
at send time, not params.

**An operation whose REACH depends on which params were supplied carries that in
its `Summary`.** The confirmation prompt is titled with the Summary and then
lists the supplied params, and nothing else in the declaration speaks at prompt
time — so an operation with several modes has to enumerate them there, widest
first. Be aware of what the prompt still cannot say: the modes are separated only
by ABSENCE, and the widest one is normally selected by supplying the least (`spot
trade cancel` with no params at all cancels every open order in the account), so
the most dangerous call renders the fewest lines, and a param lost on a retry
widens the reach with nothing in the prompt to mark it. Where that gap matters,
warn the agent in the sub-domain's `SKILL.md` — it is read before the call, which
is the only moment left.

**Pre-validation constraints.** Declare per-field bounds and the framework
enforces them uniformly (both faces, before the request is sent), like Enum:
- **params**: `Param{... MinVal / MaxVal, MinLen / MaxLen, Pattern}`.
- **body fields**: the `minval` / `maxval` / `minlen` / `maxlen` / `pattern`
  struct tags.

`minval`/`maxval` are inclusive numeric bounds (integer/number fields);
`minlen`/`maxlen` bound a string's character length; `pattern` is a regex the
whole value must match. A numeric tag on a string field (or vice-versa),
`min > max`, or an uncompilable pattern fails `ValidateDecl` at load time. Zero
value = no limit, so declare only what you need.

**Param locations.** `In` declares where a param travels: omit for query (the
common case; `api.InQuery` is its explicit spelling), or `api.InPath` /
`InHeader` / `InCookie` / `InForm`. A file upload is not a param location: it is
declared separately via `Multipart` (mutually exclusive with `Body` and with
`In: form` params), with each file supplied on the CLI via a repeatable
`--file <field>=<path>` (a bare `--file <path>` when the op declares one file
field), and any companion text fields declared with the `Fields` prototype and
supplied through `--data`. Auth and environment headers are the framework's job
(identity and signing inject them) — a per-op header param is only for a genuine
per-call header such as an idempotency key.

**Path placeholders.** A path param binds to a `{name}` placeholder in `Path`
(e.g. `Path: "/wallets/{id}"` + `Param{Name: "id", In: api.InPath, Required:
true}`). Placeholders and `In: path` params must match one-to-one, path params
must be `Required: true`, and `{name}` is the only allowed syntax.

**Request bodies.** A JSON body is declared with `Body` — a prototype the
framework reflects into the schema and validates the payload against. It is the
single body mechanism (there is no raw passthrough). A body is accepted on POST,
PUT, PATCH and DELETE — DELETE is included because some endpoints take a
batch/selector body (delete N records in one call), and the client sends it
unchanged; `ValidateDecl` rejects a body on any other method. For object bodies,
author the struct with tags: `json:"<wire-name>"` names the field,
`validate:"required"` marks it required (its absence means optional),
`enum:"A,B"` constrains the values, and `desc:"..."` is the help text; nested
structs and slices of structs describe nested JSON. For scalar-array bodies,
declare the prototype as a slice of the scalar type (for example `[]int64{}`).
On the CLI the whole body is supplied through one `--data` flag (`data` is a
reserved flag name); its shape is inspectable via `mexc schema`, and `--data`
is validated against the prototype (well-formed JSON, no unknown fields,
required + enum) before the call. Never put a signing-algorithm name in a tag.

**Object body or array body.** The prototype declares which: a struct
(`Body: PlaceOrder{}`) means the body is one JSON object; a slice of structs
(`Body: []PlaceOrder{}`) means it is a JSON array of them, and `--data` takes
`[{...},{...}]`; a slice of scalars (`Body: []int64{}`) means it is a raw JSON
array like `[123,456]`. `--data` is the wire body verbatim in every shape —
there is no wrapper object. Struct tags are authored on the item struct and
every rule (required, enum, `minval`/`maxlen`/`pattern`, `sensitive`) is enforced
on each item, with the failing item's index in the error (`item 1: required body
field "symbol" is missing`). A scalar array has no field tags but still enforces
JSON shape, element type, and non-empty arrays; an empty array is refused, as an
empty body is. `mexc schema` reports `body.shape` (`object` / `array`) and, for
arrays, `body.item_type`; the MCP face
publishes an array body as `{"type":"array","items":{…}}`. Anything that is
neither shape — a map, a bare string, a slice of maps — is rejected by
`ValidateDecl`.

**A body may be combined with any signing scheme.** How much of a request a
signature covers is the scheme's own business, decided inside its `Sign`; no
layer above it judges the pairing — `cli-hmac` signs the query and the body.
Declare the operation in its final shape regardless: a scheme covering less
would still need no change to the declaration. Be aware of what a query-only
scheme would mean at the far side: a backend that verifies over query+body
would reject such a request, which is the correct place for that answer. Ops
fronted by a gateway that authenticates by other means are unaffected.

**What assembly refuses.** Every param location
(`query`/`path`/`header`/`cookie`/`form`) and every body shape (a JSON `Body`,
or a `Multipart` upload) executes: `BuildSpec` assembles the full request and
`internal/transport` sends it. One declaration error is caught while the command
tree is built — `internal/cli/mount.go`'s `validateRegistry`, called from
`root.go` — because it depends on the **effective** host key, which only the
edition's wiring resolves. A failure here stops the binary rather than mounting
a command that cannot work:

- an operation whose effective host key is empty. Every subdomain wires a
  concrete default; an op-level override is allowed, leaving it empty is not.

`--dry-run` does not bypass it: it runs before the command exists.

What a dry-run does skip is drawn on one line: **it checks what it was given and
never demands what it was not.** Params are validated as always. A `--data`
payload is validated too — same required / enum / unknown-field rules a real call
applies — because none of that needs the network, and a preview that waved a bad
payload through would go green for a call already known to be rejectable. What is
exempt is only what a preview genuinely cannot do: resolving credentials and
computing a real signature. An absent `--data` is therefore not an error, so
`<op> --dry-run` still answers "where would this go?" before a caller has built
anything; the plan reports `body_checked: false` when that is what happened, and
`true` when the payload was validated. (`--read-only` is a safety axis, not a
declaration check.)

---

## 4. The Contract for Writing a SKILL.md

Every sub-domain directory must contain a `SKILL.md` — it is the **invocation guide written for the Agent**.

### 4.1 Required Frontmatter

```yaml
---
name: mexc-<domain>-<sub>     # e.g. mexc-spot-market (namespaced install id)
scope: <domain>.<sub>         # resource identifier only, e.g. spot.kline (no action suffix)
signed: yes | no              # yes iff the sub-domain's default scheme (const Signed) is not none
identity: yes | no            # yes iff it carries an identity (const Identity != none); which one is in the const
visibility: public | internal
description: <one line stating what this sub-domain does and when to use it>
---
```

Use English throughout (the machine fields `name` / `scope` / `signed` / `identity` / `visibility` and the body are all English).

The classification axes are five orthogonal dimensions (see [SUBDOMAINS.md](./SUBDOMAINS.md) for details):
- `scope`: the resource identifier `<domain>.<sub>`, **with no action**.
- `access`: the action (`read` / `write` / future `admin`, etc.), living in a column of the permission/operation table, not in frontmatter.
- `signed`: the sub-domain's **default** signing stance, a yes/no fact — `yes` iff its `const Signed` is not `SignNone`. The concrete scheme lives in code; an operation may override it (see §3.1), and the per-op effective scheme is inspectable via `schema`.
- `identity`: a yes/no fact — `yes` iff the sub-domain carries a login identity (`const Identity` is not `none`). Which identity is in `const Identity`, inspectable via `schema`.
- `visibility`: a sub-domain-level tag; every sub-domain in this repository is `public`, and Invariant 8 rejects any other value. Nothing is filtered at build time — a product mounts a sub-domain by listing it in its `domains.go` — so an operation has no visibility of its own.

Authorization = **resource(scope) × action(access)**; whether something is a write operation is read from `access` (`access != 'read'`), not parsed from scope and not stored as a separate `write` field.

### 4.2 Permission Table Required (CI-Enforced)

Every SKILL.md **must** carry a permission table whose columns include **scope and access**: scope must equal this sub-domain's resource `<domain>.<sub>` (matching its registration in [SUBDOMAINS.md](./SUBDOMAINS.md)), and the action goes in the `access` column. This is enforced as a CI gate.

```markdown
| operation | scope | access |
|------|-------|--------|
| place / cancel | `spot.trade` | write |
| get / open     | `spot.trade` | read  |
```

### 4.3 Recommended Skeleton (Advanced via Review, Not Mandatory)

As operations grow, organize the body in this order — it reads most smoothly for an Agent:

```
Core Concepts           # Core entities + ID formats (e.g. symbol looks like BTCUSDT)
→ Resource Relationships # ASCII tree of entity relationships
→ Important Notes        # Pitfalls / default behaviors / identity differences
→ Operation table
→ Permission table (required, see 4.2)
```

A placeholder skill need not force-fill the skeleton — this avoids writing a "placeholder for a placeholder". Fill it in when real operations are wired up.

---

## 5. Boundary Red Lines (Enforced by Machine, Not by Self-Discipline)

- **What this product mounts is decided by composition, not by a runtime filter.** An edition declares its own topology; a capability that is not in `editions/public/domains.go` is absent from the binary's import graph entirely — no command, no `schema` entry, no MCP tool.
- **This tree is build-tag free** (Invariant 11). Code selection is by the import graph. `//go:embed` is the one thing that ignores the import graph, so `embed.go` is asserted against the skills on disk (Invariant 9) — an embedded file is a deliberate, listed choice.
- **An edition must never import `internal/`** (Invariant 10). It composes against the `pkg/` ports and the `framework/` door, so the machinery stays replaceable and module-private.
- **Nothing here may name a system that is not published with it.** Every file this repository tracks ships — tests, scripts and docs included. The rule splits by what a check has to KNOW. Rejecting a **name** needs the list of names, and carrying that list here would publish exactly what the rule protects, so that half stays a **release-flow step**: a scanner held outside this tree runs over a checkout of it before publishing anywhere new. Rejecting a **shape** needs no list — a service-name prefix, a host that is not on the public allowlist, a query or a column — so that half runs here, as `scripts/check-exposure.py` (Invariant 12). Neither half attests to the other, and a green `check-boundary.py` attests to neither.

`scripts/check-boundary.py` enforces these (language-agnostic, depending only on Python3; run both locally and in CI, and any violation goes red). **Numbers 1–20**: all twenty run in this repository, but not all from one script — **19 in this one**, and **12 in `scripts/check-exposure.py`**, which reads prose rather than taxonomy and answers to its own tiers:

```
python3 scripts/check-boundary.py    # exit 0 = pass, 1 = violations
```

1. Every sub-domain directory (`skills/<domain>/<sub>/`) must have a `SKILL.md`.
2. Every sub-domain must be registered in the `SUBDOMAINS.md` registry.
3. Every `SKILL.md`'s frontmatter must be complete and well-formed: `name`=`mexc-<domain>-<sub>` (namespaced install id), `scope`=`<domain>.<sub>` (resource only, no action suffix), `signed`∈{yes,no}, `identity`∈{yes,no}, `visibility`∈{public,internal}, `description` non-empty.
4. Every `SKILL.md` must have a permission table; every scope in it must equal this sub-domain's resource `<domain>.<sub>`, and the table must include an `access` column whose values are valid actions (`read`/`write`/`admin`/`cancel-only`/`history-only`).
5. Operational docs must stay self-contained and must not reference an external project as a benchmark — the reader can't open that benchmark, so "benchmarked against XX" says nothing (a violation if any forbidden token — `lark`, `larksuite`, or the two CJK benchmark words listed in `check-boundary.py` — appears). A line that defines the rule itself may be allowed through by appending `<!-- self-contained-ok -->` at end of line.
6. Closed-enum discipline in api declarations (the CI half of §3.1, kept in Python so it runs anywhere the other checks do): no string literal on `Signed:`/`Identity:`/`In:` fields (6a); every referenced constant must exist — the legal sets are extracted from the three files that declare them (`pkg/api/operation.go` for the param locations and the `none` value of each auth axis, `signing/signing.go` for this product's schemes, `identity/identity.go` for its identities), never hand-copied (6b); every sub-domain api package declares `const Signed` / `const Identity` and the frontmatter mirrors them as yes/no facts — `signed: yes` iff the scheme isn't none, `identity: yes` iff it carries an identity (`const Identity` != none) (6c).
7. No signing-algorithm name in a user-facing `SKILL.md` (`hmac`/`sha256`/`ed25519`/…): the signing scheme is chosen in `pkg/api` (Go) and applied by the framework; a skill states only whether an op is signed (`signed: yes/no`), never how.
8. Every sub-domain must declare `visibility: public`. This repository builds one product and publishes its whole tree, so anything marked otherwise is in the wrong place. Asserted while the embed manifest is checked.
9. `embed.go` embeds **exactly** the skill files on disk. "Skill files" means the whole shippable surface — a `SKILL.md` plus its `references/` `scripts/` `assets/` subtrees — and **no `*.go` may be embedded**, so a widened directive can never ship a skill's api source. The reverse direction (a manifest line naming a missing file) is already fatal at compile time, so it is the compiler's job.
10. **Editions compose against ports only**: no `editions/*` source imports ANY `internal/` package — an edition uses `framework/` + the `pkg/` ports (the transport via `pkg/transport.Sender`), never module-private machinery. `.golangci.yml`'s depguard asserts the same rule independently.
11. **No build tags**: no `.go` file carries an `internal` / `!internal` build constraint. Code selection is by the import graph.
12. **Nothing names the service behind an endpoint** — `scripts/check-exposure.py`, by shape rather than by a list (a list here would publish what it guards; that half stays on the release side, see the bullet above). Three tiers, drawn by sensitivity rather than tidiness, because `skills/**/api` belongs to the team that wrote it. **HARD, no excuse**: an internal service name, a host that is not on the public allowlist, a query or column from upstream storage — none of the three is ever needed to describe a request a caller makes. **SOFT**: a market-segmentation label, gateway-injection wording, account grading or feature gating, an issue key — blocking too, but a line that must carry the shape to state a rule is accepted by ending it with `exposure-ok`, the idiom Invariant 5 already uses. **Advisory, never failing**: an upstream type name, the upstream language or its annotations, a shipped open question, a pointer to a document the published tree does not carry — review material, and not a reason to rewrite another team's file. Go is read in comments and `desc:` tags, markdown outside fenced blocks, everything else whole; tests are in scope because they ship.
13. **One producer for the state directory**: only `pkg/config/statedir.go` computes where on-disk state lives — it joins `$HOME` with the name the composition root set from `Edition.StateDir`. Every other consumer (settings, profiles, skill state, any identity's session store) asks it via `config.StateDir()` / `config.StatePath()`. Any other file that reads a per-user directory from the OS (`os.UserHomeDir` / `UserConfigDir` / `UserCacheDir`, or `os.Getenv("HOME")`) is a violation: a second computation does not fail, it silently disagrees, and half the product's state lands in a directory it does not own. A line that legitimately needs its own path is exempted by marking it `state-dir-ok`.

14. **No operation declares `Permission`**: it names an RBAC bucket and the codes an operation needs, and evaluating them requires credentials that carry a grant map (`credential.GrantCarrier`). This product's only identity is an api key, whose permissions live on the exchange side and are not readable from the credential, so nothing here could check them. The field stays in `pkg/api` for an identity that does carry grants; here it is inert, and a declaration that no layer reads misleads whoever reads the source. `validateRegistry` refuses it at assembly too — this rule catches it earlier, in CI.
15. **A frontmatter `description` holding a colon must be quoted** (or written as a `>-` block scalar). Unquoted it is invalid YAML — a plain scalar cannot hold a colon-space — and `npx skills` then fails to parse the frontmatter and drops the whole skill without reporting anything. Checked alongside Invariant 3, since both are facts about the same frontmatter block.
16. **`pkg/` names no product's host key, signing scheme or identity.** Those are the product's vocabulary — declared in `signing/`, `identity/` or the product's own adapter package, and injected at composition time; `pkg/` holds the port and the orchestration. `pkg/api` already states the rule in prose above `Identity` ("The concrete values are deliberately NOT catalogued here"); this is that rule enforced, across host keys and schemes too. The check is name-shaped — it allows `SignNone` and `IdentityNone`, the two values no product owns.
17. **No user-facing string hard-codes the binary name.** Every product built on this module ships its own binary and compiles in the same shared layer, so a literal command name in a hint is wrong in every product that is not the one it names — and wrong again for any renamed or symlinked invocation. The command tree already follows `argv[0]`; the prose must follow the same source, `cliname.Name()`, which `framework.Run` fills from `argv[0]`. Both Go literal forms are checked — interpreted and raw — and `skills/` is in scope alongside the engine, since a subdomain's `Summary` / `Desc` is as user-facing as a hint. Skill names (`mexc-common`, `mexc-spot-market`, …) are installation identifiers, not commands, and are deliberately not matched. A line whose preceding comment carries `binary-name-ok` is excused — for a name that merely looks like the binary but has its own lifetime, such as an artifact repository.
18. **No `SKILL.md` names the retired binary `mexc-cli`.** 17 covers Go and misses the file a business author actually writes: a skill's operation table and its recipes are literal commands, and an agent runs what the skill prints, so a retired name is a command that does not exist. Every occurrence fails except the module path (`mexc-cli-customer`), which is a package identity rather than a command; a line may be excused deliberately with `binary-name-ok`.
19. **Every signed sub-domain uses `signing.CliHMAC`.** No `.go` under `skills/` or `editions/` may assign the raw `"spot-hmac"` literal to a `Signed:` field. The constant it replaced is gone, so naming it is already a compile error and the string literal is the only form the Go compiler still accepts. 6a rejects the same line as an enum-field literal; this rule is the one that names the scheme, so its message says what to use instead.
20. **Every sub-domain points at the shared skills it relies on.** An agent loads ONE sub-domain skill and operates from it, so a missing pointer is a rule it never meets. `mexc-common` — the error envelope, `--read-only`, and above all that a write needs the USER's confirmation and is never self-approved — is required when the sub-domain declares a write **or** carries an identity. `mexc-auth` — the credential flow — is required when it carries an identity, since no sub-domain skill describes login. Both triggers read a source something else already pins: the frontmatter to `const Identity` via 6c, and the write to `Access: api.Write` in the api package. The write fact deliberately does **not** come from the permission table — nothing reconciles that table's `access` column with the code (the Go contract tests reconcile the *operations* table, a different one), so a `write` mistyped as `read` there would switch the rule off for the sub-domain that needs it most. The pointers themselves are prose, the Auth blockquote `skill-template/SKILL.md` ships. A public sub-domain with no identity and no write is exempt from both.

---

## 6. Pre-Commit Self-Check

CI (`.github/workflows/ci.yml`) runs the four repository checks, gofmt, build, vet,
the Go and npm-kit tests and golangci-lint on every push to `main` and every pull
request. Install the pre-commit hook the first time you work in this repo, with
`bash scripts/install-hooks.sh` (runs check-boundary + check-exposure + check-english +
gofmt / build / vet / test + golangci-lint on every commit), so a failure shows up
before CI. Then run the full set below before sending a change.

**Packaging**: `bash scripts/build.sh` compiles the host binary with version metadata
stamped from git. `bash scripts/build.sh --snapshot` requires `--version <prerelease>`;
it creates tagless test tarballs from the current worktree
and refuses a stable version. `bash scripts/build.sh --release`
requires a SemVer-tagged HEAD and derives the formal package version from that
tag. Both modes accept files generated in the worktree before the build and
cross-compile one binary per platform the product declares in `npm/product*.json`
(a subset of what `npmkit/platforms.json` can build; today macOS, Linux and
Windows, arm64 and x64) plus one npm tarball each and a main tarball, under
`dist/npm/`; `--product scoped|unscoped|all` (default `all`) picks which package
variant to build. They do not publish or change dist-tags.

**Publication** happens only in `.github/workflows/release.yml`, when a
`vMAJOR.MINOR.PATCH` tag is pushed: it builds `@mexc-ai/cli` with
`build.sh --release --product scoped`, publishes it through npm trusted publishing
(no token is stored anywhere) and creates the GitHub Release from the version's
`CHANGELOG.md` section. Prerelease tags are not released. **No registry is named in
the packages** — not a flag, not an environment variable, not a field in a
generated file: `npm publish` reads the address from the publishing environment's
own npm configuration, and the launcher resolves it per run on the user's side.

1. `python3 scripts/check-boundary.py` and `python3 scripts/check-exposure.py` pass (exit 0). The second also prints advisory notes, which do not affect its exit code.
2. `git ls-files '*.go' ':(exclude)vendor/' | xargs gofmt -l` is empty, `go build ./...` and `go vet ./...` are clean.
   Ask git for the files, not the filesystem: `gofmt -l .` walks into `vendor/`,
   where some files arrive unformatted from upstream and `go mod vendor` would
   undo any fix — so that form can never be empty and reports a failure nobody
   should act on. The pre-commit hook has always used the git form; this line
   used to disagree with it. (No count is given on purpose: it changes with every
   dependency bump.)
3. Behavior changes carry tests; `go test ./...` passes.
4. `golangci-lint run ./...` is clean (layering rules included; see .golangci.yml).
   Install the version in `.golangci-version`: the linter set changes between
   releases, so a different one can report findings this tree was never written
   against. It runs analyzers `go vet` does not, which is why item 2 does not
   cover it. The config reports every finding rather than the default first three
   per kind — a truncated report makes "clean" mean less than it says.
5. dry-run to verify the request — do not call the real API, only assert that method / path / params are correct. Pass the `--data` you intend to send: a supplied payload is validated, and the plan's `body_checked` says whether it was.
6. Conventional Commits (English): `feat:` / `fix:` / `docs:` / `refactor:` / `chore:` / `ci:`.
