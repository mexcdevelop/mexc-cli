#!/usr/bin/env python3
"""check-boundary — enforce the skill-library taxonomy invariants.

Language-agnostic structural linter for the mexc-cli skill library.
Run from repo root: `python3 scripts/check-boundary.py`
Exit 0 = clean, 1 = violations found.

Taxonomy (see AGENTS.md §4 "The Contract for Writing a SKILL.md" and
SUBDOMAINS.md) — directory
main axis is `skills/<domain>/<sub>/SKILL.md`, one level of sub-domain with
no common/biz split. Classification rides on five orthogonal dimensions:
  - scope:      resource identifier `<domain>.<sub>` (NO action suffix)
  - access:     action verb, an operation-level tag (read | write | … )
  - signed:     subdomain-level frontmatter tag (yes | no): yes iff the
                subdomain's default signing scheme (`const Signed` in its api
                package) is not none. An op may override the scheme with a
                concrete api.Sign* constant (tri-state: omit = inherit).
  - identity:   subdomain-level frontmatter tag (yes | no): yes iff the
                subdomain carries an identity (`const Identity` != none).
                WHICH identity is in the const; the frontmatter records only
                that one is required.
  - visibility: a SUBDOMAIN-level frontmatter tag. Every sub-domain here is
                `public`; the tag is kept because it is the axis the sub-domain
                registries share (Invariant 8).
Authorization is `resource(scope) × action(access)`, AWS-IAM style. The
action axis is an open enum — beyond read/write it may grow admin /
cancel-only / history-only, so `access` is NOT a boolean.

This repository builds ONE product and is tag-free: what the binary carries is
decided by the import graph, from the topology `editions/public` declares. The
one thing the import graph cannot decide is //go:embed — it reads files off disk,
ignoring imports — so the embed manifest is asserted against them (Invariant 9).

Invariants:
  1. Every sub-domain dir must have a SKILL.md.
  2. Every sub-domain must be registered in SUBDOMAINS.md.
  3. Every SKILL.md frontmatter must be complete and well-formed:
     name == `mexc-<domain>-<sub>` (namespaced install id), scope ==
     `<domain>.<sub>` (resource only, no action suffix), signed ∈ {yes, no},
     identity ∈ {yes, no}, visibility ∈ {public, internal},
     description non-empty.
  4. Every SKILL.md must carry a permission table. Every scope token in it
     must equal the sub-domain's resource scope `<domain>.<sub>`; the action
     lives in a separate `access` column, verb ∈ VALID_ACCESS.
  5. Operational docs must stay self-contained: no reference to an external
     project as a benchmark (see AGENTS.md §5 item 5, self-contained rule).
     No file is exempt; a line that must name a token to define the rule
     marks itself `self-contained-ok`.
  6. Closed-enum discipline in api declarations (the CI half of the
     closed-enum contract — the Go tests are the other half, but this shared
     runner cannot run Go):
     6a. No string literal on the enum fields: `Signed:`, `Identity:`, and
         `In:` in a skills api file must reference api.* constants, never a
         quoted string.
     6b. Every referenced constant must exist in pkg/api (the legal sets are
         EXTRACTED from pkg/api/operation.go, so adding a constant there is
         automatically picked up here — single source of truth).
     6c. Every subdomain api package must declare `const Signed = api.<X>` and
         `const Identity = api.<Y>` (its defaults), and the frontmatter must
         mirror them as yes/no facts: signed == yes iff Sign != SignNone,
         identity == yes iff Identity != IdentityNone (which identity is in
         the const, and is not asserted here).
  7. No signing-algorithm name in a user-facing SKILL.md (hmac / sha256 /
     ed25519 / …): the scheme is chosen in pkg/api (Go) and applied by the
     framework; the skill only says whether an op is signed, never how.
  8. Every sub-domain here must declare `visibility: public`. This repository
     builds one product and publishes its whole tree, so a sub-domain marked
     otherwise is simply in the wrong repository. Asserted in
     check_embed_manifest(), which already buckets every skill by visibility.
  9. embed.go embeds EXACTLY the skill files on disk — every SKILL.md plus its
     references/ scripts/ assets/ subtrees, and no api/*.go. //go:embed reads
     files off disk regardless of the import graph, so the manifest is the only
     thing deciding which skill bytes reach the binary; a skill on disk but
     missing from it ships a binary that cannot install it. The reverse (a
     manifest line naming a file that does not exist) is already fatal at
     compile time, so it is the compiler's job, not this check's.
 10. Editions compose against ports only: no editions/* source imports ANY
     internal/ package — an edition uses framework/ + the pkg/ ports (transport
     via pkg/transport.Sender), never module-private machinery.
 11. No build tag: no .go file carries an internal/!internal build constraint.
     This repository builds one product, so there is nothing to select between.
 12. The no-disclosure rule — enforced by check-exposure.py (shape rules) and by
     the release flow (the names). Not a check here; see the note above main().
 13. On-disk state lives where the PRODUCT says it lives: only
     pkg/config/statedir.go may ask the OS for a per-user directory. No other
     tracked non-test .go may call os.UserHomeDir / UserConfigDir / UserCacheDir
     or read $HOME.
 14. No operation declares Permission: checking permission codes needs
     credentials carrying a grant map, and this product's only identity is an
     api key, whose permissions are not readable from the credential.
 15. A frontmatter `description` holding a colon must be quoted or a block
     scalar. Unquoted it is invalid YAML, and `npx skills` then drops the whole
     skill without saying so.
 16. pkg/ declares no product host key, signing scheme or identity. Those are
     the product's vocabulary (signing/, identity/, its adapter package) and are
     injected; pkg/ holds the port and the orchestration.
 17. No user-facing Go string hard-codes the binary name. Every product built on
     this module ships its own binary, so a literal command name is wrong in the
     others — and in any renamed or symlinked invocation. Use cliname.Name(),
     which framework.Run fills from argv[0], the same source the command paths
     use. Both literal forms are checked (interpreted and raw), and skills/ is in
     scope as well as the engine. A line whose preceding comment carries
     `binary-name-ok` is excused for a name that only looks like the binary (an
     artifact repository, say).
 18. No SKILL.md names the retired binary `mexc-cli`. 17 covers Go and misses the
     file a business author actually writes: a SKILL.md operation table and its
     recipes are literal commands, and an agent runs what the skill prints, so a
     retired name is a command that does not exist. Any occurrence fails except
     the module path (read from go.mod) and the internal project name
     `mexc-cli-customer`, which are identities rather than commands;
     `binary-name-ok` excuses a line deliberately.
 19. Every signed sub-domain uses signing.CliHMAC. No .go under skills/ or
     editions/ may assign the raw "spot-hmac" literal to a `Signed:` field. The
     constant it replaced is gone, so naming it is already a compile error and
     the string literal is the only form left that the Go compiler accepts. 6a
     rejects the same line as an enum-field literal; this rule is what names the
     scheme, so the message says what to use instead.
 20. Every sub-domain hands off to the shared skills it relies on. An agent loads
     one sub-domain skill and operates from it, so a missing pointer is a rule it
     never meets: `mexc-common` (error envelope, --read-only, and the rule that a
     write needs the USER's confirmation and is never self-approved) is required
     when the sub-domain declares a write (`Access: api.Write` in its api
     package) OR carries an identity; `mexc-auth`
     (the credential flow) is required when it carries an identity, since no
     sub-domain skill describes login. Both pointers are prose — the Auth
     blockquote in skill-template/SKILL.md — so only this check keeps a new
     sub-domain from shipping without them. A public sub-domain with no identity
     and no write is exempt from both.

Number 12 is not skipped and not this file's: it is the no-disclosure rule,
split between check-exposure.py and the release flow — see the note above
main().
"""

import re
import subprocess
import sys
from pathlib import Path


def force_utf8_stdio():
    """Emit UTF-8 whatever code page the console is set to.

    THE FAILURE THIS FIXES. Python picks the process's ANSI code page for
    stdout, which on a Chinese-locale Windows is GBK. The marks this gate prints
    ("✓", "✗", "·", "—") have no GBK encoding, so the print RAISES — after every
    invariant has already been checked and passed. The script then exits 1 with
    a UnicodeEncodeError traceback and no findings, and the pre-commit hook
    reads that exit as a violation.

    That is the worst shape a gate can fail in: it blocks every commit from that
    machine, names nothing to fix, and points at a print statement rather than
    at any rule. The check was never the problem — only the reporting was, and
    only on a code page CI never runs under.

    WHY errors="replace". A console that cannot render a glyph should show a
    substitute, never abort the run. What the hook and CI capture is a pipe,
    which now receives well-formed UTF-8 either way; only a human's terminal
    display is affected, and only for the decorative marks.

    reconfigure() is 3.7+, so the call is guarded — the shared CI runner's
    python3 is older (the same constraint the subprocess calls below carry). It
    is a UTF-8 Linux box, where there was nothing to fix in the first place.

    check-english.py, check-exposure.py and check-test-isolation.py each carry a
    copy: they are standalone scripts by design, with no shared module to hold
    this, and the note lives here in full.
    """
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


