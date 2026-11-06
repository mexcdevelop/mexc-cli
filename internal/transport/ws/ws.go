// WS execution machinery: dial, the optional login handshake, subscribe,
// subscribe-ack wait, heartbeat, and the read loop shared by every WS-backed
// operation (Spot and Futures alike). A business WSCollect closure
// (skills/**/api) only ever constructs a port.WSConfig as pure data; this file
// is the one place that actually opens a connection and reads from it.
package ws

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mexcdevelop/mexc-cli/internal/transport"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// dialer is the shared gorilla/websocket dialer. websocket.DefaultDialer
// already honors HTTP_PROXY/HTTPS_PROXY/NO_PROXY via
// http.ProxyFromEnvironment, so no extra proxy wiring is needed here.
var dialer = websocket.DefaultDialer

// heartbeatWriteTimeout bounds a single heartbeat frame write, so a stalled
// connection cannot block the heartbeat goroutine indefinitely.
const heartbeatWriteTimeout = 5 * time.Second

// loginTimeout bounds the Futures in-connection login handshake, independent
// of subscribeAckTimeout: login happens BEFORE subscribing, so a server that
// never replies to the login frame must not be caught only by the
// subscribe-ack timeout, which never starts counting until login succeeds.
const loginTimeout = 10 * time.Second

// subscribeAckTimeout bounds how long Collect waits for the server to
// acknowledge (accept or reject) the subscription after the subscribe
// frame(s) are sent, independent of cfg.Duration/cfg.Limit — those only bound
// the read loop AFTER subscription succeeds. Without this, a server that
// never responds to the subscribe frame would hang Collect indefinitely
// regardless of what Duration/Limit the caller set. A subscribe-ack timeout
// is treated exactly like an explicit rejection: cfg.OnReject fires with
// reason "ack timeout", Collect returns a non-nil error.
//
// A var, not a const, so tests can shorten it without a real multi-second wait.
var subscribeAckTimeout = 10 * time.Second

// Collector implements pkg/transport.WSCollector. Carries no mutable state:
// every call owns its own connection, so one value serves the whole process.
type Collector struct {
	// BuildVersionHeader is the WS counterpart of transport.Client's field of the
	// same name: a product names one and gets it on both faces, or on neither.
	BuildVersionHeader string
}

var _ port.WSCollector = Collector{}

// handshakeHeader builds the upgrade request's extra headers, or nil for none.
//
// It must be an http.Header, not a bare map: Set canonicalizes the name, and
// gorilla then copies it verbatim (req.Header[k] = vs, deliberately not Set). A
// raw name would go out spelled differently here than on the REST path, where
// Set canonicalizes it — one header, two spellings.
func (c Collector) handshakeHeader() http.Header {
	if c.BuildVersionHeader == "" {
		return nil
	}
	h := http.Header{}
	h.Set(c.BuildVersionHeader, buildinfo.Version)
	return h
}

// Collect dials cfg.URL, performs the optional login handshake
// (cfg.LoginFrame), subscribes to cfg.Channels, waits for the server to
// acknowledge the subscription (bounded by subscribeAckTimeout, independent
// of Duration/Limit), starts the heartbeat ticker, and runs the read loop,
// calling cfg.OnMessage for every decoded data frame until one of:
//
//   - Duration elapses or Limit is reached (only when either is > 0) — returns nil;
//   - ctx is cancelled (SIGINT/SIGTERM, or the caller's own deadline) — returns ctx.Err();
//   - the subscription (or login) is rejected, or the ack wait times out —
//     cfg.OnReject fires once (if set), then a non-nil error is returned,
//     without ever entering the read loop's data-frame branch;
//   - the connection ends unexpectedly (no auto-reconnect) — returns a non-nil error.
//
// Collect owns dial/heartbeat/close entirely; the caller never touches a raw
// *websocket.Conn, and every message reaches it exclusively through
// cfg.OnMessage — there is no buffered return value.
func (c Collector) Collect(ctx context.Context, cfg port.WSConfig) error {
	codec, err := codecFor(cfg.Codec)
	if err != nil {
		return err
	}

	// bodyclose flags the discarded handshake response, but gorilla documents that
	// this body "does not need to be closed by the application".
	conn, _, err := dialer.DialContext(ctx, cfg.URL, c.handshakeHeader()) //nolint:bodyclose // gorilla: handshake response needs no closing
	if err != nil {
		return errs.Network("failed to connect to %s: %v", transport.RedactURLQuery(cfg.URL), err).WithCause(err)
	}
	defer func() { _ = conn.Close() }()

	// A login-based private channel (Futures) and an ordinary subscription
	// (every public channel, and Spot's pre-dial-authenticated private
	// channel) are NOT the same protocol shape after the connection opens:
	// login streams every private event by default with no separate
	// subscribe step at all, and personal.filter (which narrows that default
	// down to cfg.Channels) carries no acknowledgment frame — unlike an
	// ordinary sub.<name> request, which the server always acks (accept or
	// reject). So there is no subscribe-ack wait on this path: a successful
	// login is the only confirmation the server gives.
	if cfg.LoginFrame != nil {
		if err := doLogin(ctx, conn, cfg.LoginFrame, codec); err != nil {
			return err
		}
		if len(cfg.Channels) > 0 {
			frame, ferr := codec.personalFilterFrame(cfg.Channels)
			if ferr != nil {
				return errs.Internal("failed to build personal.filter frame: %v", ferr).WithCause(ferr)
			}
			if werr := writeFrame(conn, frame); werr != nil {
				return errs.Network("failed to send personal.filter frame: %v", werr).WithCause(werr)
			}
		}
		stopHeartbeat := startHeartbeat(conn, codec)
		defer stopHeartbeat()
		return runLoop(ctx, conn, codec, cfg)
	}

	frames, err := codec.subscribeFrames(cfg.Channels)
	if err != nil {
		return errs.Internal("failed to build subscribe frame(s): %v", err).WithCause(err)
	}
	for _, f := range frames {
		if err := writeFrame(conn, f); err != nil {
			return errs.Network("failed to send subscribe frame: %v", err).WithCause(err)
		}
	}

	if err := awaitSubscribeAck(ctx, conn, codec, cfg); err != nil {
		return err
	}

	stopHeartbeat := startHeartbeat(conn, codec)
	defer stopHeartbeat()

	return runLoop(ctx, conn, codec, cfg)
}

