package api

import (
	"strings"
	"testing"
)

// base returns a minimal valid declaration; each test mutates one aspect.
func base() Operation {
	return Operation{
		Name: "example", Method: "GET", Path: "/api/v3/example",
		Access: Read, Summary: "example op",
		Params: []Param{{Name: "symbol", Desc: "pair", Required: true}},
	}
}

func TestValidateDecl_MinimalValid(t *testing.T) {
	if err := base().ValidateDecl(); err != nil {
		t.Fatalf("minimal declaration must be valid: %v", err)
	}
}

func TestEnums_ValidAndConcrete(t *testing.T) {
	// Zero values are valid (inherit / query) but not concrete.
	if Signing("").Concrete() {
		t.Fatal("zero Signing must not be concrete")
	}
	if Identity("").Concrete() {
		t.Fatal("zero Identity must not be concrete")
	}
	if !ParamIn("").Valid() {
		t.Fatal("zero ParamIn must be valid (query)")
	}
	// InQuery and the zero value are equivalent (both mean query).
	if !InQuery.Valid() || !InQuery.IsQuery() || !ParamIn("").IsQuery() {
		t.Fatal("InQuery and the zero value must both count as query")
	}
	if InPath.IsQuery() {
		t.Fatal("InPath is not query")
	}
	// Neither Signing nor Identity has a Valid(): the shared core does not know
	// which schemes or identities exist, so there is no list to check against.
	// Concreteness is all this layer can say; whether a value is REAL — has a
	// signer / a provider registered — is asserted at assembly, which is stricter
	// than a list ever was.
	for _, s := range []Signing{SignNone, testSchemeHMAC} {
		if !s.Concrete() {
			t.Fatalf("scheme %q must be concrete", s)
		}
	}
	for _, i := range []Identity{IdentityNone, testIdentityAPIKey, testIdentityLogin} {
		if !i.Concrete() {
			t.Fatalf("identity %q must be concrete", i)
		}
	}
	// Anything else is invalid — the closed-enum property.
	if ParamIn("body").Valid() {
		t.Fatal("body is not a param location (use Operation.Body)")
	}
}

// wantDeclErr asserts ValidateDecl fails and the message carries a fragment.
func wantDeclErr(t *testing.T, op Operation, fragment string) {
	t.Helper()
	err := op.ValidateDecl()
	if err == nil {
		t.Fatalf("expected declaration error containing %q, got nil", fragment)
	}
	if !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error %q does not mention %q", err.Error(), fragment)
	}
}

func TestValidateDecl_CoreFields(t *testing.T) {
	op := base()
	op.Summary = ""
	wantDeclErr(t, op, "required")

	op = base()
	op.Access = "delete"
	wantDeclErr(t, op, "access")

	// An undefined identity is NOT a declaration error any more: this layer has
	// no list to compare against. It is caught at assembly instead, where the
	// provider registry can answer definitively — see
	// TestValidateRegistry_IdentityNeedsProvider in internal/cli.
}

func TestValidateDecl_Params(t *testing.T) {
	op := base()
	op.Params = append(op.Params, Param{Name: "symbol"})
	wantDeclErr(t, op, "declared twice")

	op = base()
	op.Params = []Param{{Name: ""}}
	wantDeclErr(t, op, "empty Name")

	op = base()
	op.Params = []Param{{Name: "profile"}} // reserved framework flag
	wantDeclErr(t, op, "reserved")

	op = base()
	op.Params = []Param{{Name: "x", In: "querystring"}}
	wantDeclErr(t, op, "undefined In")
}

func TestValidateDecl_VersionParamAllowed(t *testing.T) {
	// `version` is a legitimate backend wire param; cobra's --version is root-only
	// (not persistent), so a leaf op may declare it without collision.
	op := base()
	op.Params = []Param{{Name: "version", Desc: "API version", Required: true}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("`version` param must be allowed, got: %v", err)
	}
}

func TestValidateDecl_ReservedNamesStillRejected(t *testing.T) {
	// The genuinely-owned flags (persistent globals + help + per-op carriers)
	// stay reserved; only `version` was released.
	for _, name := range []string{"base-url", "read-only", "dry-run", "no-retry", "profile", "env", "help", "data", "file"} {
		op := base()
		op.Params = []Param{{Name: name}}
		wantDeclErr(t, op, "reserved")
	}
}

