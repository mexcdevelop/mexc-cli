package ws

import (
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// wsFrame is a WS frame's type + payload, decoupled from gorilla/websocket's
// own message-type constants so a codec implementation does not need to
// import gorilla/websocket itself.
type wsFrame struct {
	binary  bool
	payload []byte
}

// wsCodec abstracts the wire-format differences between Spot (binary
// Protobuf, one batched SUBSCRIPTION frame) and Futures (JSON text, one
// sub.<name> frame per channel), so Collect's dial/heartbeat/read-loop
// shape is shared by both instead of forked per protocol. It is private to
// this package: a business WSCollect closure never references it, only the
// port.StreamCodec marker that selects one via codecFor.
type wsCodec interface {
	// subscribeFrames returns the frame(s) to send right after dial (and
	// after the optional login succeeds). Spot returns one frame carrying
	// every channel; futures returns one frame per channel.
	subscribeFrames(channels []string) ([]wsFrame, error)
	// decode classifies one received frame: a control frame (subscription
	// ack, heartbeat reply) returns ok=false; a business-data frame returns
	// the routed message with ok=true. A rejected subscription is reported
	// via rejected, keyed by the channel that was rejected, with the server's
	// reason as the value — never silently dropped.
	decode(f wsFrame) (msg port.WSMessage, ok bool, rejected map[string]string, err error)
	// decodeLogin classifies one received frame during the login handshake:
	// accepted/rejected report a definitive outcome; err non-nil means the
	// frame was not meaningful to the login handshake at all (e.g. it decoded
	// as neither an rs.login nor an rs.error), telling the caller to keep
	// reading rather than treat this as neutral. Only used when a codec's
	// protocol has an in-connection login step (Futures); Spot's codec is
	// never asked, since Spot's private channel never sets a LoginFrame.
	decodeLogin(f wsFrame) (accepted, rejected bool, err error)
	// heartbeat returns the frame to send on each heartbeat tick, and the
	// tick interval (Spot: 5s; Futures: 15s).
	heartbeat() (wsFrame, time.Duration)
	// personalFilterFrame builds the personal.filter frame a login-based
	// private channel (Futures) sends once, right after a successful login,
	// to narrow the default full private push down to the named filters. A
	// codec whose protocol has no such concept (Spot, whose private channel
	// authenticates by a pre-dial listenKey and subscribes with an ordinary
	// SUBSCRIPTION frame) returns an error; Collect only calls this when
	// cfg.LoginFrame is non-nil, which Spot never sets.
	personalFilterFrame(filters []string) (wsFrame, error)
}

// codecFor resolves a business-declared port.StreamCodec marker to the
// concrete wsCodec implementation. It is a closed, compile-time switch, not a
// registry: today there are exactly two protocol lines, and a third is a new
// case here, not a new registration mechanism.
func codecFor(c port.StreamCodec) (wsCodec, error) {
	switch c {
	case port.CodecSpotV3:
		return &spotCodec{}, nil
	case port.CodecFuturesEdge:
		return &futuresCodec{}, nil
	default:
		return nil, errs.Internal("unknown WS codec %q", c)
	}
}
