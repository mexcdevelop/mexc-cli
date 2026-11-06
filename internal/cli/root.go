// Package cli assembles a product's command tree.
//
// Command shape: <cli> <domain> <subdomain> <operation>.
// This layer mounts the api.Operation
// values declared by each subdomain into cobra commands, binds flags, and handles
// the unified output finalization; it holds no business logic.
package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mexcdevelop/mexc-cli/internal/fileio"
	"github.com/mexcdevelop/mexc-cli/internal/output"
	"github.com/mexcdevelop/mexc-cli/internal/ratelimit"
	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
	xport "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// The build metadata this package reads (the --version string, the MCP server
// version, the skills staleness check) deliberately does NOT live here: it is in
// pkg/buildinfo, which each product stamps at link time.
//
// Why it moved: -ldflags -X resolves a symbol by import path against the symbol
// table, ignoring Go visibility — so a consumer in another module can stamp an
// unexported variable in this package, but gets NO error if the path stops
// resolving. Renaming this package would then silently produce release binaries
// reporting "dev / none / unknown". pkg/buildinfo is the stable exported path
// that cannot break that way; see its doc comment.

// versionString is what `--version` prints. It names the build variant
// (public / internal) so a binary's provenance is unambiguous even when both
// products are invoked under the same name.
func versionString(name string) string {
	return buildinfo.Version + " [" + name + "] (commit " + buildinfo.Commit +
		", built " + buildinfo.Date + ")"
}

// VersionString is the exported form of versionString: it lets a composition
// root / edition (e.g. the internal `status` command) report the same build
// string `--version` prints, without reaching into engine internals for the
// ldflags-injected version/commit/date.
func VersionString(name string) string { return versionString(name) }

// defaultRootShort is the root command's one-line description when the edition
// does not supply its own.
//
// The shared layer deliberately does NOT know the set of products or annotate
// per product: it used to special-case one variant name and append a suffix
// describing that variant, which put a product's self-description — and the fact
// that the product exists — inside a package published with every edition. An
// edition that wants a different line supplies it via Edition.Short.
const defaultRootShort = "Unified command-line entry point for MEXC capabilities (for AI agents)"

// rootShort returns the edition's own description, or the default when it gave
// none.
func rootShort(short string) string {
	if short == "" {
		return defaultRootShort
	}
	return short
}

// globalFlags collects the global switches; a single instance lives on the root
// command, so there is no shared-state problem.
type globalFlags struct {
	baseURL   string
	readOnly  bool
	dryRun    bool
	noRetry   bool
	assumeYes bool   // --yes: the user has explicitly confirmed a write (skips the confirmation prompt)
	env       string // --env: deployment env for this invocation; overrides MEXC_ENV and the persisted global env
	// envAxis is whether this product serves more than one env. False leaves --env
	// unregistered and short-circuits the precedence chain to the single default.
	envAxis bool
}

// Env abstracts environment-variable reads to ease test injection. Defaults to os.Getenv.
type Env func(key string) string

// Factory injects runtime dependencies into the command tree: config, HTTP
// client, and IO streams. The IO fields are io.Writer rather than *os.File so
// commands can attach a bytes.Buffer in unit tests and assert on the output.
type Factory struct {
	Config func() config.Config
	// Client returns the transport client for ONE request.
	//
	// It takes a context because a face that serves many callers binds this
	// request's credentials while the client is being built (see
	// credential.Binder), and the caller's identity lives in the context. Passing
	// one is free at every existing call site: a CLI leaf has cmd.Context(), and an
	// MCP tool handler is handed one.
	//
	// This is an internal signature. An edition cannot reach this package
	// (Invariant 10), so widening it breaks nothing outside this module.
	Client    func(context.Context) *transport.Client
	DryRun    func() bool // whether --dry-run is set; execution-time gates skip under it
	AssumeYes func() bool // whether --yes is set; a write treats it as the user's explicit confirmation
	// Unattended reports whether this face serves callers with no person behind
	// them. Nil means false — a person is reachable — which is what every locally
	// invoked build is, so the write-confirmation gate runs exactly as before. A
	// server face sets it true (via App.SetUnattended), which skips that gate
	// entirely. Read through unattendedNow so nil is safe at every call site.
	Unattended func() bool
	Out        io.Writer // stdout: data
	Err        io.Writer // stderr: errors / hints
	// Domains is the business topology this build assembles and derives its faces
	// (schema / mcp / explore) from. It is injected here rather than read from a
	// global so a composition root (an edition) is the single source of the domain
	// set — the faces read f.Domains, never a package-level registry.
	Domains []api.DomainSpec
	// SkillsFS is this build's embedded SKILL.md set, injected by the composition
	// root (framework.Run bridges Edition.SkillsFS here). The `skills` command
	// reads it rather than a package-level global, so each edition serves exactly
	// its own docs.
	SkillsFS []fs.FS
	// BuildVersionHeader is bridged from Edition.BuildVersionHeader; empty means
	// none. A field rather than a package global so one build's value cannot leak
	// into the next test's.
	BuildVersionHeader string
	// RateLimit is bridged from Edition.RateLimit; nil means no client-side
	// throttle. The shared limiter is built from it once per invocation.
	RateLimit *xport.RateLimitPolicy
	// Setting answers, for one global setting key, the value in effect this
	// invocation and the layer that supplied it (see the Source* constants).
	//
	// It is a Factory field rather than something the `config` command works out
	// for itself because only the engine holds the precedence chain — the flag set
	// and this invocation's environment reader are here, not there. When `config`
	// answered from the settings file alone it reported the wrong value with
	// complete confidence: `MEXC_ENV=testnet config get env` said "prod",
	// source "default", while the very next request went to testnet.
	//
	// An unknown key returns the same structured error `config get` would raise.
	Setting func(key string) (value, source string, err error)
	// ConfirmReporter resolves the write-confirmation audit sink on first use. A
	// thunk, because the sink is built from the composition root's Deps, which do
	// not exist while this Factory is constructed. Nil means none was injected;
	// SetConfirmReporter refuses that for a build with writes.
	ConfirmReporter func() confirmaudit.Reporter
	// WSCollector resolves the WS subscription runner on first use, like
	// ConfirmReporter. Nil unless the product mounts a WS operation.
	WSCollector func() xport.WSCollector
	// IdentityBinder resolves the per-request credential Lookup on first use, like
	// ConfirmReporter. Nil — no server face configured one — leaves every client's
	// Identities unset, so the transport falls back to the process registry: the
	// byte-for-byte previous behaviour, and the reason this whole mechanism is
	// invisible to the local CLI. Set, it is consulted while each client is built,
	// so a request's credentials depend on the caller its context carries.
	IdentityBinder func() credential.Binder
	// Env reads this invocation's environment, so the calling harness's markers
	// (for the audit record) come through the same injectable seam as every other
	// ambient input.
	Env Env
}

