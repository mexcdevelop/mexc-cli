package skillstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

// global builds a default-shaped install: the global scope, the one agent
// `skills install` targets when given no flags, and the complete skill set.
func global(skills map[string]string) Install {
	return Install{Scope: ScopeGlobal, Agents: []string{"claude-code"}, All: true, Skills: skills}
}

// subset builds a partial install: the user named the skills explicitly.
func subset(skills map[string]string) Install {
	return Install{Scope: ScopeGlobal, Agents: []string{"claude-code"}, All: false, Skills: skills}
}

// stubDigester registers a content digester for one test and removes it after,
// so the package-level hook does not leak into the version-fallback tests.
func stubDigester(t *testing.T, d map[string]string, err error) {
	t.Helper()
	prev := digester
	SetContentDigester(func() (map[string]string, error) { return d, err })
	t.Cleanup(func() { digester = prev })
}

func TestSkillsState_RoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	in := global(map[string]string{"mexc-common": "aa", "mexc-spot-trade": "bb"})
	if err := Record("1.2.3", in, epoch); err != nil {
		t.Fatalf("Record: %v", err)
	}
	st, ok, err := Read()
	if err != nil || !ok {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	if st.Version != "1.2.3" || st.UpdatedAt != "2026-07-21T10:00:00Z" {
		t.Fatalf("state mismatch: %+v", st)
	}
	if len(st.Installs) != 1 {
		t.Fatalf("installs = %+v, want one destination", st.Installs)
	}
	got := st.Installs[0]
	if got.Scope != ScopeGlobal || !got.All || len(got.Agents) != 1 || got.Agents[0] != "claude-code" {
		t.Fatalf("destination did not round-trip: %+v", got)
	}
	if got.Skills["mexc-common"] != "aa" || got.Skills["mexc-spot-trade"] != "bb" {
		t.Fatalf("digests did not round-trip: %+v", got.Skills)
	}
}

// THE regression this shape exists to prevent. Recording the last command's set
// would drop the earlier install, and `uninstall --all` would then leave it on
// disk with no record that it is there — a silent leak, worse than the
// over-removal it replaced.
func TestRecord_MergesInsteadOfReplacing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if err := Record("1.2.3", subset(map[string]string{"a": "h1"}), epoch); err != nil {
		t.Fatal(err)
	}
	if err := Record("1.2.3", subset(map[string]string{"b": "h2"}), epoch); err != nil {
		t.Fatal(err)
	}
	st, _, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("names = %v, want both installs remembered", got)
	}
	if len(st.Installs) != 1 {
		t.Fatalf("the same destination must stay one entry, got %+v", st.Installs)
	}
}

// A re-install of the same skill updates its digest rather than duplicating it.
func TestRecord_RefreshesDigests(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if err := Record("1.2.3", global(map[string]string{"a": "old"}), epoch); err != nil {
		t.Fatal(err)
	}
	if err := Record("1.3.0", global(map[string]string{"a": "new"}), epoch); err != nil {
		t.Fatal(err)
	}
	st, _, _ := Read()
	if st.Digests("")["a"] != "new" {
		t.Fatalf("digest was not refreshed: %+v", st.Installs)
	}
	if st.Version != "1.3.0" {
		t.Fatalf("version = %q, want the most recent install's", st.Version)
	}
}

// Different destinations stay separate: the same skill can be in the global
// agent dirs and in a project, and removing it from one must not claim the other.
func TestRecord_SeparatesDestinations(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if err := Record("1.2.3", global(map[string]string{"a": "h"}), epoch); err != nil {
		t.Fatal(err)
	}
	proj := Install{Scope: ScopeProject, Root: "/tmp/proj", Agents: []string{"cursor"}, All: true,
		Skills: map[string]string{"a": "h"}}
	if err := Record("1.2.3", proj, epoch); err != nil {
		t.Fatal(err)
	}
	st, _, _ := Read()
	if len(st.Installs) != 2 {
		t.Fatalf("global and project are different destinations: %+v", st.Installs)
	}
}

