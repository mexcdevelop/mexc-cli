package profile

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

// Profiles let a user keep several named credential sets (e.g. prod vs a
// secondary account) on disk and switch between them, instead of exporting
// MEXC_API_KEY / MEXC_SECRET_KEY by hand each time.
//
// Storage layout under ~/.mexc (dir 0700):
//
//	profiles/<name>   one profile, key=value lines, file mode 0600
//	active            a single line naming the active profile
//
// SECURITY: a profile stores the api key and secret in plaintext, protected
// only by 0600 file permissions inside a 0700 directory. This is a deliberate
// trade-off for multi-account convenience: this is a local, human-operated tool,
// so there is no credential environment variable — credentials live only in a
// saved profile (see resolveConfig). Never commit ~/.mexc, and be aware that disk
// backups/sync (iCloud, Time Machine, dotfile
// repos) will capture it. Load refuses a profile whose file OR whose
// containing directory is more permissive than expected, and writes never follow
// a symlink, so a planted link cannot redirect the plaintext secret elsewhere.
//
// The permission half of that sentence holds only where a file's mode bits ARE
// the access control. Where the OS synthesises them (see
// secfile.ModeBitsMeaningful) the mode is not checked, because checking it there
// tests something other than access. What protects the file instead is whatever
// the OS grants the per-user directory it lives in — which this product does not
// inspect, and therefore does not vouch for. The symlink guard is unaffected.

// Profile is one named credential set: an openapi api key + secret. The
// deployment environment is NOT part of a profile — it is a global setting (see
// settings.go), because env is a routing axis shared
// by every identity, whereas these credentials are openapi-specific.
type Profile struct {
	Name      string
	APIKey    string
	SecretKey string
}

const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// keyRef names this product's wrapping key in the OS keyring. The namespace is
// this product's own — NOT a name any sibling could share, since two binaries
// under one namespace would read and overwrite each other's keys, the keyring
// twin of a shared state directory.
var keyRef = secretstore.Ref{Namespace: "mexc", Account: "profile-key"}

// KeyRef exposes that same ref, so a caller outside this package names the key
// this package actually uses instead of restating it. A second literal is how
// `doctor` ends up reporting the protection of a key nothing reads.
func KeyRef() secretstore.Ref { return keyRef }

// KeyBackend reports where the wrapping key lives — see secretstore.Backend for
// what that does and does not tell you.
func KeyBackend() string { return secretstore.Backend(keyRef) }

// DowngradeKeyToFile materialises the wrapping key into a local file, the
// deliberate escape hatch for a machine with no usable OS keyring.
func DowngradeKeyToFile() (secretstore.DowngradeResult, error) {
	return secretstore.DowngradeToFile(keyRef)
}

// nameTheEscapeHatch appends this product's actual recovery command to a key-store
// failure from pkg/secretstore.
//
// The shared layer says only that the key can be pinned to a local file "if this CLI
// offers a command for that", because it is compiled into every product built on the
// module and only this one has `profile keychain-downgrade`. A hint naming a command
// the reader cannot run is worse than none — an agent relays a hint verbatim — so
// naming it is the product's job, and this is where the product does it.
//
// Only the hint is touched; type, subtype, message and cause are left alone, so
// errors.Is(err, secretstore.ErrUndecryptable) still works through it.
// The same reasoning applies to the PLATFORM: on one with no OS keyring the
// command exists but refuses, because there is no key to copy out of anything. So
// naming it there would be the exact failure this function was written to prevent
// — a hint pointing at a command that cannot work, relayed verbatim by an agent.
// The shared layer's own conditional wording ("if this CLI offers a command for
// that") is left to stand alone there.
func nameTheEscapeHatch(err error) error {
	var e *errs.Error
	if !errors.As(err, &e) || e.Hint == "" || !strings.Contains(e.Hint, "pin the key to a local file") {
		return err
	}
	if !secretstore.KeyringAvailable() {
		return err
	}
	// WithHint mutates e in place (e aliases err); the returned pointer is discarded
	// deliberately, matching how the command layer already extends a hint.
	_ = e.WithHint("%s The command is `%s profile keychain-downgrade --yes`, run in a terminal "+
		"where the keyring answers.", e.Hint, cliname.Name())
	return err
}

