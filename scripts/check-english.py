#!/usr/bin/env python3
"""check-english — enforce the repository's English-only rule.

The primary consumer of this repo is AI Agents, and the hard rule (AGENTS.md
section 0) is that everything WRITTEN INTO the repo is English: Go identifiers,
comments, string literals, log messages, and the commit message. This linter is
the machine gate behind that rule — the pointer files (CLAUDE.md, .cursorrules,
.cursor/rules/, .kiro/steering/) tell the Agent; this script turns the pipeline
red when the Agent (or a human) writes CJK anyway.

Dependency-free (Python3 stdlib only) so it runs on the shared CI runner, same
as check-boundary.py.

Scope, by design:
  - Go source (*.go): the place CJK most often leaks in via comments, and the
    place it is least acceptable. This is the enforced surface.
  - The commit message: Conventional Commits must be English (always, no
    exemption — it describes the change, not skill content).
  - Markdown / docs are NOT scanned: some skill files legitimately carry CJK
    example payloads. Keep those English by review, not by this gate.
  - NO exemptions: every sub-domain in this repository is public and ships as
    open source, so all Go is held to the same rule.

Modes:
  - default (CI): DIFF mode. Only the Go lines a merge request ADDS are checked,
    plus the commit message. Pre-existing CJK in untouched code does not block
    unrelated MRs; new CJK is always caught. This is what CI runs.
  - --all: scan every tracked *.go file in full. Use locally to find and clean
    up the existing backlog. Not run in CI (would red-flag legacy debt).

Exit 0 = clean, 1 = violations found.

Usage:
  python3 scripts/check-english.py              # diff mode (CI default)
  python3 scripts/check-english.py --all        # full scan of all *.go
  python3 scripts/check-english.py --base <ref> # diff against an explicit ref
"""

import argparse
import os
import re
import subprocess
import sys


def force_utf8_stdio():
    """Emit UTF-8 whatever code page the console is set to.

    This gate reports in prose containing "—", and reports VIOLATIONS by echoing
    the offending line — which is CJK by definition. On a GBK console the report
    of a violation is therefore the one thing guaranteed to be unprintable: the
    check would find the problem and then die encoding it. See check-boundary.py
    for the full account.
    """
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


# CJK Unified Ideographs + common CJK punctuation ranges. We deliberately keep
# this to Han + CJK symbols (not, say, full-width Latin) to avoid false hits on
# things like emoji or accented Latin used in a name.
CJK_RE = re.compile(
    "["
    "一-鿿"   # CJK Unified Ideographs
    "㐀-䶿"   # CJK Extension A
    "　-〿"   # CJK symbols and punctuation (，。、《》 etc.)
    "＀-￯"   # full-width forms (，！？（） etc.)
    "]"
)

# vendor/ is third-party source, and some of it legitimately contains CJK: a
# Unicode segmentation table names the characters it classifies. That is the
# library doing its job, not Chinese written into this repository, and `go mod
# vendor` would overwrite any marker added to excuse it. Passed to git as a
# pathspec so both the diff and the --all listing skip it.
EXCLUDE_VENDOR = ":(exclude)vendor/"


def run(cmd):
    """Run a git command, return stdout as text (empty string on failure).

    Use for best-effort probes where a failure legitimately means "absent"
    (e.g. `git rev-parse --verify` on a maybe-missing ref). For a diff whose
    empty output must mean "no CJK" — never "git errored" — use run_checked,
    which distinguishes the two so the caller can fail closed.
    """
    try:
        # capture_output=/text= are 3.7+; the CI runner's python3 is older, so
        # use the long-standing stdout=/stderr=PIPE form (works back to 3.5) to
        # stay portable, and decode the bytes here — see run_checked for why
        # this must not be left to universal_newlines=True.
        out = subprocess.run(
            cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False
        )
        return out.stdout.decode("utf-8", "replace")
    except Exception:
        return ""


class GitError(RuntimeError):
    """A git invocation failed — the caller must fail closed, not treat the
    empty stdout as a clean result."""


def run_checked(cmd):
    """Run a git command, returning stdout, or raise GitError on failure.

    This is the fail-closed counterpart to run(): a non-zero exit or a missing
    git binary raises instead of silently yielding "" — which the diff scanner
    would otherwise read as "nothing added, all clean" and wave CJK through.

    THE DECODE IS PART OF FAILING CLOSED, and universal_newlines=True was a hole
    in it. That flag decodes git's output with the LOCALE encoding; on a
    Chinese-locale Windows that is GBK, and a diff carrying the very characters
    this gate exists to reject is exactly what GBK cannot decode as UTF-8. The
    UnicodeDecodeError was raised in subprocess's reader thread, where neither
    try block below can see it — the call returned, stdout was empty, and
    `if not diff` read that as "genuinely clean". The gate reported OK on a diff
    it never read. Observed on Windows 11 (cp936) before this change.

    So: take bytes, decode UTF-8 STRICTLY, and turn a failure into GitError.
    errors="replace" would be wrong here — U+FFFD is not in CJK_RE, so undecodable
    bytes would again pass as clean. Undecodable output means this gate could not
    read what it was asked to check, and that must fail, not pass.
    """
    try:
        # 3.5-compatible form — see run() for why capture_output/text are avoided.
        out = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             check=False)
    except Exception as exc:
        raise GitError(f"failed to run {' '.join(cmd)}: {exc}") from exc
    if out.returncode != 0:
        raise GitError(
            f"`{' '.join(cmd)}` exited {out.returncode}: "
            f"{out.stderr.decode('utf-8', 'replace').strip()}"
        )
    try:
        return out.stdout.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise GitError(
            f"`{' '.join(cmd)}` produced output that is not UTF-8: {exc}"
        ) from exc


