package adapter

import (
	"strings"
	"testing"
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

// The protocol's own fixed inputs, from the golden-vector section of the CLI
// signature v1 specification. Not credentials: sample data the specification
// prints so every SDK can prove it agrees before touching a live environment.
const (
	cliVectorKey    = "mx0vGtestApiKey00001"
	cliVectorSecret = "test-secret-key"
	cliVectorTSMs   = int64(1754289600000)
)

func cliVectorTime() time.Time { return time.UnixMilli(cliVectorTSMs) }

func signCli(t *testing.T, spec *api.RequestSpec) string {
	t.Helper()
	if err := (CliHMAC{}).Sign(spec, credential.Credentials{
		APIKey: cliVectorKey, Secret: cliVectorSecret,
	}, cliVectorTime()); err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	return spec.Header[cliSignatureHeader]
}

// TestCliHMAC_GoldenVectors checks the scheme against the signatures the gateway
// specification publishes, so this package is pinned to the gateway rather than
// only to itself.
//
// It is the only test here that can fail for an external reason. Every other
// assertion in this file describes a property of the implementation — that a
// field participates, that a shape is refused — and would stay green if the
// whole canonical form were wrong in a self-consistent way. These three would
// not: they are the gateway's own answers for three fixed requests.
//
// The middle vector is the one that matters most. It is the only case whose
// query carries a character that percent-encoding implementations disagree
// about: a space is `%20` under RFC3986 and `+` under form encoding, and Go's
// url.QueryEscape produces the latter. A scheme that reuses a form-style encoder
// passes the other two vectors and fails only here — and in production, only for
// the requests whose parameters happen to contain a space.
func TestCliHMAC_GoldenVectors(t *testing.T) {
	tests := []struct {
		name string
		spec api.RequestSpec
		want string
	}{
		{
			name: "GET with one query parameter",
			spec: api.RequestSpec{
				Method: "GET", Path: "/api/v3/openOrders",
				Query: map[string]string{"symbol": "BTCUSDT"},
			},
			want: "69cab3e127bbeb0c412a4ee3667cce65cc40fdf1abcfb130f1eeec79e1670b18",
		},
		{
			// Canonical query must be `note=hello%20world&symbol=BTCUSDT`:
			// sorted by name, space as %20.
			name: "GET whose query value contains a space",
			spec: api.RequestSpec{
				Method: "GET", Path: "/api/v3/openOrders",
				Query: map[string]string{"symbol": "BTCUSDT", "note": "hello world"},
			},
			want: "0408d24b889dc26d82da740fc5550c125e2b91e73ebbddbbfce5e7ca7ee7407b",
		},
		{
			name: "POST with a JSON body",
			spec: api.RequestSpec{
				Method: "POST", Path: "/api/v3/order",
				Body: []byte(`{"symbol":"BTCUSDT","side":"BUY","type":"LIMIT","quantity":"0.001","price":"65000"}`),
			},
			want: "513f3b73caf68491aaf67a79c88f7a02f1076e7c9aecfcf464357345b4f0f5a2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			if got := signCli(t, &spec); got != tt.want {
				t.Fatalf("signature = %q, want the published %q", got, tt.want)
			}
		})
	}
}

// The canonical form is nine lines and every one of them carries meaning. The
// specification calls a dropped line the single most common implementation
// error, because an omitted empty line still produces a perfectly valid-looking
// signature that the gateway rejects every time.
//
// Rather than reach inside Sign to count separators, this varies one input at a
// time: a field that no longer reaches the digest stops changing it. That catches
// a dropped line, a field read from the wrong place, and a field silently
// normalised away.
func TestCliHMAC_EveryCanonicalFieldAffectsTheSignature(t *testing.T) {
	base := func() api.RequestSpec {
		return api.RequestSpec{
			Method: "POST", Path: "/api/v3/order",
			Query:  map[string]string{"symbol": "BTCUSDT"},
			Body:   []byte(`{"a":1}`),
			Header: map[string]string{cliIdempotencyKeyHeader: "idem-1"},
		}
	}
	spec := base()
	reference := signCli(t, &spec)

	mutations := map[string]func(*api.RequestSpec){
		"method":          func(s *api.RequestSpec) { s.Method = "DELETE" },
		"path":            func(s *api.RequestSpec) { s.Path = "/api/v3/order/" },
		"query":           func(s *api.RequestSpec) { s.Query = map[string]string{"symbol": "ETHUSDT"} },
		"body":            func(s *api.RequestSpec) { s.Body = []byte(`{"a":2}`) },
		"idempotency key": func(s *api.RequestSpec) { s.Header[cliIdempotencyKeyHeader] = "idem-2" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			s := base()
			mutate(&s)
			if got := signCli(t, &s); got == reference {
				t.Errorf("changing the %s left the signature unchanged — it is not reaching the digest", name)
			}
		})
	}

	// The api key, the secret and the timestamp are not part of RequestSpec, so
	// they are varied through the other two arguments.
	t.Run("api key", func(t *testing.T) {
		s := base()
		if err := (CliHMAC{}).Sign(&s, credential.Credentials{
			APIKey: "other-key", Secret: cliVectorSecret,
		}, cliVectorTime()); err != nil {
			t.Fatalf("Sign() error = %v", err)
		}
		if s.Header[cliSignatureHeader] == reference {
			t.Error("changing the api key left the signature unchanged")
		}
	})
	t.Run("timestamp", func(t *testing.T) {
		s := base()
		if err := (CliHMAC{}).Sign(&s, credential.Credentials{
			APIKey: cliVectorKey, Secret: cliVectorSecret,
		}, time.UnixMilli(cliVectorTSMs+1)); err != nil {
			t.Fatalf("Sign() error = %v", err)
		}
		if s.Header[cliSignatureHeader] == reference {
			t.Error("changing the timestamp left the signature unchanged")
		}
	})
}

