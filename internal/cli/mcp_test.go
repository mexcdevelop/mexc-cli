package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// MCP tool descriptors are injected into the model's context on every session,
// so their serialized size is a real, recurring token cost. These budgets are a
// governance gate: a new subdomain that
// balloons the tool set fails the build here, forcing a conscious decision —
// compress descriptions, merge tools, or move an operation to CLI-only — rather
// than letting the context cost creep up unnoticed.
//
// Tokens are approximated as serialized-bytes / 4, a rough but conventional
// heuristic. The numbers are deliberately loose: the gate should fire on real bloat,
// not on routine growth. Baseline at introduction: 13 tools, ~1190 tokens.
const (
	// tokenBudget caps the whole tool set. Re-budgeted from 8000 to 9000 when the
	// spot.dca write surface was completed to match the backend (strategy-create
	// plus the whole-strategy / symbol updates changeDetail / changeSymbol): those
	// carry a nested symbolList body, so the set crossed 8000. The growth is real
	// capability, not bloat; raised deliberately per this test's own guidance.
	tokenBudget = 9000
	// perToolTokenBudget caps any single tool, catching an over-long description
	// or param list before it skews the whole set. Re-budgeted from 400 to 500
	// when spot.trade moved to the gateway: its placement ops (place, place-oco,
	// place-trigger, place-market-trigger, place-track) carry a 10-13 field JSON
	// body, and each field's name+type+enum+desc is reflected into the tool's
	// input schema. Those tools land at ~436-449 tokens with descriptions already
	// pared to one clause each, so the size is the operation's inherent shape, not
	// bloat. The gate still fires above 500.
	perToolTokenBudget = 500
)

// estTokens approximates the token cost of a JSON-serializable value.
func estTokens(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b) / 4
}

// TestMCPToolTokenBudget is the governance gate: it derives the full tool set
// the MCP server would expose and asserts the total (and each tool) stays within
// budget. On failure it reports the usage and the remedies, so the breach is
// actionable rather than just a red build.
func TestMCPToolTokenBudget(t *testing.T) {
	total := 0
	worst := ""
	worstTokens := 0
	count := 0
	for _, d := range domains() {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				tool := toolFor(d.Name, sd.Name, op, false)
				tk := estTokens(tool)
				total += tk
				count++
				if tk > worstTokens {
					worstTokens, worst = tk, tool.Name
				}
				if tk > perToolTokenBudget {
					t.Errorf("tool %q ~%d tokens exceeds per-tool budget %d; shorten its description or param help",
						tool.Name, tk, perToolTokenBudget)
				}
			}
		}
	}
	t.Logf("MCP tool set: %d tools, ~%d tokens (budget %d, %d remaining); largest: %q ~%d tokens",
		count, total, tokenBudget, tokenBudget-total, worst, worstTokens)
	if total > tokenBudget {
		t.Fatalf("MCP tool set ~%d tokens exceeds budget %d. Remedies, in order: "+
			"compress descriptions; merge similar operations; move an operation to CLI-only; "+
			"or, if the growth is justified, raise tokenBudget deliberately in this test.",
			total, tokenBudget)
	}
}

func TestToolName(t *testing.T) {
	// Segments join with '_'; a hyphenated op name is left intact.
	if got := toolName("sample", "public", "ticker"); got != "sample_public_ticker" {
		t.Fatalf("toolName = %q", got)
	}
	// A dotted scope (defensive) collapses to underscores.
	if got := toolName("sample", "sub", "a.b"); got != "sample_sub_a_b" {
		t.Fatalf("toolName dotted = %q", got)
	}
}

