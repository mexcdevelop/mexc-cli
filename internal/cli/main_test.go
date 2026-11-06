package cli

import (
	"os"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// TestMain isolates the engine test binary from the developer's real ~/.mexc: it
// points HOME at a throwaway directory so on-disk global settings (config env)
// and saved profiles never leak into tests that drive the CLI in-process. The
// contract reachability tests, for instance, resolve hosts from cfg.Env, so a
// developer who switched envs with `config set env` must not thereby break
// the openapi (spot) contract tests. Individual tests that need specific HOME
// state override this with t.Setenv, which restores the value set here afterward.
//
// It also registers test doubles for the public adapters (openapi identity +
// a query-signing scheme + openapi host). Production registration lives in
// editions/*, which engine cannot import (that would cycle through
// framework); the engine tests drive the generic assembly + faces against
// the fixture topology (fixtures_test.go domains()) with these minimal
// doubles.
// testHostKey stands in for a product's host key. This package is the shared
// machinery: it must not name a product's vocabulary (Invariant 16), and the
// host table is injected anyway, so the tests register their own.
const testHostKey = "public-example"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mexc-cli-enginetest-home")
	if err != nil {
		panic(err)
	}
	// os.Setenv (not t.Setenv) because this runs outside a *testing.T.
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	// USERPROFILE is the variable os.UserHomeDir reads on Windows, and HOME is
	// ignored there. Without this the whole package's tests would run against the
	// developer's REAL state directory — writing profiles into it, changing its
	// modes, and removing key files.
	if err := os.Setenv("USERPROFILE", dir); err != nil {
		panic(err)
	}
	credential.Register(testOpenAPI{})
	signer.Register(testQuerySigner{})
	hosts.Register(hosts.Registration{Key: testHostKey, Rows: map[hosts.Env]hosts.Endpoint{
		"":            {URL: "https://example.test"},
		"example-alt": {URL: "https://alt.example.test"},
	}})
	// The settable key set starts empty and is filled by a composition root; these
	// tests build the tree directly, so they stand in for that. Two envs above is
	// the case where a real product registers it (framework.assemble).
	config.RegisterSetting(config.SettingEnv)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