// App holds a once-assembled command tree and its recorded exit code, driving
// both Execute and the tests.
type App struct {
	root *cobra.Command
	fe   *finalErr
	errW io.Writer
	f    *Factory
	ae   *assemblyErr
}

// finalErr records the exit code when a subcommand fails, for Run to read.
type finalErr struct{ exitCode int }

// assemblyErr holds the declaration/wiring error this build must fail every
// invocation with, if any. A holder rather than a value because one check —
// whether an audit sink was injected — is only knowable after NewApp returns.
type assemblyErr struct{ err error }

// syncWriter serializes writes to one stream.
//
// stderr needs it because the MCP face serves tool calls concurrently and the
// transport narrates advisories (a host override, an RBAC preflight) from wherever
// it is called — so two calls can reach this writer at once. os.Stderr survives
// that by luck, one Write being one syscall; a caller that passes a buffer, as
// every test does, does not.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// AddCommand mounts an edition-specific command onto the assembled root. The
// composition root (framework.Run) uses it to add the edition's builtins after
// the shared tree is built.
func (a *App) AddCommand(c *cobra.Command) { a.root.AddCommand(c) }

// Deps returns the runtime services an edition-specific command needs, backed by
// this app's Factory and exit-code recorder. *DepsAdapter satisfies
// framework.Deps structurally, so the framework can hand it to an edition builtin
// without engine importing framework (no cycle).
func (a *App) Deps() *DepsAdapter { return &DepsAdapter{f: a.f, fe: a.fe} }

// SetConfirmReporter injects the write-confirmation audit sink and validates that
// a build mounting writes has one. The composition root calls it after assembly,
// because the sink is built from Deps — which need this App. nil means the
// product declared none.
//
// When this build is unattended, it also wraps the reporter thunk in an
// assertion: an unattended face must pair with the explicit no-op sink
// (confirmaudit.Discard), because keeping a real confirmation sink alive on a
// face that skips the confirmation gate is how a trail acquires records of
// confirmations that never happened. The assertion is on first resolution rather
// than eager, so it does not force a Deps-built sink to construct before assembly
// finishes; on an unattended face the gate is skipped, so this is defence in depth
// behind RunMCPServer's validateUnattendedPairing, which is the primary check.
func (a *App) SetConfirmReporter(rep func() confirmaudit.Reporter) {
	if rep != nil && a.unattendedNow() {
		inner := rep
		rep = func() confirmaudit.Reporter {
			r := inner()
			if _, ok := r.(confirmaudit.Discard); !ok && a.ae.err == nil {
				a.ae.err = errs.Internal("an unattended build resolved a non-Discard confirmation sink").
					WithHint("an unattended Edition must set ConfirmReporter to a factory returning confirmaudit.Discard{}")
			}
			return r
		}
	}
	a.f.ConfirmReporter = rep
	if a.ae.err == nil {
		a.ae.err = validateConfirmReporter(a.f.Domains, rep != nil)
	}
}

// SetBuildVersionHeader names the header carrying this binary's build version,
// and rejects a name an operation already declares as a header param. Same
// timing as the setters around it; an empty name is legitimate and validates.
func (a *App) SetBuildVersionHeader(name string) {
	a.f.BuildVersionHeader = name
	if a.ae.err == nil {
		a.ae.err = validateBuildVersionHeader(a.f.Domains, name)
	}
}

// SetRateLimit records the product's client-side throttle policy. Nil means the
// shared default, NOT no throttle — a product that wants none says
// transport.Unlimited. Nothing to validate here: the limiter is built from it
// later, once, in the Client factory.
func (a *App) SetRateLimit(p *xport.RateLimitPolicy) { a.f.RateLimit = p }

// SetUnattended records whether this build serves callers with no person behind
// them. The composition root calls it during assembly, before SetConfirmReporter,
// so the confirmation-sink assertion above can see it. Same setter shape as the
// other post-assembly injections, which is what keeps NewApp's signature from
// widening for a per-build fact.
func (a *App) SetUnattended(v bool) { a.f.Unattended = func() bool { return v } }

// SetIdentityBinder injects the per-request credential binder, resolved on first
// use. The composition root calls it after assembly only when the Edition
// supplied one; leaving it unset keeps c.Identities nil on every client and so
// the process registry in effect. Same setter shape as SetConfirmReporter /
// SetWSCollector, which is why NewApp's nine-parameter signature does not widen
// for it.
func (a *App) SetIdentityBinder(b func() credential.Binder) { a.f.IdentityBinder = b }

// unattendedNow reads the build's unattended fact, nil-safe: an unset field means
// attended (a person is reachable), the state of every locally invoked build.
func (a *App) unattendedNow() bool { return a.f.unattendedNow() }

// unattendedNow is the nil-safe read of the unattended fact for the engine's own
// gates. Nil (no server face configured it) is attended.
func (f *Factory) unattendedNow() bool { return f.Unattended != nil && f.Unattended() }

// AssemblyErr returns the declaration/wiring error this build must fail with, if
// any. The CLI path surfaces it in PersistentPreRunE; RunMCPServer, which mounts
// no command tree, reads it here before it begins serving.
func (a *App) AssemblyErr() error { return a.ae.err }

// SetWSCollector injects the WS subscription runner and validates that a build
// mounting a WS-backed operation has one. Same timing as SetConfirmReporter.
func (a *App) SetWSCollector(c func() xport.WSCollector) {
	a.f.WSCollector = c
	if a.ae.err == nil {
		a.ae.err = validateWSCollector(a.f.Domains, c != nil)
	}
}

// validateWSCollector refuses a build that mounts a WS-backed operation with no
// runner, which would otherwise be a nil call mid-execution. The reverse (a
// collector with no WS operation) is the product's own call, not an error.
func validateWSCollector(ds []api.DomainSpec, present bool) error {
	if present || !hasWSOp(ds) {
		return nil
	}
	return errs.Internal("this build mounts a WS-backed operation but declares no WS collector").
		WithHint("set Edition.WSCollector; the engine cannot supply one without importing a WebSocket client into every product")
}

func hasWSOp(ds []api.DomainSpec) bool {
	for _, d := range ds {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				if op.Kind == api.KindWSCollect {
					return true
				}
			}
		}
	}
	return false
}

// DepsAdapter adapts the engine's internal Factory + finalErr to the small,
// exported surface an edition command uses. It exists so edition commands, which
// live in their own module and cannot import internal/cli, can be written
// against framework.Deps (which *DepsAdapter satisfies).
type DepsAdapter struct {
	f  *Factory
	fe *finalErr
}

func (d *DepsAdapter) Config() config.Config { return d.f.Config() }
func (d *DepsAdapter) DryRun() bool          { return d.f.DryRun() }
func (d *DepsAdapter) AssumeYes() bool       { return d.f.AssumeYes() }

