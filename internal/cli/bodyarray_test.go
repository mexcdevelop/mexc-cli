package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

// batchItem is one item of an array body: a required field and an enum, so the
// end-to-end tests can prove the per-item rules run on the real command path and
// not only in pkg/api's unit tests.
type batchItem struct {
	Symbol string `json:"symbol" validate:"required" desc:"trading pair"`
	Side   string `json:"side" enum:"BUY,SELL" desc:"order side"`
}

// arrayBodyDomains mounts one unsigned, no-identity op whose body is a JSON
// array. Unsigned keeps credentials out of the picture: what is under test is the
// body path from --data to the wire.
func arrayBodyDomains() []api.DomainSpec {
	return []api.DomainSpec{{
		Name:  "spot",
		Short: "Spot",
		Subs: []api.Subdomain{{
			Name:     "batch",
			Short:    "Batch operations",
			Signed:   api.SignNone,
			Identity: api.IdentityNone,
			HostKey:  testHostKey,
			Ops: []api.Operation{{
				Name: "place", Method: "POST", Path: "/api/v3/batchOrders",
				Access: api.Write, Summary: "place several orders in one call",
				Body: []batchItem{},
			}},
		}},
	}}
}

func TestArrayBody_MountsAndDryRuns(t *testing.T) {
	stdout, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--dry-run",
		"--data", `[{"symbol":"BTCUSDT","side":"BUY"},{"symbol":"ETHUSDT","side":"SELL"}]`)
	if code != 0 {
		t.Fatalf("an array-body op must mount and dry-run: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "/api/v3/batchOrders") {
		t.Fatalf("plan is missing the path: %s", stdout)
	}
	if !strings.Contains(stdout, `"body_kind": "json"`) {
		t.Fatalf("plan should report a json body: %s", stdout)
	}
}

// A body failure reaches the user through the real command, naming the item.
// That is the whole point of validating an array rather than waving it through:
// with twenty items, "symbol is missing" alone is not actionable.
//
// These run WITHOUT --dry-run on purpose. A dry-run skips ValidateBody entirely
// (mount.go: a preview must not demand a filled-in body), so asserting a
// rejection there would assert nothing. The server is wired up only to catch the
// opposite failure — validation must reject before anything is sent.
func TestArrayBody_RejectsABadBodyBeforeSending(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []string
	}{
		{
			name: "required field missing in the second item",
			data: `[{"symbol":"BTCUSDT","side":"BUY"},{"side":"BUY"}]`,
			want: []string{"item 1", "symbol"},
		},
		{
			name: "enum violated in the first item",
			data: `[{"symbol":"BTCUSDT","side":"HOLD"}]`,
			want: []string{"item 0", "side"},
		},
		{
			name: "empty array",
			data: `[]`,
			want: []string{"empty array"},
		},
		{
			name: "an object where the op declares an array",
			data: `{"symbol":"BTCUSDT","side":"BUY"}`,
			want: []string{"--data"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("a request was sent despite an invalid body")
			}))
			defer srv.Close()

			_, stderr, code := runWith(arrayBodyDomains(), nil,
				"spot", "batch", "place", "--yes", "--base-url", srv.URL, "--data", tt.data)
			if code == 0 {
				t.Fatalf("an invalid body must fail the command, stderr=%s", stderr)
			}
			for _, w := range tt.want {
				if !strings.Contains(stderr, w) {
					t.Fatalf("error must mention %q, got: %s", w, stderr)
				}
			}
		})
	}
}

// The closing link: the array reaches the server byte-for-byte, as JSON, on a
// real send. Everything above this line is validation; this is the part that
// proves nothing in the assembly or transport path rewrites or drops it.
func TestArrayBody_SendsTheArrayVerbatim(t *testing.T) {
	const payload = `[{"symbol":"BTCUSDT","side":"BUY"},{"symbol":"ETHUSDT","side":"SELL"}]`
	var gotBody, gotContentType, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	_, stderr, code := runWith(arrayBodyDomains(), nil,
		"spot", "batch", "place", "--yes", "--base-url", srv.URL, "--data", payload)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	if gotMethod != "POST" {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotBody != payload {
		t.Fatalf("body = %q, want it delivered unchanged as %q", gotBody, payload)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
}

// `schema` has to say WHICH shape --data takes, or an agent reading it would
// send an object. The fields it lists describe one item.
func TestArrayBody_SchemaPublishesTheShape(t *testing.T) {
	stdout, stderr, code := runWith(arrayBodyDomains(), nil, "schema", "spot.batch.place")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	var got struct {
		Body struct {
			Shape  string `json:"shape"`
			Fields []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"fields"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("schema output is not valid JSON: %v\n%s", err, stdout)
	}
	if got.Body.Shape != api.BodyShapeArray {
		t.Fatalf("body shape = %q, want %q\n%s", got.Body.Shape, api.BodyShapeArray, stdout)
	}
	var sawSymbol bool
	for _, f := range got.Body.Fields {
		if f.Name == "symbol" && f.Required {
			sawSymbol = true
		}
	}
	if !sawSymbol {
		t.Fatalf("schema should list the item's required fields, got: %s", stdout)
	}
}
