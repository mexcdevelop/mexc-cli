// Package skillstate records what `skills install` laid down and detects when
// those installed docs have fallen behind the running binary.
//
// DRIFT IS DECIDED ON CONTENT. `skills install` records a SHA-256 per skill over
// the files it ships, and a later run re-hashes what this binary would install
// now. Only a differing hash counts as drift, so a release that changes no
// SKILL.md text says nothing — a build version cannot stand in for content,
// because it moves on every release whether the docs changed or not, and a
// notice that cries wolf is one the reader learns to skip.
//
// VERSION COMPARISON IS THE FALLBACK, for a record written before digests
// existed. Compare picks the basis and is the only place that ordering lives.
//
// THE RECORD IS AN INVENTORY, per destination. It holds what is on disk — not
// what the last command's source directory contained — so `install --skill x`
// adds to it, `uninstall x` subtracts from it, and each entry remembers the
// scope and agents it went to. That last part is what lets a notice name the
// command which refreshes the user's OWN choice; a bare `skills install` would
// silently reset scope, agents, and subset to their defaults.
//
// WHAT IT STILL DOES NOT DETECT: whether the installed files are present or
// unmodified. `skills install` delegates placement to `npx skills add`, so the
// destination is not ours to track — this compares the running binary against
// its own install record, not against the agent directories.
//
// WHY IT LIVES IN pkg/. It began inside the CLI engine, where only the engine
// could reach it — and then `doctor`, an edition command, needed the same fact
// for its report. The alternatives were to widen a composition-root interface or
// to re-read the file format in a second place; both are worse than moving code
// that never depended on the engine to begin with. Nothing here imports
// internal/: the state directory comes from pkg/config and the version from
// pkg/buildinfo, so this package sits with the other ports and both the engine
// and an edition can use it.
package skillstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

const stateFile = "skills-state.json"

// Scope is where an install put its skills.
const (
	ScopeGlobal  = "global"  // the user's agent directories under $HOME
	ScopeProject = "project" // one project directory
	ScopeUnknown = ""        // a legacy record does not say
)

// AgentsAll is the agent list of an install that targeted every detected agent.
const AgentsAll = "*"

// Install is what one destination holds. A destination is a scope — the global
// agent directories, or one project directory — NOT an agent: the installer fans
// a single call out to several agents and re-running it is idempotent, so the
// agent list is a union rather than a partition. Keying per agent would also make
// `--all-agents` produce one row per skill per agent.
type Install struct {
	Scope  string   `json:"scope"`
	Root   string   `json:"root,omitempty"` // project directory; empty when global
	Agents []string `json:"agents,omitempty"`
	// All records that this destination tracks the COMPLETE embedded set, i.e. the
	// user passed no --skill. It is the user's intent and cannot be derived: a
	// record that happens to hold every current skill is indistinguishable from a
	// subset install until the binary gains one, which is exactly when the two must
	// behave differently — a full install wants to hear about a new skill, a subset
	// install must not be nagged about one it deliberately skipped.
	All    bool              `json:"all,omitempty"`
	Skills map[string]string `json:"skills"` // skill name -> content digest
}

// State is the inventory of what `skills install` has put on disk, per
// destination, plus the binary version of the most recent install.
//
// It is an INVENTORY, not a snapshot of the last command. `install --skill x`
// adds x to its destination without dropping what a previous install put there,
// and `uninstall x` removes it. Writing the last command's source set instead —
// which is what the flat shape below did — made `uninstall --all` and `doctor`
// describe skills that were never installed.
type State struct {
	Version   string    `json:"version"`
	Installs  []Install `json:"installs,omitempty"`
	UpdatedAt string    `json:"updated_at"`

	// The flat shape written before Installs existed. Read-only: migrate promotes
	// it to one Install of unknown scope, and Record never writes these back.
	LegacySkills  []string          `json:"skills,omitempty"`
	LegacyDigests map[string]string `json:"digests,omitempty"`
}

