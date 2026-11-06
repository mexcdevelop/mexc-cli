package hosts

import (
	"strings"
	"testing"
)

// TestEnvDefined_AcrossRegisteredHosts pins the property `config set env` relies
// on: an env is legal iff SOME registered host serves it, and the answer is
// therefore per-product — the two products register different hosts and so
// accept different envs.
func TestEnvDefined_AcrossRegisteredHosts(t *testing.T) {
	orig, origDefault := builtin, defaultEnv
	t.Cleanup(func() { builtin, defaultEnv = orig, origDefault })
	builtin = map[string]map[Env]Endpoint{}
	SetDefaultEnv("prod")

	Register(Registration{Key: "alpha", Rows: map[Env]Endpoint{
		"prod": {URL: "https://a"},
	}})
	Register(Registration{Key: "beta", Rows: map[Env]Endpoint{
		"prod": {URL: "https://b"}, "staging": {URL: "https://b-staging"},
	}})

	// An unselected env resolves to the product's default, so the blank is
	// defined exactly when that default is — not by being blank.
	if !EnvDefined("") {
		t.Error("the blank env must resolve to the default, which IS served here")
	}
	// Defined by ANY host, not by all of them — "staging" is beta's alone.
	for _, e := range []Env{"prod", "staging"} {
		if !EnvDefined(e) {
			t.Errorf("env %q is served by a registered host, must be defined", e)
		}
	}
	if EnvDefined("nope") {
		t.Error("an env no host serves must not be defined")
	}

	// The hint names every env once, sorted. Every name is typeable now: there is
	// no blank row to render as an unusable "(default)".
	got := KnownEnvs()
	if want := "prod, staging"; got != want {
		t.Fatalf("KnownEnvs() = %q, want %q", got, want)
	}
	if strings.Contains(got, "beta") || strings.Contains(got, "https") {
		t.Errorf("KnownEnvs must list envs, not hosts or URLs: %q", got)
	}
}

// A blank row is listed only when it can actually be selected.
//
// With a product default declared, Resolve substitutes it before lookup, so a
// blank row is unreachable — dead data. Listing it raw put a leading empty entry
// in the hint (`known env: , prod`), offering the caller something that does not
// exist. With NO default, "nothing selected" is a real env and is named
// "(default)", because a caller cannot type a blank.
func TestDefinedEnvs_BlankRow(t *testing.T) {
	rows := map[Env]Endpoint{"": {URL: "https://blank.test"}, "prod": {URL: "https://prod.test"}}

	prev := defaultEnv
	// The registration below writes the package-global table; leaving it there
	// would make every later test in this package see a dead blank row (Validate
	// caught exactly that when it was first added).
	defer func() { defaultEnv = prev; delete(builtin, "blankrowtest") }()

	SetDefaultEnv("prod")
	if got := definedEnvs(rows); got != "prod" {
		t.Fatalf("with a default declared, definedEnvs = %q, want the blank row omitted (%q)", got, "prod")
	}
	// The unreachable row must not resolve either — the default wins.
	Register(Registration{Key: "blankrowtest", Rows: rows})
	if ep, err := Resolve("blankrowtest", ""); err != nil || ep.URL != "https://prod.test" {
		t.Fatalf("Resolve(\"\") = %+v err=%v, want the default env's row", ep, err)
	}

	SetDefaultEnv("")
	if got := definedEnvs(rows); got != "(default), prod" {
		t.Fatalf("with no default, definedEnvs = %q, want the blank named (%q)", got, "(default), prod")
	}
}

// A blank row that can never be selected is a wiring error, not a silent no-op.
// Nothing else surfaces one: it is accepted, stored, listed by nothing, and never
// served — so an endpoint someone believed they had wired is simply absent.
func TestValidate_DeadBlankRow(t *testing.T) {
	prev := defaultEnv
	prevRows := builtin["validatetest"]
	defer func() {
		defaultEnv = prev
		if prevRows == nil {
			delete(builtin, "validatetest")
		} else {
			builtin["validatetest"] = prevRows
		}
	}()

	SetDefaultEnv("prod")
	Register(Registration{Key: "validatetest", Rows: map[Env]Endpoint{"prod": {URL: "https://p.test"}}})
	if err := Validate(); err != nil {
		t.Fatalf("a table with no blank row must validate: %v", err)
	}

	Register(Registration{Key: "validatetest", Rows: map[Env]Endpoint{
		"": {URL: "https://dead.test"}, "prod": {URL: "https://p.test"},
	}})
	err := Validate()
	if err == nil {
		t.Fatalf("a blank row under a declared default env must be reported")
	}
	if !strings.Contains(err.Error(), "validatetest") {
		t.Fatalf("the error should name the offending host key, got: %v", err)
	}

	// With no default declared, the same row is a real, selectable env.
	SetDefaultEnv("")
	if err := Validate(); err != nil {
		t.Fatalf("with no default env a blank row is legitimate, got: %v", err)
	}
}
