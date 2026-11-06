package framework

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// nameEdition is the smallest edition that assembles: one unsigned op on a
// registered host. It exists to build a real command tree, which is what the
// ordering below is about.
func nameEdition() Edition {
	return Edition{
		Name:       "nametest",
		DefaultEnv: "prod",
		HostRows: []hosts.Registration{{
			Key:  "nametest-host",
			Rows: map[hosts.Env]hosts.Endpoint{"prod": {URL: "https://example.invalid"}},
		}},
		Domains: []api.DomainSpec{{
			Name: "probe",
			Subs: []api.Subdomain{{
				Name:     "body",
				Signed:   api.SignNone,
				Identity: api.IdentityNone,
				HostKey:  "nametest-host",
				Ops: []api.Operation{{
					Name: "send", Method: "POST", Path: "/send",
					Access: api.Write, Summary: "send a body",
					Body: struct {
						Note string `json:"note"`
					}{},
				}},
			}},
		}},
	}
}

// A command's Short/Long and its flag usage strings are built by string
// CONCATENATION while the tree is assembled, so they capture whatever cliname
// held at that moment — unlike a hint, which calls cliname.Name() when the error
// is raised. The name therefore has to be recorded BEFORE assembly; recorded
// after, it reached the hints and left every help text naming the wrong binary.
//
// It drives runWith rather than RunForTest: the ordering under test lives there,
// and a test that set cliname itself would pass no matter which order runWith
// used — which is exactly how the first version of this test was worthless.
func TestRunWith_HelpTextUsesTheInvokedName(t *testing.T) {
	prev := cliname.Name()
	t.Cleanup(func() { cliname.Set(prev) })

	var out, errBuf bytes.Buffer
	code := runWith(nameEdition(), "probe-cli", func(string) string { return "" },
		&out, &errBuf, []string{"probe", "body", "send", "--help"})
	if code != 0 {
		t.Fatalf("--help exited %d: %s", code, errBuf.String())
	}
	help := out.String() + errBuf.String()

	// The --data flag's usage is assembled from cliname at mount time.
	if !strings.Contains(help, "probe-cli schema") {
		t.Fatalf("flag usage does not name the invoked binary; got:\n%s", help)
	}
	if strings.Contains(help, "mexc-cli schema") {
		t.Fatalf("flag usage still names the default binary; got:\n%s", help)
	}
	// And the command path, which comes from SetRootName after assembly.
	if !strings.Contains(help, "probe-cli probe body send") {
		t.Fatalf("usage line does not name the invoked binary; got:\n%s", help)
	}
}

// An argv[0] that names no usable command leaves the default rather than
// producing a hint that tells the reader to run "." or "/".
func TestRunWith_OddArgv0KeepsTheDefault(t *testing.T) {
	prev := cliname.Name()
	t.Cleanup(func() { cliname.Set(prev) })

	for _, argv0 := range []string{"", ".", "/"} {
		t.Run("argv0="+argv0, func(t *testing.T) {
			cliname.Set(prev)
			var out, errBuf bytes.Buffer
			runWith(nameEdition(), argv0, func(string) string { return "" },
				&out, &errBuf, []string{"probe", "body", "send", "--help"})
			if got := cliname.Name(); got != prev {
				t.Fatalf("argv0 %q set the name to %q, want the default %q", argv0, got, prev)
			}
		})
	}
}

// The name a hint tells the user to type. On Windows that is never the filename:
// PATHEXT means `mexc doctor` runs mexc.exe, and a hint naming the suffix sends
// the reader to type something the docs never mention.
//
// Driven as a table from both sides because the answer differs by platform and
// only one platform can be run here — the windows parameter is what makes the
// other half reachable at all.
func TestCommandName_DropsTheWindowsSuffixOnlyOnWindows(t *testing.T) {
	cases := []struct {
		argv0             string
		onWindows, onUnix string
	}{
		{"mexc.exe", "mexc", "mexc.exe"},
		// The filesystem is case-insensitive, and argv[0] reports whatever spelling
		// was typed — so matching only lowercase would miss half the invocations.
		{"MEXC.EXE", "MEXC", "MEXC.EXE"},
		{"Mexc.Exe", "Mexc", "Mexc.Exe"},
		// No suffix, and a suffix that is not .exe: both are the name as given.
		{"mexc", "mexc", "mexc"},
		{"mexc.com", "mexc.com", "mexc.com"},
		// A dotted name that merely ENDS in something else must keep every part;
		// trimming by substring rather than by extension would eat this one.
		{"mexc.exe.bak", "mexc.exe.bak", "mexc.exe.bak"},
		// Nothing to trim, and nothing to crash on.
		{"", "", ""},
		{".exe", "", ".exe"},
	}
	for _, c := range cases {
		if got := commandName(c.argv0, true); got != c.onWindows {
			t.Errorf("commandName(%q, windows) = %q, want %q", c.argv0, got, c.onWindows)
		}
		if got := commandName(c.argv0, false); got != c.onUnix {
			t.Errorf("commandName(%q, unix) = %q, want %q", c.argv0, got, c.onUnix)
		}
	}
}

// The stripped name has to reach the help text and the hints, not just the
// helper — that is the whole point, and the ordering inside runWith is what
// carries it (see the note there).
func TestRunWith_WindowsSuffixDoesNotReachTheHelpText(t *testing.T) {
	prev := cliname.Name()
	t.Cleanup(func() { cliname.Set(prev) })

	var out, errBuf bytes.Buffer
	runWith(nameEdition(), commandName("probe-cli.exe", true), func(string) string { return "" },
		&out, &errBuf, []string{"probe", "body", "send", "--help"})
	help := out.String() + errBuf.String()
	if strings.Contains(help, ".exe") {
		t.Errorf("help text names a suffix the user never types:\n%s", help)
	}
	if !strings.Contains(help, "probe-cli probe body send") {
		t.Errorf("usage line does not name the invoked binary; got:\n%s", help)
	}
}
