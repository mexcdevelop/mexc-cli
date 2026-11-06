package api

import "testing"

func TestBuildSpec_RoutesByLocation(t *testing.T) {
	op := Operation{
		Method: "GET", Path: "/x/{id}",
		Params: []Param{
			{Name: "id", In: InPath, Required: true},
			{Name: "symbol", In: InQuery},
			{Name: "token", In: InHeader},
			{Name: "session", In: InCookie},
		},
	}
	values := map[string]string{
		"id": "42", "symbol": "BTCUSDT", "token": "abc", "session": "sid-1",
	}
	spec, err := op.BuildSpec(values, "", nil, testSchemeHMAC, testIdentityAPIKey, "spot")
	if err != nil {
		t.Fatalf("BuildSpec returned an error: %v", err)
	}
	if spec.Path != "/x/42" {
		t.Fatalf("path placeholder not substituted: %+v", spec.Path)
	}
	if spec.Query["symbol"] != "BTCUSDT" {
		t.Fatalf("query param not routed: %+v", spec.Query)
	}
	if spec.Header["token"] != "abc" {
		t.Fatalf("header param not routed: %+v", spec.Header)
	}
	if spec.Cookie["session"] != "sid-1" {
		t.Fatalf("cookie param not routed: %+v", spec.Cookie)
	}
	if spec.HostKey != "spot" {
		t.Fatalf("HostKey not carried through: %+v", spec.HostKey)
	}
	if spec.Signed != testSchemeHMAC {
		t.Fatalf("Signed not carried through: %+v", spec.Signed)
	}
	if spec.Identity != testIdentityAPIKey {
		t.Fatalf("Identity not carried through: %+v", spec.Identity)
	}
}

func TestBuildSpec_VersionParam(t *testing.T) {
	// `version` is a normal wire query param (no longer reserved). It must reach
	// the query string unchanged, so the CLI never blocks a backend `version` key.
	op := Operation{
		Method: "GET", Path: "/api/v3/example",
		Params: []Param{{Name: "version", Required: true}},
	}
	spec, err := op.BuildSpec(map[string]string{"version": "v3"}, "", nil, SignNone, IdentityNone, "spot")
	if err != nil {
		t.Fatalf("BuildSpec returned an error: %v", err)
	}
	if spec.Query["version"] != "v3" {
		t.Fatalf("query[version] = %q, want v3", spec.Query["version"])
	}
}

func TestBuildSpec_JSONBody(t *testing.T) {
	type freezeReq struct {
		Note string `json:"note"`
	}
	op := Operation{Method: "POST", Path: "/x", Body: freezeReq{}}
	spec, err := op.BuildSpec(nil, `{"a":"1"}`, nil, SignNone, IdentityNone, "")
	if err != nil {
		t.Fatalf("BuildSpec returned an error: %v", err)
	}
	if string(spec.Body) != `{"a":"1"}` {
		t.Fatalf("body not carried through: %q", spec.Body)
	}
}

func TestBuildSpec_Multipart(t *testing.T) {
	type meta struct {
		Kind string `json:"kind"`
	}
	op := Operation{
		Method: "POST", Path: "/upload",
		Multipart: &MultipartDecl{
			Files:  []MultipartFile{{Field: "file", Required: true}},
			Fields: meta{},
		},
	}
	files := []FilePart{{Field: "file", Filename: "a.png", Content: []byte("PNG")}}
	spec, err := op.BuildSpec(nil, `{"kind":"avatar"}`, files, SignNone, testIdentityLogin, "example-host")
	if err != nil {
		t.Fatalf("BuildSpec returned an error: %v", err)
	}
	if spec.Multipart == nil {
		t.Fatal("Multipart not attached to spec")
	}
	if len(spec.Multipart.Files) != 1 || spec.Multipart.Files[0].Field != "file" ||
		string(spec.Multipart.Files[0].Content) != "PNG" {
		t.Fatalf("file part not carried through: %+v", spec.Multipart.Files)
	}
	if spec.Multipart.Fields["kind"] != "avatar" {
		t.Fatalf("text field not parsed from --data: %+v", spec.Multipart.Fields)
	}
	// The JSON Body path must stay untouched for a multipart op.
	if spec.Body != nil {
		t.Fatalf("multipart op must not set a JSON Body: %q", spec.Body)
	}
}

// A non-string --data scalar (number/bool) is rendered to its string form, since
// multipart form fields are strings on the wire.
func TestBuildSpec_MultipartScalarFields(t *testing.T) {
	type meta struct {
		N int  `json:"n"`
		B bool `json:"b"`
	}
	op := Operation{
		Method: "POST", Path: "/upload",
		Multipart: &MultipartDecl{Files: []MultipartFile{{Field: "file"}}, Fields: meta{}},
	}
	spec, err := op.BuildSpec(nil, `{"n":42,"b":true}`, nil, SignNone, testIdentityLogin, "example-host")
	if err != nil {
		t.Fatalf("BuildSpec returned an error: %v", err)
	}
	if spec.Multipart.Fields["n"] != "42" || spec.Multipart.Fields["b"] != "true" {
		t.Fatalf("scalar fields not stringified: %+v", spec.Multipart.Fields)
	}
}

func TestValidateDecl_BodyKindsMutuallyExclusive(t *testing.T) {
	type freezeReq struct {
		Note string `json:"note"`
	}
	op := base()
	op.Method = "POST"
	op.Body = freezeReq{}
	op.Params = []Param{{Name: "note", In: InForm}}
	wantDeclErr(t, op, "at most one body kind")
}

func TestValidateDecl_MultipartRequiresFileField(t *testing.T) {
	op := base()
	op.Method = "POST"
	op.Multipart = &MultipartDecl{}
	wantDeclErr(t, op, "at least one file field")

	op = base()
	op.Method = "POST"
	op.Multipart = &MultipartDecl{Files: []MultipartFile{{Field: "doc"}}}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a well-formed Multipart declaration must pass: %v", err)
	}
}

func TestBuildSpecThreadsPermission(t *testing.T) {
	op := Operation{
		Name: "x", Method: "GET", Path: "/p", Access: Read,
		Permission: &PermReq{System: "example-admin", Codes: []string{"a:b"}},
	}
	spec, err := op.BuildSpec(nil, "", nil, "", testIdentityLogin, "example-host")
	if err != nil {
		t.Fatalf("BuildSpec error = %v", err)
	}
	if spec.Permission == nil || spec.Permission.System != "example-admin" ||
		len(spec.Permission.Codes) != 1 || spec.Permission.Codes[0] != "a:b" {
		t.Fatalf("spec.Permission = %+v, want the op's PermReq", spec.Permission)
	}
}