func TestInputSchemaFor(t *testing.T) {
	op := api.Operation{
		Params: []api.Param{
			{Name: "symbol", Desc: "trading pair", Required: true},
			{Name: "interval", Enum: []string{"1m", "1d"}},
		},
	}
	var schema struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type        string   `json:"type"`
			Description string   `json:"description"`
			Enum        []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(inputSchemaFor(op, false), &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("schema type = %q, want object", schema.Type)
	}
	if schema.Properties["symbol"].Type != "string" || schema.Properties["symbol"].Description != "trading pair" {
		t.Fatalf("symbol prop wrong: %+v", schema.Properties["symbol"])
	}
	if len(schema.Properties["interval"].Enum) != 2 {
		t.Fatalf("interval enum missing: %+v", schema.Properties["interval"])
	}
	if len(schema.Required) != 1 || schema.Required[0] != "symbol" {
		t.Fatalf("required = %v, want [symbol]", schema.Required)
	}
}

// TestInputSchemaFor_Body asserts a Body op surfaces its JSON body as one
// structured, required `body` object property whose shape (nested objects,
// arrays, enums, required fields) is reflected inline — so an agent can fill a
// native object without a separate `schema` call.
func TestInputSchemaFor_Body(t *testing.T) {
	type nested struct {
		Kind string `json:"kind" enum:"a,b" desc:"k"`
	}
	type reqBody struct {
		AppID string   `json:"appId" validate:"required" desc:"id"`
		Tags  []string `json:"tags,omitempty"`
		Meta  nested   `json:"meta,omitempty"`
	}
	op := api.Operation{Method: "POST", Access: api.Write, Body: reqBody{}}

	var m map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &m); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	// `body` is a required top-level object property.
	if !containsStr(toStrs(m["required"]), "body") {
		t.Fatalf("top-level required must include body, got %v", m["required"])
	}
	body, ok := m["properties"].(map[string]any)["body"].(map[string]any)
	if !ok || body["type"] != "object" {
		t.Fatalf("expected a `body` object property, got %v", m["properties"])
	}
	bprops := body["properties"].(map[string]any)
	// Required body field appId is listed in body.required.
	if !containsStr(toStrs(body["required"]), "appId") {
		t.Fatalf("body.required must include appId, got %v", body["required"])
	}
	// Nested object carries its own properties + enum.
	meta := bprops["meta"].(map[string]any)
	if meta["type"] != "object" {
		t.Fatalf("meta type = %v, want object", meta["type"])
	}
	kind := meta["properties"].(map[string]any)["kind"].(map[string]any)
	if kind["enum"] == nil {
		t.Fatalf("nested enum was lost: %v", kind)
	}
	// Array of scalars: typed array, no items constraint (element type unknown).
	tags := bprops["tags"].(map[string]any)
	if tags["type"] != "array" {
		t.Fatalf("tags type = %v, want array", tags["type"])
	}
	if _, ok := tags["items"]; ok {
		t.Fatalf("array of scalars must not fabricate an items object: %v", tags)
	}
}

// TestInputSchemaFor_ArrayBody asserts an op whose body IS a JSON array
// advertises `body` as an array of the item object, not as the item object
// itself. api.BodyFields describes one item, so publishing it unwrapped would
// tell an agent to send `{...}` where ValidateBody demands `[{...}]` — the two
// faces have to agree or every call from an MCP host is rejected locally.
func TestInputSchemaFor_ArrayBody(t *testing.T) {
	type item struct {
		Symbol string `json:"symbol" validate:"required" desc:"pair"`
		Side   string `json:"side" enum:"BUY,SELL"`
	}
	op := api.Operation{Method: "POST", Access: api.Write, Body: []item{}}

	var m map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &m); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if !containsStr(toStrs(m["required"]), "body") {
		t.Fatalf("top-level required must include body, got %v", m["required"])
	}
	body, ok := m["properties"].(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("no body property: %v", m["properties"])
	}
	if body["type"] != "array" {
		t.Fatalf("body type = %v, want array", body["type"])
	}
	items, ok := body["items"].(map[string]any)
	if !ok {
		t.Fatalf("an array body must publish its item schema, got %v", body)
	}
	if items["type"] != "object" {
		t.Fatalf("items type = %v, want object", items["type"])
	}
	if !containsStr(toStrs(items["required"]), "symbol") {
		t.Fatalf("items.required must include symbol, got %v", items["required"])
	}
	side, ok := items["properties"].(map[string]any)["side"].(map[string]any)
	if !ok || side["enum"] == nil {
		t.Fatalf("the item's enum was lost: %v", items["properties"])
	}
}

