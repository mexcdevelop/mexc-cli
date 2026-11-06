package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

func TestIsWrite(t *testing.T) {
	if (Operation{Access: Read}).IsWrite() {
		t.Fatal("read op should not be write")
	}
	if !(Operation{Access: Write}).IsWrite() {
		t.Fatal("write op should be write")
	}
}

func TestDestructiveAndIdempotentHints_DefaultsFromAccess(t *testing.T) {
	read := Operation{Access: Read}
	if read.DestructiveHint() {
		t.Error("a read must default to non-destructive")
	}
	if !read.IdempotentHint() {
		t.Error("a read must default to idempotent")
	}

	write := Operation{Access: Write}
	if !write.DestructiveHint() {
		t.Error("a write must default to destructive")
	}
	if write.IdempotentHint() {
		t.Error("a write must default to non-idempotent")
	}
}

func TestDestructiveAndIdempotentHints_Overrides(t *testing.T) {
	no, yes := false, true

	// place-order: a write that only adds state — non-destructive.
	place := Operation{Access: Write, Destructive: &no}
	if place.DestructiveHint() {
		t.Error("Destructive=false must override the write default")
	}
	if place.IdempotentHint() {
		t.Error("place-order is still not idempotent by default")
	}

	// cancel/amend/set: a write that converges to a fixed state — idempotent.
	cancel := Operation{Access: Write, Idempotent: &yes}
	if !cancel.IdempotentHint() {
		t.Error("Idempotent=true must override the write default")
	}
	if !cancel.DestructiveHint() {
		t.Error("cancel still destroys/overwrites state by default")
	}
}

