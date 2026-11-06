package cli

import (
	"context"
	"testing"

	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/caller"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
)

// TestDepsClientCarriesNoCallerAndClientForDoes pins the distinction between the
// two Deps senders, which is easy to erase by "simplifying" one into the other.
//
// Client is for work belonging to the invocation (reporting an audit record,
// answering a probe): it must carry NO caller, so on a face that binds credentials
// per caller it resolves nothing and authenticates as the product. ClientFor is for
// work done on behalf of a caller and must carry that caller through.
//
// Without this test, changing DepsAdapter.Client to reuse a request context — or
// ClientFor to drop it — would compile, pass everything else, and silently make
// invocation-level work speak as whichever caller happened to be in scope.
func TestDepsClientCarriesNoCallerAndClientForDoes(t *testing.T) {
	var seen []context.Context
	f := &Factory{
		Config: func() config.Config { return config.Config{} },
		Client: func(ctx context.Context) *transport.Client {
			seen = append(seen, ctx)
			return &transport.Client{DryRun: true}
		},
	}
	d := &DepsAdapter{f: f}

	// Client(): the context it builds with must not carry a principal.
	_ = d.Client()
	if len(seen) != 1 {
		t.Fatalf("Client() built %d clients, want 1", len(seen))
	}
	if _, ok := caller.PrincipalFrom(seen[0]); ok {
		t.Fatal("Deps.Client() passed a context carrying a caller; " +
			"invocation-level work must authenticate as the product, not as a caller")
	}

	// ClientFor(ctx): the caller must survive, because that is the entire point.
	want := caller.Principal{ID: "c-1"}
	_ = d.ClientFor(caller.WithPrincipal(context.Background(), want))
	if len(seen) != 2 {
		t.Fatalf("ClientFor() built %d clients, want 2 total", len(seen))
	}
	got, ok := caller.PrincipalFrom(seen[1])
	if !ok {
		t.Fatal("Deps.ClientFor() dropped the caller from its context")
	}
	if got.ID != want.ID {
		t.Fatalf("ClientFor carried principal %q, want %q", got.ID, want.ID)
	}
}
