# CLAUDE.md — AI Agent Entry Point

> This file is a pointer. **The single source of truth for all rules is [AGENTS.md](./AGENTS.md)** — read it in full before writing any skill, API wrapper, or code. This file only restates the one rule most often missed.

## English-only (non-negotiable)

**Everything written into this repository is English-only.** No exceptions:

- Go code — identifiers, **comments**, string literals, log messages.
- Commit messages and MR/PR descriptions — Conventional Commits in English (`feat:` / `fix:` / `docs:` / `refactor:` / `chore:` / `ci:`).
- Skill files and docs — machine fields and prose body.

CI enforces this: `scripts/check-english.py` scans changed Go lines and the commit message and turns the pipeline red on any CJK character. Do not work around it — write English.

## Everything else

See [AGENTS.md](./AGENTS.md) (hard contracts) and [CONTRIBUTING.md](./CONTRIBUTING.md) (how to add a directory). Run the local checks in AGENTS.md §6 before opening an MR.
