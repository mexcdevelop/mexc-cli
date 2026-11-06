package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mexcdevelop/mexc-cli/internal/ratelimit"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/caller"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// Send executes a transport-neutral api.RequestSpec and decodes the response
// JSON into dst. It is the RequestSpec-consuming counterpart of Do: it resolves
// the host (via c.BaseURLOverride or pkg/hosts), the identity (via
// pkg/credential), and the signing scheme (via pkg/signer), then encodes the
// JSON/form/multipart body and sends the request through the same rate-limit,
// retry, and response-classification machinery as Do.
//
// In DryRun mode nothing is sent, a *Plan is returned for assertion, and neither
// credentials nor a real signature are required (a dry-run of a signed request
// carries the signaturePlaceholder, never a replayable signature).
//
// On a transient failure (rate limit / network error) of an idempotent request,
// Send retries with exponential backoff, honoring an upstream Retry-After, up to
// maxRetries extra attempts. Each attempt re-signs from scratch over a fresh copy
// of the query (the signature embeds a fresh timestamp), so a retry is a fully
// fresh request, never a replay of a stale one.
func (c *Client) Send(ctx context.Context, spec api.RequestSpec, dst any) (*Plan, error) {
	// Resolve the base URL once; a retry reuses it (only signing/timestamp is
	// per-attempt).
	base, epHeaders, err := c.resolveBase(spec)
	if err != nil {
		return nil, err
	}

	plan, err := c.sendOnce(ctx, spec, base, epHeaders, dst)
	// Retry only an idempotent request on a transient failure, and never in
	// dry-run (nothing was sent) or when retry is opted out. A non-idempotent
	// request is sent exactly once — but it must still flow through the
	// auth-failure hook below, so the retry decision is a guarded branch rather
	// than an early return.
	if !c.DryRun && !c.NoRetry && spec.Idempotent {
		for n := 1; n <= maxRetries && retryable(err); n++ {
			// If the context ends during the backoff, keep the real failure (the
			// rate-limit error with its Retry-After hint), not a generic "cancelled".
			if werr := c.wait(ctx, backoff(n, err)); werr != nil {
				break
			}
			plan, err = c.sendOnce(ctx, spec, base, epHeaders, dst)
		}
	}

	return plan, c.remapAuthError(spec, err)
}

// remapAuthError gives a login identity the chance to recognize its own auth
// failure (session expired/invalid), clear the stored session, and remap the
// error (e.g. to login_required) once a request has reached its final outcome.
// It returns err unchanged when there is nothing to remap (no error, dry-run, no
// identity, or the provider is not an AuthInvalidator). Shared by Send and
// SendStream so a streaming download gets the same session handling as a JSON
// call. It runs for non-idempotent requests too — most login-token writes are
// non-idempotent — so callers invoke it after the retry branch, not inside it.
func (c *Client) remapAuthError(spec api.RequestSpec, err error) error {
	if err == nil || c.DryRun || spec.Identity == "" || spec.Identity == api.IdentityNone {
		return err
	}
	p, ok := c.identities().Provider(credential.Realm(spec.Identity))
	if !ok {
		return err
	}
	inv, ok := p.(credential.AuthInvalidator)
	if !ok {
		return err
	}
	if remapped := inv.OnRequestError(&spec, c.Env, err); remapped != nil {
		return remapped
	}
	return err
}