ROOT = Path(__file__).resolve().parent.parent
SKILLS = ROOT / "skills"
REGISTRY = ROOT / "SUBDOMAINS.md"

# vendor/ holds third-party source this repository does not author and cannot
# change: `go mod vendor` rewrites it wholesale on the next dependency bump, so
# an inline exemption marker there would not survive. These invariants are about
# what WE write, so the tree-wide collectors skip it. Everything else is already
# scoped to a directory vendor cannot reach (pkg / internal / framework /
# editions / cmd / skills).
#
# Only the tree-wide collectors need this: a repo with no vendor/ is unaffected,
# so the rule costs nothing until a vendored build lands.
VENDOR_DIR = "vendor/"
# `git ls-files` takes it as a pathspec; a filesystem walk takes the prefix.
GIT_EXCLUDE_VENDOR = ":(exclude)vendor/"

# .internal/ holds files that only the internal build platform reads (its image,
# its CI configuration, the release sync tooling). The sync excludes the whole
# directory, so nothing in it is ever published, and the invariants here — which
# are about the published tree — do not apply to it.
INTERNAL_DIR = ".internal/"
GIT_EXCLUDE_INTERNAL = ":(exclude).internal/"


def _outside_vendor(rel):
    """True when a repo-relative path is ours to lint (not vendored, not internal)."""
    return not rel.startswith(VENDOR_DIR) and not rel.startswith(INTERNAL_DIR)


def _read_module_path():
    """The module path go.mod declares, so no rule hard-codes it."""
    for line in (ROOT / "go.mod").read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.split()[1]
    sys.exit("check-boundary: go.mod declares no module path")


# The module path names the repository, and the repository is named after the
# product, so it contains the very word Invariants 17 and 18 hunt for. It is a
# package identity, not a command, and both rules strip it from a line before
# matching. Read from go.mod rather than written here so a rename cannot leave
# the exemption behind.
MODULE_PATH = _read_module_path()

VALID_VISIBILITY = ("public", "internal")
VALID_SIGNED = ("yes", "no")
VALID_IDENTITY = ("yes", "no")
# Open enum: read/write today; admin, cancel-only, history-only may follow.
VALID_ACCESS = ("read", "write", "admin", "cancel-only", "history-only")

# Invariant 6: the closed enums live in pkg/api; the legal constant sets are
# extracted from its const blocks, so this script never hand-maintains a copy.
# The enum constants come from TWO packages now. pkg/api holds the axes that are
# universal (the signing schemes, the param locations, and IdentityNone); the
# identities a product actually supplies are declared in that product's own
# identity package, because the shared core must not catalogue them. Both are
# extracted, so this script still never hand-maintains a copy.
ENUM_SOURCES = (
    ROOT / "pkg" / "api" / "operation.go",
    ROOT / "identity" / "identity.go",
    ROOT / "signing" / "signing.go",
)
# Matches a const line like:  SignSpotHMAC Signing = "spot-hmac"
# The optional `const ` prefix matters: a value may be declared inside a const
# block (SignNone Signing = "none") or standalone (const IdentityNone Identity =
# "none"), and missing the standalone form silently shrinks the legal set.
ENUM_CONST_RE = re.compile(
    r"^\s*(?:const\s+)?(Sign\w*|Identity\w*|In\w*)\s+(Signing|Identity|ParamIn)\s*=",
    re.MULTILINE,
)
# Matches a product constant declared against the imported type:
#   OpenAPI api.Identity = "openapi"   /   SpotHMAC api.Signing = "spot-hmac"
PRODUCT_CONST_RE = re.compile(
    r"^\s*(?:const\s+)?(\w+)\s+api\.(Identity|Signing)\s*=", re.MULTILINE
)
# Matches an enum field referencing a constant from any of the three packages:
#   Signed: signing.SpotHMAC   /   Identity: identity.OpenAPI   /   In: api.InPath
ENUM_FIELD_CONST_RE = re.compile(r"\b(Signed|Identity|In):\s*(api|identity|signing)\.(\w+)")
# Matches an enum field holding a string literal:     Signed: "spot-hmac"
ENUM_FIELD_LITERAL_RE = re.compile(r"\b(Signed|Identity|In):\s*\"")
# Matches the subdomain default declarations in an api package.
CONST_SIGN_RE = re.compile(r"^const\s+Signed\s*=\s*(?:api|signing)\.(\w+)", re.MULTILINE)
CONST_IDENTITY_RE = re.compile(r"^const\s+Identity\s*=\s*(?:api|identity)\.(\w+)", re.MULTILINE)


def load_api_enums():
    """Extract the legal constant names per enum type from ENUM_SOURCES.

    Returns {"Signing": {...}, "Identity": {...}, "ParamIn": {...}} of constant
    NAMES (SignNone, InPath, OpenAPI, ...). The zero values are deliberately
    unnamed in Go (omit-to-inherit / omit-for-query), so every extracted name is
    a concrete, writable value.

    Two shapes are read. pkg/api declares typed constants inside a const block
    or standalone (`const SignNone Signing = "none"`); a product package declares
    them against the imported type (`SpotHMAC api.Signing = "spot-hmac"`), which
    the first pattern cannot see. Both feed the same sets, so a sub-domain may
    reference any of the three packages and Invariant 6b still holds.
    """
    sets = {"Signing": set(), "Identity": set(), "ParamIn": set()}
    for src in ENUM_SOURCES:
        if not src.exists():
            fail(f"missing {src.relative_to(ROOT)} (needed to extract enum sets)")
            continue
        text = src.read_text(encoding="utf-8")
        for name, typ in ENUM_CONST_RE.findall(text):
            sets[typ].add(name)
        for name, typ in PRODUCT_CONST_RE.findall(text):
            sets[typ].add(name)
    if not sets["Signing"] or not sets["Identity"]:
        fail("could not extract Signing/Identity constants from "
             f"{', '.join(str(p.relative_to(ROOT)) for p in ENUM_SOURCES)} "
             "(a const-block layout may have changed; update ENUM_CONST_RE / "
             "PRODUCT_CONST_RE)")
    return sets


# Invariant 5: tokens that signal an external-project benchmark in prose.
# No file is exempt: everything this repository tracks is published, so a
# comparison to a project the reader cannot open has no place in any of it. A
# single line that must name a token to DEFINE the rule marks itself with the
# inline `self-contained-ok` comment below.
# Latin tokens are matched on word boundaries so an innocent substring (Clark,
# skylark, larkspur) doesn't trip the gate; CJK tokens use plain substring
# matching because \b word boundaries don't apply to CJK characters.
# The inline escape hatch: a line that must name a forbidden token in order to
# DEFINE the rule ends with this marker.
SELF_CONTAINED_MARKER = "self-contained-ok"
SELF_CONTAINED_FORBIDDEN_LATIN = ("lark", "larksuite")  # self-contained-ok: defines the rule
SELF_CONTAINED_FORBIDDEN_CJK = ("飞书", "对标")  # self-contained-ok: defines the rule
# Compiled word-boundary matchers for the Latin tokens (case-insensitive).
SELF_CONTAINED_LATIN_RE = tuple(
    (tok, re.compile(rf"\b{re.escape(tok)}\b", re.IGNORECASE))
    for tok in SELF_CONTAINED_FORBIDDEN_LATIN
)

# Invariant 7: signing-algorithm names must not appear in a user-facing SKILL.md.
# The scheme is named in Go (signing/, referenced from the api declaration) and
# applied by the framework; the skill only states whether an op is signed
# (frontmatter signed: yes/no), never how.
# Word-boundary, case-insensitive so an innocent substring won't trip the gate.
SKILL_FORBIDDEN_ALGO_RE = tuple(
    (tok, re.compile(rf"\b{re.escape(tok)}\b", re.IGNORECASE))
    for tok in ("hmac", "sha256", "sha-256", "ed25519")
)

