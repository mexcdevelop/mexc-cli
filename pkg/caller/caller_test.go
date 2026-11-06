package caller

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestPrincipalFromReportsAbsence is the test that matters most in this file. A
// face that binds credentials per caller branches on the boolean, and if absence
// were reported as a zero Principal instead, that face would silently serve a
// request with an empty caller id.
func TestPrincipalFromReportsAbsence(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("PrincipalFrom reported a principal on a bare context")
	}

	want := Principal{ID: "c-1", Actor: "someone", Session: "s-1"}
	got, ok := PrincipalFrom(WithPrincipal(context.Background(), want))
	if !ok {
		t.Fatal("PrincipalFrom did not report the principal it was given")
	}
	// Compared field by field: Principal holds a map, so it is not comparable with
	// ==. Attrs is checked separately below.
	if got.ID != want.ID || got.Actor != want.Actor || got.Session != want.Session {
		t.Fatalf("principal round-trip: got %+v, want %+v", got, want)
	}

	withAttrs := Principal{ID: "c-2", Attrs: map[string]string{"k": "v"}}
	got, ok = PrincipalFrom(WithPrincipal(context.Background(), withAttrs))
	if !ok {
		t.Fatal("PrincipalFrom did not report the principal it was given")
	}
	if got.Attrs["k"] != "v" {
		t.Fatalf("Attrs did not survive the round-trip: %v", got.Attrs)
	}
}

// TestCarryTreatsEmptyAsAbsent pins the equivalence the transport relies on:
// nothing distinguishes "no headers to pass through" from "an empty set", so no
// call site downstream needs a special case for a missing header.
func TestCarryTreatsEmptyAsAbsent(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Carry
	}{
		{"nil", nil},
		{"empty", Carry{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithCarry(context.Background(), tc.in)
			if got := CarryFrom(ctx); got != nil {
				t.Fatalf("CarryFrom returned %v, want nil", got)
			}
			// Ranging over the result must iterate zero times: this is the whole
			// mechanism by which an absent header stays absent instead of being
			// written as an empty value.
			n := 0
			for range CarryFrom(ctx) {
				n++
			}
			if n != 0 {
				t.Fatalf("ranged over %d entries, want 0", n)
			}
		})
	}

	want := Carry{"traceparent": "00-abc-def-01"}
	got := CarryFrom(WithCarry(context.Background(), want))
	if len(got) != 1 || got["traceparent"] != want["traceparent"] {
		t.Fatalf("carry round-trip: got %v, want %v", got, want)
	}
}

// TestInboundMaterialMasksEveryFormattingPath is the reason the type exists. If
// it ever becomes a plain string, or gains a Format/MarshalJSON that returns the
// real value, this fails.
func TestInboundMaterialMasksEveryFormattingPath(t *testing.T) {
	// A sentinel that cannot plausibly appear by accident in a masked rendering.
	const secret = "zzz-sentinel-9f3c-not-a-real-secret"
	m := NewInboundMaterial(secret)

	for _, rendered := range []string{
		fmt.Sprintf("%v", m),
		fmt.Sprintf("%+v", m),
		//nolint:staticcheck // S1025 says to call String() directly, but the point of
		// this case is that the %s VERB is safe: a log line writes %s, not .String().
		fmt.Sprintf("%s", m),
		fmt.Sprint(m),
		// %#v asks for a Go-syntax representation and does NOT consult Stringer, so
		// without GoString it prints the unexported field verbatim. It is included
		// here because dumping a whole struct with %#v is a normal debugging move,
		// which makes it the one path where masking would fail unnoticed.
		fmt.Sprintf("%#v", m),
		// Wrapped in a struct, which is how it would reach a log line in practice.
		// Both verbs, because the wrapper does not change which method fmt calls.
		fmt.Sprintf("%+v", struct{ M InboundMaterial }{m}),
		fmt.Sprintf("%#v", struct{ M InboundMaterial }{m}),
	} {
		if rendered == "" {
			t.Fatal("rendering produced an empty string")
		}
		if strings.Contains(rendered, secret) {
			t.Fatalf("formatting leaked the material: %q", rendered)
		}
	}

	// The other half of the assertion. Without it, an implementation whose Reveal
	// returned "" would pass everything above while silently breaking every face
	// that needs the real value.
	if got := m.Reveal(); got != secret {
		t.Fatalf("Reveal returned %q, want the original value", got)
	}
}

// TestInboundMaterialAbsentIsNotUsable covers the case a face must refuse on:
// there is no way to end up with a present-but-empty material.
func TestInboundMaterialAbsentIsNotUsable(t *testing.T) {
	if NewInboundMaterial("").Present() {
		t.Fatal("empty material reported present")
	}
	if (InboundMaterial{}).Present() {
		t.Fatal("zero material reported present")
	}
	if MaterialFrom(context.Background()).Present() {
		t.Fatal("material from a bare context reported present")
	}
	// Absent material must not enter the context, so it cannot be read back as
	// present by a later stage.
	ctx := WithMaterial(context.Background(), NewInboundMaterial(""))
	if MaterialFrom(ctx).Present() {
		t.Fatal("absent material survived a round-trip as present")
	}

	m := NewInboundMaterial("v")
	if got := MaterialFrom(WithMaterial(context.Background(), m)); !got.Present() || got.Reveal() != "v" {
		t.Fatalf("material round-trip failed: present=%v", got.Present())
	}
}
