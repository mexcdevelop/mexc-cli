package command

import (
	"strings"

	"golang.org/x/term"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/console"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// Reading a credential from the controlling terminal, and from nowhere else.
//
// WHY. This product is driven by an AI agent, and an agent's shell-out is
// recorded: the command line it ran goes into a transcript that is typically
// sent to a model provider and retained. An api secret passed as an argument
// therefore does not merely land in `ps` output and shell history — it leaves
// the machine, before it ever reaches a file, where no amount of at-rest
// protection can touch it. `profile set` accordingly takes no credential flags
// at all; the prompt below is the only input channel, so no invocation of that
// command can carry a secret.
//
// WHAT THIS ACHIEVES, STATED EXACTLY. The CLI never ACCEPTS a secret from a
// recordable channel — not argv, not stdin, not the environment, not a file.
// That closes the accidental path, which is the realistic one: an agent handed
// a secret and asked to "set up my credentials" runs the documented command,
// and the documented command cannot take it.
//
// WHAT THIS IS NOT — and the first version of this file got this wrong, in
// print. It is NOT a barrier against a process that is determined to store a
// secret it already holds:
//
//   - A terminal can be manufactured. `script -q /dev/null <cmd>`, python's
//     pty.fork, expect and unbuffer all call setsid + TIOCSCTTY, after which
//     /dev/tty opens and term.IsTerminal is true. Nothing distinguishes such a
//     pty from a human's terminal, and nothing can.
//   - A process running as the user can drive the prompt through that pty, or
//     skip this command entirely and write ~/.mexc/profiles/<name> itself.
//
// Those are Unix mechanisms, named because they are the ones measured here. The
// CEILING is not Unix-specific and must not be read as such: Windows allocates
// pseudo-consoles too, and a process that attaches one gets a console this code
// cannot tell from a person's. Nothing below is stronger on one platform.
//
// So "has a terminal" is not a proxy for "a human is present", and this file
// must not be described as if it were. What survives contact with a pty is the
// narrower claim above, plus a refusal whose hint tells an agent to hand the
// job back to the user.
//
// WHAT THAT REFUSAL RESTS ON — CORRECTED, because the first version of this
// paragraph named two conditions and relied on them as one. It read: "stdin and
// stdout are pipes AND the controlling terminal cannot be opened at all (ENXIO
// on Unix; on Windows a process with no console fails the same way, one layer
// down in CreateFile)". The first half holds. The second does not, and only the
// second was ever checked.
//
// An agent shelling out from a terminal session hands its child pipes, not a new
// session. The child keeps the controlling terminal, so /dev/tty resolves and
// IsTerminal is true — CONIN$/CONOUT$ likewise on Windows, which
// pkg/console/console_windows.go states outright ("Both open even when the
// standard streams are redirected"). The device fails only where there is no
// controlling terminal AT ALL: a service, a scheduled task, CI with no pty.
// That is not the runtime this product targets; the agent-in-a-terminal one is,
// and there the device opens.
//
// Measured under an agent host on Windows: stdin and stdout report IsTerminal
// false while CONIN$ and CONOUT$ both open with IsTerminal true. Before the
// stdio gate below, this command therefore prompted on a console the agent could
// neither see nor type into, and hung until the host's timeout killed it.
//
// The refusal now rests on the FIRST half — this process's own streams — which
// is the half that describes the caller rather than the machine.
//
// The DEVICE differs by platform and the claim does not. pkg/console holds that
// difference — one device on Unix, two on Windows — precisely so this file can
// keep saying "the controlling terminal, and from nowhere else" without caring
// which.

// The controlling terminal's two sides; package vars so a test can redirect them
// to a non-terminal and exercise the refusal branch. Same idiom as the shared
// write-confirmation prompt (internal/cli/confirm.go).
//
// Two names rather than one because Windows has two devices (pkg/console). On
// Unix they hold the same path, which is how console.Open is told to open it
// once — so redirecting BOTH is what a test must do, and redirecting only one
// silently exercises the wrong shape.
var (
	ttyInPath  = console.InPath
	ttyOutPath = console.OutPath
)

// Whether this process is being driven through a terminal, as opposed to one
// merely existing on the machine. A separate seam from the paths above because a
// test must be able to disagree with the two independently: "the device opens
// but nobody is attending" is the agent's shape, and it is the one that hung.
var stdioIsTerminal = console.StdioIsTerminal

// openTTY returns the controlling terminal, or the structured refusal an agent
// receives when there is none. The hint names the command the USER should run,
// because an agent's only correct move here is to relay it.
// what names the action for the message; relay names the command the USER should
// run, and must be the one the caller actually is — a refusal that points at the
// wrong command is worse than none, because an agent relays it verbatim.
func openTTY(what, relay string) (console.Pair, error) {
	refuse := func() error {
		// interactive_required, not failed_precondition: this is not "your
		// arguments were wrong, adjust and retry" — no argument reaches it. Exit 2
		// says the former, and an agent that reads it as such goes looking for a
		// variation that works, which is precisely how it finds `script -q
		// /dev/null`. The dedicated code says instead: stand aside, the user acts,
		// then retry unchanged.
		return errs.InteractiveRequired(
			"%s requires a real terminal", what).
			WithHint("ask the user to run `%s %s` in their own terminal — it prompts, and "+
				"there is no flag, stdin or environment variable that stands in for it, "+
				"so do not look for one", cliname.Name(), relay)
	}
	// Our own streams first. An unattended caller must reach this refusal rather
	// than a prompt on a terminal it does not own — see the CORRECTED paragraph
	// above, and console.StdioIsTerminal.
	if !stdioIsTerminal() {
		return console.Pair{}, refuse()
	}
	// Every failure console.Open can report — no such device, no console at all,
	// a path that is not a terminal — is the same answer here: there is nobody to
	// ask. The distinctions matter to console.Open's own tests, not to the person
	// reading this refusal.
	p, err := console.Open(ttyInPath, ttyOutPath)
	if err != nil {
		return console.Pair{}, refuse()
	}
	return p, nil
}

// promptLine reads one echoed line — for a value that is an identifier rather
// than a secret, where seeing it typed is what catches a mistyped paste.
func promptLine(tty console.Pair, label string) (string, error) {
	if _, err := tty.Out.WriteString(label); err != nil {
		return "", errs.Internal("cannot write prompt: %v", err).WithCause(err)
	}
	var b []byte
	buf := make([]byte, 1)
	for {
		n, err := tty.In.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			if buf[0] != '\r' {
				b = append(b, buf[0])
			}
		}
		if err != nil {
			if len(b) == 0 {
				return "", errs.Internal("cannot read input: %v", err).WithCause(err)
			}
			break
		}
	}
	return strings.TrimSpace(stripTerminalControls(string(b))), nil
}

