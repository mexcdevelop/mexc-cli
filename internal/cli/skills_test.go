package cli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
)

// fakeSkillsFS mimics the embedded layout: one shared skill (depth 3) and one
// operation subdomain (depth 4).
func fakeSkillsFS() fs.FS {
	return fstest.MapFS{
		"skills/mexc-common/SKILL.md": {Data: []byte("---\nname: mexc-common\ndescription: shared\n---\nbody\n")},
		"skills/spot/trade/SKILL.md":  {Data: []byte("---\nname: spot-trade\ndescription: trade\n---\nbody\n")},
		// A supporting reference doc must ship alongside its SKILL.md...
		"skills/spot/trade/references/fields.md": {Data: []byte("field dictionary\n")}, // publish-ok: a synthetic fixture path
		// ...but the skill's Go (api/*.go) must never be packaged, even though a
		// real embed FS would not contain it — the allowlist is the safety net.
		"skills/spot/trade/api/trade.go": {Data: []byte("package tradeapi\n")},
	}
}

// fakeSkillsFSes is the injected SKILL.md set for skills tests, replacing the
// former package-level global: an install/export test either passes it to
// exportSkills or sets it on the Factory (f.SkillsFS), mirroring how framework.Run
// injects an edition's Edition.SkillsFS into cli.NewApp.
func fakeSkillsFSes() []fs.FS { return []fs.FS{fakeSkillsFS()} }

// stubNPX makes npx look present and captures the args the runner is invoked
// with, returning runErr from the runner.
func stubNPX(t *testing.T, runErr error) *[]string {
	t.Helper()
	var got []string
	oldLook, oldRun := npxLookPath, npxRunner
	npxLookPath = func(string) (string, error) { return "/usr/bin/npx", nil }
	npxRunner = func(_ context.Context, args []string, _ io.Writer) error {
		got = args
		return runErr
	}
	t.Cleanup(func() { npxLookPath, npxRunner = oldLook, oldRun })
	return &got
}

func TestScopeAndAgentArgs(t *testing.T) {
	if got := scopeArgs(false); len(got) != 1 || got[0] != "-g" {
		t.Fatalf("global scope = %v, want [-g]", got)
	}
	if got := scopeArgs(true); got != nil {
		t.Fatalf("project scope = %v, want nil", got)
	}
	if got := agentArgs([]string{"claude-code", "cursor"}); strings.Join(got, " ") != "-a claude-code -a cursor" {
		t.Fatalf("agentArgs = %v", got)
	}
}

func TestExportSkills(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "src")
	names, err := exportSkills(dest, fakeSkillsFSes())
	if err != nil {
		t.Fatalf("exportSkills: %v", err)
	}
	if strings.Join(names, ",") != "mexc-common,mexc-spot-trade" {
		t.Fatalf("names = %v (a reference doc must not add a duplicate skill name)", names)
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dest, n, "SKILL.md")); err != nil {
			t.Fatalf("missing exported %s: %v", n, err)
		}
	}
	// A references/ file travels with its SKILL.md so {baseDir}/references/... resolves.
	if _, err := os.Stat(filepath.Join(dest, "mexc-spot-trade", "references", "fields.md")); err != nil { // publish-ok: asserts the synthetic fixture above
		t.Fatalf("references doc was not exported alongside SKILL.md: %v", err)
	}
	// The skill's Go must NOT be packaged (allowlist excludes api/ and *.go).
	for _, bad := range []string{
		filepath.Join(dest, "mexc-spot-trade", "api", "trade.go"),
		filepath.Join(dest, "mexc-spot-trade", "trade.go"),
	} {
		if _, err := os.Stat(bad); !os.IsNotExist(err) {
			t.Fatalf("skill Go must not be exported, but found %s", bad)
		}
	}
}

func TestExportSkills_WipesStale(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "src")
	// A leftover skill from a previous export must not survive a re-export.
	if err := os.MkdirAll(filepath.Join(dest, "mexc-old-gone"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := exportSkills(dest, fakeSkillsFSes()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "mexc-old-gone")); !os.IsNotExist(err) {
		t.Fatal("stale skill dir should have been wiped")
	}
}

