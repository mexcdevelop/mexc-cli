package public

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/framework"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/secfile"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// TestMain registers the public edition's injected adapters once, mirroring what
// framework.Run does at process start, so tests that resolve the openapi identity
// / signer / host via the registries find them registered.
func TestMain(m *testing.M) {
	// Run against go-keyring's in-memory mock. Overriding HOME (below) is exactly
	// what makes the login keychain unreachable on macOS, so a real keyring is both
	// unavailable and slow to say so — and provisioning a key would otherwise
	// attempt a WRITE against the developer's own login keychain.
	keyring.MockInit()
	// Isolate from the developer's real ~/.mexc (saved profiles / global env), so
	// the reachability + dry-run tests are deterministic.
	dir, err := os.MkdirTemp("", "mexc-cli-publictest-home")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	// USERPROFILE is the variable os.UserHomeDir reads on Windows, and HOME is
	// ignored there. Without this the whole package's tests would run against the
	// developer's REAL state directory — writing profiles into it, changing its
	// modes, and removing key files.
	if err := os.Setenv("USERPROFILE", dir); err != nil {
		panic(err)
	}
	ed := Edition()
	for _, s := range ed.Signers {
		signer.Register(s)
	}
	for _, p := range ed.Identities {
		credential.Register(p)
	}
	for _, h := range ed.HostRows {
		hosts.Register(h)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// run assembles the public edition (via framework's test entry) with injected
// env/IO and runs args, returning captured stdout, stderr, and the exit code —
// the end-to-end harness for this edition's command/reachability tests.
func run(env map[string]string, args ...string) (stdout, stderr string, code int) {
	var out, errBuf bytes.Buffer
	ed := Edition()
	// A stub, not the real runner: no test here should be able to open a socket.
	ed.WSCollector = func(framework.Deps) transport.WSCollector { return stubWSCollector{} }
	code = framework.RunForTest(ed, args, func(k string) string { return env[k] }, &out, &errBuf)
	return out.String(), errBuf.String(), code
}

// stubWSCollector satisfies the WS port without dialling anything.
type stubWSCollector struct{}

func (stubWSCollector) Collect(context.Context, transport.WSConfig) error { return nil }

// isolatedHome points HOME at a fresh directory AND provisions a file-backed
// encryption key inside it, returning that directory.
//
// Both halves are needed: profiles are sealed at rest (pkg/secretstore), so without
// a key nothing can be saved or loaded. Two steps, because the downgrade only ever
// COPIES a key: Seal mints one in the mocked keyring, then the downgrade pins it to
// a local file.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, _, err := secretstore.Seal(profile.KeyRef(), []byte("provision")); err != nil {
		t.Fatalf("provision test encryption key: %v", err)
	}
	if _, err := profile.DowngradeKeyToFile(); err != nil {
		t.Fatalf("pin the test encryption key to a file: %v", err)
	}
	return home
}

// requireRealModeBits skips a test whose subject is a file-permission check.
// Where the OS synthesises mode bits, this product deliberately does not check
// them, so asserting that a wide mode gets refused would assert the opposite of
// the intended behaviour — see secfile.ModeBitsMeaningful. Skipping states that;
// leaving the test to fail would read as a port that is broken rather than one
// that made a decision.
func requireRealModeBits(t *testing.T) {
	t.Helper()
	if !secfile.ModeBitsMeaningful {
		t.Skip("this platform synthesises file mode bits; the check under test does not run here")
	}
}
