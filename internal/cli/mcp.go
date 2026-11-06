package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/internal/output"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/toolref"
)

// newMCPCmd builds the `mcp` command: it serves every declared operation as an
// MCP tool over stdio, so an agent host (Claude Code, Cursor, …) can call the
// same operations the CLI exposes — without shelling out and parsing stdout.
//
// The tools are derived from the SAME domains() registry that assembles the
// command tree and the schema command, so the three
// faces of the binary — CLI, schema, MCP — cannot drift. Nothing about an
// operation is restated here: name, description, parameter schema, and the
// read-only/destructive/idempotent safety hints all come from its Operation.
//
//	<cli> mcp                 # serve all operations over stdio
//	<cli> --read-only mcp     # serve read operations only
func newMCPCmd(f *Factory, fe *finalErr) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp <domain> [<subdomain>...]",
		Short: "Serve one domain's operations as MCP tools over stdio (for agent hosts)",
		Long: "Serve declared operations as MCP tools over stdio.\n\n" +
			"A domain is REQUIRED. The tool list is what an agent host loads into\n" +
			"context before reading anything, and this registry holds far more\n" +
			"operations than an agent should hold at once, so there is no unscoped\n" +
			"mode to fall into by accident.\n\n" +
			mcpUsageExamples(f.Domains) +
			"\nOne domain per server; mount several scoped servers in your host for\n" +
			"more. Tools derive from the same registry as the CLI command tree, so\n" +
			"they never drift. Respects --read-only (write tools withheld) and the\n" +
			"same auth and safety configuration as the CLI.",
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			scope, err := resolveMCPScope(f.Domains, args)
			if err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			server := newMCPServer(mcpServerName(args), nil)
			registerTools(server, f, scope)
			// Run blocks for the whole session and returns when the connection
			// ends. For a stdio server, the session ending — the host closing the
			// pipe (EOF), the server shutting down, or the context being cancelled
			// — is the normal end of life, not a failure. Treat all of these as a
			// clean exit; per-tool errors are reported inside their own tool result,
			// so there is nothing fatal to surface at this level. Only a context
			// error other than cancellation is worth noting.
			if err := server.Run(c.Context(), &mcp.StdioTransport{}); err != nil {
				if ctxErr := c.Context().Err(); ctxErr != nil && !errors.Is(ctxErr, context.Canceled) {
					writeAndRecord(f, fe, errs.Internal("mcp server stopped: %v", err).WithCause(err))
					return err
				}
			}
			return nil
		},
	}
}

// mcpUsageExamples builds the help text's examples from the registry actually
// mounted. A hard-coded example can name a domain this binary does not mount, and
// the help then tells the user to run something that answers "unknown domain" —
// which is exactly what a hard-coded `spot` example used to do.
func mcpUsageExamples(ds []api.DomainSpec) string {
	if len(ds) == 0 {
		return ""
	}
	d := ds[0]
	name := cliname.Name()
	out := "Examples:\n  " + name + " mcp " + d.Name + "\n"
	if len(d.Subs) > 0 {
		out += "  " + name + " mcp " + d.Name + " " + d.Subs[0].Name + "\n"
		if len(d.Subs) > 1 {
			out += "  " + name + " mcp " + d.Name + " " + d.Subs[0].Name + " " + d.Subs[1].Name + "\n"
		}
	}
	return out
}

// mcpScope limits which operations the MCP server exposes, so an agent host
// loads only the tools it needs instead of the whole registry (context bloat).
// The zero value exposes everything. domain empty = all domains; subs empty =
// all subdomains of that domain. Built and validated by resolveMCPScope.
type mcpScope struct {
	domain string          // "" = all domains
	subs   map[string]bool // empty = all subs of the domain
}

