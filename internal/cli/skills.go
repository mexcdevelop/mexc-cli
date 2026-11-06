package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
)

// The compile-time-embedded SKILL.md trees are injected per edition via
// Factory.SkillsFS (framework.Run bridges Edition.SkillsFS → cli.NewApp): the
// root `assets` package carries the //go:embed directives (embed cannot escape
// its package dir), and each edition selects its own set. Two path layouts
// inside them:
// skills/<domain>/<subdomain>/SKILL.md (operation subdomains) and
// skills/<shared>/SKILL.md (CLI-maintained shared skills, e.g. mexc-common).

// npxRunner runs `npx <args...>`, streaming its combined output to w. It is a
// package var so tests can stub the subprocess. `skills install/uninstall/list`
// delegate to the `skills` multi-agent installer (github.com/vercel-labs/skills),
// run unpinned via `npx -y skills …`.
var npxRunner = func(ctx context.Context, args []string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "npx", args...)
	cmd.Stdout = w
	cmd.Stderr = w
	// stdin left unconnected: the installer auto-detects an agent and runs
	// non-interactively, and we pass -y, so no prompt is ever awaited.
	return cmd.Run()
}

// newSkillsCmd builds the `skills` command group: install / uninstall / list /
// update, each delegating to the `skills` installer so a single SKILL.md source
// (embedded in this binary) reaches every supported AI tool.
func newSkillsCmd(f *Factory, fe *finalErr) *cobra.Command {
	skills := mountGroup("skills", "Manage skill docs for AI tools", f, fe)
	skills.AddCommand(newSkillsInstallCmd(f, fe))
	skills.AddCommand(newSkillsUninstallCmd(f, fe))
	skills.AddCommand(newSkillsListCmd(f, fe))
	skills.AddCommand(newSkillsUpdateCmd(f, fe))
	return skills
}

// skillPlan is one file to write: from the embedded path to the on-disk target.
type skillPlan struct {
	name   string // mexc-<domain>-<subdomain>, or a shared skill's own name
	target string // absolute destination path of SKILL.md
	rel    string // path within the skill, slash-separated (SKILL.md, references/x.md)  // publish-ok: an illustrative path, not a real file
	src    string // path inside srcFS
	srcFS  fs.FS  // the embedded FS src is read from
}

