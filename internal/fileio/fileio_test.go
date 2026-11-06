package fileio

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hashHex is the trailing uniqueness token: the last hyphen-separated segment
// before any extension. Helper so the tests assert the naming shape without
// recomputing the hash (which would just duplicate the implementation).
func hashPart(name string) string {
	base := name
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if i := strings.LastIndex(base, "-"); i >= 0 {
		return base[i+1:]
	}
	return base
}

func TestDefaultDownloadName_ContentDisposition(t *testing.T) {
	name := DefaultDownloadName(NameInput{
		Method: "GET", Path: "/files/abc/download", Query: "",
		ContentDisposition: `attachment; filename="report.pdf"`,
	})
	if !strings.HasPrefix(name, "report-") {
		t.Fatalf("expected readable prefix from Content-Disposition, got %q", name)
	}
	if !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("expected .pdf extension, got %q", name)
	}
	if len(hashPart(name)) != hashLen {
		t.Fatalf("expected a %d-char hash segment, got %q", hashLen, name)
	}
}

func TestDefaultDownloadName_ExtensionFromContentType(t *testing.T) {
	name := DefaultDownloadName(NameInput{
		Method: "GET", Path: "/files/abc/download",
		ContentType: "application/pdf",
	})
	// No Content-Disposition: no readable prefix, just <hash>.pdf.
	if !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("expected .pdf from Content-Type, got %q", name)
	}
	if name != hashPart(name)+".pdf" {
		t.Fatalf("expected bare <hash>.pdf, got %q", name)
	}
}

func TestDefaultDownloadName_UnknownTypeHasNoFakeExt(t *testing.T) {
	name := DefaultDownloadName(NameInput{Method: "GET", Path: "/files/abc/download"})
	if filepath.Ext(name) != "" {
		t.Fatalf("expected no synthetic extension, got %q", name)
	}
	if len(name) != hashLen {
		t.Fatalf("expected a bare %d-char hash, got %q", hashLen, name)
	}
}

func TestDefaultDownloadName_DeterministicAndUnique(t *testing.T) {
	a := DefaultDownloadName(NameInput{Method: "GET", Path: "/export", Query: "id=1"})
	again := DefaultDownloadName(NameInput{Method: "GET", Path: "/export", Query: "id=1"})
	if a != again {
		t.Fatalf("same request must yield same name: %q vs %q", a, again)
	}
	byQuery := DefaultDownloadName(NameInput{Method: "GET", Path: "/export", Query: "id=2"})
	if a == byQuery {
		t.Fatalf("different query must yield different name, both %q", a)
	}
	byBody := DefaultDownloadName(NameInput{Method: "POST", Path: "/export", Query: "id=1", Body: []byte(`{"fmt":"csv"}`)})
	byBody2 := DefaultDownloadName(NameInput{Method: "POST", Path: "/export", Query: "id=1", Body: []byte(`{"fmt":"pdf"}`)})
	if byBody == byBody2 {
		t.Fatalf("different body must yield different name, both %q", byBody)
	}
}

func TestDefaultDownloadName_StripsDangerousUnicode(t *testing.T) {
	// A bidi-override in the server's Content-Disposition filename must be stripped
	// so the saved filename cannot be visually spoofed.
	cd := `attachment; filename="re` + string(rune(0x202E)) + `port.pdf"`
	name := DefaultDownloadName(NameInput{Method: "GET", Path: "/files/x/download", ContentDisposition: cd})
	if strings.ContainsRune(name, 0x202E) {
		t.Fatalf("bidi-override not stripped from filename: %q", name)
	}
	if !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("expected .pdf preserved, got %q", name)
	}
}

func TestDefaultDownloadName_SanitizesTraversal(t *testing.T) {
	name := DefaultDownloadName(NameInput{
		Method: "GET", Path: "/files/abc/download",
		ContentDisposition: `attachment; filename="../../etc/passwd"`,
	})
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		t.Fatalf("filename must be sanitized to a single path element, got %q", name)
	}
	if !strings.HasPrefix(name, "passwd-") {
		t.Fatalf("expected sanitized base 'passwd', got %q", name)
	}
}

func TestResolveOutputPath_RelativeWithinCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	root, _ := filepath.EvalSymlinks(dir)

	// A bare relative file path resolves to an absolute path under the cwd.
	got, err := ResolveOutputPath("out.bin", false, "derived-1234.bin")
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if !filepath.IsAbs(got) || !strings.HasPrefix(got, root) {
		t.Fatalf("resolved must be absolute and under cwd %q: %q", root, got)
	}
	if filepath.Base(got) != "out.bin" {
		t.Fatalf("base = %q", filepath.Base(got))
	}

	// Omitted output -> the derived name in the cwd.
	got, err = ResolveOutputPath("", false, "derived-1234.bin")
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if !strings.HasPrefix(got, root) || filepath.Base(got) != "derived-1234.bin" {
		t.Fatalf("default name in cwd expected: %q", got)
	}

	// An existing directory -> the derived name inside it.
	if err := os.Mkdir("sub", 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveOutputPath("sub", false, "derived-1234.bin")
	if err != nil {
		t.Fatalf("dir target: %v", err)
	}
	if filepath.Base(got) != "derived-1234.bin" || !strings.Contains(got, "sub") {
		t.Fatalf("dir join expected: %q", got)
	}
}

