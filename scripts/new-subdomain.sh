#!/usr/bin/env bash
# new-subdomain — generate the skeleton for one sub-domain (flat directory, no common/biz).
#
# Usage:
#   bash scripts/new-subdomain.sh <domain> <sub>
#   e.g.  bash scripts/new-subdomain.sh spot market
#
# What it generates (gofmt-clean and passing go build / go test out of the box;
# check-boundary goes green once you complete manual steps 1 and 3 below —
# registering in SUBDOMAINS.md and adding the //go:embed line):
#   skills/<domain>/<sub>/SKILL.md             -- copied from skill-template/SKILL.skeleton.md (domain/sub substituted)
#   skills/<domain>/<sub>/api/<sub>.go         -- Operation declarations + subdomain defaults
#   skills/<domain>/<sub>/api/contract_test.go -- code <-> SKILL.md consistency self-check
#
# Taxonomy = five orthogonal dimensions (see SUBDOMAINS.md):
#   scope=<domain>.<sub> (pure resource), access (read/write, permission column),
#   signed  (whether signing is required — subdomain default `const Signed`),
#   identity(which auth credential — subdomain default `const Identity`),
#   visibility (public — every sub-domain in this repository is public).
#
# signed/identity are the FACTS the SKILL.md expresses in yes/no; the api package
# expresses them as the signing.* / identity.* constants (`const Signed`,
# `const Identity`). The generated contract test compares the two by fact.
#
# NOTE: the generated SKILL.md uses the yes/no fact model for signed/identity
# (Invariant 3/6c) — signed==yes iff Signed!=SignNone, identity==yes iff
# Identity!=IdentityNone — which matches the current check-boundary. go build /
# go test are unaffected by the frontmatter values.
#
# It does NOT edit SUBDOMAINS.md, nor wire the sub-domain into the edition
# (editions/public/domains.go), nor add the //go:embed line -- all deliberate
# manual steps, printed at the end.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODULE="github.com/mexcdevelop/mexc-cli"

die() { echo "✗ $*" >&2; exit 1; }

[ $# -ge 2 ] || die "usage: bash scripts/new-subdomain.sh <domain> <sub>"
DOMAIN="$1"; SUB="$2"; shift 2
# This repository builds one product, so every sub-domain it scaffolds is public.
VISIBILITY="public"
[ $# -gt 0 ] && die "unexpected argument: $1 (this repository scaffolds public sub-domains only)"

# Naming check: singular lowercase, hyphenate multi-word, no domain prefix.
echo "$DOMAIN" | grep -qE '^[a-z0-9-]+$' || die "domain may only contain lowercase/digits/hyphens: $DOMAIN"
echo "$SUB"    | grep -qE '^[a-z0-9-]+$' || die "sub-domain may only contain lowercase/digits/hyphens: $SUB"

# Go package name / identifier: strip hyphens (e.g. mx-deduct -> mxdeductapi).
PKG="$(echo "$SUB" | tr -d '-')api"

# The effective host key is MANDATORY (assembly refuses to build when it is
# empty), but in this repository it is the same for every sub-domain: the edition
# wires hosts.KeyOpenAPI, so an api package declares no HostKey of its own.
HOSTKEY_CONST=""

DIR="$REPO_ROOT/skills/$DOMAIN/$SUB"
[ -e "$DIR/SKILL.md" ] && die "already exists, not overwriting: skills/$DOMAIN/$SUB/SKILL.md"
mkdir -p "$DIR/api"

# SKILL.md is copied from the CLEAN skeleton (skill-template/SKILL.skeleton.md) and
# substituted, so it always tracks the current template structure -- no hand-kept
# copy here to drift. The annotated guide (skill-template/SKILL.md) explains what each
# section should contain; the skeleton is the machine-copied structure the tool emits.
SKELETON="$REPO_ROOT/skill-template/SKILL.skeleton.md"
[ -f "$SKELETON" ] || die "missing clean skeleton: skill-template/SKILL.skeleton.md"
# The auth-recovery line names the credential skill a reader should load; the
# api-key flow is the only identity this product carries.
AUTH_SKILL="mexc-auth"

sed -e "s|<domain>|${DOMAIN}|g" -e "s|<sub>|${SUB}|g" -e "s|<auth-skill>|${AUTH_SKILL}|g" "$SKELETON" > "$DIR/SKILL.md"

cat > "$DIR/api/${SUB}.go" <<EOF
// Package ${PKG} declares the operations of the ${DOMAIN}.${SUB} sub-domain.
//
// See AGENTS.md section 3.1 for the declaration contract: declare only Operation
// descriptors (op name / path / access / params). Signing and login identity
// default at the subdomain level (const Signed / const Identity below); an op
// omits them to inherit, or sets a concrete signing.* / identity.* constant to
// override. The framework sends HTTP, signs, and mounts commands -- not this file.
package ${PKG}

import "${MODULE}/pkg/api"

// Operations returns all operations of this sub-domain. The framework assembles
// commands from it, and the contract test self-checks against it. The example op
// inherits the subdomain defaults (Sign / Identity omitted) — the golden pattern.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "example", Method: "GET", Path: "/api/v3/example",
			Access:  api.Read,
			Summary: "placeholder op -- replace with a real capability",
			Params: []api.Param{
				{Name: "symbol", Desc: "trading pair, e.g. BTCUSDT", Required: true},
				// A write is confirmed before it runs and the prompt shows the params;
				// mark a param whose value is a secret with Sensitive: true to mask it
				// (a body field uses the sensitive:"true" struct tag). Rare.
				// Pre-validation constraints are declared per field: on a Param via
				// MinVal/MaxVal (inclusive numeric), MinLen/MaxLen, Pattern; on a body
				// field via the minval/maxval/minlen/maxlen/pattern struct tags.
			},
		},
	}
}

