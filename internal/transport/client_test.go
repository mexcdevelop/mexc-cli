package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// These tests exercise the retry/backoff/classification machinery that Send
// shares with sendOnce. They drive Send directly (via newSendClient, which pins
// BaseURLOverride to the test server so host resolution is skipped) and steer
// retry through spec.Idempotent / c.NoRetry, exactly as production code does.
// Body encoding, signing, identity injection, and dry-run previews are covered
// separately in send_test.go.

// spec is a small helper for the unsigned GET/POST requests these classification
// tests use; HostKey is cosmetic here (BaseURLOverride wins) but set for realism.
func spec(method, path string, idempotent bool) api.RequestSpec {
	return api.RequestSpec{Method: method, Path: path, HostKey: "x", Idempotent: idempotent}
}

func TestSend_DecodesSuccessBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","price":"100.5"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	if _, err := c.Send(context.Background(), spec("GET", "/api/v3/ticker/price", false), &dst); err != nil {
		t.Fatalf("err: %v", err)
	}
	m, ok := dst.(map[string]any)
	if !ok || m["price"] != "100.5" {
		t.Fatalf("unexpected body: %#v", dst)
	}
}

func TestSend_ArrayResponsePassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[[1,2,3],[4,5,6]]`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	var dst any
	if _, err := c.Send(context.Background(), spec("GET", "/api/v3/klines", false), &dst); err != nil {
		t.Fatalf("array response should pass through, got: %v", err)
	}
	if _, ok := dst.([]any); !ok {
		t.Fatalf("expected array, got %T", dst)
	}
}

func TestSend_RateLimitedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"msg":"too many"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	// Non-idempotent: a single 429 surfaces without any retry.
	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", false), nil)
	var e *errs.Error
	if err == nil || !errors.As(err, &e) || e.Type != errs.TypeAPI || e.Subtype != errs.SubtypeRateLimited {
		t.Fatalf("expected rate_limited api error, got %v", err)
	}
}

// countingRateLimiter returns a handler that fails with 429 for the first
// failFirst calls, then succeeds, recording the total number of calls.
func countingRateLimiter(failFirst int, calls *int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		*calls++
		if *calls <= failFirst {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":429,"msg":"too many"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}
}

func TestSend_RetriesIdempotentOnRateLimit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(countingRateLimiter(2, &calls))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil } // no real backoff in tests

	var dst any
	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", true), &dst)
	if err != nil {
		t.Fatalf("idempotent request should succeed after retries, got %v", err)
	}
	if calls != 3 { // 2 failures + 1 success
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestSend_DoesNotRetryNonIdempotent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(countingRateLimiter(99, &calls))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }

	// Idempotent false: a lost write must not be replayed.
	_, err := c.Send(context.Background(), spec("POST", "/api/v3/order", false), nil)
	if err == nil {
		t.Fatal("expected rate-limit error to surface")
	}
	if calls != 1 {
		t.Fatalf("non-idempotent request must not retry, got %d attempts", calls)
	}
}

func TestSend_NoRetryFlagDisablesRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(countingRateLimiter(99, &calls))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }
	c.NoRetry = true

	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", true), nil)
	if err == nil {
		t.Fatal("expected rate-limit error to surface")
	}
	if calls != 1 {
		t.Fatalf("--no-retry must disable retry, got %d attempts", calls)
	}
}

func TestSend_GivesUpAfterMaxRetries(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(countingRateLimiter(99, &calls)) // always fails
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", true), nil)
	if err == nil {
		t.Fatal("expected the final rate-limit error after exhausting retries")
	}
	if calls != 1+maxRetries { // first attempt + maxRetries
		t.Fatalf("expected %d attempts, got %d", 1+maxRetries, calls)
	}
}

// On ctx cancellation during backoff, Send must surface the real rate-limit error
// (with its Retry-After hint), not a generic "cancelled" error — otherwise the
// agent cannot tell it was throttled and would retry immediately.
func TestSend_CtxCancelDuringBackoffKeepsRealError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(countingRateLimiter(99, &calls)) // always 429
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	// Simulate the deadline elapsing during the first backoff.
	c.sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }

	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", true), nil)
	var e *errs.Error
	if err == nil || !errors.As(err, &e) {
		t.Fatalf("expected a structured error, got %v", err)
	}
	if e.Type != errs.TypeAPI || e.Subtype != errs.SubtypeRateLimited {
		t.Fatalf("expected the real rate_limited error to survive cancellation, got %s/%s", e.Type, e.Subtype)
	}
	if calls != 1 { // first attempt failed, backoff cancelled before a second send
		t.Fatalf("expected 1 attempt before cancellation, got %d", calls)
	}
}

// A 418 IP ban must NOT be retried (retrying prolongs the ban), and it carries
// the distinct ip_banned subtype plus the Retry-After window.
func TestSend_IPBanNotRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(418)
		_, _ = w.Write([]byte(`{"code":418,"msg":"banned"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", true), nil)
	var e *errs.Error
	if err == nil || !errors.As(err, &e) || e.Subtype != errs.SubtypeIPBanned {
		t.Fatalf("expected ip_banned error, got %v", err)
	}
	if e.RetryAfter != 120*time.Second {
		t.Fatalf("expected RetryAfter=120s, got %v", e.RetryAfter)
	}
	if calls != 1 {
		t.Fatalf("a 418 ban must not be retried, got %d attempts", calls)
	}
}

