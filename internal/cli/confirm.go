package cli

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/console"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// Confirmation. An operation is confirmed with the user before it executes when
// either of two things is true: it is a write (see the write-confirmation
// design), or the identity it authenticates with grades it as needing a person
// (see gradeFor). The channel is chosen by what
// the environment offers, best first:
//   - --yes on the invocation          → treated as an explicit confirmation
//   - our own stdio is a terminal      → interactive [Confirm]/[Cancel] prompt on it
//   - anything else (agent shell-out)  → refuse with confirmation_required, so
//     the agent relays to the user and retries with --yes
//
// ONE QUESTION DECIDES IT, and it is about the caller's own streams rather than
// about the machine. This gate used to open the controlling terminal instead and
// read "it opened" as "somebody is there". An agent's shell-out inherits the
// person's terminal while holding the child's streams as pipes, so the device
// opened, every check console.Open made passed, and the prompt rendered where the
// agent could not see it and nobody was typing. That hung until something killed
// it: no refusal, no exit code, no output to route on. See
// console.StdioIsTerminal.
//
// THE PROMPT NOW RUNS ON STDIO, not on a separately opened console device, and
// the gate above is what makes that correct: by the time the prompt is reached,
// both streams are known to be a terminal, so there is nothing a device handle
// would add. Three things follow, and the third is why it changed:
//
//   - Nothing is opened and nothing is closed, so the device's own failure modes
//     stop reaching a caller who plainly has a terminal — including the deeply
//     nested working directory that rewrites CONIN$ into a path that does not
//     open (pkg/console/console_windows.go records it).
//   - The prompt cannot land in machine-readable output, because a redirected
//     stdout is exactly what the gate refuses.
//   - On Windows it is the difference between a prompt that answers and one that
//     hangs on the way out. bubbletea reaches for the native console reader only
//     when its input IS os.Stdin (inputreader_windows.go); handed any other
//     handle it falls back to a reader whose Cancel is documented to always
//     return false, so after the keypress its read stays parked on CONIN$ and the
//     command waits for one more keystroke that has nothing to do with the
//     answer. Unix never had this: its cancelreader cancels any descriptor.
//
// The tier only changes the prompt's wording/strictness (Heavy warns it is
// irreversible and defaults the cursor to Cancel), never whether a write may run.
// This file carries no build tag: writes exist in the public binary too, so the
// prompt must compile in both variants (unlike an edition's login tty helpers).
//
// `profile set` still opens the device (editions/public/command/tty.go) and must
// keep doing so. It reads a SECRET, and the argument for keeping that off the
// standard streams is a product decision documented there; a keypress answering
// [Confirm]/[Cancel] is not the same thing.

// Indirections over the two steps, so a test can drive every outcome —
// unattended, asked and confirmed, asked and cancelled, could not ask — without
// a pty. Each reports a different stage/channel pair, which is the audit's
// signal.
var (
	stdioIsTerminalFn = console.StdioIsTerminal
	runConfirmTUIFn   = runConfirmTUI
)

// confirmForCLI decides whether a CLI operation may proceed — a write, or a read
// its identity graded as needing a person. It returns nil to run,
// or a typed error to abort: confirmation_required when no interactive channel is
// reachable (agent must relay to the user, then retry with --yes), or a
// validation error when the user cancelled at the prompt. A read (TierNone) and
// an explicit --yes both proceed without a prompt.
//
// It records the outcomes that END the command, and returns the channel for the
// one that does not: on an accepted judgement the caller records both stages once
// the write has been issued, so nothing about the write waits on the trail (see
// confirmaudit.go). The channel is empty when there is nothing to confirm or
// nothing was confirmed.
func confirmForCLI(f *Factory, base confirmaudit.Event, op api.Operation, values map[string]string, data string, assumeYes bool, grade credential.ConfirmGrade) (confirmaudit.Channel, error) {
	// Two independent reasons to ask, and either is enough: the write semantics
	// the framework derives, and a grade the identity supplies for this
	// operation's permission requirement. A read reaches here only for the
	// second — before the grade existed this returned on every read.
	if op.RiskTier() == api.TierNone && !grade.Confirm {
		return "", nil
	}
	if assumeYes {
		// No synthesised REMIND: a CONFIRMED with no REMIND under the same
		// ConfirmKey is exactly the self-approving caller the audit looks for, and
		// inventing the missing half would erase that signal. The CONFIRMED itself
		// is the caller's to record, after the write goes out.
		return confirmaudit.ChannelAssumeYes, nil
	}
	display := gradedDisplay(op, values, data, grade)
	// The one question, asked before anything is rendered: an unattended caller
	// must reach the refusal below, never a prompt on a terminal it does not own.
	// The caller is being told to relay and come back with --yes, so this reminder
	// is the left half of a pair completed by another process.
	if !stdioIsTerminalFn() {
		sendRecords(f, base, record{confirmaudit.StageRemind, confirmaudit.ChannelRefusal})
		return "", needsConfirmation(op, display)
	}

	confirmed, ran := runConfirmTUIFn(display, confirmNoun(op), op.RiskTier())
	if !ran {
		// Nothing was rendered and nothing answered, and the caller gets the same
		// refusal as a caller with no terminal at all — so it is recorded the same
		// way, as the half of a pair a later --yes completes.
		sendRecords(f, base, record{confirmaudit.StageRemind, confirmaudit.ChannelRefusal})
		return "", needsConfirmation(op, display) // fail closed: could not ask
	}
	if !confirmed {
		// Inline, because this judgement ends the command: no write follows, so
		// there is nothing left for the trail to delay. Both facts are known, and
		// they go in the order they happened, under one budget.
		sendRecords(f, base,
			record{confirmaudit.StageRemind, confirmaudit.ChannelTTY},
			record{confirmaudit.StageCancelled, confirmaudit.ChannelTTY})
		return "", errs.Validation(errs.SubtypeFailedPrecondition,
			"%s %q was cancelled at the confirmation prompt", confirmNoun(op), op.Name).
			WithHint("the operation was not executed")
	}
	// The reminder is NOT sent here even though the prompt has been answered: the
	// user is now waiting on the write, and a send placed here delays it by the
	// report timeout — a dead audit endpoint would price that into every confirmed
	// order. The caller sends it with the confirmation, after the fact
	// (reportConfirmed).
	return confirmaudit.ChannelTTY, nil
}

