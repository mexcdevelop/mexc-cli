package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	port "github.com/mexcdevelop/mexc-cli/pkg/transport"
)

const testBuildHeader = "X-MEXC-CLI-BUILD-VERSION"

// No header named means nil, the dialer's documented "no extra headers" value.
func TestHandshakeHeader_AbsentWhenUnnamed(t *testing.T) {
	if h := (Collector{}).handshakeHeader(); h != nil {
		t.Fatalf("unnamed header produced %v, want nil", h)
	}
}

// The key must reach the dialer already canonicalized, since gorilla copies it
// verbatim. Asserted client-side because net/http canonicalizes inbound names
// while parsing, so no server-side check can tell the spellings apart.
func TestHandshakeHeader_KeyIsCanonicalizedLikeREST(t *testing.T) {
	h := Collector{BuildVersionHeader: testBuildHeader}.handshakeHeader()

	rest := http.Header{}
	rest.Set(testBuildHeader, buildinfo.Version) // what the REST path does

	wsKeys := keysOf(h)
	restKeys := keysOf(rest)
	if len(wsKeys) != 1 || len(restKeys) != 1 || wsKeys[0] != restKeys[0] {
		t.Fatalf("WS spells the header %v, REST spells it %v — they must match", wsKeys, restKeys)
	}
	if wsKeys[0] == testBuildHeader {
		t.Fatalf("key %q was passed through raw; it must be canonicalized", wsKeys[0])
	}
	if got := h.Get(testBuildHeader); got != buildinfo.Version {
		t.Fatalf("header value = %q, want the build version %q", got, buildinfo.Version)
	}
}

// End to end over a real upgrade. The server closes right after it, so Collect
// fails; the handshake is the point.
func TestCollect_HandshakeCarriesTheBuildVersion(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get(testBuildHeader)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c := Collector{BuildVersionHeader: testBuildHeader}
	_ = c.Collect(context.Background(), port.WSConfig{
		URL:      "ws" + strings.TrimPrefix(srv.URL, "http"),
		Codec:    port.CodecFuturesEdge,
		Channels: []string{"sub.ticker:BTC_USDT"},
	})

	select {
	case got := <-seen:
		if got != buildinfo.Version {
			t.Fatalf("server saw %q, want the build version %q", got, buildinfo.Version)
		}
	default:
		t.Fatal("the server never saw a handshake")
	}
}

// Naming none must send none, over a real upgrade too.
func TestCollect_NoBuildVersionHeaderWhenUnnamed(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get(testBuildHeader)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	_ = Collector{}.Collect(context.Background(), port.WSConfig{
		URL:      "ws" + strings.TrimPrefix(srv.URL, "http"),
		Codec:    port.CodecFuturesEdge,
		Channels: []string{"sub.ticker:BTC_USDT"},
	})

	select {
	case got := <-seen:
		if got != "" {
			t.Fatalf("an unnamed header still reached the server as %q", got)
		}
	default:
		t.Fatal("the server never saw a handshake")
	}
}

func keysOf(h http.Header) []string {
	out := make([]string, 0, len(h))
	for k := range h {
		out = append(out, k)
	}
	return out
}