# Invariant 8 is asserted in check_embed_manifest(): every sub-domain here must
# declare `visibility: public`. It carries no token list; the scan that needed
# one is out of tree for the reason given above main().

errors = []


def fail(msg):
    errors.append(msg)


def load_registry():
    """Parse subdomain codes from SUBDOMAINS.md markdown tables.

    Rows look like: | `market` | spot market data | `spot.market` | read | public | no |
    We collect every backtick-wrapped token in the first column (the sub-domain
    code). Later columns also hold backtick tokens (scope) but are ignored here.
    """
    if not REGISTRY.exists():
        fail(f"missing registry: {REGISTRY.name}")
        return set()
    codes = set()
    for line in REGISTRY.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line.startswith("|"):
            continue
        first_col = line.split("|")[1].strip()
        m = re.fullmatch(r"`([a-z0-9-]+)`", first_col)
        if m:
            codes.add(m.group(1))
    return codes


def load_registry_axes():
    """Parse each subdomain's signed/identity columns from SUBDOMAINS.md tables,
    keyed by scope (`<domain>.<sub>`, the one globally-unique cell in a row).

    Used by Invariant 2 to keep the registry overview's signed/identity columns
    from silently drifting away from the code const — the per-skill SKILL.md is
    already pinned to the const by Invariant 6c, but nothing pinned this
    hand-maintained overview, so a row could claim `signed: yes` while the code
    (and SKILL.md) said no. signed/identity are always the last two columns; a
    row whose last two cells are not a yes/no pair (a malformed or differently
    shaped table) is skipped rather than misread.
    """
    axes = {}
    if not REGISTRY.exists():
        return axes
    scope_re = re.compile(r"^`([a-z0-9-]+\.[a-z0-9-]+)`$")
    for line in REGISTRY.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip("|").split("|")]
        scope = next((m.group(1) for c in cells
                      for m in [scope_re.fullmatch(c)] if m), None)
        if scope is None:
            continue
        signed, identity = cells[-2], cells[-1]
        if signed in VALID_SIGNED and identity in VALID_IDENTITY:
            axes[scope] = {"signed": signed, "identity": identity}
    return axes


def read_frontmatter(skill_md):
    """Return dict of name/scope/signed/identity/visibility from a SKILL.md frontmatter block."""
    fields = {}
    text = skill_md.read_text(encoding="utf-8")
    if not text.startswith("---"):
        return fields
    block = text.split("---", 2)[1]
    for line in block.splitlines():
        m = re.match(r"^(name|scope|signed|identity|visibility):\s*(.+?)\s*(?:#.*)?$", line)
        if m:
            fields[m.group(1)] = m.group(2).strip()
    return fields


def check_description_yaml_safe(tag, skill_md):
    """Invariant 15: a frontmatter `description` holding a colon must be quoted.

    A value that contains ": " (colon+space) or a
    trailing ":" while UNQUOTED is invalid YAML — a plain scalar cannot hold a
    colon-space. `npx skills` then fails to parse the frontmatter and SILENTLY
    DROPS the whole skill. Require it quoted ('...' / "...") or a block scalar
    (>- / |). This targets that one failure mode, so no YAML library is needed.
    """
    text = skill_md.read_text(encoding="utf-8")
    if not text.startswith("---"):
        return
    block = text.split("---", 2)[1]
    for line in block.splitlines():
        m = re.match(r"^\s*description:\s*(.*)$", line)
        if not m:
            continue
        val = m.group(1).strip()
        if not val or val[0] in "'\"|>":
            return  # quoted, or a block scalar (>- / |), or empty→block next: safe
        if ": " in val or val.endswith(":"):
            fail(f"{tag}: `description` contains a colon but is unquoted — this is "
                 f"invalid YAML and `npx skills` will silently drop the whole skill; "
                 f"wrap the value in single quotes (or use a >- block scalar) "
                 f"(Invariant 15)")
        return


def permission_section(skill_md):
    """Return only the Permissions section text (up to the next heading).

    Validation must read scopes from the permission table alone — prose
    elsewhere may legitimately name *other* sub-domains' resource scopes
    (e.g. account's notes compare against `spot.trade`), which must not be
    mistaken for this skill's own permission entries.

    The section heading is matched case-insensitively against the "Permissions"
    label (English-only, per AGENTS.md §0).
    """
    text = skill_md.read_text(encoding="utf-8")
    body = text.split("---", 2)[2] if text.startswith("---") else text
    lines = body.splitlines()
    out, capturing = [], False
    for line in lines:
        if re.match(r"^#{1,6}\s", line):
            capturing = "permissions" in line.lower()
            continue
        if capturing:
            out.append(line)
    return "\n".join(out)


def read_perm_scopes(skill_md):
    """Return the resource-scope tokens (`<domain>.<sub>`) in the permission table.

    Scope is a resource identifier with exactly two dot-segments and no action
    suffix. The trailing `(?!\\.)` rejects any leftover three-segment token
    (e.g. a stale `spot.trade.write`).
    """
    return set(re.findall(r"`([a-z0-9-]+\.[a-z0-9-]+)`(?!\.)", permission_section(skill_md)))


def read_access_values(skill_md):
    """Return (has_column, values) for the permission table's `access` column.

    Invariant 4 requires the action axis to live in its own column rather
    than baked into the scope string — AND that each value be a known verb.
    We locate the `access` header cell, then collect that column's value from
    every subsequent data row (skipping the `|---|` separator). Markdown leads
    with an empty cell before the first pipe, so column indices align across
    header and data rows via the same split.
    """
    lines = permission_section(skill_md).splitlines()
    col = None
    values = set()
    for line in lines:
        if "|" not in line:
            continue
        cells = [c.strip() for c in line.split("|")]
        if col is None:
            if "access" in [c.lower() for c in cells]:
                col = [c.lower() for c in cells].index("access")
            continue
        # separator row like | --- | --- |
        if set("".join(cells).replace("-", "").replace(":", "")) <= {" ", ""} and "-" in line:
            continue
        if col < len(cells) and cells[col]:
            values.add(cells[col])
    return (col is not None, values)


def check_frontmatter(domain_name, sub, skill_md):
    """Invariant 3: frontmatter is complete and well-formed."""
    fm = read_frontmatter(skill_md)
    tag = f"[{domain_name}] {sub}"

    text = skill_md.read_text(encoding="utf-8")
    has_desc = bool(re.search(r"^\s*description:\s*\S", text, re.MULTILINE))

    # The skill name is namespaced with a `mexc-` prefix so the INSTALLED skill id
    # (npx skills registers by this frontmatter name) does not collide with another
    # vendor's `spot-trade` / `account` in a shared agent skills dir, and matches
    # the export dir name buildSkillPlans derives (mexc-<domain>-<sub>). scope stays
    # the bare resource id.
    want_name = f"mexc-{domain_name}-{sub}"
    want_scope = f"{domain_name}.{sub}"  # resource only, no action suffix

    if not fm.get("name"):
        fail(f"{tag}: frontmatter missing `name`")
    elif fm["name"] != want_name:
        fail(f"{tag}: name `{fm['name']}` should be `{want_name}`")

    if not fm.get("scope"):
        fail(f"{tag}: frontmatter missing `scope`")
    elif fm["scope"] != want_scope:
        fail(f"{tag}: scope `{fm['scope']}` should be `{want_scope}`")

    if not fm.get("signed"):
        fail(f"{tag}: frontmatter missing `signed`")
    elif fm["signed"] not in VALID_SIGNED:
        fail(f"{tag}: signed `{fm['signed']}` must be yes|no")

    if not fm.get("identity"):
        fail(f"{tag}: frontmatter missing `identity`")
    elif fm["identity"] not in VALID_IDENTITY:
        fail(f"{tag}: identity `{fm['identity']}` must be yes|no")

    if not fm.get("visibility"):
        fail(f"{tag}: frontmatter missing `visibility`")
    elif fm["visibility"] not in VALID_VISIBILITY:
        fail(f"{tag}: visibility `{fm['visibility']}` must be public|internal")

    if not has_desc:
        fail(f"{tag}: frontmatter missing non-empty `description`")

    # Invariant 15 rides along here: completeness (3) and YAML-parseability (15)
    # are both facts about this one frontmatter block, read from the same file.
    check_description_yaml_safe(tag, skill_md)


ENUM_TYPE_OF_FIELD = {"Signed": "Signing", "Identity": "Identity", "In": "ParamIn"}