func newSkillsInstallCmd(f *Factory, fe *finalErr) *cobra.Command {
	var (
		project   bool
		dryRun    bool
		agents    []string
		skills    []string
		allAgents bool
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the embedded SKILL.md into your AI tools (delegates to `npx skills`)",
		Long: "Exports the embedded SKILL.md files to ~/.mexc/skills-src and installs them\n" +
			"into your AI coding tools via the `skills` installer (needs Node.js/npx on PATH).\n\n" +
			"Defaults when run with no flags:\n" +
			"  • scope : GLOBAL (~/) — use --project for the current project instead.\n" +
			"  • agent : ONLY " + defaultAgent + " (→ ~/.claude/skills/) — a conservative single\n" +
			"            target. Pick others with --agent <id> (repeatable), or --all-agents\n" +
			"            for every detected agent (~70).\n" +
			"            Internal agent ids: " + internalAgentIDs + "\n" +
			"            (note: Kiro's id is `kiro-cli`; cursor/codex land in ~/.agents/skills/).\n" +
			"  • skills: ALL embedded skills — use --skill <name> (repeatable) to limit.\n\n" +
			"--dry-run prints the exact `npx skills` command without installing.",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := runSkillsInstall(c.Context(), f, project, agents, skills, allAgents, dryRun); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Install into the current project instead of globally (~/)")
	cmd.Flags().StringArrayVar(&agents, "agent", nil, "Target specific AI tool(s), repeatable (ids: "+internalAgentIDs+"); default: only "+defaultAgent)
	cmd.Flags().BoolVar(&allAgents, "all-agents", false, "Install to every detected agent instead of only "+defaultAgent)
	cmd.Flags().StringArrayVar(&skills, "skill", nil, "Install only the named skill(s) (repeatable); omit to install all embedded skills")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would run without installing")
	return cmd
}

func newSkillsUpdateCmd(f *Factory, fe *finalErr) *cobra.Command {
	var (
		project   bool
		dryRun    bool
		agents    []string
		skills    []string
		allAgents bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Re-install the embedded SKILL.md so installed docs match this binary",
		Long: "Alias of `install`: re-exports the embedded SKILL.md and re-installs them\n" +
			"(idempotent), refreshing the on-disk copies to this binary's version. Run it\n" +
			"after upgrading " + cliname.Name() + " to clear a 'skills out of sync' notice.\n" +
			"Same defaults as install (global, only " + defaultAgent + ", all skills).",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := runSkillsInstall(c.Context(), f, project, agents, skills, allAgents, dryRun); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Update in the current project instead of globally (~/)")
	cmd.Flags().StringArrayVar(&agents, "agent", nil, "Target specific AI tool(s), repeatable (ids: "+internalAgentIDs+"); default: only "+defaultAgent)
	cmd.Flags().BoolVar(&allAgents, "all-agents", false, "Update every detected agent instead of only "+defaultAgent)
	cmd.Flags().StringArrayVar(&skills, "skill", nil, "Update only the named skill(s) (repeatable); omit for all")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would run without installing")
	return cmd
}

func newSkillsUninstallCmd(f *Factory, fe *finalErr) *cobra.Command {
	var (
		project   bool
		all       bool
		agents    []string
		allAgents bool
	)
	cmd := &cobra.Command{
		Use:   "uninstall [<skill>...]",
		Short: "Remove installed skills from your AI tools (delegates to `npx skills`)",
		Long: "Removes skills from your AI tools via the `skills` installer. Name one or\n" +
			"more skills, or pass --all to remove every skill this binary installed —\n" +
			"only this binary's own (tracked in its skills-state.json), never other\n" +
			"tools' skills. List installed skills with `" + cliname.Name() + " skills list`.\n\n" +
			"Same scope/agent defaults as install: GLOBAL (~/, --project for project) and\n" +
			"ONLY " + defaultAgent + " (--agent <name> for others, --all-agents for every\n" +
			"agent) — so it removes from where install placed them.",
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if err := runSkillsUninstall(c.Context(), f, project, agents, args, all, allAgents); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "Remove from the current project instead of globally (~/)")
	cmd.Flags().BoolVar(&all, "all", false, "Remove every skill this binary installed (from its skills-state.json; not other tools' skills)")
	cmd.Flags().StringArrayVar(&agents, "agent", nil, "Scope removal to specific AI tool(s), repeatable (ids: "+internalAgentIDs+"); default: only "+defaultAgent)
	cmd.Flags().BoolVar(&allAgents, "all-agents", false, "Remove from every detected agent instead of only "+defaultAgent)
	return cmd
}

func newSkillsListCmd(f *Factory, fe *finalErr) *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills (delegates to `npx skills`)",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := runSkillsList(c.Context(), f, project); err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "List the current project's skills instead of the global (~/) ones")
	return cmd
}

// npxLookPath resolves npx on PATH; a package var so tests can stub presence.
var npxLookPath = exec.LookPath

// requireNPX fails closed with an actionable hint when npx is not on PATH. B4
// decision A (short term): no silent degrade — tell the user to install Node.js
// (which provides npx) and retry. Distribution via npm (which guarantees npx) is
// the long-term fix (blueprint B5).
func requireNPX() error {
	if _, err := npxLookPath("npx"); err != nil {
		return errs.Validation(errs.SubtypeDependencyMissing,
			"`npx` was not found on PATH").
			WithHint("%s skills delegates to the `skills` installer, which needs Node.js. "+
				"Install Node.js from https://nodejs.org (it provides npx), then re-run `%s skills install`.",
				cliname.Name(), cliname.Name())
	}
	return nil
}

// scopeArgs maps --project to the installer's scope: global (-g) by default,
// project (no flag) when project is true.
func scopeArgs(project bool) []string {
	if project {
		return nil
	}
	return []string{"-g"}
}

// defaultAgent is the single AI tool `skills install` targets when neither
// --agent nor --all-agents is given. Chosen conservatively (one tool, least
// surprise) instead of the installer's own default, which fans out to EVERY
// known agent (~70). Override with --agent (internal ids: claude-code, kiro-cli,
// cursor, codex), or opt into the fan-out with --all-agents.
const defaultAgent = "claude-code"

// internalAgentIDs are the agents used inside the team, surfaced in --help so a
// user picks the right `skills` id (e.g. Kiro is "kiro-cli", not "kiro").
const internalAgentIDs = "claude-code, kiro-cli, cursor, codex"

// resolveAgentArgs turns the --agent / --all-agents flags into the installer's
// `-a <agent>` arguments. Precedence: the two are mutually exclusive; an explicit
// --agent list wins; --all-agents targets every agent (`-a '*'`); otherwise the
// conservative single defaultAgent.
func resolveAgentArgs(agents []string, allAgents bool) ([]string, error) {
	if allAgents && len(agents) > 0 {
		return nil, errs.Validation(errs.SubtypeInvalidArgument,
			"--all-agents cannot be combined with --agent").
			WithHint("use either --all-agents (every tool) or one/more --agent <name>, not both")
	}
	return agentArgs(resolveAgentNames(agents, allAgents)), nil
}

// resolveAgentNames applies the same precedence as resolveAgentArgs but yields
// the agent ids rather than installer flags. The install record keeps the names,
// so a later notice can name the command that refreshes THIS destination; the
// precedence lives here once so the two cannot disagree.
func resolveAgentNames(agents []string, allAgents bool) []string {
	switch {
	case allAgents:
		return []string{skillstate.AgentsAll}
	case len(agents) > 0:
		return agents
	default:
		return []string{defaultAgent}
	}
}

// agentArgs expands --agent values into repeated `-a <agent>` flags.
func agentArgs(agents []string) []string {
	var out []string
	for _, a := range agents {
		out = append(out, "-a", a)
	}
	return out
}

// skillArgs expands --skill values into repeated `-s <name>` flags (the installer
// filters the source to those skill names).
func skillArgs(skills []string) []string {
	var out []string
	for _, s := range skills {
		out = append(out, "-s", s)
	}
	return out
}

// validateSkillSubset checks that every requested --skill name is one this binary
// actually embeds, so a typo fails fast with the available names rather than
// silently installing nothing.
func validateSkillSubset(requested, available []string) error {
	have := make(map[string]bool, len(available))
	for _, n := range available {
		have[n] = true
	}
	for _, r := range requested {
		if !have[r] {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"unknown skill %q", r).WithParam(r).
				WithHint("available skills: %s", strings.Join(available, ", "))
		}
	}
	return nil
}

