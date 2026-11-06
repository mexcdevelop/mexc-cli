package ws

import (
	"reflect"
	"testing"
)

// Tests for decodeSpotRejections, added alongside the commit that replaced
// the literal full-width colon character (U+FF1A) in the "Reason" marker
// with its \uFF1A escape (see wscodec_spot.go) to keep this source file
// English-only without changing which bytes the
// function matches against. Every case here uses \uFF1A in the INPUT
// payload too, mirroring the real server: MEXC's live rejection message
// (observed against spot@public.aggre.bookTicker.v3.api.pb@100ms@INVALIDSYMBOL
// during manual verification) is byte-for-byte "Reason\uFF1A Blocked!", not
// "Reason: Blocked!" — an ASCII colon would never match in production, so a
// test using an ASCII colon would pass for the wrong reason and mask a
// regression back to a literal (or absent) marker.

// TestDecodeSpotRejections_SingleChannel is the exact shape observed live: a
// subscribe request for one invalid channel, rejected with a reason.
func TestDecodeSpotRejections_SingleChannel(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"Not Subscribed successfully! [spot@public.aggre.bookTicker.v3.api.pb@100ms@INVALIDSYMBOL].  Reason\uFF1A Blocked!"}`)
	got := decodeSpotRejections(payload)
	want := map[string]string{
		"spot@public.aggre.bookTicker.v3.api.pb@100ms@INVALIDSYMBOL": "Blocked!",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestDecodeSpotRejections_MultipleChannels exercises the marker-scanning
// loop: a single batched SUBSCRIPTION request can reject more than one
// channel in the same ack, each with its own "Not Subscribed...Reason\uFF1A"
// segment concatenated in one msg string.
func TestDecodeSpotRejections_MultipleChannels(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"Not Subscribed successfully! [chanA].  Reason\uFF1A Blocked!Not Subscribed successfully! [chanB].  Reason\uFF1A Invalid symbol"}`)
	got := decodeSpotRejections(payload)
	want := map[string]string{
		"chanA": "Blocked!",
		"chanB": "Invalid symbol",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestDecodeSpotRejections_NoReasonMarker confirms a rejected channel whose
// segment carries no "Reason\uFF1A" text still yields an entry (empty
// reason), rather than being dropped or panicking.
func TestDecodeSpotRejections_NoReasonMarker(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"Not Subscribed successfully! [chanA]."}`)
	got := decodeSpotRejections(payload)
	want := map[string]string{"chanA": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// TestDecodeSpotRejections_OrdinaryAck confirms a plain successful
// subscription ack (no "Not Subscribed" marker at all) yields nil, not an
// empty map — the caller (awaitSubscribeAck) branches on len(rejected) == 0
// via nil-safety, but nil is the documented contract for "no rejection".
func TestDecodeSpotRejections_OrdinaryAck(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"spot@public.aggre.bookTicker.v3.api.pb@100ms@BTCUSDT"}`)
	got := decodeSpotRejections(payload)
	if got != nil {
		t.Errorf("got %#v, want nil for an ordinary ack", got)
	}
}

// TestDecodeSpotRejections_Pong confirms a heartbeat PONG control frame (no
// "msg" field carrying rejection text) yields nil.
func TestDecodeSpotRejections_Pong(t *testing.T) {
	payload := []byte(`{"channel":"pong"}`)
	got := decodeSpotRejections(payload)
	if got != nil {
		t.Errorf("got %#v, want nil for a PONG frame", got)
	}
}

// TestDecodeSpotRejections_MalformedJSON confirms invalid JSON yields nil
// rather than panicking — a defensive case, since a WS text frame is
// attacker- or bug-controlled, not a trusted internal value.
func TestDecodeSpotRejections_MalformedJSON(t *testing.T) {
	got := decodeSpotRejections([]byte(`not json at all`))
	if got != nil {
		t.Errorf("got %#v, want nil for malformed JSON", got)
	}
}

// TestDecodeSpotRejections_TruncatedSegment confirms a "Not Subscribed"
// marker with no closing "]" (a truncated/malformed segment) does not panic
// and yields nil rather than a partial/garbage entry — the loop's `end < 0`
// break must fire before any map write.
func TestDecodeSpotRejections_TruncatedSegment(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"Not Subscribed successfully! [chanA missing the closing bracket"}`)
	got := decodeSpotRejections(payload)
	if got != nil {
		t.Errorf("got %#v, want nil for a truncated segment with no closing ']'", got)
	}
}

// TestDecodeSpotRejections_ASCIIColonDoesNotMatch is the regression guard
// this file exists for: the exchange's real marker is the full-width colon
// U+FF1A, not ASCII ':'. If a future edit ever reverts \uFF1A back to a
// plain ':' (breaking the live-format match while looking superficially
// similar), this test catches it — an ASCII-colon "Reason:" must NOT be
// recognized as the reason marker, so the extracted reason stays empty (the
// channel is still reported as rejected via the "Not Subscribed" marker,
// just with no reason text extracted).
func TestDecodeSpotRejections_ASCIIColonDoesNotMatch(t *testing.T) {
	payload := []byte(`{"id":1,"code":0,"msg":"Not Subscribed successfully! [chanA].  Reason: Blocked!"}`)
	got := decodeSpotRejections(payload)
	want := map[string]string{"chanA": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v (ASCII ':' must not match the \\uFF1A marker)", got, want)
	}
}
