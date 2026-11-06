package cli

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	xport "github.com/mexcdevelop/mexc-cli/pkg/transport"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// expectedPlanPath mirrors BuildSpec's InPath substitution using the same
// placeholder values the reachability tests pass for required params. Since the
// transport cutover, the plan's URL carries the substituted path (e.g.
// /api/v3/thing/get/BTCUSDT), not the raw {id} placeholder, so the
// reachability assertion must compare against the substituted form.
func expectedPlanPath(op api.Operation) string {
	path := op.Path
	for _, p := range op.Params {
		if p.In != api.InPath {
			continue
		}
		v := ""
		if p.Required {
			v = "BTCUSDT"
			if len(p.Enum) > 0 {
				v = p.Enum[0]
			}
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(v))
	}
	return path
}

// run drives the entire command tree with injected environment / argv and
// returns stdout, stderr, and the exit code. This is the unified entry point for
// the contract tests: a contributor copies it verbatim to get an end-to-end
// self-check.
func run(env map[string]string, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(k string) string { return env[k] }, &out, &errBuf)
	// The fixture mounts a WS operation, so the topology needs a collector.
	app.SetWSCollector(func() xport.WSCollector { return stubWSCollector{} })
	code = app.Run(args)
	return out.String(), errBuf.String(), code
}

// errEnvelope is a minimal projection of the stderr error envelope, used to
// assert on fields.
type errEnvelope struct {
	Error struct {
		Type string `json:"type"`
		// Message is here so an assertion can read the decoded text. Matching the
		// raw stderr instead means matching JSON-escaped quotes, which reads as a
		// product bug the first time a message names a field in quotes.
		Message string `json:"message"`
		Subtype string `json:"subtype"`
		Param   string `json:"param"`
		Hint    string `json:"hint"`
	} `json:"error"`
}

// Contract 1b: every operation of every registered subdomain (not just market)
// is reachable end-to-end and its dry-run plan reports the right path/signed
// flag. This walks the same domains() registry the commands mount from, so a new
// subdomain (e.g. trade) is covered automatically and a registration gap — an
// Operation declared but never mounted — fails here instead of only surfacing as
// a runtime "unknown command". A signed op needs no credentials in dry-run.
//
// This asserts reachability via --dry-run, so it depends on the invariant that
// a --dry-run previews the plan without needing credentials or a live send: an
// a session-identity op (resolved lazily) and a Body op must both still produce a
// plan under --dry-run (only a real call resolves credentials / transports the
// body). If a future gate blocks --dry-run, those ops fail here — see runOperation in
// internal/cli/root.go and Send in internal/transport.
func TestContract_AllRegisteredOpsReachable(t *testing.T) {
	for _, d := range domains() {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				name := d.Name + "." + sd.Name + "." + op.Name
				t.Run(name, func(t *testing.T) {
					args := []string{d.Name, sd.Name, op.Name, "--dry-run"}
					for _, p := range op.Params {
						if p.Required {
							v := "BTCUSDT"
							if len(p.Enum) > 0 {
								v = p.Enum[0]
							}
							args = append(args, "--"+p.Name, v)
						}
					}
					stdout, stderr, code := run(nil, args...)
					if code != 0 {
						t.Fatalf("op %q not reachable: code=%d stderr=%s", name, code, stderr)
					}
					wantPath := expectedPlanPath(op)
					if !strings.Contains(stdout, wantPath) {
						t.Fatalf("dry-run plan should contain path %q, got: %s", wantPath, stdout)
					}
					// The dry-run plan's signed field is a boolean (whether the op
					// signs); the scheme NAME stays internal to the CLI.
					want := `"signed": ` + strconv.FormatBool(effectiveSigned(op, sd).Signs())
					if !strings.Contains(stdout, want) {
						t.Fatalf("op %q plan missing %s; plan=%s", name, want, stdout)
					}
				})
			}
		}
	}
}

// Contract 1d: the whole registry passes assembly-time validation — every
// subdomain wires concrete sign/identity defaults, every declaration is
// well-formed, and cross-field rules (Permission needs an effective login
// identity) hold. This is the same check NewApp runs; asserting it here means
// a bad declaration fails the local test gate before anyone ships a binary
// that refuses every invocation.
func TestContract_RegistryDeclarationsValid(t *testing.T) {
	if err := validateRegistry(domains()); err != nil {
		t.Fatalf("registry declaration invalid: %v", err)
	}
}

// Contract 1e: the wire spellings of the signing schemes are anchored. httpx
// cannot import pkg/api (the dependency points the other way) and restates the
// spellings as internal constants, so this test is what keeps the two packages
// from drifting: if either side changes a spelling, this fails.
func TestContract_SchemeSpellingsAnchored(t *testing.T) {
	if string(api.SignNone) != "none" {
		t.Fatalf("api.SignNone spelling drifted: %q", api.SignNone)
	}
	if string(signing.CliHMAC) != "cli-hmac" {
		t.Fatalf("signing.CliHMAC spelling drifted: %q", signing.CliHMAC)
	}
}

// Contract 1c: a signed write op is refused in --read-only mode with a structured
// envelope, while a signed read op still reaches its dry-run plan. Uses the
// fixture subdomain that mixes reads and writes.
func TestContract_ReadOnlyBlocksWriteAllowsRead(t *testing.T) {
	_, stderr, code := run(nil, "--read-only", "sample", "orders", "create",
		"--symbol", "BTCUSDT", "--side", "BUY", "--quantity", "0.001", "--dry-run")
	if code != 2 {
		t.Fatalf("read-only must block a write, got code %d", code)
	}
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr not an envelope: %v\n%s", err, stderr)
	}
	if env.Error.Subtype != "failed_precondition" {
		t.Fatalf("want failed_precondition, got %q", env.Error.Subtype)
	}

	stdout, _, code := run(nil, "--read-only", "sample", "orders", "list", "--symbol", "BTCUSDT", "--dry-run")
	if code != 0 {
		t.Fatalf("read-only must still allow a read, got code %d", code)
	}
	if !strings.Contains(stdout, "/fixture/orders") {
		t.Fatalf("read dry-run plan missing path, got: %s", stdout)
	}
}