func TestRunSkillsInstall_ArgsAndState(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}

	if err := runSkillsInstall(context.Background(), f, false, []string{"claude-code"}, nil, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	// npx -y skills add <src> -g -a claude-code -y
	joined := strings.Join(*got, " ")
	for _, want := range []string{"-y skills add", "-g", "-a claude-code"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
	if (*got)[len(*got)-1] != "-y" {
		t.Fatalf("last arg should be -y (installer non-interactive): %v", *got)
	}
	// State recorded for the staleness check.
	st, ok, err := skillstate.Read()
	if err != nil || !ok || len(st.Names()) != 2 {
		t.Fatalf("state after install: ok=%v err=%v st=%+v", ok, err, st)
	}
}

func TestRunSkillsInstall_Project_NoGlobalFlag(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, true, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	for _, a := range *got {
		if a == "-g" {
			t.Fatalf("--project must not pass -g: %v", *got)
		}
	}
}

func TestRunSkillsInstall_DryRunDoesNotRun(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	ran := false
	oldLook, oldRun := npxLookPath, npxRunner
	npxLookPath = func(string) (string, error) { return "/usr/bin/npx", nil }
	npxRunner = func(context.Context, []string, io.Writer) error { ran = true; return nil }
	t.Cleanup(func() { npxLookPath, npxRunner = oldLook, oldRun })

	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, false, true); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("dry-run must not invoke npx")
	}
	if _, ok, _ := skillstate.Read(); ok {
		t.Fatal("dry-run must not write state")
	}
	// dry-run must be side-effect free: it must not create/wipe the export dir.
	if src, _ := skillstate.SrcDir(); dirExists(src) {
		t.Fatalf("dry-run must not create the export dir %s", src)
	}
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func TestRequireNPX_Missing(t *testing.T) {
	old := npxLookPath
	npxLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { npxLookPath = old })

	err := requireNPX()
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeDependencyMissing {
		t.Fatalf("want dependency_missing, got %v", err)
	}
	if !strings.Contains(e.Hint, "Node.js") {
		t.Fatalf("hint should point to Node.js: %q", e.Hint)
	}
}

func TestRunSkillsUninstall_RequiresNames(t *testing.T) {
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	err := runSkillsUninstall(context.Background(), f, false, nil, nil, false, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeMissingArgument {
		t.Fatalf("want missing_argument, got %v", err)
	}
}

func TestRunSkillsUninstall_Args(t *testing.T) {
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsUninstall(context.Background(), f, false, []string{"cursor"}, []string{"mexc-common"}, false, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*got, " ")
	for _, want := range []string{"skills remove mexc-common", "-g", "-a cursor"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q missing %q", joined, want)
		}
	}
}

func TestRunSkillsInstall_SkillSubset(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, false, nil, []string{"mexc-common"}, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*got, " "), "-s mexc-common") {
		t.Fatalf("--skill should pass -s mexc-common: %v", *got)
	}
}

func TestRunSkillsInstall_UnknownSkill(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	err := runSkillsInstall(context.Background(), f, false, nil, []string{"nope"}, false, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("want invalid_argument for unknown skill, got %v", err)
	}
}

func TestRunSkillsUninstall_All(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	// Record an install so --all has something to remove.
	rec := skillstate.Install{Scope: skillstate.ScopeGlobal, All: true,
		Skills: map[string]string{"mexc-common": "", "mexc-spot-trade": ""}}
	if err := skillstate.Record("1.0.0", rec, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	// Seed a skills-src staging dir so we can assert --all wipes it.
	src, _ := skillstate.SrcDir()
	if err := os.MkdirAll(filepath.Join(src, "mexc-common"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsUninstall(context.Background(), f, false, nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "remove mexc-common mexc-spot-trade") {
		t.Fatalf("--all should remove recorded names: %v", *got)
	}
	// State cleared so no stale nudge later.
	if _, ok, _ := skillstate.Read(); ok {
		t.Fatal("--all should clear the state file")
	}
	// Export staging dir wiped too.
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("--all should remove skills-src (%s), stat err=%v", src, err)
	}
}

func TestRunSkillsUninstall_AllNothingRecorded(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	err := runSkillsUninstall(context.Background(), f, false, nil, nil, true, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeFailedPrecondition {
		t.Fatalf("want failed_precondition when nothing recorded, got %v", err)
	}
}

func TestRunSkillsUninstall_AllPlusNamesRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	err := runSkillsUninstall(context.Background(), f, false, nil, []string{"mexc-common"}, true, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("want invalid_argument for --all + names, got %v", err)
	}
}

func TestResolveAgentArgs(t *testing.T) {
	// Default (no flags): the conservative single agent, not the installer's fan-out.
	if got, err := resolveAgentArgs(nil, false); err != nil || strings.Join(got, " ") != "-a "+defaultAgent {
		t.Fatalf("default = %v (err %v), want [-a %s]", got, err, defaultAgent)
	}
	// --all-agents: the installer's wildcard.
	if got, err := resolveAgentArgs(nil, true); err != nil || strings.Join(got, " ") != "-a *" {
		t.Fatalf("all-agents = %v (err %v), want [-a *]", got, err)
	}
	// Explicit --agent overrides the default.
	if got, err := resolveAgentArgs([]string{"cursor", "zed"}, false); err != nil || strings.Join(got, " ") != "-a cursor -a zed" {
		t.Fatalf("explicit = %v (err %v)", got, err)
	}
	// --agent + --all-agents conflict.
	if _, err := resolveAgentArgs([]string{"cursor"}, true); err == nil {
		t.Fatal("--agent + --all-agents must conflict")
	}
}

func TestRunSkillsInstall_DefaultAgentIsSingle(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	// No --agent, no --all-agents → only the default agent, never a wildcard.
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*got, " ")
	if !strings.Contains(joined, "-a "+defaultAgent) {
		t.Fatalf("default install must target %s: %v", defaultAgent, *got)
	}
	if strings.Contains(joined, "-a *") {
		t.Fatalf("default install must NOT fan out to all agents: %v", *got)
	}
}

func TestRunSkillsInstall_AllAgents(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	got := stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(*got, " "), "-a *") {
		t.Fatalf("--all-agents must pass -a *: %v", *got)
	}
}

