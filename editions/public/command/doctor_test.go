package command

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/editions/public/adapter"
	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
)

// testSelectedProfile stands in for the --profile flag. In production the
// edition binds that flag to a package var and the identity provider reads it
// through a closure (editions/public/edition.go); doctor reads the same flag
// separately, only to decide how severe a resolution failure is. Wiring both
// sides to one variable here keeps the test faithful to that arrangement — the
// provider and the verdict must be talking about the same selection.
var testSelectedProfile string

// TestMain injects this edition's host rows and identity, mirroring what
// framework.Run does at process start, so the checks resolve against the real
// registrations rather than empty registries.
func TestMain(m *testing.M) {
	// Run against go-keyring's in-memory mock. isolatedHome overrides HOME, which
	// is exactly what makes the login keychain unreachable on macOS, so a real
	// keyring is both unavailable and slow to say so — and provisioning a key would
	// otherwise attempt a WRITE against the developer's own login keychain.
	keyring.MockInit()
	hosts.SetDefaultEnv("prod")
	hosts.Register(adapter.OpenAPIRows())
	credential.Register(adapter.OpenAPI{SelectProfile: func() string { return testSelectedProfile }})
	// Report the INTENDED at-rest state by default. isolatedHome downgrades to a
	// key file (a test cannot reach the OS keyring), so leaving this real would
	// make every credentials assertion measure the harness. Tests that care about
	// the annotation set it explicitly with stubKeyBackend.
	keyBackend = func() string { return secretstore.BackendKeychain }
	os.Exit(m.Run())
}

// The at-rest protection is invisible unless the report says so: a machine whose
// key sits in a local file behaves identically to one holding it in the OS
// keyring, right up until a backup copies both.
func TestWithKeyBackend(t *testing.T) {
	base := pass("credentials", "loaded")

	stubKeyBackend(t, secretstore.BackendKeychain)
	if got := withKeyBackend(base); got.Status != statusPass || !strings.Contains(got.Message, "OS keyring") {
		t.Errorf("keychain: %+v", got)
	}

	stubKeyBackend(t, secretstore.BackendFile)
	got := withKeyBackend(base)
	switch {
	case !secretstore.KeyringAvailable():
		// Not a downgrade where there is nothing to downgrade FROM. A warning here
		// would fire on every run forever, about a state nobody can change.
		if got.Status != statusPass {
			t.Fatalf("a file key is normal where no keyring exists, got %+v", got)
		}
		if strings.Contains(got.Message, "downgraded") {
			t.Errorf("nothing was downgraded on this platform, got %q", got.Message)
		}
		// Whether the co-location caveat belongs depends on whether the file IS the
		// key, which is the distinction KeyFileWrappedByOS exists to draw.
		if secretstore.KeyFileWrappedByOS() {
			if strings.Contains(got.Message, "copies the key too") {
				t.Errorf("the OS wraps the key file here, so a copy carries nothing: %q", got.Message)
			}
			if !strings.Contains(got.Message, "user account") {
				t.Errorf("message must say what the wrap binds it to, got %q", got.Message)
			}
		} else if !strings.Contains(got.Message, "copies the key too") {
			t.Errorf("the file is the key here; the caveat must be stated: %q", got.Message)
		}
	default:
		if got.Status != statusWarn {
			t.Fatalf("a downgraded key must warn, got %+v", got)
		}
		if !strings.Contains(got.Message, "downgraded") {
			t.Errorf("message must name the state, got %q", got.Message)
		}
		if !strings.Contains(got.Hint, "copies the key too") {
			t.Errorf("hint must say why it matters, got %q", got.Hint)
		}
	}

	// Nothing sealed yet is not a finding — saving a profile creates the key.
	stubKeyBackend(t, secretstore.BackendNone)
	if got := withKeyBackend(base); got != base {
		t.Errorf("no key: expected the line untouched, got %+v", got)
	}
}