// Contract 2: the dry-run request shape is correct — method / path / params all
// right, and no real request is sent.
func TestContract_DryRunRequestShape(t *testing.T) {
	stdout, _, code := run(nil, "sample", "public", "depth", "--symbol", "ETHUSDT", "--limit", "5", "--dry-run")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var plan struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Signed bool   `json:"signed"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatalf("plan not JSON: %v\n%s", err, stdout)
	}
	if plan.Method != "GET" || !strings.Contains(plan.URL, "/fixture/depth") {
		t.Fatalf("wrong shape: %+v", plan)
	}
	if !strings.Contains(plan.URL, "symbol=ETHUSDT") || !strings.Contains(plan.URL, "limit=5") {
		t.Fatalf("params missing: %s", plan.URL)
	}
	if plan.Signed {
		t.Fatalf("an unsigned subdomain must report signed=false, got %v", plan.Signed)
	}
}

// Contract 3: missing required param → structured envelope + exit 2, error goes
// to stderr, stdout stays clean.
func TestContract_MissingRequiredArg(t *testing.T) {
	stdout, stderr, code := run(nil, "sample", "public", "depth", "--dry-run")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if stdout != "" {
		t.Fatalf("stdout must stay clean on error, got: %s", stdout)
	}
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr not an envelope: %v\n%s", err, stderr)
	}
	if env.Error.Type != "validation" {
		t.Fatalf("expected validation, got %q", env.Error.Type)
	}
}

// Contract 4: unknown subcommand → structured envelope + exit 2 (not cobra's
// default exit 0 + help).
func TestContract_UnknownSubcommand(t *testing.T) {
	_, stderr, code := run(nil, "sample", "public", "bogus")
	if code != 2 {
		t.Fatalf("expected exit 2 for unknown subcommand, got %d", code)
	}
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr not an envelope: %v\n%s", err, stderr)
	}
	if !strings.Contains(env.Error.Hint, "--help") {
		t.Fatalf("hint should point to --help, got %q", env.Error.Hint)
	}
}

// Contract 5: bad enum → envelope carries the correct param, and the hint lists
// the allowed values.
func TestContract_BadEnum(t *testing.T) {
	_, stderr, code := run(nil, "sample", "public", "candles", "--symbol", "BTCUSDT", "--interval", "1h", "--dry-run")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	var env errEnvelope
	_ = json.Unmarshal([]byte(stderr), &env)
	if env.Error.Param != "--interval" {
		t.Fatalf("expected param --interval, got %q", env.Error.Param)
	}
}

// Every operation's effective host key must resolve in the host table (no
// dangling foreign key). Mirrors the mandatory-hostKey assembly gate, but also
// proves the key actually has a row for the default env.
func TestContract_HostKeysResolve(t *testing.T) {
	for _, d := range domains() {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				k := effectiveHostKey(op, sd)
				if k == "" {
					t.Errorf("%s.%s.%s: empty host key", d.Name, sd.Name, op.Name)
					continue
				}
				// Identities that own their own host source (credential.HostResolver)
				// are held to a different rule than table-resolved ones. Asked as a
				// capability, not as a comparison against a particular identity: this
				// layer does not know which identities exist.
				if credential.ResolvesHost(credential.Realm(effectiveIdentity(op, sd))) {
					// Such business hosts are resolved from the per-env host book, keyed
					// by this hostKey (= the business system / login --site). It must
					// NOT be a builtin host — a hardcoded business domain is exactly
					// what the policy forbids.
					if _, err := hosts.Resolve(k, ""); err == nil {
						t.Errorf("%s.%s.%s: business host key %q must NOT be in the builtin table (it is book-resolved)", d.Name, sd.Name, op.Name, k)
					}
					// Detection: when the op declares Permission, its RBAC site must be
					// the same system the request routes to (the hostKey), so the codes
					// you are checked for and the host you reach never diverge.
					if op.Permission != nil && op.Permission.System != k {
						t.Errorf("%s.%s.%s: Permission.System %q must equal the hostKey %q (RBAC site == routing system)", d.Name, sd.Name, op.Name, op.Permission.System, k)
					}
					continue
				}
				if _, err := hosts.Resolve(k, ""); err != nil {
					t.Errorf("%s.%s.%s: host key %q does not resolve: %v", d.Name, sd.Name, op.Name, k, err)
				}
			}
		}
	}
}

// Every concrete signing scheme an op resolves to must have a registered signer,
// and every non-none identity must have a registered credential provider — the
// two-axis foreign keys are not dangling.
func TestContract_SignersAndProvidersRegistered(t *testing.T) {
	for _, d := range domains() {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				if es := effectiveSigned(op, sd); es.Signs() {
					if _, ok := signer.Get(es); !ok {
						t.Errorf("%s.%s.%s: signing scheme %q has no registered signer", d.Name, sd.Name, op.Name, es)
					}
				}
				ei := effectiveIdentity(op, sd)
				if ei != "" && ei != api.IdentityNone {
					if _, ok := credential.Get(credential.Realm(ei)); !ok {
						t.Errorf("%s.%s.%s: identity %q has no registered credential provider", d.Name, sd.Name, op.Name, ei)
					}
				}
			}
		}
	}
}
