// Package framework is the composition-root entry point for a product built on
// this module.
//
// A product (an "edition") declares what it injects via Edition and calls Run.
// framework depends on internal/cli (the shared assembly + runtime) and on
// the pkg/ ports, but never on any concrete domain, provider, or host — those
// are the edition's to supply. This is the one package a product main imports.
//
// This file holds the injection contract (Deps, Edition, BuiltinCommand) and the
// small helpers an edition builds against (Group, Version); run.go holds the
// assembly + entry points (Run / RunForTest).
package framework

import (
	"context"
	"embed"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mexcdevelop/mexc-cli/internal/cli"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// Deps is the runtime contract an edition-specific command (profile / auth /
// status) is written against. It is exported here — not in internal/cli —
// because an edition lives in its own module and cannot import internal
// packages; *cli.DepsAdapter satisfies it structurally. It exposes only what
// a command needs (config, IO, structured output), never the engine internals.
type Deps interface {
	Config() config.Config
	DryRun() bool
	AssumeYes() bool
	// Client returns a transport Sender already configured with this invocation's
	// flags (dry-run / no-retry / env / base-url). An edition command sends through
	// this port, so it never imports the concrete internal/transport client.
	//
	// It carries no caller. On a face that binds credentials per caller that means
	// it resolves NOTHING (see credential.Binder's fail-closed rule), which is the
	// right answer for what this method is for: work that belongs to the invocation
	// itself — reporting an audit record, answering a probe — authenticates as the
	// product, not as somebody's caller.
	Client() transport.Sender
	// ClientFor returns a Sender for work done ON BEHALF OF the caller that ctx
	// carries. An edition command serving a remote caller uses this one; a locally
	// invoked command has no caller to speak for and uses Client.
	ClientFor(ctx context.Context) transport.Sender
	Out() io.Writer
	Err() io.Writer
	WriteJSON(v any) error
	WriteError(err error)
}

// Version reports the build string `--version` prints for the named
// edition (e.g. "dev [public] (commit …, built …)"). An edition builtin that
// reports the running build — `doctor` does — uses it instead of reaching into
// the engine's ldflags-injected version internals.
func Version(name string) string { return cli.VersionString(name) }

// BuiltinCommand builds an edition-specific command against the runtime Deps.
// framework.Run mounts each one after the engine assembles the shared tree.
type BuiltinCommand func(Deps) *cobra.Command

// Group builds an intermediate (group) command carrying the shared
// unknown-subcommand guard, for an edition builtin that owns subcommands
// (e.g. profile, auth). It is the composition-root-facing counterpart of the
// engine's internal group mounting: an edition package cannot reach that
// unexported helper, so it assembles its groups through here and they behave
// identically (a direct or unknown invocation yields a structured envelope +
// non-zero exit, never cobra's "print help, exit 0").
func Group(use, short string, d Deps) *cobra.Command {
	return cli.NewGroup(use, short, d.WriteError)
}

// Edition is a product's injection set — everything that distinguishes one
// product (public / internal) from another. framework.Run registers the adapters
// (signers / identities / host rows) and mounts the domains and builtins; the
// shared faces (schema / mcp / explore / skills) are added by the engine.
// Config is mounted when the product has settings. npm update behavior belongs
// to the generated JavaScript launcher and is not part of the Go framework.
type Edition struct {
	Name string // variant label, surfaced in --version
	// Short is this product's one-line root description, shown by --help. Empty
	// uses the shared default. It is the edition's to supply because only the
	// product knows how it wants to describe itself — the shared layer must not
	// carry a per-product string, which would publish one product's identity to
	// every other.
	Short string
	// StateDir is the directory under $HOME where this product keeps its state —
	// settings, profiles, session stores, exported skill docs. Empty keeps the
	// default: the plain .mexc directory named in pkg/config/statedir.go.
	//
	// Every product built on this module installs its binary under the same name,
	// so sharing one directory means two products overwriting each other's state.
	// Nothing kept there is genuinely shared: `env`'s legal values are exactly the
	// envs a product's own hosts serve (disjoint between products, so one
	// product's value breaks the other's host resolution), and profiles, sessions
	// and skill state are per-product by construction. A product that must not
	// collide names its own directory here.
	StateDir string
	// DefaultEnv is the env used when the caller selects none — a product's
	// production environment. Naming it here means "no --env" and that env are one
	// value with one spelling, instead of the blank being a second name for it.
	// Empty leaves the blank unresolved, and then a host must register a row for
	// it explicitly.
	DefaultEnv string
	// GlobalFlags registers the persistent (global) flags only THIS product needs
	// — a credential selector, for instance. It runs after the shared flags, so a
	// product cannot silently shadow one. Nil for a product that adds none.
	//
	// It belongs to the edition because a flag the product cannot honour is worse
	// than a missing one: while credential selection lived in the shared layer,
	// every product carried the flag, including one with no such credentials and
	// no command to manage them.
	GlobalFlags func(*pflag.FlagSet)
	Domains     []api.DomainSpec      // business topology to mount + derive faces from
	Signers     []signer.Signer       // signing schemes to register
	Identities  []credential.Provider // identity providers to register
	HostRows    []hosts.Registration  // host-table rows to register
	Builtins    []BuiltinCommand      // edition-specific commands (profile / auth / status)
	SkillsFS    []embed.FS            // this edition's embedded SKILL.md set

	// ConfirmReporter supplies this product's write-confirmation audit sink (see
	// pkg/confirmaudit). A factory over Deps because a reporter sends through the
	// invocation's transport Sender, which does not exist until assembly;
	// framework resolves it once, on the first report.
	//
	// It belongs to the edition because only the product knows the endpoint, how
	// the report authenticates, and who the acting operator is.
	//
	// REQUIRED for a product that mounts writes: a missing sink and a working one
	// produce identical output, so assembly refuses rather than shipping a gate
	// that leaves no trace. Opting out is confirmaudit.Discard{}.
	ConfirmReporter func(Deps) confirmaudit.Reporter

	// WSCollector runs a WS-backed operation's subscription. Nil unless the
	// product mounts one: leaving it nil is what keeps the WS client out of the
	// build. Required when it does — assembly refuses otherwise.
	WSCollector func(Deps) transport.WSCollector

	// BuildVersionHeader names a header every request and WS handshake carries,
	// valued with this binary's build version. Empty means none.
	//
	// The name is the product's — its contract with its own gateway, and the
	// products answer to different ones. The value is not: the engine reads it
	// from pkg/buildinfo, so a product cannot report a version it was not stamped
	// with. Same split as StateDir, which the product names and pkg/config
	// resolves. A name and not a header map: business headers belong in an
	// operation's Params, where the mount-time collision check can see them.
	BuildVersionHeader string

	// RateLimit is this product's client-side throttle: a coarse token bucket the
	// transport applies before sending (see transport.RateLimitPolicy). It exists
	// to spare the gateway obviously-runaway traffic, not to mirror its real quota;
	// the backend stays the authority.
	//
	// NIL MEANS THE SHARED DEFAULT, not "no throttle". A face that genuinely wants
	// none says so with transport.Unlimited, and the difference is the whole point:
	// "I forgot to state a policy" and "I want no ceiling at all" must not be
	// spelled the same way, because one of them is a decision and the other is an
	// omission that earns an upstream IP ban.
	//
	// It is not a per-caller quota. A face serving many callers from one process
	// shares this budget across all of them, so it protects the BACKEND from this
	// process, not the callers from each other — per-caller quota is a separate
	// mechanism and does not belong on an Edition field.
	//
	// The VALUE is the product's decision; reading it from the environment is the
	// product's too. The shared layer must not, because a user of a locally invoked
	// product who could raise their own ceiling would simply earn that ban.
	RateLimit *transport.RateLimitPolicy

	// The three fields below configure a long-lived server FACE — one that serves
	// tools to remote callers rather than to a person at a terminal. Each is
	// optional, and for each of them injecting it IS the whole of the choice:
	// there is no capability to probe at run time and no type assertion to satisfy,
	// so a build that leaves one unset simply does not have that behaviour, and one
	// that sets it always does. That is what keeps every locally invoked build
	// byte-for-byte unchanged by their mere existence.

	// IdentityBinder supplies the per-request credential lookup for a face whose
	// callers differ from one another.
	//
	// Nil keeps the process-wide registry — which is what a local, one-account
	// product wants, and what every existing product gets without changing a line
	// (credential.Registry is the zero-value Lookup). Set, it is consulted on every
	// request, so the credentials a request is made with can depend on who asked.
	//
	// A factory over Deps for the same reason ConfirmReporter is one: the binder
	// reaches a configuration source through this invocation's Sender, which does
	// not exist until assembly.
	IdentityBinder func(Deps) credential.Binder
	// Unattended states that this face serves callers with no person behind them.
	//
	// The zero value — false — means the opposite, a person is reachable, which is
	// what every locally invoked product is; so no existing edition changes
	// behaviour. What it turns off is the write-confirmation gate, entirely: with
	// nobody to ask, the only confirmation obtainable is one the caller sets for
	// itself, and recording that as a human's approval would put a fabricated
	// confirmation in the audit trail. It is a fact rather than a port because the
	// only thing a product expresses here is one bit; see the pairing check in
	// RunMCPServer for why it must travel with a Discard confirmation sink.
	//
	// The gate it turns off is the ONLY place a credential.ConfirmGrade is
	// enforced, so an identity that grades operations (credential.ConfirmGrader)
	// grades nothing here: those operations run unconfirmed and unrecorded, like
	// every write on this face. That follows from the decision above rather than
	// contradicting it — a face with nobody to ask cannot ask, whichever reason
	// asked it to — but it is worth stating, because the grade is set OUTSIDE the
	// product, by whoever governs the identity, and they will not learn from this
	// face that their grade does not apply to it. A product mounting a grading
	// identity here should scope which operations it serves accordingly.
	Unattended bool
	// MCPCapabilities is the capability set the MCP server advertises at
	// initialize.
	//
	// Nil leaves the SDK's own inference in place, which is what a face that states
	// nothing wants and what keeps the local stdio face's initialize response
	// unchanged. A non-nil value is authored by the product, and the product must
	// list EVERY capability it wants: the SDK still fills a nil SUB-field (Tools /
	// Prompts / Resources) from what is registered, so — for one example — a face
	// that means "tools.listChanged = false" must set a non-nil
	// &mcp.ToolCapabilities{ListChanged: false}, not leave the field nil.
	MCPCapabilities *mcp.ServerCapabilities
}