def check_enum_discipline(domain_name, sub, api_dir, fm, enums):
    """Invariant 6: closed-enum discipline in the subdomain's api package.

    6a: no string literal on Signed:/Identity:/In: fields — only api.* constants.
    6b: every referenced api.* constant exists in the extracted legal set.
    6c: `const Signed = api.<X>` / `const Identity = api.<Y>` are declared, legal,
        and mirrored by the frontmatter (signed == (X != SignNone); identity ==
        Y with the `Identity` prefix stripped and lowercased).
    """
    tag = f"[{domain_name}] {sub}"
    if not api_dir.is_dir():
        fail(f"{tag}: missing api/ package (expected Operation declarations)")
        return

    const_sign, const_identity = None, None
    for go in sorted(api_dir.glob("*.go")):
        if go.name.endswith("_test.go"):
            continue  # tests may quote enum spellings when asserting on them
        for i, line in enumerate(go.read_text(encoding="utf-8").splitlines(), 1):
            code = line.split("//", 1)[0]  # ignore trailing comments
            if ENUM_FIELD_LITERAL_RE.search(code):
                fail(f"{tag}: {go.name}:{i} assigns a string literal to an enum "
                     f"field — use the api.Sign*/api.Identity*/api.In* constants "
                     f"(closed enums, defined only by the CLI platform)")
            for field, pkg, const in ENUM_FIELD_CONST_RE.findall(code):
                legal = enums[ENUM_TYPE_OF_FIELD[field]]
                if const not in legal:
                    fail(f"{tag}: {go.name}:{i} references {pkg}.{const}, which is "
                         f"not a defined {ENUM_TYPE_OF_FIELD[field]} constant "
                         f"(known: {', '.join(sorted(legal))})")
            m = CONST_SIGN_RE.match(code.strip())
            if m:
                const_sign = m.group(1)
            m = CONST_IDENTITY_RE.match(code.strip())
            if m:
                const_identity = m.group(1)

    # 6c: subdomain defaults declared, legal, and mirrored by the frontmatter.
    if const_sign is None:
        fail(f"{tag}: api package must declare `const Signed = <api|signing>.<scheme>` "
             f"(the subdomain's default signing scheme)")
    elif const_sign not in enums["Signing"]:
        fail(f"{tag}: `const Signed = {const_sign}` is not a defined scheme "
             f"(known: {', '.join(sorted(enums['Signing']))})")
    elif fm.get("signed") in VALID_SIGNED:
        want = "no" if const_sign == "SignNone" else "yes"
        if fm["signed"] != want:
            fail(f"{tag}: frontmatter signed={fm['signed']} but `const Signed = "
                 f"api.{const_sign}` implies signed={want}")

    if const_identity is None:
        fail(f"{tag}: api package must declare `const Identity = <api|identity>.<identity>` "
             f"(the subdomain's default login identity)")
    elif const_identity not in enums["Identity"]:
        fail(f"{tag}: `const Identity = {const_identity}` is not a defined identity "
             f"(known: {', '.join(sorted(enums['Identity']))})")
    elif fm.get("identity") in VALID_IDENTITY:
        # Frontmatter identity is a yes/no fact ("carries an identity"): the
        # constant carries which one; the doc only mirrors whether an identity is
        # required at all, not which. IdentityNone is the sole "no" — it is the
        # one identity pkg/api names, and every product identity means "yes".
        want = "no" if const_identity == "IdentityNone" else "yes"
        if fm["identity"] != want:
            fail(f"{tag}: frontmatter identity={fm['identity']} but `const Identity "
                 f"= {const_identity}` implies identity={want}")


def tracked_markdown():
    """Return the repo-tracked *.md paths (staged files included), or None when
    git is unavailable. Scoping invariant 5 to tracked files keeps local
    scratch drafts (git-ignored, never part of the repo's docs) out of the
    gate, while CI — a clean checkout — still scans everything committed.
    check-english.py uses the same git-based scoping.

    subprocess args stay in the 3.5-compatible stdout=/stderr=PIPE form
    (capture_output needs 3.7; the shared CI runner is older — same note as
    check-english.py)."""
    try:
        out = subprocess.run(
            ["git", "-C", str(ROOT), "ls-files", "-z", "--", "*.md", GIT_EXCLUDE_VENDOR,
             GIT_EXCLUDE_INTERNAL],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
        )
    except OSError:
        return None
    if out.returncode != 0:
        return None
    return [ROOT / p for p in out.stdout.decode("utf-8").split("\0") if p]


def check_self_contained():
    """Invariant 5: no external-project benchmark in operational docs."""
    files = tracked_markdown()
    if files is None:  # no git: fall back to scanning the working tree
        files = sorted(md for md in ROOT.rglob("*.md")
                       if _outside_vendor(md.relative_to(ROOT).as_posix()))
    for md in sorted(files):
        if not md.exists():  # tracked but deleted in the working tree
            continue
        rel = md.relative_to(ROOT).as_posix()
        if rel.startswith(".git/"):
            continue
        for i, line in enumerate(md.read_text(encoding="utf-8").splitlines(), 1):
            # An inline marker exempts a line that must name the forbidden tokens
            # to *define* the rule itself (e.g. AGENTS.md §5 self-contained rule).
            if SELF_CONTAINED_MARKER in line:
                continue
            hit = None
            for tok, rx in SELF_CONTAINED_LATIN_RE:  # self-contained-ok: iterates the rule tokens
                if rx.search(line):
                    hit = tok
                    break
            if hit is None:
                for tok in SELF_CONTAINED_FORBIDDEN_CJK:  # self-contained-ok: iterates the rule tokens
                    if tok in line:
                        hit = tok
                        break
            if hit is not None:
                fail(f"[self-contained] {rel}:{i} mentions `{hit}` — "
                     f"operational docs must not reference an external project "
                     f"as a benchmark; if this line defines the rule itself, "
                     f"append `{SELF_CONTAINED_MARKER}`")


def check_no_algo_names(domain_name, sub, skill_md):
    """Invariant 7: no signing-algorithm name leaks into the user-facing SKILL.md."""
    tag = f"[{domain_name}] {sub}"
    for i, line in enumerate(skill_md.read_text(encoding="utf-8").splitlines(), 1):
        for tok, rx in SKILL_FORBIDDEN_ALGO_RE:
            if rx.search(line):
                fail(f"{tag}: SKILL.md:{i} names a signing algorithm (`{tok}`) — "
                     f"algorithm names live only in pkg/api (Go); a skill states "
                     f"only whether an op is signed (signed: yes/no), never how")


# Invariant 18: no SKILL.md names the retired binary.
#
# Invariant 17 states this rule for Go, and its own comment says "a business
# author writing a command example there is exactly who this rule is for" — and
# then globs `*.go`, which is not the file that author writes. A SKILL.md is: its
# operation table and its step-by-step recipes are made of literal commands, and
# an agent reads them and runs them verbatim. A retired name there is not a stale
# doc, it is a command that does not exist, handed straight to the caller.
#
# Unlike Invariant 17 this needs no shape heuristic. `mexc-cli` is retired
# outright, so ANY occurrence is wrong — with one exception that is not a command
# at all: the module path (MODULE_PATH, stripped from the line before matching),
# and the internal project name `mexc-cli-customer`, which the lookahead refuses
# to match. The `binary-name-ok` marker (shared with Invariant 17) excuses
# a line that must carry the old name for some other reason.
OLD_BINARY_NAME_RE = re.compile(r"\bmexc-cli\b(?!-customer)")

# The message carries the fix because of the scale this rule lands at: the
# branches that will first meet it hold 89-295 occurrences each, and a finding
# that only says "wrong" invites 300 hand edits.
#
# Two details in it were each wrong on the first attempt, and both were found by
# running the command rather than reading it:
#
#   - it must NOT anchor on a backtick. Converting only "`mexc-cli " left 38 of
#     90 in place, because a SKILL.md writes commands two ways: inline in a table
#     cell (backticked) and bare inside a fenced block. The pattern here is the
#     checker's own regex, so the fix and the rule cannot disagree — perl rather
#     than sed because BSD sed has no lookahead and the internal project name
#     `mexc-cli-customer` has to survive.
#   - it must convert api/contract_test.go in the SAME pass. Each one greps the
#     SKILL.md table for "`<binary> <domain> <sub> <op>`", so a SKILL.md-only
#     conversion leaves a regex that matches nothing and a test that reports
#     every op as missing from the table — an error naming the operations, which
#     points nowhere near a renamed binary. Measured: converting both leaves 0
#     findings and the tests pass; converting only the SKILL.md fails with
#     `op "list" is in code but not in the SKILL.md operation table`.
OLD_BINARY_FIX = (
    "git ls-files 'skills/*/SKILL.md' 'skills/*/*/SKILL.md' "
    "'skills/*/*/api/contract_test.go' | "
    r"xargs perl -pi -e 's/\bmexc-cli\b(?!-customer)/mexc/g'"
    "  (convert the contract tests in the same pass — their table regex carries "
    "the binary name too, and matches nothing if only the SKILL.md moves)"
)


