package transport

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// StreamResult is the transport-neutral streaming response; the canonical type
// lives in the pkg/transport port (so editions depend on the abstraction, not
// this client). Aliased here so SendStream's signature reads naturally.
type StreamResult = port.StreamResult

// streamErrSnippetMax bounds how much of a failed download's error body is read
// for the message. A binary endpoint that errors still returns a small textual
// error (JSON / plain text); we never buffer the whole thing.
const streamErrSnippetMax = 4 << 10

// defaultStreamHeaderTimeout bounds how long a download waits for the server to
// send response headers before failing — guarding against a server that accepts
// the connection but never responds. The body transfer itself is not time-capped
// (a large download may legitimately take a long time).
const defaultStreamHeaderTimeout = 30 * time.Second

// maxStreamRedirects caps how many redirects a download follows.
const maxStreamRedirects = 10

// SendStream executes a binary (ResponseKind: binary) spec and returns a live
// response body for the caller to stream to disk, rather than buffering and
// decoding it like Send. The caller MUST Close the returned StreamResult.Body,
// which also releases the underlying request. On a non-2xx it reads only a
// bounded error snippet, returns no body, and classifies the failure like Send.
//
// Retry mirrors Send but only covers the pre-body phase: an idempotent request
// whose attempt fails before a body is handed back (network error / 429) is
// retried with backoff; once the live body is returned there is nothing to retry.
// A dry-run sends nothing and returns just the Plan.
func (c *Client) SendStream(ctx context.Context, spec api.RequestSpec) (*StreamResult, *Plan, error) {
	base, epHeaders, err := c.resolveBase(spec)
	if err != nil {
		return nil, nil, err
	}

	res, plan, err := c.streamOnce(ctx, spec, base, epHeaders)
	if !c.DryRun && !c.NoRetry && spec.Idempotent {
		for n := 1; n <= maxRetries && retryable(err); n++ {
			if werr := c.wait(ctx, backoff(n, err)); werr != nil {
				break
			}
			res, plan, err = c.streamOnce(ctx, spec, base, epHeaders)
		}
	}
	// Same post-outcome auth handling as Send: a login identity may remap an
	// expired-session failure (e.g. to login_required) and clear the stale session.
	// On an auth failure res is already nil (no body was handed back).
	return res, plan, c.remapAuthError(spec, err)
}

// streamOnce performs exactly one streaming send: it builds the request (shared
// with sendOnce via prepareRequest), sends it through a dedicated no-timeout
// client so a long healthy download is not cut off mid-stream, and — on success
// — returns the live body wrapped so Close cancels the request. It carries no
// retry logic of its own.
func (c *Client) streamOnce(ctx context.Context, spec api.RequestSpec, base string, epHeaders map[string]string) (*StreamResult, *Plan, error) {
	// A download's lifetime is the body's lifetime, not a fixed timeout, so tie
	// cancellation to the request context and cancel it when the body is closed
	// (or when this function bails before returning the body).
	streamCtx, cancel := context.WithCancel(ctx)

	plan, httpReq, err := c.prepareRequest(streamCtx, spec, base, epHeaders)
	if err != nil {
		cancel()
		return nil, plan, err
	}
	if httpReq == nil { // dry-run: nothing to send
		cancel()
		return nil, plan, nil
	}

	resp, err := c.streamClient(spec).Do(httpReq)
	if err != nil {
		cancel()
		return nil, plan, errs.Network("request failed: %v", err).WithCause(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The close error is discarded deliberately: this path already returns the
		// classified failure, and nothing useful remains to do about a body that
		// would not close.
		defer func() { _ = resp.Body.Close() }()
		defer cancel()
		return nil, plan, classifyStreamError(resp)
	}

	// A binary op is an explicit author declaration that this endpoint returns a
	// file, so a 2xx body is streamed to disk verbatim — we do not inspect it or
	// second-guess the declaration. Genuine failures come back as a non-2xx status
	// (handled above); the buffered JSON path's "HTTP 200 + business code" check
	// deliberately does NOT apply here (it cannot tell a JSON export from an error
	// envelope without false positives).
	res := &StreamResult{
		Body:               &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel},
		ContentType:        resp.Header.Get("Content-Type"),
		ContentDisposition: resp.Header.Get("Content-Disposition"),
		ContentLength:      resp.ContentLength,
	}
	return res, plan, nil
}