func runSkillsInstall(ctx context.Context, f *Factory, project bool, agents, skills []string, allAgents, dryRun bool) error {
	if err := requireNPX(); err != nil {
		return err
	}
	if len(f.SkillsFS) == 0 {
		return errs.Internal("no embedded skills available (SkillsFS not injected)")
	}
	agentA, err := resolveAgentArgs(agents, allAgents)
	if err != nil {
		return err
	}
	src, err := skillstate.SrcDir()
	if err != nil {
		return err
	}
	// Plan the export without writing anything yet, so --dry-run stays side-effect
	// free (it must not wipe/rewrite skills-src). The actual write happens only on
	// the real-install path below.
	plans, err := buildSkillPlans(src, f.SkillsFS)
	if err != nil {
		return err
	}
	names := planNames(plans)
	if len(names) == 0 {
		return errs.Internal("no embedded SKILL.md files found")
	}
	if len(skills) > 0 {
		if err := validateSkillSubset(skills, names); err != nil {
			return err
		}
	}

	args := append([]string{"-y", "skills", "add", src}, scopeArgs(project)...)
	args = append(args, agentA...)
	args = append(args, skillArgs(skills)...)
	args = append(args, "-y")

	if dryRun {
		_, _ = fmt.Fprintf(f.Err, "would export %d skill(s) to %s\n", len(names), src)
		_, _ = fmt.Fprintf(f.Err, "would run: npx %s\n", strings.Join(args, " "))
		return nil
	}
	if err := writeSkillPlans(src, plans); err != nil {
		return err
	}
	if err := npxRunner(ctx, args, f.Err); err != nil {
		return errs.Internal("`npx skills add` failed: %v", err).WithCause(err).
			WithHint("see the installer output above; ensure Node.js/npx work and retry")
	}
	// Record what we installed for the staleness check. Non-fatal on failure: the
	// install succeeded; the state file only powers the "out of sync" nudge.
	//
	// Digests come from the plans already built above, so the record describes the
	// exact bytes just exported — no second walk, and no window in which the
	// content could differ from what was hashed. A digest failure degrades to a
	// version-only record rather than failing a successful install.
	digests, derr := digestsFromPlans(plans)
	if derr != nil {
		_, _ = fmt.Fprintf(f.Err, "warning: could not hash skill content: %v\n", derr)
	}
	// Only the skills actually installed are recorded. `--skill` filters at the
	// installer, so recording the whole export source would claim skills that were
	// never placed — and `uninstall --all` and `doctor` read this as truth.
	installed := names
	if len(skills) > 0 {
		installed = skills
	}
	entry := skillstate.Install{
		Scope:  skillstate.ScopeGlobal,
		Agents: resolveAgentNames(agents, allAgents),
		All:    len(skills) == 0,
		Skills: pickDigests(digests, installed),
	}
	if project {
		// A project install lands relative to the working directory, so the record
		// keeps it: without the root, drift for one project would be reported in
		// every other one.
		entry.Scope, entry.Root = skillstate.ScopeProject, mustGetwd(f)
	}
	if err := skillstate.Record(buildinfo.Version, entry, time.Now()); err != nil {
		_, _ = fmt.Fprintf(f.Err, "warning: could not record skills state: %v\n", err)
	}
	return nil
}

