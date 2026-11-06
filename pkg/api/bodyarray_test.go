package api

import (
	"strings"
	"testing"
)

// orderItem carries one of every tag the framework projects, so these tests can
// prove each is honored PER ITEM of an array body rather than silently skipped —
// which is what happened before a slice prototype was unwrapped.
type orderItem struct {
	Symbol string `json:"symbol" validate:"required" desc:"trading pair"`
	Side   string `json:"side"   validate:"required" enum:"BUY,SELL"`
	Qty    int    `json:"qty"    minval:"1" maxval:"100"`
	Note   string `json:"note"   maxlen:"8"`
	Token  string `json:"token"  sensitive:"true"`
}

func arrayBodyOp() Operation {
	return Operation{
		Name: "batch-place", Method: "POST", Path: "/api/v3/batchOrders",
		Access: Write, Summary: "place several orders", Body: []orderItem{},
	}
}

func objectBodyOp() Operation {
	return Operation{
		Name: "place", Method: "POST", Path: "/api/v3/order",
		Access: Write, Summary: "place one order", Body: orderItem{},
	}
}

func TestBodyShape(t *testing.T) {
	tests := []struct {
		name  string
		proto any
		want  string
	}{
		{"struct prototype", orderItem{}, BodyShapeObject},
		{"pointer to struct", &orderItem{}, BodyShapeObject},
		{"slice of structs", []orderItem{}, BodyShapeArray},
		{"slice of pointers", []*orderItem{}, BodyShapeArray},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: tt.proto}
			if got := op.BodyShape(); got != tt.want {
				t.Fatalf("BodyShape() = %q, want %q", got, tt.want)
			}
		})
	}
	if got := (Operation{Name: "x", Method: "GET", Path: "/x", Access: Read, Summary: "x"}).BodyShape(); got != "" {
		t.Fatalf("BodyShape() with no Body = %q, want empty", got)
	}
}

// The field tree of an array body describes ONE ITEM, and is identical to the
// tree the same struct produces as an object body. Everything downstream —
// validation, `schema`, the MCP item schema — reads this one projection, so if
// the two ever diverge the shapes stop agreeing with each other.
func TestBodyFields_ArrayDescribesOneItem(t *testing.T) {
	arr := arrayBodyOp().BodyFields()
	obj := objectBodyOp().BodyFields()

	if len(arr) == 0 {
		t.Fatal("an array body reflected into no fields — the slice was not unwrapped")
	}
	if len(arr) != len(obj) {
		t.Fatalf("array body has %d fields, object body %d — they must describe the same struct", len(arr), len(obj))
	}
	for i := range arr {
		if arr[i].Name != obj[i].Name || arr[i].Type != obj[i].Type || arr[i].Required != obj[i].Required {
			t.Fatalf("field %d differs: array %+v vs object %+v", i, arr[i], obj[i])
		}
	}
}

func TestSchema_BodyShape(t *testing.T) {
	arr := arrayBodyOp().Schema("spot.trade.batch-place", SignNone, IdentityNone)
	if arr.Body == nil {
		t.Fatal("no body in the schema")
	}
	if arr.Body.Shape != BodyShapeArray {
		t.Fatalf("Shape = %q, want %q", arr.Body.Shape, BodyShapeArray)
	}
	if len(arr.Body.Fields) == 0 {
		t.Fatal("an array body published no item fields")
	}

	obj := objectBodyOp().Schema("spot.trade.place", SignNone, IdentityNone)
	if obj.Body.Shape != BodyShapeObject {
		t.Fatalf("Shape = %q, want %q", obj.Body.Shape, BodyShapeObject)
	}
}

