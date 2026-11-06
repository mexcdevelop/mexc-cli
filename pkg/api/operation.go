// Package api defines the descriptor a business owner uses to declare an operation.
//
// This is the core type a business team faces when writing a subdomain. It is
// deliberately restrained: it carries only the fields the business owner must
// reason about and that only the business owner can know (operation name, path,
// read/write, signing scheme, login identity, and the parameters). Execution,
// signing, rate limiting, and command assembly all sink down into the framework
// (internal/cli + internal/transport) and the business owner never touches them.
//
// This package does not
// depend on cobra, which keeps the api/ layer pure data plus pure functions:
// unit-testable and free of framework noise.
package api

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
)

// SensitiveMask replaces the value of a Sensitive param or body field in the
// write-confirmation display, so a token or secret is never shown in the prompt.
const SensitiveMask = "***"

// Access is the action dimension of an operation (the access column of its
// SKILL.md row). Whether something is a write is derived from it; there is no
// separate write field, which avoids a redundant state. It is an open
// enumeration: future values such as admin or cancel-only can be added.
type Access string

const (
	Read  Access = "read"
	Write Access = "write"
)

// Valid reports whether the access value is one of the allowed values.
func (a Access) Valid() bool {
	switch a {
	case Read, Write:
		return true
	default:
		return false
	}
}

// ResponseKind is the shape of an operation's response body. The default (zero
// value) is JSON: the body is buffered and decoded. ResponseBinary marks an
// operation whose body is an opaque byte stream (a file download / export); the
// transport streams it to disk instead of buffering and decoding it. Modeling
// this on the operation keeps the transport declarative — it never sniffs the
// body to decide how to consume it.
type ResponseKind string

const (
	ResponseJSON   ResponseKind = "json"
	ResponseBinary ResponseKind = "binary"
)

// Valid reports whether the response kind is one of the allowed values. The zero
// value is valid and means JSON, so existing operations declare nothing.
func (r ResponseKind) Valid() bool {
	switch r {
	case "", ResponseJSON, ResponseBinary:
		return true
	default:
		return false
	}
}

// IsBinary reports whether the response is an opaque byte stream to be streamed
// to disk rather than buffered and decoded as JSON.
func (r ResponseKind) IsBinary() bool { return r == ResponseBinary }

// OperationKind discriminates an Operation's execution model — the single
// source of truth for "what kind of operation is this", so a dispatch site
// (ValidateDecl, command assembly, the MCP face) reads one field instead of
// checking whichever data-carrying field (WSCollect, and any future sibling)
// happens to be non-nil. The zero value is KindREST, the overwhelmingly
// common case, so every existing Operation literal needs no change: it
// never sets Kind and is a KindREST by default, exactly as an unset
// ResponseKind defaults to JSON.
//
// A Kind value that carries its own data (KindWSCollect) still needs a
// separate field to hold that data (WSCollect) — Kind alone cannot also be
// the closure — but Kind, not the data field's nil-ness, is what every
// dispatch site tests, and ValidateDecl enforces that the two agree (see
// checkKindWSCollectConsistency).
type OperationKind string

const (
	// KindREST is an ordinary request-response operation: it builds a
	// RequestSpec (Method/Path/Params/Body) and runs through runOperation.
	// It is the zero value, so it needs no explicit declaration.
	KindREST OperationKind = ""
	// KindWSCollect is a WS-backed operation: a long-lived subscription
	// receiving pushed messages over a persistent connection, instead of a
	// one-shot request-response. It runs through internal/cli's
	// runWSOperation instead of runOperation/BuildSpec, and requires
	// WSCollect to be set.
	KindWSCollect OperationKind = "ws-collect"
)

// Valid reports whether the kind is one of the declared values.
func (k OperationKind) Valid() bool {
	switch k {
	case KindREST, KindWSCollect:
		return true
	default:
		return false
	}
}

// Signing identifies a complete request-signing scheme: what is signed (the
// canonical form), the algorithm, the encoding, where the signature is
// injected, and which credentials it needs. A business team references a
// declared constant and never writes its own string: a string literal in an
// Operation turns CI red (check-boundary Invariant 6).
//
// The concrete schemes are deliberately NOT catalogued here, for the same
// reason Identity's are not. SignNone is universal — the absence of signing —
// but a real scheme belongs to whoever supplies its signer, and is declared in
// that composition root's own package. A catalogue here would pull one root's
// vocabulary down into the foundation that every layer above it imports.
//
// A scheme is REAL iff a signer is registered for it, which assembly checks
// (validateRegistry). That is stricter than the compile-time list it replaces:
// a value in a list can still have no signer behind it, and used to pass
// assembly only to fail at send time.
//
// The zero value ("") on an Operation means "inherit the subdomain default";
// it is expressed by omitting the field, so there is deliberately no constant
// for it (see Concrete).
type Signing string

// SignNone declares an explicitly unsigned operation. On an op it can override a
// signing subdomain default — legal, but usually a smell that the operation
// belongs in its own subdomain. It is the one scheme the shared core names,
// because "unsigned" is not a product's choice of algorithm.
const SignNone Signing = "none"

// Concrete reports whether s names an actual scheme, as opposed to the zero
// value, which means "inherit the subdomain default".
func (s Signing) Concrete() bool { return s != "" }

