package convertapi

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// Guards the "code declaration <-> SKILL.md doc" consistency, the way the other
// spot subdomains do; passes go test out of the box and keeps self-checking as
// operations change.
//
// signed/identity are compared by FACT: the SKILL.md uses yes/no while the code
// uses the api.Sign* / identity.* enums, so the test asks only "is signing
// required?" / "is a credential required?" — never literal equality.

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

// TestSubdomain_GatewaySessionAuthShape pins the intended auth shape of this
// subdomain, which is not the usual one. Elsewhere identity=openapi means api-key
// auth and therefore signing. Both axes now carry weight here: identity=openapi
// means a credential is required, and the gateway scheme means the client signs
// the request with that credential's secret. So a profile needs both halves.
//
// The scheme is pinned rather than merely asserted non-none, because the SKILL.md
// frontmatter and the ops table both derive `signed` from this constant — a change
// of scheme silently rewrites what they claim. It also has to stay a body-capable
// scheme: place and place-limit carry a JSON body, and the assembly layer refuses
// to pair a query-only scheme (such as the spot api-key scheme) with one.
func TestSubdomain_GatewaySignedAuthShape(t *testing.T) {
	if Signed != signing.CliHMAC {
		t.Errorf("subdomain Signed=%q, want %q: the gateway verifies a client "+
			"signature on these endpoints", Signed, signing.CliHMAC)
	}
	if Identity != identity.OpenAPI {
		t.Errorf("subdomain Identity=%q, want openapi: the convert flow is per-member "+
			"(it moves the caller's assets), so a credential must be required", Identity)
	}
	for _, op := range Operations() {
		if op.Signed.Concrete() {
			t.Errorf("op %q declares signing scheme %q; ops must inherit the subdomain "+
				"default", op.Name, op.Signed)
		}
	}
}

// identityOverrideAllowed names the ops that legitimately depart from the
// subdomain identity default, and the identity each one must declare. Every other
// op inherits it — restating a default per op is the drift this guards against, so
// the exception list is explicit rather than open.
//
// It is EMPTY, and that is the point: no convert endpoint is reachable without a
// credential. quantity-limit used to be listed here, on the grounds that the
// backend excludes its path from the member interceptor and its handler never
// reads a member. Both are true of the service and neither decides the question:
// the gateway in front of it authenticates the whole /api/cli/* surface, so the
// call was refused with `api key required` (HTTP 400) before reaching that
// handler — the operation was simply unusable. Verified against the gateway, with
// and without the CLI. Keep this map empty unless an endpoint is shown to answer
// with no key.
var identityOverrideAllowed = map[string]api.Identity{}

func TestOps_IdentityOverridesAreDocumented(t *testing.T) {
	for _, op := range Operations() {
		if want, ok := identityOverrideAllowed[op.Name]; ok {
			if op.Identity != want {
				t.Errorf("op %q must declare Identity=%q, got %q", op.Name, want, op.Identity)
			}
			continue
		}
		if op.Identity.Concrete() {
			t.Errorf("op %q overrides Identity; ops must inherit the subdomain default "+
				"unless listed in identityOverrideAllowed", op.Name)
		}
	}
}

// Contract: an op carrying a request body must resolve to a scheme that can sign
// one. The assembly layer (internal/cli.validateRegistry, via signer.CanSignBody)
// refuses a query-only scheme paired with a body and the binary then fails to
// start, so catch it here, where the declaration lives.
//
// This stopped being vacuous when the subdomain adopted a signing scheme: place
// and place-limit now sit under one, so swapping it for a query-only scheme breaks
// both, and breaks them at startup rather than at call time.
func TestOps_BodyOpsResolveToABodyCapableScheme(t *testing.T) {
	bodyCapable := map[api.Signing]bool{signing.CliHMAC: true}
	for _, op := range Operations() {
		if op.Body == nil {
			continue
		}
		if scheme := effectiveSigned(op); scheme.Signs() && !bodyCapable[scheme] {
			t.Errorf("op %q carries a request body but resolves to signing scheme %q, "+
				"which does not cover a body; assembly rejects that pairing", op.Name, scheme)
		}
	}
}