func TestInputSchemaFor_ScalarArrayBody(t *testing.T) {
	op := api.Operation{Method: "POST", Access: api.Write, Body: []int64{}}

	var m map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &m); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	body, ok := m["properties"].(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("no body property: %v", m["properties"])
	}
	if body["type"] != "array" {
		t.Fatalf("body type = %v, want array", body["type"])
	}
	items, ok := body["items"].(map[string]any)
	if !ok {
		t.Fatalf("scalar array body must publish item schema, got %v", body)
	}
	if items["type"] != "integer" {
		t.Fatalf("items type = %v, want integer", items["type"])
	}
	if _, ok := items["properties"]; ok {
		t.Fatalf("scalar array items must not publish object properties: %v", items)
	}
}

func toStrs(v any) []string {
	xs, _ := v.([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func toolText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func callToolReq(args string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)}}
}

// TestToolHandler_BodyValidationRejects proves the MCP handler routes the
// structured `body` argument through ValidateBody exactly like the CLI's --data:
// a body missing a required field is refused (before any request is built), so a
// Body op can no longer silently transport an empty/invalid body over MCP.
func TestToolHandler_BodyValidationRejects(t *testing.T) {
	type reqBody struct {
		AppID string `json:"appId" validate:"required" desc:"id"`
	}
	op := api.Operation{Name: "add", Method: "POST", Path: "/x/add", Access: api.Write, Summary: "s", Body: reqBody{}}
	// Not a dry-run, so ValidateBody runs; it fails before f.Client() is touched.
	f := &Factory{Out: io.Discard, Err: io.Discard, DryRun: func() bool { return false }}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "")

	res, err := h(context.Background(), callToolReq(`{"body":{}}`))
	if err != nil {
		t.Fatalf("handler must not return a transport error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a body missing the required appId must be rejected, got %+v", res)
	}
	if !strings.Contains(toolText(res), "appId") {
		t.Fatalf("error should name the missing field, got %q", toolText(res))
	}
}

// TestToolHandler_BodyRoutedToPlan proves a valid structured body flows all the
// way into the request spec: a dry-run reports a JSON body was attached (empty
// data would still be BodyKind json, but the companion rejection test above rules
// out the empty case, so together they pin routing end to end).
func TestToolHandler_BodyRoutedToPlan(t *testing.T) {
	type reqBody struct {
		AppID string `json:"appId" validate:"required"`
	}
	op := api.Operation{Name: "add", Method: "POST", Path: "/x/add", Access: api.Write, Summary: "s", Body: reqBody{}}
	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		DryRun: func() bool { return true },
		// BaseURLOverride bypasses the host table so the dry-run renders a plan
		// offline (a public host key with an unsigned identity honors the override).
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "sample")

	res, err := h(context.Background(), callToolReq(`{"body":{"appId":"x"}}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("a valid body dry-run must not error: %s", toolText(res))
	}
	if !strings.Contains(toolText(res), `"body_kind": "json"`) {
		t.Fatalf("dry-run plan should report a json body, got %s", toolText(res))
	}
	if !strings.Contains(toolText(res), `"method": "POST"`) {
		t.Fatalf("dry-run plan method wrong: %s", toolText(res))
	}
}

// TestToolHandler_StrayBodyOnNonBodyOp confirms a `body` argument sent to an op
// that declares no Body is caught as an undeclared key (fail-closed), never
// silently dropped.
func TestToolHandler_StrayBodyOnNonBodyOp(t *testing.T) {
	op := api.Operation{Name: "get", Method: "GET", Path: "/x", Access: api.Read, Summary: "s",
		Params: []api.Param{{Name: "id"}}}
	f := &Factory{Out: io.Discard, Err: io.Discard, DryRun: func() bool { return false }}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "")

	res, err := h(context.Background(), callToolReq(`{"body":{"x":1}}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a stray body on a non-body op must be rejected, got %+v", res)
	}
}