// pickDigests keeps only names, mapping each to its digest. A name whose digest
// is missing maps to "", which records that it is installed while leaving the
// drift check on the version fallback for it.
func pickDigests(digests map[string]string, names []string) map[string]string {
	out := make(map[string]string, len(names))
	for _, n := range names {
		out[n] = digests[n]
	}
	return out
}

// mustGetwd reports the working directory, warning and returning "" if it cannot
// be read. An empty root matches no directory, so the entry stays recorded but
// never claims to apply anywhere — better than guessing a path.
func mustGetwd(f *Factory) string {
	wd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(f.Err, "warning: could not resolve the working directory: %v\n", err)
		return ""
	}
	return wd
}

func runSkillsUninstall(ctx context.Context, f *Factory, project bool, agents, names []string, all, allAgents bool) error {
	if err := requireNPX(); err != nil {
		return err
	}
	agentA, err := resolveAgentArgs(agents, allAgents)
	if err != nil {
		return err
	}
	switch {
	case all && len(names) > 0:
		return errs.Validation(errs.SubtypeInvalidArgument,
			"--all cannot be combined with explicit skill names").
			WithHint("use either `--all` or a list of names, not both")
	case all:
		// Remove only the skills this binary recorded installing — never the
		// installer's own `--all`, which would also wipe other tools' skills.
		st, ok, err := skillstate.Read()
		if err != nil {
			return err
		}
		if !ok || len(st.Names()) == 0 {
			return errs.Validation(errs.SubtypeFailedPrecondition,
				"no %s skills are recorded as installed", cliname.Name()).
				WithHint("nothing to remove; `%s skills install` records what it installs in its skills-state.json", cliname.Name())
		}
		names = st.Names()
	case len(names) == 0:
		return errs.Validation(errs.SubtypeMissingArgument,
			"no skill names given to uninstall").
			WithHint("pass one or more skill names (e.g. `%s skills uninstall mexc-common`), or `--all` to remove every skill this binary installed; see installed names with `%s skills list`",
				cliname.Name(), cliname.Name())
	}

	args := append([]string{"-y", "skills", "remove"}, names...)
	args = append(args, scopeArgs(project)...)
	args = append(args, agentA...)
	args = append(args, "-y")
	if err := npxRunner(ctx, args, f.Err); err != nil {
		return errs.Internal("`npx skills remove` failed: %v", err).WithCause(err).
			WithHint("see the installer output above")
	}
	// A full removal cleans up our own artifacts too: drop the state (so no stale
	// nudge fires) and the export staging dir (safe here — everything is being
	// removed, so no still-installed skill can be symlinked back into it; a single
	// `uninstall <name>` deliberately leaves skills-src in place for that reason).
	if all {
		if err := skillstate.Clear(); err != nil {
			_, _ = fmt.Fprintf(f.Err, "warning: could not clear skills state: %v\n", err)
		}
		if src, serr := skillstate.SrcDir(); serr != nil {
			_, _ = fmt.Fprintf(f.Err, "warning: could not resolve skills-src to clean: %v\n", serr)
		} else if rerr := os.RemoveAll(src); rerr != nil {
			_, _ = fmt.Fprintf(f.Err, "warning: could not remove %s: %v\n", src, rerr)
		}
		return nil
	}
	// A named removal has to subtract from the inventory too, or the record keeps
	// claiming a skill that is gone — `doctor` counts it and the drift check
	// watches it forever.
	if err := skillstate.Forget(buildinfo.Version, names, time.Now()); err != nil {
		_, _ = fmt.Fprintf(f.Err, "warning: could not update skills state: %v\n", err)
	}
	return nil
}