def check_no_old_binary_name(domain_name, sub, skill_md):
    """Invariant 18: no SKILL.md names the retired binary."""
    tag = f"[{domain_name}] {sub}"
    for i, line in enumerate(skill_md.read_text(encoding="utf-8").splitlines(), 1):
        if BINARY_NAME_MARKER in line:
            continue
        if OLD_BINARY_NAME_RE.search(line.replace(MODULE_PATH, "")):
            fail(f"{tag}: SKILL.md:{i} names the retired binary `mexc-cli` — this "
                 f"product's binary is `mexc`, and an agent runs what a skill "
                 f"prints. Fix: {OLD_BINARY_FIX}\n"
                 f"         {line.strip()[:120]}")


# The two shared skills a sub-domain must hand off to instead of restating them.
# COMMON_SKILL holds the error envelope, --read-only, and the write-confirmation
# contract; AUTH_SKILL owns the credential flow (the exact subtype, message and
# recovery for a failed_precondition). Named here as the tokens Invariant 20
# looks for.
COMMON_SKILL = "mexc-common"
AUTH_SKILL = "mexc-auth"

# The write fact comes from the api package, NOT from the SKILL.md permission
# table. That table is prose a human maintains and nothing reconciles ITS access
# column with the code -- the Go contract tests reconcile the OPERATIONS table,
# which is a different one -- so a `write` mistyped as `read` there would quietly
# switch this rule off for the sub-domain that needs it most. `Access: api.Write`
# is what the CLI actually dispatches on, so the requirement cannot be lost by
# editing a document.
ACCESS_WRITE_RE = re.compile(r"\bAccess:\s*api\.Write\b")


def declares_write(api_dir):
    """True when the sub-domain's api package declares at least one write op.

    A missing api/ yields False rather than an error of its own: Invariant 6
    already fails loudly on it, and naming the same broken sub-domain twice helps
    nobody.
    """
    if not api_dir.is_dir():
        return False
    for go in sorted(api_dir.glob("*.go")):
        if go.name.endswith("_test.go"):
            continue
        for line in go.read_text(encoding="utf-8").splitlines():
            if ACCESS_WRITE_RE.search(line.split("//", 1)[0]):
                return True
    return False


def check_shared_skill_pointers(domain_name, sub, skill_md):
    """Invariant 20: a sub-domain hands off to the shared skills it relies on.

    An agent loads ONE sub-domain skill and operates from it. The shared skills
    hold what that skill deliberately does not repeat, so a sub-domain that never
    names them is one an agent can drive to completion without ever meeting those
    rules:

      - `mexc-common` -- how errors are shaped, what --read-only refuses, and
        above all that a write needs the USER's confirmation and must never be
        self-approved with --yes / confirm=true. Required when the sub-domain
        declares a write OR carries an identity.
      - `mexc-auth` -- the credential flow. Required when the sub-domain carries
        an identity: without the pointer an agent meeting a credential error has
        nowhere to go, since no sub-domain skill describes login.

    Both pointers are prose -- the Auth blockquote skill-template/SKILL.md ships
    -- so nothing except this check keeps them from being dropped when a new
    sub-domain is written by copying a neighbour and trimming.

    Both facts are read from a source something else already pins -- the
    frontmatter to `const Identity` by Invariant 6c, the write to `Access:
    api.Write`, the declaration the CLI dispatches on -- so neither trigger can
    be edited away in the document this rule guards.

    A public sub-domain with no identity and no write is exempt from both: it
    needs no credential and performs no write. Exempt does not mean forbidden --
    this is a floor, not a ceiling.
    """
    tag = f"[{domain_name}] {sub}"
    fm = read_frontmatter(skill_md)
    has_identity = fm.get("identity") == "yes"
    writes = declares_write(skill_md.parent / "api")
    text = skill_md.read_text(encoding="utf-8")

    if (has_identity or writes) and COMMON_SKILL not in text:
        why = " and ".join(
            w for w in ("carries an identity" if has_identity else "",
                        "declares a write" if writes else "") if w)
        fail(f"{tag}: SKILL.md {why} but never names the `{COMMON_SKILL}` skill — "
             f"an agent loading only this skill would never read the error "
             f"envelope, --read-only, or the write-confirmation rules. Fix: add "
             f"the template's pointer to the Auth blockquote — "
             f"`Common error/safety rules → load the `{COMMON_SKILL}` skill.`")

    if has_identity and AUTH_SKILL not in text:
        fail(f"{tag}: SKILL.md carries an identity but never names the "
             f"`{AUTH_SKILL}` skill — an agent meeting a credential error here "
             f"has nowhere to go, and no sub-domain skill describes login. Fix: "
             f"add the template's pointer to the Auth blockquote — `On a "
             f"structured credential error, load the `{AUTH_SKILL}` skill; that "
             f"skill owns the exact subtype/message/recovery.`")




# Invariant 9: the embed manifests must match on-disk visibility exactly. The
# embed seam lives in the root `assets` package: embed_public.go (//go:build
# !internal) and embed_internal.go (//go:build internal); each has an empty stub
# behind the opposite tag so `go build ./...` compiles. This is the ONE build-tag
# seam the editions split keeps (//go:embed ignores the import graph).
# This repository builds ONE product, so there is a single embed manifest and no
# build tag on it (the tagged public/internal seam only exists where both
# products share a tree).
EMBED_MANIFEST = ROOT / "embed.go"
EMBED_DIRECTIVE_RE = re.compile(r"^//go:embed\s+(.+)$", re.MULTILINE)


# SKILL_DOC_DIRS are the supporting-file subdirs shipped alongside SKILL.md (the
# Agent Skills layout). They travel into the installed skill; api/*.go never does.
# Keep in sync with skillDocDirs in internal/cli/skills.go.
SKILL_DOC_DIRS = ("references", "scripts", "assets")


def _embed_excluded(rel):
    """Mirror //go:embed's directory rule: names beginning '.' or '_' are skipped
    when a directory is embedded."""
    return any(part.startswith((".", "_")) for part in rel.split("/"))


def embedded_paths(go_file):
    """Parse //go:embed patterns from a Go file and expand them to the full set of
    repo-relative, forward-slashed FILE paths they cover — explicit files, glob
    patterns, and directories (a directory contributes its whole subtree, as
    //go:embed does, minus dot/underscore names). This is deliberately not
    filtered to SKILL.md: it must also surface a stray *.go so Invariant 9 can
    reject it."""
    paths = set()
    if not go_file.exists():
        return paths
    for line in EMBED_DIRECTIVE_RE.findall(go_file.read_text(encoding="utf-8")):
        for pat in line.split():
            pat = pat.strip().strip('"')
            if not pat:
                continue
            matches = ROOT.glob(pat) if any(c in pat for c in "*?[") else [ROOT / pat]
            for m in matches:
                if m.is_dir():
                    for f in m.rglob("*"):
                        rel = f.relative_to(ROOT).as_posix()
                        if f.is_file() and not _embed_excluded(rel):
                            paths.add(rel)
                elif m.is_file():
                    paths.add(m.relative_to(ROOT).as_posix())
    return paths


def disk_skill_paths_by_visibility():
    """Every SHIPPABLE skill file on disk (each SKILL.md plus the files under its
    references/ scripts/ assets/ subtrees), bucketed by the owning skill's
    frontmatter visibility. These are exactly the files the embed manifest must
    carry for that visibility — no more (no api/*.go), no less."""
    buckets = {"public": set(), "internal": set()}
    for md in sorted(SKILLS.rglob("SKILL.md")):
        rel = md.relative_to(ROOT).as_posix()
        vis = read_frontmatter(md).get("visibility")
        if vis not in buckets:
            fail(f"embed-manifest: {rel} has visibility {vis!r} (must be public|internal)")
            continue
        buckets[vis].add(rel)
        for sub in SKILL_DOC_DIRS:
            d = md.parent / sub
            if d.is_dir():
                for f in d.rglob("*"):
                    frel = f.relative_to(ROOT).as_posix()
                    if f.is_file() and not _embed_excluded(frel):
                        buckets[vis].add(frel)
    return buckets