func profilesDir() (string, error) {
	d, err := config.StateDir()
	if err != nil {
		return "", err
	}
	pd := filepath.Join(d, "profiles")
	if err := os.MkdirAll(pd, dirPerm); err != nil {
		return "", errs.Internal("cannot create %s: %v", pd, err).WithCause(err)
	}
	return pd, nil
}

// validProfileName guards the name against path traversal and odd characters,
// since it becomes a filename. Lowercase letters, digits, hyphen, underscore.
func validProfileName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

func invalidName(name string) error {
	return errs.Validation(errs.SubtypeInvalidArgument,
		"invalid profile name %q: use lowercase letters, digits, '-' or '_' (max 64)", name)
}

// checkDirPerm refuses if the profiles dir is group/world accessible — an
// over-permissive dir defeats the file's own 0600, since another local user
// could traverse in and read it.
//
// Where the OS synthesises mode bits there is nothing here to judge, and this
// becomes a stat that only reports an unreachable directory. Both loader gates in
// this file are guarded the same way; `doctor` is the one place that has to SAY it
// went unchecked (see secfile.ModeBitsMeaningful).
func checkDirPerm(pd string) error {
	info, err := os.Stat(pd)
	if err != nil {
		return errs.Internal("cannot stat profiles dir: %v", err).WithCause(err)
	}
	if secfile.ModeBitsMeaningful && info.Mode().Perm()&0o077 != 0 {
		return errs.Validation(errs.SubtypeInsecurePermission,
			"profiles directory %s has insecure permissions %o; run: chmod 700 %s",
			pd, info.Mode().Perm(), pd)
	}
	return nil
}

// Save writes a profile to disk with 0600 permissions, overwriting any
// existing one of the same name. It refuses to follow a symlink at the target
// path (a planted link could otherwise redirect the plaintext secret elsewhere).
func Save(p Profile) error {
	if !validProfileName(p.Name) {
		return invalidName(p.Name)
	}
	if p.APIKey == "" || p.SecretKey == "" {
		return errs.Validation(errs.SubtypeMissingArgument,
			"profile %q needs both an Access Key and a Secret Key", p.Name)
	}
	// The on-disk format is line-oriented key=value, so a CR/LF inside any value
	// would be re-parsed on load as an extra field — silently truncating the real
	// value and, worse, letting an injected key=value line alter how the profile
	// parses. Reject control characters up front.
	// The labels match what the MEXC website calls these two values, and what
	// `profile set` prompts for, so an error names the field the user was looking
	// at rather than a synonym they have to map back themselves.
	for field, v := range map[string]string{"Access Key": p.APIKey, "Secret Key": p.SecretKey} {
		if strings.ContainsAny(v, "\r\n") {
			return errs.Validation(errs.SubtypeInvalidArgument,
				"profile %q %s must not contain a newline", p.Name, field)
		}
	}
	pd, err := profilesDir()
	if err != nil {
		return err
	}
	// Refuse to write the plaintext secret into a group/world-traversable dir.
	// The read path already checks this; the write path must match, or the secret
	// lands in an insecure dir and is only caught on a later load.
	if err := checkDirPerm(pd); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("api-key=" + p.APIKey + "\n")
	b.WriteString("api-secret=" + p.SecretKey + "\n")
	// Sealed BEFORE the file is opened, so a failure to reach the key store leaves
	// no file at all rather than a plaintext one waiting to be encrypted later.
	blob, _, serr := secretstore.Seal(keyRef, []byte(b.String()))
	if serr != nil {
		return nameTheEscapeHatch(serr)
	}
	// The temp file goes in the state directory, NOT in profiles/. Audit walks
	// profiles/ and reports anything it cannot load, so a temp left behind by an
	// interrupted write was counted as a stored-but-unreadable profile and turned
	// doctor red — the atomic write inventing the very failure it exists to
	// prevent. Same filesystem either way, so the rename stays atomic.
	stateDir, derr := config.StatePath()
	if derr != nil {
		return derr
	}
	return writeAtomic(filepath.Join(pd, p.Name), filepath.Join(stateDir, "."+p.Name+".profile.tmp"), blob, p.Name)
}

