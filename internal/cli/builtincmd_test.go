package cli

import (
	"bytes"
	"strings"
	"testing"
)

// cobra's own `help` and `completion` never reach a RunE, so App.Run's envelope
// wrapper never saw them: a bad invocation printed prose to STDOUT and exited 0.
// That is the outcome the wrapper exists to prevent — an agent reads exit 0 as
// success and then parses help text as data.
//
// `completion` failed for the same reason every un-guarded group does: a parent
// with subcommands and no Run. `help` needed replacing outright, because its Run
// prints "Unknown help topic" and returns nil.
func TestBuiltinCommands_BadInvocationYieldsAnEnvelope(t *testing.T) {
	bad := []struct {
		name string
		args []string
		want string
	}{
		{"unknown help topic", []string{"help", "nosuchthing"}, "unknown help topic"},
		{"help topic with leftovers", []string{"help", "sample", "nosuchthing"}, "unknown help topic"},
		{"unknown completion shell", []string{"completion", "nosuchshell"}, "unknown subcommand"},
		{"completion with no shell", []string{"completion"}, "expects a subcommand"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)
			var out, errb bytes.Buffer
			app := NewApp("mexc-cli", "", domains(), nil, nil, true,
				func(string) string { return "" }, &out, &errb)
			code := app.Run(tt.args)
			if code != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", code, out.String(), errb.String())
			}
			if out.Len() != 0 {
				t.Fatalf("stdout must stay empty on an error, got %q", out.String())
			}
			if !strings.Contains(errb.String(), tt.want) {
				t.Fatalf("stderr missing %q:\n%s", tt.want, errb.String())
			}
		})
	}
}

// The good paths still work, and still write the requested help to stdout — the
// help text IS the data the caller asked for.
func TestBuiltinCommands_GoodInvocationStillWorks(t *testing.T) {
	good := []struct {
		name string
		args []string
		want string
	}{
		{"root help", []string{"help"}, "Usage:"},
		{"help for a command", []string{"help", "sample"}, "sample"},
		{"completion for a real shell", []string{"completion", "bash"}, "bash completion"},
	}
	for _, tt := range good {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			t.Setenv("HOME", homeDir)
			t.Setenv("USERPROFILE", homeDir)
			var out, errb bytes.Buffer
			app := NewApp("mexc-cli", "", domains(), nil, nil, true,
				func(string) string { return "" }, &out, &errb)
			if code := app.Run(tt.args); code != 0 {
				t.Fatalf("exit = %d, want 0; stderr=%s", code, errb.String())
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("stdout missing %q:\n%s", tt.want, out.String())
			}
		})
	}
}

// Help must work when the configuration is broken, which is when someone reaches
// for it. It skips the root's config-resolving PersistentPreRunE for the same
// reason `config` and `profile` do.
func TestHelp_WorksWithABrokenConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var out, errb bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true,
		func(k string) string { return map[string]string{"MEXC_READ_ONLY": "not-a-bool"}[k] }, &out, &errb)
	if code := app.Run([]string{"help", "sample"}); code != 0 {
		t.Fatalf("help exited %d with a hostile environment; stderr=%s", code, errb.String())
	}
}