func runSkillsList(ctx context.Context, f *Factory, project bool) error {
	if err := requireNPX(); err != nil {
		return err
	}
	args := append([]string{"-y", "skills", "list"}, scopeArgs(project)...)
	if err := npxRunner(ctx, args, f.Out); err != nil {
		return errs.Internal("`npx skills list` failed: %v", err).WithCause(err)
	}
	return nil
}

// exportSkills materializes every embedded SKILL.md into destDir/<name>/SKILL.md
// and returns the skill names, sorted. destDir is wiped first so a skill removed
// from the binary does not linger in the export source.
func exportSkills(destDir string, fses []fs.FS) ([]string, error) {
	plans, err := buildSkillPlans(destDir, fses)
	if err != nil {
		return nil, err
	}
	if err := writeSkillPlans(destDir, plans); err != nil {
		return nil, err
	}
	return planNames(plans), nil
}

// planNames returns the DISTINCT skill names of the plans, sorted. A skill now
// contributes several plans (SKILL.md plus its references/scripts/assets files),
// so the names are de-duplicated — callers count and validate skills, not files.
func planNames(plans []skillPlan) []string {
	seen := map[string]bool{}
	var names []string
	for _, p := range plans {
		if !seen[p.name] {
			seen[p.name] = true
			names = append(names, p.name)
		}
	}
	sort.Strings(names)
	return names
}

// SkillContentDigests returns one SHA-256 per embedded skill over the files that
// skill ships. It lets the composition root register a content digester with
// pkg/skillstate without exporting the plan machinery, and without widening the
// Deps interface an edition command is written against.
func SkillContentDigests(fses []fs.FS) (map[string]string, error) {
	plans, err := buildSkillPlans("", fses)
	if err != nil {
		return nil, err
	}
	return digestsFromPlans(plans)
}

// digestsFromPlans hashes each skill's shipped files, keyed by skill name.
//
// Each file contributes its in-skill path AND its bytes, both length-prefixed, so
// moving content between files cannot leave the digest unchanged. Files are sorted
// by that path, so a skill hashes identically regardless of walk order across the
// embedded FSes.
//
// Only what ships is hashed: the plans already exclude api/*.go, so editing a
// skill's Go beside its SKILL.md does not read as a docs change.
func digestsFromPlans(plans []skillPlan) (map[string]string, error) {
	bySkill := map[string][]skillPlan{}
	for _, p := range plans {
		bySkill[p.name] = append(bySkill[p.name], p)
	}
	out := make(map[string]string, len(bySkill))
	for name, ps := range bySkill {
		sort.Slice(ps, func(i, j int) bool { return ps[i].rel < ps[j].rel })
		h := sha256.New()
		for _, p := range ps {
			data, err := fs.ReadFile(p.srcFS, p.src)
			if err != nil {
				return nil, errs.Internal("failed to read embedded %s: %v", p.src, err).WithCause(err)
			}
			_, _ = fmt.Fprintf(h, "%d:%s:%d:", len(p.rel), p.rel, len(data))
			h.Write(data)
		}
		out[name] = hex.EncodeToString(h.Sum(nil))
	}
	return out, nil
}

// writeSkillPlans wipes destDir (so a skill dropped from the binary does not
// linger) then writes each plan. This is the only filesystem-mutating step of an
// export — kept separate from buildSkillPlans so a --dry-run can compute names
// without touching disk.
func writeSkillPlans(destDir string, plans []skillPlan) error {
	if err := os.RemoveAll(destDir); err != nil {
		return errs.Internal("cannot reset %s: %v", destDir, err).WithCause(err)
	}
	for _, p := range plans {
		if err := writeSkill(p); err != nil {
			return err
		}
	}
	return nil
}