// writeAtomic writes through a temp file and renames over the target.
//
// In place with O_TRUNC — what this used to do — means a crash or a full disk
// between truncate and write destroys the previous good profile and leaves a
// short file behind. Since the content is now ciphertext, that stub reads back
// as "cannot be decrypted", i.e. an interrupted write is indistinguishable from
// key loss and invites exactly the wrong remedy. rename(2) is atomic on the same
// filesystem, so a reader sees the old profile or the new one, never a fragment.
// The same create-write-chmod-rename shape used elsewhere in this codebase for
// files that must never be observed half-written.
func writeAtomic(path, tmp string, blob []byte, name string) error {
	// A leftover or planted temp path must not be followed or reused.
	if fi, lerr := os.Lstat(tmp); lerr == nil && !fi.Mode().IsRegular() {
		if rerr := os.Remove(tmp); rerr != nil {
			return errs.Internal("cannot replace the temp path for %q: %v", name, rerr).WithCause(rerr)
		}
	}
	f, err := secfile.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return errs.Internal("cannot write profile %q: %v", name, err).WithCause(err)
	}
	if _, werr := f.Write(blob); werr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return errs.Internal("cannot write profile %q: %v", name, werr).WithCause(werr)
	}
	// Chmod before the rename: the file must never be visible at its real name
	// with a mode the loader would refuse.
	if cerr := f.Chmod(filePerm); cerr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return errs.Internal("cannot secure profile %q: %v", name, cerr).WithCause(cerr)
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return errs.Internal("cannot finalize profile %q: %v", name, cerr).WithCause(cerr)
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		_ = os.Remove(tmp)
		return errs.Internal("cannot install profile %q: %v", name, rerr).WithCause(rerr)
	}
	return nil
}

// loadProfileChecked is the shared load used by both Load and the list
// filter. It enforces name validity, directory and file permissions where those
// are meaningful, and parses the key=value body.
func loadProfileChecked(name string) (Profile, error) {
	if !validProfileName(name) {
		return Profile{}, invalidName(name)
	}
	pd, err := profilesDir()
	if err != nil {
		return Profile{}, err
	}
	if err := checkDirPerm(pd); err != nil {
		return Profile{}, err
	}
	path := filepath.Join(pd, name)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, errs.Validation(errs.SubtypeNotFound, "profile %q does not exist", name)
		}
		return Profile{}, errs.Internal("cannot stat profile %q: %v", name, err).WithCause(err)
	}
	if !info.Mode().IsRegular() {
		return Profile{}, errs.Validation(errs.SubtypeFailedPrecondition,
			"profile %q is not a regular file", name)
	}
	// Guarded per checkDirPerm above: real bits only.
	if secfile.ModeBitsMeaningful && info.Mode().Perm()&0o077 != 0 {
		return Profile{}, errs.Validation(errs.SubtypeInsecurePermission,
			"profile %q has insecure permissions %o; run: chmod 600 %s", name, info.Mode().Perm(), path)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, errs.Internal("cannot open profile %q: %v", name, err).WithCause(err)
	}
	// ENCRYPTED FORMAT ONLY — a plaintext file from an older build is not read.
	//
	// Accepting both would be friendlier and is the wrong call: a plaintext
	// profile would go on working forever, so nobody would ever learn they are
	// unprotected, which is precisely the state this is meant to end. There is
	// also nothing to stay compatible with — this is a pre-release, dev-only tool
	// with no published profiles (the same reasoning already applied to retired
	// env= / base-url= lines below). Re-saving takes one command.
	plain, oerr := secretstore.Open(keyRef, blob)
	if oerr != nil {
		return Profile{}, nameTheEscapeHatch(oerr)
	}

	p := Profile{Name: name}
	sc := bufio.NewScanner(strings.NewReader(string(plain)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "api-key":
			p.APIKey = strings.TrimSpace(v)
		case "api-secret":
			p.SecretKey = strings.TrimSpace(v)
		}
		// Any other key (including a retired env= / base-url= / testnet= line from
		// an older build) is simply ignored — see the compatibility note above.
	}
	if err := sc.Err(); err != nil {
		return Profile{}, errs.Internal("cannot read profile %q: %v", name, err).WithCause(err)
	}
	return p, nil
}