// The downgraded hint must not offer deleting the key file as the way back.
//
// Where the downgrade was automatic the keyring holds no key at all, so the file is
// the only copy: deleting it makes every stored profile permanently unreadable, and
// `profile show` masks the secret so they cannot be read back and re-saved.
//
// Asserted from both sides because a banned-substring check cannot tell "delete the
// key file" from "do NOT delete the key file". Rewording stays free; routing back to
// deletion does not.
func TestWithKeyBackend_DowngradedHintNeverAdvisesDeletingTheKey(t *testing.T) {
	requireKeyring(t)
	stubKeyBackend(t, secretstore.BackendFile)
	got := withKeyBackend(pass("credentials", "loaded"))

	if !strings.Contains(got.Hint, "Do NOT delete the key file") {
		t.Errorf("hint must warn against deleting the key file, got %q", got.Hint)
	}
	if strings.Contains(got.Hint, "to go back to the OS keyring") {
		t.Errorf("hint offers key deletion as the way back to the keyring: %q", got.Hint)
	}
}

// stateDirPath returns the state directory path for the current HOME without
// creating it, so a test can assert the "not created yet" branch.
func stateDirPath(t *testing.T) string {
	t.Helper()
	p, err := config.StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}
	return p
}

func TestCheckStateDir_AbsentIsPass(t *testing.T) {
	isolatedHome(t)
	path := stateDirPath(t)

	got := checkStateDir(path, nil, false)
	// A fresh install is not a problem to report. Warning here made a clean first
	// run show three warnings for one situation, which teaches the reader to skim.
	if got.Status != statusPass {
		t.Fatalf("absent state dir: status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, path) {
		t.Errorf("message must name the path, got %q", got.Message)
	}
}

func TestCheckStateDir_SecureModePasses(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	dir, err := config.StateDir() // creates it 0700
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}

	got := checkStateDir(dir, nil, true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "0700") {
		t.Errorf("message must report the mode, got %q", got.Message)
	}
}

// Readable-by-others is NOT reported. Everything secret under the state
// directory carries its own mode (profiles/ 0700, each profile 0600, the active
// pointer 0600, the skills record 0600), so a traversable directory exposes a
// list of filenames and nothing more. Warning about it fired on a real,
// correctly-protected installation — the cry-wolf a diagnostic cannot afford.
func TestCheckStateDir_OthersCanReadIsNotReported(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got := checkStateDir(dir, nil, true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "0755") {
		t.Errorf("the mode is still reported as fact, got %q", got.Message)
	}
}

