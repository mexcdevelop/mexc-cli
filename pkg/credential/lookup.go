package credential

import "context"

// Lookup answers which Provider serves a realm.
//
// It exists so the transport can ask a QUESTION instead of reading a package-level
// map. The process-wide registry is one answer and is the default; a face that
// serves many callers from one process supplies a per-request answer, so the
// credentials a request is made with can depend on who asked for it.
//
// Note what this deliberately does NOT do: it does not add a context to Resolve.
// The binding happens one layer earlier — while the request's client is being
// built — so a Provider stays a plain object that knows one identity's material
// and nothing about requests. That is what keeps this change additive: no
// existing provider, in this module or in any product, is touched.
type Lookup interface {
	Provider(r Realm) (Provider, bool)
}

// processRegistry is the Lookup backed by Register/Get — the default, and the
// only one a single-account face ever needs.
type processRegistry struct{}

func (processRegistry) Provider(r Realm) (Provider, bool) { return Get(r) }

// Registry returns the process-wide Lookup.
//
// A nil Lookup anywhere in this module means this one, so every existing call
// site keeps its behaviour unchanged.
func Registry() Lookup { return processRegistry{} }

// ReservedHeadersVia is ReservedHeaders asked of a specific Lookup.
//
// It exists because the transport must ask the SAME source for the reserved
// header names that it asks for the provider itself. Reading the process registry
// while resolving credentials per caller would answer from a provider that is not
// the one serving this request — and on a face where the registered provider is a
// refusing placeholder, that answer would be silently empty.
func ReservedHeadersVia(l Lookup, r Realm) []string {
	if l == nil {
		l = Registry()
	}
	if p, ok := l.Provider(r); ok {
		if hr, ok := p.(HeaderReserver); ok {
			return hr.ReservedHeaders()
		}
	}
	return nil
}

// Binder decides, per request, which Lookup that request's credentials come from.
//
// A face supplies one only when its callers differ from each other. When it does,
// Bind is consulted on EVERY request, and the composition root is the single
// place where that choice is expressed — there is no capability to probe and no
// type assertion to satisfy, so a face that does not supply a Binder cannot
// accidentally acquire per-caller credentials, and one that does cannot
// accidentally lose them.
//
// Bind MUST fail closed. When the context carries nothing to bind to, return a
// Lookup that refuses rather than one that answers from process-wide state:
// falling back is exactly the single-account behaviour a Binder exists to end,
// and it would come back silently.
type Binder interface {
	Bind(ctx context.Context) Lookup
}