// Client returns a Sender for work that belongs to the INVOCATION rather than to
// any one caller — an audit report, a health probe.
//
// It carries no caller, so on a face that binds credentials per caller it resolves
// nothing (see credential.Binder's fail-closed rule). That is correct rather than
// a limitation: such work authenticates as the product, not as somebody's caller.
func (d *DepsAdapter) Client() xport.Sender { return d.f.Client(context.Background()) }

// ClientFor returns a Sender for work done ON BEHALF OF the caller ctx carries. An
// edition command that serves a remote caller uses this one.
func (d *DepsAdapter) ClientFor(ctx context.Context) xport.Sender { return d.f.Client(ctx) }

func (d *DepsAdapter) Out() io.Writer        { return d.f.Out }
func (d *DepsAdapter) Err() io.Writer        { return d.f.Err }
func (d *DepsAdapter) WriteJSON(v any) error { return output.JSON(d.f.Out, v) }
func (d *DepsAdapter) WriteError(err error)  { writeAndRecord(d.f, d.fe, err) }

// SetRootName overrides the root command's Use, so a binary invoked under a
// renamed/symlinked argv[0] shows matching command paths in --help. The default
// stays whatever cliname holds (its own default, which is what the unit tests
// see); the entry point (framework.Run) sets both from argv[0].
func (a *App) SetRootName(name string) { a.root.Use = name }

// Kernel is the bundle of engine services a SHARED subsystem package (internal/
// faces, internal/builtin) — which lives outside package engine and therefore
// cannot reach (f, fe) directly — builds its commands against. It embeds the
// Factory (so Config/Client/DryRun/Out/Err/Domains/SkillsFS promote) and adds the
// error/JSON writing, group building, and build version those constructors used
// to reach in-package. framework.Run hands each shared command an App.Kernel().
type Kernel struct {
	*Factory
	fe *finalErr
}

// WriteError writes the error envelope to stderr and records the exit code.
func (k *Kernel) WriteError(err error) { writeAndRecord(k.Factory, k.fe, err) }

// WriteJSON writes v as the command's data output (stdout).
func (k *Kernel) WriteJSON(v any) error { return output.JSON(k.Out, v) }

// Group builds an intermediate (group) command with the shared unknown-subcommand
// guard, recording exit codes via this kernel.
func (k *Kernel) Group(use, short string) *cobra.Command { return NewGroup(use, short, k.WriteError) }

// Version reports the ldflags-injected build version (bare, without the [variant]
// suffix VersionString adds) — for the mcp server info and `skills` state.
func (k *Kernel) Version() string { return buildinfo.Version }

// Kernel returns this app's engine-services bundle, for framework.Run to hand to
// the shared face / builtin command constructors.
func (a *App) Kernel() *Kernel { return &Kernel{Factory: a.f, fe: a.fe} }

