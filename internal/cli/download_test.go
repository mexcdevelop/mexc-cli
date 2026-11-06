package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// downloadDomains is a one-op domain set whose single operation is a binary
// download, used to exercise the ResponseKind: binary CLI path end to end.
func downloadDomains() []api.DomainSpec {
	return []api.DomainSpec{{
		Name: "spot", Short: "spot",
		Subs: []api.Subdomain{{
			Name: "files", Short: "files", Signed: api.SignNone, Identity: api.IdentityNone, HostKey: testHostKey,
			Ops: []api.Operation{{
				Name: "download", Method: "GET", Path: "/files/{id}/download",
				Access: api.Read, Summary: "download a file", ResponseKind: api.ResponseBinary,
				Params: []api.Param{{Name: "id", In: api.InPath, Required: true}},
			}},
		}},
	}}
}

func TestCollectMCPTools_WithholdsBinary(t *testing.T) {
	// A binary op cannot round-trip a file over an MCP string result, so — like a
	// multipart upload — it is withheld from the MCP face and stays CLI-only.
	if tools := collectMCPTools(downloadDomains(), mcpScope{}, false); len(tools) != 0 {
		t.Fatalf("binary op must be withheld from MCP, got %d tools", len(tools))
	}

	// The same op declared as JSON WOULD be exposed — proving it is the binary
	// response kind, not some other attribute, that withholds it.
	ds := downloadDomains()
	ds[0].Subs[0].Ops[0].ResponseKind = api.ResponseJSON
	if tools := collectMCPTools(ds, mcpScope{}, false); len(tools) != 1 {
		t.Fatalf("a JSON op must be exposed on MCP, got %d tools", len(tools))
	}
}

func runWith(ds []api.DomainSpec, env map[string]string, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", ds, nil, nil, true, func(k string) string { return env[k] }, &out, &errBuf)
	code = app.Run(args)
	return out.String(), errBuf.String(), code
}

func TestDownload_StreamsToOutputFile(t *testing.T) {
	payload := []byte("\x00PDF-DATA\xff\xfe")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	t.Chdir(t.TempDir())
	if err := os.Mkdir("dl", 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runWith(downloadDomains(), nil,
		"spot", "files", "download", "--id", "abc",
		"--base-url", srv.URL, "--output", "dl")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var res struct {
		SavedPath string `json:"saved_path"`
		Bytes     int64  `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not the expected JSON result: %v\n%s", err, stdout)
	}
	if filepath.Base(filepath.Dir(res.SavedPath)) != "dl" {
		t.Fatalf("saved outside the target dir: %s", res.SavedPath)
	}
	if got, _ := os.ReadFile(res.SavedPath); string(got) != string(payload) {
		t.Fatalf("file content mismatch: %q want %q", got, payload)
	}
	if res.Bytes != int64(len(payload)) {
		t.Fatalf("reported bytes = %d, want %d", res.Bytes, len(payload))
	}
}

func TestDownload_ExistingFileNeedsOverwrite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	t.Chdir(t.TempDir())
	const out = "fixed.bin"
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without --overwrite the pre-existing file must block the download.
	_, _, code := runWith(downloadDomains(), nil,
		"spot", "files", "download", "--id", "abc", "--base-url", srv.URL, "--output", out)
	if code == 0 {
		t.Fatal("expected non-zero exit when target exists without --overwrite")
	}
	if got, _ := os.ReadFile(out); string(got) != "old" {
		t.Fatalf("existing file must be untouched, got %q", got)
	}
	// With --overwrite it succeeds and replaces the file.
	_, stderr, code := runWith(downloadDomains(), nil,
		"spot", "files", "download", "--id", "abc", "--base-url", srv.URL, "--output", out, "--overwrite")
	if code != 0 {
		t.Fatalf("overwrite should succeed: code=%d stderr=%s", code, stderr)
	}
	if got, _ := os.ReadFile(out); string(got) != "data" {
		t.Fatalf("file should be replaced, got %q", got)
	}
}

func TestDownload_AllowedUnderReadOnly(t *testing.T) {
	// A download is a read: --read-only must NOT block it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	t.Chdir(t.TempDir())
	stdout, stderr, code := runWith(downloadDomains(), nil,
		"--read-only", "spot", "files", "download", "--id", "abc", "--base-url", srv.URL, "--output", "out.bin")
	if code != 0 {
		t.Fatalf("download must be allowed under --read-only: code=%d stderr=%s", code, stderr)
	}
	var res struct {
		SavedPath string `json:"saved_path"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout not the expected result: %v\n%s", err, stdout)
	}
	if got, _ := os.ReadFile(res.SavedPath); string(got) != "payload" {
		t.Fatalf("content mismatch: %q", got)
	}
}

func TestDownload_DryRunPrintsPlanWithoutWriting(t *testing.T) {
	// --dry-run builds and prints the request plan, sends nothing, writes nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("dry-run must not send a request")
	}))
	defer srv.Close()

	t.Chdir(t.TempDir())
	stdout, stderr, code := runWith(downloadDomains(), nil,
		"--dry-run", "spot", "files", "download", "--id", "abc", "--base-url", srv.URL, "--output", "out.bin")
	if code != 0 {
		t.Fatalf("dry-run code=%d stderr=%s", code, stderr)
	}
	var plan struct {
		Method string `json:"method"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatalf("dry-run stdout is not a plan: %v\n%s", err, stdout)
	}
	if plan.Method != "GET" || !strings.Contains(plan.URL, "/files/abc/download") {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	// Nothing was written to the cwd.
	if entries, _ := os.ReadDir("."); len(entries) != 0 {
		t.Fatalf("dry-run must not create files, found %d entries", len(entries))
	}
}

func TestDownload_RejectsAbsoluteOutput(t *testing.T) {
	// An absolute --output is refused by the cwd sandbox; nothing is written there.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	t.Chdir(t.TempDir())
	abs := filepath.Join(t.TempDir(), "abs.bin") // absolute, outside cwd
	_, stderr, code := runWith(downloadDomains(), nil,
		"spot", "files", "download", "--id", "abc", "--base-url", srv.URL, "--output", abs)
	if code == 0 {
		t.Fatal("expected a non-zero exit for an absolute --output")
	}
	if !strings.Contains(stderr, "relative") && !strings.Contains(stderr, "working directory") {
		t.Fatalf("error should explain the sandbox rule, got: %s", stderr)
	}
	if _, statErr := os.Stat(abs); !os.IsNotExist(statErr) {
		t.Fatal("nothing must be written to the absolute path")
	}
}