// migrate promotes a legacy flat record to the install list. The synthesized
// install has ScopeUnknown and no agents because the old format never recorded
// where it went, so a fix command derived from it stays generic instead of
// inventing a destination.
//
// All is true because the flat record always held the complete set (it recorded
// the export source, not the installed subset — the very inaccuracy this shape
// fixes). Assuming otherwise would silently narrow drift detection for every
// record already on disk.
func (s *State) migrate() {
	if len(s.Installs) > 0 || len(s.LegacySkills) == 0 {
		return
	}
	skills := make(map[string]string, len(s.LegacySkills))
	for _, name := range s.LegacySkills {
		skills[name] = s.LegacyDigests[name] // "" when the record predates digests
	}
	s.Installs = []Install{{Scope: ScopeUnknown, All: true, Skills: skills}}
	s.LegacySkills, s.LegacyDigests = nil, nil
}

// underRoot reports whether dir is root or sits inside it.
func underRoot(dir, root string) bool {
	if dir == root {
		return true
	}
	return root != "" && strings.HasPrefix(dir, root+string(filepath.Separator))
}

// Relevant returns the installs that apply at dir: global and legacy ones always,
// a project one only while the caller is inside its root. Without this filter,
// recording project roots would start reporting drift for projects the user is
// not in — the exact noise content digests exist to remove.
func (s *State) Relevant(dir string) []Install {
	var out []Install
	for _, in := range s.Installs {
		if in.Scope != ScopeProject || underRoot(dir, in.Root) {
			out = append(out, in)
		}
	}
	return out
}

