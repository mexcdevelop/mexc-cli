package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A cobra-level argument failure must name the usage of the command that failed.
// cobra's own message states a COUNT ("accepts 1 arg(s), received 0") and never
// which argument, so a caller who supplied every flag and forgot the positional
// was told to go read --help about flags that were already correct.
//
// This is a unit test of usageHint rather than an end-to-end one because the
// function is the whole fix and it is a pure func(*cobra.Command) string. It used
// to be exercised only through one edition's `profile set <name>`, which meant a
// shared-machinery regression that showed on some other product's command shape
// had no coverage at all.
func TestUsageHint(t *testing.T) {
	leaf := &cobra.Command{Use: "set <name>", Run: func(*cobra.Command, []string) {}}
	leaf.Flags().String("api-key", "", "")
	group := &cobra.Command{Use: "profile"}
	group.AddCommand(leaf)
	root := &cobra.Command{Use: "mexc-cli"}
	root.AddCommand(group)

	// A group as PRODUCTION builds it. NewGroup/guardGroupErr sets RunE on every
	// group so a bare invocation yields an envelope instead of cobra's help-and-
	// exit-0 — which made every group Runnable, and while that was the predicate
	// this branch never fired for one: `mexc-cli spot --nosuchflag` was answered
	// with a usage line. The predicate is HasSubCommands now, so a namespace is
	// treated as a namespace however it is wired.
	runnableGroup := NewGroup("profile", "", func(error) {})
	runnableGroup.AddCommand(&cobra.Command{Use: "set <name>", Run: func(*cobra.Command, []string) {}})

	tests := []struct {
		name string
		cmd  *cobra.Command
		want string
	}{
		{"a leaf quotes its usage line", leaf, "usage: mexc-cli profile set <name>"},
		{"a parent points at the list", group, "run `mexc-cli profile --help` to list available commands"},
		{"a production group does too, though it is Runnable", runnableGroup, "run `profile --help` to list available commands"},
		{"the root points at the list", root, "run `mexc-cli --help` to list available commands"},
		{"nil falls back to the generic hint", nil, "run with --help"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := usageHint(tt.cmd); !strings.Contains(got, tt.want) {
				t.Fatalf("usageHint = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// End to end: a cobra-level failure on a real assembled tree carries the hint,
// and the envelope still exits 2 rather than escaping as bare text or exit 0.
func TestUsageHint_ReachesTheErrorEnvelope(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing required flag", []string{"sample", "public", "candles"},
			"usage: mexc-cli sample public candles"},
		{"unknown top-level command", []string{"definitely-not-a-command"},
			"to list available commands"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			app := NewApp("mexc-cli", "", domains(), nil, nil, true,
				func(string) string { return "" }, &out, &errb)
			if code := app.Run(tt.args); code != 2 {
				t.Fatalf("exit = %d, want 2 (validation); stderr = %s", code, errb.String())
			}
			if !strings.Contains(errb.String(), tt.want) {
				t.Fatalf("stderr missing %q:\n%s", tt.want, errb.String())
			}
		})
	}
}
