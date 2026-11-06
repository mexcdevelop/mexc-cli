package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// Validate checks the collected values against the Operation's parameter
// declarations and returns the first structured error it finds. A missing key in
// values is treated as an empty string. The error's param field is set to
// --<name>, and the hint gives recoverable guidance.
//
// This is validation the framework does uniformly, so a business owner does not
// have to hand-write required/enum checks in every operation: declaring them in
// Param is enough.
func (o Operation) Validate(values map[string]string) error {
	for _, p := range o.Params {
		v := values[p.Name]
		if p.Required && v == "" {
			return errs.Validation(errs.SubtypeMissingArgument,
				"required argument --%s is missing", p.Name).
				WithParam("--"+p.Name).
				WithHint("%s", p.Desc)
		}
		if v == "" {
			continue // absent optional param: enum / length / pattern / bounds do not apply
		}
		if len(p.Enum) > 0 && !contains(p.Enum, v) {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--%s value %q is not allowed", p.Name, v).
				WithParam("--"+p.Name).
				WithHint("use one of: %s", join(p.Enum))
		}
		if msg := lenViolation(v, p.MinLen, p.MaxLen); msg != "" {
			return errs.Validation(errs.SubtypeInvalidArgument, "--%s %s", p.Name, msg).
				WithParam("--"+p.Name).WithHint("%s", p.Desc)
		}
		if patternViolation(v, p.Pattern) {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--%s value %q does not match the required pattern %q", p.Name, v, p.Pattern).
				WithParam("--"+p.Name).WithHint("%s", p.Desc)
		}
		if p.MinVal != nil || p.MaxVal != nil {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return errs.Validation(errs.SubtypeInvalidArgument,
					"--%s must be a number", p.Name).WithParam("--"+p.Name).WithHint("%s", p.Desc)
			}
			if msg := boundViolation(f, p.MinVal, p.MaxVal); msg != "" {
				return errs.Validation(errs.SubtypeInvalidArgument, "--%s %s", p.Name, msg).
					WithParam("--"+p.Name).WithHint("%s", p.Desc)
			}
		}
	}
	return nil
}

// ValidateBody validates the raw --data JSON of a Body operation against its
// prototype: no unknown fields and no type mismatches, every field tagged
// validate:"required" present, and every enum-tagged field holding an allowed
// value. The prototype's own shape decides what --data must be — a struct
// prototype expects one JSON object, a slice-of-structs prototype a JSON array of
// objects checked against the same rules, and a slice-of-scalars prototype a raw
// JSON array of primitive values. An operation without a Body takes no body, so
// ValidateBody is a no-op. This is the body counterpart of Validate: deep
// checking the framework does uniformly, so a business owner never hand-writes
// body validation.
func (o Operation) ValidateBody(data string) error {
	if o.Body == nil {
		return nil
	}
	if strings.TrimSpace(data) == "" {
		return errs.Validation(errs.SubtypeMissingArgument,
			"required body --data is missing").
			WithParam("--data").
			WithHint("pass the JSON request body; inspect its shape with `%s schema`", cliname.Name())
	}
	return validateBodyPayload(o.Body, data)
}

// validateBodyPayload validates a non-empty --data against a Body prototype,
// dispatching on the shape the prototype declares. The strict decode is common to
// both shapes: it decodes into the prototype's own type, so that type is what
// rejects an object where an array belongs (and the reverse) before either
// branch runs.
func validateBodyPayload(prototype any, data string) error {
	data = strings.TrimSpace(data)
	if err := strictDecodeBody(prototype, data); err != nil {
		return err
	}
	el, shape := bodyElemType(reflect.TypeOf(prototype))
	fields := reflectBodyFields(el)
	if shape == BodyShapeArray {
		if isScalarBodyType(el) {
			return validateScalarBodyArray(data)
		}
		return validateBodyArray(fields, data)
	}
	m, err := decodeBodyObject(data)
	if err != nil {
		return err
	}
	return validateBodyFields(fields, m)
}

