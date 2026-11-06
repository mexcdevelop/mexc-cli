package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

func TestParseFileEntries_FieldEqualsPath(t *testing.T) {
	decl := &api.MultipartDecl{Files: []api.MultipartFile{{Field: "file"}, {Field: "thumb"}}}
	entries, provided, err := parseFileEntries(decl, []string{"file=/a/x.png", "thumb=/a/y.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 || entries[0].field != "file" || entries[0].path != "/a/x.png" {
		t.Fatalf("entries not parsed: %+v", entries)
	}
	if !provided["file"] || !provided["thumb"] {
		t.Fatalf("provided set wrong: %+v", provided)
	}
}

func TestParseFileEntries_BarePathSingleField(t *testing.T) {
	decl := &api.MultipartDecl{Files: []api.MultipartFile{{Field: "file"}}}
	entries, provided, err := parseFileEntries(decl, []string{"/a/only.png"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 || entries[0].field != "file" || entries[0].path != "/a/only.png" {
		t.Fatalf("bare path not mapped to the single field: %+v", entries)
	}
	if !provided["file"] {
		t.Fatalf("provided set wrong: %+v", provided)
	}
}

func TestParseFileEntries_BarePathAmbiguous(t *testing.T) {
	decl := &api.MultipartDecl{Files: []api.MultipartFile{{Field: "file"}, {Field: "thumb"}}}
	_, _, err := parseFileEntries(decl, []string{"/a/x.png"})
	if err == nil || !strings.Contains(err.Error(), "<field>=<path>") {
		t.Fatalf("a bare path with multiple file fields must fail, got: %v", err)
	}
}

func TestParseFileEntries_Duplicate(t *testing.T) {
	decl := &api.MultipartDecl{Files: []api.MultipartFile{{Field: "file"}}}
	_, _, err := parseFileEntries(decl, []string{"file=/a/x.png", "file=/a/y.png"})
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("a duplicate file field must fail, got: %v", err)
	}
}

func TestReadFileParts_ReadsContentAndBaseName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "avatar.png")
	if err := os.WriteFile(p, []byte("PNGDATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	parts, err := readFileParts([]fileEntry{{field: "file", path: p}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 || parts[0].Field != "file" || parts[0].Filename != "avatar.png" ||
		string(parts[0].Content) != "PNGDATA" {
		t.Fatalf("file part not read correctly: %+v", parts)
	}
}

func TestReadFileParts_MissingFile(t *testing.T) {
	_, err := readFileParts([]fileEntry{{field: "file", path: "/no/such/path/here.png"}})
	if err == nil || !strings.Contains(err.Error(), "cannot read --file") {
		t.Fatalf("a missing file must fail, got: %v", err)
	}
}