// awaitSubscribeAck reads frames until the codec reports a rejection or
// subscribeAckTimeout elapses (treated exactly like an explicit rejection).
// It returns nil the moment either an ordinary control frame or a data frame
// arrives — an ack that carries no rejection is implicitly a success, and a
// data frame arriving before any ack is proof the subscription succeeded.
// Either way, subsequent frames belong to runLoop, not here: the first
// non-rejection frame's decode result (if it was already business data) is
// forwarded to cfg.OnMessage so runLoop does not have to read past it.
func awaitSubscribeAck(ctx context.Context, conn *websocket.Conn, codec wsCodec, cfg port.WSConfig) error {
	if err := conn.SetReadDeadline(time.Now().Add(subscribeAckTimeout)); err != nil {
		return errs.Internal("failed to set subscribe-ack read deadline: %v", err).WithCause(err)
	}

	stopWatch := watchCtxDone(ctx, conn)
	defer stopWatch()

	msgType, data, err := conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return errs.Network("subscribe interrupted: %v", ctx.Err()).WithCause(ctx.Err())
		}
		if !isTimeoutOrExpectedClose(err) {
			return errs.Network("connection failed while waiting for subscribe ack: %v", err).WithCause(err)
		}
		fireReject(cfg, joinChannels(cfg.Channels), "ack timeout")
		return errs.API(0, errs.SubtypeSubscriptionAckTimeout, "subscribe ack timed out after %s", subscribeAckTimeout)
	}

	msg, ok, rejected, decErr := codec.decode(wsFrame{binary: msgType == websocket.BinaryMessage, payload: data})
	if len(rejected) > 0 {
		for ch, reason := range rejected {
			fireReject(cfg, ch, reason)
		}
		return errs.API(0, errs.SubtypeSubscriptionRejected, "subscription rejected by server")
	}
	if decErr == nil && ok {
		fireMessage(cfg, msg)
	}
	return nil
}

// fireReject invokes cfg.OnReject if the caller set one; a nil OnReject is
// valid (the caller may only care about the returned error).
func fireReject(cfg port.WSConfig, channel, reason string) {
	if cfg.OnReject != nil {
		cfg.OnReject(channel, reason)
	}
}

// fireMessage invokes cfg.OnMessage if the caller set one.
func fireMessage(cfg port.WSConfig, msg port.WSMessage) {
	if cfg.OnMessage != nil {
		cfg.OnMessage(msg)
	}
}

// doLogin sends loginFrame (already built and signed by the business Build
// closure) and blocks until the server replies success (returns nil) or
// rejection (returns a validation error — a login rejection means the
// credentials themselves are the problem, distinct from a subscription
// rejection, which is a business/parameter problem), or the connection
// fails. It
// watches ctx the same way the read loop does, so an interactive Ctrl+C during
// the handshake unblocks immediately instead of waiting out loginTimeout.
func doLogin(ctx context.Context, conn *websocket.Conn, loginFrame []byte, codec wsCodec) error {
	if err := writeFrame(conn, wsFrame{payload: loginFrame}); err != nil {
		return errs.Network("failed to send login frame: %v", err).WithCause(err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(loginTimeout)); err != nil {
		return errs.Internal("failed to set login read deadline: %v", err).WithCause(err)
	}

	stopWatch := watchCtxDone(ctx, conn)
	defer stopWatch()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return errs.Network("login interrupted: %v", ctx.Err()).WithCause(ctx.Err())
			}
			return errs.Network("connection failed while waiting for login response: %v", err).WithCause(err)
		}
		accepted, rejected, decErr := codec.decodeLogin(wsFrame{binary: msgType == websocket.BinaryMessage, payload: data})
		if decErr != nil {
			continue // not every frame during login is meaningful; keep waiting
		}
		if accepted {
			return nil
		}
		if rejected {
			// A login rejection means the credentials are the problem (e.g. an
			// invalid or expired API key), not the subscription request that
			// has not even been sent yet — validation/failed_precondition, not
			// api/subscription_rejected.
			return errs.Validation(errs.SubtypeFailedPrecondition, "WS login rejected by server").
				WithHint("verify the API key has futures trading permission and has not expired")
		}
	}
}