// resolveMCPScope turns the positional args (`mcp <domain> [<subdomain>...]`)
// into a validated scope. Unknown domain/subdomain fails with a structured error
// that lists the valid values, so a typo self-corrects rather than silently
// serving nothing.
//
// A domain is REQUIRED. Serving the whole registry is not a useful default: the
// tool list is what an agent host loads into context up front, and every product
// built on this layer has more operations than an agent should hold at once —
// several hundred in the larger one. An unscoped server spends that budget
// before the agent has read a single tool description, so the caller has to say
// what it came for.
func resolveMCPScope(ds []api.DomainSpec, args []string) (mcpScope, error) {
	names := make([]string, 0, len(ds))
	for i := range ds {
		names = append(names, ds[i].Name)
	}
	if len(args) == 0 {
		return mcpScope{}, errs.Validation(errs.SubtypeMissingArgument,
			"mcp requires a domain to scope the exposed tool set").
			WithHint("pass one: `%s mcp <domain>`, narrowed further with `%s mcp <domain> <subdomain>...`; "+
				"available domains: %s", cliname.Name(), cliname.Name(), strings.Join(names, ", "))
	}
	domain := args[0]
	var dom *api.DomainSpec
	for i := range ds {
		if ds[i].Name == domain {
			dom = &ds[i]
		}
	}
	if dom == nil {
		return mcpScope{}, errs.Validation(errs.SubtypeInvalidArgument,
			"unknown domain %q", domain).
			WithHint("available domains: %s", strings.Join(names, ", "))
	}
	scope := mcpScope{domain: domain}
	if len(args) > 1 {
		valid := make(map[string]bool, len(dom.Subs))
		subNames := make([]string, 0, len(dom.Subs))
		for _, sd := range dom.Subs {
			valid[sd.Name] = true
			subNames = append(subNames, sd.Name)
		}
		subs := make(map[string]bool, len(args)-1)
		for _, s := range args[1:] {
			if !valid[s] {
				return mcpScope{}, errs.Validation(errs.SubtypeInvalidArgument,
					"unknown subdomain %q for domain %q", s, domain).
					WithHint("available subdomains of %s: %s", domain, strings.Join(subNames, ", "))
			}
			subs[s] = true
		}
		scope.subs = subs
	}
	return scope, nil
}

// mcpServerName tags the MCP server with its scope so a host that mounts several
// scoped mexc servers can tell them apart.
//
// It leads with the binary's own name, not a fixed string: a host may mount
// servers from more than one product built on this module, and two servers
// reporting the same name is exactly the ambiguity the scope suffix exists to
// remove.
func mcpServerName(args []string) string {
	if len(args) == 0 {
		return cliname.Name()
	}
	return cliname.Name() + " [" + strings.Join(args, " ") + "]"
}

// mcpToolReg is one operation resolved for MCP registration (effective auth/host
// welded in), the single source both registerTools and its test consume.
type mcpToolReg struct {
	domain, sub string
	op          api.Operation
	signed      api.Signing
	identity    api.Identity
	hostKey     string
}

// collectMCPTools walks the registry and returns the operations to expose under
// the given scope, minus write ops (read-only mode) and multipart ops.
func collectMCPTools(ds []api.DomainSpec, scope mcpScope, readOnly bool) []mcpToolReg {
	var out []mcpToolReg
	for _, d := range ds {
		if scope.domain != "" && d.Name != scope.domain {
			continue
		}
		for _, sd := range d.Subs {
			if len(scope.subs) > 0 && !scope.subs[sd.Name] {
				continue
			}
			for _, op := range sd.Ops {
				if readOnly && op.IsWrite() {
					continue
				}
				// Multipart uploads are withheld from the MCP face: a file cannot
				// transit an MCP string argument, so there is no way to carry --file
				// over the protocol yet. The CLI face is the path for uploads until
				// an MCP file convention lands. (A body-only op still transports.)
				if op.Multipart != nil {
					continue
				}
				// Binary (download) operations are likewise withheld: their response
				// is a byte stream saved to a local file, which cannot round-trip over
				// an MCP string result. Downloads are a CLI-only capability, symmetric
				// with the multipart uploads withheld above.
				if op.ResponseKind.IsBinary() {
					continue
				}
				// KindWSCollect (WS-backed) operations are withheld too: an MCP
				// tool call is a single request-response, with no protocol
				// convention for "this call streams a series of messages before
				// returning" — the model an unbounded WS subscription needs. A
				// future bounded-mode exposure (buffer up to --limit messages,
				// return them as one result) is deliberately out of scope for
				// now; the CLI face is the path for a WS operation until that
				// lands.
				if op.Kind == api.KindWSCollect {
					continue
				}
				out = append(out, mcpToolReg{
					domain: d.Name, sub: sd.Name, op: op,
					signed:   effectiveSigned(op, sd),
					identity: effectiveIdentity(op, sd),
					hostKey:  effectiveHostKey(op, sd),
				})
			}
		}
	}
	return out
}

