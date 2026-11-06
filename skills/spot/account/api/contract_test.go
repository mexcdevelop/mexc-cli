package accountapi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// These tests keep the code declarations and the SKILL.md doc in sync.

// Contract: every Operation declaration is well-formed (ValidateDecl covers
// core fields, enum values, params, and path placeholders), and the sample ops
// demonstrate the golden pattern: inherit the subdomain defaults (Sign /
// Identity omitted), never restate them per op — EXCEPT a KindWSCollect
// (WS-backed) op, which legitimately overrides Signed to api.SignNone: it
// never builds a RequestSpec of its own, so the signing axis does not apply
// to it (a private channel's one-shot preflight, if any, is a separate
// RequestSpec, signed on its own terms). Identity still must be inherited
// even for a KindWSCollect op: the same api-key/secret source every other
// operation in this subdomain uses.
func TestOps_FieldsValid(t *testing.T) {
	for _, op := range Operations() {
		if err := op.ValidateDecl(); err != nil {
			t.Errorf("op %q: %v", op.Name, err)
		}
		if op.Kind != api.KindWSCollect && op.Signed.Concrete() {
			t.Errorf("op %q overrides Sign; sample ops must inherit the subdomain default", op.Name)
		}
		if op.Kind == api.KindWSCollect && op.Signed != api.SignNone {
			t.Errorf("op %q is a KindWSCollect operation and must override Signed to api.SignNone", op.Name)
		}
		if op.Identity.Concrete() {
			t.Errorf("op %q overrides Identity; sample ops must inherit the subdomain default", op.Name)
		}
	}
}

// effectiveSigned mirrors the assembly-layer inheritance for this subdomain: a
// concrete op-level value overrides, the zero value inherits the Sign constant.
func effectiveSigned(op api.Operation) api.Signing {
	if op.Signed.Concrete() {
		return op.Signed
	}
	return Signed
}

// Contract: the subdomain default is the CLI gateway HMAC scheme, so an
// operation added here without an explicit override signs. No operation
// currently inherits it — every one is a KindWSCollect op that overrides
// Signed to none — but ws-deals' listenKey preflight signs with this scheme,
// which is why the default is a scheme rather than none.
func TestSubdomain_SignsWithCliHMAC(t *testing.T) {
	if Signed != signing.CliHMAC {
		t.Errorf("subdomain Signed = %q, want cli-hmac", Signed)
	}
}

// Contract: every operation is a read. A write here would need its own review
// (confirmation class, MCP safety hints, the doc's permissions table), so it
// must not arrive unnoticed.
func TestOps_NoWrites(t *testing.T) {
	var writes []string
	for _, op := range Operations() {
		if op.IsWrite() {
			writes = append(writes, op.Name)
		}
	}
	if len(writes) != 0 {
		t.Errorf("account writes = %v, want none", writes)
	}
}

// Contract: the SKILL.md operation table and the code Operations() agree on
// name, HTTP method, path, and access, both ways. (signed is not a per-row
// column in this subdomain; it is asserted via the frontmatter below.)
func TestOps_MatchSkillTable(t *testing.T) {
	doc, err := os.ReadFile("../SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(doc)

	// The frontmatter mirrors the code constants as yes/no facts: signed is yes
	// iff the subdomain's default scheme is not none, and identity is yes iff the
	// subdomain carries an identity (const Identity != none); which identity is in
	// the const, and is not asserted in the doc.
	if got := frontmatterField(content, "signed"); got != boolStr(Signed != api.SignNone) {
		t.Errorf("SKILL.md frontmatter signed=%q but subdomain Sign=%q", got, Signed)
	}
	if got := frontmatterField(content, "identity"); got != boolStr(Identity != api.IdentityNone) {
		t.Errorf("SKILL.md frontmatter identity=%q but code Identity=%q", got, Identity)
	}
	if got := frontmatterField(content, "scope"); got != Scope {
		t.Errorf("SKILL.md frontmatter scope=%q but code Scope=%q", got, Scope)
	}

	docRows := parseSkillTable(content)
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
			t.Errorf("op %q: SKILL.md signed=%v != effective sign %q", name, row.signed, effectiveSigned(op))
		}
		if want := Identity != api.IdentityNone; row.identity != want {
			t.Errorf("op %q: SKILL.md identity=%v != code %v", name, row.identity, want)
		}
		if row.idempotent != op.IdempotentHint() {
			t.Errorf("op %q: SKILL.md idempotent=%v != code %v", name, row.idempotent, op.IdempotentHint())
		}
	}
}

// skillRow is one parsed operation-table row.
type skillRow struct {
	method     string
	path       string
	access     string
	signed     bool
	identity   bool
	idempotent bool
}

var (
	// Matches a table row whose first cell is `mexc spot account <op>`.
	skillRowRe = regexp.MustCompile("`mexc spot account ([a-z0-9-]+)`")
	// Matches the Endpoint cell, e.g. `GET /api/v3/account`.
	apiCellRe = regexp.MustCompile("`([A-Z]+) (/[^`]+)`")
	fmFieldRe = regexp.MustCompile(`(?m)^([a-z]+):\s*(.+?)\s*$`)
)

// parseSkillTable reads the operation table into name -> {method,path,access,signed,identity,idempotent}.
// Column layout: `Command | scope | access | signed | identity | idempotent | Endpoint | Description`.
func parseSkillTable(content string) map[string]skillRow {
	rows := map[string]skillRow{}
	yes := func(col string) bool { v := strings.TrimSpace(col); return v == "yes" || v == "true" }
	for _, line := range strings.Split(content, "\n") {
		m := skillRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		cols := strings.Split(line, "|") // leading/trailing empties from the | borders
		// cols: ["", Command, scope, access, signed, identity, idempotent, `GET /path`, desc, ""]
		if len(cols) < 8 {
			continue
		}
		row := skillRow{
			access:     strings.TrimSpace(cols[3]),
			signed:     yes(cols[4]),
			identity:   yes(cols[5]),
			idempotent: yes(cols[6]),
		}
		if api := apiCellRe.FindStringSubmatch(cols[7]); api != nil {
			row.method = api[1]
			row.path = api[2]
		}
		rows[name] = row
	}
	return rows
}

// frontmatterField returns a single field value from the --- delimited frontmatter.
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

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
