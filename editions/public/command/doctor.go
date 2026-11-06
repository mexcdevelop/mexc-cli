package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mexcdevelop/mexc-cli/editions/public/adapter"
	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/framework"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
)

// `doctor` is a health check, not a state dump: every entry carries a verdict
// and, when something is wrong, the command that fixes it. With a single
// deployment and a single identity, a dump of the effective configuration would
// be four lines of almost nothing, while the things that actually break here are
// all invisible — a credential that will not load, a profile the loader silently
// refuses, an endpoint quietly redirected by the environment.
//
// EVERY CHECK CALLS THE PRODUCTION PATH: credentials through
// credential.Provider.Resolve, the endpoint through hosts.Resolve, the active
// profile through profile.Load — the same functions a real request uses. A
// diagnostic that reimplements the logic it is diagnosing eventually disagrees
// with it, and then it reports health for a product that is broken (or the
// reverse), which is worse than having no diagnostic at all. The first version
// first version of the credential check got this wrong in exactly the way the
// rule predicts: it decided whether the active profile was usable by looking for
// it in a list of on-disk-but-refused entries, so a pointer to a DELETED profile
// — which a real request fails on immediately — was reported as healthy.
//
// This first version sends NOTHING of its own. Network checks (endpoint
// reachability via /api/v3/ping, and clock skew against /api/v3/time — a local
// clock outside the signature's recvWindow makes every signed request fail with
// an error that never mentions the clock) are deliberately deferred, along with
// the --offline switch that would then be needed to turn them off. A flag whose
// only setting is the current behaviour is noise, so it arrives with the checks
// it governs.
//
// "Sends nothing" is a property of THIS product's identities, not of the code
// below: checkCredentials calls each registered provider's Resolve, and the one
// provider here reads a file. An identity that resolved by talking to a server —
// refreshing a session, say — would make this command hit the network, and the
// promise above would need re-stating rather than assuming.
//
// It is NOT free of side effects, and the report is written so that it does not
// pretend to be: resolving credentials along the production path reads the
// active-profile pointer, which creates the state directory. See runDoctor.

// The three verdicts a check can carry.
const (
	statusPass = "pass"
	statusWarn = "warn"
	statusFail = "fail"
)

// checkResult is one diagnostic line. Hint is the remedy and is omitted when
// there is nothing to do, so a caller can treat "has a hint" as "actionable".
type checkResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// doctorReport is the whole run. OK is false when any check FAILED — warnings do
// not clear it but do not sink it either, which is the distinction the statuses
// exist to draw: a fail means this installation is broken, a warn means it is
// working but not in the state the reader probably expects (no credentials
// saved, writes blocked, endpoint redirected).
//
// Version is a field rather than a check because it is context, not a verdict:
// it can never fail, and a row that is always green trains the eye to skip rows.
// Both renderings then agree on what it is — a heading in the text view, a
// sibling of `checks` in the JSON.
type doctorReport struct {
	OK      bool          `json:"ok"`
	Version string        `json:"version"`
	Checks  []checkResult `json:"checks"`
}

// checkLabels maps a check's machine name to its heading in the text view. The
// machine name stays snake_case for the JSON; a reader gets "Read-only" rather
// than "read_only", which no amount of automatic title-casing produces.
//
// A missing entry would silently render as the bare machine name, so
// TestTextView_EveryCheckHasALabel asserts the map covers every check a real run
// emits — the map cannot fall behind the checks it labels.
var checkLabels = map[string]string{
	"state_dir":   "State dir",
	"endpoint":    "Endpoint",
	"credentials": "Credentials",
	"skills_docs": "Skill docs",
	"read_only":   "Read-only",
	// Emitted only on Windows, and only when npm installed a .ps1 shim. The label
	// is registered unconditionally because the map is a lookup, not a schedule —
	// TestTextView_EveryCheckHasALabel asserts coverage of what a run emits, and an
	// entry for a row this platform never emits costs nothing.
	"powershell_policy": "PowerShell policy",
	// Likewise Windows-only, and only when a code-integrity policy is enforcing or
	// evaluating.
	"app_control": "App control",
}

// statusMark is the text view's leading glyph per verdict.
var statusMark = map[string]string{
	statusPass: "✓",
	statusWarn: "!",
	statusFail: "✗",
}