// TestToolHandler_NonStringParamRejected keeps the params-are-strings contract:
// a non-string param value is refused with a structured error naming the key.
func TestToolHandler_NonStringParamRejected(t *testing.T) {
	op := api.Operation{Name: "get", Method: "GET", Path: "/x", Access: api.Read, Summary: "s",
		Params: []api.Param{{Name: "limit"}}}
	f := &Factory{Out: io.Discard, Err: io.Discard, DryRun: func() bool { return false }}
	h := toolHandler(f, op, api.SignNone, api.IdentityNone, "")

	res, err := h(context.Background(), callToolReq(`{"limit":5}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a non-string param must be rejected, got %+v", res)
	}
	if !strings.Contains(toolText(res), "limit") {
		t.Fatalf("error should name the offending key, got %q", toolText(res))
	}
}

// TestInputSchemaFor_Body_TypedEnum asserts a numeric-enum body field surfaces
// its enum as JSON numbers, consistent with its integer type — so an agent fills
// the value the strict body decode expects, not a string it would reject.
func TestInputSchemaFor_Body_TypedEnum(t *testing.T) {
	type reqBody struct {
		Model int `json:"channelModel" validate:"required" enum:"1,2"`
	}
	op := api.Operation{Method: "POST", Access: api.Write, Body: reqBody{}}

	var m map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &m); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	body := m["properties"].(map[string]any)["body"].(map[string]any)
	cm := body["properties"].(map[string]any)["channelModel"].(map[string]any)
	if cm["type"] != "integer" {
		t.Fatalf("channelModel type = %v, want integer", cm["type"])
	}
	enum, ok := cm["enum"].([]any)
	if !ok || len(enum) != 2 {
		t.Fatalf("enum missing/wrong: %v", cm["enum"])
	}
	for _, e := range enum {
		if _, isStr := e.(string); isStr {
			t.Fatalf("integer enum value must be a JSON number, not a string: %v", enum)
		}
	}
	if enum[0] != float64(1) || enum[1] != float64(2) {
		t.Fatalf("enum = %v, want [1 2]", enum)
	}
}

func TestInputSchemaFor_NoParams(t *testing.T) {
	// An op with no params still produces a valid object schema with no required[].
	raw := inputSchemaFor(api.Operation{}, false)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "object" {
		t.Fatalf("type = %v", m["type"])
	}
	if _, ok := m["required"]; ok {
		t.Fatal("required[] must be omitted when there are no required params")
	}
}

func TestRejectUndeclared(t *testing.T) {
	op := api.Operation{Params: []api.Param{{Name: "symbol"}, {Name: "limit"}}}

	if err := rejectUndeclared(op, map[string]string{"symbol": "BTC", "limit": "5"}); err != nil {
		t.Fatalf("declared keys must pass, got %v", err)
	}
	err := rejectUndeclared(op, map[string]string{"symbol": "BTC", "bogus": "x"})
	if err == nil {
		t.Fatal("an undeclared key must be rejected")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Type != errs.TypeValidation || e.Param != "bogus" {
		t.Fatalf("expected a validation error naming param 'bogus', got %v", err)
	}
}

func TestToolFor_AnnotationsFromWriteSemantics(t *testing.T) {
	no, yes := false, true

	read := toolFor("sample", "public", api.Operation{Name: "depth", Access: api.Read, Summary: "s"}, false)
	if !read.Annotations.ReadOnlyHint {
		t.Error("read op must be readOnly")
	}

	// A preference-toggle write: non-destructive, idempotent.
	set := toolFor("sample", "signed", api.Operation{
		Name: "toggle", Access: api.Write, Summary: "s",
		Destructive: &no, Idempotent: &yes,
	}, false)
	if set.Annotations.ReadOnlyHint {
		t.Error("write op must not be readOnly")
	}
	if set.Annotations.DestructiveHint == nil || *set.Annotations.DestructiveHint {
		t.Error("toggle write must advertise destructive=false")
	}
	if !set.Annotations.IdempotentHint {
		t.Error("toggle write must advertise idempotent=true")
	}
}

func TestResolveMCPScope(t *testing.T) {
	// No args → refused. Serving the whole registry unscoped would spend an
	// agent host's context on a tool list before it reads one description, so
	// there is no unscoped mode; the caller must name a domain. The hint has to
	// name the domains available, or the refusal is a dead end.
	_, err := resolveMCPScope(domains(), nil)
	var missing *errs.Error
	if !errors.As(err, &missing) || missing.Subtype != errs.SubtypeMissingArgument {
		t.Fatalf("no args want missing_argument, got %v", err)
	}
	if !strings.Contains(missing.Hint, "sample") {
		t.Fatalf("no-args hint must list the available domains, got %q", missing.Hint)
	}
	// Domain only.
	s, err := resolveMCPScope(domains(), []string{"sample"})
	if err != nil || s.domain != "sample" || s.subs != nil {
		t.Fatalf("domain scope = %+v err=%v", s, err)
	}
	// Domain + subdomains.
	s, err = resolveMCPScope(domains(), []string{"sample", "orders", "public"})
	if err != nil || s.domain != "sample" || !s.subs["orders"] || !s.subs["public"] || s.subs["signed"] {
		t.Fatalf("sub scope = %+v err=%v", s, err)
	}
	// Unknown domain → structured invalid_argument.
	_, err = resolveMCPScope(domains(), []string{"bogus"})
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("unknown domain want invalid_argument, got %v", err)
	}
	// Unknown subdomain → structured invalid_argument.
	_, err = resolveMCPScope(domains(), []string{"sample", "bogus"})
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("unknown subdomain want invalid_argument, got %v", err)
	}
}

func collectSubs(regs []mcpToolReg) map[string]bool {
	m := map[string]bool{}
	for _, r := range regs {
		m[r.domain+"."+r.sub] = true
	}
	return m
}

func TestCollectMCPTools_Scoped(t *testing.T) {
	// All three fixture subdomains present.
	all := collectSubs(collectMCPTools(domains(), mcpScope{}, false))
	for _, want := range []string{"sample.public", "sample.signed", "sample.orders"} {
		if !all[want] {
			t.Fatalf("unscoped should expose %s; got %v", want, all)
		}
	}
	// Scoped to one subdomain: the other two are withheld.
	only := collectSubs(collectMCPTools(domains(), mcpScope{domain: "sample", subs: map[string]bool{"orders": true}}, false))
	if !only["sample.orders"] {
		t.Fatalf("scoped should expose sample.orders; got %v", only)
	}
	if only["sample.public"] || only["sample.signed"] {
		t.Fatalf("scoping to orders must withhold the others; got %v", only)
	}
	// Read-only drops write ops (sample.orders has writes; still exposes its reads).
	ro := collectMCPTools(domains(), mcpScope{domain: "sample", subs: map[string]bool{"orders": true}}, true)
	for _, r := range ro {
		if r.op.IsWrite() {
			t.Fatalf("read-only must withhold write op %q", r.op.Name)
		}
	}
}

func TestMCPServerName(t *testing.T) {
	if got := mcpServerName(nil); got != "mexc-cli" {
		t.Fatalf("no-arg name = %q", got)
	}
	if got := mcpServerName([]string{"sample", "orders"}); got != "mexc-cli [sample orders]" {
		t.Fatalf("scoped name = %q", got)
	}
}

// fakeElicitor stands in for a ServerSession's Elicit in mcpConfirm tests.
type fakeElicitor struct {
	action string
	err    error
	calls  int
	// gotMessage is what the host would have shown the user, kept so a test can
	// assert the prompt names what it is asking about.
	gotMessage string
}

func (f *fakeElicitor) Elicit(ctx context.Context, p *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	f.calls++
	f.gotMessage = p.Message
	if f.err != nil {
		return nil, f.err
	}
	return &mcp.ElicitResult{Action: f.action}, nil
}

func TestMCPConfirm_Elicitation(t *testing.T) {
	op := api.Operation{Name: "place", Access: api.Write}

	// accept → proceeds.
	acc := &fakeElicitor{action: "accept"}
	if _, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, acc, op, "msg", false); err != nil {
		t.Errorf("accept must proceed: %v", err)
	}
	if acc.calls != 1 {
		t.Errorf("elicitation should be called once, got %d", acc.calls)
	}

	// decline / cancel → refused, not executed.
	for _, action := range []string{"decline", "cancel"} {
		_, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, &fakeElicitor{action: action}, op, "msg", false)
		if err == nil {
			t.Fatalf("%s must refuse", action)
		}
		var e *errs.Error
		if !errors.As(err, &e) || e.Subtype != errs.SubtypeFailedPrecondition {
			t.Errorf("%s: want failed_precondition, got %v", action, err)
		}
	}

	// With elicitation present, the confirm flag is ignored (still elicits).
	el := &fakeElicitor{action: "decline"}
	if _, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, el, op, "msg", true); err == nil {
		t.Error("confirm=true must NOT bypass an elicitation dialog")
	}
	if el.calls != 1 {
		t.Error("elicitation must run even when confirm=true was passed")
	}

	// Elicit transport error → internal error (not a silent proceed).
	_, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, &fakeElicitor{err: errors.New("boom")}, op, "msg", false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Type != errs.TypeInternal {
		t.Errorf("elicit error must surface as internal, got %v", err)
	}
}

func TestMCPConfirm_TwoStepFallback(t *testing.T) {
	op := api.Operation{Name: "place", Access: api.Write}

	// No elicitation, no confirm → confirmation_required (agent relays, retries).
	// The display (summary + params) must be carried in the message.
	display := "Place a spot order\n  symbol  BTCUSDT"
	_, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, nil, op, display, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeConfirmationRequired {
		t.Fatalf("want confirmation_required, got %v", err)
	}
	if !strings.Contains(e.Message, "Place a spot order") || !strings.Contains(e.Message, "BTCUSDT") {
		t.Errorf("confirmation_required must carry the request display: %q", e.Message)
	}
	if !strings.Contains(e.Hint, "confirm=true") {
		t.Errorf("hint should tell the agent to retry with confirm=true: %q", e.Hint)
	}

	// No elicitation, confirm=true → proceeds.
	if _, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, nil, op, display, true); err != nil {
		t.Errorf("confirm=true must proceed when there is no elicitation: %v", err)
	}
}

func TestHostSupportsElicitation_NilSession(t *testing.T) {
	if hostSupportsElicitation(nil) {
		t.Error("a nil session must not report elicitation support")
	}
}

func TestInputSchemaFor_WriteConfirmParam(t *testing.T) {
	var m map[string]any
	// A write op exposes a non-required `confirm` boolean.
	_ = json.Unmarshal(inputSchemaFor(api.Operation{Name: "place", Access: api.Write}, false), &m)
	props := m["properties"].(map[string]any)
	confirm, ok := props["confirm"].(map[string]any)
	if !ok || confirm["type"] != "boolean" {
		t.Fatalf("write op must expose a boolean confirm param, got %v", props["confirm"])
	}
	if containsStr(toStrs(m["required"]), "confirm") {
		t.Error("confirm must not be required (first call previews)")
	}
	// A read op does not.
	var r map[string]any
	_ = json.Unmarshal(inputSchemaFor(api.Operation{Name: "get", Access: api.Read}, false), &r)
	if _, ok := r["properties"].(map[string]any)["confirm"]; ok {
		t.Error("a read op must not expose a confirm param")
	}
}

func TestInputSchemaFor_Constraints(t *testing.T) {
	type body struct {
		Amount float64 `json:"amount" minval:"0" maxval:"1000"`
		Slug   string  `json:"slug" minlen:"2" maxlen:"10" pattern:"[a-z-]+"`
	}
	op := api.Operation{
		Name: "w", Method: "POST", Path: "/w", Access: api.Write, Summary: "w", Body: body{},
		Params: []api.Param{{Name: "code", MinLen: 1, MaxLen: 4, Pattern: "[A-Z]+"}},
	}
	var m map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &m); err != nil {
		t.Fatal(err)
	}
	props := m["properties"].(map[string]any)

	// Param (string type): minLength/maxLength/pattern.
	code := props["code"].(map[string]any)
	if code["minLength"] != float64(1) || code["maxLength"] != float64(4) || code["pattern"] != "[A-Z]+" {
		t.Errorf("param constraints not in schema: %v", code)
	}

	// Body numeric field: minimum/maximum. String field: minLength/maxLength/pattern.
	bprops := props["body"].(map[string]any)["properties"].(map[string]any)
	amount := bprops["amount"].(map[string]any)
	if amount["minimum"] != float64(0) || amount["maximum"] != float64(1000) {
		t.Errorf("numeric body bounds not in schema: %v", amount)
	}
	slug := bprops["slug"].(map[string]any)
	if slug["minLength"] != float64(2) || slug["maxLength"] != float64(10) || slug["pattern"] != "[a-z-]+" {
		t.Errorf("string body constraints not in schema: %v", slug)
	}
}

// The MCP face has the same obligation as the CLI one: a read asked about only
// because the identity graded it must not be announced as a write.
func TestMCPConfirm_GradedReadWithoutElicitationIsRefusedAsARead(t *testing.T) {
	op := api.Operation{Name: "list", Access: api.Read, Summary: "List things"}

	_, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, nil, op,
		"graded: because\n\nList things", false)
	if err == nil {
		t.Fatal("a graded read was allowed through")
	}
	msg := err.Error()
	if !strings.Contains(msg, "this read needs") {
		t.Errorf("refusal does not call it a read:\n%s", msg)
	}
	if strings.Contains(msg, "this write needs") {
		t.Errorf("refusal calls a read a write:\n%s", msg)
	}
}

// With elicitation the host renders the dialog, and its message must name the
// read too — that surface is the one the user actually reads.
func TestMCPConfirm_GradedReadElicitationMessageNamesTheRead(t *testing.T) {
	op := api.Operation{Name: "list", Access: api.Read, Summary: "List things"}
	el := &fakeElicitor{action: "accept"}

	if _, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, el, op, "the request", false); err != nil {
		t.Fatalf("accepted elicitation errored: %v", err)
	}
	if !strings.Contains(el.gotMessage, "Confirm this read:") {
		t.Errorf("elicitation message:\n%s", el.gotMessage)
	}
}

// The elicitation refusal named everything a write too.
func TestMCPConfirm_DeclinedGradedReadIsNamedARead(t *testing.T) {
	op := api.Operation{Name: "list", Access: api.Read, Summary: "List things"}
	el := &fakeElicitor{action: "decline"}

	_, err := mcpConfirm(context.Background(), nil, confirmaudit.Event{}, el, op, "the request", false)
	if err == nil {
		t.Fatal("a declined graded read was allowed through")
	}
	if msg := err.Error(); !strings.Contains(msg, `read "list" was not confirmed`) {
		t.Errorf("refusal does not name the read:\n%s", msg)
	}
}

// TestToolHandler_GradedReadEntersTheGate is this face's counterpart to
// TestGradedReadConfirmation_CLIWiring. The mcpConfirm tests above are handed a
// display that already carries the reason; only this one proves toolHandler asks
// the identity for a grade and lets the answer open the gate. It is the second
// half of the same omission: both wiring lines were deletable with every test
// still green.
func TestToolHandler_GradedReadEntersTheGate(t *testing.T) {
	p := &gradingIdentityProvider{grade: credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "graded: the identity demands it",
	}}
	credential.Register(p)

	op := api.Operation{
		Name: "list", Method: "GET", Path: "/fixture/graded",
		Access: api.Read, Summary: "A graded read",
		Permission: &api.PermReq{System: "fixture", Codes: []string{"fixture:list"}},
	}
	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		Config: func() config.Config { return config.Config{} },
		DryRun: func() bool { return false },
		// A client this call must never reach. It is here so that a regression which
		// lets the read through fails on the assertion below rather than panicking on
		// a nil client, and so that such a failure sends nothing.
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	// No elicitor: this session's host cannot ask, so the gate takes the relayed
	// refusal, which is the branch a tool caller actually meets.
	h := toolHandler(f, op, api.SignNone, api.Identity(p.Realm()), "sample")

	res, err := h(context.Background(), callToolReq(`{}`))
	if err != nil {
		t.Fatalf("handler must not return a transport error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("a graded read ran without confirmation: %+v", res)
	}
	txt := toolText(res)
	if !strings.Contains(txt, "this read needs") {
		t.Errorf("the refusal does not call it a read:\n%s", txt)
	}
	if !strings.Contains(txt, "graded: the identity demands it") {
		t.Errorf("the identity's reason never reached the caller:\n%s", txt)
	}
}

// A graded read is asked about on a host WITHOUT an elicitation dialog exactly as
// a write is: the refusal tells the caller to come back with confirm=true. So the
// tool has to publish that parameter and the handler has to accept it. Both were
// gated on op.IsWrite(), which made the refusal instruct an action the schema did
// not offer and the handler rejected — a graded read could never run on such a
// host.
func TestGradedRead_CanComplyWithItsOwnRefusal(t *testing.T) {
	p := &gradingIdentityProvider{grade: credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "graded: the identity demands it",
	}}
	credential.Register(p)

	op := api.Operation{
		Name: "list", Method: "GET", Path: "/fixture/graded",
		Access: api.Read, Summary: "A graded read",
		Permission: &api.PermReq{System: "fixture", Codes: []string{"fixture:list"}},
	}

	// The schema must offer it, or a well-behaved caller cannot even try.
	var schema map[string]any
	if err := json.Unmarshal(inputSchemaFor(op, false), &schema); err != nil {
		t.Fatalf("schema does not unmarshal: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["confirm"]; !ok {
		t.Errorf("a gradeable read publishes no confirm parameter; properties = %v", props)
	}

	// And the handler must honour it rather than reject it as undeclared.
	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		Config: func() config.Config { return config.Config{} },
		// A preview: the gate does not run, so this call reaches the argument
		// handling that used to reject `confirm`, and stops at a rendered plan
		// instead of sending anything.
		DryRun: func() bool { return true },
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	h := toolHandler(f, op, api.SignNone, api.Identity(p.Realm()), "sample")
	res, err := h(context.Background(), callToolReq(`{"confirm":true}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	// Asserted on the OUTCOME, not on one error string. Matching a particular
	// message let a mutation that only reverted the handler half slip through: the
	// argument then failed the string coercion instead, with a different sentence
	// and the same dead end.
	if res.IsError {
		t.Errorf("confirm=true on a graded read is rejected:\n%s", toolText(res))
	}
}

// An unattended face must notice nothing about grades. The gate does not run
// there, so the identity is never even asked — asking would cost it a lookup of
// its own stored state (a keychain read, a network round trip) on every tool
// call, for an answer that is then discarded.
func TestToolHandler_UnattendedFaceNeverAsksForAGrade(t *testing.T) {
	p := &gradingIdentityProvider{grade: credential.ConfirmGrade{
		Confirm: true, Code: "X2", Reason: "graded: the identity demands it",
	}}
	credential.Register(p)
	p.called = 0

	op := api.Operation{
		Name: "list", Method: "GET", Path: "/fixture/graded",
		Access: api.Read, Summary: "A graded read",
		Permission: &api.PermReq{System: "fixture", Codes: []string{"fixture:list"}},
	}
	f := &Factory{
		Out: io.Discard, Err: io.Discard,
		Config:     func() config.Config { return config.Config{} },
		DryRun:     func() bool { return false },
		Unattended: func() bool { return true },
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	h := toolHandler(f, op, api.SignNone, api.Identity(p.Realm()), "sample")

	res, err := h(context.Background(), callToolReq(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("an unattended face refused a graded read: %s", toolText(res))
	}
	if p.called != 0 {
		t.Errorf("the identity was asked %d times on an unattended face, want 0", p.called)
	}
}