func TestValidateDecl_PathPlaceholders(t *testing.T) {
	// A placeholder with no matching path param.
	op := base()
	op.Path = "/wallets/{id}"
	wantDeclErr(t, op, "no matching Param")

	// A path param with no placeholder.
	op = base()
	op.Params = []Param{{Name: "id", In: InPath, Required: true}}
	wantDeclErr(t, op, "no {id} placeholder")

	// A path param that is not required.
	op = base()
	op.Path = "/wallets/{id}"
	op.Params = []Param{{Name: "id", In: InPath}}
	wantDeclErr(t, op, "Required: true")

	// Malformed brace.
	op = base()
	op.Path = "/wallets/{id"
	wantDeclErr(t, op, "malformed placeholder")

	// Duplicated placeholder.
	op = base()
	op.Path = "/w/{id}/x/{id}"
	op.Params = []Param{{Name: "id", In: InPath, Required: true}}
	wantDeclErr(t, op, "exactly once")

	// A fully wired path param (placeholder <-> In:path match, Required) is
	// now executable and must pass — the phase gate that used to reject
	// non-query locations at declaration time has been retired.
	op = base()
	op.Path = "/wallets/{id}"
	op.Params = []Param{{Name: "id", In: InPath, Required: true}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a well-formed path param must be allowed: %v", err)
	}
}

func TestValidateDecl_BodyRules(t *testing.T) {
	type freezeReq struct {
		Note string `json:"note"`
	}

	// A body on a GET is meaningless and rejected.
	op := base() // GET
	op.Body = freezeReq{}
	wantDeclErr(t, op, "POST/PUT/PATCH/DELETE")

	// A body on a body-bearing method is well-formed.
	op = base()
	op.Method = "POST"
	op.Body = freezeReq{}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a Body on POST must be allowed: %v", err)
	}

	// DELETE carries a body too: some admin endpoints take a batch/selector
	// body on DELETE (e.g. delete N records in one call).
	op = base()
	op.Method = "DELETE"
	op.Body = freezeReq{}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a Body on DELETE must be allowed: %v", err)
	}
}

func TestValidateDecl_EnumOnlyOnScalar(t *testing.T) {
	// An enum tag on an object field is meaningless — the enum compares a single
	// scalar value — so the declaration must fail loud at load time.
	type inner struct {
		A string `json:"a"`
	}
	type badObj struct {
		Obj inner `json:"obj" enum:"1,2"`
	}
	op := base()
	op.Method = "POST"
	op.Body = badObj{}
	wantDeclErr(t, op, "enum")

	// An enum tag on an array field is equally meaningless.
	type badArr struct {
		Items []inner `json:"items" enum:"1,2"`
	}
	op = base()
	op.Method = "POST"
	op.Body = badArr{}
	wantDeclErr(t, op, "enum")

	// An enum on an integer (or any scalar) field is legitimate and must pass.
	type okInt struct {
		Model int `json:"model" enum:"1,2"`
	}
	op = base()
	op.Method = "POST"
	op.Body = okInt{}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("enum on an integer field must be allowed: %v", err)
	}
}

// TestValidateDecl_EnumTokenMustMatchType: an enum token that cannot be parsed
// as the field's declared type is a declaration error caught at load time —
// otherwise the field types cleanly for the valid tokens but the bad one falls
// back to a string, silently making the field uncallable at runtime.
func TestValidateDecl_EnumTokenMustMatchType(t *testing.T) {
	type badInt struct {
		N int `json:"n" enum:"1,two"`
	}
	op := base()
	op.Method = "POST"
	op.Body = badInt{}
	wantDeclErr(t, op, "enum")

	type badBool struct {
		B bool `json:"b" enum:"true,maybe"`
	}
	op = base()
	op.Method = "POST"
	op.Body = badBool{}
	wantDeclErr(t, op, "enum")

	// Well-typed enums across every scalar kind must pass.
	type okAll struct {
		I int     `json:"i" enum:"1,2"`
		F float64 `json:"f" enum:"0.5,1.5"`
		B bool    `json:"b" enum:"true,false"`
		S string  `json:"s" enum:"a,b"`
	}
	op = base()
	op.Method = "POST"
	op.Body = okAll{}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("well-typed enums must pass: %v", err)
	}
}

func TestValidateDecl_PermissionShape(t *testing.T) {
	op := base()
	op.Permission = &PermReq{Codes: []string{"a"}}
	wantDeclErr(t, op, "System")

	op = base()
	op.Permission = &PermReq{System: "sys"}
	wantDeclErr(t, op, "at least one code")

	op = base()
	op.Permission = &PermReq{System: "sys", Codes: []string{"a", ""}}
	wantDeclErr(t, op, "empty code")

	op = base()
	op.Permission = &PermReq{System: "sys", Codes: []string{"a", "b"}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("well-shaped permission must pass: %v", err)
	}
}

