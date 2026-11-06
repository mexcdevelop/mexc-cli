package cli

import (
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// The subdomain-topology types (api.DomainSpec / api.Subdomain) live in pkg/api
// so a composition root can declare business topology against a stable,
// importable type. The helpers below resolve an operation's effective auth axes
// against its subdomain default at assembly time — the one place that sees both
// the op and its subdomain. Command assembly is handled generically by
// mountSubdomain — no hand-written cobra flag binding required.

// effectiveSigned resolves an operation's signing scheme against its subdomain
// default: a concrete op-level value overrides; the zero value inherits.
func effectiveSigned(op api.Operation, sd api.Subdomain) api.Signing {
	if op.Signed.Concrete() {
		return op.Signed
	}
	return sd.Signed
}

// effectiveIdentity resolves an operation's login identity the same way.
func effectiveIdentity(op api.Operation, sd api.Subdomain) api.Identity {
	if op.Identity.Concrete() {
		return op.Identity
	}
	return sd.Identity
}

// effectiveHostKey resolves an operation's host key: a concrete op-level
// override wins, otherwise the subdomain default.
func effectiveHostKey(op api.Operation, sd api.Subdomain) string {
	if op.HostKey != "" {
		return op.HostKey
	}
	return sd.HostKey
}

// validateRegistry walks every declared operation and validates the
// declarations plus the cross-field rules that need the subdomain defaults.
// It is the assembly-time safety net: the shared CI runner cannot run Go, so
// a bad declaration that slips past the local gates must still fail loudly for
// the first person who runs the binary — as a structured error, not a silent
// misbehavior. NewApp runs it once and surfaces the error on every invocation.
func validateRegistry(ds []api.DomainSpec) error {
	for _, d := range ds {
		for _, sd := range d.Subs {
			if !sd.Signed.Concrete() {
				return errs.Internal(
					"subdomain %s.%s declares no concrete signing default (got %q)", d.Name, sd.Name, sd.Signed).
					WithHint("wire `Signed: <pkg>.Signed` in the edition's domains.go from the subdomain's api package constant")
			}
			// A scheme is real iff a signer is registered for it — the same rule
			// the identity below follows, and stricter than the compile-time list
			// in pkg/api that it replaces: a listed scheme with no signer used to
			// assemble fine and fail at send time. SignNone is exempt: not signing
			// needs no signer.
			if sd.Signed != api.SignNone {
				if _, ok := signer.Get(sd.Signed); !ok {
					return errs.Internal(
						"subdomain %s.%s declares signing scheme %q, but no signer is registered for it",
						d.Name, sd.Name, sd.Signed).
						WithHint("register a signer.Signer whose Scheme() is this value via Edition.Signers, " +
							"or declare a scheme this product supplies")
				}
			}
			if !sd.Identity.Concrete() {
				return errs.Internal(
					"subdomain %s.%s declares no concrete identity default (got %q)", d.Name, sd.Name, sd.Identity).
					WithHint("wire `Identity: <pkg>.Identity` in the edition's domains.go from the subdomain's api package constant")
			}
			// An identity is real iff a provider is registered for it. This
			// replaces a compile-time list of identity values in pkg/api, and is
			// stricter: a listed value could still have no provider behind it and
			// would fail only at the first real call. IdentityNone is exempt —
			// "no identity" needs nothing to resolve it.
			if sd.Identity != api.IdentityNone {
				if _, ok := credential.Get(credential.Realm(sd.Identity)); !ok {
					return errs.Internal(
						"subdomain %s.%s declares identity %q, but no provider is registered for it",
						d.Name, sd.Name, sd.Identity).
						WithHint("inject a credential.Provider whose Realm() is this identity via Edition.Identities, " +
							"or declare an identity this product supplies")
				}
			}
			for _, op := range sd.Ops {
				if err := op.ValidateDecl(); err != nil {
					return err
				}
				// Permission expresses RBAC against a grant map, so it is only
				// meaningful on an identity whose credentials carry one. This is a
				// cross-field rule needing the EFFECTIVE identity, so it lives here
				// rather than in ValidateDecl.
				//
				// The test is the provider's capability, not a particular identity
				// value: this layer must not know which identities exist — only the
				// edition that injects one does. Registration happens before assembly
				// (framework.Run registers adapters, then builds the tree), so the
				// registry is populated by the time this runs.
				if id := effectiveIdentity(op, sd); op.Permission != nil &&
					!credential.CarriesGrants(credential.Realm(id)) {
					return errs.Internal(
						"operation %q declares Permission but its effective identity %q does not carry RBAC grants",
						op.Name, id).
						WithHint("Permission needs an identity whose provider carries a grant map; " +
							"declare an identity that does, or drop the Permission field")
				}
				// A business Param must not collide with a wire name the framework
				// injects for the op's effective scheme/identity (a signing param, the
				// api-key header, Content-Type) — such a param is silently overwritten
				// at send time, so reject it at assembly instead.
				if err := validateReservedNames(op, effectiveSigned(op, sd), effectiveIdentity(op, sd)); err != nil {
					return err
				}
				// NOTE: a login-token op is NOT required to declare Permission — RBAC is
				// a per-business decision. Host routing does not depend on Permission: it
				// keys off the (mandatory) hostKey, which for such an identity is the
				// business system. So no "identity ⟹ Permission" gate here.
				// Every op must resolve to a concrete host key (subdomain default or
				// op-level override) so httpx can eventually route it; the transport
				// does not consume it yet, but the registry must never allow a gap.
				if effectiveHostKey(op, sd) == "" {
					return errs.Internal("subdomain %s.%s operation %q has no host key", d.Name, sd.Name, op.Name).
						WithHint("wire `HostKey:` on the subdomain in the edition's domains.go (a pkg/hosts key)")
				}
			}
		}
	}
	return nil
}

// validateReservedNames rejects a business Param whose wire name collides with a
// name the framework injects for the op's effective signing scheme or identity:
// a colliding query param is silently overwritten by signing, and a colliding
// header by identity injection or the framework's own Content-Type. The owning
// packages (signer, credential) name what they inject, so this knowledge is not
// duplicated here. Header comparison is case-insensitive (HTTP header names are).
func validateReservedNames(op api.Operation, effSigned api.Signing, effIdentity api.Identity) error {
	reservedQuery := map[string]bool{}
	for _, n := range signer.ReservedParams(effSigned) {
		reservedQuery[n] = true
	}
	reservedHeader := map[string]bool{http.CanonicalHeaderKey("Content-Type"): true}
	for _, n := range signer.ReservedHeaders(effSigned) {
		reservedHeader[http.CanonicalHeaderKey(n)] = true
	}
	for _, n := range credential.ReservedHeaders(credential.Realm(effIdentity)) {
		reservedHeader[http.CanonicalHeaderKey(n)] = true
	}
	for _, p := range op.Params {
		switch {
		case p.In.IsQuery():
			if reservedQuery[p.Name] {
				return errs.Internal(
					"operation %q declares query param %q, which the %q signing scheme injects and would overwrite",
					op.Name, p.Name, effSigned).
					WithHint("rename the wire parameter; this signing param is framework-owned")
			}
		case p.In == api.InHeader:
			if reservedHeader[http.CanonicalHeaderKey(p.Name)] {
				return errs.Internal(
					"operation %q declares header param %q, which the framework injects and would overwrite",
					op.Name, p.Name).
					WithHint("rename the header; signer, identity, and Content-Type headers are framework-owned (see api.Param docs)")
			}
		}
	}
	return nil
}

// validateBuildVersionHeader rejects an Edition.BuildVersionHeader that some
// operation also declares as a header param: the param is set later and wins, so
// the version would silently vanish from that one operation — the same
// silent-overwrite bug validateReservedNames gates for signer/identity headers.
//
// Separate from that function because the name is not known when it runs: the
// composition root injects it only after NewApp has validated the registry, so
// this runs from the setter instead.
func validateBuildVersionHeader(ds []api.DomainSpec, name string) error {
	if name == "" {
		return nil
	}
	want := http.CanonicalHeaderKey(name)
	for _, d := range ds {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				for _, p := range op.Params {
					if p.In == api.InHeader && http.CanonicalHeaderKey(p.Name) == want {
						return errs.Internal(
							"operation %s.%s.%s declares header param %q, which Edition.BuildVersionHeader also names",
							d.Name, sd.Name, op.Name, p.Name).
							WithHint("rename the wire parameter, or have the edition name a different build-version header")
					}
				}
			}
		}
	}
	return nil
}

