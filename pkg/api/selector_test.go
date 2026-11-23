package api

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// selectorOp is an operation shaped like spot.trade.cancel: three selectors, a
// filter, and a switch that names the widest mode.
func selectorOp() Operation {
	return Operation{
		Name: "cancel", Method: "DELETE", Path: "/x/cancel", Access: Write,
		Summary: "Cancel everything (--all), one pair (symbol) or one order (orderId)",
		Params: []Param{
			{Name: "orderId"},
			{Name: "symbol"},
			{Name: "side"},
			{Name: "all", Switch: true, Desc: "every open order"},
		},
		RequireOneOf: []string{"orderId", "symbol", "all"},
		Exclusive:    [][]string{{"all", "orderId"}, {"all", "symbol"}},
	}
}

func subtypeOf(t *testing.T, err error) string {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("want a structured error, got %v", err)
	}
	return e.Subtype
}

func TestValidateDecl_SwitchAndGroupsAccepted(t *testing.T) {
	if err := selectorOp().ValidateDecl(); err != nil {
		t.Fatalf("a well-formed switch + groups declaration must pass: %v", err)
	}
}

// A switch is only a switch: every other attribute would be a promise nothing
// keeps, so each one is refused at load time.
func TestValidateDecl_SwitchRefusesOtherAttributes(t *testing.T) {
	maxLen := Param{Name: "all", Switch: true, MaxLen: 3}
	minVal := Param{Name: "all", Switch: true, MinVal: Ptr(1.0)}
	for name, p := range map[string]Param{
		"required":  {Name: "all", Switch: true, Required: true},
		"enum":      {Name: "all", Switch: true, Enum: []string{"true"}},
		"in":        {Name: "all", Switch: true, In: InHeader},
		"sensitive": {Name: "all", Switch: true, Sensitive: true},
		"pattern":   {Name: "all", Switch: true, Pattern: "x"},
		"maxlen":    maxLen,
		"minval":    minVal,
	} {
		op := Operation{Name: "op", Method: "GET", Path: "/x", Access: Read, Summary: "s", Params: []Param{p}}
		if err := op.ValidateDecl(); err == nil {
			t.Errorf("%s: a switch carrying it must be refused", name)
		}
	}
}

func TestValidateDecl_GroupsMustBeSound(t *testing.T) {
	base := selectorOp()
	cases := map[string]func(o *Operation){
		"undeclared member":       func(o *Operation) { o.RequireOneOf = []string{"orderId", "nope"} },
		"single member":           func(o *Operation) { o.RequireOneOf = []string{"orderId"} },
		"duplicate member":        func(o *Operation) { o.RequireOneOf = []string{"orderId", "orderId"} },
		"required member":         func(o *Operation) { o.Params[0].Required = true },
		"exclusive single member": func(o *Operation) { o.Exclusive = [][]string{{"all"}} },
		"exclusive undeclared":    func(o *Operation) { o.Exclusive = [][]string{{"all", "nope"}} },
	}
	for name, mutate := range cases {
		op := base
		op.Params = append([]Param(nil), base.Params...)
		mutate(&op)
		if err := op.ValidateDecl(); err == nil {
			t.Errorf("%s: must be refused at load time", name)
		}
	}
}

// The runtime rules, as both faces call them: no selector is a missing argument
// that names no single param and carries the Summary as its hint; a switch next
// to a selector it is paired with is refused; filters select nothing.
func TestValidate_SelectorRules(t *testing.T) {
	op := selectorOp()

	err := op.Validate(map[string]string{"side": "1"})
	if got := subtypeOf(t, err); got != errs.SubtypeMissingArgument {
		t.Fatalf("filters alone: want missing_argument, got %s", got)
	}
	var e *errs.Error
	errors.As(err, &e)
	if e.Param != "" {
		t.Errorf("no single input failed, so param must be empty, got %q", e.Param)
	}
	if e.Hint != op.Summary {
		t.Errorf("the hint must be the Summary that enumerates the modes, got %q", e.Hint)
	}
	for _, flag := range []string{"--orderId", "--symbol", "--all"} {
		if !strings.Contains(e.Message, flag) {
			t.Errorf("the refusal must name %s: %q", flag, e.Message)
		}
	}

	err = op.Validate(map[string]string{"all": "true", "symbol": "BTC_USDT"})
	if got := subtypeOf(t, err); got != errs.SubtypeInvalidArgument {
		t.Fatalf("switch with a paired selector: want invalid_argument, got %s", got)
	}
	errors.As(err, &e)
	if e.Param != "--all" {
		t.Errorf("the conflict is reported on the group's first member, got %q", e.Param)
	}

	for name, values := range map[string]map[string]string{
		"switch":            {"all": "true"},
		"switch and filter": {"all": "true", "side": "2"},
		"selector":          {"orderId": "1"},
		"unpaired together": {"orderId": "1", "symbol": "BTC_USDT"},
	} {
		if err := op.Validate(values); err != nil {
			t.Errorf("%s: must pass, got %v", name, err)
		}
	}
}

// A switch is collected as "true" or nothing; anything else is refused rather
// than read as either answer.
func TestValidate_SwitchValues(t *testing.T) {
	op := selectorOp()
	for _, bad := range []string{"false", "yes", "1"} {
		err := op.Validate(map[string]string{"all": bad})
		if got := subtypeOf(t, err); got != errs.SubtypeInvalidArgument {
			t.Errorf("all=%q: want invalid_argument, got %s", bad, got)
		}
	}
}

func TestBuildSpec_SwitchIsNeverSent(t *testing.T) {
	spec, err := selectorOp().BuildSpec(map[string]string{"all": "true", "side": "2"}, "", nil, SignNone, IdentityNone, "h")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Query["all"]; ok {
		t.Fatalf("a switch must never be sent, got %v", spec.Query)
	}
	if len(spec.Query) != 1 || spec.Query["side"] != "2" {
		t.Fatalf("only the filter travels, got %v", spec.Query)
	}
}

// The schema face reports a switch without a location and publishes both group
// rules, so an agent can read them before calling.
func TestSchema_SwitchAndGroups(t *testing.T) {
	s := selectorOp().Schema("x.y.cancel", SignNone, IdentityNone)
	raw, _ := json.Marshal(s)
	var view struct {
		Params []map[string]any `json:"params"`
		OneOf  []string         `json:"require_one_of"`
		Excl   [][]string       `json:"exclusive"`
	}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	for _, p := range view.Params {
		if p["name"] == "all" {
			if p["switch"] != true {
				t.Errorf("all must report switch=true: %v", p)
			}
			if _, ok := p["in"]; ok {
				t.Errorf("a switch has no location to report: %v", p)
			}
		} else if p["in"] != "query" {
			t.Errorf("an ordinary param keeps its canonical location: %v", p)
		}
	}
	if strings.Join(view.OneOf, ",") != "orderId,symbol,all" || len(view.Excl) != 2 {
		t.Errorf("schema must publish the groups, got one_of=%v exclusive=%v", view.OneOf, view.Excl)
	}
}