func TestResolveOutputPath_RejectsUnsafe(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cases := map[string]string{
		"absolute path":  filepath.Join(dir, "abs.bin"),
		"parent escape":  "../escape.bin",
		"control char":   "a\x00b.bin",
		"empty via dots": "  ",
	}
	for name, out := range cases {
		if _, err := ResolveOutputPath(out, false, "d.bin"); err == nil {
			t.Fatalf("%s: expected rejection for %q", name, out)
		}
	}
}

func TestResolveOutputPath_RejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir() // a directory NOT under the cwd
	cwd := t.TempDir()
	t.Chdir(cwd)
	if err := os.Symlink(outside, "link"); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}

	// A not-yet-existing target reached through the escaping symlink is rejected
	// (resolveNearestAncestor resolves "link" -> outside, which is not under cwd).
	if _, err := ResolveOutputPath("link/new.bin", false, "d.bin"); err == nil {
		t.Fatal("expected rejection: symlink target escapes cwd (non-existing target)")
	}
	// An existing target under the escaping symlink is likewise rejected
	// (EvalSymlinks resolves it outside cwd).
	if err := os.WriteFile(filepath.Join(outside, "exists.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveOutputPath("link/exists.bin", true, "d.bin"); err == nil {
		t.Fatal("expected rejection: symlink target escapes cwd (existing target)")
	}
}

func TestResolveOutputPath_RejectsTabNewline(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, bad := range []string{"a\tb.bin", "a\nb.bin"} {
		if _, err := ResolveOutputPath(bad, false, "d.bin"); err == nil {
			t.Fatalf("expected rejection of control char in %q", bad)
		}
	}
}

func TestResolveOutputPath_OverwriteAndParent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile("exists.bin", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveOutputPath("exists.bin", false, "d"); err == nil {
		t.Fatal("expected 'already exists' error without overwrite")
	}
	if _, err := ResolveOutputPath("exists.bin", true, "d"); err != nil {
		t.Fatalf("overwrite must be allowed: %v", err)
	}
	// A missing parent directory resolves fine here (it is created at write time).
	if _, err := ResolveOutputPath("nope/out.bin", false, "d"); err != nil {
		t.Fatalf("a missing parent must not fail resolution: %v", err)
	}
}

func TestResolveOutputPath_RejectsDangerousUnicode(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// U+202E (RLO) in --output must be rejected (filename-spoofing guard).
	rlo := "re" + string(rune(0x202E)) + "port.bin"
	if _, err := ResolveOutputPath(rlo, false, "d.bin"); err == nil {
		t.Fatal("expected rejection of a bidi-override character in --output")
	}
}

// cwdRoot returns the canonical absolute cwd, matching what ResolveOutputPath
// produces (the resolved path AtomicWriteFromReader is given in production).
func cwdRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	r, err := filepath.EvalSymlinks(wd)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAtomicWriteFromReader(t *testing.T) {
	t.Chdir(t.TempDir())
	target := filepath.Join(cwdRoot(t), "out.bin")

	payload := []byte("streamed-bytes-\x00\xff")
	n, err := AtomicWriteFromReader(target, strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("bytes written = %d, want %d", n, len(payload))
	}
	if got, _ := os.ReadFile(target); string(got) != string(payload) {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestAtomicWriteFromReader_CreatesParentDirs(t *testing.T) {
	t.Chdir(t.TempDir())
	target := filepath.Join(cwdRoot(t), "a", "b", "out.bin")

	// A nested target within the cwd has its parent directories created.
	if _, err := AtomicWriteFromReader(target, strings.NewReader("hi")); err != nil {
		t.Fatalf("write with missing parents: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "hi" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestAtomicWriteFromReader_RefusesSymlinkComponentEscape(t *testing.T) {
	outside := t.TempDir()
	cwd := t.TempDir()
	t.Chdir(cwd)
	root := cwdRoot(t)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	// The target is lexically under cwd (root/link/...), but "link" is a symlink
	// escaping cwd — a lexical-only check would follow it and write outside.
	target := filepath.Join(root, "link", "out.bin")
	if _, err := AtomicWriteFromReader(target, strings.NewReader("x")); err == nil {
		t.Fatal("expected refusal: a symlink component escapes the cwd")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "out.bin")); !os.IsNotExist(statErr) {
		t.Fatal("nothing must be written outside cwd through a symlink component")
	}
}

func TestAtomicWriteFromReader_RefusesOutsideCwd(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	outside := filepath.Join(t.TempDir(), "escape.bin") // a different temp dir, not under cwd
	if _, err := AtomicWriteFromReader(outside, strings.NewReader("x")); err == nil {
		t.Fatal("expected refusal to write outside the working directory")
	}
	if _, statErr := os.Stat(outside); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("nothing must be written outside the cwd")
	}
}

// errReader fails partway through, to prove a failed copy leaves no target file.
type errReader struct{ n int }

func (e *errReader) Read(p []byte) (int, error) {
	if e.n > 0 {
		e.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("boom")
}

func TestAtomicWriteFromReader_FailureLeavesNoTarget(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	target := filepath.Join(cwdRoot(t), "partial.bin")

	_, err := AtomicWriteFromReader(target, &errReader{n: 3})
	if err == nil {
		t.Fatal("expected a copy error")
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("failed write must not leave a target file")
	}
	// And no leftover temp files in the directory.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected no leftover files, found %v", names)
	}
}

var _ io.Reader = (*errReader)(nil)