// registerTools adds one MCP tool per in-scope operation. In a read-only binary,
// write operations are withheld entirely — the agent never sees a tool it is not
// allowed to call. The effective sign/identity are resolved at registration and
// captured by each handler closure, same weld as the CLI leaf commands.
func registerTools(server *mcp.Server, f *Factory, scope mcpScope) {
	unattended := f.unattendedNow()
	for _, r := range collectMCPTools(f.Domains, scope, f.Config().ReadOnly) {
		server.AddTool(toolFor(r.domain, r.sub, r.op, unattended),
			toolHandler(f, r.op, r.signed, r.identity, r.hostKey))
	}
}

// newMCPServer is the single call site of mcp.NewServer in this layer, shared by
// the stdio face and the server face so a capability decision is made in one
// place.
//
// caps is threaded into ServerOptions.Capabilities. A nil caps is byte-for-byte
// the previous behaviour: NewServer copies a nil *ServerOptions and a
// &ServerOptions{Capabilities: nil} into the same zero-value options, so the
// stdio face passing nil here lands on the SDK's own capability inference exactly
// as before. A non-nil caps (the server face) overrides that inference per the
// SDK's rules.
func newMCPServer(name string, caps *mcp.ServerCapabilities) *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{
		Name:    name,
		Version: buildinfo.Version,
	}, &mcp.ServerOptions{Capabilities: caps})
}

// NewToolServer builds the server face's tool surface: every non-excluded
// operation in the whole registry registered as a tool. It is what
// framework.RunMCPServer hands to a product's transport, alongside the tool index
// from ToolIndex; the product attaches whatever request middleware it needs (the
// stdio face in this module attaches none — its trust boundary is the process
// boundary).
//
// Scope is the whole registry (mcpScope{}), not a single domain. The stdio face
// takes a domain because an agent host loads the whole tool list into a context
// budget; a face reached through something that already filters per caller has no
// such pressure, and narrowing here would only hide tools that caller was
// authorized for. The tool index handed alongside is built from the same walk, so
// the two cannot disagree.
func (k *Kernel) NewToolServer(caps *mcp.ServerCapabilities) *mcp.Server {
	srv := newMCPServer(cliname.Name(), caps)
	registerTools(srv, k.Factory, mcpScope{})
	return srv
}

// ToolIndex returns a read-only map from tool name to the neutral descriptor a
// product's transport needs, built from the SAME registry walk registerTools uses
// (the whole registry, which is the scope RunMCPServer registers). A product that
// attaches middleware resolves a tool name through this so it cannot disagree with
// the surface that was actually registered about what a name means, or whether it
// is a write.
//
// It returns toolref.Ref, not api.Operation, on purpose: the shared layer owes the
// product only the name and the read/write fact, never the declaration layer's
// vocabulary — see the note in pkg/toolref.
func (k *Kernel) ToolIndex() map[string]toolref.Ref {
	return buildToolIndex(k.Factory, mcpScope{})
}