// NewApp assembles the command tree for one product: name is the variant label
// (surfaced in --version), short is the edition's own one-line root description
// (empty for the default), ds is the domain set to mount and derive faces from,
// and edFlags (may be nil) lets the edition register the persistent flags
// only it needs. env / out / err are injectable to ease testing.
//
// envAxis says whether this product serves more than one env. When it does not,
// --env is not registered and MEXC_ENV is not read: there is exactly one
// deployment, so a selector could only ever re-select it, and honouring the
// variable would let a value exported for ANOTHER product break every command
// here with "host has no endpoint for env …".
func NewApp(name, short string, ds []api.DomainSpec, skillsFS []fs.FS, edFlags func(*pflag.FlagSet), envAxis bool, env Env, out, errW io.Writer) *App {
	gf := &globalFlags{envAxis: envAxis}
	fe := &finalErr{}
	// Wrapped once, here, so every consumer below shares one serialized stream. The
	// reason is the MCP face's concurrent tool calls — see syncWriter.
	errW = &syncWriter{w: errW}

	root := &cobra.Command{
		// The default; framework.Run replaces it (and cliname) from argv[0] before
		// the tree is used, so a product's own binary name reaches every command
		// path. Reading it from cliname rather than repeating the literal keeps one
		// definition of "what this binary is called by default".
		Use:           cliname.Name(),
		Short:         rootShort(short),
		Version:       versionString(name),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errW)

	// Route credential-layer advisories (e.g. the RBAC preflight heads-up) to
	// this invocation's stderr, alongside every other hint. stdout stays data-only.
	credential.Warnf = func(format string, a ...any) {
		_, _ = fmt.Fprintf(errW, "warning: "+format+"\n", a...)
	}

	root.PersistentFlags().StringVar(&gf.baseURL, "base-url", "", "Pin the full base URL for this invocation, overriding host resolution (dev/testing)")
	root.PersistentFlags().BoolVar(&gf.readOnly, "read-only", false, "Reject all write operations")
	root.PersistentFlags().BoolVar(&gf.dryRun, "dry-run", false, "Do not send the request; only print the request that would be sent")
	root.PersistentFlags().BoolVar(&gf.noRetry, "no-retry", false, "Disable automatic retry of rate-limit/network failures on idempotent operations")
	root.PersistentFlags().BoolVar(&gf.assumeYes, "yes", false, "Confirm a write operation non-interactively (asserts the USER confirmed; an agent must not add --yes on its own, only after the user confirms)")
	if gf.envAxis {
		root.PersistentFlags().StringVar(&gf.env, "env", "", "Deployment environment for this invocation; overrides MEXC_ENV and the persisted `config set env`")
	}

	// An edition adds the global flags only IT needs — a credential selector, say.
	// They are registered after the shared set so a product cannot silently shadow
	// one, and they belong to the edition because a flag the product cannot honour
	// is worse than a missing one: a shared --profile flag appeared on a product
	// that has no profiles and no `profile` command to point the user at.
	if edFlags != nil {
		edFlags(root.PersistentFlags())
	}

	// resolved is filled once by PersistentPreRunE, after flags are parsed, so
	// every command sees the same effective config. Config() reads it; if a
	// command runs without PreRun (only in unit tests calling resolveConfig
	// directly), Config() falls back to a fresh resolve.
	var resolved *config.Config
	f := &Factory{
		Out: out,
		Err: errW,
		Config: func() config.Config {
			if resolved != nil {
				return *resolved
			}
			cfg, _ := resolveConfig(env, gf)
			return cfg
		},
	}
	// One per invocation, shared by every Client built below: a Client is per
	// request, but "your credentials are going to an overridden host" is advice about
	// the invocation and is stated once.
	advisories := &transport.HostAdvisories{}
	// The rate limiter is one-per-invocation and shared by every Client below: the
	// throttle is a process budget, not a per-request one, and the server face
	// builds a Client per request — a limiter per Client would reset the bucket on
	// every call and make the ceiling a no-op.
	//
	// Built lazily via OnceValue because the composition root injects f.RateLimit
	// AFTER NewApp returns (like the audit sink and WS collector); reading it here
	// at NewApp time would capture the nil that precedes SetRateLimit. First use is
	// a real request, long after assembly, so the policy is set by then. A nil
	// policy resolves to the shared default; only transport.Unlimited yields a nil
	// limiter and so no throttle.
	sharedLimiter := sync.OnceValue(func() *ratelimit.Limiter {
		return transport.NewSharedLimiter(f.RateLimit)
	})
	f.Client = func(ctx context.Context) *transport.Client {
		cfg := f.Config()
		c := transport.New(cfg)
		c.DryRun = gf.dryRun
		c.NoRetry = gf.noRetry
		c.Advisories = advisories
		c.Limiter = sharedLimiter()
		// The endpoint is resolved from these two, not from Config: Env selects a
		// row in the host table (pkg/hosts), and a non-empty BaseURLOverride (from
		// --base-url) pins the full URL and skips that resolution entirely.
		c.Env = hosts.Env(cfg.Env)
		c.BaseURLOverride = gf.baseURL
		// Read at call time, not captured: the composition root injects it after
		// NewApp returns, like the audit sink and the WS collector.
		c.BuildVersionHeader = f.BuildVersionHeader
		// Bind this request's credential lookup to the caller its context carries.
		// This is the point at which the per-request identity mechanism actually
		// takes effect: absent a binder, c.Identities stays nil and the transport's
		// identities() falls back to credential.Registry() — every locally invoked
		// build is byte-for-byte unchanged. With a binder, an absent caller
		// yields a fail-closed Lookup (credential.Binder's contract), never the
		// process registry.
		if f.IdentityBinder != nil {
			c.Identities = f.IdentityBinder().Bind(ctx)
		}
		return c
	}
	f.Setting = func(key string) (string, string, error) {
		// GetSetting validates the key against the closed set and reads the file.
		stored, err := config.GetSetting(key)
		if err != nil {
			return "", "", err
		}
		if key == "env" {
			// env is the one key with layers above the file; envInEffect owns them,
			// and resolveConfig reads the same function, so the two cannot disagree.
			v, src := envInEffect(env, gf)
			return v, src, nil
		}
		if stored != "" {
			return stored, SourceConfig, nil
		}
		return "", SourceUnset, nil
	}
	f.DryRun = func() bool { return gf.dryRun }
	f.AssumeYes = func() bool { return gf.assumeYes }
	f.Domains = ds
	f.SkillsFS = skillsFS
	f.Env = env

	// Validate every declared operation once at assembly. A bad declaration is
	// a programming error in a skill package; the shared CI runner cannot run
	// Go, so this is the net that makes it fail loudly — as a structured error
	// on every invocation — for the first person who runs the binary, instead
	// of silently misbehaving.
	// A holder because SetConfirmReporter adds a check after NewApp returns, and
	// PersistentPreRunE below has to see it.
	ae := &assemblyErr{err: validateRegistry(ds)}
	if ae.err == nil {
		// The host table is fully injected by now (framework registers adapters
		// before calling NewApp), so this is the first moment a dead row can be
		// seen — and the same net catches it as a bad operation declaration.
		ae.err = hosts.Validate()
	}

	// Resolve config once, up front, so every command in this invocation sees the
	// same effective safety mode and env.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if ae.err != nil {
			writeAndRecord(f, fe, ae.err)
			return ae.err
		}
		cfg, err := resolveConfig(env, gf)
		if err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		resolved = &cfg
		// Nudge (to stderr) when the installed SKILL.md docs are stale relative to
		// this binary — but never on the `skills` commands themselves (the user is
		// already there to fix it).
		if !strings.Contains(cmd.CommandPath(), "skills") {
			if n := skillstate.CheckStale(buildinfo.Version); n != nil {
				_, _ = fmt.Fprintln(f.Err, n.Message())
			}
		}
		return nil
	}

	// Mount every domain from the single registry, so a new domain needs only a
	// domains() entry — its command tree and its schema entry follow from the
	// same source and cannot drift apart.
	for _, d := range ds {
		root.AddCommand(mountDomain(d, f, fe))
	}
	// The shared faces are mounted here (they derive from the domain set). The
	// Shared builtins and these shared faces could equally be mounted by
	// framework.Run. Builtins live outside this package and are mounted there;
	// edition-specific builtins are mounted there as well.
	root.AddCommand(newSchemaCmd(f, fe))
	root.AddCommand(newMCPCmd(f, fe))
	root.AddCommand(newSkillsCmd(f, fe))
	root.AddCommand(newExploreCmd(f, fe))
	// cobra's two built-in commands never reach RunE, so App.Run's envelope wrapper
	// never sees them: both answered a bad invocation with help text on STDOUT and
	// exit 0 — the outcome the wrapper exists to prevent, since an agent reads exit
	// 0 as success and then parses prose as data.
	root.SetHelpCommand(newHelpCmd(root, f, fe))
	// InitDefaultCompletionCmd would otherwise run inside ExecuteC, too late to
	// touch. Forcing it here lets the completion group carry the same
	// unknown-subcommand guard every other group in this tree carries — its own
	// failure mode was exactly the one guardGroup fixes: a parent with subcommands
	// and no Run.
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			guardGroup(c, f, fe)
		}
	}

	return &App{root: root, fe: fe, errW: errW, f: f, ae: ae}
}

// newHelpCmd replaces cobra's built-in `help`, which prints "Unknown help topic"
// plus the root usage to stdout and exits 0. A typo therefore looked like success
// and put prose where an agent expects JSON.
//
// A resolvable topic behaves as before — the help text IS the requested data, so
// stdout is right for it. An unresolvable one is a structured error on stderr
// with exit 2, like every other bad argument in this tree.
func newHelpCmd(root *cobra.Command, f *Factory, fe *finalErr) *cobra.Command {
	c := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long: "Help provides help for any command in the application.\n" +
			"Type `" + cliname.Name() + " help [path to command]` for full details.",
		// Help must work when the config is broken — that is when someone needs it
		// most — so it skips the root's config-resolving PersistentPreRunE, exactly
		// as `config` and `profile` do.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := root.Find(args)
			// rest is what Find could not consume: `help spot nosuchthing` resolves
			// to spot with "nosuchthing" left over, which is not a topic either.
			if err != nil || target == nil || len(rest) > 0 {
				verr := errs.Validation(errs.SubtypeNotFound,
					"unknown help topic %q", strings.Join(args, " ")).
					WithHint("run `%s --help` to list available commands", root.Name())
				writeAndRecord(f, fe, verr)
				return verr
			}
			target.InitDefaultHelpFlag()
			target.InitDefaultVersionFlag()
			if herr := target.Help(); herr != nil {
				ierr := errs.Internal("cannot render help: %v", herr).WithCause(herr)
				writeAndRecord(f, fe, ierr)
				return ierr
			}
			return nil
		},
	}
	return c
}

