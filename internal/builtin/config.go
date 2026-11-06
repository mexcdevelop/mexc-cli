// Package builtin holds shared built-in commands that are not engine machinery.
// framework.Run mounts config only when the product registers a setting.
package builtin

import (
	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/internal/cli"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// NewConfig builds the `config` command group: manage CLI-wide global settings —
// a CLOSED key set (today just `env`) persisted in this product's state dir. env
// is a global routing axis shared by every identity, kept deliberately separate
// from credentials and from whatever read-only diagnostics an edition adds.
// Adding a new global setting means extending the closed key set in pkg/config,
// not adding another top-level command.
//
// The reads report a setting's EFFECTIVE value — what it actually takes effect
// as this invocation — together with a `source` naming the layer it came from
// (flag / env / config / default / unset).
//
// They get both from cli.Factory.Setting rather than working them out here,
// because the precedence chain lives with the engine that applies it. This file
// twice tried to answer from what it could see locally and twice answered the
// wrong question: first the raw settings file (so `config get env` printed "" while
// every request went to the default env), then the file plus the default (so
// `MEXC_ENV=testnet config get env` printed "prod", source "default", while the
// request went to testnet — a confident false statement, which is worse than the
// blank it replaced).
//
//	<cli> config set env <one of the envs this product serves>
//	<cli> config get env
//	<cli> config unset env
//	<cli> config list
func NewConfig(k *cli.Kernel) *cobra.Command {
	cmd := k.Group("config", "Manage global CLI settings (env, …)")
	// Config management needs no credentials and must work even when the active
	// profile is broken (so you can fix state); override the root PersistentPreRunE
	// exactly as `profile` does — cobra runs only the nearest one in the chain.
	cmd.PersistentPreRunE = func(*cobra.Command, []string) error { return nil }
	cmd.AddCommand(
		newConfigGetCmd(k),
		newConfigSetCmd(k),
		newConfigUnsetCmd(k),
		newConfigListCmd(k),
	)
	return cmd
}

func newConfigGetCmd(k *cli.Kernel) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print a global setting's effective value and where it comes from",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			value, source, err := k.Setting(args[0])
			if err != nil {
				return writeErr(k, err)
			}
			return k.WriteJSON(map[string]any{"key": args[0], "value": value, "source": source})
		},
	}
}

func newConfigSetCmd(k *cli.Kernel) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a global setting (e.g. `env`; see `config list` for the keys)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			// set / unset change on-disk state, so they honor --read-only.
			if err := blockedByReadOnly(k, "config set"); err != nil {
				return writeErr(k, err)
			}
			if err := validateSetting(args[0], args[1]); err != nil {
				return writeErr(k, err)
			}
			if err := config.SetSetting(args[0], args[1]); err != nil {
				return writeErr(k, err)
			}
			// Report what the setting now resolves to rather than echoing the
			// argument: a higher-precedence layer (--env, MEXC_ENV) still wins over
			// what was just written, and saying otherwise would be the same lie the
			// reads used to tell.
			value, source, err := k.Setting(args[0])
			if err != nil {
				return writeErr(k, err)
			}
			return k.WriteJSON(map[string]any{"key": args[0], "value": value, "source": source})
		},
	}
}

func newConfigUnsetCmd(k *cli.Kernel) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Clear a global setting (fall back to its default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := blockedByReadOnly(k, "config unset"); err != nil {
				return writeErr(k, err)
			}
			if err := config.UnsetSetting(args[0]); err != nil {
				return writeErr(k, err)
			}
			// Report what the key now falls back to. Without it, `unset` followed by
			// a `get` that answers "prod" reads as an unset that did not take.
			value, source, err := k.Setting(args[0])
			if err != nil {
				return writeErr(k, err)
			}
			return k.WriteJSON(map[string]any{"unset": args[0], "value": value, "source": source})
		},
	}
}

func newConfigListCmd(k *cli.Kernel) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every setting key with its effective value and source",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			settings := map[string]any{}
			for _, key := range config.SettingKeys() {
				value, source, err := k.Setting(key)
				if err != nil {
					return writeErr(k, err)
				}
				settings[key] = map[string]string{"value": value, "source": source}
			}
			return k.WriteJSON(map[string]any{"settings": settings})
		},
	}
}

// writeErr records the error envelope + exit code and returns the error, the
// shared shape the config subcommands use on failure.
func writeErr(k *cli.Kernel, err error) error {
	k.WriteError(err)
	return err
}

// blockedByReadOnly returns a structured error when --read-only (or
// MEXC_READ_ONLY) is set, for the config subcommands that change on-disk state:
// changing a global setting is a state change, so read-only blocks it.
func blockedByReadOnly(k *cli.Kernel, op string) error {
	if !k.Config().ReadOnly {
		return nil
	}
	return errs.Validation(errs.SubtypeFailedPrecondition,
		"%q is blocked in --read-only mode", op).
		WithHint("drop --read-only (and MEXC_READ_ONLY) to change global settings")
}

// validateSetting rejects a value that would make the setting unusable, before
// it reaches disk.
//
// A persisted setting is sticky: an unchecked typo in `env` is written happily
// and then fails EVERY later command at host resolution, with the error pointing
// at the request rather than at the `config set` that caused it. Checking here
// puts the complaint where the mistake was made.
//
// Only `env` is checkable today, and only against the hosts THIS product
// registered — which is the right scope: an env means nothing except as
// something a host serves, and the two products serve different ones. A
// per-invocation --env / MEXC_ENV is deliberately not validated here; those are
// not sticky, and they already fail immediately with the same list of names.
func validateSetting(key, value string) error {
	// An empty value used to fall through to SetSetting, where writeSettings drops
	// empty entries — so `config set env ""` cleared the key while reporting that
	// it had stored one. Refuse it: clearing has its own verb, and a `set` that
	// silently means `unset` is a trap for an agent constructing the value.
	if value == "" {
		return errs.Validation(errs.SubtypeMissingArgument,
			"setting %q needs a value", key).
			WithHint("to clear it and fall back to the default, run `%s config unset %s`", cliname.Name(), key)
	}
	if key != "env" {
		return nil
	}
	if hosts.EnvDefined(hosts.Env(value)) {
		return nil
	}
	return errs.Validation(errs.SubtypeInvalidArgument,
		"no host serves env %q", value).
		WithHint("known env: %s — or `config unset env` to fall back to the default", hosts.KnownEnvs())
}
