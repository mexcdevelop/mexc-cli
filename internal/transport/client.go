// Package transport is the single exit point for all outbound API requests.
//
// The thin wrappers in api/ do not send HTTP themselves; sending
// requests, signing, rate limiting, and error classification are all funneled
// here. The behavior:
//   - parameter cleaning: empty values are stripped before they take part in signing or the URL
//   - signed requests automatically inject timestamp + recvWindow and attach a signature
//   - rate limiting: 10/s for normal requests, 20/s when the path contains /order
//   - error classification: 429/418 -> rate limited; non-2xx / body code != 0 -> api error
package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mexcdevelop/mexc-cli/internal/ratelimit"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// signaturePlaceholder is the placeholder for the signature in dry-run output.
// A dry-run never computes a real signature, to avoid leaking a replayable
// signed request into stdout / the agent context / logs.
const signaturePlaceholder = "DRY_RUN_NOT_SIGNED"

// Client holds the configuration, the HTTP client, and (optionally) the shared
// rate limiter.
type Client struct {
	cfg config.Config
	hc  *http.Client
	// Limiter is the client-side throttle, or nil for none. It is a process-wide
	// singleton (see NewSharedLimiter), assigned to every Client the invocation
	// builds — a Client is per-request, but the rate budget belongs to the process.
	Limiter *ratelimit.Limiter
	// DryRun, when true, sends no request; Send returns a *Plan so the caller can assert the request structure.
	DryRun bool
	// NoRetry disables automatic retry of transient failures (rate limit / network
	// error) on idempotent requests. Retry is on by default; the --no-retry flag
	// (or a caller that wants strict single-shot semantics) turns it off.
	NoRetry bool
	// Env selects which deployment hosts resolve to (used by Send). Zero = prod.
	Env hosts.Env
	// Identities answers which credential provider serves an identity.
	//
	// Nil means the process-wide registry, which is what every face that speaks as
	// ONE account wants — so a zero-value Client behaves exactly as it did before
	// this field existed. A long-lived server face that serves many callers sets a
	// per-request Lookup here (see credential.Binder), which is how two concurrent
	// requests resolve two different accounts through this same code path.
	//
	// Read it through identities(), never directly: the accessor is what keeps the
	// nil case meaning "the registry" in every branch rather than in most of them.
	Identities credential.Lookup
	// BaseURLOverride, when set (from --base-url), pins the full base URL for
	// Send and skips host-table resolution. Empty means resolve via pkg/hosts.
	BaseURLOverride string
	// nowMillis injects the current millisecond timestamp, to ease testing; when nil, time.Now is used.
	nowMillis func() int64
	// sleep injects the backoff wait, to ease testing; when nil, a real
	// timer-vs-ctx select is used. It must honor ctx (return ctx.Err() if the
	// context ends first) so the cancellation branch stays exercised in tests.
	sleep func(context.Context, time.Duration) error
	// maxRespBytes caps how many bytes of a response body are buffered before the
	// request is rejected as oversize. It guards against a malformed or hostile
	// upstream returning a huge body and driving the process out of memory. Zero
	// means the default (defaultMaxRespBytes); tests pin it low.
	maxRespBytes int64
	// streamHeaderTO overrides the download response-header timeout. Zero means
	// the default (defaultStreamHeaderTimeout); tests pin it low.
	streamHeaderTO time.Duration
	// streamTr is the download transport, built once and reused across download
	// attempts so retries share one connection pool instead of allocating a fresh
	// transport per attempt. streamTrOnce guards its lazy construction.
	streamTr     *http.Transport
	streamTrOnce sync.Once
	// Advisories remembers which host advisories this invocation has already stated.
	// Shared, because a Client is built per request while the advice belongs to the
	// invocation; nil states every advisory every time.
	Advisories *HostAdvisories
	// BuildVersionHeader names the header carrying pkg/buildinfo.Version on every
	// request; empty means none. Only the name arrives here — the value is a fact
	// about this binary, so a caller cannot claim a version it was not stamped with.
	BuildVersionHeader string
}

// HostAdvisories keeps one invocation from repeating the same host advisory.
//
// A single command can send more than once — an operation's own request plus the
// write-confirmation record — and the same advice printed twice reads as two
// separate problems. Safe for concurrent use: the MCP face serves tool calls in
// parallel.
type HostAdvisories struct {
	mu   sync.Mutex
	seen map[string]bool
}

