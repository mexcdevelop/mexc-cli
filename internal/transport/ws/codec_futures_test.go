package ws

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// Tests for futuresCodec, added alongside codec_spot_test.go so a future edit
// to the futures wire-format handling (subscribe framing, push/control frame
// classification, the login handshake, or the personal.filter narrowing) is
// caught by a failing test rather than drifting silently — the same
// regression-guard role codec_spot_test.go already plays for spotCodec.

// TestFuturesCodec_SubscribeFrames_WithSymbol confirms the
// "<method>:<symbol>" convention (as used by ws-depth and ws-trades) is
// decoded into the method/param shape the server expects.
func TestFuturesCodec_SubscribeFrames_WithSymbol(t *testing.T) {
	c := &futuresCodec{}
	frames, err := c.subscribeFrames([]string{"sub.depth:BTC_USDT"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].binary {
		t.Errorf("frame marked binary, want a JSON text frame")
	}
	var got futuresSubscribeRequest
	if err := json.Unmarshal(frames[0].payload, &got); err != nil {
		t.Fatalf("frame payload is not valid JSON: %v", err)
	}
	want := futuresSubscribeRequest{Method: "sub.depth", Param: map[string]any{"symbol": "BTC_USDT"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestFuturesCodec_SubscribeFrames_WithoutSymbol confirms a channel with no
// "<method>:<symbol>" separator (whole-market ws-ticker, e.g.
// "sub.tickers") builds a frame with an empty param map
// rather than treating the bare channel string as malformed.
func TestFuturesCodec_SubscribeFrames_WithoutSymbol(t *testing.T) {
	c := &futuresCodec{}
	frames, err := c.subscribeFrames([]string{"sub.tickers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got futuresSubscribeRequest
	if err := json.Unmarshal(frames[0].payload, &got); err != nil {
		t.Fatalf("frame payload is not valid JSON: %v", err)
	}
	want := futuresSubscribeRequest{Method: "sub.tickers", Param: map[string]any{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestFuturesCodec_SubscribeFrames_OnePerChannel confirms futures' framing is
// the polar opposite of spot's batched SUBSCRIPTION frame: N channels build N
// separate frames, not one frame carrying all of them.
func TestFuturesCodec_SubscribeFrames_OnePerChannel(t *testing.T) {
	c := &futuresCodec{}
	frames, err := c.subscribeFrames([]string{"sub.depth:BTC_USDT", "sub.deal:BTC_USDT", "sub.tickers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3 (one per channel)", len(frames))
	}
}

// TestFuturesCodec_Decode_PushFrame confirms a "push.<name>" data frame is
// classified as business data (ok=true), with Channel/Symbol/Body populated
// from the envelope.
func TestFuturesCodec_Decode_PushFrame(t *testing.T) {
	c := &futuresCodec{}
	payload := []byte(`{"channel":"push.depth","symbol":"BTC_USDT","data":{"asks":[[64000,1,1]],"bids":[]}}`)
	msg, ok, rejected, err := c.decode(wsFrame{payload: payload})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("got ok=false, want true for a push.* frame")
	}
	if rejected != nil {
		t.Errorf("got rejected=%#v, want nil for a push.* frame", rejected)
	}
	if msg.Channel != "push.depth" || msg.Symbol != "BTC_USDT" {
		t.Errorf("got channel=%q symbol=%q, want push.depth/BTC_USDT", msg.Channel, msg.Symbol)
	}
	if msg.Body == nil {
		t.Errorf("got nil Body, want the decoded data payload")
	}
}

// TestFuturesCodec_Decode_OrdinaryAck confirms a plain subscription ack
// (channel "rs.sub.<name>", no error) is classified as a control frame
// (ok=false) with no rejection — the caller (awaitSubscribeAck) treats this
// as "subscription succeeded."
func TestFuturesCodec_Decode_OrdinaryAck(t *testing.T) {
	c := &futuresCodec{}
	payload := []byte(`{"channel":"rs.sub.depth"}`)
	msg, ok, rejected, err := c.decode(wsFrame{payload: payload})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("got ok=true, want false for a control frame")
	}
	if rejected != nil {
		t.Errorf("got rejected=%#v, want nil for an ordinary ack", rejected)
	}
	if msg.Channel != "rs.sub.depth" {
		t.Errorf("got channel=%q, want rs.sub.depth", msg.Channel)
	}
}

// TestFuturesCodec_Decode_Pong confirms the heartbeat reply ("channel":"pong")
// is classified as a control frame with no rejection, same as any other
// non-push.* channel.
func TestFuturesCodec_Decode_Pong(t *testing.T) {
	c := &futuresCodec{}
	msg, ok, rejected, err := c.decode(wsFrame{payload: []byte(`{"channel":"pong"}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("got ok=true, want false for a pong frame")
	}
	if rejected != nil {
		t.Errorf("got rejected=%#v, want nil for a pong frame", rejected)
	}
	if msg.Channel != "pong" {
		t.Errorf("got channel=%q, want pong", msg.Channel)
	}
}

// TestFuturesCodec_Decode_RsError is the regression guard this file exists
// for: an invalid-symbol subscribe gets back exactly
// {"channel":"rs.error","data":"Contract [X] not exists"} with no push.*
// frame ever following. Without classifying this case, awaitSubscribeAck's
// "not rejected after one frame == subscribed" logic would treat this as an
// ordinary ack and hang waiting for data that never arrives (see
// codec_futures.go's decode comment).
func TestFuturesCodec_Decode_RsError(t *testing.T) {
	c := &futuresCodec{}
	payload := []byte(`{"channel":"rs.error","data":"Contract [NOTREAL] not exists"}`)
	msg, ok, rejected, err := c.decode(wsFrame{payload: payload})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("got ok=true, want false for rs.error")
	}
	want := map[string]string{"rs.error": "Contract [NOTREAL] not exists"}
	if !reflect.DeepEqual(rejected, want) {
		t.Errorf("got rejected=%#v, want %#v", rejected, want)
	}
	if msg.Channel != "rs.error" {
		t.Errorf("got channel=%q, want rs.error", msg.Channel)
	}
}

// TestFuturesCodec_Decode_RsErrorNonStringData confirms an rs.error whose
// "data" field is not a JSON string (defensive: the live server always sends
// a string reason, but decode must not panic or lose the frame if it ever
// doesn't) falls back to the raw data bytes as the reason rather than an
// empty string.
func TestFuturesCodec_Decode_RsErrorNonStringData(t *testing.T) {
	c := &futuresCodec{}
	payload := []byte(`{"channel":"rs.error","data":{"code":400}}`)
	_, ok, rejected, err := c.decode(wsFrame{payload: payload})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("got ok=true, want false for rs.error")
	}
	if reason := rejected["rs.error"]; reason != `{"code":400}` {
		t.Errorf("got reason=%q, want the raw data bytes as fallback", reason)
	}
}

// TestFuturesCodec_Decode_BinaryFrame confirms a binary frame is reported as
// undecodable (ok=false, no error, no rejection) rather than matched against
// this protocol's JSON-only shape — futures has no Protobuf frames at all,
// unlike spot.
func TestFuturesCodec_Decode_BinaryFrame(t *testing.T) {
	c := &futuresCodec{}
	msg, ok, rejected, err := c.decode(wsFrame{binary: true, payload: []byte{0x01, 0x02, 0x03}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("got ok=true, want false for a binary frame")
	}
	if rejected != nil {
		t.Errorf("got rejected=%#v, want nil for a binary frame", rejected)
	}
	if msg != (port.WSMessage{}) {
		t.Errorf("got msg=%#v, want the zero value for a binary frame", msg)
	}
}

// TestFuturesCodec_Decode_MalformedJSON confirms invalid JSON surfaces as an
// error rather than a silently zeroed message — a WS text frame is
// attacker- or bug-controlled, not a trusted internal value.
func TestFuturesCodec_Decode_MalformedJSON(t *testing.T) {
	c := &futuresCodec{}
	_, _, _, err := c.decode(wsFrame{payload: []byte(`not json at all`)})
	if err == nil {
		t.Errorf("got nil error, want an error for malformed JSON")
	}
}

// TestFuturesCodec_DecodeLogin_Accepted confirms "rs.login" is classified as
// login acceptance.
func TestFuturesCodec_DecodeLogin_Accepted(t *testing.T) {
	c := &futuresCodec{}
	accepted, rejected, err := c.decodeLogin(wsFrame{payload: []byte(`{"channel":"rs.login"}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !accepted || rejected {
		t.Errorf("got accepted=%v rejected=%v, want accepted=true rejected=false", accepted, rejected)
	}
}

// TestFuturesCodec_DecodeLogin_Rejected confirms "rs.error" during the login
// phase is classified as login rejection — a distinct code path from the
// same channel name arriving during the subscribe-ack phase (decode, tested
// above), per codec_futures.go's decodeLogin comment: a rejection here means
// the credentials themselves are the problem.
func TestFuturesCodec_DecodeLogin_Rejected(t *testing.T) {
	c := &futuresCodec{}
	accepted, rejected, err := c.decodeLogin(wsFrame{payload: []byte(`{"channel":"rs.error","data":"invalid signature"}`)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if accepted || !rejected {
		t.Errorf("got accepted=%v rejected=%v, want accepted=false rejected=true", accepted, rejected)
	}
}

// TestFuturesCodec_DecodeLogin_Irrelevant confirms a frame whose channel is
// neither rs.login nor rs.error (e.g. a stray pong arriving before the login
// reply) returns errNotLoginRelevant, telling the caller (doLogin) to keep
// reading rather than treat it as either outcome.
func TestFuturesCodec_DecodeLogin_Irrelevant(t *testing.T) {
	c := &futuresCodec{}
	accepted, rejected, err := c.decodeLogin(wsFrame{payload: []byte(`{"channel":"pong"}`)})
	if !errors.Is(err, errNotLoginRelevant) {
		t.Errorf("got err=%v, want errNotLoginRelevant", err)
	}
	if accepted || rejected {
		t.Errorf("got accepted=%v rejected=%v, want both false alongside errNotLoginRelevant", accepted, rejected)
	}
}

// TestFuturesCodec_DecodeLogin_BinaryFrame confirms a binary frame during the
// login phase is also errNotLoginRelevant rather than a panic or false
// acceptance — futures has no binary frames at all.
func TestFuturesCodec_DecodeLogin_BinaryFrame(t *testing.T) {
	c := &futuresCodec{}
	accepted, rejected, err := c.decodeLogin(wsFrame{binary: true, payload: []byte{0x01}})
	if !errors.Is(err, errNotLoginRelevant) {
		t.Errorf("got err=%v, want errNotLoginRelevant", err)
	}
	if accepted || rejected {
		t.Errorf("got accepted=%v rejected=%v, want both false", accepted, rejected)
	}
}

// TestFuturesCodec_DecodeLogin_MalformedJSON confirms invalid JSON during the
// login phase is errNotLoginRelevant (keep reading) rather than a hard error
// that would abort the handshake on one malformed frame.
func TestFuturesCodec_DecodeLogin_MalformedJSON(t *testing.T) {
	c := &futuresCodec{}
	accepted, rejected, err := c.decodeLogin(wsFrame{payload: []byte(`not json`)})
	if !errors.Is(err, errNotLoginRelevant) {
		t.Errorf("got err=%v, want errNotLoginRelevant", err)
	}
	if accepted || rejected {
		t.Errorf("got accepted=%v rejected=%v, want both false", accepted, rejected)
	}
}

// TestFuturesCodec_Heartbeat confirms the heartbeat frame and interval match
// MEXC's documented futures protocol: an application-level
// {"method":"ping"} text frame every 15 seconds — distinct from spot's
// 5-second interval.
func TestFuturesCodec_Heartbeat(t *testing.T) {
	c := &futuresCodec{}
	frame, interval := c.heartbeat()
	if frame.binary {
		t.Errorf("heartbeat frame marked binary, want a JSON text frame")
	}
	if string(frame.payload) != `{"method":"ping"}` {
		t.Errorf("got payload %q, want {\"method\":\"ping\"}", frame.payload)
	}
	if interval != 15*time.Second {
		t.Errorf("got interval %v, want 15s", interval)
	}
}

// TestFuturesCodec_PersonalFilterFrame confirms the personal.filter frame
// narrowing the default full private push down to the named filters is
// built with the correct method/shape.
func TestFuturesCodec_PersonalFilterFrame(t *testing.T) {
	c := &futuresCodec{}
	frame, err := c.personalFilterFrame([]string{"order.deal"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got personalFilterRequest
	if err := json.Unmarshal(frame.payload, &got); err != nil {
		t.Fatalf("frame payload is not valid JSON: %v", err)
	}
	want := personalFilterRequest{
		Method: "personal.filter",
		Param:  personalFilterParam{Filters: []personalFilterEntry{{Filter: "order.deal"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestFuturesCodec_PersonalFilterFrame_MultipleFilters confirms more than one
// filter key is carried in the same frame, not one frame per filter.
func TestFuturesCodec_PersonalFilterFrame_MultipleFilters(t *testing.T) {
	c := &futuresCodec{}
	frame, err := c.personalFilterFrame([]string{"order.deal", "position"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got personalFilterRequest
	if err := json.Unmarshal(frame.payload, &got); err != nil {
		t.Fatalf("frame payload is not valid JSON: %v", err)
	}
	want := personalFilterParam{Filters: []personalFilterEntry{{Filter: "order.deal"}, {Filter: "position"}}}
	if !reflect.DeepEqual(got.Param, want) {
		t.Errorf("got %#v, want %#v", got.Param, want)
	}
}

// TestDecodeFuturesChannel_WithSymbol and TestDecodeFuturesChannel_NoSymbol
// exercise decodeFuturesChannel directly, since subscribeFrames' JSON-level
// tests above already cover the composed behavior but not the split logic
// in isolation (e.g. a symbol value that itself contains no further colon).
func TestDecodeFuturesChannel_WithSymbol(t *testing.T) {
	method, param, err := decodeFuturesChannel("sub.depth:BTC_USDT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "sub.depth" {
		t.Errorf("got method=%q, want sub.depth", method)
	}
	want := map[string]any{"symbol": "BTC_USDT"}
	if !reflect.DeepEqual(param, want) {
		t.Errorf("got param=%#v, want %#v", param, want)
	}
}

func TestDecodeFuturesChannel_NoSymbol(t *testing.T) {
	method, param, err := decodeFuturesChannel("sub.tickers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "sub.tickers" {
		t.Errorf("got method=%q, want sub.tickers", method)
	}
	if len(param) != 0 {
		t.Errorf("got param=%#v, want an empty map", param)
	}
}

// TestFuturesCodec_ImplementsWsCodec is a compile-time-style check kept as a
// runtime assertion too: codecFor's switch (codec.go) must keep resolving
// port.CodecFuturesEdge to a *futuresCodec that satisfies wsCodec — a
// regression here would surface as a type-assertion failure far from this
// file otherwise.
func TestFuturesCodec_ImplementsWsCodec(t *testing.T) {
	codec, err := codecFor(port.CodecFuturesEdge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := codec.(*futuresCodec); !ok {
		t.Errorf("got %T, want *futuresCodec", codec)
	}
}
