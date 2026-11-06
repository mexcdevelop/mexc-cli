package framework

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mexcdevelop/mexc-cli/internal/output"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/toolref"
)

// ServeFunc is a product's transport for the tool surface: it receives a server
// with every tool already registered, the neutral tool index alongside it, and
// owns everything about how that server is reached — including any request
// middleware it wants to attach (srv.AddReceivingMiddleware is a public SDK
// method) and draining that middleware before it returns.
//
// The split is where it is for one reason. Deriving the tool surface needs the
// operation registry, which only this module holds, so it cannot be a product's
// job. Listening on a port needs an authentication story, which only a product can
// decide, so it cannot be this module's — and a shared layer that shipped a
// listener without one would hand every consumer a process that accepts requests
// from anywhere while holding real credentials. stdio has no such gap (the process
// boundary IS the trust boundary), which is why the stdio face stays in this
// module and this one does not.
//
// The tools argument is the reason a product can attach middleware at all: shaping
// or auditing needs to turn a tool name into its read/write semantics, and the
// Kernel that knows that lives in an internal package a product (Invariant 10)
// cannot reach. So this module resolves the index and hands it over. It is the
// neutral toolref.Ref — a name and a write bit — never the declaration; the ports
// that consume it live in the product, not here.
type ServeFunc func(ctx context.Context, srv *mcp.Server, tools map[string]toolref.Ref) error

// RunMCPServer assembles ed and serves its whole tool surface through serve,
// returning the process exit code.
//
// It is a sibling of Run, not a mode of it. Run builds a command tree for a person
// and offers the MCP face as one subcommand of it; this builds the tool surface
// and nothing else — no command tree, no global flags, no help or settings face. A
// process answering remote callers while holding delegated credentials has no use
// for a shell, and every command it does not mount is one less thing reachable
// inside its container. Configuration comes from the environment, not argv,
// because at run time there is no argv author.
func RunMCPServer(ed Edition, serve ServeFunc) int {
	return runMCPServer(ed, serve, os.Getenv, os.Stdout, os.Stderr)
}

// runMCPServer is RunMCPServer with its ambient inputs passed in, so a test can
// drive it without touching the real environment or process streams.
func runMCPServer(ed Edition, serve ServeFunc, env func(string) string, out, errW io.Writer) int {
	// Before anything builds: an unattended build that turns the confirmation gate
	// off must also state, explicitly, that it discards confirmation audit. A
	// half-taken decision is exactly what fills a trail with records of
	// confirmations that never happened.
	if err := validateUnattendedPairing(ed); err != nil {
		output.WriteError(errW, err)
		return errs.ExitCode(err)
	}
	// Shares assemble's front half with Run so the two cannot drift: it registers
	// adapters, pins the outbound rate before the first client is built, validates
	// the registry and host table into the assembly error, and sets Unattended +
	// the confirm reporter.
	app := assemble(ed, env, out, errW)
	// Per-request identity takes effect here. Wire the binder only when the Edition
	// supplied one; leaving it unwired keeps every client's Identities nil, so the
	// transport falls back to the process registry — the byte-for-byte current
	// behaviour. Deps-built and resolved once, matching the confirm reporter.
	if ed.IdentityBinder != nil {
		app.SetIdentityBinder(sync.OnceValue(func() credential.Binder {
			return ed.IdentityBinder(app.Deps())
		}))
	}
	if err := app.AssemblyErr(); err != nil {
		output.WriteError(errW, err)
		return errs.ExitCode(err)
	}
	// Build the tool surface and derive the neutral index from the same registry
	// walk. Both are handed to the product's transport: the server to reach, and
	// the index so any middleware the product attaches can resolve a tool name to
	// its read/write semantics. No middleware is attached here — the shared engine
	// serving a local request never shapes or audits, and a server face's shaping
	// and auditing are the product's, hung on the server it receives.
	srv := app.Kernel().NewToolServer(serverCapabilities(ed))
	tools := app.Kernel().ToolIndex()

	// SIGINT / SIGTERM: serve returns when the context is done (or the transport
	// closes). Anything the product buffered — an audit queue, say — is the
	// product's to drain before its serve returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := serve(ctx, srv, tools)
	return exitForServe(err)
}

// serverCapabilities is what the server advertises at initialize: the Edition's
// value verbatim.
//
// nil leaves the SDK's own inference in place, which keeps the local stdio face
// unchanged. A non-nil value is the product's authored set, and it must list every
// capability wanted — a nil sub-field is still filled from what is registered. In
// particular tools.listChanged is BOTH a declaration and the send-side switch the
// SDK reads before emitting a list-changed notification, so leaving it nil keeps
// notifications on; a face that means "off" must set a non-nil
// &mcp.ToolCapabilities{ListChanged: false} rather than omit it.
func serverCapabilities(ed Edition) *mcp.ServerCapabilities { return ed.MCPCapabilities }

// validateUnattendedPairing refuses a build that turns the confirmation gate off
// while declaring no confirmation-audit decision. The two are one decision, and
// this keeps it from being half-taken.
//
// It checks only that a reporter was declared; that the resolved sink is actually
// confirmaudit.Discard is asserted where the sink is built (App.SetConfirmReporter),
// because the concrete type is Deps-built and unknown until then.
func validateUnattendedPairing(ed Edition) error {
	if !ed.Unattended {
		return nil
	}
	if ed.ConfirmReporter == nil {
		return errs.Internal("an unattended build must state its confirmation-audit decision explicitly").
			WithHint("set Edition.ConfirmReporter to a factory returning confirmaudit.Discard{}")
	}
	return nil
}

// exitForServe maps the transport's return to a process exit code. A server that
// stops because its context was cancelled (SIGINT / SIGTERM) or its transport
// closed cleanly ended normally, not in error.
func exitForServe(err error) int {
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	return errs.ExitCode(err)
}