def check_embed_manifest():
    """Invariant 9: embed.go must embed EXACTLY the skill files on disk.

    //go:embed reads files off disk regardless of the import graph, so the
    manifest is the only thing deciding which skill bytes reach the binary. The
    direction that needs a checker is disk -> manifest: a skill on disk but
    missing from the manifest fails silently, shipping a binary that cannot
    install it. The reverse (a manifest line naming a file that does not exist)
    is already fatal at compile time — `go build` reports "pattern ...: no
    matching files found" — so it is the compiler's job, not this check's.

    "Skill files" means the whole shippable surface — each SKILL.md plus its
    references/ scripts/ assets/ subtrees — so a supporting doc is held to the
    same rules as its SKILL.md. A skill's Go (api/*.go) must never be embedded:
    it would bloat the binary and publish API source as strings.

    Every sub-domain here is public, so there is no cross-visibility direction to
    guard; a sub-domain that declares `visibility: internal` is a mistake in this
    repository and is reported by disk_skill_paths_by_visibility()."""
    disk = disk_skill_paths_by_visibility()
    embedded = embedded_paths(EMBED_MANIFEST)

    # No Go (or other non-skill source) may be embedded — the allowlist is
    # SKILL.md + references/scripts/assets. Catches a widened //go:embed directive
    # (e.g. a whole subdomain dir) before it ships api/*.go bytes.
    for rel in sorted(embedded):
        base = rel.rsplit("/", 1)[-1]
        in_doc_dir = any(("/" + d + "/") in rel for d in SKILL_DOC_DIRS)
        if base != "SKILL.md" and not in_doc_dir:
            fail(f"embed-manifest: {EMBED_MANIFEST.name} embeds {rel}, which is neither "
                 f"a SKILL.md nor a {'/'.join(SKILL_DOC_DIRS)} file — a skill's Go "
                 f"(api/*.go) and other source must never be embedded")

    # An internal-visibility sub-domain does not belong in this repository at all.
    for rel in sorted(disk["internal"]):
        fail(f"embed-manifest: {rel} declares visibility: internal, but this "
             f"repository builds the public product only — every sub-domain here "
             f"must be visibility: public")

    # The manifest and the disk must agree exactly, in both directions.
    for rel in sorted(disk["public"] - embedded):
        fail(f"embed-manifest: {rel} is on disk but not embedded in "
             f"{EMBED_MANIFEST.name} (it would not ship) — add a //go:embed line")
    for rel in sorted(embedded - disk["public"]):
        fail(f"embed-manifest: {EMBED_MANIFEST.name} embeds {rel}, which is not a "
             f"skill file on disk — remove the stale //go:embed line")


# Invariant 10: editions/* compose against framework + pkg/ ports only — they
# must not import ANY internal/ package (engine, transport, …). The engine is a
# black box to an edition; the transport is reached via the pkg/transport.Sender
# port. This keeps editions replaceable: a module composing its own edition can
# only reach the engine through the same ports, since Go forbids importing
# internal/ across a module boundary.
INTERNAL_IMPORT = MODULE_PATH + "/internal/"
EDITIONS_DIR = ROOT / "editions"

# Invariant 11: this repository builds a single product, so no file may carry an
# internal/!internal build constraint at all.
BUILD_TAG_INTERNAL_RE = re.compile(r"^//go:build\b.*\binternal\b", re.MULTILINE)


def check_editions_no_internal_import():
    """Invariant 10: no editions/* source imports any internal/ package."""
    if not EDITIONS_DIR.is_dir():
        return
    for go in sorted(EDITIONS_DIR.rglob("*.go")):
        if go.name.endswith("_test.go"):
            continue  # tests compose via framework.RunForTest, not internal/ directly
        if INTERNAL_IMPORT in go.read_text(encoding="utf-8"):
            rel = go.relative_to(ROOT).as_posix()
            fail(f"[editions-boundary] {rel} imports an internal/ package — an edition must "
                 f"compose against framework + pkg/ ports only (reach transport via "
                 f"pkg/transport.Sender, not internal/transport) (Invariant 10)")


def check_single_build_tag_seam():
    """Invariant 11: no file carries an internal/!internal build tag (single product)."""
    for go in sorted(ROOT.rglob("*.go")):
        rel = go.relative_to(ROOT).as_posix()
        if rel.startswith(".git/") or not _outside_vendor(rel) or "/testdata/" in rel:
            continue
        if BUILD_TAG_INTERNAL_RE.search(go.read_text(encoding="utf-8")):
            fail(f"[build-tag-seam] {rel} carries an internal/!internal build constraint. "
                 f"This repository builds a single product and must be tag-free: the seam "
                 f"that once needed a tag exists only where two products share a tree "
                 f"(Invariant 11)")


# Invariant 16: pkg/ names no product's host key, signing scheme or identity.
#
# pkg/ is the port layer: it carries the interfaces a product plugs into and the
# logic that orchestrates them. Which hosts, schemes and identities exist is the
# product's vocabulary — declared in signing/, identity/ and the product's own
# adapter package, and injected at composition time. A constant naming one of
# them here is a value the shared layer cannot use and a second product would
# have to inherit.
#
# The three axes below are the ones that have drifted: a host key constant sat
# in pkg/hosts while the same concept was already declared in an edition's own
# adapter, and pkg/api already carries the rule in prose ("The concrete values
# are deliberately NOT catalogued here"). This is that rule, enforced.
#
# The check is name-shaped, not semantic: it cannot tell a product value from a
# universal one in general, so it looks at the three prefixes that carry these
# axes and allows the two universal values. A new universal value goes in
# UNIVERSAL_AXIS_CONSTS with a reason.
PKG = ROOT / "pkg"
UNIVERSAL_AXIS_CONSTS = {
    "SignNone",      # the absence of a scheme — no product owns "unsigned"
    "IdentityNone",  # the absence of an identity, likewise
}
PRODUCT_AXIS_CONST_RE = re.compile(
    r"^\s*(?:const\s+)?((?:Key|Sign|Identity)[A-Z]\w*)\s+(?:[\w.\[\]*]+\s+)?=",
    re.MULTILINE,
)


def check_no_product_consts_in_pkg():
    """Invariant 16: pkg/ declares no product host key / scheme / identity."""
    for go in sorted(PKG.rglob("*.go")):
        rel = go.relative_to(ROOT).as_posix()
        if rel.endswith("_test.go") or "/testdata/" in rel:
            continue
        text = go.read_text(encoding="utf-8")
        for m in PRODUCT_AXIS_CONST_RE.finditer(text):
            name = m.group(1)
            if name in UNIVERSAL_AXIS_CONSTS:
                continue
            line = text[: m.start()].count("\n") + 1
            fail(f"[pkg-product-const] {rel}:{line} declares `{name}` — a host key, "
                 f"signing scheme or identity is the PRODUCT's vocabulary. Declare it "
                 f"in signing/, identity/ or the product's adapter package and inject "
                 f"it; pkg/ holds the port and the orchestration (Invariant 16)")


