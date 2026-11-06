#!/usr/bin/env python3
"""check-exposure — assert this repository discloses nothing about what serves it.

Usage:
    python3 scripts/check-exposure.py [--all]
Exit 0 = clean, 1 = disclosure found.

WHY THIS ONE CAN LIVE HERE

Everything git tracks here ships: the Go source is published, every SKILL.md is
compiled into the binary, and the tests ship with the source. So the tree needs a
check for references that describe the upstream implementation rather than the API
a caller can use.

The export side already has one, and it cannot move here: its rules are a list of
the concrete internal systems, hosts and endpoints that must never appear, and
that list IS the thing being guarded — a guard that names its own secret is worse
than no guard.

This check is the half that CAN live here, and the difference is deliberate:
every rule below is a SHAPE or an ALLOWLIST, never a denylist of internal names.
`mxc-[a-z-]+` catches an internal service name without naming one. An allowlist of
public hosts catches an internal endpoint without printing it. A CamelCase name
ending in `Dto`/`Controller` that this repository does not declare is an upstream
type, whoever's it is. Nothing here tells a reader what it is keeping out.

WHAT IT SCANS

Every file git tracks, except `vendor/` and this script. Go files are scanned in
their comments and `desc:` tags — the two places prose reaches a reader — while
markdown is scanned outside fenced code blocks. Tests are in scope: they ship.

THREE TIERS, AND WHY

Not every reference to the other side of the wire is worth blocking a merge, and
the api/ packages under skills/ belong to the business teams that wrote them. So
the tiers are drawn by SENSITIVITY, not by tidiness:

  HARD  — blocks, no excuse: an internal service name, a non-public host, a query
          or column from upstream storage. Each of these hands a reader something
          about the infrastructure, and none of them is ever needed to describe a
          request a caller makes.
  SOFT  — blocks, but a line that must carry the shape to state a rule is accepted
          by ending it with `exposure-ok` (the same idiom check-boundary uses).
  NOTE  — reported and does NOT fail: an upstream type name, the upstream language
          or its annotations, a shipped open question, a pointer to a document the
          published tree does not carry. These are worth seeing in review and are
          not worth rewriting someone else's file over, so the exit code ignores
          them. Fix them when you are already editing that file.
"""

# Kept to the same Python floor as the other checks in scripts/: no annotations (PEP
# 585 generics need 3.9) and no capture_output=/text= (3.7). The shared CI runner
# is older than both.
import re
import subprocess
import sys
from pathlib import Path


def force_utf8_stdio():
    """Emit UTF-8 whatever code page the console is set to.

    Without this, the "✓" / "·" / "—" printed below raise UnicodeEncodeError on
    a GBK console, and this gate exits 1 with a traceback AFTER passing. See
    check-boundary.py's copy for the full account; these scripts are standalone
    by design and share no module.
    """
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


ROOT = Path(__file__).resolve().parent.parent
MARKER = "exposure-ok"
SELF = "scripts/check-exposure.py"
# .internal/ is excluded from the sync to the public repository as a whole, so
# what it names is never published. It is also exactly where the internal
# platform's image, CI configuration and release tooling live, which name hosts
# and services by necessity; scanning it would only teach people to ignore red.
INTERNAL_DIR = ".internal/"

# Hosts a published tree may name. Anything else — an internal registry, a build
# host, another git server — is a finding, WITHOUT this file naming it.
# RFC 2606 reserves .example/.test/.invalid/example.com for documentation.
PUBLIC_HOSTS = (
    "mexc.com",              # this exchange, all subdomains
    "localhost", "127.0.0.1", "0.0.0.0",
    "example.com", "example.org", "example.net",
    "golang.org", "go.dev", "pkg.go.dev",
    "github.com", "raw.githubusercontent.com", "githubusercontent.com",
    "npmjs.com", "npmjs.org", "nodejs.org",
    "json-schema.org", "modelcontextprotocol.io", "spec.modelcontextprotocol.io",
    "opensource.org", "apache.org", "creativecommons.org", "unicode.org",
    "w3.org", "ietf.org", "rfc-editor.org", "semver.org",
    "keepachangelog.com", "spec.commonmark.org", "jsonlines.org", "jqlang.github.io",
)
RESERVED_TLDS = (".example", ".test", ".invalid", ".localhost")

# The release path had a path-keyed exemption here while it carried a registry
# address as a literal. Nothing names a registry any longer — publication reads it
# from the publishing environment's npm configuration — so the exemption is gone.
# If a registry address is ever needed here again, that is the finding, not the fix.

URL_RE = re.compile(r"(?:https?|wss?)://([A-Za-z0-9._-]+(?:/[A-Za-z0-9._/-]*)?)")