func TestSchema_ScalarArrayBodyShape(t *testing.T) {
	op := Operation{
		Name: "cancel", Method: "POST", Path: "/api/v1/order/cancel",
		Access: Write, Summary: "cancel by ids", Body: []int64{},
	}
	got := op.Schema("contract.trade.cancel", SignNone, IdentityNone)
	if got.Body == nil {
		t.Fatal("no body in the schema")
	}
	if got.Body.Shape != BodyShapeArray {
		t.Fatalf("Shape = %q, want %q", got.Body.Shape, BodyShapeArray)
	}
	if got.Body.ItemType != "integer" {
		t.Fatalf("ItemType = %q, want integer", got.Body.ItemType)
	}
	if len(got.Body.Fields) != 0 {
		t.Fatalf("scalar array body should publish no object fields, got %+v", got.Body.Fields)
	}
}

func TestValidateBody_ArrayAccepted(t *testing.T) {
	op := arrayBodyOp()
	data := `[{"symbol":"BTCUSDT","side":"BUY","qty":1},{"symbol":"ETHUSDT","side":"SELL","qty":2}]`
	if err := op.ValidateBody(data); err != nil {
		t.Fatalf("a well-formed array body must pass: %v", err)
	}
}

func TestValidateBody_ScalarArrayAccepted(t *testing.T) {
	op := Operation{
		Name: "cancel", Method: "POST", Path: "/api/v1/order/cancel",
		Access: Write, Summary: "cancel by ids", Body: []int64{},
	}
	if err := op.ValidateBody(`[123,456]`); err != nil {
		t.Fatalf("a well-formed scalar array body must pass: %v", err)
	}
	if err := op.ValidateBody(`[]`); err == nil || !strings.Contains(err.Error(), "empty array") {
		t.Fatalf("an empty scalar array body must be refused, got: %v", err)
	}
	if err := op.ValidateBody(`["123"]`); err == nil || !strings.Contains(err.Error(), "--data") {
		t.Fatalf("a wrong scalar item type must be refused, got: %v", err)
	}
}

// Every per-field rule applies inside an item, and the failure names WHICH item.
// Before the slice was unwrapped none of these fired: the field tree was empty,
// so a body of any shape passed.
func TestValidateBody_ArrayRulesApplyPerItem(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantItem string // the item prefix the message must carry
		wantText string // the rule that must be named
	}{
		{
			name:     "required field missing in the second item",
			data:     `[{"symbol":"BTCUSDT","side":"BUY"},{"side":"BUY"}]`,
			wantItem: "item 1",
			wantText: `"symbol"`,
		},
		{
			name:     "enum violated in the first item",
			data:     `[{"symbol":"BTCUSDT","side":"HOLD"}]`,
			wantItem: "item 0",
			wantText: `"side"`,
		},
		{
			name:     "numeric bound violated in the third item",
			data:     `[{"symbol":"A","side":"BUY"},{"symbol":"B","side":"BUY"},{"symbol":"C","side":"BUY","qty":9999}]`,
			wantItem: "item 2",
			wantText: "must be <= 100",
		},
		{
			name:     "string length violated in an item",
			data:     `[{"symbol":"A","side":"BUY","note":"far too long"}]`,
			wantItem: "item 0",
			wantText: "at most 8 characters",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := arrayBodyOp().ValidateBody(tt.data)
			if err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantItem) {
				t.Fatalf("error does not name the failing item (%s): %v", tt.wantItem, err)
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error does not name the rule (%s): %v", tt.wantText, err)
			}
			if !strings.Contains(err.Error(), "--data") {
				t.Fatalf("error lost the --data param: %v", err)
			}
		})
	}
}

// The strict decode runs against the prototype's own type, so it rejects an
// unknown key at item depth just as it does at body depth.
func TestValidateBody_ArrayUnknownFieldInItem(t *testing.T) {
	err := arrayBodyOp().ValidateBody(`[{"symbol":"BTCUSDT","side":"BUY","bogus":1}]`)
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("an unknown key inside an item must be rejected, got: %v", err)
	}
}

// A batch of nothing is the array analogue of an empty body, which ValidateBody
// already refuses.
func TestValidateBody_ArrayEmptyRefused(t *testing.T) {
	err := arrayBodyOp().ValidateBody(`[]`)
	if err == nil || !strings.Contains(err.Error(), "empty array") {
		t.Fatalf("an empty array body must be refused, got: %v", err)
	}
}