func TestValidateDecl_ExplicitQueryParamOK(t *testing.T) {
	// InQuery and the zero value are equivalent (both mean query), so an
	// explicit InQuery must pass just like an omitted In.
	op := base()
	op.Params = []Param{{Name: "symbol", In: InQuery, Required: true}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("explicit InQuery must be allowed: %v", err)
	}
}

func TestValidateDecl_NonQueryLocationsAndHostKeyAllowed(t *testing.T) {
	// The phase gate that used to reject a non-empty HostKey and non-query
	// param locations at declaration time has been retired: their transports
	// are landing, so both are now accepted by ValidateDecl.
	op := base()
	op.HostKey = "demo-wallet"
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a declared HostKey must be allowed: %v", err)
	}

	op = base()
	op.Params = []Param{{Name: "req-id", In: InHeader}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("an In: InHeader param must be allowed: %v", err)
	}
}

func writeOp(body any) Operation {
	return Operation{Name: "w", Method: "POST", Path: "/w", Access: Write, Summary: "w", Body: body}
}

func TestValidateDecl_BodyConstraints(t *testing.T) {
	// Valid: numeric bounds on a number field, length/pattern on a string field.
	type good struct {
		N int    `json:"n" minval:"0" maxval:"100"`
		S string `json:"s" minlen:"1" maxlen:"20" pattern:"[a-z]+"`
	}
	if err := writeOp(good{}).ValidateDecl(); err != nil {
		t.Fatalf("valid constraints must pass: %v", err)
	}

	bad := []struct {
		name string
		body any
	}{
		{"minval on string", struct {
			S string `json:"s" minval:"1"`
		}{}},
		{"minlen on number", struct {
			N int `json:"n" minlen:"1"`
		}{}},
		{"pattern on number", struct {
			N int `json:"n" pattern:"[0-9]+"`
		}{}},
		{"minval > maxval", struct {
			N int `json:"n" minval:"10" maxval:"1"`
		}{}},
		{"minlen > maxlen", struct {
			S string `json:"s" minlen:"5" maxlen:"2"`
		}{}},
		{"malformed minval", struct {
			N int `json:"n" minval:"abc"`
		}{}},
		{"malformed maxlen", struct {
			S string `json:"s" maxlen:"x"`
		}{}},
		{"uncompilable pattern", struct {
			S string `json:"s" pattern:"[a-z"`
		}{}},
	}
	for _, c := range bad {
		if err := writeOp(c.body).ValidateDecl(); err == nil {
			t.Errorf("%s: expected a declaration error", c.name)
		}
	}
}

func TestValidateDecl_ParamConstraints(t *testing.T) {
	mk := func(p Param) Operation {
		o := base()
		o.Params = append(o.Params, p)
		return o
	}
	// Valid.
	if err := mk(Param{Name: "q", MinVal: Ptr(1.0), MaxVal: Ptr(9.0)}).ValidateDecl(); err != nil {
		t.Fatalf("valid param constraints must pass: %v", err)
	}
	// MinVal > MaxVal.
	if err := mk(Param{Name: "q", MinVal: Ptr(9.0), MaxVal: Ptr(1.0)}).ValidateDecl(); err == nil {
		t.Error("MinVal > MaxVal must be a declaration error")
	}
	// MinLen > MaxLen.
	if err := mk(Param{Name: "q", MinLen: 5, MaxLen: 2}).ValidateDecl(); err == nil {
		t.Error("MinLen > MaxLen must be a declaration error")
	}
	// Bad pattern.
	if err := mk(Param{Name: "q", Pattern: "[a-z"}).ValidateDecl(); err == nil {
		t.Error("an uncompilable Pattern must be a declaration error")
	}
}

// testIdentityAPIKey / testIdentityLogin stand in for the concrete identities
// this package deliberately does not declare. pkg/api names only IdentityNone;
// a concrete identity belongs to whoever supplies its provider, and those
// declarations live in packages that import THIS one — so a test here cannot
// reference them without a cycle, and should not want to.
const (
	testIdentityAPIKey Identity = "test-apikey"
	testIdentityLogin  Identity = "test-login"
	testSchemeHMAC     Signing  = "test-hmac"
)
