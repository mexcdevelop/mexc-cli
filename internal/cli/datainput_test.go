package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// The unit surface: every convention, and the two that must NOT fire.
func TestResolveDataInput(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		stdin string
		want  string
	}{
		{
			name: "plain inline JSON is untouched",
			raw:  `{"symbol":"BTC_USDT"}`,
			want: `{"symbol":"BTC_USDT"}`,
		},
		{
			// cmd.exe does not treat ' as a quoting character, so it arrives
			// inside the value. Stripping it is what this covers.
			//
			// It is NOT a claim that this rescues every cmd.exe body — measured
			// with an argv probe, `--data '{"symbol":"BTC_USDT"}'` reaches the
			// exe as `'{symbol:BTC_USDT}'`: cmd ate the double quotes as well,
			// and those are gone for good. The case this genuinely saves is the
			// one below, where there were none to lose.
			name: "a single-quoted body has the quotes stripped",
			raw:  `'{"symbol":"BTC_USDT"}'`,
			want: `{"symbol":"BTC_USDT"}`,
		},
		{
			// The measured cmd.exe delivery for `--data '[999999999]'`, which is
			// exactly how the skills write a cancel. No double quotes in a scalar
			// array, so the outer pair is the only damage and stripping it makes
			// the body whole.
			name: "a scalar array is fully recovered, as cmd.exe delivers it",
			raw:  `'[999999999]'`,
			want: `[999999999]`,
		},
		{
			// An apostrophe INSIDE a JSON string is not a wrapper and must not be
			// touched: only a matched leading+trailing pair is stripped.
			name: "an apostrophe inside the body is not a wrapper",
			raw:  `{"note":"it's fine"}`,
			want: `{"note":"it's fine"}`,
		},
		{
			name: "@@ escapes a body that really starts with @",
			raw:  `@@{"a":1}`,
			want: `@{"a":1}`,
		},
		{
			name:  "- reads stdin",
			raw:   "-",
			stdin: "  {\"from\":\"stdin\"}\n",
			want:  `{"from":"stdin"}`,
		},
		{
			// The strip runs BEFORE the dispatch, which is what makes the file
			// form reachable from cmd.exe too.
			name:  "a quoted - still reads stdin",
			raw:   `'-'`,
			stdin: `{"from":"stdin"}`,
			want:  `{"from":"stdin"}`,
		},
		{
			name: "empty stays empty",
			raw:  "   ",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDataInput(tt.raw, strings.NewReader(tt.stdin))
			if err != nil {
				t.Fatalf("resolveDataInput(%q) errored: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("resolveDataInput(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// Windows PowerShell 5.1 writes a UTF-8 BOM from `Set-Content -Encoding utf8`,
// `Out-File -Encoding utf8` and plain `>` alike — measured, three of the five
// obvious ways to produce a body file there. @path is the form the skills tell
// people to fall back to, so it has to survive the platform's own default output.
func TestResolveDataInput_StripsTheUTF8BOM(t *testing.T) {
	const body = `{"symbol":"BTC_USDT"}`
	dir := t.TempDir()
	path := filepath.Join(dir, "bom.json")
	if err := os.WriteFile(path, []byte("\xef\xbb\xbf"+body+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveDataInput("@"+path, nil)
	if err != nil {
		t.Fatalf("a BOM-prefixed file must be readable: %v", err)
	}
	if got != body {
		t.Fatalf("file body = %q, want %q", got, body)
	}

	got, err = resolveDataInput("-", strings.NewReader("\xef\xbb\xbf"+body))
	if err != nil {
		t.Fatalf("a BOM-prefixed stdin must be readable: %v", err)
	}
	if got != body {
		t.Fatalf("stdin body = %q, want %q", got, body)
	}
}

// UTF-16 is refused rather than transcoded: every second byte is NUL, so no
// trimming makes it JSON, and guessing at the caller's intent is not something
// to do to a payload that may be an order. What the error must not do is leave
// them to work it out from `invalid character '\x00'`.
func TestResolveDataInput_RefusesUTF16(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		name string
		bom  string
	}{
		{"little endian", "\xff\xfe"},
		{"big endian", "\xfe\xff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".json")
			if err := os.WriteFile(path, []byte(tt.bom+"{\x00"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := resolveDataInput("@"+path, nil)
			if err == nil {
				t.Fatal("a UTF-16 body must be refused")
			}
			for _, w := range []string{"--data", "UTF-16", "UTF-8"} {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error must mention %q, got: %v", w, err)
				}
			}
		})
	}
}

// A legacy code page carries no byte-order mark, so refuseUTF16 cannot see it and
// the bytes travel all the way to the JSON parser. Measured before this check
// existed: a GBK body came back as `json: unknown field "<mojibake>"` with a hint
// pointing at `mexc schema` — the caller sent to check their field names while the
// file's encoding was the problem.
//
// This is the DEFAULT an editor produces on a Chinese-locale Windows (Notepad's
// "ANSI" save, and Set-Content without -Encoding), which is what makes it worth a
// check of its own rather than a footnote on the UTF-16 one.
func TestResolveDataInput_RefusesALegacyCodePage(t *testing.T) {
	dir := t.TempDir()
	// A two-key JSON object whose key and value are Chinese, encoded as GBK.
	// Written as raw bytes because the bytes ARE the subject: the same characters
	// as a Go string literal would be UTF-8 and would rightly pass.
	gbk := []byte{0x7b, 0x22, 0xb1, 0xb8, 0xd7, 0xa2, 0x22, 0x3a, 0x22, 0xd6, 0xb5, 0x22, 0x7d}

	for _, tt := range []struct {
		name string
		call func(path string) (string, error)
	}{
		{"file", func(path string) (string, error) { return resolveDataInput("@"+path, nil) }},
		{"stdin", func(string) (string, error) { return resolveDataInput("-", bytes.NewReader(gbk)) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".json")
			if err := os.WriteFile(path, gbk, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := tt.call(path)
			if err == nil {
				t.Fatal("a body in a legacy code page must be refused")
			}
			// The encoding must be named, and so must the offending byte and where it
			// is — without those the message is no more actionable than the JSON
			// parser's was.
			for _, w := range []string{"--data", "not UTF-8", "0xB1", "offset 2"} {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error must mention %q, got: %v", w, err)
				}
			}
			// And it must NOT be the JSON-shaped diagnosis this check exists to
			// replace: sending the caller to the schema is the wrong direction.
			var e *errs.Error
			if !errors.As(err, &e) {
				t.Fatalf("want a structured error, got %T", err)
			}
			if strings.Contains(e.Hint, "mexc schema") {
				t.Errorf("the hint must be about encoding, not the schema: %q", e.Hint)
			}
			if !strings.Contains(e.Hint, "utf8") && !strings.Contains(e.Hint, "UTF-8") {
				t.Errorf("the hint must say how to re-save the file: %q", e.Hint)
			}
		})
	}
}

// A valid UTF-8 body carrying non-ASCII must pass untouched. The check above
// rejects on bytes, so this is the half that keeps it from rejecting the whole
// world outside ASCII — a body with Chinese in it is ordinary, not an error.
func TestResolveDataInput_AcceptsValidUTF8NonASCII(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf8.json")
	body := `{"symbol":"BTC_USDT","note":"café naïve"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveDataInput("@"+path, nil)
	if err != nil {
		t.Fatalf("valid UTF-8 with non-ASCII must be accepted: %v", err)
	}
	if got != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestResolveDataInput_ReadsAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.json")
	if err := os.WriteFile(path, []byte("\n"+`{"symbol":"BTC_USDT"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Bare, and wrapped in the quotes cmd.exe would leave behind. Both must reach
	// the file: if the strip ran after the dispatch instead of before it, the
	// second would come back as the literal string "@<path>" and then fail body
	// validation with a message about JSON, pointing at the wrong thing entirely.
	for _, raw := range []string{"@" + path, "'@" + path + "'"} {
		got, err := resolveDataInput(raw, nil)
		if err != nil {
			t.Fatalf("resolveDataInput(%q) errored: %v", raw, err)
		}
		if got != `{"symbol":"BTC_USDT"}` {
			t.Fatalf("resolveDataInput(%q) = %q", raw, got)
		}
	}
}

// Every refusal names --data and says what to do instead. These are the errors an
// agent routes on, so the param and the hint matter as much as the failure.
func TestResolveDataInput_Rejections(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		raw      string
		stdin    *string
		wantText []string
	}{
		{
			name:     "missing file",
			raw:      "@" + filepath.Join(dir, "nope.json"),
			wantText: []string{"--data", "nope.json"},
		},
		{
			name:     "empty file",
			raw:      "@" + empty,
			wantText: []string{"--data", "is empty"},
		},
		{
			name:     "@ with no path",
			raw:      "@",
			wantText: []string{"--data", "file path"},
		},
		{
			name:     "empty stdin",
			raw:      "-",
			stdin:    strPtr(""),
			wantText: []string{"--data", "stdin is empty"},
		},
		{
			name:     "no stdin at all",
			raw:      "-",
			wantText: []string{"--data", "stdin"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := func() (string, error) {
				if tt.stdin == nil {
					return resolveDataInput(tt.raw, nil)
				}
				return resolveDataInput(tt.raw, strings.NewReader(*tt.stdin))
			}()
			if err == nil {
				t.Fatalf("resolveDataInput(%q) must fail, got %q", tt.raw, got)
			}
			for _, w := range tt.wantText {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error must mention %q, got: %v", w, err)
				}
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// End-to-end, on the real command path: a body that arrived quoted must validate
// and be sent, not be rejected as malformed JSON.
//
// This is the case that made the whole change worth doing. Before it, the failure
// on Windows was a body-shaped error for a command line that was correct, and the
// next move available to an agent holding one is to permute quotes and re-run —
// which on this CLI means re-running a write.
func TestDataFlag_AcceptsAQuotedBodyEndToEnd(t *testing.T) {
	stdout, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--dry-run",
		"--data", `'[{"symbol":"BTCUSDT","side":"BUY"}]'`)
	if code != 0 {
		t.Fatalf("a quoted body must be accepted: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"body_checked": true`) {
		t.Fatalf("the body must have been validated, not waved through: %s", stdout)
	}
}

func TestDataFlag_ReadsTheBodyFromAFileEndToEnd(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("body.json", []byte(`[{"symbol":"BTCUSDT","side":"BUY"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--dry-run", "--data", "@./body.json")
	if code != 0 {
		t.Fatalf("--data @file must be accepted: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"body_checked": true`) {
		t.Fatalf("the file's body must have been validated: %s", stdout)
	}
}

// A file whose contents are not valid JSON fails as a BODY, with the item-level
// message the inline form gives — the file is a transport for the body, not a
// separate validation path.
func TestDataFlag_AFileWithABadBodyStillFailsValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("body.json", []byte(`[{"side":"BUY"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--dry-run", "--data", "@./body.json")
	if code == 0 {
		t.Fatal("a file carrying an invalid body must still fail validation")
	}
	for _, w := range []string{"item 0", "symbol"} {
		if !strings.Contains(stderr, w) {
			t.Fatalf("error must mention %q, got: %s", w, stderr)
		}
	}
}

// An unreadable --data file must fail BEFORE the operation's write guard is
// reached, and it must not be papered over by --dry-run: a preview that cannot
// read the body it was pointed at has nothing to preview.
func TestDataFlag_MissingFileFailsUnderDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	_, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--dry-run", "--data", "@./absent.json")
	if code == 0 {
		t.Fatal("a missing --data file must fail the command")
	}
	if !strings.Contains(stderr, "absent.json") {
		t.Fatalf("error must name the file, got: %s", stderr)
	}
}