// Scope is the resource identifier of this sub-domain (matches SKILL.md frontmatter).
const Scope = "${DOMAIN}.${SUB}"

// Signed is this sub-domain's default signing scheme. api.SignNone = unsigned —
// the one scheme pkg/api names. This product's schemes live in this repository's
// signing package: switch to signing.CliHMAC, add that import, and flip the
// SKILL.md frontmatter signed to yes. Assembly refuses a scheme with no signer.
const Signed = api.SignNone

// Identity is this sub-domain's default auth identity/credential (a closed enum
// api.IdentityNone = public, no credential — the one identity pkg/api names.
// Switch to identity.OpenAPI (the API-key identity, declared in this
// repository's identity package, so add that import) when the operations need a
// credential, and flip the SKILL.md frontmatter identity to yes.
const Identity = api.IdentityNone
${HOSTKEY_CONST}
EOF

cat > "$DIR/api/contract_test.go" <<EOF
package ${PKG}

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"${MODULE}/identity"
	"${MODULE}/pkg/api"
)

// Generated by new-subdomain.sh. Guards the "code declaration <-> SKILL.md doc"
// consistency, the way the spot samples do; passes go test out of the box and
// keeps self-checking after real operations are wired up.
//
// signed/identity are compared by FACT: the SKILL.md uses yes/no while the code
// uses the signing.* / identity.* constants, so the test asks only "is signing
// required?" / "is a login required?" — never literal equality.

func TestOps_FieldsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range Operations() {
		if seen[op.Name] {
			t.Errorf("duplicate op name %q in Operations() — op names must be unique", op.Name)
		}
		seen[op.Name] = true
		if err := op.ValidateDecl(); err != nil {
			t.Errorf("op %q: %v", op.Name, err)
		}
	}
}

// effectiveSigned / effectiveIdentity mirror the assembly-layer inheritance: a
// concrete op-level value overrides; the zero value inherits the subdomain const.
func effectiveSigned(op api.Operation) api.Signing {
	if op.Signed.Concrete() {
		return op.Signed
	}
	return Signed
}

func effectiveIdentity(op api.Operation) api.Identity {
	if op.Identity.Concrete() {
		return op.Identity
	}
	return Identity
}