// windowsChecks runs the platform checks that do not always apply, concurrently,
// and returns the ones that do — in a fixed order.
//
// The order is the declaration order below, not completion order, so two runs on
// the same machine produce the same report. A check that panics is dropped
// rather than allowed to take down the whole diagnostic: its result stays the
// zero value, which reads as "does not apply", and that is the same answer it
// gives when the state could not be established.
func windowsChecks() []checkResult {
	type outcome struct {
		result  checkResult
		applies bool
	}
	probes := []func() (checkResult, bool){
		checkPowerShellPolicy,
		checkAppControl,
	}
	outcomes := make([]outcome, len(probes))
	var wg sync.WaitGroup
	for i, probe := range probes {
		wg.Add(1)
		go func(i int, probe func() (checkResult, bool)) {
			defer wg.Done()
			defer func() { _ = recover() }()
			outcomes[i].result, outcomes[i].applies = probe()
		}(i, probe)
	}
	wg.Wait()

	out := make([]checkResult, 0, len(outcomes))
	for _, o := range outcomes {
		if o.applies {
			out = append(out, o.result)
		}
	}
	return out
}

func pass(name, msg string) checkResult {
	return checkResult{Name: name, Status: statusPass, Message: msg}
}

// passWith is a pass that still leaves the reader something to do. The pair
// exists because "nothing is wrong" and "nothing to do" are not the same
// statement, and conflating them cost the reader exactly where it matters most
// for this product: an agent-driven CLI whose docs are not installed is not
// broken, but it is not finished either. Keeping the verdict at pass (so `ok`
// stays true and the row is not dressed up as a problem) while still printing
// the command is the honest rendering of that state.
func passWith(name, msg, hint string) checkResult {
	return checkResult{Name: name, Status: statusPass, Message: msg, Hint: hint}
}

func warn(name, msg, hint string) checkResult {
	return checkResult{Name: name, Status: statusWarn, Message: msg, Hint: hint}
}

func failed(name, msg, hint string) checkResult {
	return checkResult{Name: name, Status: statusFail, Message: msg, Hint: hint}
}

// Doctor builds the `doctor` command. name is the edition's variant label, and
// is reported in the build line the same way `--version` prints it.
func Doctor(name string) framework.BuiltinCommand {
	return func(d framework.Deps) *cobra.Command {
		var asJSON bool
		c := &cobra.Command{
			Use:   "doctor",
			Short: "Check this installation: state, endpoint, profiles, credentials",
			Args:  cobra.NoArgs,
			// Doctor must run when the configuration is broken — that is precisely
			// when it is wanted — so it skips the root's config-resolving
			// PersistentPreRunE, exactly as `profile` and `help` do. Deps.Config()
			// still answers: it re-resolves on demand when the pre-run did not.
			//
			// The cost of skipping it is that the assembly-time validation it runs is
			// skipped too; checkHostTable below repeats the half of it that a
			// composition-root package can reach.
			PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
			RunE: func(cmd *cobra.Command, _ []string) error {
				// The root's persistent flags are merged into every subcommand's flag
				// set by cobra, so this invocation's --base-url and --profile are
				// readable here even though framework.Deps exposes neither. Reading
				// them matters: both change where THIS command's report should point,
				// and a report that ignores them describes a different invocation than
				// the one the caller just made.
				baseURL, _ := cmd.Flags().GetString("base-url")
				selected, _ := cmd.Flags().GetString("profile")
				return runDoctor(d, name, baseURL, selected, asJSON)
			},
		}
		c.Flags().BoolVar(&asJSON, "json", false,
			"Emit the report as JSON even at a terminal (the default when output is piped)")
		return c
	}
}

// textView reports whether to render for a human rather than as JSON.
//
// The default follows the destination, because the destination IS the audience:
// an agent or a script receives this command's stdout through a pipe, and a
// person reads it on a terminal. So the "stdout is data" rule keeps
// holding exactly where it matters — anything consuming the output
// programmatically gets the JSON envelope, unchanged — while the case the rule
// was never written about, someone typing `doctor` because something is wrong,
// gets a report they can read at a glance.
//
// --json forces the machine form for the remaining case: a person who wants the
// raw shape while sitting at a terminal. There is deliberately no --text; a flag
// to turn ON the thing that already happens when you would want it is a flag
// nobody needs to learn.
func textView(out io.Writer, forceJSON bool) bool {
	return !forceJSON && isTerminal(out)
}

// isTerminal is a var so a test can assert the --json branch. Without stubbing,
// every writer a test can build is already non-terminal, so "--json forces JSON"
// and "a buffer gets JSON" are the same assertion — deleting the --json check
// entirely kept the suite green until this was split out.
//
// Same shape as internal/cli's own isTerminal (explore.go), duplicated rather
// than shared because it is unexported there and an edition cannot import
// internal/: six lines of a stdlib predicate, not business logic that could
// drift into disagreeing with itself.
var isTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false // a buffer (tests) or a pipe: machine-bound
	}
	return term.IsTerminal(int(f.Fd()))
}

