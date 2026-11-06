# Changelog

Each section is the release note of one version: it becomes the body of the
GitHub Release and of the commit that publishes the version, so a version is
not released until its section exists here.

Dates are the release date. Versions are `vMAJOR.MINOR.PATCH`; while the
major is 0, every release is a patch bump, breaking changes included, and each
entry says what breaks.

## v0.0.24 — 2026-11-06

The first version published to npm from the public repository. v0.0.23 was
tagged, but its release build stopped on a test and nothing reached npm, so
everything listed under v0.0.23 ships with this version. No command, flag or
output changed.

- Fixed: two npm kit tests failed on a case-sensitive filesystem such as
  Linux. They are what stopped the v0.0.23 release.
- The release workflow only builds, publishes and creates the release; it no
  longer runs the test suite. CI still runs the full gate on every pull request.

## v0.0.23 — 2026-11-04

First release from the public repository,
[mexcdevelop/mexc-cli](https://github.com/mexcdevelop/mexc-cli). No command,
flag or output changed; what changes is where the source lives, its license and
how a release is built.

**Breaking for anyone importing the Go packages.** The module path is now
`github.com/mexcdevelop/mexc-cli`.

- **MIT license.** Every npm package declares `MIT`, ships the `LICENSE` file and
  names the repository it is built from.
- **Releases are built and published by GitHub Actions** from a version tag,
  through npm trusted publishing, so each `@mexc-ai/cli` package on npmjs.com
  carries provenance tying it to the workflow run that built it. Each GitHub
  Release also attaches the bare binary for every platform with `SHA256SUMS`.
- **Only formal versions are published.** Untagged commits no longer publish a
  `0.0.1-beta.<commit>` snapshot.
- `scripts/build.sh --release` and `--snapshot` take `--product scoped|unscoped|all`
  (default `all`) to build one package variant.
- CI runs the full gate on every pull request: the repository checks, gofmt,
  build, vet, tests and golangci-lint.
- The README is rewritten for the public repository and now covers install,
  credentials, request bodies, streaming, agent hosts and configuration in one
  place. The design documents under `docs/` are no longer part of the repository.
- Fixed: a private WebSocket operation run without credentials (`spot account
  ws-deals`, `futures account ws-deals`) no longer tells you to pass `--api-key` and
  `--api-secret` to `profile set`, which takes no such flags.

## v0.0.22 — 2026-09-29

New: query deposit networks and deposit addresses (`spot.asset`).

- `spot asset deposit-chains --coin <X>`: list the networks a coin can be
  deposited over.
- `spot asset deposit-address --coin <X>`: get the coin's deposit address,
  optionally for one network (`--netWork <walletType>` from `deposit-chains`).

## v0.0.21 — 2026-09-11

Packaging and update path only. No Go code changed, so no exported symbol moved,
and the embedded skills are untouched, so no consumer's installed copy moves
either. What changes is the npm kit and the launcher it ships: this tree no
longer names a registry anywhere — not a flag, not an environment variable, not
a field in a generated file. An address is either where we publish or where a
user's package manager points, and neither is a fact about the package.

**Breaking for a caller of the build scripts.** `scripts/build.sh --release` and
`--snapshot` no longer accept `--registry <url>` or `--allow-http-registry`, and
`npmkit/package.mjs` no longer requires `--registry`; both now reject either flag
as an unknown option rather than ignoring it. `npm publish` reads the address
from the publishing environment's own npm configuration, which is where its
credentials live and therefore the only place the two cannot disagree.

- **`update` resolves the registry per run instead of carrying one.**
  `queryVersion` tries two candidates in order — no `--registry` at all, which is
  what lets npm apply the user's own `.npmrc`, `npm_config_registry` and auth
  token, then `https://registry.npmjs.org/` — and returns `{ version, registry }`
  for whichever answered rather than a version string. The install, the rollback
  command and the guidance for an installation mode the CLI cannot mutate all use
  that same one, so what was read and what gets installed cannot diverge.

  Before this, every `npm` / `pnpm` call carried `--registry <published
  address>`. An explicit `--registry` outranks `.npmrc`, `npm_config_registry`
  and any `@scope:registry` override, so `update` ignored the registry the user
  installed from and failed for anyone off the publisher's network — including
  the detached notifier, which fails silently on the same address.

  There is deliberately no override on the update side: no flag, no environment
  variable. A user whose registry serves the package needs none, and one whose
  registry does not is better told to point their package manager at it than
  handed a per-command workaround — which would also be the first path carrying
  user input into a package-manager command line.

- **`distribution.json` and the release manifest no longer carry `registry`.**
  Reading the address back out of npm was not an option: npm exposes no supported
  record of the registry an install used, so omitting the argument is not a guess
  at it but the only way to use it.

- **The notifier cache moves to schema 2.** The registry left the cache identity,
  since there is no longer a fixed address to key on. A version 1 cache is
  discarded rather than migrated, so the first run after upgrading re-checks once.

- **`scripts/check-exposure.py` drops `HOST_DEBT`.** The release path was exempt
  from the non-public-host rule by path rather than by host, because naming the
  host in the checker would have published the very thing the rule protects. With
  no address left to exempt, the exemption is gone and the release path is covered
  like every other file.

## v0.0.20 — 2026-09-08

Documentation only. No Go code changed, so no exported symbol moved and no
behaviour differs; the embedded `mexc-common` skill did, which is why the
skill-hash tests and every consumer's installed copy move with it.

- `mexc-common` tells a caller what to do when the inline `--data '{…}'` form is
  itself the problem rather than the payload. Every skill teaches that form, and
  it has two failure modes a caller cannot fix by editing JSON: a shell that eats
  the quotes, and a command line the operating system will not run at all. Both
  have one answer — `--data @<path>` (or `--data -`) carries no JSON into the
  command line — and the document now leads with it instead of arriving at it
  after teaching three shells' escaping.

  What a caller gets that they did not have: **use `@<path>` for a body that was
  generated rather than typed**, which needs no measuring; on **Windows use
  `@<path>` and not `-`**, quoted as `'@./body.json'` on PowerShell because a
  bare `@` is its splatting operator; and a failure that returns **no structured
  envelope at all** means the CLI never ran, so nothing was sent, a write did not
  happen, and the same call should be re-run with `@<path>` rather than
  truncated.

  The section is shorter than what it replaces — 74 lines to 46. The per-shell
  escaping table, the argv measurements behind it and the advice to identify your
  shell first are gone: once `@<path>` is the standing answer they document the
  form the document steers away from. `git show v0.0.20` carries the measured
  limits per platform, deliberately kept out of a file that enters an agent's
  context every session.

## v0.0.19 — 2026-09-08

Additive for a consumer of the shared layer: no exported symbol was removed or
renamed, so upgrading from v0.0.18 needs no code change.

- `pkg/credential` gains `ConfirmGrade`, the optional `ConfirmGrader` capability
  and `GradeConfirm`. An identity can now mark an operation as needing a
  person's confirmation beyond what its write semantics ask for, and supplies
  the sentence shown to that person. The grading vocabulary stays with whoever
  supplies the identity: this port learns only "must a person confirm this, and
  what do I tell them". An identity without the capability grades nothing, so a
  product that does not implement it is unchanged — verified on the public
  build, where no operation declares `Permission` and no provider carries
  grants.
- The confirmation gate is no longer reachable only from a write. A graded read
  enters it, is called a read rather than a write on all three surfaces (the
  terminal prompt, the MCP elicitation dialog and the relayed
  `confirmation_required`), and is rendered neutrally — never with the
  irreversible banner, whose cursor deliberately starts on Cancel.
- `confirmaudit.Event` gains `GradeCode` and `GradeReason`, saying why an
  identity demanded the confirmation, so the trail separates that case from an
  ordinary write. Two fields because a trail is queried and a person is informed
  on different clocks: the code is the stable key an audit groups by, the reason
  is prose that will be reworded, and keying a trail on the sentence loses its
  history the first time the sentence improves. Reads now produce confirmation
  records where they produced none before, and a query filtering on
  `RiskTier != none` will miss every one of them — group on `GradeCode` too.
- A grade is not asked for under `--dry-run`. An identity answers from its own
  stored session, so asking would make a preview read the credential store,
  which the dry-run exemption for credentials exists to prevent.
- The MCP `confirm` parameter — the two-step confirmation fallback for a host
  with no elicitation dialog — is now published for every operation an identity
  could grade, not only for a write, and its description no longer calls itself a
  write's. Without this a graded read could not comply with its own refusal: the
  refusal says to come back with `confirm=true`, and that parameter was neither
  offered by the schema nor accepted by the handler. A product whose operations
  declare `Permission` will see it appear on those tools' schemas; a product
  whose operations do not see no change beyond the description's wording.
- A grade is neither asked for nor enforced on an unattended face
  (`Edition.Unattended`). The confirmation gate does not run there: with nobody
  to ask, the only confirmation obtainable is one the caller sets for itself, and
  recording that as a person's approval would put a fabricated confirmation in
  the trail. The grade is set outside the product, by whoever governs the
  identity, and they will not learn from this face that it does not apply — a
  product mounting a grading identity there should scope what it serves
  accordingly.

## v0.0.18 — 2026-09-07

Windows only, and both halves of one problem: a code-integrity policy can refuse
to start the platform binary, and until now nothing said so. No exported symbol
changed, so upgrading from v0.0.17 needs no code change.

- The npm launcher tells a blocked binary apart from a broken one. Smart App
  Control refusing an unsigned image and a download that stopped half way both
  surface as `spawn UNKNOWN`, because libuv has no mapping for what
  CreateProcess returns; the launcher reads the image header to choose between
  "run the command again, the verdict is cached" and "reinstall, retrying will
  not help". Measured on Windows 11 with the policy enforcing: the first run
  after an install failed, a 3077 event named the file, and a retry succeeded.
- `doctor` reports that policy as `app_control` before it bites, with the event
  log query that confirms a block. The row is absent when the policy is off,
  which is the common case. Both Windows checks now run concurrently, so the new
  one costs no wall clock.
- Docs: the no-credential operation count is 63, not 43. The wrong figure was
  published twice; the declarations changed in v0.0.16 and only the human-facing
  docs had drifted. `spot marketv2 ticker` and both `spot kline` operations need
  no key.

## v0.0.17 — 2026-09-07

Windows and Linux join macOS as shipping platforms. Additive for a consumer of
the shared layer: no exported symbol was removed or renamed, so upgrading from
v0.0.16 needs no code change.

- npm distribution covers six targets: darwin, linux and win32, arm64 and x64.
  `npmkit/platforms.json` is the kit's capability and `npm/product*.json` the
  decision to ship, checked as a subset rather than an equality.
- `pkg/console` (new): the controlling terminal as a port, plus
  `StdioIsTerminal`. The write-confirmation gate now asks whether the caller is
  attended instead of whether a console exists, so a prompt inherited by an
  unattended caller refuses with `interactive_required` rather than hanging.
- `pkg/secfile` (new): the credential write path, with a no-follow open and an
  advisory lock, on both POSIX and Windows.
- `pkg/secretstore`: the local key file is wrapped with DPAPI on Windows and
  bound to the Windows account. Off macOS the OS keyring is no longer consulted,
  which on Linux leaves the key unwrapped beside the data it opens. New
  `KeyringAvailable` and `KeyFileWrappedByOS` report which applies. A key file
  from another machine is named foreign rather than unreadable, and deletion is
  no longer advised for one that still works elsewhere.
- `--data` accepts `@<path>` and `-` (stdin), so a request body need not survive
  a shell. A body a shell has already mangled is routed to that hint instead of
  reported against the schema.
- `doctor` reports a PowerShell execution policy that blocks the npm shim, and a
  legacy code page as an encoding problem rather than a bad flag.
- Acceptance harnesses, both exercised against real binaries:
  `scripts/verify-linux.sh` in containers and `scripts/verify-windows.ps1` on a
  Windows machine. New gate `scripts/check-test-isolation.py`: a test that
  redirects only `HOME` isolates nothing on Windows, where `os.UserHomeDir`
  reads `USERPROFILE`.
- The credential prompts name the two values Access Key and Secret Key, matching
  the MEXC website.
- `docs/user-guide.zh-CN.md` (new): a Chinese user guide for external users.

## v0.0.16 — 2026-09-03

Breaking: `Edition.OutboundRatePerSec` and `Edition.OutboundOrderRatePerSec` are
replaced by `Edition.RateLimit *transport.RateLimitPolicy`. Nil is the shared
default, not "no throttle"; a face that wants none says `transport.Unlimited`.

- The client throttle default is 50/s, burst 50, giving up after 30s. It was
  10/s with a wider 20/s bucket for order paths, numbers that had no source and
  inverted the backend's actual limits.
- `Edition.BuildVersionHeader` names a header every request and WS handshake
  carries, valued from `pkg/buildinfo`.
- `pkg/errs` gains `SubtypeClientRateLimited`, distinct from the upstream 429.
- Spot market data: 18 `marketv2` operations and both `kline` operations now
  declare `IdentityNone` and `SignNone`, so they need no saved profile. Within
  `marketv2` only `symbols`, `tickers`, `symbol-trade` and `user-eftd-info`
  still require a key, because their results are account-aware. That moves the
  no-credential count from 43 operations to 63.

## v0.0.15 — 2026-08-28

Breaking: `framework.Deps` gains `ClientFor(context.Context) transport.Sender`.
`Client()` is unchanged and still serves invocation-scoped work; `ClientFor`
serves work done on behalf of a remote caller. Everything else is additive.

- `framework.RunMCPServer`: a second entry point, a sibling of `Run`, that
  assembles the tool surface and nothing else. This module ships no listener.
- `pkg/credential` gains `Lookup`, `Binder` and `ReservedHeadersVia`, so two
  concurrent requests in one process can resolve two accounts.
- `pkg/caller` (new): the per-request facts a long-lived face learns from its
  transport. `pkg/toolref` (new): the neutral tool descriptor.
- `Edition.Unattended` skips the write-confirmation gate where nobody can be
  asked, and must pair with a discard sink. `Edition.MCPCapabilities` sets what
  is advertised at initialize.

## v0.0.14-rc.1 — 2026-08-28

Temporary release candidate for build-version header pass-through testing.

## v0.0.15-rc.1 — 2026-08-24

Lightweight tag on the framework's second entry point, ahead of v0.0.15.

## v0.0.13 — 2026-08-21

Nothing changes for a consumer of the shared layer.

- The fixed-term financial workflow notes the pledge order's `orderId` rather
  than its `orderNo`.

## v0.0.12 — 2026-08-20

Breaking: `hosts.Registration.Internal` is now `NoOverride`, and
`hosts.IsInternal` is now `IsNoOverride`. The flag names what it does.

- A disclosure gate, `scripts/check-exposure.py`, behind AGENTS.md Invariant 12.
- A README written for someone who has just installed the CLI.
- A second, unscoped npm package set.
- Every activity operation now requires an API key.

## v0.0.11 — 2026-08-17

Breaking: `mcp` requires a domain. A bare `mcp` registered every operation as a
tool, which is what an agent host loads into context before reading anything. It
is now refused with `missing_argument` and a hint naming the domains.

- `mcp` help examples are generated from the product's own registry.

## v0.0.10 — 2026-08-14

- Contract sub-domains: `market`, `account` and `trade`.
- The macOS npm distribution kit.
- Vendored dependencies, for builds without network access.

## v0.0.10-rc.1 — 2026-08-13

Release candidate for downstream integration.

## v0.0.9 — 2026-08-12

- `transport.WSConfig` gains `HostKey`, and `URL` becomes a suffix appended to
  the resolved base, so WS operations reach their endpoint through `pkg/hosts`
  and honour the per-key base-URL convention.
- Every signed request in this product uses `cli-hmac`.

## v0.0.8 — 2026-08-11

WS runs through a port, so a consumer with no WS operation stops linking the
WebSocket client and its protobuf runtime.

- `pkg/transport` gains the `WSCollector` interface; `RedactURLQuery` moves here
  with no behaviour change.
- The WS machinery moves to `internal/transport/ws`; `Edition.WSCollector`
  becomes an optional seam, validated at assembly.

## v0.0.7 — 2026-08-10

- `pkg/api`: the `OperationKind` discriminator (`KindREST` zero value,
  `KindWSCollect`) and the `WSCollect` payload, so a WS-backed operation is
  describable without inventing REST vocabulary for it.
- `pkg/transport`: `WSConfig` and `Plan.BodyChecked`.

## v0.0.6 — 2026-08-06

- `pkg/confirmaudit` (new): this layer decides when a confirmation record is due
  and what it says; a product supplies the wire through
  `Edition.ConfirmReporter`. A build that mounts writes and declares no sink
  fails assembly.

## v0.0.5 — 2026-08-05

- `pkg/secretstore` gains `ErrUndecryptable`, marking the one failure of `Open`
  that means the stored bytes are beyond help on this machine. Every other
  failure is a statement about reaching the key.

## v0.0.4 — 2026-08-05

- `pkg/secretstore` (new): credential encryption at rest, sealed under a key the
  OS keyring holds, so a copied data directory carries ciphertext without the
  key. Both the keyring and a local 0600 file are tried when opening.

## v0.0.3 — 2026-07-31

Breaking for consumers of `pkg/`.

- A JSON request body may be a top-level array. Every declared rule is enforced
  per item, with the failing item's index named in the error. `Schema` gains
  `body.shape`, and the MCP face publishes an array body accordingly.
- This closed a silent hole: a prototype that reflected into no fields used to be
  accepted at declaration time, leaving every rule unenforced.

## v0.0.2 — 2026-07-30

- The shared layer: `pkg/` ports, the CLI machinery and the framework
  composition contract. The first tag whose `go.mod` declares a module path that
  resolves, so internal consumers can require it directly.
