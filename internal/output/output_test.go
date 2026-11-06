package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

func decodeEnvelope(t *testing.T, b []byte) errorBody {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\n%s", err, b)
	}
	return env.Error
}

func TestWriteError_FullEnvelope(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, errs.Validation(errs.SubtypeInvalidArgument, "bad symbol").
		WithParam("--symbol").WithHint("use BTCUSDT"))

	body := decodeEnvelope(t, buf.Bytes())
	if body.Type != "validation" || body.Subtype != "invalid_argument" {
		t.Fatalf("type/subtype wrong: %+v", body)
	}
	if body.Param != "--symbol" || body.Hint != "use BTCUSDT" {
		t.Fatalf("param/hint wrong: %+v", body)
	}
}

func TestWriteError_OmitsEmptyOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, errs.Network("connection refused"))

	// param / hint / code should not appear in the JSON when unset (omitempty).
	s := buf.String()
	for _, k := range []string{`"param"`, `"hint"`, `"code"`} {
		if bytes.Contains(buf.Bytes(), []byte(k)) {
			t.Fatalf("expected %s to be omitted, got: %s", k, s)
		}
	}
}

func TestWriteError_PlainErrorBecomesInternal(t *testing.T) {
	var buf bytes.Buffer
	WriteError(&buf, errors.New("boom"))
	body := decodeEnvelope(t, buf.Bytes())
	if body.Type != "internal" {
		t.Fatalf("plain error should map to internal, got %q", body.Type)
	}
}

func TestJSON_WritesData(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, map[string]string{"price": "100"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"price": "100"`)) {
		t.Fatalf("unexpected output: %s", buf.String())
	}
}