// Load reads a profile by name, refusing one whose file or directory has
// permissions more open than 0600 / 0700 (a wider mode defeats the only
// protection plaintext storage has) — on the platforms where that mode means
// anything, per the package doc above.
func Load(name string) (Profile, error) {
	return loadProfileChecked(name)
}

// Rejected is one on-disk entry that Load refuses, paired with the reason. It
// exists so a diagnostic can NAME what List drops: the filtering is silent by
// design (see List), which is right for `profile list` and wrong for anyone
// asking why a profile they saved is not there.
type Rejected struct {
	Name   string
	Reason string
}

// Audit walks the profiles directory once and reports both sides of the filter:
// the usable names and the entries Load refuses, with each rejection's reason.
//
// List is expressed in terms of it rather than walking the directory a second
// time, so "what counts as usable" has exactly one definition. Two walks would
// be free to disagree, and the disagreement would surface as a profile that
// `list` shows and `use` then refuses — the very thing List exists to prevent.
func Audit() (usable []string, rejected []Rejected, err error) {
	pd, derr := profilesDir()
	if derr != nil {
		return nil, nil, derr
	}
	entries, rerr := os.ReadDir(pd)
	if rerr != nil {
		return nil, nil, errs.Internal("cannot list profiles: %v", rerr).WithCause(rerr)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// An invalid name is reported rather than skipped: a file called
		// "Prod.bak" sitting in profiles/ is almost always someone's saved
		// credential that this tool will never load, and saying so beats leaving
		// them to wonder.
		if !validProfileName(e.Name()) {
			rejected = append(rejected, Rejected{
				Name:   e.Name(),
				Reason: "not a valid profile name (lowercase letters, digits, '-' or '_', max 64)",
			})
			continue
		}
		if _, lerr := loadProfileChecked(e.Name()); lerr != nil {
			rejected = append(rejected, Rejected{Name: e.Name(), Reason: reasonOf(lerr)})
			continue
		}
		usable = append(usable, e.Name())
	}
	sort.Strings(usable)
	sort.Slice(rejected, func(i, j int) bool { return rejected[i].Name < rejected[j].Name })
	return usable, rejected, nil
}

// reasonOf renders a rejection for display: the structured Message when the
// error is one of ours (it already carries the remedy, e.g. "run: chmod 600 …"),
// otherwise the raw text.
func reasonOf(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

// List returns the names of usable saved profiles, sorted. It filters
// out entries that Load would reject (invalid names, non-regular files,
// insecure permissions), so `profile list` never advertises an entry that
// `profile use` would then refuse.
//
// The dropped entries are not lost, only unreported here: Audit returns them
// with reasons, which is what `doctor` shows.
func List() ([]string, error) {
	usable, _, err := Audit()
	if err != nil {
		return nil, err
	}
	return usable, nil
}

// Remove deletes a profile. Removing the active profile also clears the
// active pointer, so a later run does not point at a missing profile.
func Remove(name string) error {
	if !validProfileName(name) {
		return invalidName(name)
	}
	pd, err := profilesDir()
	if err != nil {
		return err
	}
	path := filepath.Join(pd, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return errs.Validation(errs.SubtypeNotFound, "profile %q does not exist", name)
		}
		return errs.Internal("cannot remove profile %q: %v", name, err).WithCause(err)
	}
	if active, _ := Active(); active == name {
		if cerr := SetActive(""); cerr != nil {
			return errs.Internal("removed profile %q but could not clear the active pointer: %v", name, cerr).WithCause(cerr)
		}
	}
	return nil
}

// Active returns the name recorded in ~/.mexc/active, or "" if none.
func Active() (string, error) {
	d, err := config.StateDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(d, "active"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errs.Internal("cannot read active profile: %v", err).WithCause(err)
	}
	return strings.TrimSpace(string(b)), nil
}

// SetActive records the active profile name (empty string clears it).
func SetActive(name string) error {
	if name != "" && !validProfileName(name) {
		return invalidName(name)
	}
	d, err := config.StateDir()
	if err != nil {
		return err
	}
	path := filepath.Join(d, "active")
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return errs.Internal("cannot clear active profile: %v", err).WithCause(err)
		}
		return nil
	}
	if err := os.WriteFile(path, []byte(name+"\n"), filePerm); err != nil {
		return errs.Internal("cannot set active profile: %v", err).WithCause(err)
	}
	return nil
}