func TestRunSkillsInstall_AgentPlusAllAgentsRejected(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	err := runSkillsInstall(context.Background(), f, false, []string{"cursor"}, nil, true, false)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("want invalid_argument for --agent + --all-agents, got %v", err)
	}
}

// digestOf is the digest map for one FS, for the content-drift assertions below.
func digestOf(t *testing.T, sfs fs.FS) map[string]string {
	t.Helper()
	d, err := SkillContentDigests([]fs.FS{sfs})
	if err != nil {
		t.Fatalf("SkillContentDigests: %v", err)
	}
	return d
}

// The digest must be per skill, stable across calls, and cover every shipped
// file. Stability matters most: an unstable hash would report drift on every
// command, which is the failure mode this whole mechanism exists to avoid.
func TestSkillContentDigests_PerSkillAndStable(t *testing.T) {
	first := digestOf(t, fakeSkillsFS())
	if len(first) != 2 || first["mexc-common"] == "" || first["mexc-spot-trade"] == "" {
		t.Fatalf("want a digest per skill, got %v", first)
	}
	if first["mexc-common"] == first["mexc-spot-trade"] {
		t.Fatal("two different skills must not share a digest")
	}
	second := digestOf(t, fakeSkillsFS())
	for name, sum := range first {
		if second[name] != sum {
			t.Fatalf("%s digest is not stable: %q then %q", name, sum, second[name])
		}
	}
}

// A skill's Go is not shipped, so editing it must not read as a docs change —
// otherwise every API tweak would nag the user to reinstall identical docs.
func TestSkillContentDigests_IgnoresUnshippedFiles(t *testing.T) {
	base := fakeSkillsFS().(fstest.MapFS)
	edited := fstest.MapFS{}
	for k, v := range base {
		edited[k] = v
	}
	edited["skills/spot/trade/api/trade.go"] = &fstest.MapFile{Data: []byte("package tradeapi // edited\n")}

	before, after := digestOf(t, base), digestOf(t, edited)
	if before["mexc-spot-trade"] != after["mexc-spot-trade"] {
		t.Fatal("editing api/*.go must not change a skill's content digest")
	}
}

// ...but a reference doc IS shipped, so editing it must change that skill's
// digest and only that skill's.
func TestSkillContentDigests_CoversReferenceFiles(t *testing.T) {
	base := fakeSkillsFS().(fstest.MapFS)
	edited := fstest.MapFS{}
	for k, v := range base {
		edited[k] = v
	}
	edited["skills/spot/trade/references/fields.md"] = &fstest.MapFile{Data: []byte("field dictionary v2\n")} // publish-ok: a synthetic fixture path

	before, after := digestOf(t, base), digestOf(t, edited)
	if before["mexc-spot-trade"] == after["mexc-spot-trade"] {
		t.Fatal("editing a shipped reference doc must change the skill's digest")
	}
	if before["mexc-common"] != after["mexc-common"] {
		t.Fatal("an untouched skill's digest must not move")
	}
}