// mountGroup creates an intermediate node (domain / subdomain) command and
// attaches the unknown-subcommand guard.
func mountGroup(use, short string, f *Factory, fe *finalErr) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short}
	guardGroup(c, f, fe)
	return c
}

// NewGroup builds an intermediate (group) command with the same
// unknown-subcommand guard as mountGroup, for an edition builtin assembled
// OUTSIDE this package (e.g. profile / auth, which live in an edition package
// that cannot reach the unexported mountGroup). writeErr is how the guard reports a
// bad invocation — typically framework.Deps.WriteError, which writes the
// structured envelope to stderr and records the exit code — so an edition group
// behaves identically to a shared one without engine leaking its Factory.
func NewGroup(use, short string, writeErr func(error)) *cobra.Command {
	c := &cobra.Command{Use: use, Short: short}
	guardGroupErr(c, writeErr)
	return c
}

// mountDomain assembles a domain group and all of its subdomains into a cobra
// command tree. Both NewApp (command mounting) and the spec consumed by the
// schema command come from the same domains() registry, so they cannot drift.
func mountDomain(d api.DomainSpec, f *Factory, fe *finalErr) *cobra.Command {
	group := mountGroup(d.Name, d.Short, f, fe)
	for _, sd := range d.Subs {
		mountSubdomain(group, sd, f, fe)
	}
	return group
}