// gradeFor asks the operation's identity whether it demands a person's
// confirmation beyond the write semantics. Both faces call it, so the question
// is asked the same way on each.
//
// An operation declaring no permission requirement is not asked about at all.
// There is nothing to grade, every public operation is in that case, and
// skipping it keeps the common path from resolving the env — which a caller
// that never reaches the gate has no reason to have configured.
func gradeFor(f *Factory, identity api.Identity, op api.Operation) credential.ConfirmGrade {
	// Checked before anything reads f: an operation with no permission
	// requirement is the common case, and it must not depend on a Factory field
	// being populated.
	if !gradeable(op) {
		return credential.ConfirmGrade{}
	}
	// A dry-run sends nothing, so there is nothing to confirm. Asking anyway
	// would make a PREVIEW read the credential store — an identity answers this
	// from its own stored session — which is exactly what the dry-run exemption
	// for credentials exists to prevent. On a build whose session key lives in an
	// OS keyring that is a keychain read, so a command documented to only print a
	// request would reach for a secret.
	if f.DryRun() {
		return credential.ConfirmGrade{}
	}
	// config.Config.Env is a plain string on purpose, to avoid a config -> hosts
	// import cycle; this is the same cast the transport makes. It must be the
	// RESOLVED env, so the grade comes from the deployment that will serve the
	// request.
	return credential.GradeConfirm(credential.Realm(identity),
		hosts.Env(f.Config().Env), op.Permission, op.Access)
}

// gradeable reports whether an operation can be graded AT ALL: it declares a
// permission requirement, so there is something for an identity to have an
// opinion about. This is the static half of gradeFor's question — answerable
// without a Factory, an env, or a provider.
//
// It is separate because one caller can only ask the static half. An MCP tool's
// input schema is built once, at registration, long before any invocation has an
// env to grade against; so the parameter a refused call is told to come back with
// must be published for every operation that COULD be graded, not for the ones
// that turn out to be. Publishing it on the graded ones only is not an option
// available at that point, and withholding it made the refusal instruct an action
// the schema did not offer.
func gradeable(op api.Operation) bool { return op.Permission != nil }

// gradedDisplay renders the pending request with the grade's reason above it,
// which is what merges two reasons into ONE prompt rather than two: every
// surface draws from this one string.
//
// One function for both faces on purpose. The reason above the request is a
// FORMAT, and a format each face applies for itself is how the two copies of
// "this write needs the user's explicit confirmation" drifted apart — one of
// them kept saying "write" for a read long after the other stopped.
func gradedDisplay(op api.Operation, values map[string]string, data string, grade credential.ConfirmGrade) string {
	display := confirmDisplay(op, values, data)
	if grade.Reason == "" {
		return display
	}
	// Above the request, so the reason a person is being asked reads before what
	// they are being asked about.
	return grade.Reason + "\n\n" + display
}

// confirmNoun is the word the prompts use for what is pending. A write is a
// write; an operation asked about only because the identity graded it is a read.
// One helper, so the TUI header, the elicitation message and this refusal cannot
// drift apart — a read told it is a write reads as a bug in the tool.
func confirmNoun(op api.Operation) string {
	if op.IsWrite() {
		return "write"
	}
	return "read"
}