// Contract: the write semantics the retry layer and the MCP safety hints depend
// on. The writes here are deliberately of two kinds, so each is pinned
// individually rather than by one blanket rule:
//
//   - place / place-limit only ADD state (non-destructive), but repeating either
//     converts again, so they must stay non-idempotent and are never auto-retried.
//   - cancel-limit DOES unmake a resting order (destructive), yet re-cancelling
//     an order that is already gone changes nothing, so it converges and is
//     idempotent — the one write here that may be retried.
func TestOps_WriteSemantics(t *testing.T) {
	type want struct{ destructive, idempotent bool }
	writes := map[string]want{
		"place":        {destructive: false, idempotent: false},
		"place-limit":  {destructive: false, idempotent: false},
		"cancel-limit": {destructive: true, idempotent: true},
	}
	var seen int
	for _, op := range Operations() {
		w, ok := writes[op.Name]
		if !ok {
			if op.IsWrite() {
				t.Errorf("op %q is a write but is not listed in the write-semantics contract", op.Name)
			}
			continue
		}
		seen++
		if !op.IsWrite() {
			t.Errorf("op %q must be access=write", op.Name)
		}
		if op.DestructiveHint() != w.destructive {
			t.Errorf("op %q: DestructiveHint=%v, want %v", op.Name, op.DestructiveHint(), w.destructive)
		}
		if op.IdempotentHint() != w.idempotent {
			t.Errorf("op %q: IdempotentHint=%v, want %v", op.Name, op.IdempotentHint(), w.idempotent)
		}
	}
	if seen != len(writes) {
		t.Errorf("expected all %d write ops to exist, found %d", len(writes), seen)
	}
}

// TestPlaceLimit_BodyWireForms pins the body shape of place-limit, whose two
// optional fields are easy to get wrong in the same direction twice: both look
// like integer-coded enums, but only one is.
//
//   - expireType is a plain integer code upstream, so it must be an integer
//     field here (a JSON number on the wire), not a quoted string.
//   - accountType is a NAMED enum upstream, so its wire form is the constant
//     name. Declaring it as the codes "0"/"1" would send a value the backend
//     does not promise to read.
//
// The required set is also pinned: the backend derives none of the three amount
// fields from the others, so all of them (plus both currency ids) must be sent.
func TestPlaceLimit_BodyWireForms(t *testing.T) {
	var op *api.Operation
	for _, o := range Operations() {
		if o.Name == "place-limit" {
			op = &o
			break
		}
	}
	if op == nil {
		t.Fatal("place-limit operation is missing")
	}
	fields := map[string]api.BodyField{}
	for _, f := range op.BodyFields() {
		fields[f.Name] = f
	}

	wantRequired := []string{"fromCurrencyId", "toCurrencyId", "fromConvertQuantity", "toConvertQuantity", "convertRate"}
	for _, name := range wantRequired {
		f, ok := fields[name]
		if !ok {
			t.Errorf("body field %q is missing", name)
			continue
		}
		if !f.Required {
			t.Errorf("body field %q must be required", name)
		}
	}

	if f := fields["expireType"]; f.Type != "integer" {
		t.Errorf("expireType type=%q, want integer: the backend DTO holds an Integer code, so it travels as a JSON number", f.Type)
	} else if len(f.Enum) != 5 {
		t.Errorf("expireType should enumerate the 5 lifetime codes, got %v", f.Enum)
	}

	f, ok := fields["accountType"]
	if !ok {
		t.Fatal("body field accountType is missing")
	}
	if f.Type != "string" {
		t.Errorf("accountType type=%q, want string: the wire form is the enum NAME", f.Type)
	}
	if len(f.Enum) != 2 || f.Enum[0] != "SPOT" || f.Enum[1] != "CONTRACT" {
		t.Errorf("accountType enum=%v, want [SPOT CONTRACT] — the backend enum is read by name, not by code", f.Enum)
	}
	if f.Required {
		t.Error("accountType must stay optional: it defaults to SPOT upstream")
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
	skillRowRe = regexp.MustCompile("`mexc spot convert ([a-z0-9-]+)`")
	apiCellRe  = regexp.MustCompile("`([A-Z]+) (/[^`]+)`")
	fmFieldRe  = regexp.MustCompile(`(?m)^([a-z]+):\s*(.+?)\s*$`)
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
