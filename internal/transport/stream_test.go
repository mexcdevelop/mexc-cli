package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

func binarySpec(path string) api.RequestSpec {
	return api.RequestSpec{Method: "GET", Path: path, HostKey: "x", ResponseKind: api.ResponseBinary, Idempotent: true}
}

func TestSendStream_StreamsBodyAndMetadata(t *testing.T) {
	payload := []byte("\x00\x01binary-file-body\xff\xfe")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="report.bin"`)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	res, plan, err := c.SendStream(context.Background(), binarySpec("/files/abc/download"))
	if err != nil {
		t.Fatalf("SendStream: %v", err)
	}
	if plan == nil || plan.Method != "GET" {
		t.Fatalf("plan not returned: %#v", plan)
	}
	if res == nil {
		t.Fatal("nil StreamResult")
	}
	defer func() { _ = res.Body.Close() }()
	if res.ContentType != "application/octet-stream" {
		t.Fatalf("content type: %q", res.ContentType)
	}
	if res.ContentDisposition != `attachment; filename="report.bin"` {
		t.Fatalf("content disposition: %q", res.ContentDisposition)
	}
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("body mismatch: got %q want %q", got, payload)
	}
}

func TestSendStream_HTTPErrorIsClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("file not found"))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	res, _, err := c.SendStream(context.Background(), binarySpec("/files/missing/download"))
	if res != nil {
		t.Fatal("no StreamResult expected on error")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *errs.Error", err)
	}
	if e.Type != errs.TypeAPI || e.Code != 404 {
		t.Fatalf("err = %+v, want api error code 404", e)
	}
}

func TestSendStream_2xxBodyStreamedVerbatim(t *testing.T) {
	// ResponseKind: binary is the author's explicit declaration that this endpoint
	// returns a file, so a 2xx body is streamed verbatim without inspection — even
	// a JSON body (which could be a legitimate JSON export). Genuine failures come
	// back as a non-2xx status, handled separately. We deliberately do NOT apply
	// the buffered path's "HTTP 200 + business code" heuristic here.
	payload := `{"code":700003,"msg":"looks like an error but we do not second-guess"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	res, _, err := c.SendStream(context.Background(), binarySpec("/files/x/download"))
	if err != nil {
		t.Fatalf("a 2xx body must stream regardless of content, got %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if got, _ := io.ReadAll(res.Body); string(got) != payload {
		t.Fatalf("body must stream verbatim: got %q", got)
	}
}

func TestSendStream_HeaderTimeout(t *testing.T) {
	// A server that accepts the connection but stalls before sending response
	// headers must not hang the download forever — the header timeout fails it.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release // never write headers until the test releases
	}))
	defer srv.Close()
	defer close(release)

	c := newSendClient(config.Config{}, srv.URL)
	c.streamHeaderTO = 50 * time.Millisecond
	c.NoRetry = true
	res, _, err := c.SendStream(context.Background(), binarySpec("/files/x/download"))
	if err == nil {
		t.Fatal("expected a timeout error when the server stalls before headers")
	}
	if res != nil {
		t.Fatal("no body on a stalled request")
	}
}

func TestSendStream_RetriesIdempotentOnRateLimit(t *testing.T) {
	// A 429 before any body is handed back is retryable for an idempotent op: the
	// stream should retry with backoff and eventually deliver the file.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("delivered"))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil } // skip real backoff
	res, _, err := c.SendStream(context.Background(), binarySpec("/files/x/download"))
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if got, _ := io.ReadAll(res.Body); string(got) != "delivered" {
		t.Fatalf("body = %q", got)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Fatalf("expected 3 attempts (2 x 429 + success), got %d", n)
	}
}

func TestSendStream_NoRetryFlagDisablesRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.NoRetry = true
	if _, _, err := c.SendStream(context.Background(), binarySpec("/files/x/download")); err == nil {
		t.Fatal("expected a rate-limit error")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("--no-retry must send exactly once, got %d attempts", n)
	}
}

// fakeReadCloser observes Close so the cancel-on-close contract can be asserted.
type fakeReadCloser struct{ closed *bool }

