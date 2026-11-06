package cli

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

// TestUnattendedSkipsWriteConfirmation is Property 12 at the handler: for a write
// under ANY confirm-argument value, an unattended build neither refuses with
// confirmation_required nor emits a single confirmation record.
//
// With nobody in the loop the only confirmation obtainable is one the caller sets
// for itself, so recording it would fabricate a human's approval. This is the
// decision point in toolHandler; TestAttendedWriteStillConfirms is its control.
//
// _Validates: R10.1, R10.7, R10.8 (Property 12)_
func TestUnattendedSkipsWriteConfirmation(t *testing.T) {
	writeOp := api.Operation{
		Name: "toggle", Method: "POST", Path: "/fixture/toggle",
		Access: api.Write, Summary: "flip a switch",
		Params: []api.Param{{Name: "enable", Required: true}},
	}
	// The confirm argument in each form a caller could send it; unattended ignores
	// all three (and none must trip rejectUndeclared).
	for _, args := range []string{
		`{"enable":"true"}`,
		`{"enable":"true","confirm":false}`,
		`{"enable":"true","confirm":true}`,
	} {
		rep := &fakeReporter{}
		f := unattendedWriteFactory(rep, true)
		h := toolHandler(f, writeOp, api.SignNone, api.IdentityNone, "sample")

		res, err := h(context.Background(), callToolReq(args))
		if err != nil {
			t.Fatalf("args %s: transport error: %v", args, err)
		}
		if res.IsError {
			t.Fatalf("args %s: an unattended write must not be refused, got %s", args, toolText(res))
		}
		if got := rep.all(); len(got) != 0 {
			t.Fatalf("args %s: unattended build produced %d confirmation records, want 0: %+v",
				args, len(got), got)
		}
	}
}

// TestAttendedWriteStillConfirms is the control for the test above: with a person
// reachable and no confirm argument, the SAME write is refused with
// confirmation_required and DOES record a reminder. If this stops holding, the
// unattended test is passing vacuously.
func TestAttendedWriteStillConfirms(t *testing.T) {
	writeOp := api.Operation{
		Name: "toggle", Method: "POST", Path: "/fixture/toggle",
		Access: api.Write, Summary: "flip a switch",
		Params: []api.Param{{Name: "enable", Required: true}},
	}
	rep := &fakeReporter{}
	f := unattendedWriteFactory(rep, false) // attended
	h := toolHandler(f, writeOp, api.SignNone, api.IdentityNone, "sample")

	res, err := h(context.Background(), callToolReq(`{"enable":"true"}`))
	if err != nil {
		t.Fatalf("transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an attended write with no confirmation must be refused, got %s", toolText(res))
	}
	if !strings.Contains(toolText(res), "confirmation_required") {
		t.Fatalf("attended refusal must be confirmation_required, got %s", toolText(res))
	}
	if len(rep.all()) == 0 {
		t.Fatal("the attended gate must record a reminder")
	}
}

// unattendedWriteFactory builds the minimum Factory the write path reads: a
// dry-run client so BuildSpec+Send return a plan without a network, a capturing
// confirmation reporter, and the unattended fact under test.
func unattendedWriteFactory(rep confirmaudit.Reporter, unattended bool) *Factory {
	f := &Factory{
		Config:          func() config.Config { return config.Config{Env: "prod"} },
		Env:             func(string) string { return "" },
		Out:             io.Discard,
		Err:             io.Discard,
		DryRun:          func() bool { return false },
		ConfirmReporter: func() confirmaudit.Reporter { return rep },
		Client: func(context.Context) *transport.Client {
			return &transport.Client{DryRun: true, BaseURLOverride: "https://example.test"}
		},
	}
	if unattended {
		f.Unattended = func() bool { return true }
	}
	return f
}

// TestUnattendedOmitsConfirmParam pins the second decision point: the two-step
// confirm parameter is published on an attended write and withheld on an
// unattended one, where it would only invite the caller to read the preview's
// refusal as a failure.
func TestUnattendedOmitsConfirmParam(t *testing.T) {
	writeOp := api.Operation{Name: "toggle", Access: api.Write, Summary: "s"}

	if !schemaHasConfirm(t, inputSchemaFor(writeOp, false)) {
		t.Fatal("an attended write must publish the confirm parameter")
	}
	if schemaHasConfirm(t, inputSchemaFor(writeOp, true)) {
		t.Fatal("an unattended write must not publish the confirm parameter")
	}
}

