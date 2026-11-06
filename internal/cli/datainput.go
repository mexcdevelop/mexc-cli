package cli

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// utf8BOM is the byte order mark Windows tooling writes at the front of a file
// it calls UTF-8. Measured on Windows PowerShell 5.1: `Set-Content -Encoding
// utf8`, `Out-File -Encoding utf8` and plain `>` redirection ALL emit it —
// three of the five obvious ways to write a file there, and the three a person
// reaches for first.
//
// It has to be stripped rather than reported, because @path is the form this
// CLI recommends precisely so that nobody has to think about their shell. A
// body file that the platform's own default tooling produces must simply work;
// telling the user to go find a "UTF-8 without BOM" option is asking them to
// know the thing @path exists to spare them. Left in, it surfaces as
// `invalid character '\ufeff' looking for beginning of value` — an error whose
// text names a codepoint most people have never seen.
//
// Written as an escape, not as the character: Go rejects a literal BOM anywhere
// but the first byte of a source file, which is the same trap one layer down.
const utf8BOM = "\ufeff"

// bodyText turns bytes read from a file or stdin into body text: BOM off,
// surrounding whitespace off. Trailing whitespace matters because a file
// written by an editor ends in a newline, and a JSON body with one is fine —
// but the emptiness checks below must not be fooled by a file holding only it.
func bodyText(b []byte) string {
	return strings.TrimSpace(strings.TrimPrefix(string(b), utf8BOM))
}

// refuseUTF16 rejects a UTF-16 body with an error that names the encoding.
//
// Unlike the UTF-8 BOM this cannot be shrugged off: every second byte is NUL and
// no amount of trimming makes it JSON. Transcoding it would be guessing at what
// the caller meant to send, on a payload that may be an order. What is worth
// doing is not making them diagnose it: without this, the body reaches the JSON
// parser as a NUL byte and reports `invalid character '\x00'`, which says
// nothing about the file's encoding. PowerShell reaches this through
// `Out-File -Encoding unicode`, and through `>` on hosts where the default is
// UTF-16 rather than the UTF-8-with-BOM measured above.
func refuseUTF16(b []byte, source string) error {
	if len(b) < 2 {
		return nil
	}
	if (b[0] == 0xFF && b[1] == 0xFE) || (b[0] == 0xFE && b[1] == 0xFF) {
		return errs.Validation(errs.SubtypeInvalidArgument,
			"--data %s is UTF-16, and the body must be UTF-8", source).
			WithParam("--data").
			WithHint("rewrite it as UTF-8 (PowerShell: `[IO.File]::WriteAllText(path, json)`, or `Set-Content -Encoding utf8`; a BOM is fine)")
	}
	return nil
}

// refuseNonUTF8 rejects a body whose bytes are not UTF-8 at all.
//
// Runs AFTER refuseUTF16 on purpose. UTF-16 is also invalid UTF-8, and it has a
// more specific thing to say; what reaches here is a legacy code page, which on a
// Chinese-locale Windows is what an editor produces by DEFAULT — Notepad's "ANSI"
// save is GB18030, and PowerShell's Set-Content without -Encoding writes the
// system code page.
//
// Measured: such a file reached the JSON parser and came back as
// `json: unknown field "<mojibake>"`, with a hint pointing at `mexc schema`. That
// sends the caller to check their field names when the file's encoding is the
// problem — the same class of misdirection refuseUTF16 exists to prevent, arriving
// through the one encoding that carries no byte-order mark to spot it by.
//
// NOT transcoded, for the reason refuseUTF16 gives: converting would be guessing
// at what the caller meant to send, on a payload that may be an order.
func refuseNonUTF8(b []byte, source string) error {
	if utf8.Valid(b) {
		return nil
	}
	// The offset is what makes this actionable: it points at the first byte that
	// cannot begin a UTF-8 sequence, which is where the other encoding shows
	// itself. utf8.Valid has already told us one exists, so the loop terminates.
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--data %s is not UTF-8 (byte 0x%02X at offset %d), and the body must be UTF-8",
				source, b[i], i).
				WithParam("--data").
				WithHint("a legacy code page is the usual cause on Windows: Notepad's \"ANSI\" save is " +
					"GB18030, and `Set-Content` without -Encoding writes the system code page. Re-save " +
					"as UTF-8 — PowerShell: `[IO.File]::WriteAllText(path, json)`, or " +
					"`Set-Content -Encoding utf8` (a BOM is fine)")
		}
		i += size
	}
	return nil
}