// Signs reports whether this scheme actually signs the request (a concrete
// scheme other than none). Used to project the signing axis into the boolean
// the schema/dry-run faces expose — the scheme NAME stays internal to the CLI.
func (s Signing) Signs() bool { return s != "" && s != SignNone }

// Identity is the credential/identity an operation authenticates with — the
// second auth axis, orthogonal to Signing (Signing = HOW to sign a request;
// Identity = WHICH credential identifies it and is verified). The zero value on
// an op means "inherit the subdomain default", and business code only ever
// references declared constants, never a string literal.
//
// The concrete values are deliberately NOT catalogued here. IdentityNone is
// universal — the absence of an identity — but a concrete identity belongs to
// whoever supplies its provider and is declared in that composition root's own
// package. Two reasons:
//
//   - A catalogue in the shared core puts one root's vocabulary into the
//     foundation every layer imports, including layers that will never have
//     that identity.
//   - Having the name in reach invites logic that branches on a specific value
//     instead of on a capability. That happened: assembly once refused an
//     operation's Permission unless its identity equalled one particular
//     constant, which was both a coupling and the wrong test — see
//     credential.GrantCarrier for the shape that replaced it.
//
// An identity is REAL iff a provider is registered for it, which assembly
// checks (validateRegistry). That is stricter than the compile-time list this
// replaced: a value in a list can still have no provider behind it, and used to
// pass.
type Identity string

// IdentityNone declares that no identity is attached — public data: no API key,
// no login. It is the one identity the shared core names, because "no identity"
// is not a product's choice of credential; the transport branches on it to skip
// credential resolution entirely.
const IdentityNone Identity = "none"

// Concrete reports whether i names an actual identity, as opposed to the zero
// value, which means "inherit the subdomain default".
func (i Identity) Concrete() bool { return i != "" }

// ParamIn declares where a parameter travels in the HTTP request. The zero
// value ("") means query — the overwhelmingly common case — so query params
// simply omit the field. It is a CLOSED enum with the same rules as Signing.
//
// Auth and environment headers are the framework's job (Identity / Signing /
// endpoint config inject them); they are never declared as per-op params. A
// header param is only for a genuine per-call header such as an idempotency
// key, and is expected to be rare.
type ParamIn string

const (
	// InQuery is the default location. It is also the zero value: omitting In
	// on a Param means query. The named constant exists for authors who prefer
	// to be explicit; "" and InQuery are equivalent everywhere.
	InQuery  ParamIn = "query"
	InPath   ParamIn = "path"   // replaces the {name} placeholder in Path
	InHeader ParamIn = "header" // sent as a request header (name used verbatim)
	InCookie ParamIn = "cookie" // sent in the Cookie header
	InForm   ParamIn = "form"   // x-www-form-urlencoded body field
)

// Valid reports whether p is the zero value (query) or a defined location.
func (p ParamIn) Valid() bool {
	switch p {
	case "", InQuery, InPath, InHeader, InCookie, InForm:
		return true
	default:
		return false
	}
}

// IsQuery reports whether the param travels in the query string — either
// explicitly (InQuery) or by the zero-value default (omitted).
func (p ParamIn) IsQuery() bool { return p == "" || p == InQuery }

// Canonical normalizes the zero value to its explicit spelling (InQuery) so a
// serialized location is always a concrete value, never the empty string. Used
// when projecting into the machine-readable Schema, so an agent never has to
// know the omit-means-query convention.
func (p ParamIn) Canonical() ParamIn {
	if p == "" {
		return InQuery
	}
	return p
}

// PermReq declares an operation's authorization requirement: which system
// bucket of the login grant to check, and which permission codes satisfy it
// (OR semantics — holding any one grants access). System and Codes are backend
// vocabulary, an OPEN set — unlike the closed Signing/Identity enums they are
// not defined by the CLI. Only meaningful for a login-token identity.
type PermReq struct {
	System string   // which system bucket of the auth grant to check against
	Codes  []string // OR: any one of these codes grants access
}

// Param describes a single operation parameter. The framework uses it to bind a
// flag automatically and to enforce required/enum validation. Name is the wire
// name — the exact query key / header name / form field the backend expects —
// and is used verbatim as the CLI flag --<name> (one declared name, two uses).
type Param struct {
	Name      string   // wire name; also the CLI flag --<name>
	Desc      string   // help text
	Required  bool     // whether the parameter is required
	Enum      []string // allowed values; empty means unconstrained
	In        ParamIn  // where it travels; zero value = query
	Sensitive bool     // value is masked in the write-confirmation display (e.g. a token). Rare; the default (false) shows the value so the user can verify the request.

	// Pre-validation constraints, checked uniformly by Validate before the request
	// is sent (so a bad value is refused with a structured error on both the CLI
	// and MCP faces). Numeric bounds act on the value parsed as a number; the
	// length/pattern constraints act on the string. Their zero value means "no
	// limit", so the common case declares none.
	MinVal  *float64 // numeric lower bound, inclusive (value >= MinVal); nil = no limit
	MaxVal  *float64 // numeric upper bound, inclusive (value <= MaxVal); nil = no limit
	MinLen  int      // minimum string length; 0 = no limit
	MaxLen  int      // maximum string length; 0 = no limit
	Pattern string   // regular expression the value must fully match; "" = no limit
}

