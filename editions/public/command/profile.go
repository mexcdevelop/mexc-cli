package command

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/framework"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/console"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/secretstore"
)

// Profile builds the `profile` command group: manage named credential sets on
// disk so an operator can switch accounts without re-exporting env vars. It is a
// builtin of this edition (openapi credentials), injected via Edition.Builtins
// rather than mounted by the shared layer.
//
// SECURITY: a profile stores the api key/secret in plaintext under ~/.mexc
// (0600). `profile show` masks the secret so it never reaches stdout / the agent
// context. There is no credential environment variable — credentials come solely
// from a profile (see resolveConfig); MEXC_ENV / MEXC_READ_ONLY are configuration,
// not credentials.
//
// `set` takes no credential flags and reads only from a terminal prompt, so a
// secret cannot be passed on a command line and therefore cannot be recorded by
// one (see tty.go for exactly what that does and does not achieve). Every other
// subcommand stays non-interactive: the point of the split is that the human
// saves the credential once and the agent uses it freely thereafter.
//
//	mexc profile set prod        # prompts on the terminal for key and secret
//	mexc profile use prod
//	mexc profile list
//	mexc profile show prod
//	mexc profile remove prod
func Profile(d framework.Deps) *cobra.Command {
	cmd := framework.Group("profile", "Manage saved credential profiles", d)
	// Profile management needs no credentials and must work even when the active
	// profile is broken (so you can fix it). Override the root PersistentPreRunE
	// (which resolves config and would otherwise fail on a bad active profile);
	// cobra runs only the nearest PersistentPreRunE in the chain.
	cmd.PersistentPreRunE = func(*cobra.Command, []string) error { return nil }
	cmd.AddCommand(
		newProfileSetCmd(d),
		newProfileListCmd(d),
		newProfileUseCmd(d),
		newProfileShowCmd(d),
		newProfileRemoveCmd(d),
		newProfileKeychainDowngradeCmd(d),
	)
	return cmd
}

func newProfileSetCmd(d framework.Deps) *cobra.Command {
	// NO --api-key / --api-secret. They existed as a convenience for someone
	// already at a keyboard, and were kept on the reasoning that the terminal
	// requirement gated them anyway. That reasoning was wrong twice over: a
	// process can manufacture a terminal (see tty.go), and even where it cannot,
	// a flag is the one input channel that writes the secret into a command line
	// — which is the artefact that gets recorded. Removing them makes the prompt
	// the only way in, so no invocation of this command can carry a secret.
	c := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or overwrite a profile (prompts on the terminal; no flags carry the secret)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// set / use / remove change on-disk state, so they honor --read-only
			// just like a write operation does (read-only blocks all state changes).
			if err := blockedByReadOnly(d, "profile set"); err != nil {
				return writeErr(d, err)
			}
			key, secret, err := readCredentials()
			if err != nil {
				return writeErr(d, err)
			}
			p := profile.Profile{Name: args[0], APIKey: key, SecretKey: secret}
			if err := profile.Save(p); err != nil {
				return writeErr(d, err)
			}
			// Say so when the credential landed under the weaker protection. Seal
			// falls back to a local key rather than refusing (see pkg/secretstore),
			// which keeps the product usable where no keyring exists — but silence
			// there would leave the user believing in a protection they do not have.
			//
			// It must not claim the machine HAS no keyring, which is what this said
			// before: the usual reason for landing here is a keyring that is present
			// and out of reach, and a reader told they have none stops looking.
			// And it must not be a WARNING where a file key is normal. On a platform
			// with no OS keyring this fires on every `profile set`, forever, about a
			// state nobody can change — which trains the reader to ignore the line and
			// tells an agent to look for a fix that does not exist. The co-location
			// caveat is still worth stating there, as a note.
			if profile.KeyBackend() == secretstore.BackendFile {
				switch {
				case secretstore.KeyringAvailable():
					_, _ = fmt.Fprintf(d.Err(),
						"warning: the encryption key is in a local file beside the profile, not in the OS "+
							"keyring — either none is usable here, or it is out of reach. A backup or synced "+
							"folder that copies one copies both, and that file may be the only copy of the "+
							"key: do not delete it\n")
				case secretstore.KeyFileWrappedByOS():
					// Same note, minus the half that stopped being true. The OS wrap is what
					// makes a copied folder harmless here; the do-not-delete half survives
					// untouched, because the file is still the only way back to the data.
					_, _ = fmt.Fprintf(d.Err(),
						"note: the encryption key is in a local file beside the profile, which is the only "+
							"key store this platform has. The OS wraps it to this user account, so a copy "+
							"of it is useless elsewhere — but it is still the only key this machine has: "+
							"do not delete it\n")
				default:
					_, _ = fmt.Fprintf(d.Err(),
						"note: the encryption key is in a local file beside the profile, which is the only "+
							"key store this platform has. A backup or synced folder that copies one copies "+
							"both, and that file is the only copy of the key: do not delete it\n")
				}
			}
			return d.WriteJSON(map[string]any{"saved": p.Name})
		},
	}
	// Note: env is not a profile field — set the deployment environment globally
	// globally, not per profile. This product serves one env and offers no such
	// setting; a multi-env product persists it with `config set env`.
	return c
}

