// Package public is the composition root for the PUBLIC edition of mexc: the
// open-core product that ships the OpenAPI capabilities (spot market /
// account / trade) and nothing internal. It declares its injection set via
// Edition() and is wired into the binary by cmd/mexc/main.go through
// framework.Run. It is a disjoint edition: every domain, provider, host and skill
// it mounts is public, and it neither imports nor includes any internal
// counterpart — the public build is not a filtered view of a larger one.
package public

import (
	"embed"

	"github.com/spf13/pflag"

	assets "github.com/mexcdevelop/mexc-cli"
	"github.com/mexcdevelop/mexc-cli/editions/public/adapter"
	"github.com/mexcdevelop/mexc-cli/editions/public/command"
	"github.com/mexcdevelop/mexc-cli/framework"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// selectedProfile holds --profile for this invocation: the credential set to use,
// empty for the active one.
//
// The flag belongs to this edition, not to the shared engine, because profiles
// are this product's way of holding credentials — a product that identifies
// itself some other way has no use for the flag and no `profile` command to send
// the user to. Package-level so the identity provider, built once in Edition()
// below, can read whatever the flag parser later writes.
var selectedProfile string

// editionName is this build's variant label. It is declared once because two
// places need the same string: Edition.Name (what --version prints) and the
// doctor builtin (which reports the same build line). Two literals could drift,
// and the symptom would be a diagnostic naming a build that does not exist.
const editionName = "public"

// Edition is the public product's injection set: the spot domains, the openapi
// api-key identity, the cli-hmac signer, the openapi host, the profile and
// doctor builtins, and this product's + the shared (mexc-common) skill docs. The
// two skill FSes are separate because mexc-common documents the shared layer
// rather than any product's business surface; see assets.CommonSkillsFS.
func Edition() framework.Edition {
	return framework.Edition{
		Name:       editionName,
		StateDir:   ".mexc",
		DefaultEnv: "prod",
		// Every request and WS handshake reports which build sent it.
		BuildVersionHeader: adapter.BuildVersionHeader,
		// Coarse client-side throttle; the backend stays the real limit.
		RateLimit: adapter.RateLimitPolicy,
		Domains:   domains(),
		Signers:   []signer.Signer{adapter.CliHMAC{}},
		// The identity reads the flag through a closure rather than a value copied
		// at construction: Edition() runs before the command line is parsed.
		Identities: []credential.Provider{
			adapter.OpenAPI{SelectProfile: func() string { return selectedProfile }},
		},
		HostRows: []hosts.Registration{adapter.OpenAPIRows(), adapter.SpotWSRows(), adapter.FuturesWSRows()},
		Builtins: []framework.BuiltinCommand{
			command.Profile,
			command.Doctor(editionName),
		},
		GlobalFlags: func(fs *pflag.FlagSet) {
			fs.StringVar(&selectedProfile, "profile", "",
				"Use a saved profile's credentials (see `"+cliname.Name()+" profile`); overrides the active profile")
		},
		SkillsFS: []embed.FS{assets.PublicSkillsFS, assets.CommonSkillsFS},
		// The write-confirmation audit sink. Deps supplies the Sender because the
		// report goes out over this invocation's transport, with its flags.
		ConfirmReporter: func(d framework.Deps) confirmaudit.Reporter {
			return adapter.NewConfirmReport(d.Client())
		},
	}
}
