package ws

import (
	"encoding/json"
	"errors"
	"time"

	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// errNotLoginRelevant signals that a decoded frame carries no login-relevant
// information (neither an rs.login acceptance nor an rs.error rejection), so
// doLogin should keep reading rather than treat the frame as either outcome.
var errNotLoginRelevant = errors.New("frame not relevant to the login handshake")

// futuresCodec implements wsCodec for MEXC's futures (contract) WebSocket
// protocol — the polar opposite of spotCodec's wire format:
//   - one sub.<name> frame PER channel (not batched);
//   - EVERY frame, control or business data, is JSON TEXT — there is no
//     Protobuf at all;
//   - a data push carries "channel":"push.<name>"; a control frame carries
//     "channel":"rs.<name>" (an ack / login reply) or "channel":"pong" (the
//     heartbeat reply);
//   - the heartbeat is an application-level {"method":"ping"} text frame
//     every 15 seconds, replied to with {"channel":"pong",...}.
type futuresCodec struct{}

// futuresSubscribeRequest is the JSON shape of one futures subscribe frame:
// unlike spot's single batched frame, futures needs one call per channel.
type futuresSubscribeRequest struct {
	Method string         `json:"method"`
	Param  map[string]any `json:"param"`
}

// futuresEnvelope is the shape every futures WS frame shares at the top
// level: Channel disambiguates a data push (push.<name>) from a control
// frame (rs.<name>, pong); Data/Symbol are decoded per-channel by the caller.
type futuresEnvelope struct {
	Channel string          `json:"channel"`
	Symbol  string          `json:"symbol,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// subscribeFrames builds one sub.<name> frame per requested channel. A
// channel string carries an optional "<method>:<symbol>" encoding (this
// codec's own convention, since a bare channel string has no param map) —
// e.g. "sub.depth:BTC_USDT" becomes {"method":"sub.depth","param":{"symbol":"BTC_USDT"}}.
func (c *futuresCodec) subscribeFrames(channels []string) ([]wsFrame, error) {
	frames := make([]wsFrame, 0, len(channels))
	for _, ch := range channels {
		method, param, err := decodeFuturesChannel(ch)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(futuresSubscribeRequest{Method: method, Param: param})
		if err != nil {
			return nil, err
		}
		frames = append(frames, wsFrame{payload: b})
	}
	return frames, nil
}

func (c *futuresCodec) decode(f wsFrame) (port.WSMessage, bool, map[string]string, error) {
	// Every futures frame is JSON text; a binary frame is not part of this
	// protocol at all, so it is reported as undecodable rather than silently
	// ignored.
	if f.binary {
		return port.WSMessage{}, false, nil, nil
	}
	var env futuresEnvelope
	if err := json.Unmarshal(f.payload, &env); err != nil {
		return port.WSMessage{}, false, nil, err
	}
	if len(env.Channel) < 5 || env.Channel[:5] != "push." {
		// rs.<name> (subscription ack / login reply) or pong — a control
		// frame, not business data (ok=false).
		//
		// rs.error specifically is a REAL rejection during the subscribe-ack
		// wait, not a bare ack: an invalid symbol gets back exactly
		// {"channel":"rs.error","data":"Contract [X] not exists",...}, no
		// push.* frame ever follows. Without this, awaitSubscribeAck (which
		// treats "not rejected" as "subscription succeeded" after reading
		// one frame) would silently accept it as a plain ack, and the
		// caller would wait forever for data that will never arrive.
		// env.Data carries the human-readable reason; the rejected map's key
		// mirrors spot's shape (channel -> reason) even though futures
		// rarely names the offending channel back.
		if env.Channel == "rs.error" {
			var reason string
			_ = json.Unmarshal(env.Data, &reason)
			if reason == "" {
				reason = string(env.Data)
			}
			return port.WSMessage{Channel: env.Channel}, false, map[string]string{env.Channel: reason}, nil
		}
		return port.WSMessage{Channel: env.Channel}, false, nil, nil
	}
	return port.WSMessage{Channel: env.Channel, Symbol: env.Symbol, Body: env.Data}, true, nil, nil
}

// decodeLogin classifies a frame during the Futures login handshake:
// "rs.login" is acceptance, "rs.error" is rejection (a distinct case from a
// rs.error received during subscribe-ack: the login handshake happens first,
// so a rejection here means the credentials themselves are the problem — see
// doLogin's error classification). Any other channel is not login-relevant.
func (c *futuresCodec) decodeLogin(f wsFrame) (accepted, rejected bool, err error) {
	if f.binary {
		return false, false, errNotLoginRelevant
	}
	var env futuresEnvelope
	if jsonErr := json.Unmarshal(f.payload, &env); jsonErr != nil {
		return false, false, errNotLoginRelevant
	}
	switch env.Channel {
	case "rs.login":
		return true, false, nil
	case "rs.error":
		return false, true, nil
	default:
		return false, false, errNotLoginRelevant
	}
}

func (c *futuresCodec) heartbeat() (wsFrame, time.Duration) {
	return wsFrame{payload: []byte(`{"method":"ping"}`)}, 15 * time.Second
}

// personalFilterEntry / personalFilterParam / personalFilterRequest are the
// JSON shape of a personal.filter frame: sent once, right after a successful
// login, to narrow the default full private push (order, order.deal,
// position, asset, ...) down to only the named filter(s). There is no
// separate sub.<name> request for a private channel at all — login alone
// already streams everything; personal.filter only narrows it — and the
// server sends no acknowledgment frame for it (unlike an ordinary
// subscription's rs.sub.<name> ack).
type personalFilterEntry struct {
	Filter string `json:"filter"`
}
type personalFilterParam struct {
	Filters []personalFilterEntry `json:"filters"`
}
type personalFilterRequest struct {
	Method string              `json:"method"`
	Param  personalFilterParam `json:"param"`
}

// personalFilterFrame builds the personal.filter frame narrowing the default
// full private push down to the named filters (e.g. "order.deal"). filters
// values are the exact filter keys MEXC documents: order, order.deal,
// position, plan.order, stop.order, stop.planorder, risk.limit, adl.level,
// asset.
func (c *futuresCodec) personalFilterFrame(filters []string) (wsFrame, error) {
	entries := make([]personalFilterEntry, 0, len(filters))
	for _, f := range filters {
		entries = append(entries, personalFilterEntry{Filter: f})
	}
	b, err := json.Marshal(personalFilterRequest{Method: "personal.filter", Param: personalFilterParam{Filters: entries}})
	if err != nil {
		return wsFrame{}, err
	}
	return wsFrame{payload: b}, nil
}

// decodeFuturesChannel is subscribeFrames' helper: a channel string carries
// an optional "<method>:<symbol>" encoding (this codec's own convention,
// mirrored by the business WSCollect closures that construct Channels
// directly as plain string concatenation, e.g. "sub.depth:" + symbol — they
// cannot import this package, so the encoding is a documented string
// convention, not a shared function).
func decodeFuturesChannel(ch string) (method string, param map[string]any, err error) {
	for i := 0; i < len(ch); i++ {
		if ch[i] == ':' {
			return ch[:i], map[string]any{"symbol": ch[i+1:]}, nil
		}
	}
	return ch, map[string]any{}, nil
}