// Run executes the command tree and returns the process exit code.
//
// It installs SIGINT/SIGTERM -> ctx cancellation for the whole invocation:
// a.root.Context() (and therefore every leaf command's c.Context()) is
// cancelled the moment either signal arrives. Most commands never notice —
// an ordinary REST call completes long before a human reacts — but a
// KindWSCollect (WS-backed) operation's unbounded read loop depends
// entirely on this: it has no deadline of its own by design (see
// internal/transport/ws's runLoop) and blocks until ctx is cancelled or
// the connection ends, so without this wiring Ctrl+C would do nothing but
// rely on the OS's default, unhandled-signal disposition to kill the
// process outright — never reaching the graceful "stop the read loop, close
// the connection, exit 0" path runWSOperation and Collect are built to
// take.
func (a *App) Run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a.root.SetContext(ctx)
	a.root.SetArgs(args)
	// ExecuteC, not Execute: it additionally returns the command the failure
	// belongs to, which is what lets the hint below quote a usage line instead of
	// telling the caller to go read --help. (Execute is literally this, discarding
	// the command.)
	cmd, err := a.root.ExecuteC()
	if err == nil {
		return 0
	}
	// exitCode still 0 → the error came from cobra itself (missing required flag,
	// unknown subcommand, argument parse failure, etc.) before RunE had a chance
	// to write an envelope. Wrap it uniformly as a validation envelope + exit 2 so
	// no error escapes as bare text or exit 0 (an unknown command with exit 0
	// would mislead the agent).
	if a.fe.exitCode == 0 {
		ve := errs.Validation(errs.SubtypeInvalidArgument, "%s", err.Error()).
			WithHint("%s", usageHint(cmd))
		output.WriteError(a.errW, ve)
		return errs.ExitCode(ve)
	}
	// The error envelope was already written to stderr inside RunE; here we just
	// return the recorded exit code.
	return a.fe.exitCode
}

// usageHint renders the hint for a cobra-level failure: the failing command's
// own usage line, plus where to get the rest.
//
// It quotes the usage rather than saying "run with --help" because the message
// cobra produces for the commonest of these — "accepts 1 arg(s), received 0" —
// names a COUNT and never the argument. The caller is told something is missing
// but not what, which for `profile set --api-key … --api-secret …` (every flag
// supplied, the positional name forgotten) reads as if the flags were wrong. The
// usage line answers it in the same breath: `… profile set <name> [flags]`.
//
// A command that HAS subcommands — a group, or the root reached by an unknown
// name — gets pointed at its subcommand list instead: it is a namespace, not an
// action, so quoting "usage: <cli> spot [flags]" there would suggest that
// flags alone are the fix when the missing piece is the name of a subcommand.
//
// The predicate is HasSubCommands and NOT Runnable, which is what it said first
// and which never fired: NewGroup/guardGroupErr sets RunE on every group (so a
// bare invocation yields an envelope rather than cobra's help-and-exit-0), making
// every group Runnable. The branch existed for groups and only the root ever
// reached it, while `<cli> spot --nosuchflag` got exactly the usage line the
// comment said the design avoids.
//
// A nil command (cobra could not attribute the failure) falls back to the generic
// hint. With cobra 1.8 this does not happen — ExecuteC returns the root when Find
// fails — but the fallback costs nothing and the guarantee is cobra's, not ours.
func usageHint(cmd *cobra.Command) string {
	if cmd == nil {
		return "run with --help to see required flags and usage"
	}
	if cmd.HasSubCommands() {
		return fmt.Sprintf("run `%s --help` to list available commands", cmd.CommandPath())
	}
	return fmt.Sprintf("usage: %s — run `%s --help` for the full flag list",
		cmd.UseLine(), cmd.CommandPath())
}

// resolveConfig composes the effective config: the safety mode and the routing
// env, which is all every product shares. Credentials are deliberately absent —
// resolving them is the injected identity provider's job, so this layer never
// learns how a given product stores them.
//
// The endpoint itself is not part of Config any more: it is resolved later,
// at request time, from cfg.Env (the host table, pkg/hosts) or from
// --base-url (Client.BaseURLOverride, an escape hatch that skips host
// resolution entirely) — see f.Client below. cfg.Env is layered here, from
// lowest to highest precedence by envInEffect: the persisted global setting
// (config.GlobalEnv, set with `config set env …`), MEXC_ENV, then --env.
// env is a global routing axis shared by every identity, not a credential, so it
// no longer lives in a profile.
//
// Note: MEXC_READ_ONLY remains environment-driven — it is configuration, not
// a credential.
func resolveConfig(env Env, gf *globalFlags) (config.Config, error) {
	cfg := config.Config{
		// Read-only is the union: the --read-only flag or MEXC_READ_ONLY=true,
		// either one being true makes it read-only (read-only is the safer
		// direction).
		ReadOnly: gf.readOnly || env("MEXC_READ_ONLY") == "true",
	}

	// Credentials are NOT resolved here. Which credential an operation needs, and
	// where it comes from, is the business of the identity provider the edition
	// injects — this layer must not know that one product keeps api keys in
	// on-disk profiles while another holds a login session. Config carries only
	// what every product shares: the safety mode and the routing env.

	// The env axis comes from envInEffect, which owns the whole precedence chain.
	// It is deliberately NOT spelled again here: the `config` command has to answer
	// the same question, and when the two spelled it separately they disagreed —
	// `config get env` reported the settings file while the request went wherever
	// MEXC_ENV pointed, and said `source: "default"` while doing it.
	cfg.Env, _ = envInEffect(env, gf)

	return cfg, nil
}

// The layer that supplied a setting's value. Reported by `config get`/`list` so a
// caller can tell "nothing is set, this is the product default" from "someone set
// this", without a second command and without guessing.
const (
	SourceFlag    = "flag"    // a per-invocation flag (--env)
	SourceEnvVar  = "env"     // an environment variable (MEXC_ENV)
	SourceConfig  = "config"  // this product's settings file
	SourceDefault = "default" // nothing set; the product's built-in default
	SourceUnset   = "unset"   // nothing set and no default — the key has no value
)

