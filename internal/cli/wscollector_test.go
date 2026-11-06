package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// The fixture mounts a KindWSCollect op; this one does not.
var wsFree = []api.DomainSpec{{Name: "sample", Subs: []api.Subdomain{{
	Name: "public", Ops: []api.Operation{{Name: "price", Access: api.Read}},
}}}}

func TestValidateWSCollector(t *testing.T) {
	t.Run("a WS op without a collector is refused", func(t *testing.T) {
		err := validateWSCollector(domains(), false)
		if err == nil {
			t.Fatal("a build mounting a WS op with no collector must fail assembly")
		}
		var e *errs.Error
		if !errors.As(err, &e) || e.Type != errs.TypeInternal {
			t.Fatalf("want an internal wiring error, got %v", err)
		}
	})
	t.Run("a WS op with a collector is accepted", func(t *testing.T) {
		if err := validateWSCollector(domains(), true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("a WS-free build needs no collector", func(t *testing.T) {
		if err := validateWSCollector(wsFree, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// The setter is not the only way in: an App built without it leaves the field
// nil, and the WS path must refuse rather than dereference it. run() injects a
// stub, so this builds the App directly to reach the nil.
func TestWSCollector_NilIsRefusedNotPanicked(t *testing.T) {
	var out, errBuf bytes.Buffer
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &out, &errBuf)
	code := app.Run([]string{"sample", "stream", "ws-ticker", "--symbol", "X", "--limit", "1"})
	if code == 0 {
		t.Fatalf("want a non-zero exit, got 0 (stderr=%s)", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "no WS collector") {
		t.Errorf("want the named wiring error, got %s", errBuf.String())
	}
}
