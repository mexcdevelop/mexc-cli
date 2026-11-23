---
name: mexc-setup
description: "Install and configure the mexc CLI (npm package @mexc-ai/cli) — prerequisites, install or upgrade, health check, the agent skill docs, an API-key profile, and optional MCP registration. Use when the mexc command is missing, when the user asks to install, set up, upgrade or check mexc, or before the first mexc call on a new machine."
---

# mexc · setup

Take the user from nothing to a working `mexc` that you can drive. Run the steps
in order and read each command's output before moving on: every step depends on
the one before it, so stop at the first one that fails and tell the user what it
printed. Talk to the user in their language; the commands stay as written.

Three rules hold for the whole flow:

- **Say what you are about to run before anything that changes the machine** —
  `npm install -g`, `mexc update`, `mexc skills install`.
- **Never ask for the Access Key or Secret Key in this conversation**, and never
  try to type them in for the user. Step 5 explains why and what to do instead.
- **Never add `--yes` to a mexc command on your own.** It asserts that the user
  approved a write.

## 1. Prerequisites

```bash
node --version    # v20 or newer
npm --version
```

Node.js missing or older than v20: ask the user to install Node.js 20 or newer
(https://nodejs.org) and stop until they have. Packaged builds exist for macOS,
Linux and Windows, each on arm64 and x64.

## 2. Install, or find the existing install

```bash
mexc --version
```

- **Prints `mexc version <x> [public] …`** — already installed. Check for a newer
  release, and install it if the user agrees:

  ```bash
  mexc update --check    # {"current": …, "latest": …, "update_available": …}
  mexc update
  ```

- **Command not found** — install it:

  ```bash
  npm install -g @mexc-ai/cli
  mexc --version
  ```

If the install fails:

| symptom | what to do |
|---|---|
| `E404` | the registry npm is configured for does not serve the package, which is published to the public npm registry. Show the user `npm config get registry` and let them point npm at a registry that serves it. Do not change their npm configuration yourself |
| `EACCES` | npm's global directory is not writable by this user. Do not retry with `sudo` on your own; tell the user and let them choose how to fix it |
| Windows PowerShell: `running scripts is disabled on this system` | the install worked; PowerShell refused to run the `mexc.ps1` shim. Ask the user before changing anything: `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` allows local scripts for their account only, or call `mexc.cmd` instead, which the policy does not govern |

Install `@mexc-ai/cli` only. An unscoped package, `mexc-ai-cli`, provides the same
`mexc` command, so having both installed leaves it unclear which one runs.

From a Windows `.bat` file, write `call mexc …` — without `call`, the rest of the
script never runs.

## 3. Health check

```bash
mexc doctor --json
```

Read `ok` and every entry of `checks`. Right after a fresh install, expect
`credentials` to warn `no profiles saved` and `skills_docs` to say the docs are
not installed — steps 4 and 5 deal with those. Any check with status `fail`:
stop and show the user its `message` and `hint`.

The endpoint is built in. There is no environment to choose: `--env` is not a
flag and `MEXC_ENV` is not read.

## 4. Install the skill docs for this agent

`mexc` ships one skill per sub-domain, embedded in the binary so they always
match the installed version. They are what tells you how to call each domain.

```bash
mexc skills install --dry-run --agent <id>   # prints the exact command; changes nothing
mexc skills install --agent <id>
```

`<id>` is the agent you are running in: `claude-code` (the default when
`--agent` is omitted), `cursor`, `codex`, or `kiro-cli` for Kiro. Repeat `--agent`
for several; `--project` installs into the current project instead of globally.
It runs `npx`, which comes with Node.js.

Compare the count the install reports with the `would export N skill(s)` line
from the dry-run, then run `mexc doctor --json` again: `skills_docs` should no
longer say not installed. Some agents load skills only when a session starts; if
the `mexc-*` skills are not visible to you yet, ask the user to start a new
session.

Read `mexc-common` (errors and write safety) and `mexc-auth` (credentials) before
anything else among them.

## 5. Credentials — only for account operations

Public market data needs no credentials. This call proves the install and the
network work end to end:

```bash
mexc spot marketv2 ticker --symbol BTC_USDT
```

Account, trading, asset and other signed operations need an **Access Key** and a
**Secret Key** — the pair the MEXC website issues — saved in a local profile.
`mexc schema <operation-id>` tells which kind an operation is: `identity` is
`none` for public data and `openapi` when it needs the key pair.

Ask the user whether they want to set this up now. If they do:

```bash
mexc profile list    # what is saved, and which profile is active
```

Nothing usable saved: ask the user to run this **in their own terminal**, not
through you:

```bash
mexc profile set <name>    # prompts for the Access Key, then the Secret Key (hidden)
```

It reads a real terminal only. No flag, environment variable or stdin carries
the secret, so it keeps the key out of shell history and out of this transcript.
If you run it yourself it fails with `interactive_required` (exit 11) — that is
expected. Relay the command; do not look for another channel or create a
terminal to get around it.

Once the user says it is saved:

```bash
mexc profile use <name>
mexc doctor --json         # the credentials check should now say the profile is loaded
```

If a stored profile cannot be decrypted, follow `mexc-auth`. Never ask the user
to save it again — that seals a second key and leaves the first behind — and
never suggest deleting `~/.mexc/profile-key.key`.

On Linux the encryption key is a file inside `~/.mexc`, so anything that copies
that directory copies the credentials with it. Tell a Linux user so.

## 6. Optional — register an MCP server

Only when the user asks to call mexc as MCP tools from an agent host. The skills
from step 4 are enough for an agent that can run shell commands.

- **One server per domain.** `mexc mcp <domain>` serves one domain; a bare
  `mexc mcp` is refused and lists the domains.
- **Keep `--read-only` unless the user wants writes through MCP.** It withholds
  write tools from the tool list, which in a host with no confirmation dialog is
  the only structural stop on a write.
- **Use the platform binary's absolute path.** A host launched outside a terminal
  may not have npm's global bin directory on its PATH, and on Windows the bare
  name never works. With npm the binary is
  `<npm root -g>/@mexc-ai/cli/node_modules/@mexc-ai/cli-<platform>/bin/mexc`
  (`mexc.exe` on Windows), where `<platform>` is `darwin-arm64`, `linux-x64`,
  `win32-x64` and so on. Confirm the path exists before writing it into a config;
  if it does not, look for the binary under the directory `npm root -g` prints.
  On Windows, JSON needs each backslash in the path doubled.

For Claude Code:

```bash
claude mcp add-json -s user mexc-spot '{"type":"stdio","command":"<absolute path>","args":["mcp","spot","--read-only"]}'
```

Hosts without a command line (Claude Desktop, Windsurf, the Kiro IDE) take the
same entry under the `mcpServers` key of their config file. Show the user the
exact entry before you add it anywhere.

## 7. Report back

Tell the user, briefly: the installed version, the `doctor` result, which agent
the skills went to, whether a profile is active, any MCP entry you added, and
anything still left for them to do.

## Later: upgrading

```bash
mexc update --check
mexc update
mexc skills update --agent <id>    # keep the installed skill docs in step with the binary
```

`mexc update` exists only on an npm-installed copy; a binary built from source
is upgraded by rebuilding it. `mexc doctor` warns when the installed skill docs
no longer match the binary.