// validateBodyArray checks every item of an array body against the item field
// tree.
//
// An empty array is refused. It is the array analogue of an empty body, which
// ValidateBody already refuses one line up, and a batch request carrying no items
// is a mistake at the keyboard, not an intent — the alternative is a round trip
// that can only come back as an upstream error.
func validateBodyArray(fields []BodyField, data string) error {
	arr, err := decodeBodyArray(data)
	if err != nil {
		return err
	}
	if err := validateNonEmptyArray(arr); err != nil {
		return err
	}
	for i, el := range arr {
		obj, ok := el.(map[string]any)
		if !ok {
			// Reachable for a JSON null: decoding null into a struct element is not an
			// error in Go, so the strict decode above lets it through.
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--data item %d is not a JSON object", i).
				WithParam("--data").
				WithHint("every item of this body is an object; inspect the item shape with `%s schema`", cliname.Name())
		}
		if err := validateBodyFields(fields, obj); err != nil {
			return withItemIndex(err, i)
		}
	}
	return nil
}

func validateScalarBodyArray(data string) error {
	arr, err := decodeBodyArray(data)
	if err != nil {
		return err
	}
	return validateNonEmptyArray(arr)
}

func validateNonEmptyArray(arr []any) error {
	if len(arr) == 0 {
		return errs.Validation(errs.SubtypeMissingArgument,
			"--data is an empty array: the request body carries no items").
			WithParam("--data").
			WithHint("pass at least one item; inspect the item shape with `%s schema`", cliname.Name())
	}
	return nil
}

// withItemIndex prefixes a per-item failure with the item's position, leaving the
// structured error otherwise intact — same subtype, same --data param, same hint.
// Without it, `required body field "symbol" is missing` gives no way to tell which
// of twenty items is wrong.
func withItemIndex(err error, i int) error {
	var e *errs.Error
	if errors.As(err, &e) {
		e.Message = fmt.Sprintf("item %d: %s", i, e.Message)
	}
	return err
}

// strictDecodeBody rejects unknown fields and type mismatches by decoding into a
// fresh instance of the prototype's type (DisallowUnknownFields applies
// recursively, at every depth).
func strictDecodeBody(prototype any, data string) error {
	probe := reflect.New(reflect.TypeOf(prototype)).Interface()
	dec := json.NewDecoder(strings.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(probe); err != nil {
		return errs.Validation(errs.SubtypeInvalidArgument,
			"--data is not a valid request body: %v", err).
			WithParam("--data").
			WithHint("%s", bodyParseHint(data))
	}
	return nil
}

// bodyParseHint picks what to tell the caller after the body failed to parse.
//
// The default — go and check the payload against the schema — is right when the
// payload is what is wrong. It is actively misleading in the case a Windows
// caller is most likely to hit, where the payload was correct and the SHELL
// removed the quotes on the way in. Sent to inspect JSON that is already right,
// an agent finds nothing, concludes the quoting must be wrong, and re-runs with
// a different guess. On this CLI, re-running is re-running a write.
//
// The wording stays platform-neutral even though Windows is where it fires most.
// A misquoted expansion strips the same quotes under bash, and this also reaches
// the MCP face, where no shell was involved at all; naming Windows as the cause
// rather than as the common example would be a wrong lead in both. The advice
// does not vary by platform, which is the point — @<path> ends the question the
// same way everywhere.
//
// The signature of that damage is specific enough to route on: a body that opens
// like JSON but contains no double quote ANYWHERE. Object keys must be quoted,
// so a real object body always has them; measured on Windows,
// `--data '{"symbol":"BTC_USDT"}'` arrives as `{symbol:BTC_USDT}` from
// PowerShell 5.1 and, once the CLI strips the stray outer pair, the same from
// cmd.exe. A quote-free body that DOES parse — a scalar array like [123456] —
// never reaches here, so this only ever fires on a failure.
//
// The hint names @<path> rather than a correct quoting incantation on purpose.
// Telling a caller which escape their shell wants invites one more attempt at
// the same command; telling them to take the shell out of the loop ends the
// question in one move, and ends it the same way on every platform.
func bodyParseHint(data string) string {
	trimmed := strings.TrimSpace(data)
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) &&
		!strings.Contains(trimmed, `"`) {
		return "this body has no double quotes at all, which is what a shell leaves " +
			"behind when it consumes them, so the payload you passed may have been " +
			"correct (PowerShell and cmd.exe both do this to '{\"a\":1}'). Do not retry " +
			"with different quoting: write the JSON to a file and pass `--data @<path>`, " +
			"or pipe it and pass `--data -` — neither carries quoting into the command line"
	}
	return fmt.Sprintf("check the JSON against `%s schema` for this operation", cliname.Name())
}