// mountSubdomain assembles all of a subdomain's Operations into cobra commands
// and attaches them to parent. The effective sign/identity of each op is
// resolved here — the assembly moment is the only time the op and its
// subdomain are both in hand — and welded into the leaf command's closure, so
// runtime does no lookup. Each operation gets its own parameter-value map
// (captured by closure), preventing shared mutable state across commands.
func mountSubdomain(parent *cobra.Command, sd api.Subdomain, f *Factory, fe *finalErr) {
	sub := mountGroup(sd.Name, sd.Short, f, fe)
	for _, op := range sd.Ops {
		sub.AddCommand(newOperationCmd(op, effectiveSigned(op, sd), effectiveIdentity(op, sd), effectiveHostKey(op, sd), f, fe))
	}
	parent.AddCommand(sub)
}

// newOperationCmd turns a single Operation into a leaf command: bind a flag per
// declared Param, then at execution time collect values → framework validation →
// build request → unified finalization. effSigned/effIdentity are the operation's
// effective auth axes, resolved by the caller at assembly time.
func newOperationCmd(op api.Operation, effSigned api.Signing, effIdentity api.Identity, effHostKey string, f *Factory, fe *finalErr) *cobra.Command {
	// Each command owns its own values map to avoid cross-command sharing (a
	// pitfall of the old implementation).
	values := make(map[string]*string, len(op.Params))
	// dataPtr backs the --data flag, registered for operations that declare a
	// JSON Body or a Multipart with companion text fields. filePtr backs the
	// repeatable --file flag, registered only for Multipart operations. Both are
	// assigned below (after the command is built) and read in RunE via closure.
	var dataPtr *string
	var filePtr *[]string
	// outputPtr / overwritePtr back the --output / --overwrite flags, registered
	// only for a binary (download) operation and read in RunE via closure.
	var outputPtr *string
	var overwritePtr *bool
	// listenDurationPtr / limitPtr back the --listen-duration / --limit flags,
	// registered only for a KindWSCollect (WS-backed) operation and read in
	// RunE via closure — the streaming counterpart of outputPtr/overwritePtr
	// above.
	var listenDurationPtr *string
	var limitPtr *int
	// spotWSURLPtr / futuresWSURLPtr back the --spot-ws-url / --futures-ws-url
	// flags, registered alongside listenDurationPtr/limitPtr on every
	// KindWSCollect operation regardless of which protocol it implements (see
	// pkg/api.Operation.WSCollect's doc comment on why both are threaded into
	// every closure uniformly: this call site has no domain/subdomain NAME to
	// gate registration on, only Kind, which is identical across all 8 spot +
	// futures WS operations).
	var spotWSURLPtr *string
	var futuresWSURLPtr *string
	// A Multipart op with declared text fields also carries them on --data.
	hasData := op.Body != nil || (op.Multipart != nil && op.Multipart.Fields != nil)

	cmd := &cobra.Command{
		Use:   op.Name,
		Short: op.Summary,
		RunE: func(c *cobra.Command, _ []string) error {
			collected := make(map[string]string, len(values))
			for name, ptr := range values {
				collected[name] = *ptr
			}
			// Framework-level validation (required / enum) applies to a
			// KindWSCollect operation's params exactly as to any other
			// operation; it never touches Method/Path/Body, which a
			// KindWSCollect operation has none of.
			if err := op.Validate(collected); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			// A KindWSCollect (WS-backed) operation runs a wholly different
			// execution path: it never builds a RequestSpec, so it branches
			// here, before any of the REST-only checks below (write
			// confirmation, --data, --file) that assume a RequestSpec exists.
			if op.Kind == api.KindWSCollect {
				listenDuration := ""
				if listenDurationPtr != nil {
					listenDuration = *listenDurationPtr
				}
				limit := 0
				if limitPtr != nil {
					limit = *limitPtr
				}
				// hostOverrides is keyed by exactly the same HostKey strings a
				// WSConfig names ("spot-ws" / "futures-ws" — literals here for
				// the same layering reason skills/**/api uses literals rather
				// than importing editions/public/adapter's constants: this
				// package composes an edition, so it must not import back into
				// one). runWSOperation reads only the ONE entry matching its
				// operation's own cfg.HostKey, so an irrelevant flag (e.g.
				// --futures-ws-url on a spot operation) is present in the map
				// but never consulted — see runWSOperation's doc comment.
				hostOverrides := map[string]string{"spot-ws": "", "futures-ws": ""}
				if spotWSURLPtr != nil {
					hostOverrides["spot-ws"] = *spotWSURLPtr
				}
				if futuresWSURLPtr != nil {
					hostOverrides["futures-ws"] = *futuresWSURLPtr
				}
				return runWSOperation(c.Context(), f, fe, op, effSigned, effIdentity, collected, listenDuration, limit, hostOverrides)
			}
			// --data is resolved before anything reads it. The inline JSON may
			// arrive still wrapped in the quotes a non-POSIX shell did not strip,
			// or be deferred to a file or stdin precisely so that no shell has to
			// be trusted with it — resolveDataInput settles which, and everything
			// downstream sees only the result. That ordering is the point: body
			// validation, the confirmation display and the request all read the
			// SAME resolved text, so what a dry-run previews and what the confirm
			// prompt shows the user is the body that would actually be sent.
			data := ""
			if dataPtr != nil {
				resolved, rerr := resolveDataInput(*dataPtr, c.InOrStdin())
				if rerr != nil {
					writeAndRecord(f, fe, rerr)
					return rerr
				}
				data = resolved
			}
			// Body operations validate the --data payload against the declared
			// prototype (well-formed JSON, no unknown fields, required + enum).
			//
			// A dry-run checks what it WAS GIVEN and never demands what it was not.
			// Both halves carry weight. Previewing structure without a payload is
			// what lets `<op> --dry-run` answer "where would this go?" before the
			// caller has built anything, so an absent --data is not an error here.
			// But a payload that WAS supplied costs nothing to validate and is the
			// entire reason to run dry before running real: skipping it returns a
			// green preview for a call already known to be rejectable, and on a
			// write that green survives until after a human has been asked to
			// confirm. The exemptions that remain are the ones that need the
			// network or a credential; this needs neither.
			bodyChecked := false
			if op.Body != nil && (!f.DryRun() || strings.TrimSpace(data) != "") {
				if err := op.ValidateBody(data); err != nil {
					writeAndRecord(f, fe, err)
					return err
				}
				bodyChecked = true
			}
			// A read-only binary rejects write operations (one of the two write
			// guards). Checked before any file IO below.
			if op.IsWrite() && f.Config().ReadOnly {
				err := errs.Validation(errs.SubtypeFailedPrecondition,
					"write operation %q is blocked in --read-only mode", op.Name).
					WithHint("drop --read-only (and MEXC_READ_ONLY) to allow writes")
				writeAndRecord(f, fe, err)
				return err
			}
			// Multipart operations carry their files on --file (<field>=<path>,
			// repeatable) and optional text fields on --data.
			//
			// The same rule splits this in two. Parsing the entries and validating
			// the field shape touch nothing outside the process, so a dry-run does
			// them as soon as the caller has supplied something to check. Reading
			// the files is IO, so it stays behind the dry-run gate — and files is
			// left nil, which is what keeps a preview's file list empty.
			var files []api.FilePart
			if op.Multipart != nil {
				supplied := len(filesFrom(filePtr)) > 0 || strings.TrimSpace(data) != ""
				if !f.DryRun() || supplied {
					entries, provided, perr := parseFileEntries(op.Multipart, filesFrom(filePtr))
					if perr != nil {
						writeAndRecord(f, fe, perr)
						return perr
					}
					if err := op.ValidateMultipart(provided, data); err != nil {
						writeAndRecord(f, fe, err)
						return err
					}
					bodyChecked = true
					if !f.DryRun() {
						parts, rerr := readFileParts(entries)
						if rerr != nil {
							writeAndRecord(f, fe, rerr)
							return rerr
						}
						files = parts
					}
				}
			}
			output := ""
			if outputPtr != nil {
				output = *outputPtr
			}
			overwrite := overwritePtr != nil && *overwritePtr
			return runOperation(c.Context(), f, fe, opInvocation{
				op:          op,
				effSigned:   effSigned,
				effIdentity: effIdentity,
				effHostKey:  effHostKey,
				values:      collected,
				data:        data,
				bodyChecked: bodyChecked,
				files:       files,
				output:      output,
				overwrite:   overwrite,
				cmdPath:     c.CommandPath(),
			})
		},
	}

	for _, p := range op.Params {
		ptr := new(string)
		values[p.Name] = ptr
		cmd.Flags().StringVar(ptr, p.Name, "", p.Desc)
		if p.Required {
			_ = cmd.MarkFlagRequired(p.Name)
		}
	}
	// A Body op (or a Multipart op with text fields) carries the JSON on a single
	// --data flag; its field structure is inspectable via the `schema` command and
	// validated in RunE (op.ValidateBody / op.ValidateMultipart).
	if hasData {
		dataPtr = new(string)
		cmd.Flags().StringVar(dataPtr, "data", "", "JSON request body / text fields; also @<path> to read it from a file or - to read stdin (inspect with `"+cliname.Name()+" schema`)")
	}
	// A Multipart op carries its files on a repeatable --file flag, each entry
	// <field>=<path> (a bare <path> is accepted when only one file field exists).
	if op.Multipart != nil {
		filePtr = new([]string)
		cmd.Flags().StringArrayVar(filePtr, "file", nil, "Upload file as <field>=<path> (repeatable; inspect fields with `"+cliname.Name()+" schema`)")
	}
	// A binary (download) op saves its streamed response to disk: --output picks
	// the destination (a directory, a file path, or omitted for the current
	// directory) and --overwrite allows replacing an existing file.
	if op.ResponseKind.IsBinary() {
		outputPtr = new(string)
		cmd.Flags().StringVarP(outputPtr, "output", "o", "", "Save the download to this path (a directory, a file path, or omit for the current directory)")
		overwritePtr = new(bool)
		cmd.Flags().BoolVar(overwritePtr, "overwrite", false, "Overwrite the output file if it already exists")
	}
	// A KindWSCollect (WS-backed) op is a long-lived subscription rather than a
	// one-shot request: --listen-duration/--limit bound how long it runs
	// before stopping on its own (both omitted means run until Ctrl+C or the
	// connection ends). These are injected here, not declared as op.Params,
	// because they govern the execution model, not the upstream request.
	if op.Kind == api.KindWSCollect {
		listenDurationPtr = new(string)
		cmd.Flags().StringVar(listenDurationPtr, "listen-duration", "", "Stop after this long (Go duration syntax, e.g. 10s, 2m); omit for no time bound")
		limitPtr = new(int)
		cmd.Flags().IntVar(limitPtr, "limit", 0, "Stop after this many messages; omit (or 0) for no count bound")
		// --spot-ws-url/--futures-ws-url pin the WS gateway URL for this
		// invocation only, the WS counterpart of --base-url: MEXC publishes no
		// testnet WS gateway, so --env/--base-url (which govern REST host
		// resolution via pkg/hosts) have no effect on a WS operation's dial
		// target at all — these two flags are the only way to redirect one.
		// Both are registered on EVERY KindWSCollect op regardless of protocol
		// (this call site has no domain/subdomain name to gate on); the
		// business closure irrelevant to a given flag simply ignores it (see
		// pkg/api.Operation.WSCollect's doc comment).
		spotWSURLPtr = new(string)
		cmd.Flags().StringVar(spotWSURLPtr, "spot-ws-url", "", "Pin the spot WebSocket gateway URL for this invocation (dev/testing); has no effect on a futures operation")
		futuresWSURLPtr = new(string)
		cmd.Flags().StringVar(futuresWSURLPtr, "futures-ws-url", "", "Pin the futures WebSocket gateway URL for this invocation (dev/testing); has no effect on a spot operation")
	}
	return cmd
}

