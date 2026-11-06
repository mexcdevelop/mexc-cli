// Package fileio computes safe local filenames for downloads and writes them
// atomically. It is deliberately free of any transport/api dependency: callers
// pass in the request identity and response metadata as plain values, so this
// stays a small, pure, unit-testable helper.
package fileio

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// hashLen is how many hex characters of the request hash form the uniqueness
// token. 12 hex = 48 bits: collision-resistant well past any realistic number of
// downloads, while staying short enough to keep a filename readable.
const hashLen = 12

// NameInput is everything DefaultDownloadName needs: the request identity (which
// makes the name unique per resource) and the response metadata (which makes it
// readable). Query is the already-canonicalized query string.
type NameInput struct {
	Method             string
	Path               string
	Query              string
	Body               []byte
	ContentDisposition string
	ContentType        string
}

// DefaultDownloadName derives the filename used when the user did not name the
// output file. It is "<readable>-<hash>[.ext]", where:
//   - <hash> is a short digest of the fully-resolved request (method + path +
//     query + body). It is the uniqueness guarantee: the same resource always
//     maps to the same name, and two distinct resources never collide — even if
//     the server hands back the same human filename for both.
//   - <readable> is the server-provided Content-Disposition filename (sanitized),
//     included only as a human-friendly prefix; absent, the name is just <hash>.
//   - .ext comes from the Content-Disposition filename, else the Content-Type;
//     an unknown type gets no synthetic extension.
func DefaultDownloadName(in NameInput) string {
	h := resourceHash(in)
	stem, ext := readableParts(in.ContentDisposition, in.ContentType)
	if stem != "" {
		return stem + "-" + h + ext
	}
	return h + ext
}

// resourceHash digests the fully-resolved request. Body is included so two
// exports that differ only by request body (not path/query) still get distinct
// names.
func resourceHash(in NameInput) string {
	h := sha256.New()
	// Length-agnostic separators keep distinct field boundaries unambiguous.
	// h.Write rather than io.WriteString: hash.Hash.Write is documented never to
	// return an error, and writing it this way says so without a discard.
	h.Write([]byte(in.Method))
	h.Write([]byte("\n"))
	h.Write([]byte(in.Path))
	h.Write([]byte("\n"))
	h.Write([]byte(in.Query))
	h.Write([]byte("\n"))
	h.Write(in.Body)
	return hex.EncodeToString(h.Sum(nil))[:hashLen]
}

// readableParts extracts a sanitized readable stem and an extension. The stem
// comes only from a Content-Disposition filename; the extension from that
// filename, else from a small curated Content-Type map.
func readableParts(contentDisposition, contentType string) (stem, ext string) {
	if fn := sanitizeBase(filenameFromDisposition(contentDisposition)); fn != "" {
		ext = filepath.Ext(fn)
		return strings.TrimSuffix(fn, ext), ext
	}
	return "", extFromContentType(contentType)
}

// filenameFromDisposition returns the raw filename from a Content-Disposition
// header (preferring the RFC 5987 filename* form), or "" if absent/unparseable.
func filenameFromDisposition(cd string) string {
	if cd == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return ""
	}
	if fn := params["filename*"]; fn != "" {
		return fn
	}
	return params["filename"]
}

// sanitizeBase reduces a possibly-hostile server-supplied name to a single safe
// path element: it strips control characters and dangerous Unicode (so a bidi
// override or zero-width char cannot spoof the saved filename), then defuses
// traversal ("../../etc/passwd" -> "passwd"). Stripping (rather than rejecting)
// degrades a weird name gracefully — the hash suffix still guarantees a valid,
// unique filename even if the readable part reduces to nothing. Returns "" for
// names that reduce to nothing usable (the caller then uses the hash alone).
func sanitizeBase(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = stripUnsafeRunes(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

// contentTypeExt is a small, curated MIME->extension map. It is intentionally
// short and system-independent (unlike mime.ExtensionsByType, which varies by
// host); unknown types simply get no extension rather than a wrong guess.
var contentTypeExt = map[string]string{
	"application/pdf":  ".pdf",
	"application/json": ".json",
	"application/zip":  ".zip",
	"application/gzip": ".gz",
	"text/csv":         ".csv",
	"text/plain":       ".txt",
	"text/html":        ".html",
	"image/png":        ".png",
	"image/jpeg":       ".jpg",
	"image/gif":        ".gif",
	"image/webp":       ".webp",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       ".xlsx",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
}

// stripUnsafeRunes removes control characters (including tab/newline, which have
// no place in a filename) and dangerous Unicode from a server-supplied name.
func stripUnsafeRunes(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || isDangerousUnicode(r) {
			return -1
		}
		return r
	}, s)
}