// writeFrame sends one frame as either a binary or text WS message.
func writeFrame(conn *websocket.Conn, f wsFrame) error {
	msgType := websocket.TextMessage
	if f.binary {
		msgType = websocket.BinaryMessage
	}
	return conn.WriteMessage(msgType, f.payload)
}

// startHeartbeat starts a goroutine that sends codec's heartbeat frame on its
// declared interval, until the returned stop function is called. It never
// itself reads from the connection. A failed heartbeat write is silently
// ignored — it is not this goroutine's job to decide the connection is dead;
// that is discovered by the read loop's next ReadMessage error, avoiding a
// race between two goroutines independently reacting to the same failure.
func startHeartbeat(conn *websocket.Conn, codec wsCodec) (stop func()) {
	frame, interval := codec.heartbeat()
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = conn.SetWriteDeadline(time.Now().Add(heartbeatWriteTimeout))
				_ = writeFrame(conn, frame)
			}
		}
	}()
	return func() { close(done) }
}

// runLoop is the read loop: it calls cfg.OnMessage for every decoded data
// frame, until Duration/Limit says stop (bounded), ctx is cancelled, or the
// connection ends. gorilla/websocket's ReadMessage must not be called again
// after it once returns an error (including a deadline timeout) — calling it
// again panics — so a bounded run sets ONE deadline for the whole window and
// reads until that single deadline/ctx fires, rather than looping with short
// per-read deadlines.
func runLoop(ctx context.Context, conn *websocket.Conn, codec wsCodec, cfg port.WSConfig) error {
	bounded := cfg.Duration > 0 || cfg.Limit > 0

	deadline := time.Time{} // zero = no deadline
	if cfg.Duration > 0 {
		deadline = time.Now().Add(cfg.Duration)
	}
	if ctxDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || ctxDeadline.Before(deadline)) {
		deadline = ctxDeadline
	}
	// SetReadDeadline must be called here even when deadline is zero: the
	// connection carries whatever deadline awaitSubscribeAck last set
	// (subscribeAckTimeout), and net.Conn's deadline, once set, stays in
	// effect until explicitly changed — it does not reset itself just
	// because this loop chooses not to impose one. Passing the zero Time
	// clears it, which is what "no Duration" must mean here: with only
	// --limit set (no --listen-duration), the read loop has to block
	// indefinitely for the Limit-th message, not silently inherit and then
	// trip over a 10s ack-wait deadline that has nothing to do with the
	// user's Limit/Duration budget.
	if err := conn.SetReadDeadline(deadline); err != nil {
		return errs.Internal("failed to set read deadline: %v", err).WithCause(err)
	}

	stopWatch := watchCtxDone(ctx, conn)
	defer stopWatch()

	count := 0
	for {
		if bounded && cfg.Limit > 0 && count >= cfg.Limit {
			return nil
		}
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if bounded && isTimeoutOrExpectedClose(err) {
				return nil // the collection window elapsed — a successful, expected end
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errs.Network("connection ended: %v", err).WithCause(err)
		}
		msg, ok, rejected, decErr := codec.decode(wsFrame{binary: msgType == websocket.BinaryMessage, payload: data})
		for ch, reason := range rejected {
			fireReject(cfg, ch, reason)
		}
		if decErr != nil || !ok {
			continue // control frame (ack/heartbeat) or an undecodable frame — not business data
		}
		fireMessage(cfg, msg)
		count++
	}
}

// watchCtxDone starts a goroutine that closes conn as soon as ctx is done, so
// a blocking ReadMessage in the caller's read loop returns promptly (with an
// error, not a panic — gorilla/websocket documents Close as a valid way to
// unblock a pending read) instead of waiting for an unrelated deadline or a
// remote hangup. The returned stop function must be called once the read loop
// has already exited on its own.
func watchCtxDone(ctx context.Context, conn *websocket.Conn) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// isTimeoutOrExpectedClose reports whether err is a read deadline timeout (the
// normal, successful end of a bounded collection window).
func isTimeoutOrExpectedClose(err error) bool {
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	return false
}

// joinChannels renders cfg.Channels as a single string for an ack-timeout
// rejection notification, which is not naturally keyed by one channel the way
// an explicit per-channel rejection is.
func joinChannels(channels []string) string {
	out := ""
	for i, c := range channels {
		if i > 0 {
			out += ","
		}
		out += c
	}
	return out
}