// credentialPrompter reads whatever the caller did not supply, from the
// terminal. ttyOpener acquires that terminal. Both are package vars so a test
// can drive the prompted paths, which otherwise need a pty; the defaults are
// the only implementations that ship. The refusal branch is exercised against
// the REAL opener (by pointing the tty paths at a non-terminal), because that branch
// is the control and a stub proving a stub is worth nothing.
var (
	credentialPrompter = promptCredentials
	ttyOpener          = openTTY
)

// readCredentials obtains the pair to save, from the terminal and from nowhere
// else. It takes no arguments on purpose: there is no caller-supplied value to
// prefer, so there is no ordering to get wrong and no channel other than the
// prompt for a secret to arrive on.
func readCredentials() (string, string, error) {
	tty, err := ttyOpener("saving a profile", "profile set <name>")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tty.Close() }()

	key, secret, err := credentialPrompter(tty)
	if err != nil {
		return "", "", err
	}
	// Validated here rather than inside the prompter so it covers whatever came
	// back, whichever half was typed. A blank half would save a profile that looks
	// configured and resolves to nothing — the exact state `doctor` had to grow a
	// check for.
	if key == "" || secret == "" {
		return "", "", errs.Validation(errs.SubtypeInvalidArgument,
			"both an Access Key and a Secret Key are required").
			WithHint("run the command again and enter both values")
	}
	return key, secret, nil
}

// promptCredentials asks for both values. The key is shown as it is typed and
// the secret is not: the key is an identifier that travels in a request header,
// and seeing it appear is what catches a truncated paste, while the secret has
// no reason to reach the screen. The secret's label says so, because a prompt
// that looks unresponsive is one people retype into.
func promptCredentials(tty console.Pair) (string, string, error) {
	apiKey, err := promptLine(tty, "Access Key: ")
	if err != nil {
		return "", "", err
	}
	apiSecret, err := promptSecret(tty, "Secret Key (hidden): ")
	if err != nil {
		return "", "", err
	}
	return apiKey, apiSecret, nil
}

// newProfileKeychainDowngradeCmd builds `profile keychain-downgrade`: pin the
// wrapping key into a local file so later reads take that path.
//
// It is NOT how you make the product work without a keyring — Seal already falls
// back on its own. This is for someone who wants the key kept locally on purpose:
// another local process must read the same store, or the keyring prompts are in
// the way. Whatever is already sealed stays readable, because the key is copied
// rather than replaced and Open tries both locations.
//
// It requires a terminal and an explicit --yes. It lowers the protection of
// stored credentials, so an agent must not be able to do it on your behalf — and
// the terminal is also the only place it can work, since it copies the key the OS
// keyring holds and the keyring answers in an interactive session, not inside the
// sandbox that made the downgrade necessary.
func newProfileKeychainDowngradeCmd(d framework.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "keychain-downgrade",
		Short: "Pin the encryption key to a local file instead of the OS keyring (weaker; terminal only)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			// Registered on every platform so `--help` reads the same everywhere, but
			// it only means something where an OS keyring exists: all it does is copy a
			// key OUT of one. Refusing up front beats letting it reach
			// secretstore.DowngradeToFile, which would report "the keyring holds no key
			// to copy" — true, but it reads as a recoverable problem and sends the
			// reader looking for a keyring to unlock.
			if !secretstore.KeyringAvailable() {
				return writeErr(d, errs.Validation(errs.SubtypeFailedPrecondition,
					"keychain-downgrade has nothing to do on this platform: it copies the encryption "+
						"key out of the OS keyring, and this platform does not use one").
					WithHint("the key is already in a local file beside the data — that is the only key "+
						"store here, not a downgraded one. Run `%s doctor` to see where it lives",
						cliname.Name()))
			}
			if err := blockedByReadOnly(d, "profile keychain-downgrade"); err != nil {
				return writeErr(d, err)
			}
			tty, err := ttyOpener("downgrading credential storage", "profile keychain-downgrade --yes")
			if err != nil {
				return writeErr(d, err)
			}
			_ = tty.Close()
			if !d.AssumeYes() {
				return writeErr(d, errs.ConfirmationRequired(
					"pinning the encryption key to a local file weakens protection of stored credentials").
					WithHint("re-run with --yes to confirm; the key will sit beside the data it protects, "+
						"so a backup or synced folder that copies one copies both"))
			}

			res, err := profile.DowngradeKeyToFile()
			if err != nil {
				return writeErr(d, err)
			}
			return d.WriteJSON(map[string]any{
				"backend": profile.KeyBackend(),
				"result":  downgradeWord(res),
				"warning": "the encryption key now sits beside the data it protects; " +
					"a backup or a synced folder that copies one copies both",
			})
		},
	}
}

