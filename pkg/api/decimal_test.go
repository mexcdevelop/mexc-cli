package api

import (
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

type decimalBody struct {
	Price  string `json:"price" validate:"required" decimal:"positive" desc:"Order price"`
	Amount string `json:"amount,omitempty" decimal:"positive" desc:"Quote amount"`
}

func decimalOp() Operation {
	return Operation{Name: "place", Method: "POST", Path: "/x/place", Access: Write, Summary: "s", Body: decimalBody{}}
}

func TestIsPositiveDecimal(t *testing.T) {
	for _, s := range []string{"1", "10", "0.5", "0.0001", "1.0", "01.50", "1000000000000000000000000000000000000.1"} {
		if !isPositiveDecimal(s) {
			t.Errorf("%q must be accepted", s)
		}
	}
	for _, s := range []string{"", "0", "00", "0.0", "000.000", "-1", "+1", "1e3", "1E3", ".5", "5.", "1.2.3", " 1", "1 ", "1,000", "abc", "0x1", "\uff11"} {
		if isPositiveDecimal(s) {
			t.Errorf("%q must be refused", s)
		}
	}
}

// The gap this tag closes: quantity "-1" used to pass every body check, so a
// dry-run said body_checked=true for a call the exchange can only refuse. A JSON
// null is the second half — it walked past every string constraint.
func TestValidateBody_DecimalPositive(t *testing.T) {
	op := decimalOp()
	for _, body := range []string{
		`{"price":"-1"}`, `{"price":"0"}`, `{"price":""}`, `{"price":null}`, `{"price":"1e3"}`,
		`{"price":"1","amount":"0"}`, `{"price":"1","amount":null}`,
	} {
		err := op.ValidateBody(body)
		if err == nil {
			t.Errorf("%s must be refused", body)
			continue
		}
		if got := subtypeOf(t, err); got != errs.SubtypeInvalidArgument {
			t.Errorf("%s: want invalid_argument, got %s", body, got)
		}
		if !strings.Contains(err.Error(), "positive decimal") {
			t.Errorf("%s: the refusal must say what is expected: %v", body, err)
		}
	}
	for _, body := range []string{`{"price":"0.001"}`, `{"price":"60000","amount":"100"}`} {
		if err := op.ValidateBody(body); err != nil {
			t.Errorf("%s must pass, got %v", body, err)
		}
	}
	// An absent optional amount is not a value, so there is nothing to check.
	if err := op.ValidateBody(`{"price":"1"}`); err != nil {
		t.Errorf("an absent optional amount must pass, got %v", err)
	}
}

func TestValidateDecl_DecimalTag(t *testing.T) {
	type onNumber struct {
		N int64 `json:"n" decimal:"positive"`
	}
	type badValue struct {
		S string `json:"s" decimal:"nonnegative"`
	}
	for name, body := range map[string]any{"on a number": onNumber{}, "unknown value": badValue{}} {
		op := Operation{Name: "op", Method: "POST", Path: "/x", Access: Write, Summary: "s", Body: body}
		if err := op.ValidateDecl(); err == nil {
			t.Errorf("decimal %s must be refused at load time", name)
		}
	}
	if err := decimalOp().ValidateDecl(); err != nil {
		t.Fatalf("decimal on a string must pass: %v", err)
	}
}

func TestReflectBodyFields_Decimal(t *testing.T) {
	for _, f := range decimalOp().BodyFields() {
		if f.Decimal != DecimalPositive {
			t.Errorf("%s: schema must carry decimal=%q, got %q", f.Name, DecimalPositive, f.Decimal)
		}
	}
}
