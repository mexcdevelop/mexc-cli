package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/internal/ratelimit"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// newSendClient builds a client whose Send calls hit srvURL directly (bypassing
// host-table resolution) with a fixed timestamp so signatures are reproducible.
func newSendClient(cfg config.Config, srvURL string) *Client {
	c := New(cfg)
	c.BaseURLOverride = srvURL
	c.nowMillis = func() int64 { return 1700000000000 }
	return c
}

func TestSend_JSONBodyHeaderCookie(t *testing.T) {
	var gotBody, gotTrace, gotCookie, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotTrace = r.Header.Get("X-Trace")
		gotCookie = r.Header.Get("Cookie")
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	spec := api.RequestSpec{
		Method: "POST", Path: "/api/v3/thing",
		Header: map[string]string{"X-Trace": "abc123"},
		Cookie: map[string]string{"b": "2", "a": "1"},
		Body:   []byte(`{"hello":"world"}`),
	}
	plan, err := c.Send(context.Background(), spec, &dst)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if gotBody != `{"hello":"world"}` {
		t.Fatalf("body not echoed: %q", gotBody)
	}
	if gotTrace != "abc123" {
		t.Fatalf("custom header lost: %q", gotTrace)
	}
	if gotCookie != "a=1; b=2" { // sorted keys, joined with "; "
		t.Fatalf("cookie header wrong: %q", gotCookie)
	}
	if gotCT != "application/json" {
		t.Fatalf("content-type wrong: %q", gotCT)
	}
	if plan.BodyKind != "json" {
		t.Fatalf("plan body kind = %q, want json", plan.BodyKind)
	}
	if m, ok := dst.(map[string]any); !ok || m["ok"] != true {
		t.Fatalf("response not decoded into dst: %#v", dst)
	}
}

func TestSend_Form(t *testing.T) {
	var gotField, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotField = r.PostFormValue("symbol")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	spec := api.RequestSpec{
		Method: "POST", Path: "/api/v3/form",
		Form: map[string]string{"symbol": "BTCUSDT"},
	}
	plan, err := c.Send(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Fatalf("content-type wrong: %q", gotCT)
	}
	if gotField != "BTCUSDT" {
		t.Fatalf("form field lost: %q", gotField)
	}
	if plan.BodyKind != "form" {
		t.Fatalf("plan body kind = %q, want form", plan.BodyKind)
	}
}