// TestOps_OpenAPINeedsSigning enforces the openapi <-> signing pairing the
// framework does NOT check itself: identity=openapi is API-key auth that MUST be
// signed, and Signed says WHICH scheme. So an openapi operation whose effective
// signing scheme is none is a business error (the request would go out unsigned
// and the backend would reject it). Other identities carry no such requirement.
func TestOps_OpenAPINeedsSigning(t *testing.T) {
	for _, op := range Operations() {
		if effectiveIdentity(op) == identity.OpenAPI && effectiveSigned(op) == api.SignNone {
			t.Errorf("op %q: identity=openapi but no signing scheme (Signed=none); "+
				"openapi requests must be signed — set a concrete signing.* scheme "+
				"on the op or the subdomain's const Signed", op.Name)
		}
	}
}

// TestOps_MatchSkillTable checks the SKILL.md operation table against
// Operations() both ways: name, method, path, access, signed/identity by fact,
// and idempotent (compared to IdempotentHint) when the table carries that column.
func TestOps_MatchSkillTable(t *testing.T) {
	doc, err := os.ReadFile("../SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(doc)

	// Frontmatter mirrors the code consts by FACT (yes/no).
	if got := frontmatterField(content, "signed"); got != yesNo(Signed != api.SignNone) {
		t.Errorf("SKILL.md frontmatter signed=%q but subdomain Signed=%q", got, Signed)
	}
	if got := frontmatterField(content, "identity"); got != yesNo(Identity != api.IdentityNone) {
		t.Errorf("SKILL.md frontmatter identity=%q but subdomain Identity=%q", got, Identity)
	}
	if got := frontmatterField(content, "scope"); got != Scope {
		t.Errorf("SKILL.md frontmatter scope=%q but code Scope=%q", got, Scope)
	}

	docRows, hasIdem := parseSkillTable(content)
	codeOps := map[string]api.Operation{}
	for _, op := range Operations() {
		codeOps[op.Name] = op
	}
	for name := range codeOps {
		if _, ok := docRows[name]; !ok {
			t.Errorf("op %q is in code but not in the SKILL.md operation table", name)
		}
	}
	for name, row := range docRows {
		op, ok := codeOps[name]
		if !ok {
			t.Errorf("op %q is in SKILL.md but has no matching Operation in code", name)
			continue
		}
		if row.method != op.Method {
			t.Errorf("op %q: SKILL.md method %q != code %q", name, row.method, op.Method)
		}
		if row.path != op.Path {
			t.Errorf("op %q: SKILL.md path %q != code %q", name, row.path, op.Path)
		}
		if row.access != string(op.Access) {
			t.Errorf("op %q: SKILL.md access %q != code %q", name, row.access, op.Access)
		}
		if want := effectiveSigned(op) != api.SignNone; row.signed != want {
			t.Errorf("op %q: SKILL.md signed=%v != effective signed %q", name, row.signed, effectiveSigned(op))
		}
		if want := effectiveIdentity(op) != api.IdentityNone; row.identity != want {
			t.Errorf("op %q: SKILL.md identity=%v != effective identity %q", name, row.identity, effectiveIdentity(op))
		}
		if hasIdem && row.idempotent != op.IdempotentHint() {
			t.Errorf("op %q: SKILL.md idempotent=%v != code IdempotentHint=%v", name, row.idempotent, op.IdempotentHint())
		}
	}
}

type skillRow struct {
	method     string
	path       string
	access     string
	signed     bool
	identity   bool
	idempotent bool
}

var (
	skillRowRe = regexp.MustCompile("\`mexc ${DOMAIN} ${SUB} ([a-z0-9-]+)\`")
	apiCellRe  = regexp.MustCompile("\`([A-Z]+) (/[^\`]+)\`")
	fmFieldRe  = regexp.MustCompile(\`(?m)^([a-z]+):\s*(.+?)\s*\$\`)
)

// parseSkillTable reads the operation table into name -> row. It locates columns
// BY HEADER NAME (Command / access / signed / identity / Endpoint), not by
// fixed position, so the table can gain/reorder columns (e.g. an idempotent
// column) without breaking this test. Only lines that start with "|" are treated
// as table rows, so example commands in prose/bash blocks are ignored.
func parseSkillTable(content string) (map[string]skillRow, bool) {
	rows := map[string]skillRow{}
	lines := strings.Split(content, "\n")

	// Map column name -> index from the Operations table header row.
	colIdx := map[string]int{}
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "command") && strings.Contains(low, "endpoint") {
			for i, c := range strings.Split(line, "|") {
				colIdx[strings.ToLower(strings.TrimSpace(c))] = i
			}
			break
		}
	}
	if len(colIdx) == 0 {
		return rows, false
	}
	_, hasIdem := colIdx["idempotent"]
	cell := func(cells []string, name string) string {
		if i, ok := colIdx[name]; ok && i < len(cells) {
			return strings.TrimSpace(cells[i])
		}
		return ""
	}

	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		m := skillRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		cells := strings.Split(line, "|")
		row := skillRow{
			access:     cell(cells, "access"),
			signed:     yes(cell(cells, "signed")),
			identity:   yes(cell(cells, "identity")),
			idempotent: yes(cell(cells, "idempotent")),
		}
		if a := apiCellRe.FindStringSubmatch(cell(cells, "endpoint")); a != nil {
			row.method = a[1]
			row.path = a[2]
		}
		rows[m[1]] = row
	}
	return rows, hasIdem
}

func yes(cell string) bool {
	s := strings.TrimSpace(cell)
	return s == "yes" || s == "true"
}

func frontmatterField(content, key string) string {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return ""
	}
	for _, m := range fmFieldRe.FindAllStringSubmatch(parts[1], -1) {
		if m[1] == key {
			return m[2]
		}
	}
	return ""
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
EOF

# Normalize the generated Go so the scaffold is gofmt-clean out of the box (the
# pre-commit hook runs gofmt, and a scaffold that fails it is a bad first
# impression). Best-effort: skip silently when gofmt is unavailable.
command -v gofmt >/dev/null 2>&1 && gofmt -w "$DIR/api" || true

echo "  ✓ skills/${DOMAIN}/${SUB}/{SKILL.md, api/${SUB}.go, api/contract_test.go} (visibility: ${VISIBILITY})"
echo
echo "Next steps (the script does not do these for you):"
echo "  1. Register a row under the \"${DOMAIN}\" section of SUBDOMAINS.md"
echo "     (match the column order/values of the existing rows in that file):"
echo "       | \`${SUB}\` | meaning | \`${DOMAIN}.${SUB}\` | read | ${VISIBILITY} | no | no |"
# Steps 2 and 3 wire the sub-domain in. The edition's domains.go is the sole
# importer of a sub-domain's api package, and embed.go is the sole manifest of
# its skill files — check-boundary Invariant 9 fails the build if a skill on disk
# is missing from the manifest, and Invariant 10 keeps the edition off internal/.
echo "  2. Wire it into the edition — editions/public/domains.go. Append to the"
echo "     \"${DOMAIN}\" domain's Subs:"
echo "       {"
echo "           Name:     \"${SUB}\","
echo "           Short:    \"…\","
echo "           Signed:   ${PKG}.Signed,"
echo "           Identity: ${PKG}.Identity,"
echo "           HostKey:  hosts.KeyOpenAPI,"
echo "           Ops:      ${PKG}.Operations(),"
echo "       },"
echo "     import  ${PKG} \"${MODULE}/skills/${DOMAIN}/${SUB}/api\"  and pkg/hosts in that file."
echo "     HostKey is MANDATORY (hosts.KeyOpenAPI is the OpenAPI host); assembly"
echo "     (validateRegistry) refuses to build if an op's effective host key is empty."
echo "     (New domain? Add a whole api.DomainSpec entry instead of a Subs row.)"
echo "  3. Embed its SKILL.md in embed.go:"
echo "       //go:embed skills/${DOMAIN}/${SUB}/SKILL.md"
echo "     Shipping supporting docs too? Add the directory as well, e.g.:"
echo "       //go:embed skills/${DOMAIN}/${SUB}/references"
echo "     (references/ scripts/ assets/ travel with the skill; api/*.go never does.)"
echo "  4. Replace example with real operations; keep the SKILL.md tables and api/${SUB}.go in sync."
echo "  5. Self-check: go build ./...  &&  go test ./...  &&  python3 scripts/check-boundary.py"
