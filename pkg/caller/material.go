package caller

import "context"

// InboundMaterial is secret material the caller supplied with its request, for a
// face whose callers authenticate to the backend THEMSELVES rather than having
// this process do it for them.
//
// It is deliberately NOT a field of Principal, and not a Carry entry. Principal
// flows into the audit record, and Carry is written onto every outbound request —
// including the one that reports that record. Either would put a directly usable
// credential somewhere it can be read back later.
//
// Keeping it a distinct type with a masking String() makes "this is not recorded"
// a property of the VALUE rather than a rule every sink has to remember: a %v in
// a log line, an error message or a dry-run plan cannot leak it. Reveal() is
// named so a reviewer can grep every place the real value is taken out, and so
// that taking it looks deliberate at the call site.
//
// Nothing in this module produces one. A face that reads such material off its
// transport constructs it; a face that does not is unaffected, because the zero
// value reports absent and never enters any path.
type InboundMaterial struct{ value string }

// NewInboundMaterial wraps material a face read off its transport.
//
// An empty value yields the zero InboundMaterial, which reports absent — so "the
// header was missing" and "the header was empty" are one case, and neither can be
// mistaken for a usable credential.
func NewInboundMaterial(v string) InboundMaterial {
	return InboundMaterial{value: v}
}

// Present reports whether this carries anything. A face that expects material and
// gets none MUST refuse rather than fall through to another strategy.
func (m InboundMaterial) Present() bool { return m.value != "" }

// String masks the value. This is the whole reason the type exists: it is what
// makes %v, %+v and %s on this value — or on any struct holding one — safe.
func (m InboundMaterial) String() string { return "[redacted]" }

// GoString masks the value under %#v.
//
// This is not redundant with String. The %#v verb asks for a Go-syntax
// representation and does NOT consult Stringer: without this method it prints the
// struct literal including the unexported field, so `%#v` on this value — or on
// any struct that holds one, which is the common case when dumping state — would
// print the secret in full. That makes it the one formatting path where a type
// whose entire purpose is masking would silently fail.
func (m InboundMaterial) GoString() string { return "caller.InboundMaterial{[redacted]}" }

// Reveal returns the real value.
//
// Every caller of this is a place a secret leaves its wrapper; there should be
// exactly one in a well-built face, where the material is injected into the
// outbound request.
func (m InboundMaterial) Reveal() string { return m.value }

type materialKey struct{}

// WithMaterial returns ctx carrying m.
//
// Same shape as WithPrincipal, for the same reason: the face that reads the
// transport and the provider that uses the value are on opposite sides of a
// machinery layer that must not know about either. Absent material does not enter
// the context at all, so MaterialFrom cannot report a present-but-empty value.
func WithMaterial(ctx context.Context, m InboundMaterial) context.Context {
	if !m.Present() {
		return ctx
	}
	return context.WithValue(ctx, materialKey{}, m)
}

// MaterialFrom returns the material ctx carries.
//
// The zero value reports absent, so a face that finds nothing and a face that was
// given an empty header take the same branch — and that branch must be a refusal,
// never a fallback to credentials this process holds on its own behalf.
func MaterialFrom(ctx context.Context) InboundMaterial {
	m, _ := ctx.Value(materialKey{}).(InboundMaterial)
	return m
}