// filesFrom dereferences the --file slice pointer, tolerating nil (an op that
// registered no --file flag, or one invoked with none).
func filesFrom(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}

// guardGroup makes an intermediate node return a structured error + non-zero
// exit code when it is invoked directly or receives an unknown subcommand,
// instead of cobra's default "print help, exit 0" — an unknown command with
// exit 0 would mislead the agent.
func guardGroup(c *cobra.Command, f *Factory, fe *finalErr) {
	guardGroupErr(c, func(err error) { writeAndRecord(f, fe, err) })
}

// guardGroupErr is the guard shared by the engine-internal mountGroup and the
// edition-facing NewGroup: it reports a direct/unknown-subcommand invocation via
// writeErr (envelope + exit code) instead of cobra's "print help, exit 0".
func guardGroupErr(c *cobra.Command, writeErr func(error)) {
	c.Args = cobra.ArbitraryArgs
	c.RunE = func(cmd *cobra.Command, args []string) error {
		var err error
		if len(args) == 0 {
			err = errs.Validation(errs.SubtypeMissingArgument,
				"%q expects a subcommand", cmd.CommandPath()).
				WithHint("run `%s --help` to list available subcommands", cmd.CommandPath())
		} else {
			err = errs.Validation(errs.SubtypeInvalidArgument,
				"unknown subcommand %q for %q", args[0], cmd.CommandPath()).
				WithHint("run `%s --help` to list available subcommands", cmd.CommandPath())
		}
		writeErr(err)
		return err
	}
}