// MultipartDecl declares (on an Operation) a multipart/form-data upload. Files
// lists the declared file fields (>=1); Fields is an optional struct prototype
// for the text fields (reflected into the schema, like Body). This is a
// DECLARATION type — the business author faces it. Its runtime transport
// counterparts (MultipartBody / FilePart) and the assembly that reads files
// (BuildSpec) live on the transport side, in requestspec.go.
type MultipartDecl struct {
	Files  []MultipartFile
	Fields any
}

// MultipartFile declares one file field of a multipart upload: its wire field
// name, help text, and whether it is required. Supplied on the CLI via --file.
type MultipartFile struct {
	Field    string
	Desc     string
	Required bool
}

// Operation is the complete declaration of an operation. It serves as the
// single source of truth for its faces: the SKILL.md table, the CLI flags, the
// request construction, the schema/MCP output, and the contract tests
// An Operation carries no domain/sub context: the
// subdomain defaults (signing scheme, identity) are resolved by the assembly
// layer, which is the only place that sees both the op and its subdomain.
type Operation struct {
	Name    string // operation name; identical across command name, file identifier, and SKILL.md table row
	Method  string // GET / POST / PUT / PATCH / DELETE
	Path    string // upstream API path, e.g. /api/v3/ticker/price; may carry {name} placeholders bound to In: path params
	Access  Access // read / write
	Summary string // one-line description, used as the Short of the cobra command
	Params  []Param

	// ResponseKind is the shape of the response body. Zero (JSON) is the common
	// case: the response is buffered and decoded. Set it to ResponseBinary for a
	// file download / export, whose body is streamed to disk rather than decoded.
	// A binary operation must be read and must not be multipart (see ValidateDecl).
	ResponseKind ResponseKind

	// Signed is the signing scheme — auth axis one (a Signing value, not a
	// bool). The zero value inherits the subdomain default (declared as
	// `const Signed` in the subdomain's api package and wired through the
	// registry); a concrete value overrides it, including SignNone to opt out
	// explicitly (a smell worth a review).
	Signed Signing

	// Identity is the login identity the call must carry — auth axis two.
	// Zero value inherits the subdomain default (`const Identity`); a
	// concrete value overrides it.
	Identity Identity

	// Body declares the JSON request body as a prototype. It is the single way to
	// declare a body: the framework reflects it into the schema (so an agent can
	// inspect the body fields with the `schema` command before calling), and on the
	// CLI the whole body is supplied through one --data flag (inline JSON, or
	// @<path> / - to keep it out of a shell's hands — see
	// internal/cli.resolveDataInput) which is validated against this type before
	// sending. Only meaningful on POST/PUT/PATCH/DELETE.
	//
	// Two shapes are supported, and the prototype IS the declaration of which:
	//   - a struct, e.g. PlaceOrder{}   — the body is one JSON object
	//   - a slice of structs, e.g. []PlaceOrder{} — the body is a JSON array of
	//     them, and --data takes `[{...},{...}]`
	//   - a slice of scalars, e.g. []int64{} — the body is a raw JSON array of
	//     primitive values, and --data takes `[123,456]`
	// For struct bodies, tags describe each field; for a slice of structs they are
	// enforced per item, with the failing item's index named in the error. Scalar
	// array bodies have no field tags but still enforce JSON shape, element type,
	// and non-empty arrays. Nothing else changes — --data is the wire body
	// verbatim in all shapes. Anything that is neither shape (a map, a bare
	// string, a slice of maps) is rejected by ValidateDecl.
	//
	// Author it with struct tags: `json:"<wire-name>"` is the field name,
	// `validate:"required"` marks a required field (its absence means optional),
	// `enum:"A,B"` constrains the allowed values, `desc:"..."` is the help text,
	// and `sensitive:"true"` masks the field's value in the write-confirmation
	// prompt (for a token/secret; omit it so the user can verify the value).
	// Pre-validation constraints (checked before the request is sent, on both
	// faces): `minval`/`maxval` bound a numeric field inclusively; `minlen`/`maxlen`
	// bound a string's character length; `pattern` is a regex the whole string must
	// match. Numeric tags belong on an integer/number field, length/pattern on a
	// string field (mismatches fail ValidateDecl at load time).
	// Nested structs and slices of structs describe nested JSON. A signing
	// algorithm name must never appear in a tag (it stays internal to the CLI).
	//
	// The JSON body transport has landed (validated end to end over the network):
	// --data is deep-validated against this prototype before the request is sent,
	// so a malformed body is refused with a structured error, never silently dropped.
	Body any

	// Multipart declares a multipart/form-data upload; mutually exclusive with
	// Body and with In:form params. Files are supplied on the CLI via --file.
	Multipart *MultipartDecl

	// Permission declares the operation's authorization requirement (see PermReq).
	// It is surfaced in the schema output; a login-token identity's provider runs a
	// preflight against the login grant — a hard block only when the op's site is
	// absent from the session, a code miss being a soft-pass warning (the backend
	// stays authoritative).
	Permission *PermReq

	// HostKey optionally overrides the subdomain's host key — the logical name
	// resolved to a base URL by the central host table.
	HostKey string

	// Destructive and Idempotent are optional overrides for the write semantics
	// that, like Access, only the business owner can know: a write is not all the
	// same. Placing an order or transferring funds is a write that *adds* state
	// without unmaking anything (non-destructive); cancelling or amending an order,
	// or setting a preference, converges to a fixed state and is safe to repeat
	// (idempotent). They are nil by default and derived from Access via
	// DestructiveHint / IdempotentHint, so the common case needs no extra fields;
	// set one only when the operation departs from the default (e.g. a place-order
	// write that should advertise Destructive=false). They feed the MCP tool
	// annotations and the write-warning grading, mirroring the
	// restraint of the Access -> IsWrite derivation: no redundant state by default.
	Destructive *bool
	Idempotent  *bool

	// Transform is an optional escape hatch for reshaping the response. The vast
	// majority of operations do not need it: by default the framework passes the
	// upstream JSON through unchanged. Set it only when an operation must rewrite
	// the response structure, so that this complexity is not imposed on everyone.
	Transform func(raw any) (any, error)

	// WSCollect is the data payload for Kind == KindWSCollect: it constructs
	// this operation's WS config from the collected flag values, the
	// resolved api-key/secret (empty strings for a public channel), and
	// whether this is a --dry-run invocation. It is meaningless (must be
	// nil) for every other Kind; ValidateDecl enforces that Kind and
	// WSCollect agree (checkKindWSCollectConsistency). Every dispatch site
	// tests Kind, never WSCollect's nil-ness directly — see OperationKind's
	// doc comment for why.
	//
	// The return type is untyped (any, not a concrete
	// *pkg/transport.WSConfig) for the same reason Body is declared as a
	// prototype value rather than a concrete transport type: pkg/api must
	// not import pkg/transport merely to name this return type, keeping
	// this the lowest-level contract package. internal/cli's runWSOperation
	// is the one place that knows the concrete shape and asserts it back.
	//
	// This closure sets the returned WSConfig's HostKey (a plain string
	// naming which gateway to dial — "spot-ws" / "futures-ws"), never a
	// literal URL: exactly the same shape RequestSpec.HostKey already takes
	// for a REST operation. Resolving HostKey to an actual URL (default,
	// MEXC_<KEY>_BASE_URL, or a per-invocation flag override) happens
	// entirely in internal/cli, one layer up — mirroring
	// internal/transport/send.go's resolveBase — so this closure, and this
	// package, never need to know a concrete endpoint or how it might be
	// overridden.
	//
	// dryRun matters when WSCollect has a side effect beyond pure
	// computation — e.g. Spot's ws-deals exchanges a real signed REST call
	// for a listenKey. WSCollect must skip that call when dryRun is true,
	// returning a config with a placeholder/representative URL. A WSCollect
	// with no such side effect (every public channel) can ignore dryRun
	// entirely.
	//
	// An operation with Kind == KindWSCollect omits Method and Path (see
	// ValidateDecl): it never builds a RequestSpec, so neither concept
	// applies.
	WSCollect func(values map[string]string, apiKey, secretKey string, dryRun bool) (any, error)

	// Kind discriminates this operation's execution model. The zero value
	// (KindREST) is the overwhelmingly common case; set it explicitly only
	// for a non-REST execution model (currently just KindWSCollect, paired
	// with WSCollect).
	Kind OperationKind
}