// buildToolIndex is the shared builder both ToolIndex and NewToolServer's walk
// draw from, so the index a product consults and the tools that were registered
// are computed from one call to collectMCPTools — the same scope and the same
// read-only stance.
func buildToolIndex(f *Factory, scope mcpScope) map[string]toolref.Ref {
	regs := collectMCPTools(f.Domains, scope, f.Config().ReadOnly)
	idx := make(map[string]toolref.Ref, len(regs))
	for _, r := range regs {
		name := toolName(r.domain, r.sub, r.op.Name)
		idx[name] = toolref.Ref{Name: name, Write: r.op.IsWrite()}
	}
	return idx
}

// toolFor derives the MCP tool descriptor from an Operation. The write-semantics
// annotations come straight from the Operation's hints (the single source added
// for this purpose); the SDK's annotation defaults match those hints exactly.
func toolFor(domain, sub string, op api.Operation, unattended bool) *mcp.Tool {
	destructive := op.DestructiveHint()
	return &mcp.Tool{
		Name:        toolName(domain, sub, op.Name),
		Description: op.Summary,
		InputSchema: inputSchemaFor(op, unattended),
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    !op.IsWrite(),
			DestructiveHint: &destructive,
			IdempotentHint:  op.IdempotentHint(),
		},
	}
}

// toolName maps a qualified operation id to an MCP tool name. MCP allows
// '. _ -'; we join the segments with '_' (matching the common convention) and
// leave hyphenated operation names intact (ticker-price stays ticker-price).
func toolName(domain, sub, op string) string {
	return strings.ReplaceAll(domain+"_"+sub+"_"+op, ".", "_")
}

// inputSchemaFor builds a JSON Schema object from the operation's params and, for
// a POST/PUT/PATCH op with a JSON Body, a structured `body` object. Every param
// is a string (the framework's deliberate constraint); required ones are listed
// in required[]; enums become a JSON Schema enum. A Body op adds one `body`
// property whose schema is the reflected field tree of the Body prototype — the
// agent fills a native nested object, mirroring what the CLI carries on --data.
// This mirrors exactly what the `schema` command and the CLI flags declare.
func inputSchemaFor(op api.Operation, unattended bool) json.RawMessage {
	props := map[string]any{}
	var required []string
	for _, p := range op.Params {
		prop := map[string]any{"type": "string"}
		if p.Desc != "" {
			prop["description"] = p.Desc
		}
		if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		// A param is a JSON string, so only the string-valued constraints map to
		// standard JSON Schema keywords; numeric MinVal/MaxVal stay server-side
		// (Validate) since they do not fit a string-typed schema.
		if p.MinLen > 0 {
			prop["minLength"] = p.MinLen
		}
		if p.MaxLen > 0 {
			prop["maxLength"] = p.MaxLen
		}
		if p.Pattern != "" {
			prop["pattern"] = p.Pattern
		}
		props[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}
	// A JSON Body op exposes the whole body under a single structured `body`
	// object, self-describing the field shape inline so the agent need not call
	// `schema` first. A declared Body is always required (ValidateBody refuses an
	// empty body), so `body` joins required[]. Multipart ops are withheld from the
	// MCP face entirely (collectMCPTools), so no body key is added for them.
	if op.Body != nil {
		// BodyFields describes one JSON object. For an array body that object is the
		// ITEM when the item is an object, so it is wrapped in an array node. Scalar
		// arrays publish their primitive item type directly. This keeps this face in
		// agreement with ValidateBody, which enforces the same declared shape.
		item := bodyObjectSchema(op.BodyFields())
		var body map[string]any
		if op.BodyShape() == api.BodyShapeArray {
			if typ := op.BodyItemType(); typ != "" && typ != "object" {
				item = map[string]any{"type": typ}
			}
			body = map[string]any{
				"type":        "array",
				"items":       item,
				"description": "JSON request body: an array of items",
			}
		} else {
			body = item
			body["description"] = "JSON request body"
		}
		props["body"] = body
		required = append(required, "body")
	}
	// An operation the gate can stop carries an optional two-step `confirm` flag —
	// the confirmation fallback for hosts without an elicitation dialog. It is
	// never required (the first call omits it to preview) and is ignored where the
	// host supports elicitation (a dialog is shown instead). See mcpConfirm.
	//
	// Every write, and every operation an identity COULD grade: the refusal such a
	// call receives tells the caller to come back with confirm=true, so an
	// operation that can be refused must offer the parameter. gradeable is the
	// static test because a schema is built once, at registration, with no
	// invocation to grade — see its note.
	//
	// Not published on an unattended face: the confirmation gate does not run
	// there, so the parameter would provide no safety (the caller can set it
	// itself) and would only invite the caller to read the first call's
	// confirmation_required as a failure and abandon a call that was never
	// refused.
	if (op.IsWrite() || gradeable(op)) && !unattended {
		props["confirm"] = map[string]any{
			"type":        "boolean",
			"description": "Confirmation for hosts without a confirmation dialog: omit or false to preview (the call is NOT executed, it returns confirmation_required); set true ONLY after the user has explicitly confirmed. Ignored where the host shows a dialog. Never set it on your own.",
		}
	}
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, _ := json.Marshal(schema)
	return raw
}

// bodyObjectSchema builds a JSON Schema `object` from a reflected body field
// tree: each field becomes a property, and validate:"required" fields are listed
// in required[]. Recurses through bodyFieldSchema for nested objects and arrays.
func bodyObjectSchema(fields []api.BodyField) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range fields {
		props[f.Name] = bodyFieldSchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	obj := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		obj["required"] = required
	}
	return obj
}