# Acronym-and-number pairs that look like an issue key but are not.
#
# CVE- belongs here for the same reason RFC- and IEEE- do: this rule exists to
# catch "a tracker a reader cannot open", and a CVE identifier is public at
# cve.org. Naming one is how a comment says which vulnerability changed a
# platform's behaviour, which is exactly the kind of thing a reader needs to be
# able to look up.
NOT_TICKETS = ("ISO-", "RFC-", "UTF-", "SHA-", "ANSI-", "ASCII-", "IEEE-", "PBKDF-", "AES-", "CVE-")

# CamelCase suffixes that mark a type from the service on the other side of the
# wire. A name this repository declares itself is not one of them (see local_types).
UPSTREAM_TYPE_RE = re.compile(
    r"(?<![\w.])([A-Z][A-Za-z0-9]*(?:Dto|DTO|Vo|VO|Req|Request|Response|Controller|Enum|Utils))\b"
)
# Names that legitimately appear in prose here and are nobody's internal type.
TYPE_ALLOW = {"Request", "Response", "Req", "Dto", "DTO", "Enum", "Utils", "Vo", "VO"}


def ANY(rel):
    return True


def SHIPPING(rel):
    """The Go source is published and every SKILL.md is compiled into the binary.

    A repository document cross-referencing another one is not a disclosure: a
    reader of the repository can follow it. The same pointer inside a shipped
    file resolves to nothing.
    """
    return rel.endswith(".go") or (rel.startswith("skills/") and rel.endswith(".md"))


RULES_HARD = (
    ("upstream service name", ANY,
     re.compile(r"\bmxc-[a-z0-9]+(?:-[a-z0-9]+)*|\b[a-z][a-z0-9]*(?:-[a-z0-9]+){1,}\s+service\b"),
     "which service serves a path is not a fact a caller sends"),
    ("upstream storage internals", ANY,
     re.compile(r"\b(?:SELECT\s|INSERT\s+INTO|DELETE\s+FROM|LEFT\s+JOIN|INNER\s+JOIN|GROUP\s+BY|ROW_NUMBER)\b"
                r"|\bBETWEEN\s+:[a-z]|:[a-z]+\s+AND\s+:[a-z]"),
     "a query, a column or a bind variable describes storage, not the API"),
)

RULES_SOFT = (
    ("segmentation label", ANY,
     re.compile(r"\b(?:C-side|C-end|B-side|B-end|toB|toC)\b"),
     "an internal market-segmentation label says nothing about the capability it labels"),
    ("issue key", ANY,
     re.compile(r"\b(?!" + "|".join(t.rstrip("-") + "-" for t in NOT_TICKETS) + r")"
                r"[A-Z]{2,10}[-_][0-9]{3,}\b"),
     "an issue key is a tracker a reader cannot open"),
    ("gateway internals", ANY,
     re.compile(r"gateway-injected|injected by the gateway|gateway injects|login-exclude"),
     "how identity reaches the service is not something a caller can act on"),
    ("account grading / feature gating", ANY,
     re.compile(r"\bgrayscale\b|\bgray release\b|member allowlist|symbol allowlist|blacklisted member"
                r"|high-frequency (?:account|member)|frozen-asset|A-type account|delay(?:ed)?-settlement"
                r"|sharded member|backend switch"),
     "how accounts are graded and features gated is upstream policy, not caller-visible behaviour"),
)

# Reported, never blocking. See THREE TIERS above.
RULES_NOTE = (
    ("upstream stack", ANY,
     re.compile(r"\b(?:Java|Spring|Jackson|MyBatis|Hibernate)\b"
                r"|@(?:Json[A-Z][A-Za-z]*|NotEmpty|NotNull|NotBlank|Valid|RequestMapping|RequestBody|Param)\b"),
     "the language and framework behind an endpoint are not part of its contract"),
    ("unresolved marker", SHIPPING,
     re.compile(r"<TODO|\bTODO:|\bFIXME\b|\bXXX:|to be confirmed"),
     "an open question shipped to a reader who cannot resolve it"),
    ("pointer to a document that does not ship", SHIPPING,
     re.compile(r"\b(?:AGENTS|CONTRIBUTING|SUBDOMAINS|DESIGN|WRITE-SAFETY)\.md|\bdocs/[A-Za-z_]+\.md"),
     "the published tree carries the source and the skills, not the docs"),
)

findings = []  # (severity, path, line, rule, why)


def tracked():
    # capture_output=/text= need 3.7; the shared CI runner's python3 is older,
    # so use the long-standing stdout=/stderr=PIPE form — the same note
    # check-boundary.py and check-english.py carry for the same reason.
    #
    # Bytes, decoded here as UTF-8, rather than universal_newlines=True: that
    # flag decodes with the LOCALE encoding, which on a Chinese-locale Windows
    # is GBK, and a tracked path git reports in UTF-8 then fails to decode.
    # Paths in this repository are ASCII today, so this is prevention rather
    # than a fix — but the same flag was a live silent-pass bug in
    # check-english.py, and leaving one instance behind invites it back.
    # splitlines() handles a stray \r, which is all universal_newlines added.
    out = subprocess.run(["git", "-C", str(ROOT), "ls-files"],
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                         check=False)
    if out.returncode != 0:
        sys.exit("check-exposure: not a git repository")
    return [p for p in out.stdout.decode("utf-8", "replace").splitlines()
            if not p.startswith(("vendor/", INTERNAL_DIR)) and p != SELF]


