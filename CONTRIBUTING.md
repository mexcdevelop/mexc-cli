# Contributing guide: how to add a domain / subdomain / operation

> The subdomain dictionary is in [SUBDOMAINS.md](./SUBDOMAINS.md); the hard contract is in [AGENTS.md](./AGENTS.md).

## Adding a capability, in 5 steps

For your first contribution, follow this path; each step links to the detailed section below.

1. **Read the contract**: start with [AGENTS.md](./AGENTS.md) — error format, directory rules, SKILL.md requirements.
2. **Generate the scaffold**: `bash scripts/new-subdomain.sh <domain> <subdomain>` auto-creates a `SKILL.md` + `api/` that already passes the check. Whether you should open a new subdomain is covered in "Subdomain classification criteria" below.
3. **Fill in the capability**: write the operation table + permission table in `SKILL.md`, and write the thin wrapper in `api/` (only parameter validation/mapping/output shaping — never issue HTTP itself).
4. **Register + self-check**: register one line for the new subdomain in [SUBDOMAINS.md](./SUBDOMAINS.md); running `python3 scripts/check-boundary.py` must exit 0.
5. **Open a pull request**: run the checks in [AGENTS.md §6](./AGENTS.md) first. CI runs them again as a hard gate.

---

## The three-layer mental model

```
domain      spot / futures / activity ...
 └ subdomain    market / account / trade ...
    └ operation  ticker-price / place / cancel ...
```

The command is isomorphic: `mexc <domain> <subdomain> <operation>`. The directory is isomorphic: `skills/<domain>/<subdomain>/` (single level, no common/biz).

## Adding an operation (most common)

1. Append the operation to the subdomain's `SKILL.md` operation table: `scope` is always the resource `<domain>.<subdomain>`, and the action goes in the `access` column (read/write).
2. Add the `api.Operation` declaration under that subdomain's `api/` (a thin wrapper: it does not issue HTTP itself; the framework does). Follow the declaration contract in [AGENTS.md §3.1](./AGENTS.md): omit `Signed`/`Identity` to inherit the subdomain defaults, use only `api.*` enum constants (never string literals), and bind path params to `{name}` placeholders.
3. Append a row to the permission table (operation → scope → access).

## Adding a subdomain

The recommended way is to generate the scaffold in one shot (with a filled-in `SKILL.md` + `api/<sub>.go` + `api/contract_test.go`, passing check-boundary directly):

```
bash scripts/new-subdomain.sh <domain> <subdomain>   # e.g. bash scripts/new-subdomain.sh spot market
```

The script **does not edit [SUBDOMAINS.md](./SUBDOMAINS.md) for you** (registration is a deliberately manual action); after generating, just register one line as prompted. Doing it by hand also works, in these steps:

1. **Register in [SUBDOMAINS.md](./SUBDOMAINS.md) first** (name, meaning, scope, access, visibility, signed, identity).
2. Then create the directory: `skills/<domain>/<subdomain>/`, with a `SKILL.md` (template at [`skill-template/SKILL.md`](./skill-template/SKILL.md)).
3. Subdomain name: singular, lowercase, hyphenated for multiple words, no domain prefix.
4. Keep a domain's subdomains to 3–6.
5. **Mind the MCP token budget.** Every operation becomes an MCP tool whose name, description, and parameter schema are injected into the agent's context on every session — a recurring token cost. There is a governance gate in `internal/cli` (`TestMCPToolTokenBudget`) capping the whole tool set and each individual tool; a subdomain that balloons the set fails the build. Keep summaries tight and parameter help terse. To see the current usage, run `go test ./internal/cli/ -run TestMCPToolTokenBudget -v` — it logs total tokens and remaining budget. If real growth justifies it, raise the budget deliberately in that test rather than trimming meaning.

### Subdomain classification criteria (open a new subdomain, or merge into an existing one?)

When a new capability comes in, don't rush to open a subdomain. Use this decision tree to avoid bikeshedding:

1. **Does it share the same scope boundary with some existing subdomain?** (maintained by the same people, the same set of permissions)
   → Yes: **merge into that subdomain**, just add an operation.
   → No: go to 2.
2. **Is it an independent permission/maintenance boundary?** (can be toggled, approved, and owned by a separate group independently and externally)
   → Yes: **open a new subdomain** and register it in SUBDOMAINS.md.
   → No (it's just more features): still **merge** into the closest subdomain — don't open a new subdomain just because "the feature is different".

The principle in one line: **a subdomain cuts along the scope boundary, not the feature list.** However many features fall within one permission boundary, they are multiple operations under a single subdomain.

Examples and counter-examples:

| Scenario | Verdict | Why |
|------|------|--------|
| Market data query vs. order placement | **two subdomains** (market / trade) | two resource scopes, two groups of people — different permission boundaries |
| ticker vs. order book depth vs. K-line | **three operations under one market subdomain** | all read-only market data, the same resource scope; splitting them only fragments the commands |
| query orders vs. place orders | **one trade subdomain**, with access split into read/write | the same resource `spot.trade`; read vs. write is distinguished by the access column, not by splitting the subdomain |
| adding a "24h gainers" board to market | **merge into market**, no new subdomain | still read-only market data, no new resource scope |
| a capability with its own permission boundary and owners | **can be its own subdomain** | an independent resource boundary — the subdomain cuts along the scope, not the feature list |

## Adding a domain

1. Open a new section in [SUBDOMAINS.md](./SUBDOMAINS.md) and list the domain's subdomains.
2. Create the `skills/<domain>/<subdomain>/` scaffold (with `new-subdomain.sh` or by hand).
3. For a lightweight domain where subdomains don't make sense, operations may be laid out directly under the domain without a subdomain directory (the command degrades to two parts).

## Boundary red lines

The boundary rules (what a sub-domain must declare, and what the source may not disclose) live in [AGENTS.md §5](./AGENTS.md). Two things specific to contributing:

- A subdomain must be registered in the SUBDOMAINS.md dictionary.
- Skill files are all in English (the machine fields `name`/`scope`/`signed`/`identity`/`visibility` and the prose body are all English).

## Pre-submit checks

Two layers of defense, reusing the same `scripts/check-boundary.py`:

- **CI (mandatory gate)**: `.github/workflows/ci.yml` runs the checks together with gofmt / build / vet / test / golangci-lint on every pull request and every push to `main`; a violation turns the run red.
- **pre-commit local hook (recommended, fast feedback)**: install it once the first time you work in this repo, and it runs the same gates instantly before each commit, so a failure never has to wait for CI. Install the golangci-lint version named in `.golangci-version`; a different one lints against a different rule set. Missing it is not fatal — the hook says so and moves on.

```
bash scripts/install-hooks.sh        # install the local hook (recommended)
python3 scripts/check-boundary.py    # run it manually once
git commit --no-verify               # skip the local hook for a single commit
```

CI is the hard gate and cannot be bypassed; the local hook is fast feedback plus the Go gate, and it can be skipped with `--no-verify`.

Beyond the boundary script, the pre-commit hook also runs `go test ./...`, which includes the MCP token-budget gate (`TestMCPToolTokenBudget`). So a tool set that exceeds the budget fails locally before commit; keep an eye on its log line for the running total.