// The install record must carry the digests of the bytes just exported, so the
// next run compares content rather than build versions.
func TestRunSkillsInstall_RecordsContentDigests(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	st, ok, err := skillstate.Read()
	if !ok || err != nil {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	want := digestOf(t, fakeSkillsFS())
	got := st.Digests("")
	if len(got) != len(want) {
		t.Fatalf("recorded digests = %v, want %v", got, want)
	}
	for name, sum := range want {
		if got[name] != sum {
			t.Errorf("%s recorded %q, want %q", name, got[name], sum)
		}
	}
}

// installedEntry returns the single recorded destination, failing if there is not
// exactly one.
func installedEntry(t *testing.T) skillstate.Install {
	t.Helper()
	st, ok, err := skillstate.Read()
	if err != nil || !ok {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	if len(st.Installs) != 1 {
		t.Fatalf("want one destination, got %+v", st.Installs)
	}
	return st.Installs[0]
}

// --skill filters at the installer, so the record must hold that subset only.
// Recording the whole export source claimed skills that were never placed.
func TestRunSkillsInstall_RecordsOnlyTheInstalledSubset(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	stubNPX(t, nil)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	if err := runSkillsInstall(context.Background(), f, false, nil, []string{"mexc-common"}, false, false); err != nil {
		t.Fatalf("install: %v", err)
	}
	in := installedEntry(t)
	if len(in.Skills) != 1 {
		t.Fatalf("recorded %v, want only the requested skill", in.Skills)
	}
	if _, ok := in.Skills["mexc-common"]; !ok {
		t.Fatalf("recorded %v, want mexc-common", in.Skills)
	}
	if in.All {
		t.Error("a --skill install does not track the complete set")
	}
	if in.Skills["mexc-common"] == "" {
		t.Error("the subset must still carry its content digest")
	}
}

// THE regression the inventory shape exists to prevent: recording the last
// command's set instead of merging would drop the first install, and
// `uninstall --all` would leave it on disk with nothing recording that it is
// there — a silent leak, worse than the over-removal it replaced.
func TestRunSkillsInstall_SuccessiveSubsetsBothSurviveUninstallAll(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	stubNPX(t, nil)
	if err := runSkillsInstall(context.Background(), f, false, nil, []string{"mexc-common"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := runSkillsInstall(context.Background(), f, false, nil, []string{"mexc-spot-trade"}, false, false); err != nil {
		t.Fatal(err)
	}
	got := stubNPX(t, nil)
	if err := runSkillsUninstall(context.Background(), f, false, nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*got, " ")
	for _, want := range []string{"mexc-common", "mexc-spot-trade"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("--all must remove every skill installed so far, missing %q in %v", want, *got)
		}
	}
}

// A named removal has to subtract, or the record keeps drift-checking a skill
// that is no longer installed.
func TestRunSkillsUninstall_NamedRemovalUpdatesTheRecord(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}
	stubNPX(t, nil)
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if err := runSkillsUninstall(context.Background(), f, false, nil, []string{"mexc-common"}, false, false); err != nil {
		t.Fatal(err)
	}
	st, ok, err := skillstate.Read()
	if err != nil || !ok {
		t.Fatalf("Read: ok=%v err=%v", ok, err)
	}
	names := st.Names()
	if len(names) != 1 || names[0] != "mexc-spot-trade" {
		t.Fatalf("names = %v, want the uninstalled skill gone", names)
	}
}

// The destination is recorded so a later notice can name the command that
// refreshes it, instead of a bare one whose defaults would rewrite the choice.
func TestRunSkillsInstall_RecordsTheDestination(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	f := &Factory{Out: io.Discard, Err: io.Discard, SkillsFS: fakeSkillsFSes()}

	stubNPX(t, nil)
	if err := runSkillsInstall(context.Background(), f, false, nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	in := installedEntry(t)
	if in.Scope != skillstate.ScopeGlobal || in.Root != "" {
		t.Errorf("global install recorded as %+v", in)
	}
	if len(in.Agents) != 1 || in.Agents[0] != skillstate.AgentsAll {
		t.Errorf("--all-agents must record %q, got %v", skillstate.AgentsAll, in.Agents)
	}
	if !in.All {
		t.Error("an install with no --skill tracks the complete set")
	}

	// A project install records the directory it belongs to, so drift for one
	// project is not reported in every other one.
	homeDir2 := t.TempDir()
	t.Setenv("HOME", homeDir2)
	t.Setenv("USERPROFILE", homeDir2)
	stubNPX(t, nil)
	if err := runSkillsInstall(context.Background(), f, true, []string{"cursor"}, nil, false, false); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	in = installedEntry(t)
	if in.Scope != skillstate.ScopeProject || in.Root != wd {
		t.Errorf("project install recorded as %+v, want root %s", in, wd)
	}
	if len(in.Agents) != 1 || in.Agents[0] != "cursor" {
		t.Errorf("agents = %v, want the explicit one", in.Agents)
	}
}
