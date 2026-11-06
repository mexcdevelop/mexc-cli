package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/caller"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
)

// Property 10 and 17, at the transport's own boundary: passthrough headers are
// forwarded byte for byte, and a header the caller did not send is ABSENT rather
// than present-and-empty.
//
// The absence assertion checks the header MAP, not the value: asserting `== ""`
// would pass for a header that was written empty, which is the mistake this pins
// against — a downstream that receives an empty trace header behaves differently
// from one that receives none.
//
// _Validates: R9.2, R9.4_
func TestCarryHeadersAreForwardedVerbatimAndAbsenceIsAbsence(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// A value with mixed case, padding-looking characters and a dash — anything a
	// well-meaning normalizer might "clean up".
	const raw = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	c := New(config.Config{})
	c.BaseURLOverride = srv.URL
	spec := api.RequestSpec{Method: http.MethodGet, Path: "/x", Signed: api.SignNone, Identity: api.IdentityNone}

	ctx := caller.WithCarry(context.Background(), caller.Carry{"X-Passthrough-One": raw})
	var dst any
	if _, err := c.Send(ctx, spec, &dst); err != nil {
		t.Fatalf("send: %v", err)
	}

	if v := got.Get("X-Passthrough-One"); v != raw {
		t.Fatalf("passthrough header was rewritten:\n got  %q\n want %q", v, raw)
	}
	if _, present := got["X-Passthrough-Absent"]; present {
		t.Fatal("a header the caller never sent was present in the outbound request")
	}
}

// TestCarryNeverOutranksRequestHeaders pins the precedence: a value that arrived
// from outside must not displace an operation's own header or an identity's
// injected credential.
func TestCarryNeverOutranksRequestHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(config.Config{})
	c.BaseURLOverride = srv.URL
	spec := api.RequestSpec{
		Method:   http.MethodGet,
		Path:     "/x",
		Signed:   api.SignNone,
		Identity: api.IdentityNone,
		Header:   map[string]string{"X-Contested": "from-request"},
	}

	ctx := caller.WithCarry(context.Background(), caller.Carry{"X-Contested": "from-carry"})
	var dst any
	if _, err := c.Send(ctx, spec, &dst); err != nil {
		t.Fatalf("send: %v", err)
	}
	if v := got.Get("X-Contested"); v != "from-request" {
		t.Fatalf("a passthrough header displaced the request's own: got %q", v)
	}
}

// TestCarryAbsentContextAddsNothing is the zero-value path every existing face
// takes: no Carry in the context must mean no extra header, with no special case
// anywhere to make that true.
func TestCarryAbsentContextAddsNothing(t *testing.T) {
	var withCarry, without http.Header
	target := &withCarry
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*target = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(config.Config{})
	c.BaseURLOverride = srv.URL
	spec := api.RequestSpec{Method: http.MethodGet, Path: "/x", Signed: api.SignNone, Identity: api.IdentityNone}

	var dst any
	target = &withCarry
	if _, err := c.Send(caller.WithCarry(context.Background(), caller.Carry{}), spec, &dst); err != nil {
		t.Fatalf("send with empty carry: %v", err)
	}
	target = &without
	if _, err := c.Send(context.Background(), spec, &dst); err != nil {
		t.Fatalf("send with no carry: %v", err)
	}

	if len(withCarry) != len(without) {
		t.Fatalf("an empty Carry changed the outbound header set: %v vs %v", withCarry, without)
	}
}