func runDoctor(d framework.Deps, name, baseURLOverride, selectedProfile string, forceJSON bool) error {
	cfg := d.Config()
	env := hosts.Env(cfg.Env)

	// Snapshot whether the state directory exists BEFORE running any check, and
	// resolve it with StatePath (which only computes the path) rather than StateDir
	// (which creates it).
	//
	// The ordering is load-bearing, not tidiness. A later check DOES create the
	// directory: checkCredentials resolves through the real provider, which reads
	// the active-profile pointer, which goes through config.StateDir and creates
	// the tree. That is the price of diagnosing along the production path and it is
	// worth paying — but it means asking afterwards would report a directory this
	// very command had just made. Asking first makes the report describe the
	// installation as the caller left it, and checkStateDir words the absent case
	// as a statement about that moment rather than about the filesystem now.
	statePath, stateErr := config.StatePath()
	stateExists := false
	if stateErr == nil {
		if fi, err := os.Stat(statePath); err == nil && fi.IsDir() {
			stateExists = true
		}
	}

	checks := []checkResult{
		checkStateDir(statePath, stateErr, stateExists),
		checkEndpoint(env, baseURLOverride),
		checkCredentials(cfg, env, stateExists, selectedProfile),
		checkSkillsDocs(stateExists),
		checkReadOnly(cfg),
	}
	// The two Windows checks are appended rather than placed in the list above
	// because they are the ones that do not always apply. Both are Windows-only,
	// and even there each is skipped unless the thing it is about is present: the
	// .ps1 shim npm installs, and a code-integrity policy that is switched on. A
	// row that would say "not applicable" on every other platform is worse than
	// no row.
	//
	// They exist for the same reason: to connect a system setting to a failure the
	// reader would otherwise take for a broken install. The execution policy stops
	// the shim before this binary is reached; app control can stop the binary
	// itself.
	//
	// And they run together, because each costs a PowerShell subprocess and
	// neither needs the other's answer. Measured on Windows: sequentially the
	// second one took a ~540ms report to ~1000ms, which is a poor trade for a
	// command whose whole purpose is to be the first thing anyone runs. Results
	// are collected by index and appended after the wait, so the report reads the
	// same on every run.
	checks = append(checks, windowsChecks()...)

	ok := true
	for _, c := range checks {
		if c.Status == statusFail {
			ok = false
		}
	}
	report := doctorReport{OK: ok, Version: framework.Version(name), Checks: checks}

	// The report goes to stdout even on failure: it IS the output the caller asked
	// for, and a reader — human or agent — needs the per-check detail far more
	// than the envelope. The envelope follows on stderr only to carry the exit
	// code, which is also why it is written in both views: the exit contract has
	// one code path. At a terminal it lands after the table, on the stream a human
	// can ignore or redirect.
	if textView(d.Out(), forceJSON) {
		if err := renderText(d.Out(), report); err != nil {
			d.WriteError(err)
			return err
		}
	} else if err := d.WriteJSON(report); err != nil {
		d.WriteError(err)
		return err
	}
	if ok {
		return nil
	}
	// Exit 2 (validation/failed-precondition) rather than a bespoke code: a broken
	// installation is a precondition failure like any other in this tree, and the
	// product has one exit-code contract (pkg/errs.ExitCode). Inventing a
	// doctor-only code would make callers special-case this command.
	err := errs.Validation(errs.SubtypeFailedPrecondition,
		"doctor found a problem with this installation").
		WithHint("see the failing checks on stdout; each names what is wrong and how to resolve it")
	d.WriteError(err)
	return err
}

// renderText writes the human view: one aligned line per check, the remedy
// indented beneath the checks that have one, and a closing count.
//
// Order is the order the checks ran, NOT failures first. A report whose rows
// move around cannot be compared against the last one at a glance, and the
// closing line already says whether anything is wrong — so stability is worth
// more here than putting the bad news on top.
func renderText(w io.Writer, r doctorReport) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "%s doctor — %s\n\n", cliname.Name(), r.Version)

	width := 0
	for _, c := range r.Checks {
		if n := len(labelFor(c.Name)); n > width {
			width = n
		}
	}
	warns, fails := 0, 0
	for _, c := range r.Checks {
		switch c.Status {
		case statusWarn:
			warns++
		case statusFail:
			fails++
		}
		fmt.Fprintf(b, "[%s] %-*s  %s\n", markFor(c.Status), width, labelFor(c.Name), c.Message)
		if c.Hint != "" {
			// Indented under its own row rather than on it: a hint is usually a
			// command, and wrapping one mid-line makes it uncopyable.
			fmt.Fprintf(b, "    %-*s  → %s\n", width, "", c.Hint)
		}
	}
	fmt.Fprintf(b, "\n%s\n", summaryLine(warns, fails))
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return errs.Internal("cannot write the report: %v", err).WithCause(err)
	}
	return nil
}