// JSON null decodes into a struct element without error, so the strict decode
// lets it through and the per-item loop is what has to catch it.
func TestValidateBody_ArrayNullItem(t *testing.T) {
	err := arrayBodyOp().ValidateBody(`[{"symbol":"A","side":"BUY"},null]`)
	if err == nil || !strings.Contains(err.Error(), "item 1 is not a JSON object") {
		t.Fatalf("a null item must be rejected by position, got: %v", err)
	}
}

// The declared shape is enforced in both directions: the prototype's type is what
// the strict decode runs against, so neither substitution reaches the field walk.
func TestValidateBody_ShapeMismatchRefused(t *testing.T) {
	if err := arrayBodyOp().ValidateBody(`{"symbol":"BTCUSDT","side":"BUY"}`); err == nil {
		t.Fatal("an object passed to an array body must be refused")
	}
	if err := objectBodyOp().ValidateBody(`[{"symbol":"BTCUSDT","side":"BUY"}]`); err == nil {
		t.Fatal("an array passed to an object body must be refused")
	}
}

// The array branch decodes with UseNumber like the object branch does, so an
// integer enum beyond float64's exact range still compares exactly. Decoded as a
// float64 this value would round to ...992 and the match would be wrong in both
// directions.
func TestValidateBody_ArrayIntegerEnumLargeValue(t *testing.T) {
	type bigItem struct {
		ID int64 `json:"id" enum:"9007199254740993,1"`
	}
	op := Operation{
		Name: "big", Method: "POST", Path: "/big",
		Access: Write, Summary: "big", Body: []bigItem{},
	}
	if err := op.ValidateBody(`[{"id":9007199254740993}]`); err != nil {
		t.Fatalf("the exact enum value must be accepted: %v", err)
	}
	if err := op.ValidateBody(`[{"id":9007199254740992}]`); err == nil {
		t.Fatal("the neighbouring value must be rejected — the comparison went through float64")
	}
}

func TestMaskSensitiveBody_Array(t *testing.T) {
	op := arrayBodyOp()
	got := op.MaskSensitiveBody(`[{"symbol":"A","token":"secret-one"},{"symbol":"B","token":"secret-two"}]`)
	if strings.Contains(got, "secret-one") || strings.Contains(got, "secret-two") {
		t.Fatalf("a sensitive field survived masking in an array body: %s", got)
	}
	if !strings.Contains(got, SensitiveMask) {
		t.Fatalf("nothing was masked: %s", got)
	}
	if !strings.Contains(got, `"symbol":"A"`) || !strings.Contains(got, `"symbol":"B"`) {
		t.Fatalf("masking dropped non-sensitive content: %s", got)
	}
}

func TestMaskSensitiveBody_ArrayWithoutSensitiveValueIsUntouched(t *testing.T) {
	const data = `[{"symbol":"A","side":"BUY"}]`
	if got := arrayBodyOp().MaskSensitiveBody(data); got != data {
		t.Fatalf("MaskSensitiveBody rewrote a body with nothing to mask: %s", got)
	}
}

func TestMaskSensitiveBody_ArrayMalformedIsUntouched(t *testing.T) {
	const data = `{"symbol":"A"}` // an object where the op declares an array
	if got := arrayBodyOp().MaskSensitiveBody(data); got != data {
		t.Fatalf("MaskSensitiveBody altered a body it could not decode: %s", got)
	}
}

func TestValidateDecl_ArrayBodyAccepted(t *testing.T) {
	if err := arrayBodyOp().ValidateDecl(); err != nil {
		t.Fatalf("a slice-of-structs body must be a legal declaration: %v", err)
	}
}