func schemaHasConfirm(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	props, _ := m["properties"].(map[string]any)
	_, ok := props["confirm"]
	return ok
}

// TestToolIndexMatchesRegisteredSurface pins ToolIndex to the same registry walk
// registerTools uses: a read and a write carry the right Write flag, and an
// excluded operation (the WS collector) is absent, exactly as it is from the tool
// surface.
func TestToolIndexMatchesRegisteredSurface(t *testing.T) {
	f := &Factory{Domains: domains(), Config: func() config.Config { return config.Config{} }}
	k := &Kernel{Factory: f}
	idx := k.ToolIndex()

	if ref, ok := idx["sample_public_ticker"]; !ok || ref.Write {
		t.Fatalf("read tool sample_public_ticker: got %+v, ok=%v (want Write=false)", ref, ok)
	}
	if ref, ok := idx["sample_signed_toggle"]; !ok || !ref.Write {
		t.Fatalf("write tool sample_signed_toggle: got %+v, ok=%v (want Write=true)", ref, ok)
	}
	// The WS-backed op is excluded from the tool surface, so it must be absent from
	// the index a middleware consults — otherwise the two would disagree.
	if _, ok := idx["sample_stream_ws-ticker"]; ok {
		t.Fatal("the WS-backed op is excluded from the surface; it must not be in the index")
	}
}

func jsonTool() (*mcp.Tool, mcp.ToolHandler) {
	return &mcp.Tool{Name: "d_s_get", Description: "x", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"a":1,"b":2}`}}}, nil
		}
}

// TestStdioCapabilitiesUnchangedByNilPath is the byte-for-byte guarantee for the
// stdio face: routing NewServer through newMCPServer(name, nil) advertises exactly
// the capabilities the old mcp.NewServer(impl, nil) did. Both register an identical
// tool so tool inference is comparable.
func TestStdioCapabilitiesUnchangedByNilPath(t *testing.T) {
	tool, h := jsonTool()

	oldSrv := mcp.NewServer(&mcp.Implementation{Name: "x", Version: "0"}, nil)
	oldSrv.AddTool(tool, h)
	newSrv := newMCPServer("x", nil)
	newSrv.AddTool(tool, h)

	oldCaps := advertisedCapabilities(t, oldSrv)
	newCaps := advertisedCapabilities(t, newSrv)

	if !reflect.DeepEqual(oldCaps, newCaps) {
		t.Fatalf("the nil-capability path changed advertised capabilities:\n old=%+v\n new=%+v", oldCaps, newCaps)
	}
	// Guard against the comparison passing on two identical nils: a tool IS
	// registered, so the tools capability must be present.
	if newCaps == nil || newCaps.Tools == nil {
		t.Fatalf("expected the tools capability to be advertised, got %+v", newCaps)
	}
}

func advertisedCapabilities(t *testing.T, srv *mcp.Server) *mcp.ServerCapabilities {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = cs.Close() }()
	return cs.InitializeResult().Capabilities
}

// stubLookup / stubBinder are the minimum credential.Binder + Lookup a wiring test
// needs; neither resolves anything.
type stubLookup struct{}

func (stubLookup) Provider(credential.Realm) (credential.Provider, bool) { return nil, false }

type stubBinder struct{ lk credential.Lookup }

func (b stubBinder) Bind(context.Context) credential.Lookup { return b.lk }

// TestIdentityBinderWiring pins task 17's zero-value path and its live path from
// one place. Unset, a client carries no Lookup and so the transport falls back to
// the process registry (the local CLI's behaviour, byte-for-byte). Set, the
// binder's Lookup lands on the client, which is where the per-request identity
// mechanism actually takes effect.
func TestIdentityBinderWiring(t *testing.T) {
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(string) string { return "" }, io.Discard, io.Discard)

	if c := app.f.Client(context.Background()); c.Identities != nil {
		t.Fatal("without SetIdentityBinder, Client.Identities must be nil (process registry in effect)")
	}

	want := stubLookup{}
	app.SetIdentityBinder(func() credential.Binder { return stubBinder{lk: want} })
	c := app.f.Client(context.Background())
	if c.Identities != credential.Lookup(want) {
		t.Fatalf("after SetIdentityBinder, Client.Identities must be the binder's Lookup, got %#v", c.Identities)
	}
}
