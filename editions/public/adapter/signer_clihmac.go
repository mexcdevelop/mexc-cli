package adapter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/signing"
)

const (
	cliProtocolMarker       = "MEXC-CLI-V1"
	cliVersion              = "v1"
	cliVersionHeader        = "X-MEXC-CLI-VERSION"
	cliTimestampHeader      = "X-MEXC-CLI-TIMESTAMP"
	cliSignatureHeader      = "X-MEXC-CLI-SIGNATURE"
	cliIdempotencyKeyHeader = "X-MEXC-CLI-IDEMPOTENCY-KEY"
	cliDryRunTimestamp      = "<dry-run>"
	cliDryRunSignature      = "<redacted>"
)

// CliHMAC signs requests for the CLI gateway. It injects protocol headers and
// signs the external, pre-rewrite request path so its canonical request matches
// SignatureCli2 at the gateway.
type CliHMAC struct{}

func (CliHMAC) Scheme() api.Signing { return signing.CliHMAC }

// ReservedHeaders names protocol headers the signer INJECTS and would therefore
// silently overwrite if an operation declared one.
//
// The idempotency key is deliberately not among them, even though the protocol
// lists it alongside these three. The rule is reserve what you write, not what
// you read: the three below are produced here, so a declared one would be
// clobbered without a word, while the idempotency key is only ever read (into
// canonical line 9) — declaring it is how an operation supplies one, and there
// is nothing to overwrite. Reserving it would leave the read permanently empty
// and no way to set it.
//
// Reserving also strips a header across a host-changing redirect. That matters
// for the signature, which is credential-equivalent; an idempotency key is not.
func (CliHMAC) ReservedHeaders() []string {
	return []string{cliVersionHeader, cliTimestampHeader, cliSignatureHeader}
}

// PrepareDryRun describes signed CLI headers without loading credentials or
// calculating a replayable signature.
func (CliHMAC) PrepareDryRun(spec *api.RequestSpec) {
	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header[cliVersionHeader] = cliVersion
	spec.Header[cliTimestampHeader] = cliDryRunTimestamp
	spec.Header[cliSignatureHeader] = cliDryRunSignature
}

// Sign builds the canonical request and injects the protocol headers.
//
// A form or multipart body is refused here, inside the scheme: the gateway
// accepts only JSON for a non-empty body, and how much of a request a signature
// covers is the scheme's own business — no layer above it judges the pairing.
func (CliHMAC) Sign(spec *api.RequestSpec, cred credential.Credentials, now time.Time) error {
	if len(spec.Form) > 0 || spec.Multipart != nil {
		return errs.Internal("cli-hmac supports JSON request bodies only").
			WithHint("use a JSON body; form and multipart request bodies are not supported by the CLI signing protocol")
	}
	// The gateway rejects a GET that carries a body. Refusing it here costs a
	// round trip less than learning the same thing from a 400.
	if strings.EqualFold(spec.Method, "GET") && len(spec.Body) > 0 {
		return errs.Internal("cli-hmac cannot sign a GET request that carries a body").
			WithHint("move the parameters to the query, or declare the operation as POST")
	}

	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	body := spec.Body
	// Canonical line 6 must be the Content-Type actually sent, normalised. This
	// literal is only correct because the transport sends exactly this for a JSON
	// body — see buildBody in internal/transport, whose own test pins the same
	// string. Two places, one value: change either and the other's tests go red
	// (this one through the published vector for a POST with a body).
	contentType := ""
	if len(body) > 0 {
		contentType = "application/json"
	}
	canonical := strings.Join([]string{
		cliProtocolMarker,
		cred.APIKey,
		strings.ToUpper(spec.Method),
		spec.Path,
		cliCanonicalQuery(spec.Query),
		contentType,
		cliSHA256Hex(body),
		timestamp,
		cliHeaderValue(spec.Header, cliIdempotencyKeyHeader),
	}, "\n")

	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header[cliVersionHeader] = cliVersion
	spec.Header[cliTimestampHeader] = timestamp
	spec.Header[cliSignatureHeader] = hmacSHA256Hex(cred.Secret, canonical)
	return nil
}

// cliCanonicalQuery renders the query the way the gateway recomputes it: names
// sorted by code point, each name and value RFC3986 percent-encoded, joined as
// k=v pairs. Empty when there are no parameters.
//
// It does NOT reuse api.CanonicalQuery. That helper builds the string the
// transport puts on the wire, using form encoding, where a space is `+`. This
// protocol is RFC3986, where a space is `%20` — and the gateway decodes whatever
// arrives and re-encodes it this way before verifying, so signing the form
// spelling fails on exactly the requests whose parameters contain a space.
//
// The two must be allowed to differ: api.CanonicalQuery also serves the
// query-signing scheme, whose own canonical form is the form spelling.
func cliCanonicalQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys) // Go compares by UTF-8 byte, which orders code points
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, cliPctEncode(k)+"="+cliPctEncode(params[k]))
	}
	return strings.Join(parts, "&")
}

// cliPctEncode percent-encodes one name or value per RFC3986: A-Z a-z 0-9 - _ . ~
// pass through, everything else becomes %XX in upper-case hex over UTF-8 bytes.
//
// url.QueryEscape already produces exactly that, with one exception: it writes a
// space as `+`. A literal `+` in the input has already become `%2B` by then, so
// rewriting the remaining `+` back to `%20` cannot corrupt one.
func cliPctEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func cliSHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256Hex(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func cliHeaderValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}