// bodyFieldSchema maps one reflected body field to its JSON Schema node. Objects
// recurse into their fields; an array of objects carries an `items` object schema
// (an array of scalars leaves items unconstrained, since the reflected field tree
// does not record a scalar element type — validation stays authoritative in
// ValidateBody). Scalars carry their type and optional enum.
func bodyFieldSchema(f api.BodyField) map[string]any {
	switch f.Type {
	case "object":
		s := bodyObjectSchema(f.Fields)
		if f.Desc != "" {
			s["description"] = f.Desc
		}
		return s
	case "array":
		s := map[string]any{"type": "array"}
		if f.Desc != "" {
			s["description"] = f.Desc
		}
		if len(f.Fields) > 0 {
			s["items"] = bodyObjectSchema(f.Fields)
		}
		return s
	default:
		s := map[string]any{"type": f.Type}
		if f.Desc != "" {
			s["description"] = f.Desc
		}
		if len(f.Enum) > 0 {
			s["enum"] = f.Enum
		}
		// Constraints map to the type-correct JSON Schema keywords: numeric bounds
		// for integer/number, length/pattern for string (matches ValidateBody).
		switch f.Type {
		case "integer", "number":
			if f.MinVal != nil {
				s["minimum"] = *f.MinVal
			}
			if f.MaxVal != nil {
				s["maximum"] = *f.MaxVal
			}
		case "string":
			if f.MinLen > 0 {
				s["minLength"] = f.MinLen
			}
			if f.MaxLen > 0 {
				s["maxLength"] = f.MaxLen
			}
			if f.Pattern != "" {
				s["pattern"] = f.Pattern
			}
		}
		return s
	}
}

// elicitor is the subset of *mcp.ServerSession the confirmation gate needs; an
// interface so the decision (mcpConfirm) is unit-testable without a live session.
type elicitor interface {
	Elicit(ctx context.Context, params *mcp.ElicitParams) (*mcp.ElicitResult, error)
}

// hostSupportsElicitation reports whether the connected client advertised the
// elicitation capability at initialize. A nil session (a unit test) or a client
// that did not advertise it yields false → the two-step confirm fallback.
func hostSupportsElicitation(ss *mcp.ServerSession) bool {
	if ss == nil {
		return false
	}
	ip := ss.InitializeParams()
	return ip != nil && ip.Capabilities != nil && ip.Capabilities.Elicitation != nil
}

