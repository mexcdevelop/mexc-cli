// Package console opens the controlling terminal — the device a person is
// sitting at, as distinct from whatever stdin and stdout happen to point at.
//
// It exists for two reasons, and neither is abstraction for its own sake.
//
// The controlling terminal is ONE device on Unix and TWO on Windows, and that
// difference reaches the call site: a prompt and the reply it collects travel
// through the same handle in one case and different handles in the other. A
// caller written for one shape does not run on the other.
//
// And the two callers that need it sit on opposite sides of an import boundary.
// An edition may not import internal/ (the invariant .golangci.yml's depguard
// asserts), so editions/public/command and internal/cli have no other way to
// share this. Stated twice, the two copies are free to drift, and the half that
// drifts is a platform nobody here can run.
//
// WHAT THIS IS NOT. It is not a terminal abstraction, and it never READS from or
// WRITES to stdin, stdout or stderr. Whether a secret may be read from the
// standard streams is a product decision with a long argument behind it, made
// and documented by the callers; this package only opens the device they
// decided on.
//
// It does INSPECT those streams, in one function and for one reason:
// StdioIsTerminal. "Does this machine have a console" (Open) and "is this
// invocation being driven through one" (StdioIsTerminal) are different
// questions, and the device cannot answer the second — a child process with
// piped streams still inherits its parent's terminal, so Open succeeds for a
// caller nobody is attending. The first version of this package offered only
// Open, and both callers read it as the second question; that is how an agent's
// write came to hang on a prompt nobody could see.
package console

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// Pair is the controlling terminal's read side and its write side.
//
// Where one handle does both, In and Out are the SAME *os.File. Callers may rely
// on that: it is not an implementation detail, it is what "one device" means on
// the platforms that have one.
type Pair struct {
	In  *os.File // typed input, including a secret read with echo suppressed
	Out *os.File // prompts
}

// Close releases whatever was opened, once — the shared-handle case must not be
// closed twice. A zero Pair closes nothing, so a caller that defers this before
// checking the error is still correct.
func (p Pair) Close() error {
	if p.In == nil {
		return nil
	}
	err := p.In.Close()
	if p.Out != nil && p.Out != p.In {
		if oerr := p.Out.Close(); err == nil {
			err = oerr
		}
	}
	return err
}

// Open opens the controlling terminal and confirms that what it got really is
// one. Failing either way is the answer a caller wants: no console means no
// person to ask.
//
// The paths are parameters rather than being read from InPath/OutPath directly
// so that a caller can point them at a regular file and exercise its own refusal
// branch against this code rather than against a stub. Two DIFFERENT paths is
// the Windows shape, and passing two regular files is how a machine with no
// Windows runs it.
//
// Both sides are opened O_RDWR although each is used one way. On Windows that is
// not a stylistic choice: reading a secret without echo puts the console into
// raw mode through SetConsoleMode, which needs write access to the input buffer,
// so an O_RDONLY CONIN$ would open cleanly and then fail at the one moment that
// matters. Unix is indifferent, and one flag for both keeps the difference off
// the platform that cannot be tested here.
func Open(inPath, outPath string) (Pair, error) {
	in, err := os.OpenFile(inPath, os.O_RDWR, 0)
	if err != nil {
		return Pair{}, err
	}
	p := Pair{In: in, Out: in}
	if outPath != inPath {
		out, oerr := os.OpenFile(outPath, os.O_RDWR, 0)
		if oerr != nil {
			_ = in.Close()
			return Pair{}, oerr
		}
		p.Out = out
	}
	// Opening is not enough. A path can exist and be a regular file — which is
	// exactly what a redirected caller, and every test of the refusal branch, has.
	if !term.IsTerminal(int(p.In.Fd())) {
		_ = p.Close()
		return Pair{}, fmt.Errorf("%s is not a terminal", inPath)
	}
	if p.Out != p.In && !term.IsTerminal(int(p.Out.Fd())) {
		_ = p.Close()
		return Pair{}, fmt.Errorf("%s is not a terminal", outPath)
	}
	return p, nil
}