func TestRecord_UnionsAgents(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	one := Install{Scope: ScopeGlobal, Agents: []string{"claude-code"}, Skills: map[string]string{"a": "h"}}
	two := Install{Scope: ScopeGlobal, Agents: []string{"cursor"}, Skills: map[string]string{"a": "h"}}
	for _, in := range []Install{one, two} {
		if err := Record("1.2.3", in, epoch); err != nil {
			t.Fatal(err)
		}
	}
	st, _, _ := Read()
	if got := st.Installs[0].Agents; len(got) != 2 || got[0] != "claude-code" || got[1] != "cursor" {
		t.Fatalf("agents = %v, want the union", got)
	}
	// Every agent absorbs any named one: it already covers them.
	all := Install{Scope: ScopeGlobal, Agents: []string{AgentsAll}, Skills: map[string]string{"a": "h"}}
	if err := Record("1.2.3", all, epoch); err != nil {
		t.Fatal(err)
	}
	st, _, _ = Read()
	if got := st.Installs[0].Agents; len(got) != 1 || got[0] != AgentsAll {
		t.Fatalf("agents = %v, want %q to absorb the named ones", got, AgentsAll)
	}
}

func TestForget_SubtractsAndPrunes(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if err := Record("1.2.3", global(map[string]string{"a": "h1", "b": "h2"}), epoch); err != nil {
		t.Fatal(err)
	}
	if err := Forget("1.2.3", []string{"a"}, epoch); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	st, _, _ := Read()
	if got := st.Names(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("names = %v, want only b", got)
	}
	// Emptying a destination removes it, so nothing claims an install that holds
	// nothing.
	if err := Forget("1.2.3", []string{"b"}, epoch); err != nil {
		t.Fatal(err)
	}
	st, _, _ = Read()
	if len(st.Installs) != 0 {
		t.Fatalf("an empty destination must be pruned, got %+v", st.Installs)
	}
}

