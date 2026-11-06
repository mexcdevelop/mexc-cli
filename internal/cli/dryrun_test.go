package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// What a dry-run does about the request body.
//
// The rule these tests pin: a dry-run validates what it WAS GIVEN and never
// demands what it was not. Both halves are load-bearing and each has its own
// failure mode if it drifts. Validate too little and a green preview goes back
// for a call already known to be rejectable — on a write, that green survives
// until after a human has been asked to confirm it. Demand too much and
// `<op> --dry-run` stops answering "where would this go?", which is the question
// a caller asks before it has built any payload at all.

// dryRunListReq exercises the three things body validation decides: a required
// field, a closed enum, and (by being a struct at all) an unknown field.
type dryRunListReq struct {
	Limit int    `json:"limit" validate:"required" desc:"Page size"`
	Tab   string `json:"tab,omitempty" enum:"A,B" desc:"Tab filter"`
}

// dryRunDomains is this file's own topology rather than the shared fixture's: the
// behaviour under test is about HAVING a body versus not, so the two operations
// are declared here to differ in exactly that and nothing else. Borrowing a real
// operation would tie these assertions to whichever product happens to declare a
// body today.
func dryRunDomains() []api.DomainSpec {
	return []api.DomainSpec{{
		Name: "probe", Short: "Body-validation probe",
		Subs: []api.Subdomain{{
			Name: "body", Short: "One op with a body, one without", HostKey: testHostKey,
			// The registry gate requires a concrete signing and identity default;
			// neither axis is what these tests are about, so both are none.
			Signed: api.SignNone, Identity: api.IdentityNone,
			Ops: []api.Operation{
				{
					Name: "post", Method: "POST", Path: "/probe/post",
					Access: api.Read, Summary: "Takes a JSON body", Body: dryRunListReq{},
				},
				{
					Name: "get", Method: "GET", Path: "/probe/get",
					Access: api.Read, Summary: "Takes no body",
				},
			},
		}},
	}}
}

// runProbe drives the command tree built from dryRunDomains, mirroring run().
func runProbe(args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", dryRunDomains(), nil, nil, true,
		func(string) string { return "" }, &out, &errBuf)
	code = app.Run(args)
	return out.String(), errBuf.String(), code
}

// planShape is the projection of a dry-run plan these tests assert on.
// BodyChecked is a pointer so "absent" is distinguishable from "false": an
// operation with no body must say nothing rather than report a meaningless false.
type planShape struct {
	Method      string `json:"method"`
	URL         string `json:"url"`
	BodyKind    string `json:"body_kind"`
	BodyChecked *bool  `json:"body_checked"`
}

func decodePlan(t *testing.T, stdout string) planShape {
	t.Helper()
	var p planShape
	if err := json.Unmarshal([]byte(stdout), &p); err != nil {
		t.Fatalf("plan is not JSON: %v\n%s", err, stdout)
	}
	return p
}

// A payload the caller supplied is checked, even though nothing will be sent.
// Every case here is decidable locally: none of them needs the network, a
// credential, or the backend's opinion.
func TestDryRun_ValidatesASuppliedBody(t *testing.T) {
	cases := []struct {
		name    string
		data    string
		wantSub string
		wantIn  string
	}{
		{"malformed json", `not json at all`, "invalid_argument", "not a valid request body"},
		{"required field missing", `{}`, "missing_argument", `"limit"`},
		{"unknown field", `{"limit":5,"nope":1}`, "invalid_argument", "unknown field"},
		{"enum violation", `{"limit":5,"tab":"NOPE"}`, "invalid_argument", "tab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runProbe("probe", "body", "post", "--data", tc.data, "--dry-run")
			if code == 0 {
				t.Fatalf("dry-run accepted a body a real call rejects; plan=%s", stdout)
			}
			var e errEnvelope
			if err := json.Unmarshal([]byte(stderr), &e); err != nil {
				t.Fatalf("stderr is not an error envelope: %v\n%s", err, stderr)
			}
			if e.Error.Subtype != tc.wantSub {
				t.Errorf("subtype = %q, want %q (stderr=%s)", e.Error.Subtype, tc.wantSub, stderr)
			}
			if e.Error.Param != "--data" {
				t.Errorf("param = %q, want --data", e.Error.Param)
			}
			if !strings.Contains(e.Error.Message, tc.wantIn) {
				t.Errorf("message = %q, want it to mention %q", e.Error.Message, tc.wantIn)
			}
		})
	}
}

// No --data is not an error. This is the discovery use: ask where an operation
// would go before building anything for it. The plan says body_checked=false so
// the caller is not left to infer that this preview skipped its payload.
func TestDryRun_DoesNotDemandABodyItWasNotGiven(t *testing.T) {
	stdout, stderr, code := runProbe("probe", "body", "post", "--dry-run")
	if code != 0 {
		t.Fatalf("dry-run without --data must still preview: code=%d stderr=%s", code, stderr)
	}
	p := decodePlan(t, stdout)
	if p.Method != "POST" || p.BodyKind != "json" {
		t.Errorf("plan = %+v, want the request structure", p)
	}
	if p.BodyChecked == nil {
		t.Fatalf("a body operation must report its body coverage; plan=%s", stdout)
	}
	if *p.BodyChecked {
		t.Errorf("body_checked = true with no --data supplied; plan=%s", stdout)
	}
}

// A valid payload previews AND says it was checked. Without the flag a caller
// cannot tell this plan apart from the one above, and the two mean very
// different things about what a real call will do.
func TestDryRun_ReportsThatItCheckedASuppliedBody(t *testing.T) {
	stdout, stderr, code := runProbe("probe", "body", "post", "--data", `{"limit":5}`, "--dry-run")
	if code != 0 {
		t.Fatalf("a valid body must preview: code=%d stderr=%s", code, stderr)
	}
	p := decodePlan(t, stdout)
	if p.BodyChecked == nil || !*p.BodyChecked {
		t.Errorf("body_checked should be true for a validated payload; plan=%s", stdout)
	}
}

// An operation with no body is asked no question about one. Reporting false here
// would read as "your payload went unchecked" for a request that has no payload.
func TestDryRun_SaysNothingAboutTheBodyOfAnOpWithoutOne(t *testing.T) {
	stdout, stderr, code := runProbe("probe", "body", "get", "--dry-run")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if p := decodePlan(t, stdout); p.BodyChecked != nil {
		t.Errorf("body_checked = %v on an operation with no body; plan=%s", *p.BodyChecked, stdout)
	}
	if strings.Contains(stdout, "body_checked") {
		t.Errorf("the field must be absent, not null; plan=%s", stdout)
	}
}

// The real path is untouched: a missing body is still a missing body, and a bad
// one still fails before anything is sent. Dry-run got stricter; nothing else
// moved.
func TestRealCall_StillRequiresAndValidatesTheBody(t *testing.T) {
	for _, tc := range []struct{ name, data, wantIn string }{
		{"no --data", "", "required body --data is missing"},
		{"required field missing", `{}`, `"limit"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"probe", "body", "post"}
			if tc.data != "" {
				args = append(args, "--data", tc.data)
			}
			_, stderr, code := runProbe(args...)
			if code == 0 {
				t.Fatal("a real call must still reject this body")
			}
			var e errEnvelope
			if err := json.Unmarshal([]byte(stderr), &e); err != nil {
				t.Fatalf("stderr is not an error envelope: %v\n%s", err, stderr)
			}
			if !strings.Contains(e.Error.Message, tc.wantIn) {
				t.Errorf("message = %q, want it to mention %q", e.Error.Message, tc.wantIn)
			}
		})
	}
}