// firstTime reports whether key has not been stated yet, and marks it stated. A nil
// receiver always reports true, so a Client built without shared state still warns.
func (h *HostAdvisories) firstTime(key string) bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen[key] {
		return false
	}
	if h.seen == nil {
		h.seen = map[string]bool{}
	}
	h.seen[key] = true
	return true
}

// defaultMaxRespBytes bounds the buffered JSON response body. MEXC API responses
// are small JSON documents, so 16 MiB is generous headroom while still capping a
// runaway body. A binary/download operation streams instead and never buffers,
// so this ceiling does not constrain legitimate large payloads.
const defaultMaxRespBytes = 16 << 20

// NewSharedLimiter builds the one rate limiter an invocation shares across every
// Client it constructs, from the product's policy.
//
// ONE limiter per invocation, assigned to every Client. A fresh limiter per Client
// would reset the bucket on every call — and in the long-lived server face, where
// each tool call builds a new Client, that makes the limit a no-op and invites a
// 429 and then an IP ban. The invocation-scoped closure that holds it lives in the
// composition root; this function only builds it.
//
// NIL POLICY MEANS THE DEFAULT, not "no throttle". The two must not share a
// spelling: one is a decision and the other is an omission, and the omission is
// the one that earns the ban. A product that genuinely wants no ceiling says
// Unlimited, and then this returns nil, which the send path reads as no throttle.
//
// Why this is a plain function over an invocation-scoped value rather than a
// package-level singleton with an atomic swap: a package var would be process
// state, which forces atomics not for the real program (assembly is
// single-threaded, long before any request) but so that a downstream test
// assembling in parallel does not trip the race detector inside this package.
// Scoping the limiter to the invocation removes that problem instead of guarding
// it — parallel assemblies simply do not share a limiter.
func NewSharedLimiter(p *port.RateLimitPolicy) *ratelimit.Limiter {
	if p == nil {
		p = port.DefaultRateLimit
	}
	if p.Unlimited {
		return nil
	}
	return ratelimit.New(p.Burst, p.RequestsPerSecond, p.MaxWait)
}

// New constructs a client. The caller assigns the shared limiter (see
// NewSharedLimiter) after construction; a Client with a nil Limiter does no
// client-side throttling.
func New(cfg config.Config) *Client {
	return &Client{
		cfg:          cfg,
		hc:           &http.Client{Timeout: 15 * time.Second},
		maxRespBytes: defaultMaxRespBytes,
	}
}

// identities is the nil-safe accessor for Identities. Every read of the credential
// lookup goes through it, so the zero value stays exactly the previous behaviour.
//
// ── Scope of the Lookup wiring, and what is deliberately left reading the
// process registry. This matters because the omissions are correct today and
// would become wrong silently, so they are named here rather than left to be
// rediscovered. The list is meant to be COMPLETE; if you find a call site that
// is not on it, that is a bug in this comment, not a call site that was judged.
//
//   - internal/cli's write-credential precheck. It runs ON THE REQUEST PATH, reads
//     the registry and actually calls Resolve — so it is the heaviest omission
//     here, not stream.go. It is left alone because it disappears wholesale on a
//     face with nobody to confirm a write: the gate it belongs to is skipped, and
//     with it the precheck. A face that keeps the gate is by definition serving one
//     account, where the registry is the right answer. Do not wire it up without
//     first checking that the gate still runs.
//   - stream.go's reserved-header query (the download path). This Client already
//     has a Lookup bound; that call site simply does not ask it. Changing it is
//     one line, but the MCP face excludes binary/streaming operations, so there is
//     no verifiable benefit today and any change would have to be re-argued
//     against local download behaviour.
//   - internal/cli's WSCollect path. It takes a provider and calls Resolve itself,
//     without going through a Client at all, so there is no identities() to ask —
//     wiring it up means rewriting that section first.
//   - internal/cli's mount-time queries: the reserved-header lookup and the
//     "is a provider registered for this identity" check. Both run while the
//     command tree is being built — there is no request and no Client in
//     existence yet, and what they ask about is the registration itself.
//   - credential.CarriesGrants / ResolvesHost, used by assembly-time validation.
//     Those ask about the TYPE of provider registered for a realm ("can this
//     identity carry a grant map?"), which is a property of the registration, not
//     of any one request.
//   - an edition's own diagnostic command that reads the registry directly. A
//     single-account face has nothing else to read.
//
// ⚠️ Whoever adds WebSocket support to a multi-caller face must wire that path up
// FIRST. Otherwise those calls go out with the PROCESS identity, and the failure
// is invisible: on such a face the registered provider refuses, so Resolve fails
// and it looks like fail-closed working as intended — right up until somebody
// registers a provider with real material to make WS work, at which point one
// account starts serving every caller and no line of code looks wrong.
func (c *Client) identities() credential.Lookup {
	if c.Identities == nil {
		return credential.Registry()
	}
	return c.Identities
}