// Names lists every skill recorded as installed anywhere, sorted. `uninstall
// --all` uses it, so it deliberately ignores the working directory: a project
// install is still on disk when the user removes everything from elsewhere.
func (s *State) Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, in := range s.Installs {
		for name := range in.Skills {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TracksAll reports whether any install applying at dir follows the complete set.
func (s *State) TracksAll(dir string) bool {
	for _, in := range s.Relevant(dir) {
		if in.All {
			return true
		}
	}
	return false
}

// Digests maps skill name to recorded content digest across the installs that
// apply at dir. Names recorded without a digest (a legacy record) are omitted, so
// an empty result correctly routes the caller to the version fallback.
func (s *State) Digests(dir string) map[string]string {
	out := map[string]string{}
	for _, in := range s.Relevant(dir) {
		for name, sum := range in.Skills {
			if sum != "" {
				out[name] = sum
			}
		}
	}
	return out
}

// InstallCommand renders the command that reproduces this install, so a notice
// can tell the user how to refresh what they actually chose. Without it the only
// advice available is a bare `skills install`, whose defaults (global, one agent,
// every skill) silently rewrite a different choice.
func (in Install) InstallCommand(cli string) string {
	cmd := cli + " skills install"
	if in.Scope == ScopeProject {
		cmd += " --project"
	}
	switch {
	case len(in.Agents) == 1 && in.Agents[0] == AgentsAll:
		cmd += " --all-agents"
	default:
		for _, a := range in.Agents {
			cmd += " --agent " + a
		}
	}
	if in.All {
		return cmd
	}
	for _, name := range in.skillNames() {
		cmd += " --skill " + name
	}
	return cmd
}

func (in Install) skillNames() []string {
	out := make([]string, 0, len(in.Skills))
	for name := range in.Skills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Basis names what a drift verdict was decided on.
const (
	BasisContent = "content" // per-skill digests on both sides
	BasisVersion = "version" // record predates digests; build versions compared
	BasisNone    = "none"    // neither basis available; no verdict
)

// Drift is the outcome of comparing an install record with the running binary.
// Changed is populated on BasisContent only.
type Drift struct {
	Basis    string
	Stale    bool
	Changed  []string
	Recorded string
	Running  string
}

// digester computes the per-skill content digests of the SKILL.md set the
// running binary would install. It is a package-level hook for the same reason
// config.SetStateDirName is: the readers below are free functions called from the
// engine and from an edition command, and the content lives behind an embedded
// FS that neither pkg/ nor the narrow Deps interface should have to carry.
var digester func() (map[string]string, error)

// SetContentDigester registers how this process computes current digests. The
// composition root calls it once, before any command runs. Leaving it unset is
// legal: every reader then falls back to comparing build versions.
func SetContentDigester(fn func() (map[string]string, error)) { digester = fn }

// CurrentDigests reports the digests of what this binary would install now.
// ok is false when no digester is registered or it fails — a caller that gets
// false compares build versions instead, because a broken hash must not be read
// as "everything changed".
func CurrentDigests() (map[string]string, bool) {
	if digester == nil {
		return nil, false
	}
	d, err := digester()
	if err != nil || len(d) == 0 {
		return nil, false
	}
	return d, true
}

// changedSkills names the skills whose content differs between two digest sets:
// added, removed, or re-hashed. Sorted, so a message is stable.
func changedSkills(recorded, current map[string]string) []string {
	var out []string
	for name, sum := range current {
		if recorded[name] != sum {
			out = append(out, name)
		}
	}
	for name := range recorded {
		if _, ok := current[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Compare resolves drift for a record against the running binary, over the
// installs that apply at dir. current is what CurrentDigests returned; pass nil
// to force the version fallback.
//
// A subset install compares only against the skills it holds — it must not be
// told a skill it deliberately skipped has changed. A destination that tracks the
// complete set compares against everything, so a NEW skill counts as drift there.
//
// This is the only place the basis ordering lives, so the startup nudge and
// `doctor` cannot disagree about what counts as drift.
func Compare(st *State, binVersion, dir string, current map[string]string) Drift {
	d := Drift{Basis: BasisNone, Recorded: st.Version, Running: binVersion}
	recorded := st.Digests(dir)
	if len(current) > 0 && len(recorded) > 0 {
		d.Basis = BasisContent
		compared := current
		if !st.TracksAll(dir) {
			compared = narrowTo(current, recorded)
		}
		d.Changed = changedSkills(recorded, compared)
		d.Stale = len(d.Changed) > 0
		return d
	}
	if st.Version != "" && Comparable(binVersion) {
		d.Basis = BasisVersion
		d.Stale = Stale(st.Version, binVersion)
	}
	return d
}

// narrowTo restricts current to the names keys holds, so a partial install is
// compared only against the skills it actually installed.
func narrowTo(current, keys map[string]string) map[string]string {
	out := make(map[string]string, len(keys))
	for name := range keys {
		if sum, ok := current[name]; ok {
			out[name] = sum
		}
	}
	return out
}

// home returns this product's user-state directory. It defers to
// config.StateDir rather than recomputing the path: the directory is chosen by
// the product (Edition.StateDir), and a second computation here would silently
// keep writing skill state to the default while everything else moved.
func home() (string, error) { return config.StateDir() }

// SrcDir is the stable export directory `skills install` writes the embedded
// SKILL.md into before delegating to `npx skills add`. Stable (not a temp dir) so
// re-installs and updates reference one consistent source path.
func SrcDir() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "skills-src"), nil
}

// Path is the state file's location.
func Path() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, stateFile), nil
}

// Read reads the recorded install state. A missing file yields (nil, false,
// nil); a corrupt/unreadable file is treated as absent (nil, false, nil) so a
// damaged state never produces a false staleness nudge.
func Read() (*State, bool, error) {
	p, err := Path()
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, errs.Internal("cannot read skills state: %v", err).WithCause(err)
	}
	var st State
	if json.Unmarshal(data, &st) != nil {
		// Swallowing the decode error is the behavior, not an oversight: a damaged
		// record reads as "nothing installed", which stays silent. Returning the
		// error instead would turn one corrupt file into a warning on every command,
		// about something the reader cannot act on.
		return nil, false, nil //nolint:nilerr // corrupt state is deliberately "absent"
	}
	st.migrate()
	return &st, true, nil
}

// save writes st, stamping the version and clock. It is the only writer, so the
// legacy fields cleared by migrate can never be written back.
func save(st *State, binVersion string, now time.Time) error {
	h, err := home()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h, 0o700); err != nil {
		return errs.Internal("cannot create %s: %v", h, err).WithCause(err)
	}
	st.Version = binVersion
	st.UpdatedAt = now.UTC().Format(time.RFC3339)
	st.LegacySkills, st.LegacyDigests = nil, nil
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return errs.Internal("cannot encode skills state: %v", err).WithCause(err)
	}
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return errs.Internal("cannot write skills state: %v", err).WithCause(err)
	}
	return nil
}

