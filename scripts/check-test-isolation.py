#!/usr/bin/env python3
"""Every test that redirects the home directory must redirect it on BOTH platforms.

Tests isolate themselves from a developer's real state by pointing the home
directory at a temporary one. `HOME` alone does that on Unix and does NOTHING on
Windows: os.UserHomeDir reads USERPROFILE there (os/file.go), so a suite that
sets only HOME runs against the developer's actual ~/.mexc — writing profiles
into it, changing modes, removing key files.

That failure is invisible on the platform this repository is developed on, which
is exactly the kind of rule worth enforcing mechanically. It is a Python check
rather than a Go test for the same reason the other checks here are: the shared
CI runner has no Go toolchain, and this can run there.

The rule: every Setenv of one of the two variables must have the other set within
a few lines, bound to the SAME expression. Binding to two separate t.TempDir()
calls satisfies "both are set" while quietly naming two different directories.
"""

import pathlib
import re
import sys


def force_utf8_stdio():
    """Emit UTF-8 whatever code page the console is set to.

    The "·" and "—" printed below have no GBK encoding, so on a Chinese-locale
    Windows this gate raises while reporting rather than while checking. Doubly
    worth fixing here: this is the check that exists BECAUSE tests behave
    differently on Windows, so Windows is where it most needs to run. See
    check-boundary.py for the full account.
    """
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


REPO = pathlib.Path(__file__).resolve().parent.parent
SETENV = re.compile(r'Setenv\("(HOME|USERPROFILE)",\s*([^)]+)\)')
# How far apart the pair may sit. They are written adjacent; the slack is for a
# comment or an `if err :=` wrapper between them.
WINDOW = 8


def check(path):
    problems = []
    lines = path.read_text(encoding="utf-8").split("\n")
    hits = [
        (i, m.group(1), m.group(2).strip())
        for i, line in enumerate(lines)
        for m in [SETENV.search(line)]
        if m
    ]
    used = set()
    for index, (line_no, name, value) in enumerate(hits):
        if index in used:
            continue
        partner = "USERPROFILE" if name == "HOME" else "HOME"
        match = None
        for other in range(len(hits)):
            if other in used or other == index:
                continue
            other_line, other_name, other_value = hits[other]
            if other_name == partner and abs(other_line - line_no) <= WINDOW:
                match = (other, other_value)
                break
        rel = path.relative_to(REPO)
        if match is None:
            problems.append(
                f"{rel}:{line_no + 1} sets {name} with no {partner} within {WINDOW} lines\n"
                f"  Windows reads USERPROFILE and ignores HOME, so this test would run "
                f"against the developer's real home directory."
            )
            continue
        other, other_value = match
        used.add(index)
        used.add(other)
        if other_value != value:
            problems.append(
                f"{rel}:{line_no + 1} binds {name} to {value!r} but {partner} to {other_value!r}\n"
                f"  Both must name the SAME directory; two t.TempDir() calls are two directories."
            )
    return problems


def main():
    problems = []
    for path in sorted(REPO.rglob("*_test.go")):
        if "vendor" in path.parts:
            continue
        problems.extend(check(path))
    if problems:
        print("check-test-isolation: home-directory isolation is not platform-complete\n")
        for problem in problems:
            print(f"  · {problem}")
        return 1
    print("check-test-isolation: OK — every home redirect names both HOME and USERPROFILE.")
    return 0


if __name__ == "__main__":
    force_utf8_stdio()
    sys.exit(main())