func extFromContentType(ct string) string {
	if ct == "" {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(ct, ";")[0])
	}
	return contentTypeExt[strings.ToLower(mediaType)]
}

// ResolveOutputPath turns the --output flag into the final absolute file path,
// validating up front (before any download starts) so a bad target fails fast.
//
// The destination is SANDBOXED to the current working directory: --output must
// be a relative path that, after cleaning and resolving symlinks, stays inside
// the cwd. An absolute path, a `..` escape, a symlink pointing outside, or a
// control character is rejected. This confines a download's filesystem effect to
// the directory the user launched the CLI in — a meaningful guard when an agent
// drives the command and could otherwise be steered into writing a sensitive
// absolute path.
//
// Within that sandbox: empty -> defaultName in the cwd; an existing directory ->
// defaultName inside it; anything else -> the given file path. A missing parent
// directory is created at write time (AtomicWriteFromReader), so it is not an
// error here. An existing file is not clobbered unless overwrite is set.
func ResolveOutputPath(output string, overwrite bool, defaultName string) (string, error) {
	raw := output
	if raw == "" {
		raw = defaultName
	}
	target, err := safeTargetPath(raw)
	if err != nil {
		return "", err
	}
	// If --output pointed at an existing directory, save the derived name inside
	// it, re-validating the joined path against the sandbox.
	if fi, statErr := os.Stat(target); statErr == nil && fi.IsDir() {
		target, err = safeTargetPath(filepath.Join(raw, defaultName))
		if err != nil {
			return "", err
		}
	}

	if fi, statErr := os.Stat(target); statErr == nil {
		if fi.IsDir() {
			return "", errs.Validation(errs.SubtypeInvalidArgument,
				"output path %q is a directory", target).WithParam("--output")
		}
		if !overwrite {
			return "", errs.Validation(errs.SubtypeInvalidArgument,
				"output file already exists: %s", target).
				WithParam("--output").
				WithHint("pass --overwrite to replace it, or choose a different --output")
		}
	}
	return target, nil
}

// safeTargetPath cleans and resolves a user-supplied relative path against the
// current working directory, returning an absolute path guaranteed to live
// inside it. It rejects control characters, empty input, absolute paths, and any
// path that resolves (through `..` or symlinks) outside the cwd.
func safeTargetPath(raw string) (string, error) {
	if err := rejectControlChars(raw); err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--output must not be empty").WithParam("--output")
	}
	if filepath.IsAbs(raw) {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--output must be a relative path within the current directory, got %q", raw).
			WithParam("--output").
			WithHint("cd to the target directory first, then use a relative path like ./name")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", errs.Internal("cannot determine working directory: %v", err).WithCause(err)
	}
	resolved := filepath.Join(cwd, filepath.Clean(raw))

	// Resolve symlinks for the path if it exists, else for its nearest existing
	// ancestor, so a symlink that escapes the cwd cannot smuggle the write out.
	if _, lstatErr := os.Lstat(resolved); lstatErr == nil {
		resolved, err = filepath.EvalSymlinks(resolved)
	} else {
		resolved, err = resolveNearestAncestor(resolved)
	}
	if err != nil {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"cannot resolve --output path: %v", err).WithParam("--output")
	}

	canonicalCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		canonicalCwd = cwd
	}
	if !isUnderDir(resolved, canonicalCwd) {
		return "", errs.Validation(errs.SubtypeInvalidArgument,
			"--output %q resolves outside the working directory", raw).
			WithParam("--output").
			WithHint("the path must stay within the working directory after resolving .. and symlinks")
	}
	return resolved, nil
}