// IsWrite reports whether this is a write operation (access != read). It is used
// for read-only binary filtering and for grading write warnings.
func (o Operation) IsWrite() bool {
	return o.Access != Read
}

// DestructiveHint reports whether this operation may destroy or overwrite
// existing state. By default a write is treated as destructive and a read is
// not; an operation that only adds state (e.g. placing an order) can override
// this to false via the Destructive field. This is the source of truth for the
// MCP destructiveHint annotation and for grading write warnings.
func (o Operation) DestructiveHint() bool {
	if o.Destructive != nil {
		return *o.Destructive
	}
	return o.IsWrite()
}

// IdempotentHint reports whether repeating this operation converges to the same
// state. By default a read is idempotent and a write is not; a write that is
// safe to repeat (e.g. cancel, amend, or setting a preference) can override this
// to true via the Idempotent field. This is the source of truth for the MCP
// idempotentHint annotation.
func (o Operation) IdempotentHint() bool {
	if o.Idempotent != nil {
		return *o.Idempotent
	}
	return !o.IsWrite()
}

// RiskTier is the write-confirmation grade the framework derives for an
// operation. It is NOT a new declaration field: the business owner only ever
// sets Access / Destructive / Idempotent (the same fields that feed the MCP
// annotations), and this projects them onto the single axis the confirmation
// flow consumes. It is a closed enum, string-backed for readability like Access
// / Signing / Identity.
type RiskTier string

const (
	// TierNone is a read: no confirmation is asked.
	TierNone RiskTier = "none"
	// TierLight is a write that either only adds state (non-destructive, e.g.
	// placing an order) or is safe to repeat/undo (idempotent, e.g. cancelling).
	// It is confirmed with neutral wording.
	TierLight RiskTier = "light"
	// TierHeavy is a destructive AND non-idempotent write — one that overwrites or
	// removes state and cannot be walked back by repeating it (e.g. a withdrawal).
	// It is confirmed with the strongest wording. An unlabeled write defaults here
	// (DestructiveHint true, IdempotentHint false), so forgetting to grade a write
	// fails safe toward the stricter confirmation.
	TierHeavy RiskTier = "heavy"
)