// Plan is the final structure of a request; in dry-run mode it is returned
// instead of actually sending.
// Plan / PlanFile are the transport-neutral request description; the canonical
// definitions live in the pkg/transport port (so editions depend on the
// abstraction, not this client). Aliased here so all internal usages and Send's
// return type stay unchanged.
type (
	Plan     = port.Plan
	PlanFile = port.PlanFile
)

// IsDryRun reports whether this client only previews (never sends), satisfying
// the pkg/transport.Sender port. It mirrors the DryRun field as a method so
// *Client structurally satisfies Sender (which cannot expose a field).
func (c *Client) IsDryRun() bool { return c.DryRun }

func (c *Client) now() int64 {
	if c.nowMillis != nil {
		return c.nowMillis()
	}
	return time.Now().UnixMilli()
}

// maxRetries is the number of additional attempts after the first for a
// retryable failure on an idempotent request. Total attempts = 1 + maxRetries.
const maxRetries = 3

// wait blocks for d or until ctx ends, returning ctx.Err() if the context ended
// first. The sleep is injectable so tests can advance backoff without real time
// while still exercising the ctx-cancellation branch.
func (c *Client) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// classify turns an HTTP response into the shared success/error outcome used by
// Send/sendOnce. 429 -> rate limited (retryable); 418 -> IP banned (not retryable);
// any other non-2xx -> upstream error; a 2xx body carrying a non-zero code -> a
// business error; a clean 2xx -> decode into dst.
func (c *Client) classify(resp *http.Response, plan *Plan, dst any) (*Plan, error) {
	// 429 (throttled) and 418 (IP auto-banned) are different animals. A 429 is a
	// short, recoverable throttle worth retrying after a brief backoff. A 418
	// means the IP is already banned — often for minutes or hours — and the worst
	// thing to do is keep sending: more requests extend the ban. So 418 is given
	// a distinct, non-retryable subtype; only its Retry-After is surfaced so the
	// caller knows how long to stay away.
	if resp.StatusCode == http.StatusTooManyRequests {
		after := parseRetryAfter(resp.Header.Get("Retry-After"))
		return plan, errs.API(resp.StatusCode, errs.SubtypeRateLimited,
			"throttled by upstream (429)").
			WithRetryAfter(after).
			WithHint("back off and retry after %s", after)
	}
	if resp.StatusCode == 418 {
		after := parseRetryAfter(resp.Header.Get("Retry-After"))
		return plan, errs.API(resp.StatusCode, errs.SubtypeIPBanned,
			"IP banned by upstream (418)").
			WithRetryAfter(after).
			WithHint("the IP is temporarily banned; stop sending and wait %s before any further request", after)
	}

	limit := c.maxRespBytes
	if limit <= 0 {
		limit = defaultMaxRespBytes
	}
	// Read at most limit+1 bytes: if the extra byte materializes the body ran
	// past the cap, so reject it rather than buffering an unbounded response.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return plan, errs.Network("failed to read response body: %v", err).WithCause(err)
	}
	if int64(len(raw)) > limit {
		return plan, errs.API(resp.StatusCode, errs.SubtypeUpstreamError,
			"response body too large (exceeds %d bytes)", limit).
			WithHint("a JSON operation buffers its whole response; a large binary payload should be a streaming download operation instead")
	}

	// A non-JSON body — an HTML error/gateway/SSO/404 page, a plain-text proxy
	// notice, or an empty body — cannot be decoded. Surface it as a clear upstream
	// error carrying the status code and a short snippet, instead of a cryptic
	// JSON-decode failure, so a wrong env / host / path (which typically yields an
	// HTML page rather than the API's JSON) is immediately diagnosable.
	if !json.Valid(bytes.TrimSpace(raw)) {
		return plan, errs.API(resp.StatusCode, errs.SubtypeUpstreamError,
			"upstream returned a non-JSON %s response: %s", resp.Status, bodySnippet(raw)).
			WithHint("verify the host and the request path — a gateway, SSO, or 404 page returns HTML, not the API's JSON")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code, msg := extractCodeMsg(raw, resp.StatusCode, resp.Status)
		return plan, errs.API(code, errs.SubtypeUpstreamError, "%s", msg)
	}
	// Even on a 2xx, MEXC may still return a non-zero code in the body to signal a business failure.
	if code, msg, ok := businessError(raw); ok {
		return plan, errs.API(code, errs.SubtypeUpstreamError, "%s", msg)
	}

	if dst != nil {
		if err := json.Unmarshal(raw, dst); err != nil {
			return plan, errs.Internal("failed to unmarshal response: %v", err).WithCause(err)
		}
	}
	return plan, nil
}

