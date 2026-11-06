package hosts

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// TestMain registers rows for testPublicKey, the example host these mechanism tests
// (Resolve / override chain / Keys) exercise. The real openapi rows now live in
// editions/public and are injected at Run time; the pkg/hosts table itself ships
// empty, so the tests register their own equivalent to stay self-contained.
// testPublicKey stands in for a product's public host key. This package must
// not name a real one (Invariant 16), and the table it resolves against is
// injected anyway, so the tests register their own.
const (
	testPublicKey = "public-example"
	// The override variable envVarName derives from testPublicKey. Spelled out
	// rather than computed, so a change to that derivation fails the test
	// instead of following it.
	testPublicKeyEnvVar = "MEXC_PUBLIC_EXAMPLE_BASE_URL"
)

func TestMain(m *testing.M) {
	// A product names its default env; Resolve substitutes it for an unselected
	// one, so no host registers a blank row.
	SetDefaultEnv("prod")
	// Synthetic endpoints. This package resolves a (key, env) pair; whose hosts
	// they are is the edition's business, and naming a real one here would put a
	// product's endpoint back into the shared layer that deliberately ships none.
	Register(Registration{Key: testPublicKey, Rows: map[Env]Endpoint{
		"prod":        {URL: "https://prod.example"},
		"example-alt": {URL: "https://alt.example"},
	}})
	// A synthetic no-override key exercises that policy (never honors a
	// MEXC_<KEY>_BASE_URL override) independent of any edition's concrete hosts,
	// which now live in editions/*.
	Register(Registration{Key: "fixed-example", NoOverride: true, Rows: map[Env]Endpoint{
		"prod":        {URL: "https://fixed.example"},
		"example-env": {URL: "https://restricted.example"},
	}})
	os.Exit(m.Run())
}

func TestResolve_OpenAPI_Production(t *testing.T) {
	ep, err := Resolve(testPublicKey, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if ep.URL != "https://prod.example" {
		t.Fatalf("URL = %q, want %q", ep.URL, "https://prod.example")
	}
}

// A non-default env resolves to its own row, not to the default's.
func TestResolve_NonDefaultEnv(t *testing.T) {
	ep, err := Resolve(testPublicKey, "example-alt")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if ep.URL != "https://alt.example" {
		t.Fatalf("URL = %q, want %q", ep.URL, "https://alt.example")
	}
}

func TestResolve_EnvVarOverride_WinsOverBuiltin(t *testing.T) {
	t.Setenv(testPublicKeyEnvVar, "https://custom")

	ep, err := Resolve(testPublicKey, "")
	if err != nil {
		t.Fatalf("Resolve() error = %v, want nil", err)
	}
	if ep.URL != "https://custom" {
		t.Fatalf("URL = %q, want %q", ep.URL, "https://custom")
	}
}

func TestResolve_UnknownKey_Errors(t *testing.T) {
	_, err := Resolve("nope", "")
	if err == nil {
		t.Fatal("Resolve() error = nil, want non-nil for unknown key")
	}
}

func TestResolve_UnknownEnv_Errors(t *testing.T) {
	_, err := Resolve(testPublicKey, "staging")
	if err == nil {
		t.Fatal("Resolve() error = nil, want non-nil for unknown env")
	}
	// Picking an env this host does not serve is a user/config problem, not an
	// internal bug: it is a validation error whose hint lists the host's envs.
	var e *errs.Error
	if !errors.As(err, &e) || e.Type != errs.TypeValidation || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("err = %v, want validation/invalid_argument", err)
	}
	if !strings.Contains(e.Hint, "example-alt") || !strings.Contains(e.Hint, "prod") {
		t.Fatalf("hint should list the host's envs, got: %q", e.Hint)
	}
}

// A no-override host key NEVER honors a MEXC_<KEY>_BASE_URL override: such a host
// must not be redirected by the environment. An ordinary key still does. Uses the
// synthetic no-override key registered in TestMain, so the mechanism is tested
// without any edition's concrete host.
func TestNoOverrideKeyIgnoresEnvOverride(t *testing.T) {
	const fixedKey = "fixed-example"
	if !IsNoOverride(fixedKey) {
		t.Fatalf("%q must be marked no-override", fixedKey)
	}
	if IsNoOverride(testPublicKey) {
		t.Fatalf("openapi must not be no-override")
	}

	t.Setenv("MEXC_FIXED_EXAMPLE_BASE_URL", "https://redirected.example")
	ep, err := Resolve(fixedKey, "")
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", fixedKey, err)
	}
	if ep.URL == "https://redirected.example" {
		t.Fatalf("internal key must ignore MEXC_*_BASE_URL, got %q", ep.URL)
	}

	// A public key still honors it, so the test proves the skip is key-scoped.
	t.Setenv(testPublicKeyEnvVar, "https://pinned.example")
	ep, err = Resolve(testPublicKey, "")
	if err != nil {
		t.Fatalf("Resolve(openapi) error = %v", err)
	}
	if ep.URL != "https://pinned.example" {
		t.Fatalf("public key should honor MEXC_*_BASE_URL, got %q", ep.URL)
	}
}

func TestKeys_ContainsOpenAPI_Sorted(t *testing.T) {
	ks := Keys()

	found := false
	for _, k := range ks {
		if k == testPublicKey {
			found = true
		}
	}
	if !found {
		t.Fatalf("Keys() = %v, want to contain %q", ks, testPublicKey)
	}
	if !sort.StringsAreSorted(ks) {
		t.Fatalf("Keys() = %v, want sorted", ks)
	}
}