// Writable-by-others IS a failure: the gate below is only as good as the
// directory holding it, and another local user can replace profiles/ outright.
func TestCheckStateDir_OthersCanWriteFails(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got := checkStateDir(dir, nil, true)
	if got.Status != statusFail {
		t.Fatalf("status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "writable by others") {
		t.Errorf("message must distinguish writable from merely readable, got %q", got.Message)
	}
}

// The counterpart of the six skips above, and the only one of this pair that can
// run on Windows: where mode bits are synthesised, the row must SAY it did not
// check rather than print a number it did not earn. Asserting the absence of a
// mode is the point — "(mode 0777)" on a healthy install is the failure this
// wording exists to prevent, and it is what the unported check produced.
func TestCheckStateDir_SynthesisedModeIsReportedAsUnchecked(t *testing.T) {
	if secfile.ModeBitsMeaningful {
		t.Skip("this platform's mode bits are real; the branch under test does not run here")
	}
	isolatedHome(t)
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}

	got := checkStateDir(dir, nil, true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass — nothing is known to be wrong (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "not checked") {
		t.Errorf("message must admit the check did not run, got %q", got.Message)
	}
	if strings.Contains(got.Message, "mode 0") {
		t.Errorf("message must not print a synthesised mode as if it were checked, got %q", got.Message)
	}
}

func TestCheckEndpoint_BuiltinHostPasses(t *testing.T) {
	// Guard the real process environment: a developer who exports this for their
	// own work would otherwise see this test fail for a reason that is not a bug.
	t.Setenv("MEXC_OPENAPI_BASE_URL", "")

	got := checkEndpoint("prod", "")
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.HasPrefix(got.Message, adapter.DefaultBaseURL) {
		t.Errorf("message = %q, want it to lead with the shipped endpoint %q", got.Message, adapter.DefaultBaseURL)
	}
	// The ordinary case names its source too. "It is the shipped host" is an
	// answer; a bare URL leaves the reader to recognise it.
	if !strings.Contains(got.Message, "built-in") {
		t.Errorf("message must name the source even when nothing overrode it, got %q", got.Message)
	}
}

// The environment override is the one that hides: it lives in a shell profile,
// nothing else reports it before a request is sent, and it sends the api key to
// whatever it names.
func TestCheckEndpoint_EnvOverrideWarns(t *testing.T) {
	// hosts.Resolve reads this through os.Getenv, not through the injected env
	// reader, so it must be set on the real process environment.
	t.Setenv("MEXC_OPENAPI_BASE_URL", "https://example.test")

	got := checkEndpoint("prod", "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "https://example.test") {
		t.Errorf("message must name where requests actually go, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "MEXC_OPENAPI_BASE_URL") {
		t.Errorf("message must name the override's source, got %q", got.Message)
	}
}

// --base-url pins the URL for one invocation and skips host resolution
// entirely (internal/transport.Send applies it before hosts.Resolve), so the
// report has to say where THIS invocation goes, not what the table holds.
func TestCheckEndpoint_BaseURLFlagWinsOverTheTable(t *testing.T) {
	t.Setenv("MEXC_OPENAPI_BASE_URL", "https://from-the-env.test")

	got := checkEndpoint("prod", "https://pinned.test")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "https://pinned.test") {
		t.Errorf("message must name the pinned URL, got %q", got.Message)
	}
	// The env override loses to the flag, exactly as the transport orders them —
	// but it is still set, so it must be named as SHADOWED rather than omitted.
	// Dropping the flag does not return you to the shipped endpoint; it hands you
	// this URL, and the reader is deciding what to change precisely now.
	if !strings.Contains(got.Message, "from-the-env.test") {
		t.Errorf("a shadowed override must still be reported, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "takes over if you drop the flag") {
		t.Errorf("the report must say what happens next, got %q", got.Message)
	}
	if !strings.Contains(got.Hint, "unset MEXC_OPENAPI_BASE_URL") {
		t.Errorf("the hint must cover both layers, got %q", got.Hint)
	}
	// The URL in effect is still the flag's: the message leads with it.
	if !strings.HasPrefix(got.Message, "https://pinned.test") {
		t.Errorf("the effective URL must lead the message, got %q", got.Message)
	}
}

// With no flag, a set variable is the winner rather than a shadow, and the
// report names the variable that produced it — built from hosts.OverrideVarName,
// not spelled out here, so the message cannot outlive the convention.
func TestCheckEndpoint_EnvOverrideNamesItsSource(t *testing.T) {
	t.Setenv("MEXC_OPENAPI_BASE_URL", "https://only-env.test")

	got := checkEndpoint("prod", "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, hosts.OverrideVarName(adapter.KeyOpenAPI)) {
		t.Errorf("message must name the variable, got %q", got.Message)
	}
	if strings.Contains(got.Message, "takes over if you drop") {
		t.Errorf("nothing is shadowed when no flag is given, got %q", got.Message)
	}
}

// saveProfile writes a usable profile through the real Save path.
func saveProfile(t *testing.T, name string) string {
	t.Helper()
	if err := profile.Save(profile.Profile{Name: name, APIKey: "K", SecretKey: "S"}); err != nil {
		t.Fatalf("Save %s: %v", name, err)
	}
	dir, err := config.StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	return filepath.Join(dir, "profiles", name)
}

func TestCheckCredentials_NothingSaved(t *testing.T) {
	isolatedHome(t)

	// No state directory at all: not walked, and nothing created by asking.
	got := checkCredentials(config.Config{}, "prod", false, "")
	if got.Status != statusWarn {
		t.Fatalf("no state dir: status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Hint, "profile set") {
		t.Errorf("hint must point at the command that fixes it, got %q", got.Hint)
	}
	// The state directory itself now exists — the encryption key lives there, put
	// in place by isolatedHome. What must NOT appear is the profiles subdirectory:
	// creating it is what walking the store would do, and this branch exists to
	// not walk.
	if _, err := os.Stat(filepath.Join(stateDirPath(t), "profiles")); !os.IsNotExist(err) {
		t.Errorf("stateExists=false must not walk (or create) the profile store")
	}

	// State directory present but empty.
	if got := checkCredentials(config.Config{}, "prod", true, ""); got.Status != statusWarn {
		t.Fatalf("empty store: status = %q, want warn (%q)", got.Status, got.Message)
	}
}

func TestCheckCredentials_HealthyActiveProfile(t *testing.T) {
	isolatedHome(t)
	saveProfile(t, "prod")
	if err := profile.SetActive("prod"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	// One row now answers both "which credentials" and "did they load".
	if !strings.Contains(got.Message, `"prod"`) {
		t.Errorf("message must name the profile in effect, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "loaded from") {
		t.Errorf("message must say the credentials came out, got %q", got.Message)
	}
}

func TestCheckCredentials_SavedButNoneActive(t *testing.T) {
	isolatedHome(t)
	saveProfile(t, "prod")

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Hint, "profile use") {
		t.Errorf("hint must point at `profile use`, got %q", got.Hint)
	}
}

// The active profile is judged by LOADING it. A refused-but-present entry and a
// DELETED one are different on disk and identical to the user: every command
// says "requires API credentials", the words a machine with no profiles gives.
func TestCheckCredentials_UnusableActiveFails(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	path := saveProfile(t, "prod")
	if err := profile.SetActive("prod"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusFail {
		t.Fatalf("insecure mode: status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "chmod 600") {
		t.Errorf("message must carry the loader's remedy, got %q", got.Message)
	}
}

func TestCheckCredentials_DanglingActivePointerFails(t *testing.T) {
	isolatedHome(t)
	saveProfile(t, "other")
	path := saveProfile(t, "prod")
	if err := profile.SetActive("prod"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusFail {
		t.Fatalf("status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, `"prod"`) {
		t.Errorf("message must name the dangling pointer, got %q", got.Message)
	}
	// The surviving profile must not be spun as health.
	if strings.Contains(got.Message, "loaded from") {
		t.Errorf("must not report success from another profile, got %q", got.Message)
	}
}

// The state only the merged row can express well: the profile loads, so the
// store looks healthy, but no credentials come out of it. Split across two rows
// this read as "1 saved, active: broken" followed by "no credentials", leaving
// the reader to connect them.
func TestCheckCredentials_ProfileLoadsButHoldsNoKey(t *testing.T) {
	isolatedHome(t)
	path := saveProfile(t, "broken")
	if err := profile.SetActive("broken"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	writeSealed(t, path, "api-key=K\napi-secret=\n")

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, `"broken"`) {
		t.Errorf("message must name the profile, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "api-key/api-secret") {
		t.Errorf("message must say which field is missing, got %q", got.Message)
	}
}

// A refused entry never breaks anything by itself, but it is invisible in
// `profile list`, so it rides along with whatever verdict the row already has.
func TestCheckCredentials_RefusedEntryIsNamedAlongsideAHealthyActive(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	saveProfile(t, "good")
	other := saveProfile(t, "leaky")
	if err := profile.SetActive("good"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := os.Chmod(other, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "leaky") {
		t.Errorf("message must name the hidden entry, got %q", got.Message)
	}
	if !strings.Contains(got.Message, `"good"`) {
		t.Errorf("message must still report the working active profile, got %q", got.Message)
	}
}

// --profile outranks the active pointer, and naming a credential set that will
// not resolve is a failure rather than "you have none": the caller pointed at
// something specific, and every other command treats that as a hard error.
func TestCheckCredentials_ExplicitProfileFailureIsAFailure(t *testing.T) {
	isolatedHome(t)
	saveProfile(t, "good")
	if err := profile.SetActive("good"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}

	testSelectedProfile = "nosuchprofile"
	t.Cleanup(func() { testSelectedProfile = "" })

	got := checkCredentials(config.Config{}, "prod", true, "nosuchprofile")
	if got.Status != statusFail {
		t.Fatalf("status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "nosuchprofile") {
		t.Errorf("message must name the selected profile, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "--profile") {
		t.Errorf("message must say the selection came from the flag, got %q", got.Message)
	}
}

// The same upgrade, reached the OTHER way. Above, the named profile does not
// exist, so profile.Load fails and the Load branch returns first — which left
// the resolve branch's explicit-selection upgrade untested, and deleting it kept
// the suite green. Here the named profile loads fine and yields no credentials,
// which is the only path through that branch.
func TestCheckCredentials_ExplicitProfileThatResolvesToNothingFails(t *testing.T) {
	isolatedHome(t)
	saveProfile(t, "good")
	if err := profile.SetActive("good"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	empty := saveProfile(t, "empty")
	writeSealed(t, empty, "api-key=K\napi-secret=\n")

	testSelectedProfile = "empty"
	t.Cleanup(func() { testSelectedProfile = "" })

	got := checkCredentials(config.Config{}, "prod", true, "empty")
	if got.Status != statusFail {
		t.Fatalf("status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, `"empty"`) {
		t.Errorf("message must name the selected profile, got %q", got.Message)
	}

	// Without the flag the same on-disk state is only a warning: nothing was
	// named, so "no usable credentials" is the whole story.
	testSelectedProfile = ""
	if err := profile.SetActive("empty"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got := checkCredentials(config.Config{}, "prod", true, ""); got.Status != statusWarn {
		t.Fatalf("unnamed: status = %q, want warn (%q)", got.Status, got.Message)
	}
}

// stampVersion pretends this is a released build for the duration of one test.
// buildinfo.Version is an ldflags-injected var and reads "dev" in a test binary,
// which is exactly the state Comparable rejects — so the in-sync and stale
// branches are unreachable without setting it.
func stampVersion(t *testing.T, v string) {
	t.Helper()
	prev := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = prev })
}

// Not installed is a pass — a user who never installed the docs is not out of
// sync with anything, and this mirrors CheckStale's cold-start silence. But it
// must still hand over the command, because for an agent-driven CLI the docs are
// how the caller learns the commands. Both halves are asserted: dropping the
// hint is the regression this test exists to catch, and status alone would not
// see it.
func TestCheckSkillsDocs_NothingInstalledPassesWithTheCommand(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "1.2.3")

	for _, tc := range []struct {
		name        string
		stateExists bool
	}{
		{"no state dir", false},
		{"state dir but no record", true},
	} {
		got := checkSkillsDocs(tc.stateExists)
		if got.Status != statusPass {
			t.Fatalf("%s: status = %q, want pass (%q)", tc.name, got.Status, got.Message)
		}
		if !strings.Contains(got.Hint, "skills install") {
			t.Errorf("%s: hint must give the command that installs them, got %q", tc.name, got.Hint)
		}
		if !strings.Contains(got.Message, "not installed") {
			t.Errorf("%s: message must say they are not installed, got %q", tc.name, got.Message)
		}
	}
}

func TestCheckSkillsDocs_InSyncPasses(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "1.2.3")
	if err := recordSkills(t, "1.2.3", map[string]string{"mexc-common": "", "mexc-spot-trade": ""}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "2 installed") {
		t.Errorf("message should report how many are installed, got %q", got.Message)
	}
}

func TestCheckSkillsDocs_DriftWarnsWithTheFix(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "1.3.0")
	if err := recordSkills(t, "1.2.3", map[string]string{"mexc-common": ""}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "1.2.3") || !strings.Contains(got.Message, "1.3.0") {
		t.Errorf("message must name both versions, got %q", got.Message)
	}
	if !strings.Contains(got.Hint, "skills install") {
		t.Errorf("hint must give the fix, got %q", got.Hint)
	}
}

// An unstamped build makes the comparison meaningless. doctor was invoked on
// purpose, so it says that out loud instead of silently passing as "in sync".
func TestCheckSkillsDocs_UnstampedBuildIsNotJudged(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "dev")
	if err := recordSkills(t, "1.2.3", map[string]string{"mexc-common": ""}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "cannot be judged") {
		t.Errorf("message must say the comparison was not made, got %q", got.Message)
	}
}

// messageOf must strip the envelope's "[type/subtype]" tag: that prefix is
// machine routing, and it reads as noise inside a sentence meant for a human.
func TestMessageOfAndHintOr(t *testing.T) {
	e := errs.Validation(errs.SubtypeNotFound, "profile %q does not exist", "prod").
		WithHint("create it first")
	if got := messageOf(e); got != `profile "prod" does not exist` {
		t.Errorf("messageOf = %q, want the bare message", got)
	}
	if strings.Contains(messageOf(e), "[") {
		t.Errorf("messageOf must not carry the envelope tag, got %q", messageOf(e))
	}
	if got := hintOr(e, "fallback"); got != "create it first" {
		t.Errorf("hintOr must prefer the error's own remedy, got %q", got)
	}
	bare := errors.New("plain")
	if got := hintOr(bare, "fallback"); got != "fallback" {
		t.Errorf("hintOr must fall back when the error carries no hint, got %q", got)
	}
	if got := messageOf(bare); got != "plain" {
		t.Errorf("messageOf on a plain error = %q", got)
	}
}

// selectionWord tells the reader WHICH knob chose the profile in effect, so the
// remedy points at the flag they just typed or at the stored pointer.
func TestSelectionWord(t *testing.T) {
	if got := selectionWord(true); got != "--profile" {
		t.Errorf("explicit selection = %q, want --profile", got)
	}
	if got := selectionWord(false); got != "the active profile" {
		t.Errorf("implicit selection = %q, want the active profile", got)
	}
}

// The text view is chosen by the DESTINATION: a terminal gets the table, a pipe
// or a buffer gets JSON — which is what keeps "stdout is data" intact for every
// programmatic reader. A buffer can never be a terminal, so this pins the whole
// decision table except the real-tty case, which no test can fabricate.
func TestTextView_ChoosesByDestination(t *testing.T) {
	var buf strings.Builder
	if textView(&buf, false) {
		t.Error("a buffer is not a terminal; must render JSON")
	}
	// An *os.File that is not a tty (a pipe, a redirect to a file) is the shape an
	// agent actually hands us, and it must behave like the buffer.
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if textView(f, false) {
		t.Error("a redirected file is not a terminal; must render JSON")
	}

	// --json has to be asserted against a writer that WOULD have rendered text.
	// Against a buffer the flag changes nothing — the destination alone already
	// forces JSON — so deleting the flag check left the suite green until this
	// stub was added.
	realIsTerminal := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	t.Cleanup(func() { isTerminal = realIsTerminal })

	if !textView(&buf, false) {
		t.Fatal("a terminal must render text")
	}
	if textView(&buf, true) {
		t.Error("--json must force JSON even at a terminal")
	}
}

// Every check a real run emits must have a heading, or the text view silently
// prints its snake_case machine name.
func TestTextView_EveryCheckHasALabel(t *testing.T) {
	isolatedHome(t)
	// The last two are the conditional rows: Windows only, and even there only
	// when the thing each is about is present — the .ps1 shim npm installs, and a
	// code-integrity policy that is switched on. They belong in this list anyway —
	// the invariant is that every row a run CAN emit has a label and that no label
	// outlives its row, and a row that appears on one platform still has to render
	// there. Listing them is also what keeps the count assertion below meaningful
	// rather than something to bump whenever it complains.
	names := []string{
		"state_dir", "endpoint", "credentials", "skills_docs", "read_only",
		"powershell_policy", "app_control",
	}
	for _, n := range names {
		if _, ok := checkLabels[n]; !ok {
			t.Errorf("check %q has no entry in checkLabels", n)
		}
	}
	if len(checkLabels) != len(names) {
		t.Errorf("checkLabels has %d entries for %d checks — a stale label is dead weight",
			len(checkLabels), len(names))
	}
}

func TestRenderText(t *testing.T) {
	r := doctorReport{
		OK:      false,
		Version: "v1.2.3 [public] (commit abc, built now)",
		Checks: []checkResult{
			pass("endpoint", "https://api.mexc.com"),
			warn("state_dir", "/home/x/.mexc is readable by others (mode 0755)", "chmod 700 /home/x/.mexc"),
			failed("credentials", `the active profile "prod" cannot be loaded`, "run: mexc profile use <name>"),
		},
	}
	var buf strings.Builder
	if err := renderText(&buf, r); err != nil {
		t.Fatalf("renderText: %v", err)
	}
	got := buf.String()

	for _, want := range []string{
		"v1.2.3 [public]",        // the heading carries the build
		"[✓] Endpoint",           // human label, not "endpoint"
		"[!] State dir",          // warn glyph
		"[✗] Credentials",        // fail glyph
		"→ chmod 700",            // the remedy, indented under its row
		"✗ 1 failure, 1 warning", // singular, and failures lead the summary
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	// Order is the order the checks ran, not failures-first: a report whose rows
	// move cannot be compared against the previous run at a glance.
	if strings.Index(got, "Endpoint") > strings.Index(got, "State dir") {
		t.Errorf("checks must render in the order given:\n%s", got)
	}
	// A passing check has no remedy, so it must not sprout an empty arrow line.
	if strings.Count(got, "→") != 2 {
		t.Errorf("expected exactly the two hints, got:\n%s", got)
	}
}

func TestSummaryLine(t *testing.T) {
	cases := []struct {
		warns, fails int
		want         string
	}{
		{0, 0, "✓ all checks passed"},
		{1, 0, "! 1 warning, no failures"},
		{2, 0, "! 2 warnings, no failures"},
		{0, 1, "✗ 1 failure, 0 warnings"},
		{3, 2, "✗ 2 failures, 3 warnings"},
	}
	for _, c := range cases {
		if got := summaryLine(c.warns, c.fails); got != c.want {
			t.Errorf("summaryLine(%d,%d) = %q, want %q", c.warns, c.fails, got, c.want)
		}
	}
}

func TestCheckReadOnly(t *testing.T) {
	if got := checkReadOnly(config.Config{}); got.Status != statusPass {
		t.Errorf("read-only off: status = %q, want pass", got.Status)
	}
	got := checkReadOnly(config.Config{ReadOnly: true})
	if got.Status != statusWarn {
		t.Fatalf("read-only on: status = %q, want warn", got.Status)
	}
	if !strings.Contains(got.Hint, "MEXC_READ_ONLY") {
		t.Errorf("hint must name the variable that can set it invisibly, got %q", got.Hint)
	}
}

// Without a key NOTHING on disk can be read. Reported as one failure naming the
// cause, not as a warning per profile naming the symptom — which is what it did
// first: eight copies of "cannot be decrypted" and `ok: true`, on a machine where
// no signed call could work.
func TestCheckCredentials_NoKeyForStoredProfilesFails(t *testing.T) {
	isolatedHome(t)
	stubKeyBackend(t, secretstore.BackendNone)

	// Sealed under a key this machine will not have once the store reports none.
	saveProfile(t, "prod")
	if err := profile.SetActive("prod"); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	dir, err := config.StateDir()
	if err != nil {
		t.Fatal(err)
	}
	// Undecryptable content: the shape a profile has when the key is gone.
	if err := os.WriteFile(filepath.Join(dir, "profiles", "prod"), []byte("not-ciphertext-at-all"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusFail {
		t.Fatalf("status = %q, want fail (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "no key on this machine can decrypt them") {
		t.Errorf("message must name the cause, not the symptom, got %q", got.Message)
	}
	if strings.Contains(got.Message, "no profiles saved") {
		t.Errorf("must not claim the store is empty when it is not, got %q", got.Message)
	}
	if !strings.Contains(got.Hint, "keychain-downgrade") {
		t.Errorf("hint must offer the way out, got %q", got.Hint)
	}
	// It must not gate that way out on "this machine has no OS keyring", which is
	// false when the keyring is merely out of reach: an agent evaluates the condition,
	// finds it false and skips the sentence. And it must not close on re-saving,
	// which seals a second key instead of recovering anything.
	if strings.Contains(got.Hint, "if this machine has no OS keyring") {
		t.Errorf("hint gates the remedy on a condition that is false when sandboxed: %q", got.Hint)
	}
	if !strings.Contains(got.Hint, "Do NOT save these profiles again first") {
		t.Errorf("hint must warn against re-saving before the cause is known, got %q", got.Hint)
	}
}

// With entries present but all refused for other reasons, the line must not say
// nothing is saved — it contradicts the refusals listed in the same sentence.
func TestCheckCredentials_AllRefusedIsNotReportedAsEmpty(t *testing.T) {
	requireRealModeBits(t)
	isolatedHome(t)
	stubKeyBackend(t, secretstore.BackendKeychain)

	path := saveProfile(t, "leaky")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got := checkCredentials(config.Config{}, "prod", true, "")
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if strings.Contains(got.Message, "no profiles saved") {
		t.Errorf("a store with entries must not report as empty, got %q", got.Message)
	}
	if !strings.Contains(got.Message, "none of them usable") {
		t.Errorf("message must say what is actually wrong, got %q", got.Message)
	}
}

// stubDigester registers a content digester for one test. Registration is
// process-wide, so the cleanup matters: leaking one would flip the version-
// fallback tests above onto the content basis.
func stubDigester(t *testing.T, d map[string]string) {
	t.Helper()
	skillstate.SetContentDigester(func() (map[string]string, error) { return d, nil })
	t.Cleanup(func() { skillstate.SetContentDigester(nil) })
}

// The point of content digests: a release that changed no SKILL.md text reports
// in sync, even though the build version moved.
func TestCheckSkillsDocs_ContentMatchesAcrossAVersionBump(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "1.9.0")
	digests := map[string]string{"mexc-common": "h1", "mexc-spot-trade": "h2"}
	stubDigester(t, digests)
	if err := recordSkills(t, "1.2.3", digests); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusPass {
		t.Fatalf("status = %q, want pass (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "content matches") {
		t.Errorf("message should say content matches, got %q", got.Message)
	}
	// The old basis would have warned here, naming two versions. It must not.
	if strings.Contains(got.Message, "1.9.0") {
		t.Errorf("a version-only bump must not be reported as drift, got %q", got.Message)
	}
}

// Content drift names the skills that changed, which is what makes the warning
// worth acting on rather than skipping.
func TestCheckSkillsDocs_ContentDriftNamesTheSkills(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "1.2.3")
	stubDigester(t, map[string]string{"mexc-common": "h1", "mexc-spot-trade": "EDITED"})
	if err := recordSkills(t, "1.2.3", map[string]string{"mexc-common": "h1", "mexc-spot-trade": "h2"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if !strings.Contains(got.Message, "mexc-spot-trade") {
		t.Errorf("message must name the changed skill, got %q", got.Message)
	}
	if strings.Contains(got.Message, "mexc-common") {
		t.Errorf("message must not name an unchanged skill, got %q", got.Message)
	}
	if !strings.Contains(got.Hint, "skills install") {
		t.Errorf("hint must give the fix, got %q", got.Hint)
	}
}

// An unstamped build has no meaningful version, but its content still hashes —
// so with digests on both sides doctor answers instead of declining.
func TestCheckSkillsDocs_UnstampedBuildIsJudgedOnContent(t *testing.T) {
	isolatedHome(t)
	stampVersion(t, "dev")
	stubDigester(t, map[string]string{"mexc-common": "EDITED"})
	if err := recordSkills(t, "dev", map[string]string{"mexc-common": "h1"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := checkSkillsDocs(true)
	if got.Status != statusWarn {
		t.Fatalf("status = %q, want warn (%q)", got.Status, got.Message)
	}
	if strings.Contains(got.Message, "cannot be judged") {
		t.Errorf("content is judgeable without a stamped version, got %q", got.Message)
	}
}

// recordSkills writes one default-shaped global install, standing in for what
// `skills install` records. The digest map doubles as the skill set, so a test
// that only cares about names passes "" digests and stays on the version basis.
func recordSkills(t *testing.T, version string, skills map[string]string) error {
	t.Helper()
	return skillstate.Record(version, skillstate.Install{
		Scope:  skillstate.ScopeGlobal,
		Agents: []string{"claude-code"},
		All:    true,
		Skills: skills,
	}, time.Unix(0, 0))
}