def local_types():
    """Every exported identifier this repository's Go CODE mentions.

    A type declared here, a struct field, an imported package's type — all of
    them appear on a line that is not a comment. An upstream class name never
    does: it only ever shows up in prose describing the other side of the wire.
    That asymmetry is the whole test, and it needs no list to maintain.
    """
    names = set()
    ident = re.compile(r"\b([A-Z][A-Za-z0-9]*)\b")
    for rel in tracked():
        if not rel.endswith(".go"):
            continue
        for line in (ROOT / rel).read_text(encoding="utf-8", errors="replace").splitlines():
            if line.lstrip().startswith("//"):
                continue
            names.update(ident.findall(re.sub(r'desc:"[^"]*"', "", line)))
    return names


def prose_lines(rel, text):
    """Yield (lineno, text) for the parts of a file a reader reads as prose.

    Go: comments and `desc:` tags. Markdown: everything outside fenced blocks.
    Anything else: every line.
    """
    fenced = False
    for i, line in enumerate(text.splitlines(), 1):
        if rel.endswith(".md"):
            if line.lstrip().startswith("```"):
                fenced = not fenced
                continue
            if fenced:
                continue
            yield i, line
        elif rel.endswith(".go"):
            stripped = line.lstrip()
            if stripped.startswith("//"):
                yield i, line
            else:
                for m in re.finditer(r'desc:"([^"]*)"', line):
                    yield i, m.group(1)
        else:
            yield i, line


def host_of(url_body):
    return url_body.split("/", 1)[0]


def check_hosts(rel, lineno, line):
    for m in URL_RE.finditer(line):
        body, host = m.group(1), host_of(m.group(1))
        if any(host == h or host.endswith("." + h) for h in PUBLIC_HOSTS):
            continue
        if any(host.endswith(t) for t in RESERVED_TLDS):
            continue
        if any(body.startswith(h) for h in PUBLIC_HOSTS):  # own module path
            continue
        if host in ("<host>", "..."):  # a placeholder, not an address
            continue
        findings.append(("HARD", rel, lineno, "non-public host",
                         f"`{host}` is not a host a published tree may name"))


def check_types(rel, lineno, line, known):
    hit = False
    for m in UPSTREAM_TYPE_RE.finditer(line):
        name = m.group(1)
        if name in known or name in TYPE_ALLOW:
            continue
        findings.append(("NOTE", rel, lineno, "upstream type name",
                         f"`{name}` is a type from the other side of the wire"))
        hit = True
    return hit


def main():
    files = tracked()
    known = local_types()
    for rel in files:
        p = ROOT / rel
        try:
            text = p.read_text(encoding="utf-8")
        except (UnicodeDecodeError, FileNotFoundError):
            continue
        for lineno, line in prose_lines(rel, text):
            marked = MARKER in line
            check_hosts(rel, lineno, line)
            for rule, scope, rx, why in RULES_HARD:
                if scope(rel) and rx.search(line):
                    findings.append(("HARD", rel, lineno, rule, why))
            if marked:
                continue
            for rule, scope, rx, why in RULES_SOFT:
                if scope(rel) and rx.search(line):
                    findings.append(("SOFT", rel, lineno, rule, why))
            for rule, scope, rx, why in RULES_NOTE:
                if scope(rel) and rx.search(line):
                    findings.append(("NOTE", rel, lineno, rule, why))
            check_types(rel, lineno, line, known)

    blocking = [f for f in findings if f[0] in ("HARD", "SOFT")]
    notes = [f for f in findings if f[0] == "NOTE"]

    for sev, rel, lineno, rule, why in blocking:
        print(f"  - [{sev}] {rel}:{lineno} {rule} — {why}")
    if blocking:
        hard = sum(1 for f in blocking if f[0] == "HARD")
        print(f"✗ {len(blocking)} disclosure(s) ({hard} HARD, {len(blocking) - hard} soft)")
        if any(f[0] == "SOFT" for f in blocking):
            print(f"  A soft finding on a line that must carry the shape to state a rule "
                  f"is accepted by ending that line with `{MARKER}`.")
    else:
        print("✓ nothing sensitive about what serves this product is disclosed")

    if notes:
        print(f"\n  {len(notes)} advisory note(s) — not blocking, fix while you are in the file:")
        for _, rel, lineno, rule, why in notes:
            print(f"  · {rel}:{lineno} {rule} — {why}")

    return 1 if blocking else 0


if __name__ == "__main__":
    force_utf8_stdio()
    sys.exit(main())