// needsConfirmation is the refusal returned when an operation the gate stopped
// cannot be confirmed interactively. It names what is pending with confirmNoun,
// because a read reaches here too. The message carries the operation summary and its parameters
// (display) so the agent relays the EXACT pending request to the user; the hint
// carries the "don't self-approve" guidance (the standing form lives in the
// mexc-common skill).
func needsConfirmation(op api.Operation, display string) error {
	if op.RiskTier() == api.TierHeavy {
		display = "IRREVERSIBLE — this operation cannot be undone.\n" + display
	}
	return errs.ConfirmationRequired("this %s needs the user's explicit confirmation before it runs:\n%s", confirmNoun(op), display).
		WithHint("show the user the operation and its parameters above, then re-run with --yes ONLY after the user explicitly confirms — do not add --yes on your own")
}

// confirmDisplay renders the pending request for a human: its summary plus the
// non-empty parameters (in declaration order) and the JSON body, if any. It is
// the single source the tty prompt, the MCP elicitation message, and the
// confirmation_required payload all draw from.
//
// The masking comes from maskedPairs, shared with the audit record, so a
// parameter newly marked Sensitive cannot be hidden here and left readable there.
func confirmDisplay(op api.Operation, values map[string]string, data string) string {
	var b strings.Builder
	title := op.Summary
	if title == "" {
		title = op.Name
	}
	b.WriteString(title)
	for _, p := range maskedPairs(op, values) {
		fmt.Fprintf(&b, "\n  %-16s %s", p.name, p.value)
	}
	if op.Body != nil {
		if d := strings.TrimSpace(data); d != "" {
			fmt.Fprintf(&b, "\n  %-16s %s", "body", op.MaskSensitiveBody(d))
		}
	}
	return b.String()
}

// confirmModel is a small bubbletea [Confirm]/[Cancel] prompt, rendered inline on
// the tty. Heavy writes warn that the action is irreversible and default the
// cursor to Cancel, so a fat-finger Enter does not execute an irreversible op.
type confirmModel struct {
	display   string
	noun      string
	heavy     bool
	cursor    int // 0 = Confirm, 1 = Cancel
	done      bool
	confirmed bool
}

func newConfirmModel(display, noun string, tier api.RiskTier) confirmModel {
	m := confirmModel{display: display, noun: noun, heavy: tier == api.TierHeavy}
	if m.heavy {
		m.cursor = 1 // Heavy defaults to Cancel — the user must actively choose Confirm
	}
	return m
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.done, m.confirmed = true, false
		return m, tea.Quit
	case tea.KeyLeft, tea.KeyUp:
		m.cursor = 0
	case tea.KeyRight, tea.KeyDown:
		m.cursor = 1
	case tea.KeyEnter:
		m.done, m.confirmed = true, m.cursor == 0
		return m, tea.Quit
	case tea.KeyRunes:
		switch strings.ToLower(string(key.Runes)) {
		case "y":
			m.done, m.confirmed = true, true
			return m, tea.Quit
		case "n", "q":
			m.done, m.confirmed = true, false
			return m, tea.Quit
		case "h": // vim-left
			m.cursor = 0
		case "l": // vim-right
			m.cursor = 1
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	if m.done {
		return "" // clear the inline render once resolved
	}
	var b strings.Builder
	if m.heavy {
		b.WriteString("! IRREVERSIBLE — this operation cannot be undone.\n")
	}
	b.WriteString("Confirm ")
	b.WriteString(m.noun)
	b.WriteString(":\n")
	b.WriteString(m.display)
	b.WriteString("\n\n")
	confirm, cancel := "  Confirm  ", "  Cancel  "
	if m.cursor == 0 {
		confirm = "> Confirm  "
	} else {
		cancel = "> Cancel  "
	}
	b.WriteString(confirm + cancel)
	b.WriteString("   (arrows/hl: move, enter: select, y: confirm, n/esc: cancel)\n")
	return b.String()
}

// runConfirmTUI runs the [Confirm]/[Cancel] prompt on this process's own stdio.
// ran is false when the TUI could not start, so the caller fails closed (treats
// it as "could not confirm" and returns confirmation_required).
//
// Only reached once the gate has established that both streams are a terminal,
// which is what makes stdio the right place for it — see the file's header.
//
// The two streams are named EXPLICITLY rather than left to bubbletea's default.
// Its default branch re-derives them, and if stdin turns out not to be a
// terminal it silently opens the console device instead (tea.go, the
// defaultInput case) — the exact substitution this gate exists to prevent, and
// on Windows the exact one that hangs. Passing them by hand takes that branch
// out of reach, so a future caller reaching this with piped stdio gets a broken
// prompt rather than a stuck process. Neither is closed here: they outlive the
// prompt and belong to the process.
func runConfirmTUI(display, noun string, tier api.RiskTier) (confirmed, ran bool) {
	final, err := tea.NewProgram(newConfirmModel(display, noun, tier),
		tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	if err != nil {
		return false, false
	}
	m, ok := final.(confirmModel)
	if !ok {
		return false, false
	}
	return m.confirmed, true
}