// resolveBase determines the base URL (and any env headers) for spec, enforcing
// the host-override policy:
//   - An identity that owns its host (implements credential.HostResolver)
//     resolves it from its own store; a --base-url override is REFUSED, so a
//     credentialed request can never be redirected to an unvetted host.
//   - A no-override host key (hosts.IsNoOverride) also refuses a --base-url
//     override (and hosts.Resolve already skips its MEXC_<KEY>_BASE_URL override).
//     That covers a host whose requests must not be redirected even though they
//     carry no identity of their own — an endpoint that authenticates, say.
//   - Every other key keeps the full override chain: --base-url > MEXC_* > builtin.
func (c *Client) resolveBase(spec api.RequestSpec) (string, map[string]string, error) {
	if spec.Identity != "" && spec.Identity != api.IdentityNone {
		if p, ok := c.identities().Provider(credential.Realm(spec.Identity)); ok {
			if hr, ok := p.(credential.HostResolver); ok {
				if c.BaseURLOverride != "" {
					return "", nil, errs.Validation(errs.SubtypeInvalidArgument,
						"the %q identity's host cannot be overridden with --base-url", spec.Identity).
						WithHint("its host is recorded per env; manage it with this identity's host-book command")
				}
				// A prod dry-run must not print the real (security-sensitive) host in
				// the plan — show the synthetic placeholder regardless of whether one
				// is recorded. Non-prod dry-runs show the real host (helpful, not
				// sensitive); a real send always resolves the true host.
				if c.DryRun && hosts.IsProduction(c.Env) {
					return hostBookDryRunPlaceholder(spec), nil, nil
				}
				url, err := hr.ResolveHost(&spec, c.Env)
				if err != nil {
					// A dry-run previews structure without requiring the host book to
					// be populated (the same way it skips credentials); show a clearly
					// synthetic placeholder so the plan still renders. A real send
					// surfaces the error so a missing host is actionable.
					if c.DryRun {
						return hostBookDryRunPlaceholder(spec), nil, nil
					}
					return "", nil, err
				}
				return url, nil, nil
			}
		}
	}
	if c.BaseURLOverride != "" {
		if hosts.IsNoOverride(spec.HostKey) {
			return "", nil, errs.Validation(errs.SubtypeInvalidArgument,
				"host %q cannot be overridden with --base-url", spec.HostKey).
				WithHint("this host's endpoint is fixed against redirection; --base-url is not honored for it")
		}
		c.warnOverriddenHost(spec.Identity, c.BaseURLOverride)
		return c.BaseURLOverride, nil, nil
	}
	ep, err := hosts.Resolve(spec.HostKey, c.Env)
	if err != nil {
		return "", nil, err
	}
	if ep.Overridden {
		c.warnOverriddenHost(spec.Identity, ep.URL)
	}
	return ep.URL, ep.Headers, nil
}

// warnOverriddenHost advises that a credentialed (non-host-book, non-internal)
// request is going to a host that came from --base-url or MEXC_<KEY>_BASE_URL
// rather than the shipped table — a heads-up that the identity's api key is about
// to be sent somewhere the CLI did not choose. Silent for an unauthenticated
// request (no credential at stake) and for a dry-run (nothing is sent). A
// host-book identity never reaches here (its overrides are refused above).
// Stated once per identity+host: the advice does not change on repetition, and a
// command that sends twice would otherwise print it twice.
func (c *Client) warnOverriddenHost(identity api.Identity, url string) {
	if c.DryRun || identity == "" || identity == api.IdentityNone {
		return
	}
	if !c.Advisories.firstTime(string(identity) + " " + url) {
		return
	}
	credential.Warnf("%s request is going to overridden host %s (not the built-in table); ensure you trust it with these credentials", identity, url)
}

// hostBookDryRunPlaceholder is the synthetic base a dry-run shows for a
// host-book identity whose host is not recorded yet — obviously not a real URL,
// but it renders a plan and signals which system's host is missing.
func hostBookDryRunPlaceholder(spec api.RequestSpec) string {
	sys := "internal-business"
	if spec.Permission != nil && spec.Permission.System != "" {
		sys = spec.Permission.System
	}
	return "https://<recorded-host-for-" + sys + ">"
}