// readForUpdate returns the current state, or an empty one when nothing is
// recorded yet, so a caller can merge into it unconditionally.
func readForUpdate() (*State, error) {
	st, ok, err := Read()
	if err != nil {
		return nil, err
	}
	if !ok || st == nil {
		return &State{}, nil
	}
	return st, nil
}

// Record merges one install into the inventory. Skills MERGE into the matching
// destination rather than replacing it: installing one skill today and another
// tomorrow leaves both on disk, and a record that forgot the first would leak it
// past `uninstall --all`.
//
// now is injected so the caller controls the clock (and tests stay deterministic).
func Record(binVersion string, in Install, now time.Time) error {
	st, err := readForUpdate()
	if err != nil {
		return err
	}
	st.merge(in)
	return save(st, binVersion, now)
}

// merge folds in into the destination it belongs to, matching on scope and root.
func (s *State) merge(in Install) {
	for i := range s.Installs {
		if s.Installs[i].Scope != in.Scope || s.Installs[i].Root != in.Root {
			continue
		}
		s.Installs[i].Agents = unionAgents(s.Installs[i].Agents, in.Agents)
		// All is sticky: once a destination has had the complete set installed, it
		// keeps tracking the complete set even if a later run targets one skill.
		s.Installs[i].All = s.Installs[i].All || in.All
		if s.Installs[i].Skills == nil {
			s.Installs[i].Skills = map[string]string{}
		}
		for name, sum := range in.Skills {
			s.Installs[i].Skills[name] = sum
		}
		return
	}
	s.Installs = append(s.Installs, in)
}

// unionAgents merges two agent lists. AgentsAll absorbs everything: an install
// that targeted every agent already covers any named one.
func unionAgents(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, name := range list {
			if name == AgentsAll {
				return []string{AgentsAll}
			}
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Forget drops names from every destination, for an explicit `uninstall <names>`.
// A destination left holding nothing is removed. Without this the record kept
// reporting an uninstalled skill as installed, and drift-checked it forever.
func Forget(binVersion string, names []string, now time.Time) error {
	st, ok, err := Read()
	if err != nil || !ok || st == nil {
		return err
	}
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	var kept []Install
	for _, in := range st.Installs {
		for name := range in.Skills {
			if drop[name] {
				delete(in.Skills, name)
			}
		}
		if len(in.Skills) > 0 {
			kept = append(kept, in)
		}
	}
	st.Installs = kept
	return save(st, binVersion, now)
}

// Clear removes the recorded install state (used after `uninstall --all`), so no
// stale nudge fires afterward. A missing file is not an error.
func Clear() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errs.Internal("cannot clear skills state: %v", err).WithCause(err)
	}
	return nil
}

// StaleNotice signals that the installed skill docs no longer match the running
// binary.
type StaleNotice struct {
	Basis   string   // BasisContent or BasisVersion
	Current string   // the version the installed skills were recorded at
	Target  string   // the running binary version
	Changed []string // skills whose content differs; BasisContent only
	Fix     string   // the command that refreshes what the user actually installed
}

// maxNamedSkills caps how many skill names a notice spells out. Naming what
// changed is the whole point, but a line long enough to wrap stops being read.
const maxNamedSkills = 3

// Message is a single-line, agent-parseable drift description plus the fix
// command. Shaped as "<what drifted>; run: <cmd>" so an agent can act on it.
func (n StaleNotice) Message() string {
	if n.Basis == BasisContent {
		return fmt.Sprintf("%s skill docs changed: %s; run: %s",
			cliname.Name(), NameList(n.Changed), n.Fix)
	}
	return fmt.Sprintf("%s skills %s are out of sync with binary %s; run: %s",
		cliname.Name(), n.Current, n.Target, n.Fix)
}

