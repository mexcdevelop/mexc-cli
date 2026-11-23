package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// selectorDomains mounts one subdomain shaped like spot.trade: a read, a
// cancel whose account-wide mode is a switch, and a write-only subdomain beside
// it, so the read-only help can be checked at both levels.
func selectorDomains() []api.DomainSpec {
	cancel := api.Operation{
		Name: "cancel", Method: "DELETE", Path: "/fixture/cancel", Access: api.Write,
		Idempotent: api.Ptr(true),
		Summary:    "Cancel everything (--all), one pair (symbol) or one order (orderId)",
		Params: []api.Param{
			{Name: "orderId", Desc: "One order"},
			{Name: "symbol", Desc: "One pair"},
			{Name: "side", Desc: "Filter"},
			{Name: "all", Switch: true, Desc: "Every open order"},
		},
		RequireOneOf: []string{"orderId", "symbol", "all"},
		Exclusive:    [][]string{{"all", "orderId"}, {"all", "symbol"}},
	}
	return []api.DomainSpec{{
		Name: "sample", Short: "Fixture domain",
		Subs: []api.Subdomain{
			{
				Name: "orders", Short: "Reads and writes",
				Signed: api.SignNone, Identity: api.IdentityNone, HostKey: "sample",
				Ops: []api.Operation{
					{Name: "list", Method: "GET", Path: "/fixture/orders", Access: api.Read, Summary: "List orders"},
					cancel,
				},
			},
			{
				Name: "joins", Short: "Writes only",
				Signed: api.SignNone, Identity: api.IdentityNone, HostKey: "sample",
				Ops: []api.Operation{
					{Name: "join", Method: "POST", Path: "/fixture/join", Access: api.Write, Summary: "Join an event"},
				},
			},
		},
	}}
}

// The bare --all reaches the account-wide request, which carries no `all`.
func TestCLI_SwitchIsBareAndNeverSent(t *testing.T) {
	out, stderr, code := runWith(selectorDomains(), nil,
		"sample", "orders", "cancel", "--all", "--side", "2", "--dry-run", "--base-url", "https://example.test")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var plan struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("stdout is not a plan: %v\n%s", err, out)
	}
	if strings.Contains(plan.URL, "all") || !strings.HasSuffix(plan.URL, "/fixture/cancel?side=2") {
		t.Fatalf("--all must not travel; want only side=2, got %s", plan.URL)
	}
}

// A call that names no mode is refused before anything is sent — on the CLI
// face, through the same Validate the MCP face calls.
func TestCLI_NoSelectorIsRefused(t *testing.T) {
	for _, args := range [][]string{
		{"sample", "orders", "cancel", "--dry-run"},
		{"sample", "orders", "cancel", "--side", "2", "--dry-run"},
	} {
		_, stderr, code := runWith(selectorDomains(), nil, args...)
		if code == 0 {
			t.Fatalf("%v: must be refused", args)
		}
		var env errEnvelope
		if err := json.Unmarshal([]byte(stderr), &env); err != nil {
			t.Fatalf("stderr is not an envelope: %v\n%s", err, stderr)
		}
		if env.Error.Subtype != "missing_argument" || !strings.Contains(env.Error.Message, "--all") {
			t.Fatalf("%v: want missing_argument naming --all, got %+v", args, env.Error)
		}
	}
	_, stderr, code := runWith(selectorDomains(), nil, "sample", "orders", "cancel", "--all", "--orderId", "1", "--dry-run")
	if code == 0 || !strings.Contains(stderr, "invalid_argument") {
		t.Fatalf("--all with --orderId must be refused, code=%d stderr=%s", code, stderr)
	}
}

// Under --read-only the help lists only what can run; without it, everything.
func TestHelp_ReadOnlyHidesWrites(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		args []string
	}{
		"flag":        {nil, []string{"sample", "orders", "--read-only", "--help"}},
		"env":         {map[string]string{"MEXC_READ_ONLY": "true"}, []string{"sample", "orders", "--help"}},
		"help topic":  {nil, []string{"help", "sample", "orders", "--read-only"}},
		"short flag":  {nil, []string{"sample", "orders", "--read-only", "-h"}},
		"domain list": {nil, []string{"sample", "--read-only", "--help"}},
	} {
		out, stderr, code := runWith(selectorDomains(), tc.env, tc.args...)
		if code != 0 {
			t.Fatalf("%s: code=%d stderr=%s", name, code, stderr)
		}
		if strings.Contains(out, "cancel ") || strings.Contains(out, "joins") {
			t.Errorf("%s: a write (and a write-only group) must not be listed:\n%s", name, out)
		}
		if name != "domain list" && !strings.Contains(out, "list ") {
			t.Errorf("%s: the read must stay listed:\n%s", name, out)
		}
		if name == "domain list" && !strings.Contains(out, "orders") {
			t.Errorf("%s: a group that still holds a read stays listed:\n%s", name, out)
		}
	}

	out, _, _ := runWith(selectorDomains(), nil, "sample", "orders", "--help")
	if !strings.Contains(out, "cancel ") || !strings.Contains(out, "list ") {
		t.Errorf("without read-only every command is listed:\n%s", out)
	}
	out, _, _ = runWith(selectorDomains(), nil, "sample", "--help")
	if !strings.Contains(out, "joins") {
		t.Errorf("without read-only a write-only group is listed:\n%s", out)
	}
}

