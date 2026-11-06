// Package output funnels a product's machine-readable output.
//
// The hard constraint: stdout is data, stderr is everything else.
//   - program output (a success result / an error envelope) -> stdout
//   - progress, warnings, hints -> stderr
//
// The error envelope carries exactly these fields: type/subtype/param/hint + message.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// errorEnvelope is the error envelope emitted to the agent. omitempty ensures
// optional fields do not appear when unset.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message string `json:"message"`
	Param   string `json:"param,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Code    int    `json:"code,omitempty"`
}

// JSON writes a success result to stdout. The value is the parsed API response (map / slice).
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// JSONLine writes one compact JSON object followed by a newline — a single
// line of a JSON Lines stream, unlike JSON's indented, multi-line encoding.
// Used by a KindWSCollect (WS-backed) operation, which writes one line per
// message as it arrives rather than buffering a whole response to encode at
// once; a consumer can pipe the stream through `jq -c` / `read -r` line by
// line.
func JSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// WriteError writes the error to stderr as an envelope. Anything that is not an
// *errs.Error is wrapped as internal/unexpected.
func WriteError(w io.Writer, err error) {
	var e *errs.Error
	if !errors.As(err, &e) {
		e = errs.Internal("%s", err.Error())
	}
	env := errorEnvelope{Error: errorBody{
		Type:    string(e.Type),
		Subtype: e.Subtype,
		Message: e.Message,
		Param:   e.Param,
		Hint:    e.Hint,
		Code:    e.Code,
	}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if encErr := enc.Encode(env); encErr != nil {
		// If even the envelope cannot be written, fall back to bare text so the error
		// is at least not silently swallowed. A write failure here has nowhere left to
		// be reported, so it is explicitly ignored.
		_, _ = fmt.Fprintln(w, e.Error())
	}
}
