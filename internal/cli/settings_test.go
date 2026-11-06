package cli

import (
	"bytes"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// envInEffect is the ONE place the env precedence chain is written down —
// resolveConfig reads it for cfg.Env and the `config` command reads it through
// Factory.Setting — so this table is what keeps "the value a caller is shown" and
// "the value a request uses" from drifting apart. They did drift: `config` used to
// answer from the settings file plus the product default, so `MEXC_ENV=testnet
// config get env` reported "prod" with source "default" while the request went to
// testnet. A confident wrong answer, handed to an agent.
func TestEnvInEffect_PrecedenceAndSource(t *testing.T) {
	tests := []struct {
		name       string
		stored     string
		envVars    map[string]string
		flag       string
		defaultEnv hosts.Env
		wantValue  string
		wantSource string
	}{
		{"nothing set at all, no product default", "", nil, "", "", "", SourceUnset},
		{"nothing set, product default stands in", "", nil, "", "prod", "prod", SourceDefault},
		{"settings file", "stored-env", nil, "", "prod", "stored-env", SourceConfig},
		{"MEXC_ENV beats the file", "stored-env", map[string]string{"MEXC_ENV": "var-env"}, "", "prod", "var-env", SourceEnvVar},
		{"--env beats MEXC_ENV", "stored-env", map[string]string{"MEXC_ENV": "var-env"}, "flag-env", "prod", "flag-env", SourceFlag},
		{"--env with nothing else", "", nil, "flag-env", "prod", "flag-env", SourceFlag},
		{"MEXC_ENV with nothing else", "", map[string]string{"MEXC_ENV": "var-env"}, "", "prod", "var-env", SourceEnvVar},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)
			withDefaultEnv(t, tt.defaultEnv)
			if tt.stored != "" {
				if err := config.SetSetting("env", tt.stored); err != nil {
					t.Fatal(err)
				}
			}
			value, source := envInEffect(envFrom(tt.envVars), &globalFlags{env: tt.flag, envAxis: true})
			if value != tt.wantValue || source != tt.wantSource {
				t.Fatalf("envInEffect = (%q, %q), want (%q, %q)",
					value, source, tt.wantValue, tt.wantSource)
			}
		})
	}
}

// What envInEffect reports and what resolveConfig puts in cfg.Env are the same
// value by construction. Asserted anyway: the whole defect class here was two
// pieces of code answering one question separately.
func TestEnvInEffect_AgreesWithResolveConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withDefaultEnv(t, "prod")
	for _, tc := range []struct {
		envVars map[string]string
		flag    string
	}{
		{nil, ""},
		{map[string]string{"MEXC_ENV": "var-env"}, ""},
		{nil, "flag-env"},
		{map[string]string{"MEXC_ENV": "var-env"}, "flag-env"},
	} {
		gf := &globalFlags{env: tc.flag, envAxis: true}
		want, _ := envInEffect(envFrom(tc.envVars), gf)
		cfg, err := resolveConfig(envFrom(tc.envVars), gf)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.Env != want {
			t.Fatalf("cfg.Env = %q but envInEffect says %q (env=%v flag=%q)",
				cfg.Env, want, tc.envVars, tc.flag)
		}
	}
}

// Factory.Setting is how the `config` command reaches that chain. A key with no
// layers above the file reports the file or nothing; an unknown key errors the
// same way `config get` does.
func TestFactorySetting(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withDefaultEnv(t, "prod")
	var out, errb bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(k string) string { return map[string]string{"MEXC_ENV": "var-env"}[k] }, &out, &errb)

	value, source, err := app.f.Setting("env")
	if err != nil {
		t.Fatalf("Setting(env): %v", err)
	}
	if value != "var-env" || source != SourceEnvVar {
		t.Fatalf("Setting(env) = (%q, %q), want (var-env, %s)", value, source, SourceEnvVar)
	}
	if _, _, err := app.f.Setting("nosuchkey"); err == nil {
		t.Fatalf("Setting on an unknown key should error")
	}
}
