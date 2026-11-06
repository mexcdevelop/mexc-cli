package ws

import (
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	spotpb "github.com/mexcdevelop/mexc-cli/internal/transport/ws/gen/spotpb"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// spotCodec implements wsCodec for MEXC's spot WebSocket protocol:
//   - one batched {"method":"SUBSCRIPTION","params":[...]} frame subscribes to
//     every channel at once;
//   - the subscription-ack and heartbeat-reply control frames are JSON text;
//     every business-data frame is BINARY Protobuf, decoded as a
//     PushDataV3ApiWrapper (the V3 wrapper; the V2 wrapper is out of scope);
//   - the heartbeat is an application-level {"method":"PING"} text frame
//     every 5 seconds — NOT the WS protocol's native ping/pong control frame.
//
// Spot never sets a LoginFrame (its private channel authenticates by a
// pre-dial listenKey query parameter, not an in-connection handshake), so
// decodeLogin is never actually called on this codec in practice.
type spotCodec struct {
	// nextID is incremented per subscribeFrames call so repeated subscribes
	// within one connection carry distinct ids; the id is otherwise opaque —
	// the server echoes it back but does not require global uniqueness.
	nextID int
}

// spotSubscribeRequest is the JSON shape of a subscribe frame.
type spotSubscribeRequest struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     int      `json:"id"`
}

// spotAck is the JSON shape of a subscription-confirmation or PONG control
// frame. msg on a subscription ack lists the successfully subscribed channels
// comma-separated, and on partial failure additionally contains
// "Not Subscribed successfully! [<channel>].  Reason\uFF1A <reason>" segments
// (the exchange's live server uses a full-width colon, U+FF1A, not ASCII ':'
// — see decodeSpotRejections, which matches it via \uFF1A rather than the
// literal character so this file stays English-only).
type spotAck struct {
	ID   *int   `json:"id"`
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (c *spotCodec) subscribeFrames(channels []string) ([]wsFrame, error) {
	c.nextID++
	req := spotSubscribeRequest{Method: "SUBSCRIPTION", Params: channels, ID: c.nextID}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return []wsFrame{{payload: b}}, nil
}

func (c *spotCodec) decode(f wsFrame) (port.WSMessage, bool, map[string]string, error) {
	if !f.binary {
		return port.WSMessage{}, false, decodeSpotRejections(f.payload), nil
	}
	var wrapper spotpb.PushDataV3ApiWrapper
	if err := proto.Unmarshal(f.payload, &wrapper); err != nil {
		return port.WSMessage{}, false, nil, err
	}
	body := spotWrapperBody(&wrapper)
	if body == nil {
		// A well-formed frame whose oneof did not match any case this codec
		// handles — not an error, just not business data we route.
		return port.WSMessage{}, false, nil, nil
	}
	return port.WSMessage{Channel: wrapper.GetChannel(), Symbol: wrapper.GetSymbol(), Body: body}, true, nil, nil
}

func (c *spotCodec) decodeLogin(f wsFrame) (accepted, rejected bool, err error) {
	// Spot never sets a LoginFrame, so doLogin never calls this in practice;
	// it exists only to satisfy the wsCodec interface.
	return false, false, errNotLoginRelevant
}

func (c *spotCodec) heartbeat() (wsFrame, time.Duration) {
	return wsFrame{payload: []byte(`{"method":"PING"}`)}, 5 * time.Second
}

// personalFilterFrame is unreachable in practice: Spot's private channel
// authenticates by a pre-dial listenKey query parameter (Collect never sets
// cfg.LoginFrame for it), not an in-connection login, so Collect never
// calls this on a spotCodec. It exists only to satisfy the wsCodec interface.
func (c *spotCodec) personalFilterFrame(filters []string) (wsFrame, error) {
	return wsFrame{}, errNotLoginRelevant
}

// spotWrapperBody extracts the concrete business message from the wrapper's
// oneof, for every V3 channel this codec's operations subscribe to (ticker,
// depth, trades, and the private new-trade push). Returns nil for any other
// oneof case.
func spotWrapperBody(w *spotpb.PushDataV3ApiWrapper) any {
	if v := w.GetPublicAggreBookTicker(); v != nil {
		return v
	}
	if v := w.GetPublicAggreDepths(); v != nil {
		return v
	}
	if v := w.GetPublicAggreDeals(); v != nil {
		return v
	}
	if v := w.GetPrivateDeals(); v != nil {
		return v
	}
	return nil
}

// decodeSpotRejections parses a text control frame for the
// "Not Subscribed successfully! [<channel>].  Reason\uFF1A <reason>" segments
// a subscription ack carries when part of a batched SUBSCRIPTION request was
// rejected (observed live: "Reason\uFF1A Blocked!" for a non-aggregated
// public channel — the exchange uses a full-width colon, U+FF1A, matched here
// via its escape rather than the literal character, keeping this file
// English-only).
// A frame with no such segment (an ordinary ack, a PONG, or malformed JSON)
// yields nil.
func decodeSpotRejections(payload []byte) map[string]string {
	var ack spotAck
	if err := json.Unmarshal(payload, &ack); err != nil {
		return nil
	}
	const marker = "Not Subscribed successfully! ["
	if !strings.Contains(ack.Msg, marker) {
		return nil
	}
	rejected := map[string]string{}
	rest := ack.Msg
	for {
		idx := strings.Index(rest, marker)
		if idx < 0 {
			break
		}
		rest = rest[idx+len(marker):]
		end := strings.Index(rest, "]")
		if end < 0 {
			break
		}
		channel := rest[:end]
		rest = rest[end+1:]
		reason := ""
		if ri := strings.Index(rest, "Reason\uFF1A"); ri >= 0 {
			reasonPart := strings.TrimSpace(rest[ri+len("Reason\uFF1A"):])
			// The reason runs up to the next "Not Subscribed" segment or end
			// of string, whichever comes first.
			if ni := strings.Index(reasonPart, "Not Subscribed"); ni >= 0 {
				reasonPart = reasonPart[:ni]
			}
			reason = strings.TrimSpace(reasonPart)
		}
		rejected[channel] = reason
	}
	if len(rejected) == 0 {
		return nil
	}
	return rejected
}
