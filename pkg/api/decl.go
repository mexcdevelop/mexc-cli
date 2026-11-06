// Declaration-time validation of an Operation. Validate (validate.go) checks
// the runtime VALUES a caller passed; ValidateDecl checks the DECLARATION
// itself — the shape a business team wrote. One implementation, three call
// sites: the assembly layer (cmd/mount, so a bad declaration fails the binary
// loudly on first run — the safety net for the CI runner that cannot run Go),
// the per-subdomain L1 contract tests, and the cross-domain L2 contract test.
package api

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// reservedFlagNames are flag names owned by the framework: the global persistent
// flags (inherited by every command), cobra's persistent --help, and the per-op
// --data / --file carriers. A Param.Name colliding with one would shadow or be
// shadowed by the framework flag, so it is rejected at declaration time.
//
// `version` is deliberately NOT reserved: cobra's --version lives only on the
// root command (it is not persistent), so a leaf operation may declare a wire
// param named "version" with no collision — a backend key the CLI must not block.
// See TestValidateDecl_VersionParamAllowed and cmd's runtime cross-face test.
var reservedFlagNames = map[string]bool{
	"base-url":  true,
	"read-only": true,
	"dry-run":   true,
	"no-retry":  true,
	"profile":   true,
	"env":       true,
	"help":      true,
	"yes":       true, // the write-confirmation flag (--yes asserts the user confirmed)
	"data":      true, // the request-body carrier for Body operations
	"file":      true, // the file carrier for Multipart operations
}

