package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSchema_SurfacesBinaryResponseKind(t *testing.T) {
	bin := Operation{
		Name: "download", Method: "GET", Path: "/files/{id}/download",
		Access: Read, Summary: "download", ResponseKind: ResponseBinary,
		Params: []Param{{Name: "id", In: InPath, Required: true}},
	}
	b, _ := json.Marshal(bin.Schema("spot.files.download", SignNone, IdentityNone))
	if !strings.Contains(string(b), `"response_kind":"binary"`) {
		t.Fatalf("schema should surface the binary response kind: %s", b)
	}

	// A plain JSON op omits the field entirely (it is the default).
	jsonOp := Operation{Name: "get", Method: "GET", Path: "/x", Access: Read, Summary: "get"}
	b2, _ := json.Marshal(jsonOp.Schema("spot.x.get", SignNone, IdentityNone))
	if strings.Contains(string(b2), "response_kind") {
		t.Fatalf("a JSON op must not carry response_kind: %s", b2)
	}
}

func TestResponseKind_Enum(t *testing.T) {
	// The zero value is JSON: existing operations declare nothing and stay JSON.
	if !ResponseKind("").Valid() || ResponseKind("").IsBinary() {
		t.Fatal("zero ResponseKind must be valid and mean JSON (not binary)")
	}
	if !ResponseJSON.Valid() || ResponseJSON.IsBinary() {
		t.Fatal("ResponseJSON must be valid and not binary")
	}
	if !ResponseBinary.Valid() || !ResponseBinary.IsBinary() {
		t.Fatal("ResponseBinary must be valid and binary")
	}
	if ResponseKind("stream").Valid() {
		t.Fatal("an undefined response kind must be invalid")
	}
}

func TestBuildSpec_PropagatesResponseKind(t *testing.T) {
	o := Operation{
		Name: "download", Method: "GET", Path: "/files/{id}/download",
		Access: Read, Summary: "download a file", ResponseKind: ResponseBinary,
		Params: []Param{{Name: "id", In: InPath, Required: true}},
	}
	spec, err := o.BuildSpec(map[string]string{"id": "abc"}, "", nil, SignNone, IdentityNone, "h")
	if err != nil {
		t.Fatalf("BuildSpec: %v", err)
	}
	if spec.ResponseKind != ResponseBinary {
		t.Fatalf("ResponseKind not propagated to spec: %q", spec.ResponseKind)
	}
}

func TestValidateDecl_BinaryResponseRules(t *testing.T) {
	// A read GET download is the canonical valid binary operation.
	ok := Operation{
		Name: "download", Method: "GET", Path: "/files/{id}/download",
		Access: Read, Summary: "download", ResponseKind: ResponseBinary,
		Params: []Param{{Name: "id", In: InPath, Required: true}},
	}
	if err := ok.ValidateDecl(); err != nil {
		t.Fatalf("a read GET binary op must be valid: %v", err)
	}

	// A binary response that is also a write is a contradiction: a download does
	// not mutate server state, and read access is what grants it --read-only /
	// no-write-confirmation treatment.
	w := ok
	w.Access = Write
	w.Method = "POST"
	wantDeclErr(t, w, "binary")

	// Multipart (a file upload) and a streamed binary response are not modeled
	// together.
	m := ok
	m.Multipart = &MultipartDecl{Files: []MultipartFile{{Field: "file"}}}
	wantDeclErr(t, m, "binary")
}