// mcpConfirm decides whether an MCP operation may proceed — a write, or a read
// its identity graded as needing a person. With elicitation (el
// non-nil) it asks the host to show a confirmation dialog in-band and honors only
// an explicit "accept" — there is no argument the agent can forge to self-approve.
// Without it, it honors the two-step confirm flag (forgeable, so paired with the
// mexc-common skill's "do not self-add" guidance) and otherwise refuses with a
// confirmation_required error the agent relays to the user.
// It records the outcomes that end the call, and returns the channel for the one
// that does not, so the caller records an accepted judgement after the request has
// been issued (see confirmaudit.go). The channel is empty when nothing was
// confirmed.
func mcpConfirm(ctx context.Context, f *Factory, base confirmaudit.Event, el elicitor, op api.Operation, display string, confirmParam bool) (confirmaudit.Channel, error) {
	if op.RiskTier() == api.TierHeavy {
		display = "IRREVERSIBLE — this operation cannot be undone.\n" + display
	}
	if el != nil {
		res, err := el.Elicit(ctx, &mcp.ElicitParams{
			Message:         "Confirm this " + confirmNoun(op) + ":\n" + display,
			RequestedSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		})
		if err != nil {
			// The dialog broke: whether the user saw anything is unknown, and the
			// caller is not being told to relay, so there is no pair to open here.
			return "", errs.Internal("confirmation dialog failed: %v", err).WithCause(err)
		}
		if res.Action != "accept" {
			// Inline, because this judgement ends the call: no write follows, so there
			// is nothing left for the trail to delay.
			sendRecords(f, base,
				record{confirmaudit.StageRemind, confirmaudit.ChannelElicitation},
				record{confirmaudit.StageCancelled, confirmaudit.ChannelElicitation})
			return "", errs.Validation(errs.SubtypeFailedPrecondition,
				"%s %q was not confirmed (%s)", confirmNoun(op), op.Name, res.Action).
				WithHint("the operation was not executed")
		}
		// The reminder goes out with the confirmation, after the request — putting it
		// here would delay the write the user just approved (see confirmForCLI).
		return confirmaudit.ChannelElicitation, nil
	}
	if confirmParam {
		// No synthesised REMIND, as on the CLI face: its absence is the signal.
		return confirmaudit.ChannelAssumeYes, nil
	}
	// The caller is being told to relay and come back with confirm=true, so this
	// reminder is the left half of a pair completed by another call.
	sendRecords(f, base, record{confirmaudit.StageRemind, confirmaudit.ChannelRefusal})
	// No elicitation and no confirm flag: refuse with the full request (summary +
	// parameters) in the message, so the agent relays the EXACT pending write to
	// the user rather than reconstructing it from the args it sent.
	return "", errs.ConfirmationRequired("this %s needs the user's explicit confirmation before it runs:\n%s", confirmNoun(op), display).
		WithHint("show the user the operation and its parameters above, then call again with confirm=true ONLY after the user explicitly confirms — do not set confirm on your own")
}