// fixCommand is the command a notice tells the user to run. A bare
// `skills install` is wrong advice for anyone who chose a scope, an agent set, or
// a subset: its defaults are global + one agent + every skill, so following it
// would silently rewrite that choice. So the exact command is given whenever the
// destination is unambiguous — exactly one install applies here and it says where
// it went. Otherwise the bare command is the only honest answer.
func fixCommand(st *State, dir string) string {
	bare := cliname.Name() + " skills install"
	relevant := st.Relevant(dir)
	if len(relevant) != 1 || relevant[0].Scope == ScopeUnknown {
		return bare
	}
	return relevant[0].InstallCommand(cliname.Name())
}

// NameList renders skill names for a one-line message, trailing "and N more"
// past maxNamedSkills. Exported because `doctor` renders the same list.
func NameList(names []string) string {
	if len(names) <= maxNamedSkills {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more",
		strings.Join(names[:maxNamedSkills], ", "), len(names)-maxNamedSkills)
}

// Comparable reports whether binVersion is a stamped release version, so that
// comparing it against a recorded one means anything. An unstamped local build
// reports "dev" (pkg/buildinfo's default) for every build ever made, which would
// make every comparison either a false match or a false mismatch.
//
// It is exported because two callers need the same judgement for different
// reasons: the startup nudge stays silent, while `doctor` says out loud that
// staleness could not be evaluated. Both must agree on when a version is
// meaningful, and neither should spell the rule itself.
func Comparable(binVersion string) bool {
	v := strings.TrimSpace(binVersion)
	return v != "" && v != "dev"
}

// Stale reports whether installed and running name different builds, ignoring a
// leading v so "v1.2.3" and "1.2.3" compare equal. The caller is expected to
// have checked Comparable first.
//
// Any difference counts, not just "older". A binary older than the docs beside
// it is drift in the same way, and the remedy is identical.
func Stale(installed, running string) bool {
	return normalizeVersion(installed) != normalizeVersion(running)
}

// CheckStale returns a notice when the installed skill docs have drifted from
// the running binary, or nil for: a skip rule (dev build / CI / opt-out), a cold
// start (no state), an unreadable state, or no drift.
//
// Cold start is deliberately silent: a user who never installed the docs is not
// out of sync with anything, and nagging them to install something they did not
// ask for is how a useful notice becomes noise.
func CheckStale(binVersion string) *StaleNotice {
	if skipStaleCheck(binVersion) {
		return nil
	}
	st, ok, err := Read()
	if err != nil || !ok {
		return nil
	}
	// A failed Getwd yields "", which matches no project root — so the check falls
	// back to global installs only rather than reporting on a project the caller
	// may not be in.
	dir, _ := os.Getwd()
	current, _ := CurrentDigests()
	d := Compare(st, binVersion, dir, current)
	if !d.Stale {
		return nil
	}
	return &StaleNotice{
		Basis:   d.Basis,
		Current: d.Recorded,
		Target:  d.Running,
		Changed: d.Changed,
		Fix:     fixCommand(st, dir),
	}
}

// skipStaleCheck suppresses the nudge for builds/environments where it is
// noise: an unstamped dev build, CI, or an explicit opt-out.
//
// The dev-build rule outlives the move to content digests, which could now judge
// an unstamped build perfectly well. It stays because the person running one is
// usually the person editing the SKILL.md, and telling them their edits differ
// from their last install on every rebuild is noise aimed at the one reader who
// already knows. `doctor` still answers for a dev build, on the content basis.
//
// The CI and opt-out rules stay here rather than in Comparable because they are
// about whether to SPEAK, not about whether a comparison means anything — and
// `doctor`, which the user invoked on purpose, should answer regardless of them.
func skipStaleCheck(v string) bool {
	switch {
	case !Comparable(v):
		return true
	case os.Getenv("MEXC_CLI_NO_SKILLS_NOTIFIER") != "":
		return true
	case os.Getenv("CI") != "":
		return true
	}
	return false
}

// normalizeVersion strips a leading v/V and surrounding space so "v1.2.3" and
// "1.2.3" compare equal.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V")
}