// reservedHeadersFor returns the header names that must be stripped before this
// request follows a cross-host redirect: the ones its identity injects, plus the
// ones its signing scheme adds.
//
// It is a method rather than an inline expression at the call site so that "the
// identity's names come from THIS request's Lookup" is assertable without an HTTP
// round trip. That distinction is the whole point of it and it fails quietly: on a
// face that resolves credentials per caller, the provider in the process registry
// is a refusing placeholder that reserves nothing, so asking the wrong source
// returns an empty list — which strips nothing and forwards the injected
// credential to whatever host the redirect names.
func (c *Client) reservedHeadersFor(spec api.RequestSpec) []string {
	out := append([]string{}, credential.ReservedHeadersVia(c.identities(), credential.Realm(spec.Identity))...)
	return append(out, signer.ReservedHeaders(spec.Signed)...)
}

// sendOnce performs exactly one send of spec against an already-resolved base
// URL: identity injection, signing (with a fresh timestamp), body encoding, rate
// limiting, transport, and response classification. It carries no retry logic of
// its own. spec is taken by value and its mutable maps are copied, so signing and
// identity mutations from one attempt never leak into the next retry.
func (c *Client) sendOnce(ctx context.Context, spec api.RequestSpec, base string, epHeaders map[string]string, dst any) (*Plan, error) {
	plan, httpReq, err := c.prepareRequest(ctx, spec, base, epHeaders)
	if err != nil {
		return plan, err
	}
	if httpReq == nil { // dry-run: nothing to send
		return plan, nil
	}

	// Copy the shared client so a per-request redirect policy can be set without
	// racing other requests (the copy keeps the same Transport, so the connection
	// pool is shared). The policy strips this request's credentials before a
	// cross-host redirect, matching the download path (see redirectPolicy).
	hc := *c.hc
	hc.CheckRedirect = redirectPolicy(
		c.reservedHeadersFor(spec),
		signer.ReservedParams(spec.Signed),
	)
	resp, err := hc.Do(httpReq)
	if err != nil {
		return plan, errs.Network("request failed: %v", err).WithCause(err)
	}
	defer func() { _ = resp.Body.Close() }()

	return c.classify(resp, plan, dst)
}