func TestSend_MultipartSingleFile(t *testing.T) {
	var gotFileContent, gotField string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		gotField = r.FormValue("desc")
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("FormFile: %v", err)
		} else {
			b, _ := io.ReadAll(f)
			gotFileContent = string(b)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	spec := api.RequestSpec{
		Method: "POST", Path: "/api/v3/upload",
		Multipart: &api.MultipartBody{
			Fields: map[string]string{"desc": "a file"},
			Files: []api.FilePart{
				{Field: "file", Filename: "a.txt", Content: []byte("hello file")},
			},
		},
	}
	plan, err := c.Send(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if gotField != "a file" {
		t.Fatalf("multipart text field lost: %q", gotField)
	}
	if gotFileContent != "hello file" {
		t.Fatalf("multipart file content lost: %q", gotFileContent)
	}
	if plan.BodyKind != "multipart" {
		t.Fatalf("plan body kind = %q, want multipart", plan.BodyKind)
	}
	if len(plan.Files) != 1 || plan.Files[0].Field != "file" || plan.Files[0].Filename != "a.txt" || plan.Files[0].Size != len("hello file") {
		t.Fatalf("plan files wrong: %#v", plan.Files)
	}
}

func TestSend_MultipartFilePartContentType(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		if fhs := r.MultipartForm.File["file"]; len(fhs) == 1 {
			gotCT = fhs[0].Header.Get("Content-Type")
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	spec := api.RequestSpec{
		Method: "POST", Path: "/api/v3/upload",
		Multipart: &api.MultipartBody{
			Files: []api.FilePart{
				// A .png filename must yield image/png, not the stdlib
				// CreateFormFile default of application/octet-stream (some
				// backends reject octet-stream for image uploads).
				{Field: "file", Filename: "banner.png", Content: []byte("\x89PNG\r\n\x1a\n")},
			},
		},
	}
	if _, err := c.Send(context.Background(), spec, nil); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if gotCT != "image/png" {
		t.Fatalf("file part Content-Type = %q, want image/png", gotCT)
	}
}

func TestSend_MultipartMultipleFiles(t *testing.T) {
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		// Two files sent under the same field name "files".
		for _, fh := range r.MultipartForm.File["files"] {
			f, _ := fh.Open()
			b, _ := io.ReadAll(f)
			_ = f.Close()
			got[fh.Filename] = string(b)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	spec := api.RequestSpec{
		Method: "POST", Path: "/api/v3/upload",
		Multipart: &api.MultipartBody{
			Files: []api.FilePart{
				{Field: "files", Filename: "one.txt", Content: []byte("first")},
				{Field: "files", Filename: "two.txt", Content: []byte("second")},
			},
		},
	}
	plan, err := c.Send(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if got["one.txt"] != "first" || got["two.txt"] != "second" {
		t.Fatalf("multiple files not all received: %#v", got)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("plan should list 2 files, got %#v", plan.Files)
	}
}

func TestSend_SignedOpenAPI(t *testing.T) {
	var gotAPIKey, gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get(testAPIKeyHeader)
		gotSig = r.URL.Query().Get("signature")
		_, _ = w.Write([]byte(`{"balances":[]}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	spec := api.RequestSpec{
		Method:   "GET",
		Path:     "/api/v3/account",
		Query:    map[string]string{"symbol": "BTCUSDT"},
		Identity: identity.OpenAPI,
		Signed:   testQueryScheme,
	}
	plan, err := c.Send(context.Background(), spec, &dst)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if gotAPIKey != "k" {
		t.Fatalf("api key header missing/wrong: %q", gotAPIKey)
	}
	if gotSig == "" || gotSig == signaturePlaceholder {
		t.Fatalf("real signature missing on a live signed send: %q", gotSig)
	}
	if !plan.Signed {
		t.Fatalf("plan should report signed")
	}
	if _, ok := dst.(map[string]any); !ok {
		t.Fatalf("signed response not decoded: %#v", dst)
	}
}

// A signed request may carry a body: the two are independent, and the transport
// must deliver both. Assembly no longer refuses the pairing, so this is the path
// an operation declared in its final shape actually takes. The scheme here signs
// the query only, which is the point of the second assertion — the body reaches
// the server byte-for-byte, unread by signing and unaltered by it.
func TestSend_SignedWithJSONBody(t *testing.T) {
	const payload = `{"note":"hello"}`
	var gotSig, gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.URL.Query().Get("signature")
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	spec := api.RequestSpec{
		Method:   "POST",
		Path:     "/api/v3/order",
		Query:    map[string]string{"symbol": "BTCUSDT"},
		Body:     []byte(payload),
		Identity: identity.OpenAPI,
		Signed:   testQueryScheme,
	}
	plan, err := c.Send(context.Background(), spec, &dst)
	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if gotSig == "" || gotSig == signaturePlaceholder {
		t.Fatalf("real signature missing on a live signed send: %q", gotSig)
	}
	if gotBody != payload {
		t.Fatalf("body = %q, want it delivered unchanged as %q", gotBody, payload)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if !plan.Signed || plan.BodyKind != "json" {
		t.Fatalf("plan = {Signed:%v BodyKind:%q}, want {true json}", plan.Signed, plan.BodyKind)
	}
}

func TestSend_DryRunSignedNoCredentials(t *testing.T) {
	// A server that fails the test if it is ever hit: a dry-run must not send.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run must not send a request")
	}))
	defer srv.Close()

	// Empty credentials: a dry-run of a signed request must not require them.
	c := newSendClient(config.Config{}, srv.URL)
	c.DryRun = true
	spec := api.RequestSpec{
		Method:   "POST",
		Path:     "/api/v3/order",
		Query:    map[string]string{"symbol": "BTCUSDT"},
		Body:     []byte(`{"side":"BUY"}`),
		Identity: identity.OpenAPI,
		Signed:   testQueryScheme,
	}
	plan, err := c.Send(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("dry-run err: %v", err)
	}
	if !plan.Signed {
		t.Fatalf("dry-run plan should report signed")
	}
	if plan.BodyKind != "json" {
		t.Fatalf("dry-run plan body kind = %q, want json", plan.BodyKind)
	}
	// The signature is the placeholder, never a real, replayable signature.
	if !strings.Contains(plan.URL, "signature="+signaturePlaceholder) {
		t.Fatalf("dry-run signature should be the placeholder: %s", plan.URL)
	}
}

func TestSend_UnknownIdentity(t *testing.T) {
	c := newSendClient(config.Config{}, "http://dummy.invalid")
	spec := api.RequestSpec{
		Method:   "GET",
		Path:     "/api/v3/thing",
		Identity: api.Identity("no-such-realm"),
	}
	_, err := c.Send(context.Background(), spec, nil)
	if err == nil {
		t.Fatalf("expected an error for an unwired identity")
	}
	if !strings.Contains(err.Error(), "no credential provider") {
		t.Fatalf("wrong error: %v", err)
	}
}

// Every request carries it, signed or not: the transport sets it, not a signer.
func TestSend_CarriesTheBuildVersionHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-MEXC-CLI-BUILD-VERSION")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.BuildVersionHeader = "X-MEXC-CLI-BUILD-VERSION"
	var dst any
	if _, err := c.Send(context.Background(), api.RequestSpec{Method: "GET", Path: "/x"}, &dst); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if got != buildinfo.Version {
		t.Fatalf("build version header = %q, want %q", got, buildinfo.Version)
	}
}

// The zero value opts out, and must add nothing.
func TestSend_NoBuildVersionHeaderWhenUnnamed(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-MEXC-CLI-BUILD-VERSION")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	if _, err := c.Send(context.Background(), api.RequestSpec{Method: "GET", Path: "/x"}, &dst); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if got != "" {
		t.Fatalf("an unnamed header still reached the server as %q", got)
	}
}

// Lowest precedence of every header source.
func TestSend_BuildVersionHeaderLosesToARequestHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-MEXC-CLI-BUILD-VERSION")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.BuildVersionHeader = "X-MEXC-CLI-BUILD-VERSION"
	var dst any
	spec := api.RequestSpec{
		Method: "GET", Path: "/x",
		Header: map[string]string{"X-MEXC-CLI-BUILD-VERSION": "from-the-request"},
	}
	if _, err := c.Send(context.Background(), spec, &dst); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if got != "from-the-request" {
		t.Fatalf("header = %q, want the per-request value to win", got)
	}
}

// A Client with a limiter throttles: draining the burst forces the next Send to
// wait on refill. With a nil limiter (the default) there is no throttle at all.
func TestSend_LimiterThrottles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.Limiter = ratelimit.New(2, 2, 0) // burst 2, 2/s
	var dst any
	sp := api.RequestSpec{Method: "GET", Path: "/x"}
	for i := 0; i < 2; i++ {
		if _, err := c.Send(context.Background(), sp, &dst); err != nil {
			t.Fatalf("burst send %d: %v", i, err)
		}
	}
	start := time.Now()
	if _, err := c.Send(context.Background(), sp, &dst); err != nil {
		t.Fatalf("third send: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("at 2/s the third send should wait ~500ms, only waited %v", elapsed)
	}
}

// ErrWaitTooLong surfaces as a client-side failed_precondition, not the upstream
// 429 (rate_limited), so it is not auto-retried.
func TestSend_LimiterBudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.Limiter = ratelimit.New(1, 1, 10*time.Millisecond) // 1/s, tiny budget
	var dst any
	sp := api.RequestSpec{Method: "GET", Path: "/x"}
	if _, err := c.Send(context.Background(), sp, &dst); err != nil {
		t.Fatalf("first send drains the token: %v", err)
	}
	_, err := c.Send(context.Background(), sp, &dst)
	if err == nil {
		t.Fatal("expected the second send to be refused by the local throttle")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeClientRateLimited {
		t.Fatalf("want failed_precondition (client-side), got %v", err)
	}
}