// A flat record written by an older binary must keep working: promoted to one
// install of unknown destination, still tracking the complete set.
func TestRead_MigratesLegacyRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".mexc"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":"1.2.3","skills":["a","b"],"digests":{"a":"h1"},"updated_at":"2026-07-21T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(home, ".mexc", stateFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st, ok, err := Read()
	if !ok || err != nil {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	if len(st.Installs) != 1 {
		t.Fatalf("legacy record must become one install, got %+v", st.Installs)
	}
	in := st.Installs[0]
	if in.Scope != ScopeUnknown || len(in.Agents) != 0 {
		t.Fatalf("the old format never said where it went: %+v", in)
	}
	if !in.All {
		t.Fatal("the flat record always held the complete set; assuming a subset would narrow drift detection")
	}
	if got := st.Names(); len(got) != 2 {
		t.Fatalf("names = %v, want both", got)
	}
	// A name with no recorded digest must not enter the content basis.
	if d := st.Digests(""); len(d) != 1 || d["a"] != "h1" {
		t.Fatalf("digests = %v, want only the one that had a digest", d)
	}
}

// The legacy fields must never be written back, or a new binary would keep
// producing a file that looks like the old shape.
func TestRecord_DropsLegacyFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".mexc"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":"1.0.0","skills":["a"],"updated_at":"2026-07-21T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(home, ".mexc", stateFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Record("1.2.3", global(map[string]string{"b": "h"}), epoch); err != nil {
		t.Fatal(err)
	}
	p, _ := Path()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"skills": [`) || strings.Contains(string(raw), `"digests"`) {
		t.Fatalf("legacy fields were written back:\n%s", raw)
	}
	st, _, _ := Read()
	if got := st.Names(); len(got) != 2 {
		t.Fatalf("names = %v, want the migrated skill plus the new one", got)
	}
}

// A project install belongs to its directory. Reporting it from anywhere else
// would reintroduce exactly the noise content digests remove.
func TestRelevant_ScopesProjectInstallsToTheirRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "proj")
	st := &State{Installs: []Install{
		global(map[string]string{"g": "h"}),
		{Scope: ScopeProject, Root: root, All: true, Skills: map[string]string{"p": "h"}},
	}}
	if got := st.Relevant(root); len(got) != 2 {
		t.Fatalf("inside the root both apply, got %d", len(got))
	}
	if got := st.Relevant(filepath.Join(root, "sub", "dir")); len(got) != 2 {
		t.Fatalf("a subdirectory is still inside the project, got %d", len(got))
	}
	if got := st.Relevant(filepath.Join(string(filepath.Separator), "tmp", "other")); len(got) != 1 {
		t.Fatalf("outside the root only the global install applies, got %d", len(got))
	}
	// A sibling whose name merely starts the same is NOT inside it.
	if got := st.Relevant(root + "-other"); len(got) != 1 {
		t.Fatalf("a name-prefix sibling is not inside the project, got %d", len(got))
	}
	// Names ignores the directory: the files are on disk either way.
	if got := st.Names(); len(got) != 2 {
		t.Fatalf("Names must span every destination, got %v", got)
	}
}

func TestCurrentDigests(t *testing.T) {
	// Unregistered: not an error, just no content basis.
	if _, ok := CurrentDigests(); ok {
		t.Fatal("no digester registered must report ok=false")
	}
	// A failure must also report false: a broken hash must never be read as
	// "everything changed".
	stubDigester(t, nil, errors.New("boom"))
	if _, ok := CurrentDigests(); ok {
		t.Fatal("a failing digester must report ok=false")
	}
	// An empty result is the same situation.
	stubDigester(t, map[string]string{}, nil)
	if _, ok := CurrentDigests(); ok {
		t.Fatal("an empty digest set must report ok=false")
	}
	stubDigester(t, map[string]string{"a": "1"}, nil)
	got, ok := CurrentDigests()
	if !ok || got["a"] != "1" {
		t.Fatalf("CurrentDigests() = %v, %v", got, ok)
	}
}

// The headline behavior: a release that changes no SKILL.md text must not nudge,
// even though the build version moved.
func TestCompare_ContentBeatsVersion(t *testing.T) {
	same := map[string]string{"mexc-common": "h1", "mexc-spot-trade": "h2"}
	st := &State{Version: "1.2.3", Installs: []Install{global(same)}}

	d := Compare(st, "1.9.0", "", same)
	if d.Basis != BasisContent {
		t.Fatalf("basis = %q, want %q", d.Basis, BasisContent)
	}
	if d.Stale {
		t.Fatalf("identical content across a version bump is not drift: %+v", d)
	}
	if len(d.Changed) != 0 {
		t.Fatalf("nothing changed, got %v", d.Changed)
	}
}

// And the converse: same version, edited content, still drift. This is the case
// version comparison could not see at all.
func TestCompare_ContentDriftAtTheSameVersion(t *testing.T) {
	st := &State{Version: "1.2.3", Installs: []Install{
		global(map[string]string{"mexc-common": "h1", "mexc-spot-trade": "h2"}),
	}}
	d := Compare(st, "1.2.3", "", map[string]string{"mexc-common": "h1", "mexc-spot-trade": "EDITED"})
	if d.Basis != BasisContent || !d.Stale {
		t.Fatalf("edited content at the same version is drift: %+v", d)
	}
	if len(d.Changed) != 1 || d.Changed[0] != "mexc-spot-trade" {
		t.Fatalf("changed = %v, want only mexc-spot-trade", d.Changed)
	}
}

// An added or removed skill is drift too, and must be named.
func TestCompare_AddedAndRemovedSkills(t *testing.T) {
	st := &State{Version: "1.2.3", Installs: []Install{global(map[string]string{"gone": "h1", "kept": "h2"})}}
	d := Compare(st, "1.2.3", "", map[string]string{"kept": "h2", "added": "h3"})
	if !d.Stale {
		t.Fatal("an added or removed skill is drift")
	}
	// Sorted, so a message is stable.
	if len(d.Changed) != 2 || d.Changed[0] != "added" || d.Changed[1] != "gone" {
		t.Fatalf("changed = %v, want [added gone]", d.Changed)
	}
}

// A subset install asked for specific skills. A new skill in the binary is not
// its business, and reporting one would nag the user to reinstall something they
// deliberately skipped. A destination tracking the complete set DOES want to hear.
func TestCompare_SubsetIgnoresSkillsItNeverInstalled(t *testing.T) {
	current := map[string]string{"a": "h1", "brand-new": "h9"}

	part := &State{Version: "1.2.3", Installs: []Install{subset(map[string]string{"a": "h1"})}}
	if d := Compare(part, "1.2.3", "", current); d.Stale {
		t.Fatalf("a subset install must ignore a skill it never installed: %+v", d)
	}
	full := &State{Version: "1.2.3", Installs: []Install{global(map[string]string{"a": "h1"})}}
	d := Compare(full, "1.2.3", "", current)
	if !d.Stale || len(d.Changed) != 1 || d.Changed[0] != "brand-new" {
		t.Fatalf("an install tracking every skill must report a new one: %+v", d)
	}
	// But a subset install still reports drift in what it DID install.
	edited := map[string]string{"a": "EDITED", "brand-new": "h9"}
	if d := Compare(part, "1.2.3", "", edited); !d.Stale || d.Changed[0] != "a" {
		t.Fatalf("a subset install must still watch its own skills: %+v", d)
	}
}

func TestCompare_FallsBackToVersion(t *testing.T) {
	// Names recorded with no digest (a legacy record) → version basis.
	st := &State{Version: "1.2.3", Installs: []Install{global(map[string]string{"a": ""})}}
	if d := Compare(st, "1.3.0", "", map[string]string{"a": "h"}); d.Basis != BasisVersion || !d.Stale {
		t.Fatalf("record without digests must use the version basis: %+v", d)
	}
	// Digests recorded but none computable → version basis, not "everything changed".
	st2 := &State{Version: "1.2.3", Installs: []Install{global(map[string]string{"a": "h"})}}
	if d := Compare(st2, "1.2.3", "", nil); d.Basis != BasisVersion || d.Stale {
		t.Fatalf("uncomputable digests must fall back, not cry drift: %+v", d)
	}
	// Neither basis: no digests and an unstamped build → no verdict at all.
	if d := Compare(st, "dev", "", nil); d.Basis != BasisNone || d.Stale {
		t.Fatalf("nothing to compare on must yield no verdict: %+v", d)
	}
}

func TestCheckStale_ContentBasis(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("MEXC_CLI_NO_SKILLS_NOTIFIER", "")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	recorded := map[string]string{"mexc-common": "h1", "mexc-spot-trade": "h2"}
	if err := Record("1.2.3", global(recorded), epoch); err != nil {
		t.Fatal(err)
	}

	// Version moved, content did not: silent.
	stubDigester(t, recorded, nil)
	if n := CheckStale("1.9.0"); n != nil {
		t.Fatalf("a version-only bump must not nudge, got %q", n.Message())
	}
	// Content changed: nudge names what changed AND the command that refreshes
	// this destination, not the bare one whose defaults would rewrite the choice.
	stubDigester(t, map[string]string{"mexc-common": "h1", "mexc-spot-trade": "EDITED"}, nil)
	n := CheckStale("1.2.3")
	if n == nil || n.Basis != BasisContent {
		t.Fatalf("content drift must nudge on the content basis, got %+v", n)
	}
	want := "mexc-cli skill docs changed: mexc-spot-trade; run: mexc-cli skills install --agent claude-code"
	if got := n.Message(); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestInstallCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Install
		want string
	}{
		{"global, default agent, everything",
			global(map[string]string{"a": "h"}),
			"mexc-cli skills install --agent claude-code"},
		{"a subset spells out its skills",
			subset(map[string]string{"b": "h", "a": "h"}),
			"mexc-cli skills install --agent claude-code --skill a --skill b"},
		{"project scope carries --project",
			Install{Scope: ScopeProject, Root: "/p", Agents: []string{"cursor"}, All: true, Skills: map[string]string{"a": "h"}},
			"mexc-cli skills install --project --agent cursor"},
		{"every agent collapses to --all-agents",
			Install{Scope: ScopeGlobal, Agents: []string{AgentsAll}, All: true, Skills: map[string]string{"a": "h"}},
			"mexc-cli skills install --all-agents"},
	} {
		if got := tc.in.InstallCommand("mexc-cli"); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The fix command is only exact when the destination is unambiguous. Guessing
// one is how the bare command silently rewrote the user's choice in the first
// place, so an unknown or multiple destination falls back to the bare command.
func TestFixCommand_OnlyExactWhenUnambiguous(t *testing.T) {
	one := &State{Installs: []Install{global(map[string]string{"a": "h"})}}
	if got := fixCommand(one, ""); got != "mexc-cli skills install --agent claude-code" {
		t.Errorf("single destination = %q", got)
	}
	legacy := &State{Installs: []Install{{Scope: ScopeUnknown, All: true, Skills: map[string]string{"a": "h"}}}}
	if got := fixCommand(legacy, ""); got != "mexc-cli skills install" {
		t.Errorf("unknown destination must not be invented: %q", got)
	}
	root := filepath.Join(string(filepath.Separator), "tmp", "proj")
	two := &State{Installs: []Install{
		global(map[string]string{"a": "h"}),
		{Scope: ScopeProject, Root: root, All: true, Skills: map[string]string{"a": "h"}},
	}}
	if got := fixCommand(two, root); got != "mexc-cli skills install" {
		t.Errorf("two applicable destinations cannot be named on one line: %q", got)
	}
}

func TestNameList_CapsTheNames(t *testing.T) {
	if got := NameList([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("short list = %q", got)
	}
	if got := NameList([]string{"a", "b", "c"}); got != "a, b, c" {
		t.Fatalf("exactly the cap = %q", got)
	}
	if got := NameList([]string{"a", "b", "c", "d", "e"}); got != "a, b, c and 2 more" {
		t.Fatalf("over the cap = %q", got)
	}
}

func TestRead_ColdStart(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	_, ok, err := Read()
	if ok || err != nil {
		t.Fatalf("cold start should be (nil,false,nil), got ok=%v err=%v", ok, err)
	}
}

func TestRead_CorruptIsAbsent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".mexc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mexc", stateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok, err := Read()
	if ok || err != nil {
		t.Fatalf("corrupt state should read as absent, got ok=%v err=%v", ok, err)
	}
}

func TestClear(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	// A missing file is not an error: uninstall may run when nothing was recorded.
	if err := Clear(); err != nil {
		t.Fatalf("Clear on a cold start: %v", err)
	}
	if err := Record("1.0.0", global(map[string]string{"a": "h"}), epoch); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, ok, _ := Read(); ok {
		t.Fatal("state must be gone after Clear")
	}
}

func TestCheckStale(t *testing.T) {
	t.Setenv("CI", "")                          // ensure the CI skip rule is off
	t.Setenv("MEXC_CLI_NO_SKILLS_NOTIFIER", "") // and the opt-out
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	// Cold start: no state, no notice.
	if n := CheckStale("1.2.3"); n != nil {
		t.Fatalf("cold start should not nudge, got %+v", n)
	}
	// A record with names but no digests stays on the version basis.
	if err := Record("1.2.3", global(map[string]string{"a": ""}), epoch); err != nil {
		t.Fatal(err)
	}
	if n := CheckStale("v1.2.3"); n != nil {
		t.Fatalf("matching version should not nudge, got %+v", n)
	}
	// Drift: notice names both versions.
	n := CheckStale("1.3.0")
	if n == nil || n.Current != "1.2.3" || n.Target != "1.3.0" {
		t.Fatalf("drift notice = %+v", n)
	}
	want := "mexc-cli skills 1.2.3 are out of sync with binary 1.3.0; run: mexc-cli skills install --agent claude-code"
	if got := n.Message(); got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestSkipStaleCheck(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("MEXC_CLI_NO_SKILLS_NOTIFIER", "")
	if !skipStaleCheck("dev") || !skipStaleCheck("") {
		t.Fatal("dev/empty build must skip")
	}
	if skipStaleCheck("1.0.0") {
		t.Fatal("a stamped release version must not skip")
	}
	t.Setenv("MEXC_CLI_NO_SKILLS_NOTIFIER", "1")
	if !skipStaleCheck("1.0.0") {
		t.Fatal("opt-out must skip")
	}
}

// Comparable answers only "does this version mean anything", so it must NOT be
// affected by the CI / opt-out rules — `doctor` runs on purpose and reports
// regardless of them, and it shares this predicate with the startup nudge.
func TestComparable_IgnoresTheSpeakingRules(t *testing.T) {
	t.Setenv("CI", "1")
	t.Setenv("MEXC_CLI_NO_SKILLS_NOTIFIER", "1")
	if !Comparable("1.0.0") {
		t.Fatal("a stamped version is comparable regardless of CI / opt-out")
	}
	if Comparable("dev") || Comparable("") || Comparable("  ") {
		t.Fatal("an unstamped build is never comparable")
	}
}

func TestStale(t *testing.T) {
	if Stale("1.2.3", "v1.2.3") || Stale("v1.2.3", "1.2.3") {
		t.Error("a leading v must not count as drift")
	}
	if !Stale("1.2.3", "1.3.0") {
		t.Error("differing versions are drift")
	}
	// Any difference counts, not just "older": a binary older than the docs beside
	// it has drifted too, and the remedy is the same.
	if !Stale("1.3.0", "1.2.3") {
		t.Error("a newer record than the binary is still drift")
	}
}
