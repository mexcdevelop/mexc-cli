# mexc-cli — AI Agent Steering

The single source of truth for all rules is #[[file:../../AGENTS.md]]. Read it before writing any skill, API wrapper, or code. This steering file only restates the one rule most often missed.

## English-only (non-negotiable)

Everything written into this repository is English-only — Go identifiers, comments, string literals, log messages, commit messages, MR/PR descriptions (Conventional Commits in English), skill files, and docs. No CJK anywhere.

CI enforces this: `scripts/check-english.py` scans changed Go lines and the commit message and turns the pipeline red on any CJK character. Write English rather than working around it.

## Everything else

See #[[file:../../AGENTS.md]] for the hard contracts and #[[file:../../CONTRIBUTING.md]] for how to add a directory. Run the local checks in AGENTS.md section 6 before opening an MR.