// prepareRequest turns a spec into a ready-to-send *http.Request against an
// already-resolved base URL, performing the steps shared by every transport
// path: per-attempt map copies, identity injection, signing (fresh timestamp),
// body encoding, URL assembly, the Plan, rate-limit admission, and header/cookie
// application. It returns the Plan always, and the request unless this is a
// dry-run (then httpReq is nil and the caller returns the Plan without sending).
// spec is taken by value and its mutable maps copied, so mutations never leak
// across retries or back to the caller.
func (c *Client) prepareRequest(ctx context.Context, spec api.RequestSpec, base string, epHeaders map[string]string) (*Plan, *http.Request, error) {
	// Work on fresh copies of the maps that signing/identity mutate, so each retry
	// re-signs over a clean query rather than one already carrying a stale
	// timestamp/signature from the previous attempt.
	spec.Query = copyStringMap(spec.Query)
	spec.Header = copyStringMap(spec.Header)

	signed := spec.Signed.Signs()

	// Identity: attach the api-key / token material. A dry-run needs no
	// credentials (it never sends), so Resolve/Inject are skipped; the provider
	// lookup itself still runs, so an unwired identity fails fast even in dry-run.
	var cred credential.Credentials
	if spec.Identity != "" && spec.Identity != api.IdentityNone {
		p, ok := c.identities().Provider(credential.Realm(spec.Identity))
		if !ok {
			return nil, nil, errs.Internal("no credential provider for identity %q", spec.Identity)
		}
		if !c.DryRun {
			var err error
			cred, err = p.Resolve(c.cfg, c.Env)
			if err != nil {
				return nil, nil, err
			}
			if err := p.Inject(&spec, cred); err != nil {
				return nil, nil, err
			}
		}
	}

	// Signing is orthogonal to identity: any identity's credentials can feed any
	// scheme. A dry-run never computes a real signature (it would be a replayable
	// credential if it reached stdout / the agent context / logs).
	if signed {
		s, ok := signer.Get(spec.Signed)
		if !ok {
			return nil, nil, errs.Internal("unknown signing scheme %q", spec.Signed)
		}
		if c.DryRun {
			if dryRunSigner, ok := s.(signer.DryRunPreparer); ok {
				dryRunSigner.PrepareDryRun(&spec)
			} else {
				if spec.Query == nil {
					spec.Query = map[string]string{}
				}
				spec.Query["signature"] = signaturePlaceholder
			}
		} else if err := s.Sign(&spec, cred, time.UnixMilli(c.now())); err != nil {
			return nil, nil, err
		}
	}

	body, contentType, bodyKind, planFiles, err := buildBody(&spec)
	if err != nil {
		return nil, nil, err
	}

	full := strings.TrimRight(base, "/") + spec.Path
	if q := api.CanonicalQuery(spec.Query); q != "" {
		full += "?" + q
	}

	plan := &Plan{Method: spec.Method, URL: full, Host: spec.HostKey, Signed: signed, BodyKind: bodyKind, Files: planFiles}
	if c.DryRun {
		return plan, nil, nil
	}

	// Client-side throttle, when the product declares one. A single coarse bucket:
	// its job is to stop a runaway caller, not to mirror the backend's per-endpoint
	// quota (which it cannot). ErrWaitTooLong means the local budget is exhausted —
	// a client-side condition, so it is not the upstream's retryable 429.
	if c.Limiter != nil {
		if err := c.Limiter.Acquire(ctx); err != nil {
			if errors.Is(err, ratelimit.ErrWaitTooLong) {
				return plan, nil, errs.Validation(errs.SubtypeClientRateLimited,
					"client-side rate limit: no local send slot within the wait budget").
					WithHint("reduce request frequency or run fewer operations concurrently; this is a local throttle, not an upstream rejection")
			}
			return plan, nil, errs.Network("rate limiter wait cancelled: %v", err).WithCause(err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, spec.Method, full, body)
	if err != nil {
		return plan, nil, errs.Internal("failed to build request: %v", err).WithCause(err)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	// Two independent header writes, and the order between them does not matter:
	// one is this build's own version, the other is a set the caller sent that this
	// process passes through untouched. Neither can name a header the other does
	// (a product picks the version header's name; which names travel in Carry is
	// also the product's choice), so a collision is a product's to avoid.
	// First, so every source below outranks it: this observes the caller and must
	// never win over a header the request needs to work.
	if c.BuildVersionHeader != "" {
		httpReq.Header.Set(c.BuildVersionHeader, buildinfo.Version)
	}
	// Header precedence: endpoint (env) headers first, then per-request headers
	// (which include the identity-injected api key) so a request header wins.
	// Header precedence, lowest first: passthrough headers, then endpoint (env)
	// headers, then per-request headers (which include the identity-injected api
	// key) so a request header wins.
	//
	// Passthrough headers sit at the BOTTOM deliberately: they are what some caller
	// asked us to forward, and neither an operation's own header param nor an
	// identity's injected credential may be displaced by a value that arrived from
	// outside.
	//
	// They are copied VERBATIM. Nothing here parses, validates or regenerates a
	// value, and that is the point rather than laziness: W3C trace context is the
	// case this was built for, and its parent header encodes the current hop's span
	// id — so a process that rewrote it without reporting a span of its own would
	// split the trace in two, with both halves looking correct and nothing
	// reporting an error. Copying is the only behaviour that is right whether or
	// not this process is a span.
	//
	// A caller that sent nothing yields a nil map here, and ranging over nil
	// iterates zero times — so an absent header stays absent rather than being
	// written as empty. Do not add a fallback that invents a value for a missing
	// one: a trace id this process makes up has no parent anywhere, which produces
	// a broken link that looks healthy.
	for k, v := range caller.CarryFrom(ctx) {
		httpReq.Header.Set(k, v)
	}
	for k, v := range epHeaders {
		httpReq.Header.Set(k, v)
	}
	for k, v := range spec.Header {
		httpReq.Header.Set(k, v)
	}
	if len(spec.Cookie) > 0 {
		httpReq.Header.Set("Cookie", encodeCookie(spec.Cookie))
	}
	return plan, httpReq, nil
}

// quoteEscaper mirrors mime/multipart's internal escaper for Content-Disposition
// field/file names (backslash and double-quote).
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// createFilePart is CreateFormFile with a real Content-Type. The stdlib
// CreateFormFile hardcodes application/octet-stream, which some backends reject
// for image uploads (e.g. "only PNG/JPG images are allowed"). We resolve the
// type from the filename extension, fall back to sniffing the content, and only
// then to application/octet-stream.
func createFilePart(w *multipart.Writer, field, filename string, content []byte) (io.Writer, error) {
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		ct = http.DetectContentType(content)
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(field), quoteEscaper.Replace(filename)))
	h.Set("Content-Type", ct)
	return w.CreatePart(h)
}

// buildBody returns the request body reader, its Content-Type, a body-kind label
// for the Plan, and (for multipart) file metadata. Body/Form/Multipart are
// mutually exclusive (guaranteed by ValidateDecl); if none is set, returns nils.
func buildBody(spec *api.RequestSpec) (io.Reader, string, string, []PlanFile, error) {
	switch {
	case spec.Multipart != nil:
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		// Text fields, sorted for a deterministic body (and stable test assertions).
		fkeys := make([]string, 0, len(spec.Multipart.Fields))
		for k := range spec.Multipart.Fields {
			fkeys = append(fkeys, k)
		}
		sort.Strings(fkeys)
		for _, k := range fkeys {
			if err := w.WriteField(k, spec.Multipart.Fields[k]); err != nil {
				return nil, "", "", nil, errs.Internal("failed to write multipart field %q: %v", k, err).WithCause(err)
			}
		}
		var files []PlanFile
		for _, fp := range spec.Multipart.Files {
			fw, err := createFilePart(w, fp.Field, fp.Filename, fp.Content)
			if err != nil {
				return nil, "", "", nil, errs.Internal("failed to create multipart file %q: %v", fp.Field, err).WithCause(err)
			}
			if _, err := fw.Write(fp.Content); err != nil {
				return nil, "", "", nil, errs.Internal("failed to write multipart file %q: %v", fp.Field, err).WithCause(err)
			}
			files = append(files, PlanFile{Field: fp.Field, Filename: fp.Filename, Size: len(fp.Content)})
		}
		if err := w.Close(); err != nil {
			return nil, "", "", nil, errs.Internal("failed to finalize multipart body: %v", err).WithCause(err)
		}
		return &buf, w.FormDataContentType(), "multipart", files, nil
	case len(spec.Form) > 0:
		vals := url.Values{}
		for k, v := range spec.Form {
			vals.Set(k, v)
		}
		return strings.NewReader(vals.Encode()), "application/x-www-form-urlencoded", "form", nil, nil
	case spec.Body != nil:
		// A header-signing scheme puts this exact string into its canonical
		// request, so it is a value two packages have to agree on rather than a
		// local formatting choice. Sending a variant (a charset parameter, a
		// vendor suffix) breaks every signature that covers it.
		return bytes.NewReader(spec.Body), "application/json", "json", nil, nil
	default:
		return nil, "", "", nil, nil
	}
}

// encodeCookie renders the cookie map as a single Cookie header value
// ("k1=v1; k2=v2"), keys sorted for determinism.
func encodeCookie(cookies map[string]string) string {
	keys := make([]string, 0, len(cookies))
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}

// copyStringMap returns a shallow copy of m (nil stays nil), so per-attempt
// mutations do not leak back into the caller's spec across retries.
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