// RiskTier derives the confirmation grade from the operation's write semantics.
// A read needs no confirmation; every write does. Within writes, only one that
// is destructive AND non-idempotent — it changes state irreversibly and cannot
// be undone by repeating it — is graded Heavy; every other write is Light. It
// reuses DestructiveHint / IdempotentHint, so it adds no declaration burden and
// inherits their fail-safe defaults (an unlabeled write is Heavy). This is the
// single source the CLI and MCP confirmation paths both consume; the tier only
// changes the wording/strictness of the prompt, never whether a write can run.
func (o Operation) RiskTier() RiskTier {
	if !o.IsWrite() {
		return TierNone
	}
	if o.DestructiveHint() && !o.IdempotentHint() {
		return TierHeavy
	}
	return TierLight
}

// ParamSchema is the machine-readable view of a single Param, used by the
// `schema` command so an agent can inspect parameter structure before calling.
// In carries the canonical location (the zero value is normalized to InQuery),
// so the serialized value is always a concrete "query"/"path"/… — the same
// closed-enum type the declaration uses, and consistent with how Schema.Signed
// and Schema.Identity carry their effective enum values.
type ParamSchema struct {
	Name      string   `json:"name"`
	Desc      string   `json:"desc,omitempty"`
	Required  bool     `json:"required"`
	Enum      []string `json:"enum,omitempty"`
	In        ParamIn  `json:"in"`                  // query / path / header / cookie / form
	Sensitive bool     `json:"sensitive,omitempty"` // value is masked in the write-confirmation display
	MinVal    *float64 `json:"minval,omitempty"`    // numeric lower bound, inclusive
	MaxVal    *float64 `json:"maxval,omitempty"`    // numeric upper bound, inclusive
	MinLen    int      `json:"minlen,omitempty"`    // minimum string length
	MaxLen    int      `json:"maxlen,omitempty"`    // maximum string length
	Pattern   string   `json:"pattern,omitempty"`   // regex the value must fully match
}

// BodyField is the machine-readable view of one field of a Body prototype
// struct, projected from its struct tags. A nested object or array-of-object
// carries its own Fields, so an agent can inspect the full JSON body shape from
// the schema without guessing.
type BodyField struct {
	Name     string      `json:"name"`             // wire name (the json tag)
	Type     string      `json:"type"`             // string / integer / number / boolean / object / array
	Required bool        `json:"required"`         // has validate:"required"
	Desc     string      `json:"desc,omitempty"`   // the desc tag
	Enum     []any       `json:"enum,omitempty"`   // the enum tag, split on comma, each value typed by Type
	Fields   []BodyField `json:"fields,omitempty"` // nested object / array-of-object fields
	// Sensitive marks a body field whose value is masked in the write-confirmation
	// display (declared with the `sensitive:"true"` struct tag, e.g. a token in the
	// request body). Rare; the default shows the value so the user can verify it.
	Sensitive bool `json:"sensitive,omitempty"`
	// Pre-validation constraints, projected from the struct tags `minval` / `maxval`
	// (numeric, inclusive) / `minlen` / `maxlen` (string length) / `pattern` (regex).
	// Checked by ValidateBody before the request is sent, on both faces. Zero value
	// = no limit.
	MinVal  *float64 `json:"minval,omitempty"`
	MaxVal  *float64 `json:"maxval,omitempty"`
	MinLen  int      `json:"minlen,omitempty"`
	MaxLen  int      `json:"maxlen,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
	// RequiredIf declares a conditional presence rule: the field is required when
	// a sibling field's value starts with a given prefix. Format: "Field:prefix"
	// (e.g. "ValueType:JSON_" means required when valueType starts with "JSON_").
	// Projected from the `required_if` struct tag.
	RequiredIf string `json:"required_if,omitempty"`
}

// BodySchema is the machine-readable view of an Operation's request body: the
// CLI flag that carries it (always --data), its content type, and the reflected
// field structure of the Body prototype.
type BodySchema struct {
	Flag        string `json:"flag"`         // always --data
	ContentType string `json:"content_type"` // application/json
	// Shape is "object" when --data carries one JSON object, "array" when it
	// carries a JSON array of them. Read it before Fields: for an array body the
	// fields describe each ITEM, not the body.
	Shape string `json:"shape"`
	// ItemType is set for array bodies. Struct arrays publish "object" and carry
	// Fields for each item; scalar arrays publish their scalar JSON type and have
	// no Fields.
	ItemType string      `json:"item_type,omitempty"`
	Fields   []BodyField `json:"fields,omitempty"`
}

// PermissionSchema is the machine-readable view of a PermReq.
type PermissionSchema struct {
	System string   `json:"system"`
	Codes  []string `json:"codes"`
}

// MultipartFileSchema is the machine-readable view of one declared file field of
// a multipart upload: the CLI supplies it via --file <field>=<path>.
type MultipartFileSchema struct {
	Field    string `json:"field"`
	Desc     string `json:"desc,omitempty"`
	Required bool   `json:"required"`
}

// MultipartSchema is the machine-readable view of a multipart/form-data upload:
// the flag that carries the files (--file, repeatable as <field>=<path>), the
// declared file fields, and — when the op declares companion text fields — the
// flag that carries them (--data) and their reflected field structure. It lets
// an agent inspect an upload's shape from `schema` without guessing.
type MultipartSchema struct {
	ContentType string                `json:"content_type"` // multipart/form-data
	FileFlag    string                `json:"file_flag"`    // always --file
	Files       []MultipartFileSchema `json:"files"`
	FieldsFlag  string                `json:"fields_flag,omitempty"` // --data, when text fields are declared
	Fields      []BodyField           `json:"fields,omitempty"`      // text fields (from the Fields prototype)
}

// Schema is the machine-readable view of an Operation. It deliberately mirrors
// what the SKILL.md tables and CLI flags already declare (the single source of
// truth) rather than introducing new data, and omits
// the non-serializable Transform escape hatch. The `schema` command emits this
// as JSON so an agent does not have to guess the --<flag> structure.
// Signed is a boolean (whether the request is signed) — the signing scheme
// NAME is deliberately kept internal to the CLI, never exposed to the agent.
// Identity is the EFFECTIVE identity realm (subdomain inheritance already
// resolved), so the agent always sees a concrete identity value.
type Schema struct {
	Operation    string            `json:"operation"` // fully-qualified <domain>.<sub>.<name>
	Name         string            `json:"name"`
	Method       string            `json:"method,omitempty"`
	Path         string            `json:"path,omitempty"`
	Access       Access            `json:"access"`
	Write        bool              `json:"write"`       // derived from access (access != read); see IsWrite
	Destructive  bool              `json:"destructive"` // derived; may destroy/overwrite state. see DestructiveHint
	Idempotent   bool              `json:"idempotent"`  // derived; safe to repeat. see IdempotentHint
	Signed       bool              `json:"signed"`      // whether the request is signed (the scheme NAME stays internal to the CLI)
	Identity     Identity          `json:"identity"`    // effective identity realm
	HostKey      string            `json:"host_key,omitempty"`
	Permission   *PermissionSchema `json:"permission,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Params       []ParamSchema     `json:"params"`
	Body         *BodySchema       `json:"body,omitempty"`          // present when the op has a JSON request body (carried on --data)
	Multipart    *MultipartSchema  `json:"multipart,omitempty"`     // present when the op is a multipart upload (files on --file)
	ResponseKind ResponseKind      `json:"response_kind,omitempty"` // present only for a binary (download) response; JSON is the omitted default
	// Stream is true for a WS-backed operation (Method/Path are omitted for
	// one — see above); false (omitted) for every ordinary request-response
	// operation.
	Stream bool `json:"stream,omitempty"`
}

