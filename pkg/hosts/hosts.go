// Package hosts resolves a (hostKey x env) pair to a concrete endpoint, with an
// override chain so a per-key environment variable always wins over the injected
// table. This package ships NO concrete host rows: each edition injects its own
// via Register from its adapter package, so the shared host package carries only
// the resolution machinery and stays free of any product's endpoints.
package hosts

import (
	"os"
	"sort"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// Env selects which deployment of a host to hit. The zero value is not an env
// but the absence of a selection: Effective resolves it to the product's default
// (Edition.DefaultEnv) before any lookup, so nothing downstream has to treat ""
// as a second name for production. A product that declares a default therefore
// has no use for a blank row — see Register.
type Env string

// Endpoint is a resolved host: its base URL plus any env-specific headers (some
// environments are "same URL + a marker header", so the value is not just a URL).
type Endpoint struct {
	URL     string
	Headers map[string]string
	// Overridden is true when URL came from a MEXC_<KEY>_BASE_URL env override
	// rather than the builtin table, so the caller can warn that a credentialed
	// request is going somewhere other than the shipped host.
	Overridden bool
}

// builtin maps hostKey -> env -> endpoint. It starts EMPTY: every row is injected
// at composition time via Register, so no product's endpoint is compiled into the
// shared host package.
var builtin = map[string]map[Env]Endpoint{}

// noOverrideKeys marks host keys whose endpoint must never be redirected. Unlike
// an ordinary key they honor no MEXC_<KEY>_BASE_URL override and cannot be pinned
// with --base-url; the --base-url half is enforced in internal/transport.
//
// The flag says what the key DOES, not why a composition root asked for it. A key
// is registered this way when sending its requests somewhere else would be a
// security problem rather than a configuration choice — an endpoint that
// authenticates, for instance, where a redirect would hand credentials to whoever
// supplied the flag.
var noOverrideKeys = map[string]bool{}

// IsNoOverride reports whether key's endpoint is fixed against redirection.
func IsNoOverride(key string) bool { return noOverrideKeys[key] }

// Registration is a host key with its per-env endpoints and whether the key
// refuses redirection. It is the exported injection form a composition root hands
// to Register, replacing the package-private register() the build-tag files use
// today. Rows maps each Env to its Endpoint.
type Registration struct {
	Key        string
	Rows       map[Env]Endpoint
	NoOverride bool
}

// Register adds a Registration's host rows to the table. It is the exported entry
// point a composition root uses to inject its hosts; a NoOverride registration
// also marks the key un-redirectable (no --base-url, no MEXC_<KEY>_BASE_URL). Same
// effect as the package-private register(), but callable from outside the package.
//
// Do not register a blank env row when the product declares an Edition.DefaultEnv.
// Resolve substitutes that default before every lookup, so a blank row is
// unreachable — dead data that no request can ever select. A blank row is
// meaningful only for a product that declares NO default, where "nothing
// selected" really is its own env. Validate reports the dead ones; the engine
// surfaces it as a wiring error on every invocation.
func Register(reg Registration) {
	builtin[reg.Key] = reg.Rows
	if reg.NoOverride {
		noOverrideKeys[reg.Key] = true
	}
}

// Keys returns every registered host key, sorted. Used by the status command to
// enumerate and resolve all hosts.
// Validate reports host rows that can never be selected, for the engine to
// surface as an assembly-time wiring error the way it surfaces a bad operation
// declaration. Today that is exactly one shape: a blank env row registered by a
// product that declares a default env, which Resolve substitutes past before
// every lookup.
//
// It is a separate call rather than a check inside Register because Register
// runs while the table is still being built and has no error path — and because
// the answer depends on defaultEnv, which the composition root sets first but
// which a test may legitimately change afterwards.
//
// Dead rows are worth failing over: nothing else surfaces one. The row is
// accepted, stored, listed by nothing, and silently never served, so the endpoint
// someone believed they had wired is simply absent.
func Validate() error {
	var dead []string
	if defaultEnv == "" {
		return nil // no default: a blank row is a real, selectable env
	}
	for key, rows := range builtin {
		if _, ok := rows[""]; ok {
			dead = append(dead, key)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	sort.Strings(dead)
	return errs.Internal(
		"host %s registered a blank env row, which can never be selected: this product's "+
			"default env is %q, and Resolve substitutes it before every lookup",
		strings.Join(dead, ", "), defaultEnv).
		WithHint("name the env explicitly in the host's Rows (e.g. %q), or drop the blank row", string(defaultEnv))
}

func Keys() []string {
	ks := make([]string, 0, len(builtin))
	for k := range builtin {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Resolve maps (key, env) to an endpoint. Override precedence:
//  1. MEXC_<KEY>_BASE_URL env var (env-agnostic; an explicit pinned URL) —
//     ordinary keys only; a NoOverride key never honors it, because such a host
//     must not be redirected by the environment either.
//  2. the builtin table (env-indexed)
//
// The per-invocation --base-url override is applied by the caller (httpx) before
// Resolve is consulted (and httpx likewise refuses it for a NoOverride key), so it
// is not handled here. Never a silent fallback:
//   - an unknown host KEY is an internal wiring bug (a subdomain declared a
//     hostKey that was never registered) — errs.Internal;
//   - a known host with no row for the selected env is a user/config problem
//     (the env was picked via --env / MEXC_ENV / `config set env`, and this host
//     does not serve it) — errs.Validation, with the host's defined envs listed.
//
// defaultEnv is the env a product falls back to when none is selected. The
// composition root sets it from Edition.DefaultEnv.
//
// It exists so that "no --env" and the product's production env are ONE value
// with ONE spelling, instead of the zero value being a second name for the same
// thing. While both existed, every host table carried two rows pointing at the
// same URL, the hint had to render the blank as "(default)" — a value the user
// could see but not type — and code that cared about production had to test for
// both spellings. Substituting here, at the single funnel, removes all of that.
var defaultEnv Env

// SetDefaultEnv names the env used when none is selected. Called once by the
// composition root; a product that names none leaves the zero value, and then a
// host must register a row for it explicitly.
func SetDefaultEnv(e Env) { defaultEnv = e }

// Effective resolves the env actually used: the selected one, or the product's
// default when nothing is selected. Callers that report or branch on "which env
// is this" use it so they agree with what Resolve looked up.
func Effective(env Env) Env {
	if env == "" {
		return defaultEnv
	}
	return env
}

// IsProduction reports whether env is this product's production environment.
// A product declares production as its default env, so this lets the transport
// treat a request as production-bound — masking a resolved host in a dry-run
// plan — without the shared layer hard-coding any env name.
//
// A product that declared NO default gets true for every env. That direction is
// deliberate: not knowing which env is production, we cannot rule it out, and the
// two mistakes cost very differently. Masking too much makes one dry-run less
// informative; masking too little puts a real, security-sensitive host into agent
// context and logs, which is the leak the masking exists to prevent.
func IsProduction(env Env) bool {
	if defaultEnv == "" {
		return true
	}
	return Effective(env) == defaultEnv
}

func Resolve(key string, env Env) (Endpoint, error) {
	env = Effective(env)
	// An ordinary key honors the per-key env-var override; a NoOverride key never does.
	if !noOverrideKeys[key] {
		if v := os.Getenv(envVarName(key)); v != "" {
			return Endpoint{URL: v, Overridden: true}, nil
		}
	}
	rows, ok := builtin[key]
	if !ok {
		return Endpoint{}, errs.Internal("unknown host key %q", key).
			WithHint("wire the subdomain's hostKey to a registered host, or add the row in pkg/hosts")
	}
	ep, ok := rows[env]
	if !ok {
		e := errs.Validation(errs.SubtypeInvalidArgument,
			"host %q has no endpoint for env %q", key, env)
		// Name the selectors only where there is something to select. A product
		// serving one env registers no --env, no MEXC_ENV and no `config set env`
		// (framework derives that from this table), so listing all three would send
		// the reader after three things that do not exist.
		if len(Envs()) > 1 {
			return Endpoint{}, e.WithHint(
				"this host serves env: %s — pick one with --env, MEXC_ENV, or `%s config set env`",
				definedEnvs(rows), cliname.Name())
		}
		return Endpoint{}, e.WithHint(
			"this product serves env %s only; there is no env to select", definedEnvs(rows))
	}
	return ep, nil
}

// EnvDefined reports whether ANY registered host serves env, after resolving the
// zero value to the product's default. Nothing is legal merely by being blank:
// if the default env has no row, saying nothing fails exactly like naming it.
//
// It exists so a persisted setting can be checked at the moment it is written.
// An env is only meaningful against the hosts a product registered, and a typo
// stored in the settings file is sticky — every later command fails at host
// resolution, far from the mistake.
func EnvDefined(env Env) bool {
	env = Effective(env)
	for _, rows := range builtin {
		if _, ok := rows[env]; ok {
			return true
		}
	}
	return false
}

// KnownEnvs lists every env served by at least one registered host, sorted —
// every name a caller can actually type. Same rendering as the per-host hint in
// Resolve, so the complaint from `config set env` and the one from a request
// read alike.
func KnownEnvs() string {
	return definedEnvs(toRows(envSet()))
}

// Envs returns every env served by at least one registered host, sorted. It is
// the same fact KnownEnvs formats for a hint, as data.
//
// The composition root reads it to decide whether this product HAS an env axis
// at all: more than one env means there is something to choose between, and only
// then are --env, MEXC_ENV and the `env` setting worth offering. Deriving it from
// the host table rather than from a declaration means the answer cannot drift
// from the endpoints the product actually serves.
func Envs() []Env {
	seen := envSet()
	out := make([]Env, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func envSet() map[Env]bool {
	seen := map[Env]bool{}
	for _, rows := range builtin {
		for e := range rows {
			seen[e] = true
		}
	}
	return seen
}

// toRows adapts a set of envs to the shape definedEnvs formats.
func toRows(seen map[Env]bool) map[Env]Endpoint {
	rows := make(map[Env]Endpoint, len(seen))
	for e := range seen {
		rows[e] = Endpoint{}
	}
	return rows
}

// definedEnvs lists a host's configured env names for an error hint, sorted.
//
// A blank row is handled by whether it can actually be reached. With a product
// default declared, Resolve substitutes it before lookup, so the blank row is
// unreachable and naming it would offer the caller something that does not work
// — it is omitted. With NO default declared, "nothing selected" is a real,
// selectable env, and it is named "(default)" because a caller cannot type a
// blank. Listing it raw produced a leading empty entry: `known env: , prod`.
func definedEnvs(rows map[Env]Endpoint) string {
	names := make([]string, 0, len(rows))
	for e := range rows {
		if e == "" {
			if defaultEnv != "" {
				continue // unreachable: Resolve substitutes the default first
			}
			names = append(names, "(default)")
			continue
		}
		names = append(names, string(e))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// OverrideVarName is the environment variable that redirects a host key, e.g.
// key "openapi" -> MEXC_OPENAPI_BASE_URL, key "some-host" -> MEXC_SOME_HOST_BASE_URL.
//
// Exported because a caller that REPORTS the override has to name it, and the
// only alternative is to spell the variable out at the point of reporting — which
// is what `doctor` did, hard-coding "MEXC_OPENAPI_BASE_URL" in a message while
// this function decided the real one. Two spellings of the same convention drift
// silently: the check keeps naming a variable nothing reads any more.
//
// It says nothing about whether the variable is SET; Resolve answers that, via
// Endpoint.Overridden. Callers that must also see a shadowed override (one that
// loses to --base-url this invocation) read the variable themselves, by this name.
func OverrideVarName(key string) string { return envVarName(key) }

// envVarName builds the per-key override env var, e.g. key "openapi" -> MEXC_OPENAPI_BASE_URL,
// key "some-host" -> MEXC_SOME_HOST_BASE_URL.
func envVarName(key string) string {
	return "MEXC_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_")) + "_BASE_URL"
}
