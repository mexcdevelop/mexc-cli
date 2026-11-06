# MEXC CLI

[![CI](https://github.com/mexcdevelop/mexc-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/mexcdevelop/mexc-cli/actions/workflows/ci.yml)

Command-line interface for the MEXC API, for terminals and AI agents.

```bash
mexc <domain> <sub-domain> <operation> [flags]
mexc contract market ticker --symbol BTC_USDT
```

- 309 operations across spot, contract, futures, earn and activity; 63 need no credentials.
- `mexc schema <id>` describes any operation: parameters, body, access, credentials.
- JSON on stdout; errors as a JSON envelope on stderr with a stable exit code.
- Writes need confirmation; `--read-only` blocks them; `--dry-run` sends nothing.
- Skill documents and an MCP server for AI agents, generated from the same registry.

Pre-1.0: breaking changes are possible and are listed in [CHANGELOG.md](./CHANGELOG.md).

## Contents

[Install](#install) · [Set up an AI agent](#set-up-an-ai-agent) ·
[Quick start](#quick-start) · [Credentials](#credentials) ·
[Running operations](#running-operations) · [Request bodies](#request-bodies) ·
[Streaming](#streaming) · [MCP server](#mcp-server) · [Configuration](#configuration) ·
[Coverage](#coverage) · [Building from source](#building-from-source) ·
[Contributing](#contributing) · [Security](#security) · [License](#license)

## Install

Requires Node.js 20+. macOS, Linux and Windows, arm64 and x64.

```bash
npm install -g @mexc-ai/cli
mexc doctor                      # build, endpoint, credentials, agent docs
```

| | |
|---|---|
| Update | `mexc update` (`--check` to only check) |
| Uninstall | `npm uninstall -g @mexc-ai/cli` |
| Bare binaries | each [GitHub Release](https://github.com/mexcdevelop/mexc-cli/releases), with `SHA256SUMS` |
| Provenance | every npm package is published from GitHub Actions with provenance |

**Windows.** If PowerShell reports "running scripts is disabled on this system",
run `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned`, or call `mexc.cmd`.
In `.bat` files write `call mexc ...`.

## Set up an AI agent

Paste this into Claude Code, OpenCode, Codex, Cursor or any agent with a shell:

```text
Install the MEXC CLI and its skills:
1. npm install -g @mexc-ai/cli
2. mexc skills install --agent <your agent id>   (claude-code, opencode, codex, cursor, kiro-cli)
3. mexc doctor
Do not configure API keys yourself. If a command needs credentials, ask me to
run `mexc profile set <name>` in my own terminal. Never add --yes to a write
operation unless I have confirmed it.
```

Or do it by hand:

| agent | skills | MCP (optional, read-only) |
|---|---|---|
| Claude Code | `mexc skills install` | `claude mcp add-json -s user mexc-spot '{"type":"stdio","command":"mexc","args":["mcp","spot","--read-only"]}'` |
| OpenCode | `mexc skills install --agent opencode` | add to `~/.config/opencode/opencode.json`: `"mcp": {"mexc-spot": {"type": "local", "command": ["mexc", "mcp", "spot", "--read-only"], "enabled": true}}` |
| Codex | `mexc skills install --agent codex` | `codex mcp add mexc-spot -- mexc mcp spot --read-only` |
| Cursor | `mexc skills install --agent cursor` | `cursor --add-mcp '{"name":"mexc-spot","type":"stdio","command":"mexc","args":["mcp","spot","--read-only"]}'` |
| Kiro CLI | `mexc skills install --agent kiro-cli` | `kiro-cli mcp add --name mexc-spot --command "mexc mcp spot --read-only" --scope global` |

- Skills need Node.js and `npx`. Run `mexc skills update` after upgrading the CLI.
- The agent should read `mexc-common`, `mexc-auth` and `mexc-ws` first.
- Credentials are entered by you, in a terminal; an agent cannot supply them.

## Quick start

```bash
mexc contract market ticker --symbol BTC_USDT        # public, no account
mexc spot marketv2 ticker --symbol BTC_USDT
mexc spot --help                                     # sub-domains and operations
mexc schema                                          # every operation id
mexc schema spot.trade.place                         # one operation in full
mexc explore                                         # terminal UI
```

## Credentials

```bash
mexc profile set <name>          # prompts for Access Key and Secret Key
mexc profile use <name>          # set the active profile
mexc profile list
mexc profile show <name>         # Secret Key masked
```

- Keys are entered only at a terminal prompt: no flag, environment variable or
  stdin. Without a terminal, `profile set` exits 11 (`interactive_required`).
- `--profile <name>` selects a profile for one command.
- Use an API key with only the permissions you need. MEXC enforces them; an
  operation the key may not call fails with exit 6.

**Storage.** Profiles are encrypted at rest in `~/.mexc`:

| | encryption key | state directory copied elsewhere |
|---|---|---|
| macOS | login keychain | unreadable |
| Windows | file in `~/.mexc`, DPAPI-wrapped | unreadable |
| Linux | file in `~/.mexc`, unwrapped | **readable**: protect `~/.mexc` like the keys |

If a profile fails to decrypt, do not save it again (that discards the original
key); run `mexc profile keychain-downgrade` where the keyring is available.

**Request signing.** `mexc schema <id>` shows `"identity": "none"` for public
operations, which send no credentials.

- REST: the Access Key is sent in `X-MEXC-APIKEY`. The Secret Key stays local and
  signs each request with HMAC-SHA256 over method, path, sorted query, body hash
  and a millisecond timestamp, sent as `X-MEXC-CLI-SIGNATURE` and
  `X-MEXC-CLI-TIMESTAMP`.
- Private WebSocket: spot gets a listen key through a signed REST call; futures
  logs in on the connection with a signed timestamp.
- `--dry-run` loads no credentials and computes no signature.

## Running operations

Query parameters are flags; a request body goes in `--data`.

| flag | effect |
|---|---|
| `--read-only` | refuse all writes (also `MEXC_READ_ONLY=true`) |
| `--dry-run` | print the request; send and sign nothing |
| `--yes` | confirm a write without a prompt |
| `--profile <name>` | use this profile |
| `--no-retry` | no automatic retry on rate limits or network errors |

Writes prompt in a terminal. Without a terminal and without `--yes` they exit 10
(`confirmation_required`). **Agents must not add `--yes` without the user's
confirmation.**

Errors go to stderr:

```json
{"error":{"type":"validation","subtype":"invalid_argument","param":"--symbol","message":"...","hint":"..."}}
```

| exit | meaning |
|---|---|
| 0 | success |
| 2 | validation (flag, parameter, body, precondition) |
| 4 | network |
| 5 | internal |
| 6 | API error |
| 10 | `confirmation_required` |
| 11 | `interactive_required` |

## Request bodies

```bash
mexc spot trade place --data '{"currency":"BTC","market":"USDT","tradeType":"BUY","price":"60000","quantity":"0.001","orderSource":"CLI"}'
```

Windows shells strip the quotes in `'{"a":1}'`. Use a file or stdin:

| shell | inline | file | stdin |
|---|---|---|---|
| bash / zsh | `--data '{"a":1}'` | `--data @./body.json` | `--data - <<'JSON' … JSON` |
| PowerShell | `--data '{\"a\":1}'` | `--data '@./body.json'` | `Get-Content -Raw body.json \| mexc … --data -` |
| cmd.exe | `--data "{\"a\":1}"` | `--data @./body.json` | use a file |

Body files must be UTF-8 (BOM allowed). UTF-16 and legacy code pages are
rejected, not converted.

## Streaming

```bash
mexc futures market ws-ticker --symbol BTC_USDT --limit 1           # stop after 1 message
mexc spot marketv2 ws-trades  --symbol BTCUSDT  --listen-duration 10s
```

One JSON object per line. Without `--limit` or `--listen-duration` it runs until
interrupted. Futures symbols are `BTC_USDT`; spot WebSocket symbols are `BTCUSDT`.

## MCP server

```bash
mexc mcp spot --read-only        # a domain is required
mexc mcp spot marketv2           # or a sub-domain
```

```json
{ "mcpServers": { "mexc-spot": { "command": "mexc", "args": ["mcp", "spot", "--read-only"] } } }
```

- Tools per domain: spot 125, contract 117, financial-openapi 51, activity 8.
  Uploads, downloads and WebSocket operations are not exposed; `futures` has none.
- Keep `--read-only` unless writes are intended: it removes write tools. Without
  it, a host with a confirmation dialog shows it; a host without one gets
  `confirmation_required` and must call again with `confirm=true`.
- If the host cannot start `mexc` (always on Windows, often for apps launched
  from the Dock), use the absolute path: `$(npm root -g)/@mexc-ai/cli-<platform>/bin/mexc`.
- JSON config files: Claude Desktop `claude_desktop_config.json`, Windsurf
  `~/.codeium/windsurf/mcp_config.json`, Kiro IDE `~/.kiro/settings/mcp.json`.

## Configuration

| | |
|---|---|
| `~/.mexc` (`%USERPROFILE%\.mexc`) | profiles, keys, update cache, exported skills; deleting it deletes credentials |
| `MEXC_READ_ONLY=true` | `--read-only` for every command |
| `MEXC_CLI_NO_UPDATE_NOTIFIER=1` | no update notice (also off when `CI` is set or stderr is not a terminal) |
| `--base-url` / `MEXC_OPENAPI_BASE_URL` | another endpoint, for development |

## Coverage

| domain | sub-domains |
|---|---|
| `spot` | `marketv2` · `kline` · `market-search` · `account` · `trade` · `asset` · `convert` · `dca` · `fee` · `grid` |
| `contract` | `market` · `trade` · `account` |
| `futures` | `market` · `account` (WebSocket) |
| `activity` | `online` · `reward` · `participate` |
| `financial-openapi` | `common` · `flexible` · `fixed` · `contract-hold` |

## Building from source

Go 1.25+. Dependencies are vendored.

```bash
bash scripts/build.sh                                    # ./mexc
bash scripts/build.sh --install                          # $GOBIN or ~/go/bin
bash scripts/build.sh --target windows/arm64             # cross-compile
bash scripts/build.sh --snapshot --version 0.0.0-dev.1   # npm tarballs in dist/npm/ (macOS/Linux only)
```

```
cmd/mexc/              main
editions/public/       product wiring and its own commands (profile, doctor, …)
framework/             contract an edition builds against
internal/              command tree, MCP, transport, output
pkg/                   operation types, signing, credentials, errors
skills/<domain>/<sub>/ SKILL.md + api/ (operation declarations)
npmkit/                npm launcher and packaging
```

Dependencies point down only (`pkg` ← `internal` ← `framework` ← `editions`).
`api/` packages declare operations and never send HTTP.

## Contributing

Issues and pull requests are welcome.

- [CONTRIBUTING.md](./CONTRIBUTING.md): adding a domain, sub-domain or operation.
  `bash scripts/new-subdomain.sh <domain> <sub>` scaffolds one.
- [AGENTS.md](./AGENTS.md): contracts for errors, `SKILL.md` and boundary checks.
- Install the hook (`bash scripts/install-hooks.sh`) and run the checks in
  [AGENTS.md §6](./AGENTS.md#6-pre-commit-self-check); CI runs them on every pull request.
- English only. Conventional Commits (`feat:`, `fix:`, `docs:`, …).

## Security

Report vulnerabilities by email to github@mexc.com, not in public issues.

## Disclaimer

With suitable credentials this tool places real orders and moves real funds.
Trading digital assets carries risk. Review every write before confirming it.

## License

[MIT](./LICENSE)
