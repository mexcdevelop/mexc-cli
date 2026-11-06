# internal/cli — the CLI machine

> This package assembles the `api.Operation`s declared by each sub-domain into cobra commands and
> runs them, **with no business logic and no knowledge of any particular product**. Business teams
> barely touch it; an edition never imports it (check-boundary Invariant 10) — it reaches the runtime
> through `framework.Deps`.

**What it owns**

- **Command tree**: `mexc <domain> <sub> <operation>`, mounted in three parts (degrades to two
  when a sub-domain doesn't make sense). The topology comes from the edition via `Edition.Domains`.
- **Generic assembly** (`mount.go`): walks a sub-domain's `Operations()` to build commands, bind
  flags, and enforce required / enum / range constraints — business teams write no cobra by hand.
  `validateRegistry` refuses to build the tree when a declaration is incoherent (e.g. an operation
  whose effective host key is empty).
- **Request pipeline** (`root.go`): resolves the effective signing scheme / identity / host key,
  builds the `RequestSpec`, and sends it through the `pkg/transport.Sender` port.
- **Write confirmation** (`confirm.go`): a write is confirmed before it runs — `--yes`, else an
  interactive prompt on the tty, else a structured `confirmation_required` refusal so an agent
  relays to the user instead of self-approving.
- **The derived faces**: `schema`, `mcp`, `explore` and `skills` are all projected from the same
  registry that mounts the commands, so they cannot drift from it.
- **Global flags**: `--base-url`, `--read-only`, `--dry-run`, `--no-retry`, `--yes`, plus `--env` where the product serves more than one env and `--profile` where the edition registers it.
- **Error capture**: cobra's own errors (missing required flag, unknown sub-command) are wrapped into
  a structured envelope with a non-zero exit code, so no error escapes as raw text or as exit 0.
- **Testable**: `NewApp(name, domains, skillsFS, env, out, err)` injects topology, environment and
  IO, so the whole command tree can be driven end to end in unit tests (see `contract_test.go`).

**What it does NOT own**

- `update`, and `config` where the product has a settable key, live in `internal/builtin`,
  built against the exported `Kernel` this package hands them.
- HTTP, rate limiting and output funnelling live in `internal/transport`, `internal/ratelimit` and
  `internal/output`.
- Concrete identities, signing schemes, host rows and product-specific commands belong to an
  edition's `adapter/` and `command/`.

**Adding capability**: add an operation to the sub-domain's `api/` declaration and this layer follows
automatically. Adding a sub-domain also needs one `api.Subdomain` row in its edition's `domains.go`
and a `//go:embed` line — `bash scripts/new-subdomain.sh <domain> <sub> [--internal]` scaffolds the
package and prints both steps.
