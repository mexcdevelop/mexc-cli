package signer

import (
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

// The schemes below are local stubs. This package is the port: it must work
// without knowing that any particular scheme exists, so testing it against a
// real product's scheme would test the wrong thing — and naming one here would
// be the very coupling Invariant 16 forbids.

// plainScheme implements Signer and nothing else: no capabilities.
type plainScheme struct{ name api.Signing }

func (s plainScheme) Scheme() api.Signing                                          { return s.name }
func (plainScheme) Sign(*api.RequestSpec, credential.Credentials, time.Time) error { return nil }

// reservingScheme also implements ParamReserver.
type reservingScheme struct {
	plainScheme
	params []string
}

func (s reservingScheme) ReservedParams() []string { return s.params }

func TestGet_RegisteredAndUnknown(t *testing.T) {
	const scheme api.Signing = "registry-test-plain"
	Register(plainScheme{name: scheme})

	if got, ok := Get(scheme); !ok || got.Scheme() != scheme {
		t.Fatalf("Get(%q) = (%v, %v), want the registered scheme", scheme, got, ok)
	}
	if _, ok := Get("registry-test-never-registered"); ok {
		t.Fatal("Get reported a scheme that was never registered")
	}
}

func TestReservedParams(t *testing.T) {
	const (
		reserving api.Signing = "registry-test-reserving"
		plain     api.Signing = "registry-test-not-reserving"
	)
	Register(reservingScheme{plainScheme{name: reserving}, []string{"timestamp", "signature"}})
	Register(plainScheme{name: plain})

	got := ReservedParams(reserving)
	if len(got) != 2 || got[0] != "timestamp" || got[1] != "signature" {
		t.Fatalf("ReservedParams(%q) = %v, want [timestamp signature]", reserving, got)
	}
	if got := ReservedParams(plain); got != nil {
		t.Fatalf("a scheme without ParamReserver returned %v, want nil", got)
	}
	if got := ReservedParams("registry-test-absent"); got != nil {
		t.Fatalf("an unregistered scheme returned %v, want nil", got)
	}
}

// Registering the same scheme name twice replaces the earlier entry. An edition
// composes its set once, so this is not a supported workflow — it is pinned
// because a silent merge, or a silent keep-the-first, would be worse.
func TestRegister_LastWins(t *testing.T) {
	const scheme api.Signing = "registry-test-replaced"
	Register(reservingScheme{plainScheme{name: scheme}, []string{"first"}})
	Register(reservingScheme{plainScheme{name: scheme}, []string{"second"}})

	got := ReservedParams(scheme)
	if len(got) != 1 || got[0] != "second" {
		t.Fatalf("ReservedParams(%q) = %v, want [second] — the second Register did not replace the first", scheme, got)
	}
}