func TestRiskTier(t *testing.T) {
	no, yes := false, true

	cases := []struct {
		name string
		op   Operation
		want RiskTier
	}{
		// Reads never confirm.
		{"read", Operation{Access: Read}, TierNone},

		// place-order: a write that only adds state (Destructive=false) → Light.
		{"place", Operation{Access: Write, Destructive: &no}, TierLight},
		// cancel: a destructive write that is safe to repeat (Idempotent=true) → Light.
		{"cancel", Operation{Access: Write, Idempotent: &yes}, TierLight},
		// cancel-all mirrors cancel: destructive default + Idempotent=true → Light.
		{"cancel-all", Operation{Access: Write, Idempotent: &yes}, TierLight},

		// withdrawal-shaped: destructive AND non-idempotent → Heavy.
		{"withdraw", Operation{Access: Write, Destructive: &yes, Idempotent: &no}, TierHeavy},
		// An unlabeled write defaults destructive=true / idempotent=false → Heavy
		// (fail-safe: forgetting to grade lands on the stricter confirmation).
		{"unlabeled-write", Operation{Access: Write}, TierHeavy},
		// A destructive-only write (no idempotent override) is still Heavy.
		{"destructive-only", Operation{Access: Write, Destructive: &yes}, TierHeavy},
	}
	for _, c := range cases {
		if got := c.op.RiskTier(); got != c.want {
			t.Errorf("%s: RiskTier() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	op := Operation{Params: []Param{
		{Name: "symbol", Required: true},
		{Name: "interval", Enum: []string{"1m", "1d"}},
	}}

	tests := []struct {
		name     string
		values   map[string]string
		wantErr  bool
		wantParm string
	}{
		{"all good", map[string]string{"symbol": "BTCUSDT", "interval": "1m"}, false, ""},
		{"missing required", map[string]string{"interval": "1m"}, true, "--symbol"},
		{"bad enum", map[string]string{"symbol": "BTCUSDT", "interval": "1h"}, true, "--interval"},
		{"empty optional enum ok", map[string]string{"symbol": "BTCUSDT"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := op.Validate(tt.values)
			if tt.wantErr != (err != nil) {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr {
				var e *errs.Error
				if !asError(err, &e) || e.Param != tt.wantParm {
					t.Fatalf("expected param %q, got %v", tt.wantParm, err)
				}
				if e.Type != errs.TypeValidation {
					t.Fatalf("expected validation type, got %s", e.Type)
				}
			}
		})
	}
}

func asError(err error, target **errs.Error) bool {
	return errors.As(err, target)
}

func TestSchema(t *testing.T) {
	op := Operation{
		Name: "place", Method: "POST", Path: "/api/v3/order",
		Access: Write, Summary: "place an order",
		Params: []Param{
			{Name: "symbol", Desc: "trading pair", Required: true},
			{Name: "side", Enum: []string{"BUY", "SELL"}},
		},
		Transform: func(raw any) (any, error) { return raw, nil }, // must be omitted from schema
	}
	s := op.Schema("spot.trade.place", testSchemeHMAC, IdentityNone)

	if s.Operation != "spot.trade.place" || s.Name != "place" {
		t.Fatalf("wrong identity: %+v", s)
	}
	if s.Method != "POST" || s.Path != "/api/v3/order" {
		t.Fatalf("wrong method/path: %+v", s)
	}
	if s.Access != Write || !s.Write {
		t.Fatalf("write should be derived from access: access=%q write=%v", s.Access, s.Write)
	}
	if !s.Signed {
		t.Fatalf("effective signed flag not carried: %+v", s)
	}
	if s.Identity != IdentityNone {
		t.Fatalf("effective identity not carried: %+v", s)
	}
	if len(s.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(s.Params))
	}
	if s.Params[0].Name != "symbol" || !s.Params[0].Required {
		t.Fatalf("symbol param wrong: %+v", s.Params[0])
	}
	if s.Params[0].In != "query" {
		t.Fatalf("zero-value In must be reported as \"query\", got %q", s.Params[0].In)
	}
	if len(s.Params[1].Enum) != 2 || s.Params[1].Enum[0] != "BUY" {
		t.Fatalf("enum not carried: %+v", s.Params[1])
	}
}

func TestSchema_ReadOpNotWrite(t *testing.T) {
	s := Operation{Name: "balances", Access: Read}.Schema("sample.signed.balances", testSchemeHMAC, IdentityNone)
	if s.Write {
		t.Fatal("read op must report write=false")
	}
}

func TestSchema_PermissionCarried(t *testing.T) {
	op := Operation{
		Name: "freeze", Method: "GET", Path: "/x", Access: Read,
		Permission: &PermReq{System: "demo-system", Codes: []string{"demo:freeze"}},
	}
	s := op.Schema("demo.wallet.freeze", SignNone, testIdentityLogin)
	if s.Permission == nil || s.Permission.System != "demo-system" || len(s.Permission.Codes) != 1 {
		t.Fatalf("permission not carried: %+v", s.Permission)
	}
}

type approveRisk struct {
	Level string   `json:"level" validate:"required" enum:"low,high"`
	Tags  []string `json:"tags"`
}

type approveBody struct {
	OrderID string      `json:"orderId" validate:"required" desc:"Demo order id"`
	Verdict string      `json:"verdict" validate:"required" enum:"approve,reject"`
	Reason  string      `json:"reason,omitempty"`
	Risk    approveRisk `json:"risk"`
	// Never read, deliberately: this field exists so the reflection walk can be
	// shown to skip unexported fields. Deleting it to satisfy the linter would
	// delete the assertion.
	secret string //nolint:unused // the point of the field is that nothing reads it
}

func bodyField(fields []BodyField, name string) (BodyField, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return BodyField{}, false
}

func TestSchema_ReflectsBody(t *testing.T) {
	op := Operation{
		Name: "approve", Method: "POST", Path: "/demo/v1/approve",
		Access: Write, Summary: "approve", Body: approveBody{},
	}
	s := op.Schema("demo.wallet.approve", SignNone, testIdentityLogin)
	if s.Body == nil {
		t.Fatal("schema must carry a body section when Body is set")
	}
	if s.Body.Flag != "--data" || s.Body.ContentType != "application/json" {
		t.Fatalf("body carrier wrong: %+v", s.Body)
	}
	if len(s.Body.Fields) != 4 {
		t.Fatalf("expected 4 exported body fields (secret ignored), got %d: %+v", len(s.Body.Fields), s.Body.Fields)
	}
	if _, ok := bodyField(s.Body.Fields, "secret"); ok {
		t.Fatal("unexported field must not be reflected into the schema")
	}
	orderID, _ := bodyField(s.Body.Fields, "orderId")
	if !orderID.Required || orderID.Desc != "Demo order id" || orderID.Type != "string" {
		t.Fatalf("orderId field wrong: %+v", orderID)
	}
	verdict, _ := bodyField(s.Body.Fields, "verdict")
	if len(verdict.Enum) != 2 || verdict.Enum[0] != "approve" {
		t.Fatalf("verdict enum not carried: %+v", verdict)
	}
	reason, _ := bodyField(s.Body.Fields, "reason")
	if reason.Required {
		t.Fatalf("reason without validate:required must be optional: %+v", reason)
	}
	risk, _ := bodyField(s.Body.Fields, "risk")
	if risk.Type != "object" || len(risk.Fields) != 2 {
		t.Fatalf("nested risk object not reflected: %+v", risk)
	}
	level, _ := bodyField(risk.Fields, "level")
	if !level.Required || len(level.Enum) != 2 {
		t.Fatalf("nested field tags not reflected: %+v", level)
	}
	tags, _ := bodyField(risk.Fields, "tags")
	if tags.Type != "array" {
		t.Fatalf("slice field must report array: %+v", tags)
	}
}

func TestSchema_NoBodyWhenUnset(t *testing.T) {
	s := Operation{Name: "get", Method: "GET", Path: "/x", Access: Read}.Schema("d.s.get", SignNone, IdentityNone)
	if s.Body != nil {
		t.Fatalf("op without Body must not carry a body schema: %+v", s.Body)
	}
}

func TestValidateBody(t *testing.T) {
	op := Operation{Name: "approve", Method: "POST", Path: "/x", Access: Write, Body: approveBody{}}

	// Valid body (nested object included).
	ok := `{"orderId":"1","verdict":"approve","risk":{"level":"low","tags":["kyc"]}}`
	if err := op.ValidateBody(ok); err != nil {
		t.Fatalf("valid body rejected: %v", err)
	}

	// Missing required top-level field.
	if err := op.ValidateBody(`{"verdict":"approve","risk":{"level":"low"}}`); err == nil {
		t.Fatal("missing required orderId must be rejected")
	}

	// Enum violation on a top-level field.
	if err := op.ValidateBody(`{"orderId":"1","verdict":"maybe","risk":{"level":"low"}}`); err == nil {
		t.Fatal("out-of-enum verdict must be rejected")
	}

	// Enum violation inside the nested object.
	if err := op.ValidateBody(`{"orderId":"1","verdict":"approve","risk":{"level":"medium"}}`); err == nil {
		t.Fatal("out-of-enum nested level must be rejected")
	}

	// Unknown field.
	if err := op.ValidateBody(`{"orderId":"1","verdict":"approve","bogus":true}`); err == nil {
		t.Fatal("unknown field must be rejected")
	}

	// Malformed JSON.
	if err := op.ValidateBody(`{not json`); err == nil {
		t.Fatal("malformed JSON must be rejected")
	}

	// Empty body.
	if err := op.ValidateBody(""); err == nil {
		t.Fatal("empty --data must be rejected for a Body op")
	}

	// An op without a Body accepts no body: ValidateBody is a no-op.
	if err := (Operation{Name: "x"}).ValidateBody(""); err != nil {
		t.Fatalf("no-Body op must no-op: %v", err)
	}
}

func TestMaskSensitiveBody(t *testing.T) {
	type nested struct {
		Key string `json:"key" sensitive:"true"`
	}
	type reqBody struct {
		AppID string `json:"appId"`
		Token string `json:"token" sensitive:"true"`
		Meta  nested `json:"meta"`
	}
	op := Operation{Access: Write, Body: reqBody{}}

	got := op.MaskSensitiveBody(`{"appId":"AI-TECH","token":"secret-123","meta":{"key":"k-999"}}`)
	if strings.Contains(got, "secret-123") || strings.Contains(got, "k-999") {
		t.Fatalf("sensitive values must be masked:\n%s", got)
	}
	if !strings.Contains(got, "AI-TECH") {
		t.Errorf("non-sensitive value must remain: %s", got)
	}
	if strings.Count(got, SensitiveMask) != 2 {
		t.Errorf("both the top-level token and nested key must be masked: %s", got)
	}

	// No sensitive field → original text preserved unchanged.
	type plain struct {
		AppID string `json:"appId"`
	}
	raw := `{"appId":"x"}`
	if op2 := (Operation{Access: Write, Body: plain{}}); op2.MaskSensitiveBody(raw) != raw {
		t.Errorf("a body with no sensitive field must be unchanged, got %q", op2.MaskSensitiveBody(raw))
	}

	// No body / malformed data → returned as-is (never a partial result).
	if s := (Operation{}).MaskSensitiveBody(`{"a":1}`); s != `{"a":1}` {
		t.Errorf("no-body op must return data unchanged, got %q", s)
	}
	if s := op.MaskSensitiveBody(`not json`); s != `not json` {
		t.Errorf("malformed data must return unchanged, got %q", s)
	}
}