// envInEffect resolves the env this invocation uses and names the layer it came
// from, lowest precedence to highest: settings file < MEXC_ENV < --env. When no
// layer supplies one, the product's default (Edition.DefaultEnv, held by the host
// table) stands in — "" is a selection state, not an env, and no host registers a
// blank row, so a request could never go to one.
//
// Substituting the default HERE, at the single funnel, is what keeps "" and the
// default's name from being two keys for one environment. They were, and it was
// not a display quirk: an identity that keys stored state by cfg.Env wrote a login
// under "" and then failed to find it under the default name on the very next
// command, reporting login_required for a session it had just saved.
//
// This function is the ONLY place the chain is written down. resolveConfig calls
// it for cfg.Env and the `config` command calls it through Factory.Setting, so the
// value a caller is shown and the value a request uses cannot drift apart.
func envInEffect(env Env, gf *globalFlags) (value, source string) {
	value, source = "", SourceUnset
	// The three selectable layers exist only where there is an env to select.
	// Without an axis the flag was never registered, and the file and the variable
	// are deliberately not consulted: skipping them is what keeps a value left by
	// another product — or by this one before it dropped to a single env — from
	// routing a request at an env no host here serves. The tail below is shared, so
	// both paths agree on what "nothing selected" resolves to.
	if gf.envAxis {
		if v := config.GlobalEnv(); v != "" {
			value, source = v, SourceConfig
		}
		if v := env("MEXC_ENV"); v != "" {
			value, source = v, SourceEnvVar
		}
		if gf.env != "" {
			value, source = gf.env, SourceFlag
		}
		if value != "" {
			return value, source
		}
	}
	if d := string(hosts.Effective("")); d != "" {
		return d, SourceDefault
	}
	return "", SourceUnset
}

// opInvocation bundles the resolved inputs for one operation command execution:
// the operation and its effective auth/routing axes (resolved at assembly time in
// mountSubdomain), plus the per-call flag values collected in RunE. Grouping them
// keeps runOperation's signature small and stops the download-only fields
// (output/overwrite) from being positional noise on the common (JSON) path.
type opInvocation struct {
	op          api.Operation
	effSigned   api.Signing
	effIdentity api.Identity
	effHostKey  string
	values      map[string]string // collected flag values, keyed by param name
	data        string            // --data JSON body ("" if none)
	files       []api.FilePart    // already-read multipart parts (nil if not multipart)
	// bodyChecked records whether the payload was validated, so a dry-run plan can
	// report its own coverage. Carried rather than recomputed: the predicate that
	// decided it lives in newOperationCmd, and a second copy here would be free to
	// drift away from the one that did the work.
	bodyChecked bool
	output      string // --output path (binary ops only)
	overwrite   bool   // --overwrite (binary ops only)
	// cmdPath is the path this leaf was invoked as, for the audit record's command
	// line. Taken from the invocation, so a renamed argv[0] is recorded under the
	// name the caller actually typed.
	cmdPath string
}

// runOperation is the unified execution finalization for every operation
// command: build the transport-neutral RequestSpec, send it through the client,
// decide output based on dry-run, optionally shape the response (op.Transform),
// and on error write an envelope and record the exit code.
func runOperation(ctx context.Context, f *Factory, fe *finalErr, inv opInvocation) error {
	op := inv.op
	// Carried down from the confirmation gate so an accepted judgement is recorded on
	// the way out rather than on the path the user is waiting on. An empty channel
	// means there was nothing to confirm (a read, a dry-run) — and nothing to record.
	var (
		auditEv confirmaudit.Event
		auditCh confirmaudit.Channel
	)
	// Two independent reasons to ask the user first: the write semantics the
	// framework derives, and a grade the identity supplies for this operation's
	// permission requirement. Asked here rather than inside the gate because the
	// grade decides whether the block is ENTERED at all — it used to be reachable
	// only for a write, so a graded read never got as far as the gate.
	grade := gradeFor(f, inv.effIdentity, op)
	// Skipped under --dry-run (a dry-run previews the request without sending, so
	// there is nothing to confirm — mirrors how credentials and body validation
	// are dry-run exempt). confirmForCLI is a no-op when --yes is set.
	if (op.IsWrite() || grade.Confirm) && !f.DryRun() {
		// Login/credentials come BEFORE the interactive confirmation: a write that
		// cannot run for lack of a session (or API key) fails fast with
		// login_required instead of asking the user to confirm an action that
		// provably cannot execute. See precheckWriteCredentials.
		//
		// Kept to the write. The precheck exists to fail a WRITE fast, and a read
		// has always resolved its credentials at send time; running it for a graded
		// read would move where that read's login_required surfaces, which is a
		// change this feature does not need.
		if op.IsWrite() {
			if err := precheckWriteCredentials(f, inv.effIdentity); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
		}
		// Built once, so this judgement's records cannot drift apart on the key and
		// id the pairing relies on.
		auditEv = auditEvent(f, op, inv.values, inv.data, inv.cmdPath, faceCLI, grade)
		ch, err := confirmForCLI(f, auditEv, op, inv.values, inv.data, f.AssumeYes(), grade)
		if err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		auditCh = ch
		// On the way out, so the records land after the request has been issued AND
		// after the command has written its result — the trail is then off both paths
		// the user waits on, however slow the sink is. Sent whatever became of the
		// request: the stages attest what the user was asked and answered, which holds
		// whether the upstream accepted it, rejected it, or it was never built.
		defer func() { reportConfirmed(f, auditEv, auditCh) }()
	}
	spec, err := op.BuildSpec(inv.values, inv.data, inv.files, inv.effSigned, inv.effIdentity, inv.effHostKey)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	client := f.Client(ctx)
	// A binary operation streams its response to disk instead of buffering and
	// decoding JSON.
	if op.ResponseKind.IsBinary() {
		return runDownload(ctx, f, fe, client, spec, inv.output, inv.overwrite)
	}
	var dst any
	plan, err := client.Send(ctx, spec, &dst)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	if client.DryRun {
		// Only an operation that HAS a payload gets an answer about one; on the rest
		// the field stays absent rather than reporting a meaningless false.
		if op.Body != nil || op.Multipart != nil {
			checked := inv.bodyChecked
			plan.BodyChecked = &checked
		}
		return output.JSON(f.Out, plan)
	}
	// Optional output shaping: for the vast majority of operations Transform is
	// nil and the response passes through unchanged.
	if op.Transform != nil {
		shaped, terr := op.Transform(dst)
		if terr != nil {
			err := errs.Internal("output transform failed: %v", terr).WithCause(terr)
			writeAndRecord(f, fe, err)
			return err
		}
		dst = shaped
	}
	return output.JSON(f.Out, dst)
}

