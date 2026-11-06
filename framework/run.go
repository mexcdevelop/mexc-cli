package framework

import (
	"embed"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mexcdevelop/mexc-cli/internal/builtin"
	"github.com/mexcdevelop/mexc-cli/internal/cli"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
	"github.com/mexcdevelop/mexc-cli/pkg/skillstate"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

// Run registers the edition's adapters, assembles the CLI, mounts the edition's
// builtins, and runs — returning the process exit code. It is the single entry
// point every product main calls.
func Run(ed Edition) int {
	return runWith(ed, commandName(filepath.Base(os.Args[0]), runtime.GOOS == "windows"),
		os.Getenv, os.Stdout, os.Stderr, os.Args[1:])
}

// commandName turns argv[0] into the name this CLI calls itself by. Windows
// binaries carry a .exe that the user never types — PATHEXT resolves it — so a
// hint built from argv[0] verbatim would read "run `mexc.exe doctor`", which is
// not what anyone typed and not what the docs say.
//
// windows is a parameter rather than a runtime.GOOS check inside, because the
// suffix is meaningful on the platform that does NOT use it: a Unix file really
// named foo.exe is invoked as foo.exe, and telling its user to run "foo" would be
// wrong in the one direction that leaves them with a command that does not exist.
// Passing the answer in also makes both halves testable from either platform.
//
// The comparison is case-insensitive because the Windows filesystem is: MEXC.EXE
// and mexc.exe are one file, and argv[0] reports whichever spelling was typed.
func commandName(argv0 string, windows bool) string {
	if !windows {
		return argv0
	}
	if ext := filepath.Ext(argv0); strings.EqualFold(ext, ".exe") {
		return strings.TrimSuffix(argv0, ext)
	}
	return argv0
}

// runWith is Run with every ambient input passed in, so the argv[0] handling can
// be driven by a test. Run supplies the real ones.
func runWith(ed Edition, argv0 string, env func(string) string, out, errW io.Writer, args []string) int {
	// Reflect argv[0] so a renamed/symlinked binary shows matching command paths
	// in --help, and so user-facing prose names the same command. An odd argv[0]
	// (empty, ".", "/") keeps the default name.
	name := argv0
	if name == "." || name == "/" {
		name = ""
	}
	// This runs BEFORE assemble, and the order is load-bearing: a command's
	// Short/Long and its flag usage strings are CONCATENATED while the tree is
	// built, so they capture whatever cliname held at that moment. Setting the name
	// afterwards reached only the hints that call cliname.Name() when an error is
	// raised, leaving every help text naming the wrong binary.
	if name != "" {
		cliname.Set(name)
	}
	app := assemble(ed, env, out, errW)
	// SetRootName must run after: it mutates the command assemble returned.
	if name != "" {
		app.SetRootName(name)
	}
	return app.Run(args)
}

// RunForTest assembles ed with injected env/IO and runs args, returning the exit
// code. It is the composition-root entry an edition's end-to-end tests use to
// drive the assembled tree and capture stdout/stderr: it performs the same
// adapter registration and builtin mounting as Run, but with injectable streams
// and WITHOUT the argv[0] rename, so the root keeps its default name in
// assertions. Production code calls Run.
func RunForTest(ed Edition, args []string, env func(string) string, out, errW io.Writer) int {
	return assemble(ed, env, out, errW).Run(args)
}

// assemble registers the edition's adapters and builds the command tree, the
// shared body of Run and RunForTest.
func assemble(ed Edition, env func(string) string, out, errW io.Writer) *cli.App {
	// Point on-disk state at this product's directory before anything reads it:
	// adapter registration below may touch a session store, and the command tree
	// resolves settings while it is built.
	config.SetStateDirName(ed.StateDir)
	// Teach skillstate how to hash this edition's embedded SKILL.md set, so drift is
	// decided on content rather than on a build version that moves every release.
	// Registered here, next to the state directory, because both are product facts
	// the composition root fixes once before any command reads them.
	skillsFS := bridgeSkillsFS(ed.SkillsFS)
	skillstate.SetContentDigester(func() (map[string]string, error) {
		return cli.SkillContentDigests(skillsFS)
	})
	// Before the host rows below: Resolve substitutes this for an unselected env.
	hosts.SetDefaultEnv(hosts.Env(ed.DefaultEnv))
	for _, s := range ed.Signers {
		signer.Register(s)
	}
	for _, p := range ed.Identities {
		credential.Register(p)
	}
	for _, h := range ed.HostRows {
		hosts.Register(h)
	}
	// An env axis exists only where there is more than one env to choose between.
	// A single-deployment product has no selection to make, so it offers neither
	// the flag nor the setting: a knob whose only legal value is the one already in
	// effect is a question with one answer.
	//
	// Derived from the host table rather than declared, so it cannot disagree with
	// the endpoints actually registered.
	//
	// It assumes the product gave every host a row an unqualified request can
	// land on: either Edition.DefaultEnv names one, or a host serves the blank
	// env. A product that does neither is broken with or without an axis — every
	// request fails at Resolve — and nothing here or in hosts.Validate reports it
	// up front. That gap predates this derivation and is not narrowed by it.
	envAxis := len(hosts.Envs()) > 1
	if envAxis {
		config.RegisterSetting(config.SettingEnv)
	}
	app := cli.NewApp(ed.Name, ed.Short, ed.Domains, skillsFS, ed.GlobalFlags, envAxis, env, out, errW)
	// Whether a person is reachable is a per-build fact, set before the confirm
	// reporter so its unattended-pairing assertion can see it. A setter rather than
	// a NewApp parameter, matching the other post-assembly injections, so NewApp's
	// signature does not widen for it. Zero value (false) is every locally invoked
	// build, so this changes nothing for them.
	app.SetUnattended(ed.Unattended)
	// The audit sink, injected after assembly because it is built from Deps, which
	// need the app NewApp just returned. OnceValue defers construction to the first
	// report; presence is validated immediately, inside SetConfirmReporter.
	if ed.ConfirmReporter != nil {
		app.SetConfirmReporter(sync.OnceValue(func() confirmaudit.Reporter {
			return ed.ConfirmReporter(app.Deps())
		}))
	} else {
		app.SetConfirmReporter(nil)
	}
	// Injected with the ports above: it reaches the same per-request Client.
	app.SetBuildVersionHeader(ed.BuildVersionHeader)
	app.SetRateLimit(ed.RateLimit)
	// Same terms as the sink above.
	if ed.WSCollector != nil {
		app.SetWSCollector(sync.OnceValue(func() transport.WSCollector {
			return ed.WSCollector(app.Deps())
		}))
	} else {
		app.SetWSCollector(nil)
	}
	// Shared built-ins live in internal/builtin, outside the
	// engine, so framework mounts them here against the engine's Kernel — the same
	// place it mounts the edition-specific builtins below.
	k := app.Kernel()
	// config is mounted on whether anything is settable — it asks the settings
	// registry, NOT the env axis. The two happen to coincide while env is the only
	// key; keying off the axis would mean a second key silently failed to bring the
	// command back for a single-env product.
	if len(config.SettingKeys()) > 0 {
		app.AddCommand(builtin.NewConfig(k))
	}
	d := app.Deps()
	for _, b := range ed.Builtins {
		app.AddCommand(b(d))
	}
	return app
}

// bridgeSkillsFS widens the edition's []embed.FS to the []fs.FS the engine's
// skills command consumes. embed.FS satisfies fs.FS; the conversion is only a
// slice-element retype so the engine need not know the concrete embed type.
func bridgeSkillsFS(es []embed.FS) []fs.FS {
	out := make([]fs.FS, 0, len(es))
	for _, e := range es {
		out = append(out, e)
	}
	return out
}
