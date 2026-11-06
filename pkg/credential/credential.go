// Package credential resolves per-identity credentials and injects the identity
// material into a request. A Realm is one credential kind (an api-key identity, a
// login-token identity). Providers register per Realm; the transport picks one by the
// operation's effective Identity, resolves its credentials, injects the identity
// material, and hands the SAME credentials to the signer — so signing stays
// decoupled from identity (any identity x any signing scheme).
package credential

import (
	"sort"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// Realm identifies an identity; its string values match the api.Identity constants.
type Realm string

// Credentials is the secret material an identity carries. openapi uses
// APIKey+Secret; a token identity uses Token. Extra holds scheme-specific fields
// without widening the struct. The signer consumes this too.
type Credentials struct {
	APIKey string
	Secret string
	Token  string
	Extra  map[string]string
	// Grants is the RBAC grant map (system -> permission codes) carried by a
	// login-token identity. Used by the provider's Inject for the preflight.
	// Nil for api-key identities.
	Grants map[string][]string
}

// Warnf sinks a non-fatal advisory (e.g. the RBAC preflight heads-up when a
// login's cached permissions do not cover an operation's codes) to the user's
// stderr, alongside every other hint. The cmd layer wires it to the invocation's
// stderr writer; it is a no-op by default so the library and tests stay silent
// unless a sink is set. It is a package var (not threaded through the Provider
// interface) because the warning originates deep in the transport-driven Inject,
// far from where output writers live.
var Warnf = func(string, ...any) {}

// Provider is one identity implementation. Resolve produces the credentials for
// the current config/env (or a login_required / failed_precondition error);
// Inject attaches the identity material (a header, a bearer token) to the request.
type Provider interface {
	Realm() Realm
	Resolve(cfg config.Config, env hosts.Env) (Credentials, error)
	Inject(spec *api.RequestSpec, cred Credentials) error
}

var registry = map[Realm]Provider{}

// Register adds a provider; called from each provider's init().
func Register(p Provider) { registry[p.Realm()] = p }

// Get returns the provider registered for a realm.
func Get(r Realm) (Provider, bool) { p, ok := registry[r]; return p, ok }

// HeaderReserver is an optional Provider capability: an identity that injects a
// fixed request header (an api-key header, ...) names it here so the assembly
// layer can reject a business Param that would collide with — and be silently
// overwritten by — the injected header.
type HeaderReserver interface {
	ReservedHeaders() []string
}

// GrantCarrier is an optional Provider capability: this identity's credentials
// carry an RBAC grant map (Credentials.Grants), so an operation authenticating
// with it may declare a Permission requirement. An identity without it — no
// identity at all, or a per-request api key — has nothing to check codes
// against, and an operation declaring Permission against one is a declaration
// error caught at assembly time.
//
// It is a capability rather than a check against a particular identity value:
// the assembly layer must be able to ask "can this identity carry grants?"
// without knowing which identities exist. Only the edition that supplies a
// provider knows that, and naming one identity here would put a single product's
// vocabulary into the shared core.
type GrantCarrier interface {
	// CarriesGrants distinguishes this capability at the type level; the value
	// it returns is not consulted.
	CarriesGrants()
}

// CarriesGrants reports whether the identity registered for r resolves
// credentials that carry an RBAC grant map. An unregistered realm reports false:
// with no provider there is nothing to carry them.
func CarriesGrants(r Realm) bool {
	p, ok := Get(r)
	if !ok {
		return false
	}
	_, ok = p.(GrantCarrier)
	return ok
}

// ConfirmGrade is what an identity's own governance says about an operation:
// whether a person must confirm it before it runs, beyond the write semantics
// the framework derives on its own, and one line to show that person.
//
// Both strings are supplied by the provider at runtime rather than named here.
// An identity's grading vocabulary belongs to whoever supplies the provider, for
// the same reason a concrete identity does — see the note on Identity in
// pkg/api. The zero value grades nothing, which is what an identity that does
// not grade at all answers.
//
// They are two fields because they answer to different readers and change on
// different clocks. Reason is prose shown to a person and will be reworded;
// Code is what an audit query groups by, so it has to outlive a rewording. A
// single field cannot be both: a trail keyed on the sentence loses its history
// the first time the sentence improves.
type ConfirmGrade struct {
	Confirm bool
	// Code identifies WHY, stably and opaquely. This package does not interpret
	// it and must not: interpreting it would be the moment one product's grading
	// scale entered the layer every product imports.
	Code string
	// Reason is the one line shown to the person being asked.
	Reason string
}

// ConfirmGrader is an optional Provider capability: this identity governs which
// operations need a person's confirmation, in addition to the ones the write
// semantics already do. An identity without it grades nothing, and the
// confirmation gate falls back to the write tier alone.
//
// It takes the operation's permission requirement and its access, because a
// grade can mean different things for a read and for a write and the sentence
// shown to the person differs accordingly. It takes the env for the same reason
// Resolve does: an identity may hold per-env state, and grading against the
// wrong one would answer about a different deployment.
type ConfirmGrader interface {
	ConfirmGrade(env hosts.Env, req *api.PermReq, access api.Access) ConfirmGrade
}

// GradeConfirm asks the identity registered for r how it grades req. A nil req
// has nothing to grade; an unregistered realm and a provider without the
// capability grade nothing.
func GradeConfirm(r Realm, env hosts.Env, req *api.PermReq, access api.Access) ConfirmGrade {
	if req == nil {
		return ConfirmGrade{}
	}
	p, ok := Get(r)
	if !ok {
		return ConfirmGrade{}
	}
	g, ok := p.(ConfirmGrader)
	if !ok {
		return ConfirmGrade{}
	}
	return g.ConfirmGrade(env, req, access)
}

// ResolvesHost reports whether the identity registered for r owns its own host
// source (implements HostResolver), so its endpoints come from the provider
// rather than the builtin host table. An unregistered realm reports false.
//
// Like CarriesGrants, this exists so a caller can ask what an identity CAN do
// instead of comparing against a particular identity value — the set of
// identities is a product's business, not the shared core's.
func ResolvesHost(r Realm) bool {
	p, ok := Get(r)
	if !ok {
		return false
	}
	_, ok = p.(HostResolver)
	return ok
}

// ReservedHeaders returns the request-header names that the provider for realm r
// injects (and would therefore overwrite a colliding business Param), or nil.
func ReservedHeaders(r Realm) []string {
	if p, ok := Get(r); ok {
		if hr, ok := p.(HeaderReserver); ok {
			return hr.ReservedHeaders()
		}
	}
	return nil
}

// Realms returns every registered identity realm, sorted. Used by the status
// command to report each identity's state.
func Realms() []Realm {
	rs := make([]Realm, 0, len(registry))
	for r := range registry {
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return rs
}

// AuthInvalidator is an optional Provider capability: after a request fails, the
// transport offers the provider a chance to recognize its own "session invalid /
// expired" failure, clear any stored session, and remap the error (e.g. to
// login_required). Returning nil means "not my failure, leave the error as is".
type AuthInvalidator interface {
	OnRequestError(spec *api.RequestSpec, env hosts.Env, err error) error
}

// HostResolver is an optional Provider capability: an identity that owns its own
// host source resolves the request's base URL itself, instead of the builtin host
// table. Such an identity's hosts may live in an encrypted per-env host book
// (never built in, never from argv/env), keyed by the op's hostKey — the business
// system, decoupled from Permission.System (which serves RBAC only). The transport
// consults it before the host table AND refuses a --base-url override for such an
// identity, so a credentialed request can never be redirected to an unvetted host.
// A non-nil error fails the request.
type HostResolver interface {
	ResolveHost(spec *api.RequestSpec, env hosts.Env) (string, error)
}