// skillDocDirs are the supporting-file subdirectories shipped alongside SKILL.md
// (the standard Agent Skills layout: SKILL.md may point at deeper reference docs,
// helper scripts, or assets). api/ (the Go op declarations) and anything outside
// this allowlist is deliberately NEVER packaged into an installed skill.
var skillDocDirs = []string{"references", "scripts", "assets"}

// shippableSkillRel reports whether rel — a skill-root-relative, forward-slashed
// path — belongs in the installed skill: the SKILL.md itself, or a file under
// references/ scripts/ assets/. This allowlist keeps a skill's Go (api/*.go) out
// of the package even if the embed seam ever widens to a directory glob.
func shippableSkillRel(rel string) bool {
	if rel == "SKILL.md" {
		return true
	}
	for _, d := range skillDocDirs {
		if strings.HasPrefix(rel, d+"/") {
			return true
		}
	}
	return false
}

// skillRootOf finds the skill root owning path (the longest root dir that is an
// ancestor) and returns the skill name and the path relative to that root, or
// ok=false for a file under no skill root.
func skillRootOf(path string, roots map[string]string) (name, rel string, ok bool) {
	best := ""
	for dir := range roots {
		if (path == dir+"/SKILL.md" || strings.HasPrefix(path, dir+"/")) && len(dir) > len(best) {
			best = dir
		}
	}
	if best == "" {
		return "", "", false
	}
	return roots[best], strings.TrimPrefix(path, best+"/"), true
}

// buildSkillPlans walks every embedded FS and computes the write plan for each
// skill. A skill is a directory holding a SKILL.md: an operation subdomain
// skills/<domain>/<sub>/SKILL.md becomes mexc-<domain>-<sub>; a shared skill
// skills/<shared>/SKILL.md keeps its own name (e.g. mexc-common). The whole skill
// ships — SKILL.md plus any references/ scripts/ assets/ files, preserving their
// relative path under base/<name>/ — so a SKILL.md that points at
// {baseDir}/references/... resolves once installed. api/*.go and anything outside
// shippableSkillRel is skipped, so a skill's Go can never enter the package. The
// public and internal FSes are disjoint (a skill is embedded in exactly one), so
// no cross-FS de-duplication is needed.
func buildSkillPlans(base string, fses []fs.FS) ([]skillPlan, error) {
	var plans []skillPlan
	for _, sfs := range fses {
		// Pass 1: locate every skill root (a directory holding a SKILL.md) and its
		// installed name, so pass 2 can key any supporting file to its skill.
		roots := map[string]string{} // root dir -> skill name
		err := fs.WalkDir(sfs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) != "SKILL.md" {
				return nil
			}
			dir := strings.TrimSuffix(path, "/SKILL.md")
			parts := strings.Split(path, "/")
			switch len(parts) {
			case 3: // skills/<shared>/SKILL.md — a CLI-maintained shared skill
				roots[dir] = parts[1]
			case 4: // skills/<domain>/<sub>/SKILL.md — an operation subdomain
				roots[dir] = fmt.Sprintf("mexc-%s-%s", parts[1], parts[2])
			}
			return nil
		})
		if err != nil {
			return nil, errs.Internal("failed to scan embedded skills: %v", err).WithCause(err)
		}
		// Pass 2: plan SKILL.md + every allowlisted supporting file under each root.
		err = fs.WalkDir(sfs, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name, rel, ok := skillRootOf(path, roots)
			if !ok || !shippableSkillRel(rel) {
				return nil
			}
			plans = append(plans, skillPlan{
				name:   name,
				target: filepath.Join(base, name, filepath.FromSlash(rel)),
				rel:    rel,
				src:    path,
				srcFS:  sfs,
			})
			return nil
		})
		if err != nil {
			return nil, errs.Internal("failed to scan embedded skills: %v", err).WithCause(err)
		}
	}
	return plans, nil
}

func writeSkill(p skillPlan) error {
	data, err := fs.ReadFile(p.srcFS, p.src)
	if err != nil {
		return errs.Internal("failed to read embedded %s: %v", p.src, err).WithCause(err)
	}
	if err := os.MkdirAll(filepath.Dir(p.target), 0o755); err != nil {
		return errs.Internal("failed to create %s: %v", filepath.Dir(p.target), err).WithCause(err)
	}
	if err := os.WriteFile(p.target, data, 0o644); err != nil {
		return errs.Internal("failed to write %s: %v", p.target, err).WithCause(err)
	}
	return nil
}