// runDownload executes a binary operation: it streams the response body straight
// to a local file rather than buffering and decoding it. The destination comes
// from --output (a directory, a file path, or — omitted — the current directory
// with a name derived from the request identity and the response headers); the
// write is atomic, so a failed transfer never leaves a truncated file. On success
// it prints the saved path and byte count. A dry-run prints the Plan and sends
// nothing. The operation is read, so it needs no write-confirmation and is allowed
// under --read-only.
func runDownload(ctx context.Context, f *Factory, fe *finalErr, client *transport.Client, spec api.RequestSpec, outputPath string, overwrite bool) error {
	res, plan, err := client.SendStream(ctx, spec)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	if client.DryRun {
		return output.JSON(f.Out, plan)
	}
	defer func() { _ = res.Body.Close() }()

	// Compute the default name and validate the destination BEFORE writing any
	// bytes: the headers are already in hand, but the body has not been read yet,
	// so a bad --output aborts the download before it streams.
	name := fileio.DefaultDownloadName(fileio.NameInput{
		Method:             spec.Method,
		Path:               spec.Path,
		Query:              api.CanonicalQuery(spec.Query),
		Body:               spec.Body,
		ContentDisposition: res.ContentDisposition,
		ContentType:        res.ContentType,
	})
	target, err := fileio.ResolveOutputPath(outputPath, overwrite, name)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	n, err := fileio.AtomicWriteFromReader(target, res.Body)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	return output.JSON(f.Out, map[string]any{"saved_path": target, "bytes": n})
}

// precheckWriteCredentials verifies the operation's identity can produce
// credentials BEFORE the user is asked to confirm the write. A not-logged-in (or
// missing-API-key) write then fails fast with the transport's own login_required /
// failed_precondition error, instead of prompting the user to confirm an action
// that provably cannot run. It mirrors what the transport does at send time
// (credential.Resolve(cfg, env)) minus the network: Resolve is local and
// side-effect-free — it reads the stored session / profile, never refreshes or
// consumes a token — so resolving here and again in Send is safe and cheap.
//
// A public identity (none) needs no credentials, so this is a no-op. The caller
// gates it on !DryRun (a dry-run resolves no credentials, exactly as Send skips
// Resolve under dry-run) and on IsWrite (only a write reaches the interactive
// confirm this ordering protects). It catches "no session" — the common
// not-logged-in case; a session that merely lacks the op's site is a business
// authorization the transport still checks at Inject/send time.
func precheckWriteCredentials(f *Factory, effIdentity api.Identity) error {
	if effIdentity == "" || effIdentity == api.IdentityNone {
		return nil
	}
	p, ok := credential.Get(credential.Realm(effIdentity))
	if !ok {
		return errs.Internal("no credential provider for identity %q", effIdentity)
	}
	cfg := f.Config()
	if _, err := p.Resolve(cfg, hosts.Env(cfg.Env)); err != nil {
		return err
	}
	return nil
}