// Schema returns the machine-readable view of this Operation. The fully-qualified
// id (e.g. spot.trade.place) and the effective signed/identity are supplied by
// the caller — the assembly layer — because they are the only pieces the
// Operation itself does not know (it carries no domain/sub context, so
// subdomain-default inheritance cannot resolve here).
func (o Operation) Schema(qualifiedID string, effSigned Signing, effIdentity Identity) Schema {
	params := make([]ParamSchema, 0, len(o.Params))
	for _, p := range o.Params {
		params = append(params, ParamSchema{
			Name:      p.Name,
			Desc:      p.Desc,
			Required:  p.Required,
			Enum:      p.Enum,
			In:        p.In.Canonical(),
			Sensitive: p.Sensitive,
			MinVal:    p.MinVal,
			MaxVal:    p.MaxVal,
			MinLen:    p.MinLen,
			MaxLen:    p.MaxLen,
			Pattern:   p.Pattern,
		})
	}
	var perm *PermissionSchema
	if o.Permission != nil {
		perm = &PermissionSchema{System: o.Permission.System, Codes: o.Permission.Codes}
	}
	var body *BodySchema
	if o.Body != nil {
		body = &BodySchema{
			Flag:        "--data",
			ContentType: "application/json",
			Shape:       o.BodyShape(),
			ItemType:    o.BodyItemType(),
			Fields:      o.BodyFields(),
		}
	}
	var multipart *MultipartSchema
	if o.Multipart != nil {
		files := make([]MultipartFileSchema, 0, len(o.Multipart.Files))
		for _, mf := range o.Multipart.Files {
			// A conversion, not a field-by-field literal: the two types differ only in
			// JSON tags, so adding a field to either one fails to compile here until
			// someone decides what it means. A literal would silently zero-fill it.
			files = append(files, MultipartFileSchema(mf))
		}
		multipart = &MultipartSchema{
			ContentType: "multipart/form-data",
			FileFlag:    "--file",
			Files:       files,
		}
		if o.Multipart.Fields != nil {
			multipart.FieldsFlag = "--data"
			multipart.Fields = reflectBodyFields(reflect.TypeOf(o.Multipart.Fields))
		}
	}
	return Schema{
		Operation:    qualifiedID,
		Name:         o.Name,
		Method:       o.Method,
		Path:         o.Path,
		Access:       o.Access,
		Write:        o.IsWrite(),
		Destructive:  o.DestructiveHint(),
		Idempotent:   o.IdempotentHint(),
		Signed:       effSigned.Signs(),
		Identity:     effIdentity,
		HostKey:      o.HostKey,
		Permission:   perm,
		Summary:      o.Summary,
		Params:       params,
		Body:         body,
		Multipart:    multipart,
		ResponseKind: o.ResponseKind,
		Stream:       o.Kind == KindWSCollect,
	}
}

// BodyFields returns the reflected field tree of the operation's JSON Body
// prototype, or nil when the op declares no Body. It exposes the same projection
// the schema face uses (BodySchema.Fields) so the MCP face can build a structured
// body input schema from one source of truth, without re-reflecting the tags.
// For an array body the tree describes ONE element, which is what every consumer
// needs: validation walks it per item, and both faces render it as the array's
// item schema. Pair it with BodyShape to know which.
func (o Operation) BodyFields() []BodyField {
	if o.Body == nil {
		return nil
	}
	el, _ := bodyElemType(reflect.TypeOf(o.Body))
	return reflectBodyFields(el)
}

