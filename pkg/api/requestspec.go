package api

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// CanonicalQuery renders params as the deterministic, key-sorted, percent-encoded
// query string used BOTH when sending a request (internal/transport) AND when signing one
// (pkg/signer). The two must agree byte-for-byte: a signed request's signature is
// computed over this exact form, so the transport has to send the same one or the
// upstream rejects the signature. Keeping a single implementation here — rather
// than a copy in each package — makes that agreement structural instead of a
// convention two functions must be manually kept in sync.
func CanonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	return strings.Join(parts, "&")
}

// RequestSpec is the transport-neutral description of one request. The assembly
// layer builds it (BuildSpec) and internal/transport consumes it. It lives in pkg/api so
// the contract layer depends on nothing: the transport depends on the contract,
// not the reverse. Body / Form / Multipart are mutually exclusive body shapes.
type RequestSpec struct {
	Method     string
	Path       string // {param} already substituted
	Query      map[string]string
	Header     map[string]string
	Cookie     map[string]string
	Form       map[string]string // application/x-www-form-urlencoded
	Body       []byte            // application/json (from --data)
	Multipart  *MultipartBody    // multipart/form-data (from --file + --data)
	HostKey    string
	Signed     Signing
	Identity   Identity
	Permission *PermReq // RBAC requirement copied from the Operation; nil unless the op declares one
	Idempotent bool
	// ResponseKind is copied from the Operation so the transport can decide how to
	// consume the response: buffer+decode (JSON, the zero value) or stream to disk
	// (binary). Keeping it on the spec keeps the transport from sniffing the body.
	ResponseKind ResponseKind
}

// MultipartBody is the runtime transport counterpart of MultipartDecl (declared
// in operation.go): it carries the text fields and one or more read files of a
// multipart upload. Files is a slice, so single- and multi-file uploads share
// one shape.
type MultipartBody struct {
	Fields map[string]string
	Files  []FilePart
}

// FilePart is one already-read file of a MultipartBody: its wire field name, the
// filename to send, and the bytes.
type FilePart struct {
	Field    string
	Filename string
	Content  []byte
}

// BuildSpec routes the collected flag values into a RequestSpec by each Param's
// location, substitutes path placeholders, and attaches the body. For a JSON
// Body op it attaches data as the body; for a Multipart op it packs the given
// files (already read by the assembly layer — file IO does not belong in this
// pure function) plus the text fields parsed from data. effSigned/effIdentity/
// hostKey are resolved by the caller. Value/body validation runs earlier in the
// pipeline (Validate / ValidateBody / ValidateMultipart).
func (o Operation) BuildSpec(values map[string]string, data string, files []FilePart, effSigned Signing, effIdentity Identity, hostKey string) (RequestSpec, error) {
	spec := RequestSpec{
		Method: o.Method, Path: o.Path,
		Query: map[string]string{}, Header: map[string]string{}, Cookie: map[string]string{},
		HostKey: hostKey, Signed: effSigned, Identity: effIdentity, Idempotent: o.IdempotentHint(),
		ResponseKind: o.ResponseKind,
	}
	for _, p := range o.Params {
		v := values[p.Name]
		switch p.In {
		case InPath:
			spec.Path = strings.ReplaceAll(spec.Path, "{"+p.Name+"}", url.PathEscape(v))
		case InHeader:
			if v != "" {
				spec.Header[p.Name] = v
			}
		case InCookie:
			if v != "" {
				spec.Cookie[p.Name] = v
			}
		case InForm:
			if spec.Form == nil {
				spec.Form = map[string]string{}
			}
			if v != "" {
				spec.Form[p.Name] = v
			}
		default: // InQuery / ""
			if v != "" {
				spec.Query[p.Name] = v
			}
		}
	}
	spec.Permission = o.Permission
	if o.Body != nil {
		spec.Body = []byte(data)
	}
	if o.Multipart != nil {
		mb := &MultipartBody{Files: files}
		if o.Multipart.Fields != nil {
			fields, err := parseMultipartFields(data)
			if err != nil {
				return RequestSpec{}, err
			}
			mb.Fields = fields
		}
		spec.Multipart = mb
	}
	return spec, nil
}

// parseMultipartFields turns the --data JSON object of a multipart op into the
// text-field map. Multipart form values are strings on the wire, so each JSON
// scalar is rendered to its string form. Empty data yields no fields (a
// multipart op's payload is its files; the text fields are optional unless the
// Fields prototype marks one required — enforced in ValidateMultipart).
func parseMultipartFields(data string) (map[string]string, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return nil, errs.Validation(errs.SubtypeInvalidArgument,
			"--data must be a JSON object of multipart text fields: %v", err).
			WithParam("--data").
			WithHint("inspect the declared text fields with `%s schema`", cliname.Name())
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = scalarToString(v)
	}
	return out, nil
}

// scalarToString renders a decoded JSON scalar as the string a multipart form
// field carries. A non-scalar (object/array) is re-marshaled to its JSON text.
func scalarToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
