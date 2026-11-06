package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

func multipartOp(fields any) Operation {
	return Operation{
		Name: "upload", Method: "POST", Path: "/upload",
		Access: Write, Summary: "upload",
		Multipart: &MultipartDecl{
			Files:  []MultipartFile{{Field: "file", Required: true}, {Field: "thumb"}},
			Fields: fields,
		},
	}
}

func TestValidateMultipart_RequiredFilePresent(t *testing.T) {
	op := multipartOp(nil)
	// The required "file" field is provided; the optional "thumb" is not.
	if err := op.ValidateMultipart(map[string]bool{"file": true}, ""); err != nil {
		t.Fatalf("a provided required file must pass: %v", err)
	}
}

func TestValidateMultipart_MissingRequiredFile(t *testing.T) {
	op := multipartOp(nil)
	err := op.ValidateMultipart(map[string]bool{"thumb": true}, "")
	if err == nil || !strings.Contains(err.Error(), `required file field "file"`) {
		t.Fatalf("missing required file must fail, got: %v", err)
	}
}

func TestValidateMultipart_UnknownFileField(t *testing.T) {
	op := multipartOp(nil)
	err := op.ValidateMultipart(map[string]bool{"file": true, "bogus": true}, "")
	if err == nil || !strings.Contains(err.Error(), `unknown file field "bogus"`) {
		t.Fatalf("unknown file field must fail, got: %v", err)
	}
}

func TestValidateMultipart_RequiredTextField(t *testing.T) {
	type meta struct {
		Kind string `json:"kind" validate:"required"`
	}
	op := multipartOp(meta{})
	// Files fine, but the required text field "kind" is absent from empty --data.
	err := op.ValidateMultipart(map[string]bool{"file": true}, "")
	if err == nil || !strings.Contains(err.Error(), `"kind"`) {
		t.Fatalf("missing required text field must fail, got: %v", err)
	}
	// Supplying it passes.
	if err := op.ValidateMultipart(map[string]bool{"file": true}, `{"kind":"avatar"}`); err != nil {
		t.Fatalf("supplying the required text field must pass: %v", err)
	}
}

func TestValidateMultipart_UnknownTextField(t *testing.T) {
	type meta struct {
		Kind string `json:"kind"`
	}
	op := multipartOp(meta{})
	err := op.ValidateMultipart(map[string]bool{"file": true}, `{"nope":"x"}`)
	if err == nil {
		t.Fatal("an undeclared text field must be rejected")
	}
}

// bodyOp wraps a Body prototype in a minimal POST operation.
func bodyOp(body any) Operation {
	return Operation{
		Name: "save", Method: "POST", Path: "/save",
		Access: Write, Summary: "save",
		Body: body,
	}
}

// TestValidateBody_IntegerEnumAcceptsNumber is the regression for the
// integer-enum deadlock: an integer field tagged enum:"1,2" must accept the
// JSON number 1. The strict decode requires a number (the Go field is int) and
// the enum check must interpret its tokens as integers, not demand a string.
func TestValidateBody_IntegerEnumAcceptsNumber(t *testing.T) {
	type req struct {
		ChannelModel int `json:"channelModel" validate:"required" enum:"1,2"`
	}
	if err := bodyOp(req{}).ValidateBody(`{"channelModel":1}`); err != nil {
		t.Fatalf("integer enum must accept a matching JSON number: %v", err)
	}
}

func TestValidateBody_IntegerEnumRejectsOutOfSet(t *testing.T) {
	type req struct {
		ChannelModel int `json:"channelModel" validate:"required" enum:"1,2"`
	}
	err := bodyOp(req{}).ValidateBody(`{"channelModel":3}`)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("an out-of-set integer must be rejected, got: %v", err)
	}
}

func TestValidateBody_NumberEnum(t *testing.T) {
	type req struct {
		Rate float64 `json:"rate" enum:"0.5,1.5"`
	}
	if err := bodyOp(req{}).ValidateBody(`{"rate":1.5}`); err != nil {
		t.Fatalf("number enum must accept a matching float: %v", err)
	}
	if err := bodyOp(req{}).ValidateBody(`{"rate":2.5}`); err == nil {
		t.Fatal("an out-of-set float must be rejected")
	}
}

