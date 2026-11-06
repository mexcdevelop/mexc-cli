---
name: mexc-auth
description: "How to satisfy an operation's identity for mexc — the openapi (API-key) credential flow, both proactive (set up first) and reactive (recover on error). Business skills whose frontmatter declares identity: yes point here. For the common safety/error rules see mexc-common."
visibility: public
---

# mexc · authentication (openapi)

A subdomain with `identity: yes` needs a caller identity. First find out which:

```bash
mexc schema <domain>.<sub>.<operation>
```

- `identity: none` — public data; call directly, no credentials.
- `identity: openapi` — an Access Key and a Secret Key (the pair the MEXC website issues), kept in a profile. **This guide.**

(`signed: true` only means the request is signed; the scheme is internal and you
never supply it. The safety and error-routing rules are in `mexc-common`.)

## The flow

The openapi credential is a **persistent profile** — a one-time setup, not a
per-call login. Either prepare it up front or recover when the call fails; both
reach the same place.

**DO NOT CREATE A PROFILE. ASK THE USER TO.** `mexc profile set` takes no
`--api-key` / `--api-secret` flags and prompts on a terminal, so it will simply
fail for you. There is no other channel — no flag, no stdin, no environment
variable — so do not go looking for one, and do not manufacture a terminal to
get around it.

**Never ask the user to paste an Access Key or Secret Key into this conversation.**
Anything they send you is recorded in the transcript, which leaves the machine;
that is the whole point of keeping the secret off the command line. Relay the
command and let them type it where you cannot see it.

**Proactive — check first, ask the user to set up if missing:**

```bash
mexc profile list                 # lists saved profiles, marks the active one (needs no credentials)
```

Nothing saved, or no active profile? Tell the user to run this **in their own
terminal** — it prompts for the Access Key and then the Secret Key, and the Secret Key stays hidden as they type:

```bash
mexc profile set <name>
mexc profile use <name>           # you may run `use` yourself; only `set` needs a terminal
```

Then run the operation.

**Reactive — call, recover, retry:**

```
run the op → fails with failed_precondition "requires API credentials"
           → ask the user to run `mexc profile set <name>` in their terminal
           → once they confirm, `mexc profile use <name>` and retry the op
already have a usable profile → the op just succeeds; do nothing
```

There is no credential environment variable, and no flag, stdin or file that
will do instead — credentials live only in a profile under `~/.mexc/profiles/`,
typed in at a prompt.

Profiles are **encrypted at rest**, under a key held by the OS keyring — or, where
no keyring is usable, a local key file the CLI creates by itself (saving still
works; it warns). You do not manage that key. It reaches you only when a stored
profile cannot be decrypted, and there is one rule:

**Never respond to a decryption failure by asking the user to save the profile
again.** The usual cause is a keyring that is present but out of reach — a
sandboxed agent host, CI, a locked keychain — and the operating system reports that
identically to "there is no key". Saving again does not recover anything: it seals a
*second* key and leaves the first where it was. On a sandboxed host `profile set`
cannot even run, because it needs a terminal.

What to relay instead: ask the user to run `mexc profile keychain-downgrade --yes`
in their own terminal, where the keyring answers. That copies the existing key into
a local file, after which this context can read the profiles. Then retry your
original command unchanged.

**Never suggest deleting the key file** (`~/.mexc/profile-key.key`). Where the CLI
created it by itself it is the only copy, and deleting it makes every stored profile
permanently unreadable — `profile show` masks the secret, so they cannot be read
back and re-saved either.

Only if the keyring answers and still holds no key was the profile copied from
another machine. That case is not recoverable and saving it again is the fix.

## Error → action

| subtype | `message` contains | do |
|---------|--------------------|----|
| `failed_precondition` | `requires API credentials` | set + select a profile, then retry the op |
| `api` | unauthorized / permission denied (code from the exchange) | the key is valid but lacks a permission, or is wrong — ask the user to check its permissions or re-issue it |

Exact string, so you can match and act:

- `this operation requires API credentials`
  → hint: *ask the user to run `mexc profile set <name>` in their own terminal (it prompts; no flag, stdin or environment variable supplies a secret instead), then `mexc profile use <name>`.*

- `the OS keyring did not answer, so stored credentials cannot be reached`
  → locked or sandboxed. **Do not** advise saving the credential again: the key is
    probably still there. Either the user unlocks it and you retry unchanged, or
    they run `mexc profile keychain-downgrade --yes` in their own terminal.

- `stored data cannot be decrypted with any key on this machine`
  → most often a keyring that is out of reach, not a lost key. Relay the hint:
    `mexc profile keychain-downgrade --yes` in the user's own terminal, then retry.
    Only when the keyring answers and holds nothing is this unrecoverable, and only
    then is saving it again the fix.

- `no encryption key could be read on this machine`
  → nothing saved here yet (saving a credential creates the key), or a keyring out
    of reach, which reports itself the same way. Do not save again to find out.

- `the OS keyring holds no key to copy, so there is nothing to pin to a local file`
  → `keychain-downgrade` copies an existing key and never invents one. Either save a
    credential first, or the key is out of reach and the command has to run from a
    terminal where the keyring answers.

- `saving a profile requires a real terminal` (subtype `interactive_required`, exit 11)
  → you ran `profile set` yourself. Relay the hint to the user; there is no
    non-interactive alternative to find. Exit 11 means exactly that: not "fix
    your arguments" (that is 2), but "stand aside, let the user act, then retry
    the original command unchanged".