// rejectControlChars refuses ASCII control characters (including tab and newline,
// which have no place in a path) and dangerous Unicode (bidi overrides,
// zero-width, isolates) in a user-supplied path. These enable terminal/log
// corruption or filename spoofing. It is stricter than a general text input
// guard (which often permits tab/newline) because this validates a filesystem
// path, not free text, matching how server-supplied names are stripped by
// stripUnsafeRunes.
func rejectControlChars(s string) error {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--output contains invalid control characters").WithParam("--output")
		}
		if isDangerousUnicode(r) {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"--output contains disallowed Unicode characters (bidi/zero-width)").WithParam("--output")
		}
	}
	return nil
}

// isDangerousUnicode reports whether r is a Unicode character that can spoof or
// corrupt a displayed filename: bidirectional overrides/embeddings/isolates,
// zero-width characters, the BOM, and line/paragraph separators. These are
// stripped from a server-supplied name and rejected in a user-supplied --output.
func isDangerousUnicode(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200D: // zero-width space / non-joiner / joiner
		return true
	case r == 0xFEFF: // BOM / zero-width no-break space
		return true
	case r >= 0x202A && r <= 0x202E: // bidi: LRE/RLE/PDF/LRO/RLO
		return true
	case r >= 0x2028 && r <= 0x2029: // line / paragraph separator
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates: LRI/RLI/FSI/PDI
		return true
	default:
		return false
	}
}

// resolveNearestAncestor resolves symlinks for the deepest existing ancestor of
// path, then re-appends the non-existent tail. This lets a not-yet-created
// target be sandbox-checked by resolving the real directory it would live in.
func resolveNearestAncestor(path string) (string, error) {
	dir := path
	var tail []string
	for {
		if _, err := os.Lstat(dir); err == nil {
			resolvedDir, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return "", err
			}
			full := resolvedDir
			for i := len(tail) - 1; i >= 0; i-- {
				full = filepath.Join(full, tail[i])
			}
			return full, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the root without finding an existing ancestor
			return path, nil
		}
		tail = append(tail, filepath.Base(dir))
		dir = parent
	}
}

// isUnderDir reports whether path is dir itself or lies within it, computed on
// already-cleaned absolute paths.
func isUnderDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}

// AtomicWriteFromReader streams r into a temp file alongside the target and
// renames it into place on success, so a partial or failed download never leaves
// a truncated file at the target path (and no temp file lingers on failure). It
// creates the parent directory as needed and, as defense in depth, re-enforces
// the working-directory sandbox — resolving symlinks in the target so even a
// caller that bypassed ResolveOutputPath (or a symlink component in the path)
// cannot write outside the cwd. (A local attacker racing a symlink swap between
// this resolution and the rename is out of scope — the same-process download flow
// resolves the path immediately before writing.) The file is private (0600).
func AtomicWriteFromReader(target string, r io.Reader) (int64, error) {
	// Defense in depth: refuse to write outside the working-directory sandbox,
	// even if target did not come through ResolveOutputPath. Resolve the target's
	// symlinks first (via its nearest existing ancestor) so a symlink component
	// cannot smuggle the write out — a lexical check alone would follow it.
	cwd, err := os.Getwd()
	if err != nil {
		return 0, errs.Internal("cannot determine working directory: %v", err).WithCause(err)
	}
	canonicalCwd, evErr := filepath.EvalSymlinks(cwd)
	if evErr != nil {
		canonicalCwd = cwd
	}
	checkPath := target
	if !filepath.IsAbs(checkPath) {
		checkPath = filepath.Join(cwd, checkPath)
	}
	if resolved, rerr := resolveNearestAncestor(checkPath); rerr == nil {
		checkPath = resolved
	}
	if !isUnderDir(checkPath, canonicalCwd) {
		return 0, errs.Validation(errs.SubtypeInvalidArgument,
			"refusing to write outside the working directory: %s", target).WithParam("--output")
	}
	// Create the parent directory (private) if the download targets a new subtree.
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return 0, errs.Internal("cannot create output directory: %v", err).WithCause(err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".mexc-download-*")
	if err != nil {
		return 0, errs.Internal("cannot create temp file for download: %v", err).WithCause(err)
	}
	tmpName := tmp.Name()

	n, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpName)
		return 0, errs.Network("download interrupted: %v", copyErr).WithCause(copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return 0, errs.Internal("cannot finalize download: %v", closeErr).WithCause(closeErr)
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return 0, errs.Internal("cannot move download into place: %v", err).WithCause(err)
	}
	return n, nil
}