func TestValidateDecl_ScalarArrayBodyAccepted(t *testing.T) {
	op := Operation{
		Name: "cancel", Method: "POST", Path: "/api/v1/order/cancel",
		Access: Write, Summary: "cancel by ids", Body: []int64{},
	}
	if err := op.ValidateDecl(); err != nil {
		t.Fatalf("a slice-of-scalars body must be a legal declaration: %v", err)
	}
}

// A prototype that reflects into no fields is rejected at declaration time. Left
// accepted, its consequences are all silent: no rule is enforced, `schema` shows
// an empty body, and the MCP tool advertises an empty object.
func TestValidateDecl_UnprojectableBodyRejected(t *testing.T) {
	tests := []struct {
		name  string
		proto any
		want  string
	}{
		{"slice of maps", []map[string]string{}, "slice of structs"},
		{"slice of slices", [][]string{}, "slice of structs or scalars"},
		{"bare string", "payload", "must be a struct"},
		{"map", map[string]string{}, "must be a struct"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: tt.proto}
			err := op.ValidateDecl()
			if err == nil {
				t.Fatal("expected a declaration error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error should say %q, got: %v", tt.want, err)
			}
		})
	}
}

// Declaration checks reach inside the item struct. They did not before: both
// walkers stopped at the slice, so a malformed tag on an item field shipped and
// the field it constrained went unvalidated at runtime.
func TestValidateDecl_ItemTagsAreChecked(t *testing.T) {
	t.Run("constraint tag on the wrong kind", func(t *testing.T) {
		type badItem struct {
			Name string `json:"name" minval:"1"` // numeric bound on a string
		}
		op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: []badItem{}}
		err := op.ValidateDecl()
		if err == nil || !strings.Contains(err.Error(), "minval/maxval") {
			t.Fatalf("a numeric bound on a string item field must be rejected, got: %v", err)
		}
	})

	t.Run("enum on a non-scalar", func(t *testing.T) {
		type nested struct {
			A string `json:"a"`
		}
		type badItem struct {
			Inner nested `json:"inner" enum:"x,y"` // enum on an object
		}
		op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: []badItem{}}
		err := op.ValidateDecl()
		if err == nil || !strings.Contains(err.Error(), "enum") {
			t.Fatalf("an enum tag on an object item field must be rejected, got: %v", err)
		}
	})

	t.Run("uncompilable pattern", func(t *testing.T) {
		type badItem struct {
			Name string `json:"name" pattern:"([a-z"`
		}
		op := Operation{Name: "x", Method: "POST", Path: "/x", Access: Write, Summary: "x", Body: []badItem{}}
		err := op.ValidateDecl()
		if err == nil || !strings.Contains(err.Error(), "pattern") {
			t.Fatalf("an invalid pattern on an item field must be rejected, got: %v", err)
		}
	})
}

// An array body is still a body: the method rule and the mutual exclusion with
// the other body kinds apply unchanged.
func TestValidateDecl_ArrayBodyObeysTheBodyRules(t *testing.T) {
	get := arrayBodyOp()
	get.Method = "GET"
	if err := get.ValidateDecl(); err == nil {
		t.Fatal("an array body on GET must be rejected")
	}

	both := arrayBodyOp()
	both.Multipart = &MultipartDecl{Files: []MultipartFile{{Field: "file"}}}
	if err := both.ValidateDecl(); err == nil {
		t.Fatal("an array body plus Multipart must be rejected")
	}
}

// BuildSpec attaches the payload verbatim whatever its shape — the transport has
// never cared, and this pins that the array support did not introduce a rewrite.
func TestBuildSpec_ArrayBodyPassesThroughVerbatim(t *testing.T) {
	const data = `[{"symbol":"BTCUSDT","side":"BUY"}]`
	spec, err := arrayBodyOp().BuildSpec(map[string]string{}, data, nil, SignNone, IdentityNone, "h")
	if err != nil {
		t.Fatalf("BuildSpec err: %v", err)
	}
	if string(spec.Body) != data {
		t.Fatalf("body = %q, want it unchanged at %q", string(spec.Body), data)
	}
}
