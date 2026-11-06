package api

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestCanonicalQuery_SortedAndEscaped locks the single wire canonical form that
// both the transport (internal/transport, when sending) and the signer (pkg/signer, when
// signing) must agree on byte-for-byte, or a signed request is rejected upstream.
func TestCanonicalQuery_SortedAndEscaped(t *testing.T) {
	q := map[string]string{"symbol": "BTC USDT", "b": "1", "a&x": "y=z"}
	// Keys sorted: "a&x" < "b" < "symbol"; values are query-escaped
	// (space -> '+', '&' -> %26, '=' -> %3D).
	want := "a%26x=y%3Dz&b=1&symbol=BTC+USDT"
	if got := CanonicalQuery(q); got != want {
		t.Fatalf("CanonicalQuery = %q, want %q", got, want)
	}
}

// TestCanonicalQuery_PlusIsPercentEncodedNotSpace ensures a literal '+' is
// query-escaped as %2B rather than treated as a space.
func TestCanonicalQuery_PlusIsPercentEncodedNotSpace(t *testing.T) {
	want := "note=a%2Bb"
	if got := CanonicalQuery(map[string]string{"note": "a+b"}); got != want {
		t.Fatalf("CanonicalQuery = %q, want %q", got, want)
	}
}

func TestCanonicalQuery_Empty(t *testing.T) {
	if got := CanonicalQuery(nil); got != "" {
		t.Fatalf("CanonicalQuery(nil) = %q, want empty", got)
	}
	if got := CanonicalQuery(map[string]string{}); got != "" {
		t.Fatalf("CanonicalQuery(empty) = %q, want empty", got)
	}
}

// TestReflectBodyFields_TypedEnum: a numeric/boolean body field's enum must be
// projected with the field's JSON type (so the schema/MCP faces present [1,2],
// not ["1","2"], keeping the agent-facing contract consistent with the strict
// body decode). String enums stay strings.
func TestReflectBodyFields_TypedEnum(t *testing.T) {
	type req struct {
		Model int    `json:"model" enum:"1,2"`
		On    bool   `json:"on" enum:"true"`
		Level string `json:"level" enum:"c,d"`
	}
	b, err := json.Marshal(reflectBodyFields(reflect.TypeOf(req{})))
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, want := range []string{`"enum":[1,2]`, `"enum":[true]`, `"enum":["c","d"]`} {
		if !strings.Contains(js, want) {
			t.Errorf("want %s in schema, got: %s", want, js)
		}
	}
}