// wsCollectPlan is the --dry-run preview for a KindWSCollect (WS-backed) operation.
// It is not pkg/transport.Plan: that type's Method field is HTTP-request
// vocabulary a WS operation has none of, and a WS dry-run answers a different
// question — which URL/channels would be subscribed, and under what
// termination model — not "what request would be sent".
type wsCollectPlan struct {
	Operation string   `json:"operation"`
	URL       string   `json:"url"`
	Channels  []string `json:"channels"`
	// Signed mirrors every other operation's dry-run plan: whether the
	// operation signs a request (the scheme NAME stays internal to the CLI).
	// A Stream op's OWN command never builds a signed RequestSpec, but a
	// private channel's one-shot preflight (e.g. Spot's listenKey exchange)
	// may be signed on the op's behalf — this field reports the op's
	// declared/inherited Signed axis regardless, for the same reason
	// Schema().Signed does: an agent inspecting this operation should see one
	// consistent answer across the schema, dry-run, and MCP faces.
	Signed         bool   `json:"signed"`
	Bounded        bool   `json:"bounded"`
	ListenDuration string `json:"listenDuration,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	// EnvIgnored is true for every WS operation: MEXC publishes no testnet WS
	// gateway, so --env/--base-url silently have no effect on where a WS
	// operation connects — it always talks to the production WS endpoint.
	// This field makes that fact visible in the dry-run output itself, rather
	// than only in a code comment the caller cannot see.
	EnvIgnored bool `json:"envIgnored"`
}

// newWSCollectPlan builds a KindWSCollect operation's dry-run preview from
// its fully qualified name, effective Signed axis, and the config its
// WSCollect closure returned.
func newWSCollectPlan(qualifiedName string, effSigned api.Signing, cfg *xport.WSConfig, bounded bool, duration time.Duration, limit int) wsCollectPlan {
	listenDuration := ""
	if duration > 0 {
		listenDuration = duration.String()
	}
	return wsCollectPlan{
		Operation:      qualifiedName,
		URL:            transport.RedactURLQuery(cfg.URL),
		Channels:       cfg.Channels,
		Signed:         effSigned.Signs(),
		Bounded:        bounded,
		ListenDuration: listenDuration,
		Limit:          limit,
		EnvIgnored:     true,
	}
}

// runWSOperation is the unified execution path for every KindWSCollect
// (WS-backed) operation: resolve credentials when the operation carries an
// identity (empty strings for a public channel), call the operation's
// WSCollect closure to get a *pkg/transport.WSConfig, optionally complete a
// Spot-style listenKey preflight exchange, then drive internal/transport's
// WS client.
//
// It never calls op.BuildSpec / runOperation: a KindWSCollect operation
// builds no RequestSpec at all (the optional PreflightRequest is a
// separate, one-shot REST call the WS execution path itself sends, not the
// main subscription).
//
// Bounded (listenDurationStr != "" or limit > 0) and unbounded (both
// empty/zero) are NOT two different output shapes — both stream through the
// exact same OnMessage/OnReject callbacks and write the exact same JSON Lines
// shape (one compact JSON object per line, via output.JSONLine) as each frame
// arrives. --listen-duration/--limit only decide whether the WS client
// eventually stops itself and returns nil (bounded) or keeps going until ctx
// is cancelled (Ctrl+C) or the connection ends (unbounded, no auto-reconnect
// — deliberate: a WS operation here is for on-demand observation, not a
// durable background service).
//
// hostOverrides is a per-invocation host-key -> base-URL override map,
// keyed by exactly the same HostKey values a WSConfig names (today "spot-ws"
// / "futures-ws", populated by mount.go from --spot-ws-url/--futures-ws-url,
// mirroring how gf.baseURL feeds internal/transport's single unkeyed
// --base-url override for a REST RequestSpec.HostKey). This function reads
// ONLY hostOverrides[cfg.HostKey] — the one key this specific operation's
// WSCollect closure actually named — so a flag naming a DIFFERENT key (e.g.
// --futures-ws-url passed to a spot operation) is never even consulted, not
// merely ignored by convention. Adding a third WS protocol later means one
// more registered pkg/hosts key and one more mount.go flag feeding one more
// map entry — this function's signature and resolution logic need no
// change at all.
func runWSOperation(ctx context.Context, f *Factory, fe *finalErr, op api.Operation, effSigned api.Signing, effIdentity api.Identity, values map[string]string, listenDurationStr string, limit int, hostOverrides map[string]string) error {
	var duration time.Duration
	if listenDurationStr != "" {
		d, err := time.ParseDuration(listenDurationStr)
		if err != nil {
			err2 := errs.Validation(errs.SubtypeInvalidArgument,
				"invalid --listen-duration %q: %v", listenDurationStr, err).
				WithParam("--listen-duration").
				WithHint("use Go duration syntax, e.g. 10s, 2m, 1h30m")
			writeAndRecord(f, fe, err2)
			return err2
		}
		// time.ParseDuration happily accepts a negative string ("-5s") and
		// returns a negative Duration — it is a valid Go duration, just not a
		// valid COLLECTION WINDOW. Left unchecked, a negative value fails the
		// bounded/deadline checks below (both test for > 0), so the read loop
		// silently treats it as "no bound at all" and runs unbounded with no
		// indication the flag was ignored — the same class of silent
		// misinterpretation --limit's own negative check (right below) exists
		// to prevent.
		if d < 0 {
			err2 := errs.Validation(errs.SubtypeInvalidArgument,
				"invalid --listen-duration %q: must not be negative", listenDurationStr).
				WithParam("--listen-duration").
				WithHint("use a positive duration, e.g. 10s, 2m, 1h30m, or omit the flag for no time bound")
			writeAndRecord(f, fe, err2)
			return err2
		}
		duration = d
	}
	if limit < 0 {
		err := errs.Validation(errs.SubtypeInvalidArgument, "invalid --limit %d: must not be negative", limit).
			WithParam("--limit").
			WithHint("use 0 for no limit, or a positive count")
		writeAndRecord(f, fe, err)
		return err
	}
	bounded := duration > 0 || limit > 0
	dryRun := f.DryRun()

	// Credentials are resolved BEFORE calling Build (a public channel's Build
	// ignores them), but skipped entirely under --dry-run — mirroring how the
	// REST path never resolves credentials in dry-run. This matters for a
	// private channel like ws-deals: a --dry-run preview must work with no
	// saved profile, exactly like every REST operation's dry-run does.
	apiKey, secretKey := "", ""
	if !dryRun && effIdentity != "" && effIdentity != api.IdentityNone {
		p, ok := credential.Get(credential.Realm(effIdentity))
		if !ok {
			err := errs.Internal("no credential provider for identity %q", effIdentity)
			writeAndRecord(f, fe, err)
			return err
		}
		cred, err := p.Resolve(f.Config(), hosts.Env(f.Config().Env))
		if err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		apiKey, secretKey = cred.APIKey, cred.Secret
	}

	rawCfg, err := op.WSCollect(values, apiKey, secretKey, dryRun)
	if err != nil {
		writeAndRecord(f, fe, err)
		return err
	}
	cfg, ok := rawCfg.(*xport.WSConfig)
	if !ok {
		err := errs.Internal("operation %q's WSCollect returned %T, want *pkg/transport.WSConfig", op.Name, rawCfg)
		writeAndRecord(f, fe, err)
		return err
	}

	// Resolve cfg.HostKey to a base URL — a pure lookup, so it runs
	// identically under dry-run and a real call, mirroring
	// internal/transport/send.go's resolveBase for a REST RequestSpec.HostKey:
	// a per-invocation override (--spot-ws-url / --futures-ws-url, keyed by
	// HostKey in hostOverrides) wins over pkg/hosts.Resolve's own chain
	// (MEXC_<KEY>_BASE_URL env var, then the registered default). cfg.URL is
	// ALWAYS a suffix appended to this base, never a full URL of its own
	// (see pkg/transport.WSConfig's doc comment) — the common case (every
	// public channel, and futures' login-based private channel) leaves it
	// empty, so the base alone is the dial target.
	base := hostOverrides[cfg.HostKey]
	if base == "" {
		ep, err := hosts.Resolve(cfg.HostKey, hosts.Env(f.Config().Env))
		if err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		base = ep.URL
	}
	cfg.URL = base + cfg.URL

	if dryRun {
		return output.JSON(f.Out, newWSCollectPlan(op.Name, effSigned, cfg, bounded, duration, limit))
	}

	// A Spot-style private channel authenticates by a pre-dial REST exchange
	// (listenKey): the Build closure describes it as data (PreflightRequest),
	// and this is the one place it is actually sent, using the ALREADY
	// constructed *transport.Client this invocation holds — so the exchange
	// inherits that client's timeout and dry-run guard for free, rather than
	// the business closure making its own bare HTTP call.
	if cfg.PreflightRequest != nil {
		var dst any
		if _, err := f.Client(ctx).Send(ctx, *cfg.PreflightRequest, &dst); err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		suffix, err := cfg.ApplyPreflightResult(dst)
		if err != nil {
			writeAndRecord(f, fe, err)
			return err
		}
		// ApplyPreflightResult returns a SUFFIX (e.g. "?listenKey=..."), not
		// a full URL — append it to the already-resolved base, replacing the
		// dry-run-only placeholder suffix cfg.URL carried above.
		cfg.URL = base + suffix
	}

	cfg.Duration, cfg.Limit = duration, limit

	// Both bounded and unbounded runs stream through the exact same two
	// callbacks and emit the exact same JSON Lines shape — output.JSONLine,
	// not the indented output.JSON, because a genuine JSON Lines stream must
	// be line-oriented so a consumer can pipe it through jq -c / read -r. A
	// write failure aborts the collection rather than accumulating output
	// that can never reach a broken pipe (e.g. the caller closed stdout).
	var streamErr error
	cfg.OnMessage = func(m xport.WSMessage) {
		if streamErr != nil {
			return
		}
		streamErr = output.JSONLine(f.Out, m)
	}
	cfg.OnReject = func(channel, reason string) {
		if streamErr != nil {
			return
		}
		streamErr = output.JSONLine(f.Out, map[string]any{
			"error": map[string]string{
				"type": "api", "subtype": errs.SubtypeSubscriptionRejected,
				"channel": channel, "message": reason,
			},
		})
	}

	if f.WSCollector == nil {
		err := errs.Internal("this build mounts a WS-backed operation but declares no WS collector")
		writeAndRecord(f, fe, err)
		return err
	}
	err = f.WSCollector().Collect(ctx, *cfg)
	if streamErr != nil {
		writeAndRecord(f, fe, streamErr)
		return streamErr
	}
	if err != nil && ctx.Err() == nil {
		// ctx.Err() != nil means the process was interrupted (Ctrl+C) — a
		// clean, expected stop, not a failure to report. Anything else
		// (subscription/login rejected or timed out, or the connection
		// ended) is a real error.
		writeAndRecord(f, fe, err)
		return err
	}
	return nil
}

// writeAndRecord writes the error as an envelope to stderr and records the exit
// code (used on validation failures).
func writeAndRecord(f *Factory, fe *finalErr, err error) {
	output.WriteError(f.Err, err)
	fe.exitCode = errs.ExitCode(err)
}