// retryable reports whether an error is a transient failure worth retrying: a
// network/transport error, or a 429 throttle (SubtypeRateLimited). A 418 IP ban
// (SubtypeIPBanned) is deliberately NOT retryable — retrying during a ban only
// prolongs it. Business, validation, and internal errors are never retried:
// they will not succeed on a repeat, and (for writes) a repeat could be unsafe.
func retryable(err error) bool {
	var e *errs.Error
	if !errors.As(err, &e) {
		return false
	}
	if e.Type == errs.TypeNetwork {
		return true
	}
	return e.Type == errs.TypeAPI && e.Subtype == errs.SubtypeRateLimited
}

// backoff returns the wait before retry attempt n (1-based): exponential
// 1s, 2s, 4s, but never less than the upstream's Retry-After when it gave one.
// Honoring Retry-After matters because a fixed 1/2/4s schedule can under-wait a
// throttle the server explicitly asked us to wait longer on.
func backoff(n int, err error) time.Duration {
	exp := time.Duration(1<<(n-1)) * time.Second
	var e *errs.Error
	if errors.As(err, &e) && e.RetryAfter > exp {
		return e.RetryAfter
	}
	return exp
}

// parseRetryAfter reads a Retry-After header value (delta-seconds form, which is
// what MEXC sends) into a duration. An empty or malformed value falls back to a
// conservative 10s, matching the prior default.
func parseRetryAfter(v string) time.Duration {
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return 10 * time.Second
}

// bodySnippet renders a short, single-line preview of a response body for an
// error message: capped, whitespace-collapsed, so an HTML page or proxy notice
// is legible in one line without dumping the whole body.
func bodySnippet(b []byte) string {
	if len(b) > 1024 {
		b = b[:1024]
	}
	s := strings.Join(strings.Fields(string(b)), " ")
	const max = 200
	if len(s) > max {
		s = s[:max] + "..."
	}
	if s == "" {
		return "(empty body)"
	}
	return s
}

func extractCodeMsg(raw json.RawMessage, status int, statusText string) (int, string) {
	var body struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err == nil {
		code := body.Code
		if code == 0 {
			code = status
		}
		msg := body.Msg
		if msg == "" {
			msg = body.Message
		}
		if msg == "" {
			msg = statusText
		}
		return code, msg
	}
	return status, statusText
}

// businessError detects the business error MEXC expresses through the code field
// in a 2xx response. A code of 0 / 200 is treated as success. A non-object
// response (such as an array) has no code and is passed through.
func businessError(raw json.RawMessage) (int, string, bool) {
	var body struct {
		Code    *int   `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return 0, "", false
	}
	if body.Code == nil || *body.Code == 0 || *body.Code == 200 {
		return 0, "", false
	}
	// Spot expresses the human-readable text in "msg"; the contract envelope uses
	// "message". Prefer whichever is present so the backend's error text surfaces
	// instead of a bare "upstream returned code N".
	msg := body.Msg
	if msg == "" {
		msg = body.Message
	}
	if msg == "" {
		msg = fmt.Sprintf("upstream returned code %d", *body.Code)
	}
	return *body.Code, msg, true
}
