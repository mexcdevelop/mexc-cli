package console

import (
	"os"
	"path/filepath"
	"testing"
)

// A regular file is not a terminal, which is the shape a redirected stream has.
// Driving BOTH sides and then each side alone is the point: the exchange needs a
// prompt somebody sees AND a reply somebody types, so either half being a pipe
// has to answer false. A test that only redirected both would pass against an ||
// where the code needs &&.
func TestStdioIsTerminal_RedirectedStreamsAreNotATerminal(t *testing.T) {
	dir := t.TempDir()
	regular := func(name string) *os.File {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	origIn, origOut := stdin, stdout
	t.Cleanup(func() { stdin, stdout = origIn, origOut })

	for _, tc := range []struct {
		name        string
		redirectIn  bool
		redirectOut bool
	}{
		{"both redirected", true, true},
		{"only input redirected", true, false},
		{"only output redirected", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdin, stdout = origIn, origOut
			if tc.redirectIn {
				stdin = regular("in-" + tc.name)
			}
			if tc.redirectOut {
				stdout = regular("out-" + tc.name)
			}
			if StdioIsTerminal() {
				t.Error("a redirected stream must not report a terminal — this is the gate that keeps " +
					"an unattended caller off a console it does not own")
			}
		})
	}
}

// The device and the streams are independent, and this is the pairing that used
// to be missed: Open can succeed on the very machine where StdioIsTerminal is
// false, because a child with piped streams still inherits the terminal. Asserted
// with regular files (Open refuses those) only for the half that can be produced
// without a pty; the real pairing is measured in the binary-level checks, which
// the package comment records.
func TestStdioIsTerminal_IsNotWhatOpenAnswers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-a-tty")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	if _, err := Open(p, p); err == nil {
		t.Fatal("Open must refuse a regular file")
	}
	// And the two are asked separately at the call sites, so neither answer may be
	// inferred from the other: nothing in this package couples them.
	origIn, origOut := stdin, stdout
	t.Cleanup(func() { stdin, stdout = origIn, origOut })
	in, _ := os.Open(p)
	stdin, stdout = in, in
	t.Cleanup(func() { _ = in.Close() })
	if StdioIsTerminal() {
		t.Error("regular-file stdio must report false")
	}
}
