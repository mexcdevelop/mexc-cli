// Package transport is the port (abstraction) for sending a built request. It
// exposes the Sender capability and the transport-neutral Plan an edition prints
// under --dry-run, so an edition (or any composition root) depends only on this
// interface — never on the concrete client in internal/transport. The engine's
// concrete *transport.Client satisfies Sender structurally.
//
// This lives in pkg/ (an abstraction), while the HTTP machinery that implements
// it lives in internal/transport (a capability, module-private). Keeping the two
// apart is what lets an edition — including one that lives in another module —
// talk to the transport without reaching into internal/.
package transport

import (
	"context"
	"io"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// Sender sends a built RequestSpec, decoding a successful body into dst (unless
// dry-run), and returns the request Plan — the transport-neutral description of
// what was (or under --dry-run, would be) sent. IsDryRun reports whether this
// sender only previews (so a command can branch on prompting / output shape).
type Sender interface {
	Send(ctx context.Context, spec api.RequestSpec, dst any) (*Plan, error)
	IsDryRun() bool
}

// Plan is the transport-neutral description of a request, printed as-is under
// --dry-run. The signing scheme name stays internal — only whether the request
// is signed is surfaced.
type Plan struct {
	Method   string     `json:"method"`
	URL      string     `json:"url"`
	Host     string     `json:"host_key,omitempty"`
	Signed   bool       `json:"signed"`
	BodyKind string     `json:"body_kind,omitempty"` // json | form | multipart
	Files    []PlanFile `json:"files,omitempty"`
	// BodyChecked says whether this preview validated the caller's payload.
	//
	// Nil when the operation takes no body, so a plan that never had one says
	// nothing about it. Otherwise true (the payload was validated against the
	// declared prototype) or false (none was supplied, so this plan covers
	// structure only and a real call may still be rejected on its body).
	//
	// It is IN the plan rather than left to documentation because the reader is a
	// machine. A preview whose coverage depends on what the caller happened to
	// pass has to state that coverage where the caller is already looking.
	BodyChecked *bool `json:"body_checked,omitempty"`
}

// PlanFile is one file part previewed in a multipart Plan (dry-run / assertion).
type PlanFile struct {
	Field    string `json:"field"`
	Filename string `json:"filename"`
	Size     int    `json:"size"`
}

// StreamCodec names which wire-format codec a WS-backed operation's connection
// uses. It is a closed, compile-time enum (not a registry): today there are
// exactly two protocol lines (spot / futures), and adding a third is a new
// constant plus a new case in internal/transport's dispatch, not a new
// registration mechanism. Business code (skills/**/api) sets this field on a
// WSConfig; the concrete decode/encode logic lives in internal/transport, which
// is the only layer that needs to know the wire format.
type StreamCodec string

const (
	// CodecSpotV3 is MEXC's spot WebSocket protocol: binary Protobuf business
	// frames (the V3 wrapper), one batched SUBSCRIPTION frame, a 5s PING heartbeat.
	CodecSpotV3 StreamCodec = "spot-v3"
	// CodecFuturesEdge is MEXC's futures (contract) WebSocket protocol: JSON text
	// throughout, one sub.<name> frame per channel, a 15s ping heartbeat.
	CodecFuturesEdge StreamCodec = "futures-edge"
)

// WSMessage is one decoded business-data frame delivered to a WS-backed
// operation's OnMessage callback, already routed to its channel/symbol. Body's
// concrete type depends on Codec: for CodecSpotV3 it is a generated protobuf
// message type; for CodecFuturesEdge a decoded JSON value.
type WSMessage struct {
	Channel string `json:"channel"`
	Symbol  string `json:"symbol,omitempty"`
	Body    any    `json:"body"`
}

// WSCollector runs one WS-backed operation's subscription to completion. The WS
// counterpart of Sender, but optional: a product that declares no WS operation
// supplies none, and the implementation then stays out of its build. Assembly
// refuses a build that mounts a KindWSCollect operation without one.
type WSCollector interface {
	Collect(ctx context.Context, cfg WSConfig) error
}

// RateLimitPolicy is a product's client-side throttle: a coarse token bucket the
// transport applies before sending, to stop a runaway caller from firing
// obviously-excessive traffic at the gateway. It is deliberately NOT a mirror of
// the backend's real quota — that is per-endpoint, per-identity, and changeable
// at runtime, so no fixed client value can track it. The backend stays the
// authority (a 429 is retried with backoff); this only spares it the plainly
// pointless requests.
//
// A product states one by setting Edition.RateLimit. NIL MEANS THE DEFAULT below,
// not "no throttle": a product that genuinely wants no ceiling says Unlimited, so
// that "I forgot to state a policy" and "I want none" cannot be spelled the same
// way. Only one of those is a decision, and the other is the one that earns an
// upstream ban. Editions reference this type, so it lives in the port.
type RateLimitPolicy struct {
	RequestsPerSecond float64       // steady-state refill rate
	Burst             int           // tokens held when idle (max instantaneous burst)
	MaxWait           time.Duration // give up waiting for a token after this, rather than block forever

	// Unlimited turns the client-side throttle off entirely. It exists only so the
	// decision has a spelling of its own — see the note above. When set, the other
	// three fields are ignored.
	Unlimited bool
}

// DefaultRateLimit is the policy a product gets by saying nothing.
//
// The numbers are the measured ones, not a mirror of the backend's quota — that
// cannot be mirrored, because weights are per-endpoint, per-identity and tunable
// at runtime. Measured against the gateway: it admits roughly 300 at once and
// then answers 429, with steady state on the lightest endpoint around 18-31 req/s.
// So this is a runaway ceiling well above legitimate use, and low enough to stop
// an agent firing hundreds a second.
//
// It is deliberately NOT the older 10/s-with-a-wider-20/s-order-bucket pair. Those
// numbers had no source, the burst was five times tighter than measured, and the
// order bucket was inverted — the backend limits order paths harder, not softer.
var DefaultRateLimit = &RateLimitPolicy{
	RequestsPerSecond: 50,
	Burst:             50,
	MaxWait:           30 * time.Second,
}

// Unlimited is the policy that turns the throttle off. Named so a composition
// root reads as a decision: Edition.RateLimit = transport.Unlimited.
var Unlimited = &RateLimitPolicy{Unlimited: true}

// WSConfig is the transport-neutral description of one WS-backed operation's
// subscription, the WS counterpart of RequestSpec: a business WSCollect
// closure (skills/**/api) constructs it as pure data, and internal/transport's
// WS client consumes it to actually dial, authenticate, subscribe, and read.
//
// It lives in pkg/transport (a port, importable by skills/**/api) rather than
// internal/transport (module-private machinery skills/**/api is forbidden from
// importing — see .golangci.yml's api-no-framework rule) for the identical
// reason RequestSpec lives in pkg/api: the declaration layer must depend on
// nothing above it, and the transport depends on the declaration, never the
// reverse.
type WSConfig struct {
	// HostKey names the gateway this connection dials — "spot-ws" or
	// "futures-ws" in this product, registered into pkg/hosts by the
	// composition root exactly like a REST Operation's HostKey (see
	// api.RequestSpec.HostKey). A business WSCollect closure (skills/**/api)
	// never sets a literal URL: resolving HostKey to an actual endpoint —
	// the registered default, a MEXC_<KEY>_BASE_URL env override, or a
	// per-invocation flag override — happens entirely in internal/cli's WS
	// execution path, one layer up, mirroring internal/transport/send.go's
	// resolveBase for a REST request.
	HostKey string
	// URL is an OPTIONAL suffix appended to HostKey's resolved base — never
	// a full URL of its own. internal/cli's WS execution path always
	// computes the final dial target as resolvedBase + URL (URL empty means
	// "the base alone is the whole target", the common case for every
	// public channel and for Futures' private channel). Spot's private
	// channel is the one case that needs a non-empty suffix: it sets URL to
	// a listenKey query string (e.g. "?listenKey=..." — see
	// ApplyPreflightResult below, and skills/spot/account/api/ws.go's
	// dry-run branch for the placeholder shown before a real preflight
	// runs). Named URL rather than Suffix/Query because it is still,
	// concretely, the tail of a URL — just never the whole one a closure
	// can build alone, since it never learns the resolved base.
	URL string
	// Channels is a list of channel names (a public or Spot-private
	// subscription) or, when LoginFrame is non-nil, a list of personal.filter
	// keys (Futures' private channel: login streams every private event by
	// default, and this narrows it down — see internal/transport's
	// personalFilterFrame). Which meaning applies is determined entirely by
	// whether LoginFrame is set; the field itself carries no marker.
	Channels []string
	Codec    StreamCodec

	// PreflightRequest, when non-nil, is a REST request internal/cli's WS
	// execution path must send and succeed BEFORE dialing (e.g. Spot's listenKey
	// exchange). It is a RequestSpec — the SAME transport-neutral type an
	// ordinary Operation's BuildSpec produces — so the existing Sender can send
	// it with no new machinery. Nil for a public channel and for Futures'
	// private channel (whose auth is an in-connection login frame, not a
	// pre-dial REST call).
	PreflightRequest *api.RequestSpec

	// ApplyPreflightResult, mandatory when PreflightRequest is non-nil, turns
	// the preflight response (already decoded into dst, e.g. a
	// map[string]any{"listenKey": "..."}) into the URL SUFFIX to append to
	// HostKey's resolved base (e.g. "?listenKey=" + the escaped value) — the
	// same suffix-not-full-URL contract URL itself carries above, for the
	// identical reason: only internal/cli's WS execution path knows the
	// resolved base, so a business closure can only ever hand back the part
	// it alone knows how to build. Only the business closure that set
	// PreflightRequest knows the response shape and how the credential
	// belongs in that suffix, so this stays a business-supplied function
	// rather than a generic convention internal/cli would have to guess at.
	ApplyPreflightResult func(dst any) (string, error)

	// LoginFrame, when non-nil, is a WS frame (already marshaled by the
	// business closure, which alone holds the api-key/secret needed to sign
	// it) that internal/transport's WS client sends immediately after dial and
	// blocks on until the server replies success or rejection, before
	// subscribing. Nil for a public channel and for Spot's private channel.
	LoginFrame []byte

	// Duration and Limit express an optional terminal condition for the read
	// loop; both zero means run until ctx is cancelled or the connection ends.
	// Set by internal/cli's WS execution path from --listen-duration/--limit,
	// never by the business Build closure.
	Duration time.Duration
	Limit    int

	// OnMessage is called once per decoded data frame. Set by internal/cli's WS
	// execution path, never by the business Build closure.
	OnMessage func(WSMessage)
	// OnReject is called once if the server rejects the subscription or login,
	// or the subscribe-ack wait times out, before the WS client returns a
	// non-nil error. May be nil.
	OnReject func(channel, reason string)
}

// StreamResult is the outcome of a streaming (binary) response: a live body the
// caller consumes incrementally — copying it to disk — instead of the transport
// buffering and decoding it. The caller MUST Close Body. ContentType,
// ContentDisposition, and ContentLength carry the metadata a downloader uses to
// name the output file and report its size.
type StreamResult struct {
	Body               io.ReadCloser
	ContentType        string
	ContentDisposition string
	ContentLength      int64
}
