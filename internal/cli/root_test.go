package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// withDefaultEnv pins the product default env for one test and restores it, so
// these tests do not depend on the order they run in. Effective("") is the
// read side of the same package-level value SetDefaultEnv writes.
func withDefaultEnv(t *testing.T, e hosts.Env) {
	t.Helper()
	prev := hosts.Effective("")
	t.Cleanup(func() { hosts.SetDefaultEnv(prev) })
	hosts.SetDefaultEnv(e)
}

// syncWriter is what keeps stderr intact when advisories are narrated from more
// than one goroutine at once. The MCP face serves tool calls concurrently and the
// transport narrates from wherever it is called, so two calls can reach this writer
// together. os.Stderr survives that by luck — one Write is one syscall — but a
// caller that passes a buffer, as every test and every MCP host does, does not.
//
// Two distinct advisories are needed to reach it concurrently: identical ones are
// stated once per invocation (transport.HostAdvisories). A product with a single
// identity therefore cannot get here today; the shared layer serves products with
// several, so the guard is not theirs to lose.
//
// Without the wrapper this loses bytes as well as racing, which is why the byte
// count is asserted and not just the absence of a race.
func TestSyncWriter_KeepsConcurrentAdvisoriesIntact(t *testing.T) {
	const writers, perWriter = 8, 64
	var buf bytes.Buffer
	w := &syncWriter{w: &buf}

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = w.Write([]byte(strings.Repeat(string(rune('a'+i)), perWriter)))
		}(i)
	}
	wg.Wait()

	if got, want := buf.Len(), writers*perWriter; got != want {
		t.Fatalf("wrote %d bytes, want %d — concurrent writes lost data", got, want)
	}
	// Each writer's bytes must be contiguous: interleaving would corrupt the
	// advisory text even when no byte is lost.
	for i := 0; i < writers; i++ {
		if !strings.Contains(buf.String(), strings.Repeat(string(rune('a'+i)), perWriter)) {
			t.Errorf("writer %d's output was interleaved", i)
		}
	}
}

// TestRootVersionFlagIntact guards that releasing the `version` param reservation
// did not disturb cobra's root --version flag.
func TestRootVersionFlagIntact(t *testing.T) {
	var out, errb bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &out, &errb)
	if code := app.Run([]string{"--version"}); code != 0 {
		t.Fatalf("--version exited %d (stderr=%q)", code, errb.String())
	}
	if !strings.Contains(out.String(), versionString("mexc-cli")) {
		t.Fatalf("--version output %q missing %q", out.String(), versionString("mexc-cli"))
	}
}

func envFrom(m map[string]string) Env {
	return func(k string) string { return m[k] }
}

func TestResolveConfig_ReadOnly(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		flag bool
		want bool
	}{
		{"neither", nil, false, false},
		{"flag only", nil, true, true},
		{"env only", map[string]string{"MEXC_READ_ONLY": "true"}, false, true},
		{"both", map[string]string{"MEXC_READ_ONLY": "true"}, true, true},
		{"env non-true ignored", map[string]string{"MEXC_READ_ONLY": "1"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir) // isolate from any real ~/.mexc/active
			t.Setenv("USERPROFILE", homeDir)
			cfg, err := resolveConfig(envFrom(tt.env), &globalFlags{readOnly: tt.flag, envAxis: true})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.ReadOnly != tt.want {
				t.Fatalf("ReadOnly = %v, want %v", cfg.ReadOnly, tt.want)
			}
		})
	}
}

// --base-url no longer flows through cfg at all: gf.baseURL reaches the
// transport directly as transport.Client.BaseURLOverride (wired in f.Client), so
// there is nothing for resolveConfig to assert here. --env is now the single
// per-invocation env switch and only affects cfg.Env — see below.

func TestResolveConfig_EnvPrecedence(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		gf   globalFlags
		want string
	}{
		{"default empty", nil, globalFlags{envAxis: true}, ""},
		{"env flag", nil, globalFlags{env: "testnet", envAxis: true}, "testnet"},
		{"MEXC_ENV, no flag", map[string]string{"MEXC_ENV": "staging"}, globalFlags{envAxis: true}, "staging"},
		{"env flag beats MEXC_ENV", map[string]string{"MEXC_ENV": "staging"}, globalFlags{env: "prod", envAxis: true}, "prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)
			withDefaultEnv(t, "") // a product that declares no default: "" stays ""
			gf := tt.gf
			cfg, err := resolveConfig(envFrom(tt.env), &gf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Env != tt.want {
				t.Fatalf("Env = %q, want %q", cfg.Env, tt.want)
			}
		})
	}
}

// An unselected env is normalized to the product's default before it leaves
// resolveConfig, so "" never reaches anything downstream. This is what keeps a
// diagnostic report, the transport and any identity that keys stored state by
// cfg.Env on the one spelling the host table is consulted with — the alias used
// to split a login across two slots, "" and the default's name.
func TestResolveConfig_UnselectedEnvBecomesDefault(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withDefaultEnv(t, "example-default")

	cfg, err := resolveConfig(envFrom(nil), &globalFlags{envAxis: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "example-default" {
		t.Fatalf("Env = %q, want the product default %q", cfg.Env, "example-default")
	}

	// An explicit selection is passed through untouched, including one that
	// happens to name the default.
	for _, sel := range []string{"example-other", "example-default"} {
		cfg, err := resolveConfig(envFrom(nil), &globalFlags{env: sel, envAxis: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Env != sel {
			t.Fatalf("Env = %q, want the selected %q", cfg.Env, sel)
		}
	}
}

// The persisted global env setting flows through to cfg.Env when no MEXC_ENV /
// --env override is present, and a per-invocation override still beats it.
func TestResolveConfig_GlobalEnv(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if err := config.SetSetting("env", "example-env"); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveConfig(envFrom(nil), &globalFlags{envAxis: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "example-env" {
		t.Fatalf("Env = %q, want %q (from global config)", cfg.Env, "example-env")
	}
	// --env beats the persisted global setting.
	cfg, err = resolveConfig(envFrom(nil), &globalFlags{env: "example-override", envAxis: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != "example-override" {
		t.Fatalf("Env = %q, want %q (--env overrides global)", cfg.Env, "example-override")
	}
}

// The profile-resolution tests that lived here moved with the behaviour: the
// engine no longer loads credentials, so "explicit profile wins", "a named
// profile that is missing is fatal" and "a broken active profile degrades" are
// now asserted against the identity that owns them, in
// editions/public/adapter/credential_openapi_test.go.