def resolve_base():
    """Figure out the ref to diff against in CI or locally.

    GitLab MR pipelines expose CI_MERGE_REQUEST_DIFF_BASE_SHA (the merge base).
    Fall back to the target branch, then to origin/master, then master.
    """
    base = os.environ.get("CI_MERGE_REQUEST_DIFF_BASE_SHA")
    if base:
        return base
    target = os.environ.get("CI_MERGE_REQUEST_TARGET_BRANCH_NAME")
    if target:
        return f"origin/{target}"
    for ref in ("origin/master", "master"):
        if run(["git", "rev-parse", "--verify", "--quiet", ref]).strip():
            return ref
    return ""


def added_go_lines(base, staged=False):
    """Yield (file, added_line_text) for every ADDED line in *.go in the diff.

    Parses unified diff: tracks the current file from '+++ b/<path>' headers and
    collects '+' lines (excluding the '+++' header). Line numbers are not needed
    for the error — we report the file and the offending text.

    staged=True diffs the index against `base` (`--cached`) — i.e. exactly what
    the commit will contain — so a pre-commit hook ignores unstaged edits and
    catches only what is actually being committed.

    Fail-closed: git errors raise GitError up to main(), which exits non-zero.
    A silent "" here would read as "nothing added" and wave CJK through — the
    exact hole that let an unfetched base SHA pass as clean. `--src-prefix`/
    `--dst-prefix` pin the `a/`,`b/` prefixes so a user's diff.noprefix /
    diff.mnemonicPrefix config can't break the `+++ b/` parse (silent miss).
    """
    args = [
        "git", "diff", "--unified=0", "--no-color",
        "--src-prefix=a/", "--dst-prefix=b/",
    ]
    if staged:
        args.append("--cached")
    if base:
        args.append(base)
    diff = run_checked(args + ["--", "*.go", EXCLUDE_VENDOR])
    if not diff:
        # git succeeded and reported no added *.go lines — genuinely clean.
        return
    current = None
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            current = line[len("+++ b/"):]
        elif line.startswith("+++ "):
            current = None
        elif line.startswith("+") and not line.startswith("+++"):
            if current and current.endswith(".go"):
                yield current, line[1:]


def all_go_files():
    """Every tracked *.go file we author (for --all mode)."""
    out = run(["git", "ls-files", "*.go", EXCLUDE_VENDOR])
    return [f for f in out.splitlines() if f.strip()]


def check_diff(base, staged=False):
    violations = []
    for path, text in added_go_lines(base, staged=staged):
        if CJK_RE.search(text):
            violations.append((path, text.strip()))
    return violations


def check_all():
    violations = []
    for path in all_go_files():
        try:
            with open(path, encoding="utf-8") as fh:
                for i, line in enumerate(fh, 1):
                    if CJK_RE.search(line):
                        violations.append((f"{path}:{i}", line.strip()))
        except (OSError, UnicodeDecodeError):
            continue
    return violations


def check_commit_message():
    """Check the tip commit message in CI (the MR's latest commit)."""
    if not os.environ.get("CI"):
        return []
    msg = run(["git", "log", "-1", "--format=%B"])
    bad = [ln for ln in msg.splitlines() if CJK_RE.search(ln)]
    return [("<commit message>", ln.strip()) for ln in bad]


def main():
    ap = argparse.ArgumentParser(description="Enforce English-only in Go and commit messages.")
    ap.add_argument("--all", action="store_true", help="full scan of every tracked *.go file")
    ap.add_argument("--base", default=None, help="diff against this git ref (default: auto-detect)")
    ap.add_argument("--staged", action="store_true",
                    help="diff the index (--cached) against base — for a pre-commit hook, "
                         "so only what will be committed is checked, not unstaged edits")
    args = ap.parse_args()

    if args.all:
        violations = check_all()
        scope = "all tracked *.go files"
    else:
        base = args.base if args.base is not None else resolve_base()
        # git failures inside the diff walk raise GitError — fail closed rather
        # than let an empty result pass as "clean" (an unfetched base SHA used
        # to slip through this way).
        try:
            violations = check_diff(base, staged=args.staged) + check_commit_message()
        except GitError as exc:
            print(f"check-english: ERROR — git failed, cannot verify English-only: {exc}",
                  file=sys.stderr)
            print("Refusing to pass without a real diff. Fetch the base ref "
                  "(CI: ensure full history / a valid CI_MERGE_REQUEST_DIFF_BASE_SHA), "
                  "then retry.", file=sys.stderr)
            return 1
        staged_note = " (staged)" if args.staged else ""
        scope = f"added Go lines{staged_note} vs {base or '(no base found)'} + commit message"

    if not violations:
        print(f"check-english: OK — no CJK in {scope}.")
        return 0

    print(f"check-english: FAIL — CJK characters found ({scope}).", file=sys.stderr)
    print("This repo is English-only (see AGENTS.md section 0). Rewrite these in English:\n", file=sys.stderr)
    for where, text in violations:
        print(f"  {where}: {text}", file=sys.stderr)
    print(f"\n{len(violations)} offending line(s). Fix them, or use `--no-verify` only for an emergency local commit.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    force_utf8_stdio()
    sys.exit(main())