func TestSend_NonOKStatusBecomesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":700002,"msg":"invalid symbol"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", false), nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Type != errs.TypeAPI || e.Code != 700002 {
		t.Fatalf("expected api error code 700002, got %v", err)
	}
}

func TestSend_BusinessErrorInOKBody(t *testing.T) {
	// 2xx but a non-zero body code -> business error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":10007,"msg":"symbol not support"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	_, err := c.Send(context.Background(), spec("GET", "/api/v3/depth", false), nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != 10007 {
		t.Fatalf("expected business error code 10007, got %v", err)
	}
}

// The contract envelope carries its human-readable text in "message" (spot uses
// "msg"). The surfaced error must include that text, not fall back to a bare
// "upstream returned code N".
func TestSend_BusinessErrorContractMessageField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"code":2040,"message":"Order does not exist"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	_, err := c.Send(context.Background(), spec("POST", "/api/v1/private/order/change_limit_order", false), nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != 2040 {
		t.Fatalf("expected business error code 2040, got %v", err)
	}
	if !strings.Contains(e.Error(), "Order does not exist") {
		t.Fatalf("expected the contract message to surface, got %q", e.Error())
	}
}

// An HTML (or otherwise non-JSON) response — the classic symptom of a wrong
// env / host / path hitting a gateway, SSO, or 404 page — must surface as a
// clear api/upstream_error carrying the status code and a snippet, not a cryptic
// internal "failed to decode response" error.
func TestSend_NonJSONResponseIsClearError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<!DOCTYPE html>\n<html><body>404 Not Found</body></html>"))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	_, err := c.Send(context.Background(), spec("POST", "/session/auth", false), nil)
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *errs.Error", err)
	}
	if e.Type != errs.TypeAPI || e.Subtype != errs.SubtypeUpstreamError || e.Code != 404 {
		t.Fatalf("err = %+v, want api/upstream_error code 404", e)
	}
	if !strings.Contains(e.Message, "non-JSON") || e.Hint == "" {
		t.Fatalf("unhelpful error: msg=%q hint=%q", e.Message, e.Hint)
	}
}

func TestSend_RejectsOversizeResponseBody(t *testing.T) {
	// A response body larger than the client's cap must be rejected instead of
	// being buffered whole into memory (OOM guard). The handler writes far more
	// than the tiny cap the test pins.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", 4096) + `"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.maxRespBytes = 64 // far below the 4KB body the server sends
	var dst any
	_, err := c.Send(context.Background(), spec("GET", "/api/v3/thing", false), &dst)
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want *errs.Error", err)
	}
	if !strings.Contains(e.Message, "too large") {
		t.Fatalf("expected an oversize-body error, got: %v", err)
	}
}

func TestSend_AtLimitBodyStillDecodes(t *testing.T) {
	// A body at or under the cap must decode normally: the guard bounds memory,
	// it must not truncate a legitimate response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"price":"100.5"}`))
	}))
	defer srv.Close()

	c := newSendClient(config.Config{}, srv.URL)
	c.maxRespBytes = 1024
	var dst any
	if _, err := c.Send(context.Background(), spec("GET", "/api/v3/ticker/price", false), &dst); err != nil {
		t.Fatalf("body under cap must decode, got: %v", err)
	}
	if m, ok := dst.(map[string]any); !ok || m["price"] != "100.5" {
		t.Fatalf("unexpected body: %#v", dst)
	}
}