// BodyShape reports whether the declared body is one JSON object
// (BodyShapeObject) or an array of them (BodyShapeArray). It is empty when the op
// declares no Body. Read it together with BodyFields, which describes the object
// in either case.
func (o Operation) BodyShape() string {
	if o.Body == nil {
		return ""
	}
	_, shape := bodyElemType(reflect.TypeOf(o.Body))
	return shape
}

// BodyItemType reports the JSON type of an array body's items. Struct arrays
// report object; scalar arrays report string / integer / number / boolean. It is
// empty for object bodies and operations without a Body.
func (o Operation) BodyItemType() string {
	if o.Body == nil || o.BodyShape() != BodyShapeArray {
		return ""
	}
	el, _ := bodyElemType(reflect.TypeOf(o.Body))
	return jsonKind(el)
}

// MaskSensitiveBody returns the JSON body data with the value of every field
// tagged `sensitive:"true"` (at any depth) replaced by SensitiveMask, for the
// write-confirmation display. It handles both body shapes: an object body is
// masked directly, an array body element by element. It returns data unchanged
// when the op has no body, the data does not match the declared shape, or no
// field was masked (so a body with no sensitive field keeps its original
// formatting). Errors are non-fatal: on any decode/encode problem the original
// data is returned, never a partial result.
//
// Returning the original on a decode failure is safe ONLY because a mismatched
// body never reaches the prompt — ValidateBody runs first and rejects it. The
// fallback exists for malformed input under --dry-run, which skips validation.
func (o Operation) MaskSensitiveBody(data string) string {
	if o.Body == nil {
		return data
	}
	fields := o.BodyFields()
	if o.BodyShape() == BodyShapeArray {
		var arr []any
		if decodeKeepingNumbers(data, &arr) != nil {
			return data // not a JSON array (or malformed) — show as-is
		}
		masked := false
		for _, el := range arr {
			// Elements are maps by reference, so masking one mutates it inside arr.
			if obj, ok := el.(map[string]any); ok && maskSensitiveFields(fields, obj) {
				masked = true
			}
		}
		if !masked {
			return data
		}
		b, err := json.Marshal(arr)
		if err != nil {
			return data
		}
		return string(b)
	}
	var m map[string]any
	if decodeKeepingNumbers(data, &m) != nil {
		return data // not a JSON object (or malformed) — show as-is
	}
	if !maskSensitiveFields(fields, m) {
		return data // nothing sensitive — preserve the original text
	}
	b, err := json.Marshal(m)
	if err != nil {
		return data
	}
	return string(b)
}

// decodeKeepingNumbers decodes data into dst with every number kept as its source
// literal. Through the default float64 a large integer re-encodes as a DIFFERENT
// number, so the masked body would show — and be fingerprinted as — a value that
// was never sent. Rejects trailing content, like json.Unmarshal.
func decodeKeepingNumbers(data string, dst any) error {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errTrailingJSON
	}
	return nil
}

var errTrailingJSON = errors.New("unexpected trailing content after the JSON value")

// maskSensitiveFields walks the declared field tree against a decoded JSON object
// and replaces each present Sensitive field's value with SensitiveMask, recursing
// into nested objects and arrays of objects. It reports whether anything was
// masked.
func maskSensitiveFields(fields []BodyField, m map[string]any) bool {
	masked := false
	for _, f := range fields {
		v, present := m[f.Name]
		if !present {
			continue
		}
		if f.Sensitive {
			m[f.Name] = SensitiveMask
			masked = true
			continue
		}
		if len(f.Fields) == 0 {
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			if maskSensitiveFields(f.Fields, child) {
				masked = true
			}
		case []any:
			for _, el := range child {
				if obj, ok := el.(map[string]any); ok {
					if maskSensitiveFields(f.Fields, obj) {
						masked = true
					}
				}
			}
		}
	}
	return masked
}

// BodyShapeObject and BodyShapeArray name the two JSON shapes a Body prototype
// can declare. A struct prototype means the body IS one JSON object; a slice
// prototype means it is a JSON array. A slice of structs carries a field tree for
// each object item; a slice of scalars carries only the scalar item type.
const (
	BodyShapeObject = "object"
	BodyShapeArray  = "array"
)

// bodyElemType resolves a Body prototype's type to the type describing ONE JSON
// item in the body, together with the body's shape. A struct prototype describes
// the body itself; a slice/array prototype describes each element.
// Pointers are dereferenced at both levels, so *T, []T, []*T and *[]*T all resolve
// to T.
//
// A prototype that is neither a struct, a slice of structs, or a slice of scalars
// is rejected by ValidateDecl: nothing downstream could describe or check it.
func bodyElemType(t reflect.Type) (reflect.Type, string) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return nil, BodyShapeObject
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		el := t.Elem()
		for el != nil && el.Kind() == reflect.Pointer {
			el = el.Elem()
		}
		return el, BodyShapeArray
	}
	return t, BodyShapeObject
}