func labelFor(name string) string {
	if l, ok := checkLabels[name]; ok {
		return l
	}
	return name
}

func markFor(status string) string {
	if m, ok := statusMark[status]; ok {
		return m
	}
	return "?"
}

// summaryLine closes the report with the count that matters, phrased so the
// clean case reads as an answer rather than as two zeroes.
func summaryLine(warns, fails int) string {
	switch {
	case fails == 0 && warns == 0:
		return "✓ all checks passed"
	case fails == 0:
		return fmt.Sprintf("! %s, no failures", plural(warns, "warning"))
	default:
		return fmt.Sprintf("✗ %s, %s", plural(fails, "failure"), plural(warns, "warning"))
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// checkStateDir reports where on-disk state lives, and fails when its mode lets
// another local user WRITE there.
//
// Only writability is reported, and that is a deliberate narrowing. Everything
// secret under this directory carries its own mode: profiles/ is 0700, each
// profile 0600, the active pointer 0600, the skills record 0600. So a state
// directory others can merely read or traverse exposes a list of FILENAMES and
// nothing else — not a key, not even which profile is active. Warning about that
// is the cry-wolf a diagnostic cannot afford; it fired on a real installation
// where every secret was correctly protected.
//
// Writability is different in kind: another local user can rename or replace
// profiles/ wholesale, and the 0700 gate goes with it.
func checkStateDir(path string, err error, exists bool) checkResult {
	const nm = "state_dir"
	if err != nil {
		return failed(nm, "cannot resolve the state directory: "+messageOf(err),
			"this needs a resolvable home directory; check $HOME")
	}
	if !exists {
		// pass, not warn: an absent state directory is what a correct fresh install
		// looks like, and every writer creates it on demand. Flagging it made a
		// clean first run report three warnings for one situation — this line, "no
		// profiles saved", and "requires API credentials" — which trains the reader
		// to skim past warnings, the one thing a diagnostic cannot afford. The
		// profile and credential checks below say the actionable part.
		//
		// Worded as "was not present when this run started" rather than "does not
		// exist", because by the time the caller reads it, it does: resolving
		// credentials creates it (see runDoctor).
		return pass(nm, path+" (not present when this check ran)")
	}
	fi, serr := os.Stat(path)
	if serr != nil {
		return failed(nm, "cannot stat "+path+": "+serr.Error(), "check the path's permissions")
	}
	// Where mode bits are synthesised, say so instead of judging them. Passing with
	// "(mode 0777)" would be the worst of the three options available: it reports a
	// number that came from one read-only attribute as if it were the answer to
	// "can another user write here", and a reader who sees a mode printed has every
	// reason to believe it was checked. Failing would be worse still — a permanent
	// red finding on every install, for a condition the user cannot change.
	//
	// pass, not warn: nothing is known to be wrong, and a warning the user can do
	// nothing about is the cry-wolf this check's own comment above is written
	// against. What is lost is real and worth naming: on this platform the product
	// does not notice a state directory another local user can write.
	if !secfile.ModeBitsMeaningful {
		return pass(nm, path+" (permissions not checked: on this platform a file's mode "+
			"bits do not say who can read or write it)")
	}
	// The rationale belongs in the message, the command alone in the hint. Packing
	// both into the hint produced a line that wrapped twice in the text view, and
	// a wrapped command is one you cannot copy.
	mode := fi.Mode().Perm()
	if mode&0o022 != 0 {
		return failed(nm,
			fmt.Sprintf("%s is writable by others (mode %04o) — another local user can replace the profiles directory",
				path, mode),
			fmt.Sprintf("chmod 700 %s", path))
	}
	return pass(nm, fmt.Sprintf("%s (mode %04o)", path, mode))
}

// checkEndpoint reports the URL requests will actually go to AND which of the
// three layers supplied it, because the URL alone does not tell you what you
// need to know next.
//
// Precedence, mirroring internal/transport.Send:
//
//  1. --base-url        this invocation only; wins outright, skipping resolution
//  2. MEXC_OPENAPI_BASE_URL   sticky, lives in a shell profile, invisible
//  3. the built-in host table
//
// The source is named in ALL THREE cases, including the ordinary one. "It is the
// shipped host" is an answer; a bare URL leaves the reader to recognise it.
//
// A SHADOWED override is called out too. With --base-url given, the environment
// variable loses this time but is still set, so dropping the flag does not return
// you to the shipped endpoint — it hands you the variable's URL. Reporting only
// the winner made that invisible at exactly the moment the reader is deciding
// what to change.
func checkEndpoint(env hosts.Env, baseURLOverride string) checkResult {
	const nm = "endpoint"
	varName := hosts.OverrideVarName(adapter.KeyOpenAPI)

	if baseURLOverride != "" {
		msg := fmt.Sprintf("%s (source: --base-url, this invocation only)", baseURLOverride)
		hint := "drop --base-url to fall back to the layer below; credentials are sent to whatever it names"
		if shadowed := os.Getenv(varName); shadowed != "" {
			msg += fmt.Sprintf("; %s=%s is set and takes over if you drop the flag", varName, shadowed)
			hint = fmt.Sprintf("drop --base-url AND unset %s to reach the shipped endpoint", varName)
		}
		return warn(nm, msg, hint)
	}

	ep, err := hosts.Resolve(adapter.KeyOpenAPI, env)
	if err != nil {
		return failed(nm, "cannot resolve the openapi endpoint: "+messageOf(err),
			"this is a wiring problem in the build, not something local configuration can fix")
	}
	if ep.Overridden {
		return warn(nm, fmt.Sprintf("%s (source: %s, not the built-in host)", ep.URL, varName),
			fmt.Sprintf("unset %s to go back to the shipped endpoint; credentials are sent to whatever it names", varName))
	}
	return pass(nm, ep.URL+" (source: built-in host table)")
}

// checkCredentials answers the one question the credential side of this product
// has: can a signed request be made, and with what.
//
// It was TWO checks — a `profile_store` inventory and a `credentials` resolve —
// and they were the same answer in different words. They diverge in exactly one
// state, a profile that loads but holds no key, and there the split was actively
// worse: one row said "1 saved (broken); active: broken" and the next said "no
// API credentials available", leaving the reader to join them. One row says it.
//
// EVERY VERDICT COMES FROM THE PRODUCTION PATH. profile.Load decides whether the
// selected profile is usable; the identity provider's Resolve decides whether
// credentials come out of it. Nothing here re-derives either. The one place this
// reads the profile itself is to enrich a message the provider already decided —
// naming which field is empty, after Resolve has said no — which reports an
// observable fact rather than forming a second opinion.
//
// Refused entries are surfaced because nothing else does: profile.List drops
// them silently so `profile list` never advertises an entry `use` would reject,
// which is right there and is why a profile someone definitely saved can vanish
// with no message at all.
func checkCredentials(cfg config.Config, env hosts.Env, stateExists bool, selectedProfile string) checkResult {
	const nm = "credentials"
	// Names the terminal explicitly: an agent reading this hint cannot run the
	// command itself, and its correct move is to relay it to the user.
	setHint := fmt.Sprintf("ask the user to run `%s profile set <name>` in their own terminal", cliname.Name())
	useHint := fmt.Sprintf("%s profile use <name>", cliname.Name())

	if !stateExists {
		return warn(nm, "no profiles saved", setHint)
	}
	usable, rejected, err := profile.Audit()
	if err != nil {
		return failed(nm, "cannot read the profile store: "+messageOf(err),
			"check the permissions on the profiles directory")
	}
	active, aerr := profile.Active()
	if aerr != nil {
		return failed(nm, "cannot read the active profile pointer: "+messageOf(aerr),
			"check the permissions on the state directory")
	}

	// refusedNote trails whatever verdict follows: a hidden entry is worth saying
	// in every case, but it is never the headline.
	//
	// Except when there is no key: without one NOTHING on disk can be read, every
	// profile reports its own decryption failure, and the run stays green because
	// each is merely "refused" — eight copies of a symptom with the cause named
	// nowhere. State the condition once, as the failure it is.
	if len(rejected) > 0 && len(usable) == 0 && keyBackend() == secretstore.BackendNone {
		// The hint used to open "if this machine has no OS keyring" and close with
		// "save the profiles again". Both were wrong on the path people hit: the
		// machine usually HAS a keyring that is merely out of reach, so the condition
		// reads as false and an agent skips the sentence — and re-saving recovers
		// nothing, it seals a second key and leaves the first where it was.
		return failed(nm,
			fmt.Sprintf("%s stored, but no key on this machine can decrypt them",
				plural(len(rejected), "profile")),
			fmt.Sprintf("if the OS keyring is present but out of reach — a sandbox, CI, a locked "+
				"keychain — the key is probably still in it: have the user run `%s profile "+
				"keychain-downgrade --yes` in a terminal where it answers, then retry. Do NOT save "+
				"these profiles again first. Only if the keyring answers and holds no key were they "+
				"copied from another machine, and then they cannot be recovered.", cliname.Name()))
	}

	refusedNote := ""
	if len(rejected) > 0 {
		var parts []string
		for _, r := range rejected {
			parts = append(parts, fmt.Sprintf("%s (%s)", r.Name, r.Reason))
		}
		refusedNote = fmt.Sprintf(" — %s hidden from `%s profile list`: %s",
			plural(len(rejected), "profile"), cliname.Name(), strings.Join(parts, "; "))
	}

	// --profile names the credential set for this invocation and outranks the
	// active pointer, exactly as the identity provider reads it.
	inEffect, explicit := active, false
	if selectedProfile != "" {
		inEffect, explicit = selectedProfile, true
	}
	if inEffect == "" {
		if len(usable) == 0 {
			// "None saved" only when the store really is empty. With entries present
			// but all refused, it contradicts the refusals listed in the same breath.
			if len(rejected) > 0 {
				return warn(nm,
					fmt.Sprintf("%s stored, none of them usable%s",
						plural(len(rejected), "profile"), refusedNote),
					"fix them as shown, or save one again")
			}
			return warn(nm, "no profiles saved", setHint)
		}
		return warn(nm,
			fmt.Sprintf("%s saved (%s) but none is active%s",
				plural(len(usable), "profile"), strings.Join(usable, ", "), refusedNote),
			useHint)
	}

	// An unusable selection is the confusing case: it looks configured and behaves
	// as if nothing is. Loading it is the only way to catch a pointer to a deleted
	// profile — nothing is left on disk for the audit to refuse.
	p, lerr := profile.Load(inEffect)
	if lerr != nil {
		return failed(nm,
			fmt.Sprintf("%s %q cannot be loaded: %s%s",
				selectionWord(explicit), inEffect, messageOf(lerr), refusedNote),
			hintOr(lerr, useHint))
	}

	// The identity provider has the final say on whether a request can be signed.
	if res := resolveIdentities(cfg, env); res != "" {
		detail := ""
		if p.APIKey == "" || p.SecretKey == "" {
			detail = " (it has no api-key/api-secret)"
		}
		msg := fmt.Sprintf("%s %q loads but no credentials could be resolved from it%s%s",
			selectionWord(explicit), inEffect, detail, refusedNote)
		if explicit {
			// The caller named this credential set; "you have none" understates it,
			// and every other command treats a named unloadable profile as an error.
			return failed(nm, msg, setHint)
		}
		return warn(nm, msg, setHint)
	}

	status := statusPass
	if refusedNote != "" {
		status = statusWarn
	}
	msg := fmt.Sprintf("loaded from %s %q (%s saved)%s",
		selectionWord(explicit), inEffect, plural(len(usable), "profile"), refusedNote)
	if status == statusWarn {
		return warn(nm, msg, "fix the hidden entry as shown above, or delete the file")
	}
	return withKeyBackend(pass(nm, msg))
}

// withKeyBackend annotates a healthy credentials line with how the stored secret
// is protected at rest, and lowers the verdict when that protection is weaker
// than the default.
//
// Reported on the healthy path specifically. The protection level is invisible
// otherwise — nothing fails, nothing warns, and a machine whose key sits in a
// local file looks exactly like one holding it in the OS keyring. That is the
// state worth naming, because the point of encrypting at rest is that a copied
// file is useless without the key, and a key file travels with the copy.
// keyBackend is a var so a test can drive every branch below. It has to be: a
// test isolates HOME, which is precisely what makes the OS keyring unreachable,
// so the real function can only ever answer "file" under test — and every
// unrelated credentials assertion would then be measuring the test harness.
var keyBackend = profile.KeyBackend

func withKeyBackend(c checkResult) checkResult {
	switch keyBackend() {
	case secretstore.BackendKeychain:
		return pass(c.Name, c.Message+" (encrypted; key in the OS keyring)")
	case secretstore.BackendFile:
		// On a platform with no OS keyring a file key is not a downgrade, it is the
		// only key store there is. Warning about it would put a permanent finding on
		// every Linux and Windows install for a state the user did not cause and
		// cannot change — and an agent reading `doctor` would keep trying to "fix" it.
		// The co-location caveat still belongs in the message, because it is the one
		// property this platform genuinely does not deliver (see the package doc of
		// pkg/secretstore).
		if !secretstore.KeyringAvailable() {
			// The co-location caveat is TRUE only where the file is the key. Where the OS
			// wraps it to this account, repeating the caveat would understate the
			// protection and misdescribe the file — and a reader who believes the copy is
			// dangerous handles it as though the credential travelled with it.
			if secretstore.KeyFileWrappedByOS() {
				return pass(c.Name, c.Message+
					" (encrypted; the key is in a local file, which is the only key store this "+
					"platform has, and the OS wraps it to this user account — a copy of it "+
					"opens nothing under any other account or on any other machine)")
			}
			return pass(c.Name, c.Message+
				" (encrypted; the key is in a local file, which is the only key store this "+
				"platform has — a backup or synced folder that copies the profile copies the key too)")
		}
		// The hint used to end "remove the key file to go back to the OS keyring".
		// That instruction destroys credentials. On a machine that downgraded
		// automatically — Seal's fallback, where no keyring was usable — the keyring
		// holds NO key, so the file is the only one there is: deleting it makes every
		// stored profile permanently unreadable. Measured. And `profile show` masks
		// the secret, so the user cannot read the credentials back to re-save them.
		return warn(c.Name,
			c.Message+" (encrypted, but the key is in a local file — downgraded)",
			"a backup or synced folder that copies the profile copies the key too. Do NOT "+
				"delete the key file: it may be the only copy, and stored profiles would "+
				"become unreadable with no way to recover them")
	default:
		// No key anywhere: nothing has been sealed on this machine yet. Not a
		// finding — saving a profile is what creates the key.
		return c
	}
}

// selectionWord names how the profile in effect was chosen, so the reader knows
// which knob to reach for — the flag they just typed or the stored pointer.
func selectionWord(explicit bool) string {
	if explicit {
		return "--profile"
	}
	return "the active profile"
}

// resolveIdentities runs every registered identity through its own Resolve and
// returns a joined description of the ones that failed, or "" when all resolved.
// It enumerates the registry rather than naming this product's single identity,
// so one added later is covered without editing this file.
func resolveIdentities(cfg config.Config, env hosts.Env) string {
	realms := credential.Realms()
	sort.Slice(realms, func(i, j int) bool { return realms[i] < realms[j] })
	var bad []string
	for _, r := range realms {
		p, ok := credential.Get(r)
		if !ok {
			bad = append(bad, fmt.Sprintf("%s: not registered", r))
			continue
		}
		if _, err := p.Resolve(cfg, env); err != nil {
			bad = append(bad, string(r))
		}
	}
	return strings.Join(bad, ", ")
}

// checkSkillsDocs reports whether the SKILL.md docs this binary installed are
// still the ones it would install today. It reads the record `skills install`
// leaves behind and resolves drift through skillstate.Compare — content digests
// where both sides have them, build versions otherwise — so the rule for what
// counts as drift lives in one place.
//
// It is NOT the same output as the startup nudge, and deliberately so. That nudge
// goes silent under CI or MEXC_CLI_NO_SKILLS_NOTIFIER (skillstate.skipStaleCheck)
// and is suppressed on any command path containing "skills" — and, because this
// command overrides the pre-run, on `doctor` itself. Those are rules about when
// to SPEAK unprompted; a diagnostic the user invoked on purpose answers anyway.
// So under CI the nudge is quiet and this line still warns.
//
// Three states are deliberately NOT warnings:
//   - nothing installed. A user who never installed the docs is not out of sync
//     with anything, so this is not a defect and must not sink `ok`. It does
//     carry the install command as a hint, though, and that is not the same as
//     nagging: for a CLI whose caller is an AI agent, the SKILL.md files are how
//     the agent learns these commands exist, so "not installed" is a step not
//     yet taken rather than a neutral resting state. A diagnostic the user ran
//     on purpose is the right place to say so once; the startup nudge, which
//     speaks uninvited, still stays silent here.
//   - an unstamped dev build whose record carries no digests, where every build
//     reports the same "dev" and a version comparison is meaningless either way.
//     Said out loud rather than hidden: doctor was invoked on purpose, so "cannot
//     be judged" is the honest answer. With digests on both sides an unstamped
//     build IS judged, because content does not depend on being stamped.
//   - an unreadable record, which the reader can do nothing about and which
//     already degrades to "no nudge" everywhere else.
//
// One nuance the message no longer spells out, because it is implementation
// detail the reader cannot act on: this reads THIS CLI's own install record. A
// user who ran `npx skills add` by hand has files on disk that leave no record
// here, and is told the docs are not installed. Re-running `skills install` is
// the correct move in that case anyway, which is what the hint says.
func checkSkillsDocs(stateExists bool) checkResult {
	const nm = "skills_docs"
	fix := fmt.Sprintf("run: %s skills install", cliname.Name())
	notInstalled := passWith(nm,
		"not installed; an agent driving this CLI has no docs for it", fix)
	if !stateExists {
		return notInstalled
	}
	st, ok, err := skillstate.Read()
	if err != nil {
		return warn(nm, "cannot read the skills install record: "+messageOf(err), "")
	}
	if !ok || len(st.Names()) == 0 {
		return notInstalled
	}
	// The working directory selects which installs apply: a project install is only
	// this directory's business. A failed Getwd yields "", matching no project root,
	// which reports on the global installs alone rather than on a guess.
	dir, _ := os.Getwd()
	// A diagnostic run on purpose can afford the whole list, so the hint names the
	// exact command per destination — following a bare `skills install` would reset
	// scope, agents, and subset to their defaults.
	fix = skillsFix(st, dir)
	count := skillsCount(st, dir)
	// Content first, build version as the fallback — skillstate.Compare owns that
	// ordering so this report and the startup nudge cannot disagree.
	current, _ := skillstate.CurrentDigests()
	d := skillstate.Compare(st, buildinfo.Version, dir, current)
	where := skillsWhere(st, dir)
	switch {
	case d.Stale && d.Basis == skillstate.BasisContent:
		return warn(nm,
			fmt.Sprintf("installed docs differ from this build: %s (%s)", skillstate.NameList(d.Changed), where), fix)
	case d.Stale:
		return warn(nm,
			fmt.Sprintf("installed from %s, but this binary is %s (%s)", st.Version, buildinfo.Version, where), fix)
	case d.Basis == skillstate.BasisContent:
		// Reported even for an unstamped build: a digest comparison is meaningful
		// where a version comparison is not, which is the point of having one.
		return pass(nm, fmt.Sprintf("%d installed, content matches this build (%s)", count, where))
	case d.Basis == skillstate.BasisVersion:
		return pass(nm, fmt.Sprintf("%d installed, in sync with %s (%s)", count, st.Version, where))
	default:
		return pass(nm, fmt.Sprintf("installed from %s; this build is unstamped (%s) and the record carries no content digests, so drift cannot be judged",
			st.Version, buildinfo.Version))
	}
}

// skillsCount is how many distinct skills apply here. Counted over the same
// installs the verdict used, so the number and the verdict describe one set —
// counting every destination would include a project the reader is not in.
func skillsCount(st *skillstate.State, dir string) int {
	seen := map[string]bool{}
	for _, in := range st.Relevant(dir) {
		for name := range in.Skills {
			seen[name] = true
		}
	}
	return len(seen)
}

// skillsWhere describes the destinations that apply here, so the reader can tell
// a global install from one that belongs to some other project directory. Without
// it "in sync" is ambiguous: it could describe a copy that is nowhere near the
// directory the user is standing in.
func skillsWhere(st *skillstate.State, dir string) string {
	var parts []string
	for _, in := range st.Relevant(dir) {
		switch in.Scope {
		case skillstate.ScopeUnknown:
			parts = append(parts, "destination not recorded")
		case skillstate.ScopeProject:
			parts = append(parts, fmt.Sprintf("project %s → %s", in.Root, strings.Join(in.Agents, ", ")))
		default:
			parts = append(parts, "global → "+strings.Join(in.Agents, ", "))
		}
	}
	if len(parts) == 0 {
		return "no install applies in this directory"
	}
	return strings.Join(parts, "; ")
}

// skillsFix names the exact command per applicable destination. Several
// destinations mean several commands, and the reader needs all of them: refreshing
// only one leaves the others behind, which is the failure the bare command caused.
func skillsFix(st *skillstate.State, dir string) string {
	var cmds []string
	for _, in := range st.Relevant(dir) {
		if in.Scope == skillstate.ScopeUnknown {
			continue // the record does not say where it went; do not invent one
		}
		cmds = append(cmds, in.InstallCommand(cliname.Name()))
	}
	if len(cmds) == 0 {
		return fmt.Sprintf("run: %s skills install", cliname.Name())
	}
	return "run: " + strings.Join(cmds, " && ")
}

// checkReadOnly surfaces the safety mode. Read-only on is reported as a warning
// even though it is often deliberate: it can be set by MEXC_READ_ONLY exported
// in a shell and then forgotten, and "my order will not place" with no other
// explanation is exactly the question this answers.
func checkReadOnly(cfg config.Config) checkResult {
	const nm = "read_only"
	if cfg.ReadOnly {
		return warn(nm, "read-only is in effect; every write operation is rejected",
			"drop --read-only, and unset MEXC_READ_ONLY if it is exported")
	}
	return pass(nm, "off; write operations are allowed")
}

// messageOf renders an error for a check line: the structured Message when it is
// one of ours, the raw text otherwise. Never err.Error(), which prefixes the
// envelope's own "[type/subtype]" tag — machine routing that reads as noise in
// the middle of a sentence a human is meant to act on.
func messageOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

// hintOf lifts a structured error's own remedy so the check reuses it instead of
// restating it — the failing layer knows what would fix it, and a second wording
// here would drift from the one every other command shows.
func hintOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Hint
	}
	return ""
}

// hintOr prefers the error's own remedy and falls back to one written for this
// check, for the errors that carry no hint of their own.
func hintOr(err error, fallback string) string {
	if h := hintOf(err); h != "" {
		return h
	}
	return fallback
}