// streamClient is the HTTP client used for downloads. It disables the client-level
// timeout, since a large download can legitimately outlast the 15s ceiling that
// bounds a JSON request; overall lifetime is instead governed by the request
// context (which the caller, or a closed body, cancels). It clones the default
// transport and sets a ResponseHeaderTimeout so a server that connects but never
// responds fails instead of hanging forever, and installs a redirect policy that
// strips this request's credentials before following a redirect to another host
// (see redirectPolicy) so an API key is never sent to a host the request was not
// authorized for.
func (c *Client) streamClient(spec api.RequestSpec) *http.Client {
	// Build the transport (and its connection pool) once per Client; retries reuse
	// it. The per-request redirect policy is set on the lightweight client wrapper.
	c.streamTrOnce.Do(func() {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.ResponseHeaderTimeout = c.streamHeaderTimeout()
		c.streamTr = tr
	})
	reservedHeaders := append([]string{}, credential.ReservedHeaders(credential.Realm(spec.Identity))...)
	reservedHeaders = append(reservedHeaders, signer.ReservedHeaders(spec.Signed)...)
	pol := redirectPolicy(
		reservedHeaders,
		signer.ReservedParams(spec.Signed),
	)
	return &http.Client{Timeout: 0, Transport: c.streamTr, CheckRedirect: pol}
}

// redirectPolicy is the download client's net/http CheckRedirect. It follows
// redirects (so an endpoint that offloads to an object store / presigned URL
// works) but caps their number, refuses an https->http downgrade, and — on a
// redirect to a DIFFERENT host — strips this request's credential material (the
// identity's reserved headers such as X-MEXC-APIKEY, and the signer's reserved
// query params such as signature/timestamp) so credentials never reach a host
// the original request was not authorized for. A same-host redirect keeps them
// (same trust domain). reservedHeaders/reservedParams are empty for an
// unauthenticated/unsigned download, making this a no-op there.
func redirectPolicy(reservedHeaders, reservedParams []string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxStreamRedirects {
			return errs.Network("download stopped after %d redirects", maxStreamRedirects)
		}
		origin := via[0].URL
		if origin.Scheme == "https" && req.URL.Scheme == "http" {
			return errs.Network("refusing an https->http downgrade redirect during download (to %s)", req.URL.Host)
		}
		if strings.EqualFold(req.URL.Host, origin.Host) {
			return nil // same host: same trust domain, keep credentials
		}
		for _, h := range reservedHeaders {
			req.Header.Del(h)
		}
		if len(reservedParams) > 0 {
			q := req.URL.Query()
			for _, p := range reservedParams {
				q.Del(p)
			}
			req.URL.RawQuery = q.Encode()
		}
		return nil
	}
}

// streamHeaderTimeout is how long a download waits for response headers before
// giving up. It defaults to defaultStreamHeaderTimeout; tests pin it low.
func (c *Client) streamHeaderTimeout() time.Duration {
	if c.streamHeaderTO > 0 {
		return c.streamHeaderTO
	}
	return defaultStreamHeaderTimeout
}

// classifyStreamError turns a non-2xx streaming response into the same shared
// outcomes as Send's classify — 429 (retryable throttle), 418 (non-retryable IP
// ban), otherwise an upstream error — but reads only a bounded snippet of the
// error body instead of buffering it whole.
func classifyStreamError(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		after := parseRetryAfter(resp.Header.Get("Retry-After"))
		return errs.API(resp.StatusCode, errs.SubtypeRateLimited, "throttled by upstream (429)").
			WithRetryAfter(after).
			WithHint("back off and retry after %s", after)
	}
	if resp.StatusCode == 418 {
		after := parseRetryAfter(resp.Header.Get("Retry-After"))
		return errs.API(resp.StatusCode, errs.SubtypeIPBanned, "IP banned by upstream (418)").
			WithRetryAfter(after).
			WithHint("the IP is temporarily banned; stop sending and wait %s before any further request", after)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, streamErrSnippetMax))
	return errs.API(resp.StatusCode, errs.SubtypeUpstreamError,
		"download failed: %s: %s", resp.Status, bodySnippet(raw))
}

// cancelOnCloseBody wraps a response body so that closing it also cancels the
// request context, releasing the connection and any resources held by the
// in-flight request. Without this a caller that stops reading early would leak
// the request until the process exits.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