// promptSecret reads one line without echoing it.
func promptSecret(tty console.Pair, label string) (string, error) {
	if _, err := tty.Out.WriteString(label); err != nil {
		return "", errs.Internal("cannot write prompt: %v", err).WithCause(err)
	}
	// Echo suppression is applied to the INPUT side. On a platform where the two
	// sides are one handle that is the same thing; where they are not, raw mode on
	// the output side would suppress nothing.
	b, err := term.ReadPassword(int(tty.In.Fd()))
	_, _ = tty.Out.WriteString("\n")
	if err != nil {
		return "", errs.Internal("cannot read secret: %v", err).WithCause(err)
	}
	return strings.TrimSpace(stripTerminalControls(string(b))), nil
}

// stripTerminalControls removes escape sequences and stray control bytes from
// typed or pasted input, so a value carrying them cannot be stored and later
// replayed into a terminal — or silently corrupt the credential it is meant to
// be. Only printable content survives.
func stripTerminalControls(s string) string {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case c == 0x1b: // ESC — start of an escape sequence
			if i+1 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') { // CSI or SS3
				j := i + 2
				for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f { // params / intermediates
					j++
				}
				if j < len(b) && b[j] >= 0x40 && b[j] <= 0x7e { // final byte
					j++
				}
				i = j - 1 // the loop's i++ steps past the consumed run
			} else {
				i++ // drop ESC and the single following byte (or a lone trailing ESC)
			}
		case c < 0x20: // stray C0 control byte
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