// pathPlaceholderRe matches a {name} placeholder in an Operation.Path. The
// name must start with a letter; this is the only placeholder syntax allowed
// (no wildcards, no multi-segment captures).
var pathPlaceholderRe = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9]*)\}`)

// bodyMethods are the HTTP methods on which a request body is meaningful. DELETE
// is included: some endpoints take a batch/selector body on DELETE (e.g. delete
// N records in one call), which the HTTP client sends unchanged.
var bodyMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// ValidateDecl checks that the Operation declaration itself is well-formed:
// enum fields hold defined values, path placeholders and In: path params match
// one-to-one, a request body is only declared on a body-bearing method and
// through at most one of Body / In:form params / Multipart, the Permission
// shape is complete, and param names are unique and unreserved.
//
// Cross-field checks that need the subdomain defaults (e.g. Permission
// requires a login-token effective identity) live in the assembly layer, which
// is the only place that sees both the op and its subdomain.
func (o Operation) ValidateDecl() error {
	if o.Name == "" || o.Summary == "" || (o.Kind != KindWSCollect && (o.Method == "" || o.Path == "")) {
		return declErr(o, "Name and Summary are always required; Method and Path are required "+
			"unless Kind is KindWSCollect (a WS-backed operation builds no RequestSpec and has neither)")
	}
	if !o.Kind.Valid() {
		return declErr(o, "kind %q is not a defined value — use api.KindREST (omit) / api.KindWSCollect", o.Kind)
	}
	if err := o.checkKindWSCollectConsistency(); err != nil {
		return err
	}
	if !o.Access.Valid() {
		return declErr(o, "access %q is not a defined value", o.Access)
	}
	// No signing-scheme check here, for the same reason as the identity above:
	// the set is not known to the shared core. A scheme is real iff a signer is
	// registered for it, asserted at assembly (validateRegistry).
	// No identity-value check here: the set is not known to the shared core.
	// An identity is real iff a provider is registered for it, and that is
	// asserted at assembly (validateRegistry), which is where the registry is
	// populated and where a missing provider can be reported per sub-domain.
	if !o.ResponseKind.Valid() {
		return declErr(o, "response kind %q is not a defined value — use api.ResponseJSON / api.ResponseBinary (omit for JSON)", o.ResponseKind)
	}
	if o.ResponseKind.IsBinary() {
		// A binary download does not mutate server state; requiring read is also
		// what grants it --read-only / no-write-confirmation treatment downstream.
		if o.Access != Read {
			return declErr(o, "a binary-response operation must be read")
		}
		// A file upload (multipart) and a streamed binary response are not modeled
		// on one operation.
		if o.Multipart != nil {
			return declErr(o, "a binary-response operation cannot be multipart")
		}
	}

	if err := o.validateParams(); err != nil {
		return err
	}
	if err := o.validatePathPlaceholders(); err != nil {
		return err
	}
	if err := o.validateBodyPrototype(); err != nil {
		return err
	}
	if err := o.validateBodyEnums(); err != nil {
		return err
	}
	if err := o.validateBodyConstraints(); err != nil {
		return err
	}

	if o.Permission != nil {
		if o.Permission.System == "" {
			return declErr(o, "Permission.System must name the grant bucket to check")
		}
		if len(o.Permission.Codes) == 0 {
			return declErr(o, "Permission.Codes must list at least one code (OR semantics)")
		}
		for _, c := range o.Permission.Codes {
			if c == "" {
				return declErr(o, "Permission.Codes must not contain an empty code")
			}
		}
	}

	// Body kinds are mutually exclusive: JSON Body, In:form params, or Multipart.
	formParams := 0
	for _, p := range o.Params {
		if p.In == InForm {
			formParams++
		}
	}
	kinds := 0
	if o.Body != nil {
		kinds++
	}
	if formParams > 0 {
		kinds++
	}
	if o.Multipart != nil {
		kinds++
	}
	if kinds > 1 {
		return declErr(o, "at most one body kind is allowed: a JSON Body, In:form params, or Multipart")
	}
	if (o.Body != nil || formParams > 0 || o.Multipart != nil) && !bodyMethods[o.Method] {
		return declErr(o, "a request body is only meaningful on POST/PUT/PATCH/DELETE, not %s", o.Method)
	}
	if o.Multipart != nil {
		if len(o.Multipart.Files) == 0 {
			return declErr(o, "Multipart must declare at least one file field")
		}
		seen := map[string]bool{}
		for _, f := range o.Multipart.Files {
			if f.Field == "" {
				return declErr(o, "Multipart declares a file field with an empty name")
			}
			if seen[f.Field] {
				return declErr(o, "Multipart file field %q is declared twice", f.Field)
			}
			seen[f.Field] = true
		}
	}
	return nil
}

// validateBodyPrototype checks that the Body prototype is a shape the framework
// can project: a struct (the body is one JSON object), a slice/array of structs
// (the body is a JSON array of objects), or a slice/array of scalars (the body is
// a JSON array of primitive values). Pointers at either level are fine.
//
// Anything else reflects into NO fields, and every consequence of that is silent.
// Nothing checks required / enum / pattern, because there is no field tree to
// check against; validateBodyEnums and validateBodyConstraints walk an empty tree
// and pass; `schema` reports a body with no fields; the MCP tool
// advertises an empty object. The declaration looks accepted and the operation is
// unusable. It is rejected here because this is the only layer that can see it.
func (o Operation) validateBodyPrototype() error {
	if o.Body == nil {
		return nil
	}
	t := reflect.TypeOf(o.Body)
	el, shape := bodyElemType(t)
	if el != nil && el.Kind() == reflect.Struct {
		return nil
	}
	if shape == BodyShapeArray {
		if isScalarBodyType(el) {
			return nil
		}
		return declErr(o, "Body is a slice of %s — an array body must be a slice of structs or scalars (e.g. []OrderItem{} or []int64{})", kindOf(el))
	}
	return declErr(o, "Body is %s — it must be a struct (an object body) or a slice of structs (an array body); "+
		"the struct tags are what describe and validate the payload", kindOf(t))
}

func isScalarBodyType(t reflect.Type) bool {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// kindOf names a reflect.Type for a declaration error, tolerating nil.
func kindOf(t reflect.Type) string {
	if t == nil {
		return "nil"
	}
	return t.Kind().String()
}

// validateBodyEnums enforces that an enum tag is only declared on a scalar body
// field (string / integer / number / boolean). The enum check compares a single
// scalar value, so enum on an object or array field is meaningless — a
// declaration mistake caught here at load time (ValidateDecl runs in the
// assembly layer on first boot) rather than surfacing as a confusing runtime
// rejection. It walks the Body and Multipart text-field prototypes recursively.
func (o Operation) validateBodyEnums() error {
	var walk func(fields []BodyField) error
	walk = func(fields []BodyField) error {
		for _, f := range fields {
			if len(f.Enum) > 0 {
				if f.Type == "object" || f.Type == "array" {
					return declErr(o, "body field %q is %s but carries an enum tag — enum is only valid on a scalar field (string/integer/number/boolean)", f.Name, f.Type)
				}
				// Every token must have typed cleanly for the field's kind. enumValues
				// stores a numeric/boolean token as its typed value on success and falls
				// back to a raw string on failure, so a string entry under a numeric or
				// boolean field means an unparseable token — a declaration error, not a
				// field that silently rejects every value at runtime.
				for _, v := range f.Enum {
					if !enumValueMatchesKind(f.Type, v) {
						return declErr(o, "body field %q declares enum value %v, which is not a valid %s", f.Name, v, f.Type)
					}
				}
			}
			if err := walk(f.Fields); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(o.BodyFields()); err != nil {
		return err
	}
	if o.Multipart != nil && o.Multipart.Fields != nil {
		if err := walk(reflectBodyFields(reflect.TypeOf(o.Multipart.Fields))); err != nil {
			return err
		}
	}
	return nil
}

// wsCollectReservedFlagNames are flag names the WS execution path injects for
// every KindWSCollect operation only (see internal/cli's newOperationCmd). They
// are not in reservedFlagNames because they do not apply to an ordinary
// request-response operation — a REST op's own --limit (e.g. spot.market's
// depth) is legitimate and must not be blocked globally.
var wsCollectReservedFlagNames = map[string]bool{
	"listen-duration": true,
	"limit":           true,
}

// checkKindWSCollectConsistency enforces that Kind and WSCollect agree: a
// KindWSCollect operation must set WSCollect (the only way internal/cli's
// runWSOperation knows how to construct the WS connection), and every other
// Kind must leave it nil (a REST operation's WSCollect would be silently
// ignored by every dispatch site, which all test Kind — a declaration
// mistake worth catching here rather than leaving dead data on the
// Operation).
func (o Operation) checkKindWSCollectConsistency() error {
	if o.Kind == KindWSCollect && o.WSCollect == nil {
		return declErr(o, "Kind is KindWSCollect but WSCollect is nil — a WS-backed operation must declare WSCollect")
	}
	if o.Kind != KindWSCollect && o.WSCollect != nil {
		return declErr(o, "WSCollect is set but Kind is %q, not KindWSCollect — set Kind: api.KindWSCollect, or drop WSCollect", o.Kind)
	}
	return nil
}

// validateParams checks each Param and their collective uniqueness.
func (o Operation) validateParams() error {
	seen := map[string]bool{}
	for _, p := range o.Params {
		if p.Name == "" {
			return declErr(o, "a Param has an empty Name")
		}
		if seen[p.Name] {
			return declErr(o, "param %q is declared twice", p.Name)
		}
		seen[p.Name] = true
		if reservedFlagNames[p.Name] {
			return declErr(o, "param %q collides with a reserved framework flag — rename the wire parameter or ask the CLI team", p.Name)
		}
		if o.Kind == KindWSCollect && wsCollectReservedFlagNames[p.Name] {
			return declErr(o, "param %q collides with the flag the streaming execution path injects on every KindWSCollect operation — rename the wire parameter", p.Name)
		}
		if !p.In.Valid() {
			return declErr(o, "param %q has undefined In %q — use the api.In* constants (omit for query)", p.Name, p.In)
		}
		if err := o.checkParamConstraints(p); err != nil {
			return err
		}
	}
	return nil
}

// checkParamConstraints checks a Param's pre-validation bounds are well-formed:
// a non-negative length range with MinLen <= MaxLen, MinVal <= MaxVal, and a
// Pattern that compiles. A param has no declared value type (it is a string on
// the wire), so — unlike a body field — numeric vs string appropriateness is not
// checked here; MinVal/MaxVal simply mean "parse the value as a number".
func (o Operation) checkParamConstraints(p Param) error {
	if p.MinLen < 0 || p.MaxLen < 0 {
		return declErr(o, "param %q has a negative length bound", p.Name)
	}
	if p.MinLen > 0 && p.MaxLen > 0 && p.MinLen > p.MaxLen {
		return declErr(o, "param %q has MinLen %d > MaxLen %d", p.Name, p.MinLen, p.MaxLen)
	}
	if p.MinVal != nil && p.MaxVal != nil && *p.MinVal > *p.MaxVal {
		return declErr(o, "param %q has MinVal %v > MaxVal %v", p.Name, *p.MinVal, *p.MaxVal)
	}
	if p.Pattern != "" {
		if _, err := regexp.Compile(`\A(?:` + p.Pattern + `)\z`); err != nil {
			return declErr(o, "param %q has an invalid Pattern %q: %v", p.Name, p.Pattern, err)
		}
	}
	return nil
}

// validateBodyConstraints checks the pre-validation struct tags (minval/maxval/
// minlen/maxlen/pattern) on the Body (and Multipart text-field) prototype, at
// load time: each tag must parse, sit on a field of the right JSON kind (numeric
// bounds on integer/number; length/pattern on string), form a valid range
// (min <= max), and a pattern must compile. It walks the struct type directly so
// a malformed tag string is caught here rather than silently ignored at runtime.
func (o Operation) validateBodyConstraints() error {
	var walk func(t reflect.Type) error
	walk = func(t reflect.Type) error {
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == nil || t.Kind() != reflect.Struct {
			return nil
		}
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" { // unexported
				continue
			}
			name := jsonFieldName(sf)
			if name == "-" {
				continue
			}
			ft := sf.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if err := o.checkConstraintTags(name, jsonKind(ft), sf.Tag); err != nil {
				return err
			}
			switch ft.Kind() {
			case reflect.Struct:
				if err := walk(ft); err != nil {
					return err
				}
			case reflect.Slice, reflect.Array:
				if err := walk(ft.Elem()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if o.Body != nil {
		// An array body's tags live on the ITEM struct, so unwrap before walking —
		// otherwise the walk stops at the slice and every constraint tag inside goes
		// unchecked.
		el, _ := bodyElemType(reflect.TypeOf(o.Body))
		if err := walk(el); err != nil {
			return err
		}
	}
	if o.Multipart != nil && o.Multipart.Fields != nil {
		if err := walk(reflect.TypeOf(o.Multipart.Fields)); err != nil {
			return err
		}
	}
	return nil
}

// checkConstraintTags validates one body field's constraint tags against its JSON
// kind. Numeric bounds (minval/maxval) require an integer/number field; length
// and pattern (minlen/maxlen/pattern) require a string field.
func (o Operation) checkConstraintTags(name, kind string, tag reflect.StructTag) error {
	minv, maxv := tag.Get("minval"), tag.Get("maxval")
	minl, maxl, pat := tag.Get("minlen"), tag.Get("maxlen"), tag.Get("pattern")

	if (minv != "" || maxv != "") && kind != "integer" && kind != "number" {
		return declErr(o, "body field %q is %s but carries minval/maxval — numeric bounds are only valid on an integer/number field", name, kind)
	}
	if (minl != "" || maxl != "" || pat != "") && kind != "string" {
		return declErr(o, "body field %q is %s but carries minlen/maxlen/pattern — length/pattern are only valid on a string field", name, kind)
	}

	minF, err := parseDeclFloat(o, name, "minval", minv)
	if err != nil {
		return err
	}
	maxF, err := parseDeclFloat(o, name, "maxval", maxv)
	if err != nil {
		return err
	}
	if minF != nil && maxF != nil && *minF > *maxF {
		return declErr(o, "body field %q has minval %v > maxval %v", name, *minF, *maxF)
	}

	minLen, err := parseDeclLen(o, name, "minlen", minl)
	if err != nil {
		return err
	}
	maxLen, err := parseDeclLen(o, name, "maxlen", maxl)
	if err != nil {
		return err
	}
	if minLen != nil && maxLen != nil && *minLen > *maxLen {
		return declErr(o, "body field %q has minlen %d > maxlen %d", name, *minLen, *maxLen)
	}

	if pat != "" {
		if _, err := regexp.Compile(`\A(?:` + pat + `)\z`); err != nil {
			return declErr(o, "body field %q has an invalid pattern %q: %v", name, pat, err)
		}
	}
	return nil
}

func parseDeclFloat(o Operation, field, tag, s string) (*float64, error) {
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, declErr(o, "body field %q has a malformed %s %q (want a number)", field, tag, s)
	}
	return &f, nil
}

func parseDeclLen(o Operation, field, tag, s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return nil, declErr(o, "body field %q has a malformed %s %q (want a non-negative integer)", field, tag, s)
	}
	return &n, nil
}

// validatePathPlaceholders enforces the {name} <-> In: path one-to-one match:
// every placeholder has exactly one matching path param, every path param has
// exactly one placeholder, path params are Required, and no stray brace
// survives outside the placeholder syntax.
func (o Operation) validatePathPlaceholders() error {
	placeholders := map[string]int{}
	for _, m := range pathPlaceholderRe.FindAllStringSubmatch(o.Path, -1) {
		placeholders[m[1]]++
	}
	// Any brace not consumed by a well-formed placeholder is a syntax error.
	if leftover := pathPlaceholderRe.ReplaceAllString(o.Path, ""); strings.ContainsAny(leftover, "{}") {
		return declErr(o, "Path %q carries a malformed placeholder — the only allowed form is {name}", o.Path)
	}
	for name, n := range placeholders {
		if n > 1 {
			return declErr(o, "Path placeholder {%s} appears %d times; it must appear exactly once", name, n)
		}
	}

	pathParams := map[string]Param{}
	for _, p := range o.Params {
		if p.In == InPath {
			pathParams[p.Name] = p
		}
	}
	for name := range placeholders {
		p, ok := pathParams[name]
		if !ok {
			return declErr(o, "Path placeholder {%s} has no matching Param{Name: %q, In: api.InPath}", name, name)
		}
		if !p.Required {
			return declErr(o, "path param %q must be Required: true (a URL segment cannot be empty)", name)
		}
	}
	for name := range pathParams {
		if _, ok := placeholders[name]; !ok {
			return declErr(o, "param %q is In: path but Path %q carries no {%s} placeholder", name, o.Path, name)
		}
	}
	return nil
}

// declErr wraps a declaration problem as a structured internal error: a bad
// declaration is a programming error in the skill package, not a user mistake.
func declErr(o Operation, format string, args ...any) error {
	return errs.Internal("operation %q: invalid declaration: %s", o.Name, fmt.Sprintf(format, args...)).
		WithHint("fix the api.Operation declaration; the Operation type in pkg/api documents each field's contract")
}