// downgradeWord names the outcome. Both outcomes preserve access: the key is
// copied, never replaced, and a keyring holding nothing to copy is a refusal
// rather than a result (see secretstore.DowngradeToFile).
func downgradeWord(r secretstore.DowngradeResult) string {
	if r == secretstore.DowngradeCopiedFromKeyring {
		return "copied the existing key out of the OS keyring; saved profiles stay readable"
	}
	return "already downgraded; a local key file was already in place"
}

func newProfileListCmd(d framework.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved profiles, marking the active one",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := profile.List()
			if err != nil {
				return writeErr(d, err)
			}
			active, _ := profile.Active()
			return d.WriteJSON(map[string]any{"profiles": names, "active": active})
		},
	}
}

func newProfileUseCmd(d framework.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := blockedByReadOnly(d, "profile use"); err != nil {
				return writeErr(d, err)
			}
			// Confirm the profile exists (and is readable) before pointing at it,
			// so `use` cannot leave a dangling active pointer.
			if _, err := profile.Load(args[0]); err != nil {
				var e *errs.Error
				if errors.As(err, &e) {
					// WithHint mutates e in place (e aliases err); the returned
					// pointer is intentionally discarded.
					_ = e.WithHint("create it first with `%s profile set %s ...`", cliname.Name(), args[0])
				}
				return writeErr(d, err)
			}
			if err := profile.SetActive(args[0]); err != nil {
				return writeErr(d, err)
			}
			return d.WriteJSON(map[string]any{"active": args[0]})
		},
	}
}

func newProfileShowCmd(d framework.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a profile with its secret masked",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := profile.Load(args[0])
			if err != nil {
				return writeErr(d, err)
			}
			// The field names match what the MEXC website issues and what
			// `profile set` prompts for, so a reader comparing this output against
			// the website is comparing like with like.
			return d.WriteJSON(map[string]any{
				"name":       p.Name,
				"access_key": mask(p.APIKey),
				"secret_key": mask(p.SecretKey),
			})
		},
	}
}

func newProfileRemoveCmd(d framework.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a profile (clears the active pointer if it was active)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := blockedByReadOnly(d, "profile remove"); err != nil {
				return writeErr(d, err)
			}
			if err := profile.Remove(args[0]); err != nil {
				return writeErr(d, err)
			}
			return d.WriteJSON(map[string]any{"removed": args[0]})
		},
	}
}

// mask renders a secret for display without over-exposing it. An empty value is
// shown as "(empty)" so it is distinguishable from a populated one. Short values
// (< 12 chars, where revealing the tail would leak most of the secret) are fully
// hidden; only a long value shows its last 4 characters, enough to confirm which
// credential is stored without printing it in full.
func mask(s string) string {
	switch {
	case s == "":
		return "(empty)"
	case len(s) < 12:
		return "****"
	default:
		return "****" + s[len(s)-4:]
	}
}

// blockedByReadOnly returns a structured error when --read-only (or
// MEXC_READ_ONLY) is set, for the profile subcommands that change on-disk state.
// Read-only blocks all state changes, and writing/removing a credential profile
// is a state change, even though it is local rather than an exchange call.
func blockedByReadOnly(d framework.Deps, op string) error {
	if !d.Config().ReadOnly {
		return nil
	}
	return errs.Validation(errs.SubtypeFailedPrecondition,
		"%q is blocked in --read-only mode", op).
		WithHint("drop --read-only (and MEXC_READ_ONLY) to manage profiles")
}

// writeErr records the error envelope + exit code and returns the error, the
// shared shape every profile subcommand uses on failure.
func writeErr(d framework.Deps, err error) error {
	d.WriteError(err)
	return err
}
