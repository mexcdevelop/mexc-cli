package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// schema sample.signed.fee → emits the operation's full schema to stdout,
// exit 0, and the schema mirrors the declared Operation (single source of truth).
func TestSchema_KnownOperation(t *testing.T) {
	stdout, stderr, code := run(nil, "schema", "sample.signed.fee")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr)
	}
	var got api.Schema
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout not a Schema: %v\n%s", err, stdout)
	}
	if got.Operation != "sample.signed.fee" {
		t.Fatalf("operation id = %q", got.Operation)
	}
	if got.Method != "GET" || got.Path != "/fixture/fee" {
		t.Fatalf("wrong method/path: %+v", got)
	}
	if got.Access != api.Read || got.Write {
		t.Fatalf("fee is read-only: access=%q write=%v", got.Access, got.Write)
	}
	if !got.Signed {
		t.Fatalf("this subdomain signs, got signed=%v", got.Signed)
	}
	if got.Identity != identity.OpenAPI {
		t.Fatalf("this subdomain uses the openapi identity, got %q", got.Identity)
	}
	if len(got.Params) != 1 || got.Params[0].In != "query" {
		t.Fatalf("params must report their location (query), got %+v", got.Params)
	}
	if len(got.Params) != 1 || got.Params[0].Name != "symbol" || !got.Params[0].Required {
		t.Fatalf("expected one required param symbol, got %+v", got.Params)
	}
}

// The write flag is derived from access, not stored separately: a write op
// reports write=true.
func TestSchema_WriteFlagDerivedFromAccess(t *testing.T) {
	stdout, _, code := run(nil, "schema", "sample.signed.toggle")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var got api.Schema
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout not a Schema: %v\n%s", err, stdout)
	}
	if got.Access != api.Write || !got.Write {
		t.Fatalf("toggle is write: access=%q write=%v", got.Access, got.Write)
	}
}

// schema with no argument lists every inspectable operation id to stdout, exit 0.
func TestSchema_ListAll(t *testing.T) {
	stdout, _, code := run(nil, "schema")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var got struct {
		Operations []string `json:"operations"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout not a list: %v\n%s", err, stdout)
	}
	if len(got.Operations) == 0 {
		t.Fatal("expected at least one operation id")
	}
	// Every listed id must itself resolve (the list and the lookup share the
	// same registry, so they can never disagree).
	for _, id := range got.Operations {
		if _, err := lookupSchema(domains(), id); err != nil {
			t.Fatalf("listed id %q does not resolve: %v", id, err)
		}
	}
}

// Malformed id (wrong segment count) → structured validation envelope on stderr,
// exit 2, stdout clean.
func TestSchema_MalformedID(t *testing.T) {
	stdout, stderr, code := run(nil, "schema", "sample.signed")
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
	if !strings.Contains(env.Error.Hint, "schema") {
		t.Fatalf("hint should point back to schema, got %q", env.Error.Hint)
	}
}

// Unknown operation → structured validation envelope on stderr, exit 2.
func TestSchema_UnknownOperation(t *testing.T) {
	_, stderr, code := run(nil, "schema", "sample.signed.nope")
	if code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	var env errEnvelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr not an envelope: %v\n%s", err, stderr)
	}
	if env.Error.Type != "validation" {
		t.Fatalf("expected validation, got %q", env.Error.Type)
	}
}

// The command tree and the schema registry never drift. This walks the REAL
// assembled cobra tree (not the registry) and the schema id list independently,
// then asserts the two sets are equal. Reading both from domains() would be a
// tautology — it could never catch the drift that matters: an Operations()
// package wired into a skill but missing from domains() (so it is neither
// mounted nor inspectable), or a domain mounted without a matching schema entry.
func TestSchema_MatchesCommandTree(t *testing.T) {
	// Set A: every leaf operation reachable in the actually-assembled command
	// tree, as <domain>.<sub>.<op>. Only descend into commands whose top-level
	// name is a declared domain (from domains()): ancillary root commands like
	// `status`, `auth`, `profile` are not domains and may coincidentally nest
	// three levels deep (e.g. `auth <provider> login`) without being schema-inspectable
	// operations — that is by design, not drift.
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{})
	domainNames := map[string]bool{}
	for _, d := range domains() {
		domainNames[d.Name] = true
	}
	tree := map[string]bool{}
	for _, domainCmd := range app.root.Commands() {
		if !domainNames[domainCmd.Name()] {
			continue
		}
		for _, subCmd := range domainCmd.Commands() {
			for _, opCmd := range subCmd.Commands() {
				tree[domainCmd.Name()+"."+subCmd.Name()+"."+opCmd.Name()] = true
			}
		}
	}

	// Set B: every id the schema command will list / resolve.
	schema := map[string]bool{}
	for _, id := range allOperationIDs(domains()) {
		schema[id] = true
	}

	for id := range tree {
		// Group-level guard commands (e.g. a domain or subdomain node) have no
		// operation leaves of their own and won't appear in tree; only real
		// leaves do. Every leaf must be inspectable.
		if !schema[id] {
			t.Errorf("operation %q is mounted as a command but not inspectable via schema", id)
		}
	}
	for id := range schema {
		if !tree[id] {
			t.Errorf("operation %q is inspectable via schema but not mounted as a command", id)
		}
	}
}