// decodeBodyObject decodes an object body for the presence/enum pass. UseNumber
// keeps numeric values as json.Number rather than float64: enum comparison on
// integer fields must be exact, and a large int64 would lose precision as a
// float64 (see TestValidateBody_IntegerEnumLargeValue).
func decodeBodyObject(data string) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, errs.Validation(errs.SubtypeInvalidArgument,
			"--data must be a JSON object").
			WithParam("--data").
			WithHint("the request body is an object of fields; see `%s schema`", cliname.Name())
	}
	return m, nil
}

// decodeBodyArray is decodeBodyObject's counterpart for an array body, with the
// same UseNumber contract.
func decodeBodyArray(data string) ([]any, error) {
	var arr []any
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&arr); err != nil {
		return nil, errs.Validation(errs.SubtypeInvalidArgument,
			"--data must be a JSON array").
			WithParam("--data").
			WithHint("the request body is an array of items; see `%s schema`", cliname.Name())
	}
	return arr, nil
}

// ValidateMultipart validates a multipart operation's CLI inputs: the declared
// file fields against what the caller provided (providedFiles maps a declared
// file-field name to whether a path was given), plus the optional --data JSON
// carrying the text fields (checked against the Multipart.Fields prototype). The
// file CONTENTS are read by the assembly layer; this only checks field presence
// and shape. A non-multipart operation is a no-op.
func (o Operation) ValidateMultipart(providedFiles map[string]bool, data string) error {
	if o.Multipart == nil {
		return nil
	}
	declared := make(map[string]bool, len(o.Multipart.Files))
	for _, mf := range o.Multipart.Files {
		declared[mf.Field] = true
		if mf.Required && !providedFiles[mf.Field] {
			return errs.Validation(errs.SubtypeMissingArgument,
				"required file field %q is missing", mf.Field).
				WithParam("--file").
				WithHint("pass it with --file %s=<path>", mf.Field)
		}
	}
	for f := range providedFiles {
		if !declared[f] {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"unknown file field %q for this operation", f).
				WithParam("--file").
				WithHint("inspect the declared file fields with `%s schema`", cliname.Name())
		}
	}
	// Text fields are optional; when the op declares a Fields prototype, validate
	// whatever --data carries against it (empty data is treated as {}, so a
	// required text field still fails closed).
	if o.Multipart.Fields != nil {
		return validateJSONObject(o.Multipart.Fields, data)
	}
	return nil
}

// validateJSONObject validates data against a struct prototype that is always an
// OBJECT: multipart text fields, which have no array form. Empty data is treated
// as an empty object {}, so a required field still fails closed. A JSON body goes
// through validateBodyPayload instead, which dispatches on the declared shape.
func validateJSONObject(prototype any, data string) error {
	data = strings.TrimSpace(data)
	if data == "" {
		data = "{}"
	}
	if err := strictDecodeBody(prototype, data); err != nil {
		return err
	}
	m, err := decodeBodyObject(data)
	if err != nil {
		return err
	}
	return validateBodyFields(reflectBodyFields(reflect.TypeOf(prototype)), m)
}