# Invariant 17: no user-facing string hard-codes a binary name.
#
# Every product built on this module ships its own binary, and the shared layer
# is compiled into all of them, so a literal command name in a hint is wrong in
# every product that is not the one it names. It is wrong in a second, more
# ordinary case too: a renamed or symlinked binary. The command tree already
# follows argv[0] (framework.Run derives the root command's name from it); the
# prose has to follow the same source, which is pkg/cliname.
#
# Two shapes are matched inside a Go string literal, because two names are in
# play: the historical one this module still carries as a default, and the name
# this product's binary actually ships under.
#
#   1. the historical name `mexc-cli` anywhere in a literal;
#   2. a backtick-quoted command opening with this product's own name —
#      "`mexc schema`" — since a bare "mexc" is far too common a substring
#      (MEXC the company, mexc-common the skill) to flag on its own.
#
# It cannot know what "user-facing" means, so it approximates by shape and
# deliberately does NOT match:
#
#   - the module import path, which necessarily contains a name;
#   - comments, where naming the product is documentation, not an instruction;
#   - skill names (mexc-common, mexc-spot-market, ...), which are installation
#     identifiers rather than commands — renaming a binary must not rename them,
#     and none of them follows a backtick;
#   - pkg/cliname itself, which owns the default.
#
# What it cannot catch: a hint assembled from a variable, or a command name
# written without backticks that is not "mexc-cli". Those still need review.
# A line carrying this marker is skipped. It exists for the one shape the token
# test cannot distinguish: a name that merely LOOKS like the binary but belongs
# to something else with its own lifetime — an artifact repository, say, which
# must keep its name when a binary is renamed. The marker is inline rather than a
# file list so the reason sits next to the code it excuses.
BINARY_NAME_MARKER = "binary-name-ok"
# Two literal forms carry user-facing text in Go: the interpreted string, and the
# raw string, which a multi-line help block reaches for. Both are matched; a rule
# that only knew the first was walked straight past by moving the text into
# backticks.
BINARY_NAME_RES = (
    re.compile(r'"[^"]*(?:\bmexc-cli\b|`mexc[ -])[^"]*"'),   # interpreted
    re.compile(r'`[^`]*\bmexc-cli\b[^`]*`'),                 # raw (or a markdown span)
)
CLINAME_EXEMPT = {"pkg/cliname/cliname.go"}
# protoc-gen-go output ("DO NOT EDIT") whose raw FileDescriptorProto byte string
# carries the .proto's go_package option verbatim — a length-prefixed field
# inside a marshaled descriptor, not a CLI hint or command example a caller
# ever sees. The BINARY_NAME_MARKER comment convention does not fit here: the
# hit sits inside a multi-line string built from concatenated line literals
# with no per-line comment to carry it, and hand-editing the bytes risks
# desyncing the field's own length prefix from its content, which is exactly
# what "DO NOT EDIT" is warning against. Exempt by directory instead, the same
# way CLINAME_EXEMPT exempts a single file by path.
GENERATED_DIR_EXEMPT_PREFIX = "internal/transport/ws/gen/"

# The constant itself is gone, so referencing it by name is already a compile
# error; only the raw string literal can still slip past the Go compiler.
SPOTHMAC_RE = re.compile(r'Signed:\s*"spot-hmac"')


def check_no_hardcoded_binary_name():
    """Invariant 17: no Go string literal hard-codes the binary name."""
    # skills/ is in scope: a subdomain's Summary / Desc / WithHint are as
    # user-facing as anything in the engine, and a business author writing a
    # command example there is exactly who this rule is for.
    for base in (ROOT / "pkg", ROOT / "internal", ROOT / "framework",
                 ROOT / "editions", ROOT / "cmd", ROOT / "skills"):
        if not base.is_dir():
            continue
        for go in sorted(base.rglob("*.go")):
            rel = go.relative_to(ROOT).as_posix()
            if (rel.endswith("_test.go") or rel in CLINAME_EXEMPT
                    or rel.startswith(GENERATED_DIR_EXEMPT_PREFIX)):
                continue
            # excused tracks whether the comment block immediately above the
            # current line carried the marker, so the reason can sit above the
            # code it excuses (how Go comments are written) rather than being
            # crammed onto the same line. Any non-comment, non-blank line that is
            # not itself excused clears it.
            excused = False
            for i, line in enumerate(go.read_text(encoding="utf-8").splitlines(), 1):
                stripped = line.lstrip()
                if stripped.startswith("//"):
                    if BINARY_NAME_MARKER in line:
                        excused = True
                    continue
                if BINARY_NAME_MARKER in line:  # same-line marker also works
                    excused = False
                    continue
                if not stripped:
                    continue
                hits = []
                for rx in BINARY_NAME_RES:
                    hits += rx.findall(line.replace(MODULE_PATH, ""))
                if excused:
                    excused = False  # the marker covers one statement, not a file
                    continue
                for lit in hits:
                    fail(f"[hardcoded-binary-name] {rel}:{i} has a literal binary name in "
                         f"{lit.strip()} — user-facing text must name the binary the caller "
                         f"actually ran. Use cliname.Name() (Invariant 17)")


def check_no_spothmac_usage():
    """Invariant 19: every signed subdomain under skills/ or editions/ must
    use signing.CliHMAC, not the raw "spot-hmac" string literal.
    """
    for base in (ROOT / "skills", ROOT / "editions"):
        if not base.is_dir():
            continue
        for go in sorted(base.rglob("*.go")):
            rel = go.relative_to(ROOT).as_posix()
            if rel.endswith("_test.go"):
                continue
            for i, line in enumerate(go.read_text(encoding="utf-8").splitlines(), 1):
                code = line.split("//", 1)[0]
                if SPOTHMAC_RE.search(code):
                    fail(f"[no-spothmac] {rel}:{i} must use signing.CliHMAC, "
                         f"not the raw \"spot-hmac\" literal (Invariant 19)")


# Invariant 14: no operation in this repository declares api.Operation.Permission.
#
# Permission names an RBAC bucket plus the codes an operation needs, and it is
# only meaningful when the identity making the call resolves credentials that
# carry a grant map to check against. This product's only identity is an api
# key: its permissions live on the server side and cannot be read from the
# credential, so nothing here could ever evaluate the codes.
#
# The field stays in the shared pkg/api because the taxonomy is shared — a
# product whose identity does carry grants uses it. Here it is inert, and
# `mexc-cli schema` already omits it for an identity without grants. This rule
# stops it being written in the first place, for two reasons: a declaration that
# no layer reads is a lie to whoever reads the source, and a permission bucket
# names a system this repository does not ship.
PERMISSION_DECL_RE = re.compile(r"^\s*Permission:\s*&?\w", re.MULTILINE)


def check_no_permission_declared():
    """Invariant 14: no op declares Permission (no identity here carries grants)."""
    for go in sorted(SKILLS.rglob("*.go")):
        rel = go.relative_to(ROOT).as_posix()
        if rel.endswith("_test.go") or "/testdata/" in rel:
            continue
        text = go.read_text(encoding="utf-8")
        for m in PERMISSION_DECL_RE.finditer(text):
            line = text[: m.start()].count("\n") + 1
            fail(f"[permission] {rel}:{line} declares Permission, which no identity "
                 f"in this product can check: an api key's permissions live on the "
                 f"server and are not readable from the credential, so the codes "
                 f"would never be evaluated. Drop the field (Invariant 14)")


# Invariant 13: on-disk state lives where the PRODUCT says it lives, so exactly
# one place may compute the state directory.
#
# pkg/config.StateDir() is that place: it joins $HOME with the name the
# composition root set from Edition.StateDir. Everything else — settings,
# profiles, skill state, any identity's session store — asks it. A second
# computation is not a style problem: it does not fail, it silently disagrees.
# The product then writes part of its state into a directory it does not own
# while the rest goes to the right one — and that half-split is invisible: every
# directory involved is the caller's own and 0700, so nothing errors, nothing
# warns, and only reading the source turns it up. This is not a hypothetical
# failure mode; it is one that shipped and went unnoticed.
#
# STATE_DIR_PRODUCER is the sole exemption: it IS the funnel.
#
# Tests are exempt too: a test computing $HOME/<name> to assert an on-disk layout
# is doing its job, and a path computed in a test cannot move where the product
# writes. This is NOT the exemption that hid disclosures under the old Invariant
# 12 — published source is published whether or not it is a test, but a path is
# only a path where the product actually runs.
STATE_DIR_PRODUCER = "pkg/config/statedir.go"
STATE_DIR_MARKER = "state-dir-ok"
# Every way the standard library hands a program a per-user directory. The rule
# is about the CALL, not about the directory NAME, and that is the whole design:
# a hard-coded name is inert until something can read $HOME to join it to, so
# guarding the read guards the outcome.
#
# A name rule was tried and removed. It was evadable in four ways that all still
# produced a working, wrong directory (a raw-string literal, a different case,
# concatenation, and any line that happened to contain "StateDir:"), and it was
# the sole source of false positives — it fired on doc comments and on
# user-facing strings, and it degraded one API comment into a note about a lint
# rule before being reverted. Precision was inverted: it caught prose and missed
# os.Getenv("HOME").
#
# What is still not caught is a deliberately obscured read (walking os.Environ,
# say). That is evasion, not a slip, and no line-based check stops it. The net
# that catches the BEHAVIOUR either way is the test asserting a build writes
# nothing outside its own state directory.
HOME_DIR_CALL_RE = re.compile(
    r"\bos\.(UserHomeDir|UserConfigDir|UserCacheDir)\(\)"
    r'|\bos\.Getenv\("HOME"\)'
)


