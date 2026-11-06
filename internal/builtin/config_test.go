package builtin

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mexcdevelop/mexc-cli/internal/cli"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// This file exists because it did not. `config` is a SHARED builtin — every
// product mounts it — but its only coverage lived in one edition's contract test,
// so the machinery here was tested through the public product's command shapes
// and would have travelled with that product if the repos ever diverged.

// withTestEnvs pins the product default and registers a host serving the env
// names these tests write. `config set env` validates against the hosts THIS
// product registered, and in a bare test package that is none of them.
//
// It also registers the env SETTING. The settable key set starts empty and is
// filled by the composition root (framework.assemble does it for a product
// serving more than one env). These tests build the command directly, so they
// stand in for that step — and two envs is exactly the case where env is settable.
func withTestEnvs(t *testing.T) {
	t.Helper()
	prev := hosts.Effective("")
	t.Cleanup(func() { hosts.SetDefaultEnv(prev) })
	hosts.SetDefaultEnv("prod")
	config.RegisterSetting(config.SettingEnv)
	hosts.Register(hosts.Registration{
		Key: "config-test-host",
		Rows: map[hosts.Env]hosts.Endpoint{
			"prod":           {URL: "https://example.test"},
			"example-stored": {URL: "https://example.test"},
		},
	})
}

// runConfig drives the real `config` command group against a temp $HOME, the way
// framework.Run mounts it: a Kernel from an assembled app, with this invocation's
// env reader and flags. Returns stdout and the exit code.
func runConfig(t *testing.T, envVars map[string]string, args ...string) (string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	app := cli.NewApp("mexc-cli", "", []api.DomainSpec{}, nil, nil, true,
		func(k string) string { return envVars[k] }, &out, &errb)
	app.AddCommand(NewConfig(app.Kernel()))
	code := app.Run(args)
	if code != 0 {
		return errb.String(), code
	}
	return out.String(), code
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
	return m
}

// Every read reports the value in EFFECT and the layer it came from, so a caller
// asking "what is my env" gets the env its next request will use.
//
// The regression this pins: `config` answered from what it could see locally
// (the settings file, plus the product default) while the request path layered
// MEXC_ENV and --env on top. `MEXC_ENV=testnet config get env` therefore reported
// "prod", source "default" — a confident false statement, worse than the blank it
// had replaced, and handed to an agent as fact.
func TestConfigGet_ReportsTheEffectiveValueAndItsSource(t *testing.T) {

	tests := []struct {
		name       string
		setFirst   string
		envVars    map[string]string
		args       []string
		wantValue  string
		wantSource string
	}{
		{"nothing set", "", nil, []string{"config", "get", "env"}, "prod", cli.SourceDefault},
		{"settings file", "example-stored", nil, []string{"config", "get", "env"}, "example-stored", cli.SourceConfig},
		{"MEXC_ENV beats the file", "example-stored", map[string]string{"MEXC_ENV": "example-var"},
			[]string{"config", "get", "env"}, "example-var", cli.SourceEnvVar},
		{"--env beats MEXC_ENV", "example-stored", map[string]string{"MEXC_ENV": "example-var"},
			[]string{"--env", "example-flag", "config", "get", "env"}, "example-flag", cli.SourceFlag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)
			withTestEnvs(t)
			if tt.setFirst != "" {
				if s, code := runConfig(t, nil, "config", "set", "env", tt.setFirst); code != 0 {
					t.Fatalf("config set exited %d: %s", code, s)
				}
			}
			s, code := runConfig(t, tt.envVars, tt.args...)
			if code != 0 {
				t.Fatalf("exited %d: %s", code, s)
			}
			m := decode(t, s)
			if m["value"] != tt.wantValue || m["source"] != tt.wantSource {
				t.Fatalf("got value=%v source=%v, want %q / %q",
					m["value"], m["source"], tt.wantValue, tt.wantSource)
			}
		})
	}
}

// `config list` answers with the same pair, so the two reads cannot disagree.
func TestConfigList_MatchesGet(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestEnvs(t)

	env := map[string]string{"MEXC_ENV": "example-var"}
	got, code := runConfig(t, env, "config", "get", "env")
	if code != 0 {
		t.Fatalf("config get exited %d: %s", code, got)
	}
	one := decode(t, got)

	listed, code := runConfig(t, env, "config", "list")
	if code != 0 {
		t.Fatalf("config list exited %d: %s", code, listed)
	}
	settings, ok := decode(t, listed)["settings"].(map[string]any)
	if !ok {
		t.Fatalf("config list has no settings object:\n%s", listed)
	}
	entry, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatalf("config list has no env entry:\n%s", listed)
	}
	if entry["value"] != one["value"] || entry["source"] != one["source"] {
		t.Fatalf("list says %v/%v, get says %v/%v",
			entry["value"], entry["source"], one["value"], one["source"])
	}
}

// `unset` reports the value the key falls back to, so it cannot be mistaken for
// an unset that did not take.
func TestConfigUnset_ReportsTheFallback(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestEnvs(t)

	if s, code := runConfig(t, nil, "config", "set", "env", "example-stored"); code != 0 {
		t.Fatalf("config set exited %d: %s", code, s)
	}
	s, code := runConfig(t, nil, "config", "unset", "env")
	if code != 0 {
		t.Fatalf("config unset exited %d: %s", code, s)
	}
	m := decode(t, s)
	if m["unset"] != "env" || m["value"] != "prod" || m["source"] != cli.SourceDefault {
		t.Fatalf("unset reported %v, want the product default it falls back to", m)
	}
}

// An empty value is refused rather than silently clearing the key. It used to
// fall through to SetSetting, where writeSettings drops empty entries — so
// `config set env ""` cleared the setting while reporting that it had stored one.
func TestConfigSet_RefusesAnEmptyValue(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestEnvs(t)

	s, code := runConfig(t, nil, "config", "set", "env", "")
	if code == 0 {
		t.Fatalf("config set env \"\" should fail, got exit 0: %s", s)
	}
	if !bytes.Contains([]byte(s), []byte("config unset env")) {
		t.Fatalf("the error should point at the verb that does clear it:\n%s", s)
	}
}

// A value no registered host serves is refused at write time, where the mistake
// was made, rather than at every later request.
func TestConfigSet_RefusesAnEnvNoHostServes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	withTestEnvs(t)
	if s, code := runConfig(t, nil, "config", "set", "env", "nosuchenv"); code == 0 {
		t.Fatalf("config set env nosuchenv should fail, got exit 0: %s", s)
	}
	if s, code := runConfig(t, nil, "config", "set", "env", "prod"); code != 0 {
		t.Fatalf("config set env prod should succeed, exited %d: %s", code, s)
	}
}