func (f fakeReadCloser) Read([]byte) (int, error) { return 0, io.EOF }
func (f fakeReadCloser) Close() error             { *f.closed = true; return nil }

func TestCancelOnCloseBody_CancelsRequestOnClose(t *testing.T) {
	// Closing the streamed body must both close the underlying body and cancel the
	// request context, so a caller that stops early does not leak the request.
	ctx, cancel := context.WithCancel(context.Background())
	closed := false
	b := &cancelOnCloseBody{ReadCloser: fakeReadCloser{closed: &closed}, cancel: cancel}

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !closed {
		t.Fatal("underlying body Close was not called")
	}
	if ctx.Err() == nil {
		t.Fatal("request context was not cancelled on body Close")
	}
}

func TestRedirectPolicy_StripsCredentialsCrossHost(t *testing.T) {
	pol := redirectPolicy([]string{"X-MEXC-APIKEY"}, []string{"signature", "timestamp"})
	orig, _ := http.NewRequest("GET", "https://origin.example/export", nil)
	redir, _ := http.NewRequest("GET", "https://cdn.other.example/file?signature=abc&timestamp=1&keep=2", nil)
	redir.Header.Set("X-MEXC-APIKEY", "secret")

	if err := pol(redir, []*http.Request{orig}); err != nil {
		t.Fatalf("a cross-host redirect should be followed (with creds stripped): %v", err)
	}
	if redir.Header.Get("X-MEXC-APIKEY") != "" {
		t.Fatal("the API-key header must be stripped on a cross-host redirect")
	}
	if redir.URL.Query().Get("signature") != "" || redir.URL.Query().Get("timestamp") != "" {
		t.Fatal("signing params must be stripped on a cross-host redirect")
	}
	if redir.URL.Query().Get("keep") != "2" {
		t.Fatal("non-reserved query params must be preserved")
	}
}

func TestRedirectPolicy_KeepsCredentialsSameHost(t *testing.T) {
	pol := redirectPolicy([]string{"X-MEXC-APIKEY"}, nil)
	orig, _ := http.NewRequest("GET", "https://origin.example/export", nil)
	redir, _ := http.NewRequest("GET", "https://origin.example/export/v2", nil)
	redir.Header.Set("X-MEXC-APIKEY", "secret")

	if err := pol(redir, []*http.Request{orig}); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	if redir.Header.Get("X-MEXC-APIKEY") != "secret" {
		t.Fatal("a same-host redirect must keep credentials (same trust domain)")
	}
}

func TestRedirectPolicy_RefusesDowngradeAndCaps(t *testing.T) {
	pol := redirectPolicy(nil, nil)
	secure, _ := http.NewRequest("GET", "https://o/x", nil)
	insecure, _ := http.NewRequest("GET", "http://o/x", nil)
	if err := pol(insecure, []*http.Request{secure}); err == nil {
		t.Fatal("an https->http downgrade redirect must be refused")
	}

	via := make([]*http.Request, maxStreamRedirects)
	for i := range via {
		via[i] = secure
	}
	next, _ := http.NewRequest("GET", "https://o/y", nil)
	if err := pol(next, via); err == nil {
		t.Fatal("must stop after too many redirects")
	}
}

func TestSendStream_FollowsSameHostRedirect(t *testing.T) {
	// The redirect policy is wired into the download client and does not break a
	// normal same-host redirect (e.g. /a -> /b on the same server).
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("redirected-body"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	res, _, err := c.SendStream(context.Background(), binarySpec("/a"))
	if err != nil {
		t.Fatalf("same-host redirect should be followed: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if got, _ := io.ReadAll(res.Body); string(got) != "redirected-body" {
		t.Fatalf("body after redirect = %q", got)
	}
}

func TestSendStream_DryRunSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("dry-run must not send a request")
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.DryRun = true
	res, plan, err := c.SendStream(context.Background(), binarySpec("/files/abc/download"))
	if err != nil {
		t.Fatalf("dry-run err: %v", err)
	}
	if res != nil {
		t.Fatal("dry-run must not return a body")
	}
	if plan == nil {
		t.Fatal("dry-run must return a plan")
	}
}
