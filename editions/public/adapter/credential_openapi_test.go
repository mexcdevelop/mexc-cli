package adapter

import (
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

func TestOpenAPIRealm(t *testing.T) {
	if got := (OpenAPI{}).Realm(); got != credential.Realm(identity.OpenAPI) {
		t.Fatalf("Realm() = %q, want %q", got, identity.OpenAPI)
	}
}

// With no profile on disk the identity reports "requires API credentials"
// rather than resolving empty ones — the write precheck depends on that being an
// error, not a blank credential set.
func TestOpenAPIResolveMissingCredentials(t *testing.T) {
	isolatedHome(t)
	if _, err := (OpenAPI{}).Resolve(config.Config{}, ""); err == nil {
		t.Fatalf("expected an error for missing credentials, got nil")
	}
}

// The identity loads its own credentials from a profile. Config carries none —
// that is the point: how a product stores credentials is the identity's
// business, not the shared engine's.
func TestOpenAPIResolveFromActiveProfile(t *testing.T) {
	isolatedHome(t)
	if err := profile.Save(profile.Profile{Name: "p", APIKey: "k", SecretKey: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := profile.SetActive("p"); err != nil {
		t.Fatal(err)
	}
	cred, err := (OpenAPI{}).Resolve(config.Config{}, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if cred.APIKey != "k" || cred.Secret != "s" || cred.Token != "" || cred.Extra != nil {
		t.Fatalf("Resolve() = %+v, want {APIKey:k Secret:s}", cred)
	}
}

// An explicitly selected profile wins over the active one — the user named it.
func TestOpenAPIResolveSelectedProfileWins(t *testing.T) {
	isolatedHome(t)
	for _, p := range []profile.Profile{
		{Name: "active", APIKey: "ka", SecretKey: "sa"},
		{Name: "chosen", APIKey: "kc", SecretKey: "sc"},
	} {
		if err := profile.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := profile.SetActive("active"); err != nil {
		t.Fatal(err)
	}
	o := OpenAPI{SelectProfile: func() string { return "chosen" }}
	cred, err := o.Resolve(config.Config{}, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if cred.APIKey != "kc" {
		t.Fatalf("Resolve() used %q, want the explicitly selected profile", cred.APIKey)
	}
}

// A named profile that does not exist is a HARD error, not a downgrade to
// "no credentials": the user asked for that account, so running against another
// one would be worse than failing.
func TestOpenAPIResolveSelectedProfileMissingIsFatal(t *testing.T) {
	isolatedHome(t)
	if err := profile.Save(profile.Profile{Name: "active", APIKey: "ka", SecretKey: "sa"}); err != nil {
		t.Fatal(err)
	}
	if err := profile.SetActive("active"); err != nil {
		t.Fatal(err)
	}
	o := OpenAPI{SelectProfile: func() string { return "nope" }}
	if _, err := o.Resolve(config.Config{}, ""); err == nil {
		t.Fatal("a named profile that cannot be loaded must fail, not fall back")
	}
}

func TestOpenAPIInject(t *testing.T) {
	spec := &api.RequestSpec{}
	if err := (OpenAPI{}).Inject(spec, credential.Credentials{APIKey: "k"}); err != nil {
		t.Fatalf("Inject() error = %v, want nil", err)
	}
	if got := spec.Header[APIKeyHeader]; got != "k" {
		t.Fatalf("spec.Header[%q] = %q, want %q", APIKeyHeader, got, "k")
	}
}

// The provider names the api-key header as reserved so the assembly layer can
// reject a colliding business param. (That the registry honors it end-to-end is
// covered by the edition's e2e reachability test.)
func TestOpenAPIReservedHeaders(t *testing.T) {
	if got := (OpenAPI{}).ReservedHeaders(); len(got) != 1 || got[0] != APIKeyHeader {
		t.Fatalf("openapi must reserve the api-key header, got %v", got)
	}
}

// A broken ACTIVE profile is not a hard error: it reports "requires API
// credentials", the same as having none. The distinction matters — a hard error
// here would block credential-free commands for a user whose active pointer went
// stale, while the user who explicitly named a profile still gets a loud failure
// (see the test above).
func TestOpenAPIResolveBrokenActiveProfileDegrades(t *testing.T) {
	isolatedHome(t)
	if err := profile.SetActive("ghost"); err != nil { // points at a profile that does not exist
		t.Fatal(err)
	}
	_, err := (OpenAPI{}).Resolve(config.Config{}, "")
	if err == nil {
		t.Fatal("expected a missing-credentials error")
	}
	if !strings.Contains(err.Error(), "requires API credentials") {
		t.Fatalf("a broken active profile should read as missing credentials, got: %v", err)
	}
}