// toolHandler returns the MCP handler for one operation. It does exactly what the
// CLI leaf command does — collect string args, build the request, send it through
// the one httpx exit — and reports the upstream JSON as text. A failure is
// returned as an error result carrying the SAME structured envelope the CLI
// writes to stderr, so the agent sees one error format across both faces.
// effSigned/effIdentity/effHostKey are the operation's effective auth+routing
// axes, resolved at registration time (registerTools).
func toolHandler(f *Factory, op api.Operation, effSigned api.Signing, effIdentity api.Identity, effHostKey string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Carried down from the confirmation gate so an accepted judgement is recorded
		// on the way out rather than on the path the user is waiting on.
		var (
			auditEv confirmaudit.Event
			auditCh confirmaudit.Channel
		)
		// Arguments arrive as a JSON object mixing string-valued params and, for a
		// Body op, a structured `body` object. Decode to raw messages first so the
		// nested body survives; params are then coerced to strings individually.
		raw := map[string]json.RawMessage{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &raw); err != nil {
				return errorResult(errs.Validation(errs.SubtypeInvalidArgument,
					"arguments must be a JSON object: %v", err).WithCause(err)), nil
			}
		}
		// Split the JSON body out of the argument map. Its raw bytes are exactly
		// what the CLI's --data flag carries, so it flows through the identical
		// ValidateBody + BuildSpec path below. Only pulled for a Body op; if a
		// non-body op receives a stray "body" key it stays in args and is caught by
		// rejectUndeclared, fail-closed.
		//
		// It does NOT flow through resolveDataInput, and must not be made to. Those
		// conventions exist because argv passes through a shell; this arrived as
		// JSON over a JSON-RPC frame, where no quoting was ever applied and the
		// value is already a document. Extending them here would turn "@path" in a
		// host's tool call into a file read on this machine, and "-" into a read of
		// the server's own stdin — which IS the transport.
		data := ""
		if op.Body != nil {
			if b, ok := raw["body"]; ok {
				data = string(b)
				delete(raw, "body")
			}
		}
		// Split the two-step `confirm` flag out, same pattern as body: it is the
		// confirmation fallback for hosts without elicitation, not a declared op
		// param, so it must not reach the string coercion below or rejectUndeclared.
		// Exactly the set inputSchemaFor publishes it for — a caller that was told to
		// send it must not then be told the argument is unknown.
		confirmParam := false
		if op.IsWrite() || gradeable(op) {
			if c, ok := raw["confirm"]; ok {
				_ = json.Unmarshal(c, &confirmParam) // best-effort bool; a non-bool leaves it false
				delete(raw, "confirm")
			}
		}
		args := make(map[string]string, len(raw))
		for k, v := range raw {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return errorResult(errs.Validation(errs.SubtypeInvalidArgument,
					"argument %q must be a string", k).WithParam(k).WithCause(err)), nil
			}
			args[k] = s
		}
		// Reject keys the operation did not declare. The CLI face can never carry
		// an undeclared argument (only declared cobra flags populate its value
		// map); the MCP face must match, or a stray/misspelled key would be folded
		// into the signed request and bounced by the upstream with an opaque error.
		if err := rejectUndeclared(op, args); err != nil {
			return errorResult(err), nil
		}
		if err := op.Validate(args); err != nil {
			return errorResult(err), nil
		}
		// A Body op deep-validates the payload against the declared prototype
		// (well-formed JSON, no unknown fields, required + enum), exactly like the
		// CLI leaf command. Skipped under --dry-run, which previews request
		// structure without requiring a filled-in body (mirrors the CLI).
		if op.Body != nil && !f.DryRun() {
			if err := op.ValidateBody(data); err != nil {
				return errorResult(err), nil
			}
		}
		// A write asks the user to confirm before it executes — skipped under
		// --dry-run (a preview sends nothing, so there is nothing to confirm), and
		// skipped entirely on an unattended face. On an unattended face there is
		// nobody to ask: the only confirmation obtainable is one the caller sets for
		// itself, so running the gate would only manufacture a false record of a
		// human's approval. Skipping it also removes the credential precheck inside
		// the block, so a write resolves credentials once, at send time — which
		// matters because that precheck assumed Resolve is local and side-effect-free,
		// a premise a server face's networked provider breaks.
		// The second reason to ask, alongside the write semantics: a grade the
		// identity supplies for this operation's permission requirement. Asked
		// outside the block, because it decides whether the block is entered.
		//
		// Not asked at all on an unattended face. The gate below does not run there,
		// so the answer would be discarded — and obtaining it is not free: an
		// identity answers a grade from its own stored state, which on a server face
		// is a keychain read or a network round trip, per tool call. Guarding it here
		// keeps this face's cost per request exactly what it was before grades
		// existed, which is the whole of what such a face should notice about them.
		var grade credential.ConfirmGrade
		if !f.unattendedNow() {
			grade = gradeFor(f, effIdentity, op)
		}
		// !f.unattendedNow() stays, grade or not: on such a face the only
		// confirmation obtainable is one the caller sets for itself, and recording
		// that as a person's approval would put a fabricated confirmation in the
		// trail. So a grade cannot be enforced there either — the same acceptance
		// the CLI path makes for --yes.
		if (op.IsWrite() || grade.Confirm) && !f.DryRun() && !f.unattendedNow() {
			// Login/credentials before the confirmation dialog: a not-logged-in write
			// fails fast with login_required rather than popping an elicitation dialog
			// the user accepts only to hit login_required at send (see the CLI path).
			// Kept to the write, for the reason the CLI path records.
			if op.IsWrite() {
				if err := precheckWriteCredentials(f, effIdentity); err != nil {
					return errorResult(err), nil
				}
			}
			var el elicitor
			if hostSupportsElicitation(req.Session) {
				el = req.Session
			}
			// Per tool call, not per process: this face serves many judgements from
			// one process and Event.InvocationID must distinguish them.
			auditEv = auditEvent(f, op, args, data, "", faceMCP, grade)
			ch, cerr := mcpConfirm(ctx, f, auditEv, el, op,
				gradedDisplay(op, args, data, grade), confirmParam)
			if cerr != nil {
				return errorResult(cerr), nil
			}
			auditCh = ch
			// On the way out, so the records land after the request has been issued and
			// after this call's result has been built — never on the path the user is
			// waiting on. Sent whatever the upstream did with the request: the stages
			// attest what the user was asked and answered, which is true either way.
			defer func() { reportConfirmed(f, auditEv, auditCh) }()
		}
		// No declare-ahead gate here: login-identity / body ops fall through to
		// BuildSpec + Send exactly like the CLI face, so a login-identity op surfaces
		// the provider's login_required (not a stale "not supported yet"), and a body
		// op actually transports its JSON. Credential resolution and body transport happen
		// inside Send.
		spec, err := op.BuildSpec(args, data, nil, effSigned, effIdentity, effHostKey)
		if err != nil {
			return errorResult(err), nil
		}
		var dst any
		client := f.Client(ctx)
		plan, err := client.Send(ctx, spec, &dst)
		if err != nil {
			return errorResult(err), nil
		}
		// In --dry-run the request is never sent and dst is never written; report
		// the plan (what would be sent), matching the CLI's dry-run output, instead
		// of marshaling a nil dst into the literal "null".
		if client.DryRun {
			body, _ := json.MarshalIndent(plan, "", "  ")
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil
		}
		if op.Transform != nil {
			shaped, terr := op.Transform(dst)
			if terr != nil {
				return errorResult(errs.Internal("output transform failed: %v", terr).WithCause(terr)), nil
			}
			dst = shaped
		}
		body, err := json.MarshalIndent(dst, "", "  ")
		if err != nil {
			return errorResult(errs.Internal("failed to encode response: %v", err).WithCause(err)), nil
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil
	}
}

// rejectUndeclared fails if args carries a key the operation did not declare,
// keeping the MCP face's accepted input identical to the CLI's. The error names
// the offending key and points at the schema command, so the agent can correct
// itself rather than getting an opaque upstream signature error.
func rejectUndeclared(op api.Operation, args map[string]string) error {
	declared := make(map[string]bool, len(op.Params))
	for _, p := range op.Params {
		declared[p.Name] = true
	}
	for k := range args {
		if !declared[k] {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"unknown parameter %q for this operation", k).
				WithParam(k).
				WithHint("call `schema` for this operation to see the parameters it accepts")
		}
	}
	return nil
}

// errorResult wraps an error as an MCP error result. The text is the same JSON
// envelope (type/subtype/param/hint) the CLI emits to stderr, produced by the
// one output.WriteError path, so there is a single error format for both faces.
// IsError=true lets the model see the failure and self-correct (it is not raised
// as a protocol-level error, per the SDK's guidance).
func errorResult(err error) *mcp.CallToolResult {
	var buf bytes.Buffer
	output.WriteError(&buf, err)
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: buf.String()}},
	}
}