// resolveDataInput turns the raw --data argument into the JSON text the rest of
// the command path works with. Four conventions, checked in this order:
//
//	'...'    → the surrounding single quotes are stripped, then the result is
//	           re-dispatched through the rules below
//	-        → read the whole of stdin
//	@@x      → the literal value @x (the escape for a body that starts with @)
//	@path    → read the file at path
//	anything → used as given
//
// WHY THIS EXISTS AT ALL. The body is one JSON document passed as one argv
// entry, and JSON's two structural characters — the double quote and the brace —
// are exactly the two a Windows shell treats as its own. `--data '{"a":1}'` is
// the form every skill document shows, and it is a bash form: cmd.exe does not
// recognise the single quote as a quoting character, so the pair arrives inside
// the value, and PowerShell 5's native-command argument encoding can drop the
// inner double quotes on the way to a non-.NET executable. Both produce a value
// that is not JSON, from a command line that looks exactly right.
//
// So the fix cannot be "document the correct quoting" alone. An agent handed a
// body-shaped rejection has no way to tell a malformed payload from an intact
// payload the shell chewed, and its next move is to start permuting quotes and
// re-running — on this CLI that means re-running writes. The two file-shaped
// forms below exist to take quoting out of the loop entirely: @path and - carry
// no shell metacharacters on any platform.
//
// WHY THE QUOTE STRIP IS SAFE HERE, and not a general-purpose guess: every body
// this CLI declares is an object or an array (api.Operation.Body is a struct or
// a slice), so a valid payload begins with { or [ and never with an apostrophe.
// A leading and trailing ' is therefore not ambiguous evidence — there is no
// legitimate body it could belong to. Stripping BEFORE the dispatch, rather than
// after it as a last resort, is what makes `--data '@./body.json'` work in
// cmd.exe too: the quotes come off and the @ is still seen.
//
// What is deliberately NOT done: nothing tries to reconstruct a payload whose
// inner double quotes are already gone. Rebuilding one means guessing where the
// strings were, and the guess would be applied to an order. A body that reaches
// here mangled must fail as a body — @path and - are the way out.
func resolveDataInput(raw string, stdin io.Reader) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
		if raw == "" {
			return "", nil
		}
	}

	switch {
	case raw == "-":
		return readDataStdin(stdin)
	case strings.HasPrefix(raw, "@@"):
		// Escape, not a path: hand back the literal @-prefixed value.
		return raw[1:], nil
	case strings.HasPrefix(raw, "@"):
		return readDataFile(strings.TrimSpace(raw[1:]))
	default:
		return raw, nil
	}
}

// readDataStdin consumes stdin for `--data -`.
//
// The terminal check is the same lesson pkg/console records for the confirm
// prompt, applied to the other direction: a read that nobody is feeding does not
// fail, it BLOCKS, and a blocked command produces no output for a caller to
// route on. `--data -` typed at an interactive prompt with nothing piped is that
// case, so it is refused with something to act on instead.
//
// A caller that DID pipe a body has a non-terminal stdin and passes straight
// through. Note the consequence for a write, which is correct and not a
// limitation to work around: piping makes stdin a pipe, the confirm prompt reads
// stdio as unattended (console.StdioIsTerminal), and the write returns
// confirmation_required. Piping a body therefore goes with --yes, after the user
// has confirmed — the same rule as any other unattended invocation.
func readDataStdin(stdin io.Reader) (string, error) {
	if stdin == nil {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--data - was given but this invocation has no stdin to read").
			WithParam("--data").
			WithHint("pass the JSON inline, or as --data @<path>")
	}
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--data - reads the body from stdin, but stdin is a terminal with nothing piped into it").
			WithParam("--data").
			WithHint("pipe the JSON in (e.g. `... --data - <<'JSON'`), or pass it as --data @<path>")
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"cannot read --data from stdin: %v", err).
			WithParam("--data")
	}
	if err := refuseUTF16(b, "on stdin"); err != nil {
		return "", err
	}
	if err := refuseNonUTF8(b, "on stdin"); err != nil {
		return "", err
	}
	s := bodyText(b)
	if s == "" {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--data - was given but stdin is empty").
			WithParam("--data").
			WithHint("pipe the JSON body in, or pass it as --data @<path>")
	}
	return s, nil
}

// readDataFile reads the body from a path given as --data @<path>.
//
// The path policy is the one --file already uses (multipart.go: plain os.ReadFile,
// any path the caller can read). This is a body the caller is choosing to send,
// not a credential — pkg/secfile's no-follow open guards the profile store, and
// borrowing it here would only refuse symlinks that a person deliberately pointed
// at their own request body.
func readDataFile(path string) (string, error) {
	if path == "" {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--data @ is missing a file path").
			WithParam("--data").
			WithHint("give the file to read, e.g. --data @./body.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"cannot read --data file %q: %v", path, err).
			WithParam("--data").
			WithHint("check the path exists and is readable (a relative path resolves against the current directory)")
	}
	if err := refuseUTF16(b, "file "+strconv.Quote(path)); err != nil {
		return "", err
	}
	if err := refuseNonUTF8(b, "file "+strconv.Quote(path)); err != nil {
		return "", err
	}
	s := bodyText(b)
	if s == "" {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--data file %q is empty", path).
			WithParam("--data")
	}
	return s, nil
}
