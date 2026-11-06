package marketapi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// These tests keep the code declarations and the SKILL.md doc in sync.
// Adapted from spot/market/api's contract test; the one difference specific
// to this subdomain is that every operation is Kind: api.KindWSCollect
// (Method/Path are both empty), so the SKILL.md Endpoint cell is a
// descriptive `WS <url>#<channel>` string rather than a `METHOD /path` cell
// — which never matches apiCellRe, leaving the parsed method/path empty,
// matching the code.

// Contract: every Operation declaration is well-formed, and the sample ops
// demonstrate the golden pattern: inherit the subdomain defaults (Sign /
// Identity omitted), never restate them per op.
func TestOps_FieldsValid(t *testing.T) {
	for _, op := range Operations() {
		if err := op.ValidateDecl(); err != nil {
			t.Errorf("op %q: %v", op.Name, err)
		}
		if op.Signed.Concrete() {
			t.Errorf("op %q overrides Sign; sample ops must inherit the subdomain default", op.Name)
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

// Contract: market is entirely read (a semantic constraint of the public
// market-data subdomain).
func TestOps_AllRead(t *testing.T) {
	for _, op := range Operations() {
		if op.Access != api.Read {
			t.Errorf("market op %q must be read, got %q", op.Name, op.Access)
		}
	}
}

// Contract: every operation here is KindWSCollect (this subdomain carries no
// REST operations yet).
func TestOps_AllKindWSCollect(t *testing.T) {
	for _, op := range Operations() {
		if op.Kind != api.KindWSCollect {
			t.Errorf("op %q must be KindWSCollect; this subdomain carries no REST operations", op.Name)
		}
	}
}

// Contract: the SKILL.md operation table and the code Operations() agree on
// every field, both ways.
func TestOps_MatchSkillTable(t *testing.T) {
	doc, err := os.ReadFile("../SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(doc)

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
	// Matches a table row whose first cell is `mexc futures market <op>`.
	skillRowRe = regexp.MustCompile("`mexc futures market ([a-z0-9-]+)`")
	// Matches a `METHOD /path` Endpoint cell; a `WS wss://...` cell never
	// matches (its second token does not start with '/'), which is what makes
	// the parsed method/path empty for every op here, matching the code.
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
		cols := strings.Split(line, "|")
		if len(cols) < 8 {
			continue
		}
		row := skillRow{
			access:     strings.TrimSpace(cols[3]),
			signed:     yes(cols[4]),
			identity:   yes(cols[5]),
			idempotent: yes(cols[6]),
		}
		if apiCell := apiCellRe.FindStringSubmatch(cols[7]); apiCell != nil {
			row.method = apiCell[1]
			row.path = apiCell[2]
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
