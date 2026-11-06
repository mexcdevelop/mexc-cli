package console

import (
	"os"

	"golang.org/x/term"
)

// The standard streams, as package vars so this package's own test can point
// them at a regular file and drive the false branch. Same idiom as the callers'
// path vars, and for the same reason: neither answer can be produced on demand.
var (
	stdin  = os.Stdin
	stdout = os.Stdout
)

// StdioIsTerminal reports whether THIS process's own standard input and output
// are both terminals.
//
// WHY IT IS NOT Open. Open answers "does this machine have a console"; this
// answers "is this invocation being driven through one". They read as the same
// question and are not, and the second is the one a prompt needs.
//
// A process an agent shells out to inherits its parent's controlling terminal —
// the one the person is watching the AGENT in — while its own stdin and stdout
// are pipes the agent reads. Open therefore SUCCEEDS: the device is real, it is a
// terminal, and every check Open makes passes. A prompt rendered on it is written
// where the agent cannot see it and read from where nobody is typing, so the
// command hangs until something kills it — the one outcome worse than either
// asking or refusing, because it produces no output to route on.
//
// Measured under an agent host on Windows: stdin and stdout report IsTerminal
// false while CONIN$ and CONOUT$ both open with IsTerminal true. /dev/tty has the
// same property by POSIX — it resolves for any process that still has a
// controlling terminal, which a child with redirected streams does.
//
// So the device says nothing about the caller; only the caller's own streams do.
//
// THE CEILING IS UNCHANGED, and this must not be read as more than it is: a pty
// a program allocated for itself passes this exactly as a human's terminal does
// (see editions/public/command/tty.go, which measures that ceiling). This is a
// claim about streams, never about people. What it rules out is the shape this
// product actually runs in — an agent shelling out with pipes — which is the
// shape that used to hang.
//
// BOTH sides must be terminals, and that is a deliberately conservative policy
// rather than a mechanical necessity — stating it as necessity would be wrong.
// The prompt and the reply travel over the DEVICE, never over these streams, so
// a person who redirects only output (`mexc ... > out.json`) could in principle
// still be asked, and before this gate existed they were.
//
// They are refused anyway, because no single stream separates that person from
// an agent host that captures one stream and leaves the other on the terminal,
// and the two mistakes do not cost the same: guessing "unattended" costs a human
// one --yes and tells them so in the refusal, while guessing "attended" costs an
// agent a hang with no output to route on. A redirected stream is therefore read
// as evidence that the invocation is being CAPTURED, not as a broken channel.
//
// If this ever needs relaxing, the human redirect above is the case to weigh,
// and the shape described earlier is the one not to lose.
func StdioIsTerminal() bool {
	return term.IsTerminal(int(stdin.Fd())) && term.IsTerminal(int(stdout.Fd()))
}