// validateBodyFields walks the declared body field tree against a decoded JSON
// object, enforcing required presence and enum membership, recursing into
// nested objects and arrays of objects.
func validateBodyFields(fields []BodyField, m map[string]any) error {
	for _, f := range fields {
		v, present := m[f.Name]
		if f.Required && !present {
			return errs.Validation(errs.SubtypeMissingArgument,
				"required body field %q is missing", f.Name).
				WithParam("--data").
				WithHint("%s", f.Desc)
		}
		// Conditional required: "SiblingField:prefix" — the field is required when
		// the sibling's string value starts with the given prefix.
		if f.RequiredIf != "" && !present {
			if parts := strings.SplitN(f.RequiredIf, ":", 2); len(parts) == 2 {
				siblingField := jsonNameForGoField(fields, parts[0])
				prefix := parts[1]
				if sv, ok := m[siblingField]; ok {
					if s, ok := sv.(string); ok && strings.HasPrefix(s, prefix) {
						return errs.Validation(errs.SubtypeMissingArgument,
							"body field %q is required when %q starts with %q", f.Name, siblingField, prefix).
							WithParam("--data").
							WithHint("%s", f.Desc)
					}
				}
			}
		}
		if !present {
			continue
		}
		if len(f.Enum) > 0 && !enumAllows(f, v) {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"body field %q value is not allowed", f.Name).
				WithParam("--data").
				WithHint("use one of: %s", joinEnum(f.Enum))
		}
		// Pre-validation constraints, type-aware (mirrors enumAllows): numeric bounds
		// on an integer/number field, length/pattern on a string field. The strict
		// decode in validateJSONObject already guaranteed v matches the field's kind.
		switch f.Type {
		case "integer", "number":
			if f.MinVal != nil || f.MaxVal != nil {
				if n, ok := v.(json.Number); ok {
					if msg := numBoundViolation(n, f.Type == "integer", f.MinVal, f.MaxVal); msg != "" {
						return errs.Validation(errs.SubtypeInvalidArgument, "body field %q %s", f.Name, msg).
							WithParam("--data").WithHint("%s", f.Desc)
					}
				}
			}
		case "string":
			if s, ok := v.(string); ok {
				if msg := lenViolation(s, f.MinLen, f.MaxLen); msg != "" {
					return errs.Validation(errs.SubtypeInvalidArgument, "body field %q %s", f.Name, msg).
						WithParam("--data").WithHint("%s", f.Desc)
				}
				if patternViolation(s, f.Pattern) {
					return errs.Validation(errs.SubtypeInvalidArgument,
						"body field %q does not match the required pattern %q", f.Name, f.Pattern).
						WithParam("--data").WithHint("%s", f.Desc)
				}
			}
		}
		if len(f.Fields) == 0 {
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			if err := validateBodyFields(f.Fields, child); err != nil {
				return err
			}
		case []any:
			for _, el := range child {
				if obj, ok := el.(map[string]any); ok {
					if err := validateBodyFields(f.Fields, obj); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// enumAllows reports whether v — a value decoded from the JSON body into
// map[string]any with numbers as json.Number — is a member of the enum set
// declared on f. The comparison is type-aware: f.Enum already holds values typed
// by the field's JSON kind (enumValues, at reflect time), so an integer field's
// enum:"1,2" is compared as the numbers 1,2 rather than the strings "1","2". A
// body prototype is a user-defined struct, so a field can legitimately be
// integer/number/boolean, not only string; restricting enum to strings would
// wrongly reject those. Genuine type mismatches (e.g. a string in an int field)
// are already rejected by the strict decode in validateJSONObject, so a value
// reaching here matches its field kind.
func enumAllows(f BodyField, v any) bool {
	switch f.Type {
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		iv, err := n.Int64()
		if err != nil {
			return false
		}
		for _, a := range f.Enum {
			if e, ok := a.(int64); ok && e == iv {
				return true
			}
		}
		return false
	case "number":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		fv, err := n.Float64()
		if err != nil {
			return false
		}
		for _, a := range f.Enum {
			if e, ok := a.(float64); ok && e == fv {
				return true
			}
		}
		return false
	case "boolean":
		bv, ok := v.(bool)
		if !ok {
			return false
		}
		for _, a := range f.Enum {
			if e, ok := a.(bool); ok && e == bv {
				return true
			}
		}
		return false
	default: // string (and any non-scalar, which ValidateDecl already forbids)
		s, ok := v.(string)
		if !ok {
			return false
		}
		for _, a := range f.Enum {
			if e, ok := a.(string); ok && e == s {
				return true
			}
		}
		return false
	}
}

// joinEnum renders typed enum values as a space-separated hint string.
func joinEnum(xs []any) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprint(x)
	}
	return strings.Join(parts, " ")
}

// lenViolation reports how s violates the [minLen, maxLen] character-length
// bounds (either 0 = unbounded), or "" when it satisfies them. Length is counted
// in characters (runes), not bytes, matching the character length a declarer means.
func lenViolation(s string, minLen, maxLen int) string {
	n := utf8.RuneCountInString(s)
	if minLen > 0 && n < minLen {
		return fmt.Sprintf("must be at least %d characters (got %d)", minLen, n)
	}
	if maxLen > 0 && n > maxLen {
		return fmt.Sprintf("must be at most %d characters (got %d)", maxLen, n)
	}
	return ""
}

// patternViolation reports whether s fails to match pattern (empty = no
// constraint). The pattern must match the WHOLE value: it is wrapped in \A(?:…)\z
// so a declarer need not anchor it (and ^/$ they add still work). The pattern is
// verified to compile by ValidateDecl; a compile error here rejects defensively.
func patternViolation(s, pattern string) bool {
	if pattern == "" {
		return false
	}
	re, err := regexp.Compile(`\A(?:` + pattern + `)\z`)
	if err != nil {
		return true
	}
	return !re.MatchString(s)
}

// boundViolation reports how x violates the inclusive [minVal, maxVal] bounds
// (nil = unbounded), or "" when it satisfies them. Values are rendered as plain
// decimals (not %v's scientific form), so a round bound like 1000000 reads as
// "1000000", not "1e+06".
func boundViolation(x float64, minVal, maxVal *float64) string {
	if minVal != nil && x < *minVal {
		return fmt.Sprintf("must be >= %s (got %s)", formatFloat(*minVal), formatFloat(x))
	}
	if maxVal != nil && x > *maxVal {
		return fmt.Sprintf("must be <= %s (got %s)", formatFloat(*maxVal), formatFloat(x))
	}
	return ""
}

// numBoundViolation reports how the numeric body value n violates the inclusive
// [minVal, maxVal] bounds, or "" when within them. An integer field is compared
// in int64 space so a value beyond float64's exact integer range (2^53) is not
// rounded before the check; a non-integral bound or an out-of-int64 value falls
// back to float64. A number (non-integer) field always compares as float64.
func numBoundViolation(n json.Number, isInt bool, minVal, maxVal *float64) string {
	if isInt {
		if iv, err := n.Int64(); err == nil {
			if minVal != nil {
				if m, ok := wholeInt64(*minVal); ok {
					if iv < m {
						return fmt.Sprintf("must be >= %d (got %d)", m, iv)
					}
				} else if float64(iv) < *minVal {
					return fmt.Sprintf("must be >= %s (got %d)", formatFloat(*minVal), iv)
				}
			}
			if maxVal != nil {
				if m, ok := wholeInt64(*maxVal); ok {
					if iv > m {
						return fmt.Sprintf("must be <= %d (got %d)", m, iv)
					}
				} else if float64(iv) > *maxVal {
					return fmt.Sprintf("must be <= %s (got %d)", formatFloat(*maxVal), iv)
				}
			}
			return ""
		}
	}
	x, err := n.Float64()
	if err != nil {
		return ""
	}
	return boundViolation(x, minVal, maxVal)
}

// wholeInt64 reports whether f is an integer exactly representable as an int64.
// math.MaxInt64 rounds up to 2^63 as a float64, so the `>= math.MaxInt64` guard
// (i.e. >= 2^63) is what keeps the int64(f) conversion below from overflowing.
func wholeInt64(f float64) (int64, bool) {
	if f != math.Trunc(f) || f < math.MinInt64 || f >= math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}

// formatFloat renders a numeric bound/value as a plain decimal with no trailing
// zeros and no scientific notation (unlike %v), so bounds read naturally in an
// error message.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func join(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}

// jsonNameForGoField finds the JSON wire name for a Go struct field name within
// a list of BodyFields. The required_if tag uses the Go field name (e.g.
// "ValueType") but the decoded JSON map uses wire names (e.g. "valueType"). This
// helper maps between them by checking if any field's Name matches the
// lower-camel form. As a fallback it tries a case-insensitive prefix match.
// If no match is found it returns the input lowered (best-effort).
func jsonNameForGoField(fields []BodyField, goName string) string {
	// Direct match: the Go field name lowered with first char.
	lower := strings.ToLower(goName[:1]) + goName[1:]
	for _, f := range fields {
		if f.Name == lower {
			return f.Name
		}
	}
	// Fallback: case-insensitive.
	for _, f := range fields {
		if strings.EqualFold(f.Name, goName) {
			return f.Name
		}
	}
	return lower
}