// A write named directly still documents itself under read-only; running it is
// what the gate refuses.
func TestHelp_ReadOnlyStillDocumentsANamedWrite(t *testing.T) {
	out, _, code := runWith(selectorDomains(), nil, "sample", "orders", "cancel", "--read-only", "--help")
	if code != 0 || !strings.Contains(out, "--all") {
		t.Fatalf("a named write's own help must still render, code=%d:\n%s", code, out)
	}
	_, stderr, code := runWith(selectorDomains(), nil, "sample", "orders", "cancel", "--all", "--read-only", "--base-url", "https://example.test")
	if code == 0 || !strings.Contains(stderr, "read-only") {
		t.Fatalf("running a write under read-only must still be refused, code=%d stderr=%s", code, stderr)
	}
}

// On the MCP face a switch is a JSON boolean: published as one, accepted as one,
// refused as anything else, and never sent.
func TestMCP_SwitchIsABoolean(t *testing.T) {
	op := selectorDomains()[0].Subs[0].Ops[1]

	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(inputSchemaFor(op, false), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties["all"]["type"] != "boolean" {
		t.Fatalf("a switch must be published as a boolean, got %v", schema.Properties["all"])
	}
	for _, r := range schema.Required {
		if r == "all" {
			t.Fatal("a switch is never required")
		}
	}

	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		DryRun: func() bool { return true },
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "sample")

	res, err := h(context.Background(), callToolReq(`{"all":true,"side":"2"}`))
	if err != nil || res.IsError {
		t.Fatalf("all=true must be accepted: err=%v res=%s", err, toolText(res))
	}
	if strings.Contains(toolText(res), "all=") || !strings.Contains(toolText(res), "side=2") {
		t.Fatalf("the switch must not travel: %s", toolText(res))
	}

	res, _ = h(context.Background(), callToolReq(`{"all":false,"side":"2"}`))
	if !res.IsError || !strings.Contains(toolText(res), "missing_argument") {
		t.Fatalf("all=false is the switch left off, so no selector remains: %s", toolText(res))
	}

	res, _ = h(context.Background(), callToolReq(`{"all":"true"}`))
	if !res.IsError || !strings.Contains(toolText(res), "boolean") {
		t.Fatalf("a non-boolean switch must be refused: %s", toolText(res))
	}
}

// The MCP dry-run checks the body it was given, like the CLI's, and says so.
func TestMCP_DryRunChecksASuppliedBody(t *testing.T) {
	type reqBody struct {
		Quantity string `json:"quantity" validate:"required" decimal:"positive"`
	}
	op := api.Operation{Name: "place", Method: "POST", Path: "/x/place", Access: api.Write, Summary: "s", Body: reqBody{}}
	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		DryRun: func() bool { return true },
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "sample")

	res, _ := h(context.Background(), callToolReq(`{"body":{"quantity":"-1"}}`))
	if !res.IsError || !strings.Contains(toolText(res), "positive decimal") {
		t.Fatalf("a dry-run must refuse a bad supplied body: %s", toolText(res))
	}
	res, _ = h(context.Background(), callToolReq(`{"body":{"quantity":"0.5"}}`))
	if res.IsError || !strings.Contains(toolText(res), `"body_checked": true`) {
		t.Fatalf("a checked body must be reported as checked: %s", toolText(res))
	}
	res, _ = h(context.Background(), callToolReq(`{}`))
	if res.IsError || !strings.Contains(toolText(res), `"body_checked": false`) {
		t.Fatalf("a dry-run without a body still previews, and says it checked nothing: %s", toolText(res))
	}
}

// The MCP input schema carries the decimal's shape, so a host that validates
// arguments refuses a malformed amount before the call.
func TestMCP_DecimalPublishesItsShape(t *testing.T) {
	f := api.BodyField{Name: "quantity", Type: "string", Decimal: api.DecimalPositive}
	if got := bodyFieldSchema(f)["pattern"]; got != decimalShapePattern {
		t.Fatalf("decimal field must publish the shape pattern, got %v", got)
	}
	f.Pattern = "^1$"
	if got := bodyFieldSchema(f)["pattern"]; got != "^1$" {
		t.Fatalf("an explicit pattern wins over the decimal shape, got %v", got)
	}
}