// reflectBodyFields projects a Body prototype struct type into its schema
// fields, reading the json / validate / enum / desc tags and recursing into
// nested structs and slices of structs. Pointers are dereferenced; a non-struct
// type yields no fields. This keeps the Body declaration the single source of
// truth for the body face, exactly as Params are for the query/path/header face.
//
// It takes the type of ONE object. A top-level array body is unwrapped by
// bodyElemType before reaching here, so this function never has to know which
// shape the body is.
func reflectBodyFields(t reflect.Type) []BodyField {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var fields []BodyField
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported field
			continue
		}
		name := jsonFieldName(sf)
		if name == "-" { // json:"-" opts the field out
			continue
		}
		bf := BodyField{
			Name:       name,
			Required:   tagHasOption(sf.Tag.Get("validate"), "required"),
			Desc:       sf.Tag.Get("desc"),
			Sensitive:  sf.Tag.Get("sensitive") == "true",
			MinVal:     parseFloatTag(sf.Tag.Get("minval")),
			MaxVal:     parseFloatTag(sf.Tag.Get("maxval")),
			MinLen:     parseIntTag(sf.Tag.Get("minlen")),
			MaxLen:     parseIntTag(sf.Tag.Get("maxlen")),
			Pattern:    sf.Tag.Get("pattern"),
			RequiredIf: sf.Tag.Get("required_if"),
		}
		ft := sf.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		bf.Type = jsonKind(ft)
		// Enum tokens are parsed AFTER Type is known, so each allowed value is
		// carried with the field's JSON kind (an integer field's enum:"1,2" becomes
		// [1,2], not ["1","2"]). This keeps the schema/MCP faces type-consistent with
		// the strict body decode and lets validation compare by value.
		if e := sf.Tag.Get("enum"); e != "" {
			bf.Enum = enumValues(bf.Type, strings.Split(e, ","))
		}
		switch ft.Kind() {
		case reflect.Struct:
			bf.Fields = reflectBodyFields(ft)
		case reflect.Slice, reflect.Array:
			bf.Fields = reflectBodyFields(ft.Elem())
		}
		fields = append(fields, bf)
	}
	return fields
}

// enumValues parses the comma-separated enum tag tokens into values typed by the
// field's JSON kind: an integer field yields int64s, a number field float64s, a
// boolean field bools, and any other kind keeps the raw string tokens. A token
// that does not parse for a numeric/boolean kind falls back to its raw string
// form — a declaration smell, but preserved rather than dropped. Numeric/boolean
// tokens are trimmed before parsing; string tokens are kept verbatim (a string
// enum's whitespace can be significant).
func enumValues(kind string, tokens []string) []any {
	out := make([]any, 0, len(tokens))
	for _, tok := range tokens {
		switch kind {
		case "integer":
			if n, err := strconv.ParseInt(strings.TrimSpace(tok), 10, 64); err == nil {
				out = append(out, n)
				continue
			}
		case "number":
			if f, err := strconv.ParseFloat(strings.TrimSpace(tok), 64); err == nil {
				out = append(out, f)
				continue
			}
		case "boolean":
			if b, err := strconv.ParseBool(strings.TrimSpace(tok)); err == nil {
				out = append(out, b)
				continue
			}
		}
		out = append(out, tok)
	}
	return out
}

// enumValueMatchesKind reports whether a typed enum value (as produced by
// enumValues) matches the field's JSON kind: a numeric/boolean field's values
// must have typed cleanly (int64 / float64 / bool), whereas a string field
// accepts any token. It is the declaration-time counterpart of enumValues: a
// numeric/boolean token that fell back to a string signals an unparseable token.
func enumValueMatchesKind(kind string, v any) bool {
	switch kind {
	case "integer":
		_, ok := v.(int64)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	default: // string
		return true
	}
}

// jsonFieldName returns the wire name of a struct field: the json tag's name
// part, or the Go field name when there is no json tag.
func jsonFieldName(sf reflect.StructField) string {
	tag := sf.Tag.Get("json")
	if tag == "" {
		return sf.Name
	}
	name := tag
	if i := strings.IndexByte(tag, ','); i >= 0 {
		name = tag[:i]
	}
	if name == "" {
		return sf.Name
	}
	return name
}

// tagHasOption reports whether a comma-separated tag value (e.g. a `validate`
// tag) contains the given option token.
func tagHasOption(tag, opt string) bool {
	for _, part := range strings.Split(tag, ",") {
		if strings.TrimSpace(part) == opt {
			return true
		}
	}
	return false
}

// parseFloatTag projects a `minval` / `maxval` struct-tag string into a *float64
// (nil when the tag is absent). A malformed tag also yields nil here; it is
// reported as a declaration error by ValidateDecl (validateBodyConstraints),
// which re-reads the raw tag — so a value reaching validation is known-good.
func parseFloatTag(s string) *float64 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

// parseIntTag projects a `minlen` / `maxlen` struct-tag string into an int
// (0 when absent or malformed; ValidateDecl reports a malformed tag).
func parseIntTag(s string) int {
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

// jsonKind maps a Go type's kind to the JSON type name surfaced in the schema.
func jsonKind(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Struct, reflect.Map:
		return "object"
	default:
		return "string"
	}
}

// Ptr returns a pointer to v. It exists so the optional *bool overrides
// (Destructive / Idempotent) can be set inline with an immutable value —
// api.Ptr(true) — instead of taking the address of a shared, mutable package
// variable that a stray assignment could later flip.
func Ptr[T any](v T) *T { return &v }