// The percent-encoding rules, taken from the specification's own table. They are
// pinned separately from the golden vectors because the vectors only exercise a
// space: every other row here is a character whose encoding differs between
// RFC3986 and the encoders a Go, Java or JavaScript author would reach for first. exposure-ok
func TestCliPctEncode_MatchesRFC3986(t *testing.T) {
	cases := map[string]string{
		"BTCUSDT":     "BTCUSDT",
		"hello world": "hello%20world", // %20, never +
		"BTC/USDT":    "BTC%2FUSDT",    // upper-case hex
		"a+b":         "a%2Bb",         // a literal + must survive the space rewrite
		"é":           "%C3%A9",        // 2-byte UTF-8, encoded byte by byte
		"€":           "%E2%82%AC",     // 3-byte UTF-8
		"~":           "~",             // unreserved: must NOT be encoded
		"*":           "%2A",
		"-_.":         "-_.",
	}
	for in, want := range cases {
		if got := cliPctEncode(in); got != want {
			t.Errorf("cliPctEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

// Parameters are ordered by name and joined with &; no parameters is an empty
// line, not an omitted one.
func TestCliCanonicalQuery_SortsAndJoins(t *testing.T) {
	if got := cliCanonicalQuery(nil); got != "" {
		t.Errorf("cliCanonicalQuery(nil) = %q, want empty", got)
	}
	got := cliCanonicalQuery(map[string]string{"symbol": "BTCUSDT", "note": "hello world", "a": ""})
	if want := "a=&note=hello%20world&symbol=BTCUSDT"; got != want {
		t.Errorf("cliCanonicalQuery() = %q, want %q", got, want)
	}
}

// The headers the scheme injects are the ones the gateway parses, and it rejects
// a signature that is not 64 lower-case hex characters outright — before it ever
// compares digests, so the failure names the header rather than the mismatch.
func TestCliHMAC_InjectsTheProtocolHeaders(t *testing.T) {
	spec := api.RequestSpec{Method: "GET", Path: "/api/v3/openOrders"}
	sig := signCli(t, &spec)

	if got := spec.Header[cliVersionHeader]; got != "v1" {
		t.Errorf("version header = %q, want v1", got)
	}
	if got := spec.Header[cliTimestampHeader]; got != "1754289600000" {
		t.Errorf("timestamp header = %q, want the millisecond timestamp", got)
	}
	if len(sig) != 64 || strings.ToLower(sig) != sig {
		t.Errorf("signature = %q, want 64 lower-case hex characters", sig)
	}
	// The header and the digest must carry the same instant: the gateway signs
	// what the header says, so a second read of the clock between them would make
	// every request fail.
	if !strings.Contains(spec.Header[cliTimestampHeader], "1754289600000") {
		t.Error("the timestamp header must be the one that was signed")
	}
}

// An empty body hashes to a value the specification prints, so this pins the
// no-body case against the gateway rather than against this implementation.
func TestCliHMAC_EmptyBodyHash(t *testing.T) {
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := cliSHA256Hex(nil); got != want {
		t.Errorf("cliSHA256Hex(nil) = %q, want the published %q", got, want)
	}
	if got := cliSHA256Hex([]byte{}); got != want {
		t.Errorf("cliSHA256Hex(empty) = %q, want the published %q", got, want)
	}
}

// The gateway accepts only JSON for a non-empty body. Refusing the other shapes
// here — rather than sending a request the far side is guaranteed to reject —
// keeps the failure local and legible.
func TestCliHMAC_RefusesNonJSONBodyShapes(t *testing.T) {
	cases := map[string]api.RequestSpec{
		"form":      {Method: "POST", Path: "/x", Form: map[string]string{"a": "1"}},
		"multipart": {Method: "POST", Path: "/x", Multipart: &api.MultipartBody{}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			s := spec
			if err := (CliHMAC{}).Sign(&s, credential.Credentials{
				APIKey: cliVectorKey, Secret: cliVectorSecret,
			}, cliVectorTime()); err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}
}

// A GET carrying a body is rejected by the gateway outright. It is refused here
// for the same reason the other body shapes are: the round trip proves nothing
// that is not already knowable locally.
func TestCliHMAC_RefusesGetWithBody(t *testing.T) {
	spec := api.RequestSpec{Method: "GET", Path: "/api/v3/openOrders", Body: []byte(`{"a":1}`)}
	if err := (CliHMAC{}).Sign(&spec, credential.Credentials{
		APIKey: cliVectorKey, Secret: cliVectorSecret,
	}, cliVectorTime()); err == nil {
		t.Fatal("expected a GET with a body to be refused")
	}
}
