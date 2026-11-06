package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// fileEntry is one parsed --file argument: which declared file field it fills
// and the local path to read. Parsing (this step) is separated from reading
// (readFileParts) so field-shape validation runs before any file IO.
type fileEntry struct {
	field string
	path  string
}

// parseFileEntries turns the raw --file arguments into (field, path) pairs and
// the set of declared file fields that were provided. Each argument is
// "<field>=<path>", or a bare "<path>" when the operation declares exactly one
// file field. It performs NO file IO — required/unknown-field validation runs
// against the returned set (op.ValidateMultipart) before readFileParts reads
// anything, so a bad field name fails before a file is opened.
func parseFileEntries(decl *api.MultipartDecl, args []string) ([]fileEntry, map[string]bool, error) {
	entries := make([]fileEntry, 0, len(args))
	provided := map[string]bool{}
	for _, a := range args {
		field, path, ok := strings.Cut(a, "=")
		if !ok {
			// A bare path is only unambiguous when there is exactly one file field.
			if len(decl.Files) != 1 {
				return nil, nil, errs.Validation(errs.SubtypeInvalidArgument,
					"--file %q must be <field>=<path> (this operation declares %d file fields)", a, len(decl.Files)).
					WithParam("--file").
					WithHint("inspect the declared file fields with `%s schema`", cliname.Name())
			}
			field, path = decl.Files[0].Field, a
		}
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, nil, errs.Validation(errs.SubtypeInvalidArgument,
				"--file %q has an empty field name", a).WithParam("--file")
		}
		if provided[field] {
			return nil, nil, errs.Validation(errs.SubtypeInvalidArgument,
				"file field %q is given more than once", field).WithParam("--file")
		}
		provided[field] = true
		entries = append(entries, fileEntry{field: field, path: path})
	}
	return entries, provided, nil
}

// readFileParts reads each parsed entry's bytes into an api.FilePart, using the
// path's base name as the multipart filename. It runs only after the field
// shape has been validated, so it never reads a file for an undeclared field.
func readFileParts(entries []fileEntry) ([]api.FilePart, error) {
	parts := make([]api.FilePart, 0, len(entries))
	for _, e := range entries {
		content, err := os.ReadFile(e.path)
		if err != nil {
			return nil, errs.Validation(errs.SubtypeInvalidArgument,
				"cannot read --file for %q: %v", e.field, err).
				WithParam("--file").
				WithHint("check the path exists and is readable")
		}
		parts = append(parts, api.FilePart{Field: e.field, Filename: filepath.Base(e.path), Content: content})
	}
	return parts, nil
}
