# pkg/ — the ports and shared foundation

> `pkg/` is the bottom layer, blind to everything above it: it imports no other package in this repo.
> Business teams call its stable interfaces; they never change its implementation.

`pkg/` holds two kinds of thing: the **ports** an edition plugs concrete behaviour into, and the
**shared foundation** every layer builds on. It deliberately ships **no** concrete host row, provider
or signing scheme — each edition injects its own, which is what keeps the package edition-agnostic.

| Package | Responsibility |
|----|------|
| `api` | the `Operation` descriptor business teams face, `DomainSpec` / `Subdomain`, the enum **types** plus the values the platform owns (`In*`, and the `none` value of each auth axis — a product declares its own schemes and identities in `signing/` / `identity/`), declaration validation, and `RequestSpec` (transport-neutral) — the single source of truth every face derives from (ARCHITECTURE §2) |
| `signer` | **port**: the `Signer` interface + registry; a scheme signs a `RequestSpec`. An optional capability (reserved params — the query keys a scheme injects and must not have overwritten) is a separate interface a scheme may also satisfy |
| `credential` | **port**: the `Provider` interface keyed by `Realm` — resolve credentials, inject identity material, and hand the same credentials to the signer so signing stays consistent |
| `hosts` | resolves `(hostKey × env) → Endpoint`, with a per-key `MEXC_<KEY>_BASE_URL` override that always wins. Carries the resolution logic only; each edition `Register`s its own rows |
| `transport` | **port**: the `Sender` interface + the transport-neutral `Plan` printed under `--dry-run`, so an edition never depends on the concrete client in `internal/transport` |
| `config` | the global-settings registry — a CLOSED set that starts EMPTY and is filled by the composition root, so what is configurable differs per product — and the **one** producer of the state directory (`StateDir` / `StatePath`, from `Edition.StateDir`). It holds no credentials: what a credential *is* differs per identity, so each product stores its own (this one in `editions/public/profile/`) |
| `errs` | typed errors, the four envelope fields, and the exit-code mapping (ARCHITECTURE §4) |
| `buildinfo` | the version / commit / build-time variables `scripts/build.sh` stamps via `-ldflags -X`, read by `--version` |
| `cliname` | the name this binary is invoked as, taken from `argv[0]` at startup. User-facing prose reads it instead of hard-coding a command name, so a renamed or symlinked binary describes itself correctly (enforced by check-boundary invariant 17) |
| `skillstate` | what `skills install` laid down and the binary version it came from, so a later run can tell the installed docs are stale. Compares BUILD VERSIONS, not content — see the package doc for what that does and does not detect |
| `secretstore` | seals a small blob under a key held by the OS keyring, so a credential file copied into a backup or a dotfile repo arrives useless. NOT a boundary against a process running as the same user, and deliberately without a silent fallback — see the package doc |

The implementations these ports front — the HTTP client, rate limiting, output funnelling,
self-update — are machinery and live in `internal/`. Concrete adapters (an api-key identity, an HMAC
scheme, a product's host rows) live in that product's `editions/<p>/adapter/`.

**Planned (not yet implemented)**

- **Permission validation**: intercept by resource (scope) × action (access), maintaining a
  `(scope, access) → underlying API-key permission` mapping.
- **Execution framework**: pagination. (Retry/backoff has landed — idempotent requests auto-retry
  transient failures, honouring `Retry-After`; `--no-retry` opts out.)

## Why this layer is the one that ships

`pkg/` is the **shared base both products are built from**, and the only layer an external module is
allowed to depend on: `internal/` is module-private and unreachable from outside, so a consumer can
only ever compose against the ports here plus the `framework/` entry point. When the tree is split
into two repositories, `pkg/` goes to the shared module — which is exactly why it must stay free of
any product-specific concrete implementation.