func TestValidateBody_BooleanEnum(t *testing.T) {
	type req struct {
		On bool `json:"on" enum:"true"`
	}
	if err := bodyOp(req{}).ValidateBody(`{"on":true}`); err != nil {
		t.Fatalf("boolean enum must accept a matching bool: %v", err)
	}
	if err := bodyOp(req{}).ValidateBody(`{"on":false}`); err == nil {
		t.Fatal("a bool outside the enum set must be rejected")
	}
}

// TestValidateBody_StringEnumStillWorks guards the unchanged string path.
func TestValidateBody_StringEnumStillWorks(t *testing.T) {
	type req struct {
		Level string `json:"level" enum:"c,d"`
	}
	if err := bodyOp(req{}).ValidateBody(`{"level":"d"}`); err != nil {
		t.Fatalf("string enum must accept a member: %v", err)
	}
	if err := bodyOp(req{}).ValidateBody(`{"level":"z"}`); err == nil {
		t.Fatal("a non-member string must be rejected")
	}
}

// TestValidateBody_IntegerEnumLargeValue guards against float64 precision loss:
// an int64 beyond 2^53 must still compare exactly, which requires decoding the
// value as json.Number rather than float64.
func TestValidateBody_IntegerEnumLargeValue(t *testing.T) {
	type req struct {
		ID int64 `json:"id" enum:"9007199254740993"`
	}
	if err := bodyOp(req{}).ValidateBody(`{"id":9007199254740993}`); err != nil {
		t.Fatalf("large int64 enum must compare without precision loss: %v", err)
	}
}

func TestValidate_ParamConstraints(t *testing.T) {
	op := Operation{Params: []Param{
		{Name: "name", MinLen: 2, MaxLen: 5},
		{Name: "code", Pattern: "[A-Z]+"},
		{Name: "qty", MinVal: Ptr(1.0), MaxVal: Ptr(10.0)},
	}}

	ok := func(vals map[string]string) {
		t.Helper()
		if err := op.Validate(vals); err != nil {
			t.Errorf("%v should pass, got %v", vals, err)
		}
	}
	bad := func(vals map[string]string, wantParam string) {
		t.Helper()
		err := op.Validate(vals)
		if err == nil {
			t.Fatalf("%v should fail", vals)
		}
		var e *errs.Error
		if !asError(err, &e) || e.Param != wantParam {
			t.Errorf("%v: want param %q, got %v", vals, wantParam, err)
		}
	}

	// length
	ok(map[string]string{"name": "abc"})
	bad(map[string]string{"name": "a"}, "--name")      // too short
	bad(map[string]string{"name": "abcdef"}, "--name") // too long
	// pattern (full match — "AB1" must fail, "AB" pass)
	ok(map[string]string{"code": "AB"})
	bad(map[string]string{"code": "AB1"}, "--code")
	bad(map[string]string{"code": "ab"}, "--code")
	// numeric bounds
	ok(map[string]string{"qty": "5"})
	ok(map[string]string{"qty": "1"})  // inclusive
	ok(map[string]string{"qty": "10"}) // inclusive
	bad(map[string]string{"qty": "0"}, "--qty")
	bad(map[string]string{"qty": "11"}, "--qty")
	bad(map[string]string{"qty": "abc"}, "--qty") // not a number
	// absent optional param → constraints do not apply
	ok(map[string]string{})
}

type constraintBody struct {
	Amount float64 `json:"amount" minval:"0" maxval:"1000"`
	Name   string  `json:"name" minlen:"2" maxlen:"5"`
	Slug   string  `json:"slug" pattern:"[a-z0-9-]+"`
}