def tracked_go_files():
    """Return the repo-tracked *.go paths we author (staged files included), or
    None when git is unavailable. Asking git rather than walking the tree keeps an
    untracked scratch checkout — a stale worktree, a sibling clone — from failing a
    build it is not part of; in CI, a clean checkout, everything committed is
    scanned.

    vendor/ needs its own exclusion rather than relying on that: a vendored tree
    IS tracked, by design, so being tracked no longer means being ours.

    subprocess args stay in the 3.5-compatible stdout=/stderr=PIPE form
    (capture_output needs 3.7; the shared CI runner is older — same note as
    tracked_markdown and check-english.py), and a git that is absent or angry
    returns None instead of raising: a traceback from a guard reads as a broken
    script, not as a violation."""
    try:
        out = subprocess.run(
            ["git", "-C", str(ROOT), "ls-files", "-z", "--", "*.go", GIT_EXCLUDE_VENDOR,
             GIT_EXCLUDE_INTERNAL],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
        )
    except OSError:
        return None
    if out.returncode != 0:
        return None
    return [p for p in out.stdout.decode("utf-8").split("\0") if p]


def check_state_dir_funnel():
    """Invariant 13: only pkg/config computes the state directory."""
    rels = tracked_go_files()
    if rels is None:  # no git (e.g. an exported tarball): scan the working tree
        rels = [rel for rel in
                (p.relative_to(ROOT).as_posix() for p in ROOT.rglob("*.go"))
                if not rel.startswith(".git/") and _outside_vendor(rel)]
    for rel in sorted(rels):
        if rel == STATE_DIR_PRODUCER or "/testdata/" in rel or rel.endswith("_test.go"):
            continue
        source = ROOT / rel
        # `git ls-files` includes tracked paths deleted in the working tree. A
        # removal cannot introduce a new state-directory read, so skip it rather
        # than making the policy check crash before it can inspect live files.
        if not source.is_file():
            continue
        for i, line in enumerate(source.read_text(encoding="utf-8").splitlines(), 1):
            if STATE_DIR_MARKER in line:
                continue
            if HOME_DIR_CALL_RE.search(line):
                fail(f"[state-dir] {rel}:{i} reads a per-user directory from the OS — the "
                     f"state directory is the product's (Edition.StateDir); ask "
                     f"config.StateDir()/StatePath() instead of recomputing it, or "
                     f"mark the line `{STATE_DIR_MARKER}` if this is not product "
                     f"state (Invariant 13)")


# The no-disclosure rule (number 12) is not one of the checks below, and adding
# it here would defeat it.
#
# Everything this repository tracks is published, so nothing here may name a
# system that is not published with it. Checking that mechanically needs a list
# of the names to reject — and this file would carry it, in the open, which
# discloses exactly what the rule protects. A guard that publishes its own secret
# is worse than none: it is also reassuring.
#
# So the rule is split by what a check has to KNOW. The half that needs the list
# is a RELEASE-FLOW step: a scanner held outside this tree runs over a checkout of
# it before publishing anywhere new. The half that needs no list — a shape: a
# service-name prefix, a host that is not on the public allowlist, a query or a
# column — is check-exposure.py, next to this file. The list below stays honest
# about what a green run HERE does and does not prove: neither of them.
#
# Do NOT add a token list to this file. check-exposure.py may not carry one
# either, for the same reason.


def main():
    registry = load_registry()
    registry_axes = load_registry_axes()
    enums = load_api_enums()

    for domain in sorted(p for p in SKILLS.iterdir() if p.is_dir()):
        # A one-level skills/<name>/SKILL.md is a CLI-maintained shared skill
        # (e.g. mexc-common), not an operation subdomain: it is exempt from the
        # subdomain invariants (scope/permission/enum), but being user-facing it
        # must still not leak a signing-algorithm name (Invariant 7). Invariant 5
        # (self-contained) already covers it via the repo-wide markdown scan.
        shared_md = domain / "SKILL.md"
        if shared_md.exists():
            check_no_algo_names("shared", domain.name, shared_md)
            check_no_old_binary_name("shared", domain.name, shared_md)
            check_description_yaml_safe(f"shared {domain.name}", shared_md)

        subs = {p.name for p in domain.iterdir() if p.is_dir()}

        for sub in sorted(subs):
            tag = f"[{domain.name}] {sub}"

            # Invariant 2: sub-domain must be registered
            if sub not in registry:
                fail(f"{tag} is not in SUBDOMAINS.md registry")

            # Invariant 1: SKILL.md present
            skill_md = domain / sub / "SKILL.md"
            if not skill_md.exists():
                fail(f"{tag} missing SKILL.md")
                continue

            # Invariant 4: permission table present; every scope token equals
            # the resource scope `<domain>.<sub>`; action lives in `access` col.
            want_scope = f"{domain.name}.{sub}"
            scopes = read_perm_scopes(skill_md)
            if not scopes:
                fail(f"{tag} SKILL.md has no permission table "
                     f"(expected the resource scope `{want_scope}`)")
            else:
                for scope in sorted(scopes):
                    if scope != want_scope:
                        fail(f"{tag}: scope `{scope}` must be the resource scope "
                             f"`{want_scope}` (no action suffix — action goes in "
                             f"the `access` column)")
                has_col, access_values = read_access_values(skill_md)
                if not has_col:
                    fail(f"{tag}: permission table needs an `access` column "
                         f"(verb ∈ {{{', '.join(VALID_ACCESS)}}})")
                else:
                    for v in sorted(access_values):
                        if v not in VALID_ACCESS:
                            fail(f"{tag}: access `{v}` is not a known verb "
                                 f"(allowed: {', '.join(VALID_ACCESS)})")

            # Invariant 3: frontmatter completeness + well-formedness
            check_frontmatter(domain.name, sub, skill_md)

            # Invariant 2 (registry ↔ code): the SUBDOMAINS.md overview's
            # signed/identity columns must mirror the SKILL.md frontmatter (which
            # Invariant 6c pins to the api `const Signed`/`const Identity`). The
            # overview is hand-maintained and otherwise unchecked, so without this
            # a row could advertise `signed: yes` / `identity: yes` while the
            # shipped code is unsigned / no-identity (or vice versa).
            reg_axes = registry_axes.get(want_scope)
            if reg_axes:
                fm_axes = read_frontmatter(skill_md)
                for col in ("signed", "identity"):
                    rv, fv = reg_axes.get(col), fm_axes.get(col)
                    if rv in VALID_SIGNED and fv in VALID_SIGNED and rv != fv:
                        fail(f"{tag}: SUBDOMAINS.md {col}={rv} but SKILL.md "
                             f"frontmatter {col}={fv} — the registry overview must "
                             f"mirror the code const (Invariant 2)")

            # Invariant 7: no signing-algorithm name in the user-facing SKILL.md
            check_no_algo_names(domain.name, sub, skill_md)

            # Invariant 18: no retired binary name in the user-facing SKILL.md
            check_no_old_binary_name(domain.name, sub, skill_md)

            # Invariant 20: hand-off pointers to the shared mexc-common / mexc-auth skills
            check_shared_skill_pointers(domain.name, sub, skill_md)

            # Invariant 6: closed-enum discipline in the api declarations
            check_enum_discipline(domain.name, sub, domain / sub / "api",
                                  read_frontmatter(skill_md), enums)

    # Invariant 5: self-contained operational docs (repo-wide, once)
    check_self_contained()

    # Invariant 9: embed manifests match on-disk visibility (flat-layout leak guard)
    check_embed_manifest()

    # Invariant 10: editions do not import any internal/ package (disjoint-editions boundary)
    check_editions_no_internal_import()

    # Invariant 11: no build tag anywhere (one product, nothing to select between)
    check_single_build_tag_seam()

    # Invariant 13: only pkg/config computes the state directory
    check_state_dir_funnel()

    # Invariant 14: no op declares Permission — no identity here carries grants
    check_no_permission_declared()

    # Invariant 16: pkg/ names no product host key / scheme / identity
    check_no_product_consts_in_pkg()

    # Invariant 17: no user-facing string hard-codes the binary name
    check_no_hardcoded_binary_name()

    # Invariant 19: every signed subdomain uses signing.CliHMAC
    check_no_spothmac_usage()

    # Number 12, the no-disclosure rule, is deliberately not a check here — see
    # the note above this function. Its shape half is check-exposure.py; its name
    # half is a release-flow step. Adding a token list to this published file
    # would disclose the very names it is supposed to reject.

    if errors:
        print(f"✗ {len(errors)} boundary violation(s):")
        for e in errors:
            print(f"  - {e}")
        sys.exit(1)
    print("✓ taxonomy invariants hold")


if __name__ == "__main__":
    force_utf8_stdio()
    main()
