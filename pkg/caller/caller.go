// Package caller carries the per-request facts a long-lived server face learns
// from its transport, and that the rest of the call is attributed to.
//
// It exists because a face that serves many callers from one process needs a
// place to put "who asked for this" that is neither a credential (which is what
// the call is made WITH, resolved later and cached on a different clock) nor a
// setting (which is the same for every request the process handles). Both of
// those were considered and neither fits: config.Config is invocation-wide and
// is reported verbatim by the settings face, and credential.Credentials is keyed
// by realm and shared across requests.
//
// This module NEVER interprets what is in here. Every field is an opaque string
// that a product's own extractor produced and a product's own provider or sink
// reads back, so this package can ship to any product without carrying one
// product's vocabulary.
package caller

import "context"

// Principal is one request's caller.
//
// ID is the only field anything may make a decision on, and only a product may:
// it is meaningful exactly as far as whoever produced it made it unforgeable.
// The other two are self-declared by the caller and are for attribution only —
// see the note on Actor.
type Principal struct {
	// ID identifies the caller. A product's extractor decides what it is and how
	// strongly it is proven; nothing here can tell a verified id from a string
	// somebody typed, so a product that uses ID to select credentials owes itself
	// proof (its own extractor must prove the input cryptographically).
	ID string
	// Actor names the human or service whose intent triggered this call, as
	// declared BY THE CALLER. It is not verified anywhere in this module and must
	// not drive an authorization decision — it exists so an audit trail can
	// answer "on whose behalf", which no other field can.
	Actor string
	// Session groups the calls of one conversation. Caller-declared, audit only.
	Session string
	// Attrs carries product-specific extras that a product's own sink or provider
	// reads back. Untyped on purpose: a typed field here would put one product's
	// concept in every product's build.
	Attrs map[string]string
}

// Carry is a set of request headers this face received and must pass through to
// the backend UNCHANGED — neither read nor rewritten.
//
// It is separate from Principal for a reason that is easy to lose: the value of
// a passthrough header is that we do not touch it. W3C trace context is the case
// that makes this concrete — its parent header encodes the CURRENT hop's span id,
// so a process that rewrites it without also reporting a span of its own splits
// the trace in two, and both halves look correct with nothing reporting an error.
// Keeping these in a map this module only ever copies makes "do not interpret"
// the structural default rather than a rule to remember.
//
// Which header names travel here is the product's choice, never this module's:
// there is deliberately no name of any specific header anywhere in this package,
// so switching tracing conventions is a change of configuration value and not a
// change of code.
type Carry map[string]string

type principalKey struct{}
type carryKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom reports the principal ctx carries, and whether it carries one.
//
// The boolean matters: a face that binds credentials per caller MUST refuse when
// it is false rather than fall back to something process-wide. Returning a zero
// Principal without saying so would make that mistake invisible.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithCarry returns ctx carrying the passthrough header set.
//
// An empty set is the same as none, which is what every face that received
// nothing has. That equivalence is load-bearing: it is why "the caller sent no
// trace header" needs no special case anywhere downstream — CarryFrom returns
// nil and ranging over a nil map iterates zero times, so the header is simply
// never written. Do not "fix" that by generating a value for a missing header;
// a trace id this process invents has no parent anywhere, which produces a
// broken link that looks healthy.
func WithCarry(ctx context.Context, c Carry) context.Context {
	if len(c) == 0 {
		return ctx
	}
	return context.WithValue(ctx, carryKey{}, c)
}

// CarryFrom returns the passthrough header set ctx carries, or nil.
func CarryFrom(ctx context.Context) Carry {
	c, _ := ctx.Value(carryKey{}).(Carry)
	return c
}