func TestValidateBody_Constraints(t *testing.T) {
	op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: constraintBody{}}

	ok := func(data string) {
		t.Helper()
		if err := op.ValidateBody(data); err != nil {
			t.Errorf("%s should pass, got %v", data, err)
		}
	}
	bad := func(data string) {
		t.Helper()
		if err := op.ValidateBody(data); err == nil {
			t.Errorf("%s should fail", data)
		}
	}

	ok(`{"amount":500,"name":"abc","slug":"ai-tech-1"}`)
	ok(`{"amount":0}`)    // inclusive lower
	ok(`{"amount":1000}`) // inclusive upper
	bad(`{"amount":-1}`)
	bad(`{"amount":1001}`)
	bad(`{"name":"a"}`)       // too short
	bad(`{"name":"abcdef"}`)  // too long
	bad(`{"slug":"AI_TECH"}`) // pattern (uppercase + underscore not allowed)
}

// TestValidateBody_IntegerBoundExactBeyondFloat53 pins the int64-exact integer
// bound check: 2^53+1 exceeds a maxval of 2^53, but float64 rounds it down to
// 2^53 and would wrongly pass — numBoundViolation compares in int64 space.
func TestValidateBody_IntegerBoundExactBeyondFloat53(t *testing.T) {
	type req struct {
		N int64 `json:"n" maxval:"9007199254740992"` // 2^53
	}
	op := Operation{Name: "x", Method: "POST", Access: Write, Body: req{}}
	if err := op.ValidateBody(`{"n":9007199254740992}`); err != nil { // exactly the bound: inclusive, passes
		t.Errorf("2^53 should pass the inclusive maxval, got %v", err)
	}
	if err := op.ValidateBody(`{"n":9007199254740993}`); err == nil { // 2^53+1: over the bound
		t.Fatalf("2^53+1 should fail maxval 2^53 (float64 would round it and wrongly pass)")
	}
}

// TestBoundViolation_PlainDecimal pins the C fix: a round bound like 1000000 is
// rendered "1000000", never %v's "1e+06".
func TestBoundViolation_PlainDecimal(t *testing.T) {
	msg := boundViolation(2000000, nil, Ptr(1000000.0))
	if want := "must be <= 1000000 (got 2000000)"; msg != want {
		t.Errorf("boundViolation = %q, want %q", msg, want)
	}
}

// A body the shell chewed and a body the caller got wrong are the same failure
// to the JSON decoder and need OPPOSITE advice. Routing them to the same hint is
// what sends an agent to re-inspect a payload that was already correct, and from
// there to guessing at quotes — one more attempt at a command that writes.
func TestValidateBody_HintRoutesShellDamageAwayFromTheSchema(t *testing.T) {
	type req struct {
		Symbol string `json:"symbol" validate:"required"`
	}
	op := bodyOp(req{})

	// The hint is a structured field, not part of Error(): it is what the agent
	// reads to decide what to do next, so assert on it directly.
	hintOf := func(t *testing.T, err error) string {
		t.Helper()
		if err == nil {
			t.Fatal("expected a rejection")
		}
		var e *errs.Error
		if !errors.As(err, &e) {
			t.Fatalf("expected a structured error, got %T: %v", err, err)
		}
		return e.Hint
	}

	// What PowerShell 5.1 and cmd.exe actually deliver for --data '{"symbol":"X"}'.
	hint := hintOf(t, op.ValidateBody(`{symbol:BTC_USDT}`))
	for _, want := range []string{"no double quotes", "@<path>", "Do not retry with different quoting"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("shell-damage hint must mention %q, got: %s", want, hint)
		}
	}
	if strings.Contains(hint, "check the JSON against") {
		t.Fatalf("shell damage must NOT be routed to the schema hint: %s", hint)
	}

	// A payload that is genuinely wrong, with its quotes intact, keeps the
	// schema hint — the shell advice would be a wrong lead there.
	if hint := hintOf(t, op.ValidateBody(`{"symbol":}`)); !strings.Contains(hint, "check the JSON against") {
		t.Fatalf("a genuinely bad payload must keep the schema hint, got: %s", hint)
	}

	// A quote-free body that PARSES never reaches the hint at all: the scalar
	// array is the shape that survives cmd.exe, and it must stay silent.
	if err := bodyOp([]int64{}).ValidateBody(`[123456]`); err != nil {
		t.Fatalf("a scalar array body must remain valid: %v", err)
	}
}
