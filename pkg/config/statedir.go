package config

import (
	"os"
	"path/filepath"

	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// stateDirName is the directory under $HOME holding this product's on-disk
// state: the settings file, credential profiles, any identity's session store,
// and the exported skill docs.
//
// It is a variable because the directory belongs to the assembled binary, not to
// the framework. Two binaries composed from this module can be installed under
// the same command name, and then a single shared directory means each is writing
// the other's state — while none of what lives there is safely shared:
//
//   - the settings file holds `env`, whose legal values are exactly the envs the
//     registered hosts serve. A value written by a different host table is
//     unusable here and makes every command fail host resolution.
//   - profiles, session stores and skill state describe one binary's own
//     credentials and installation, never another's.
//
// The default is the plain name. A composition root that must not collide
// overrides it via Edition.StateDir.
var stateDirName = ".mexc"

const (
	// stateDirPerm keeps the directory owner-only: it holds credential ciphertext
	// and, where no OS keyring is used, the key beside it — so no other local user
	// may traverse in.
	stateDirPerm = 0o700
	// stateFilePerm keeps a state file owner-only, for the same reason.
	stateFilePerm = 0o600

	// WHERE THIS IS A NO-OP. Both are ignored on a platform whose mode bits Go
	// synthesises: os.Mkdir there calls CreateDirectory with no security attributes
	// and the directory inherits the parent's ACL, so what actually protects it is
	// the per-user directory the OS hands out, which this product neither sets nor
	// inspects. The readers know — they stop checking modes on such a platform (see
	// secfile.ModeBitsMeaningful) — and the constants are kept because they are
	// correct wherever they mean anything. Do not read them as a guarantee.
)

// SetStateDirName points this process's state at $HOME/<name>. The composition
// root calls it once, before anything reads state (framework.Run does this from
// Edition.StateDir). An empty name keeps the default, so a product that has no
// reason to move says nothing.
//
// It is a package-level setter for the same reason the signer / credential /
// host registries are: the state accessors below are free functions called from
// everywhere, and threading a directory through all of them would be far more
// invasive than a value fixed once at startup.
func SetStateDirName(name string) {
	if name != "" {
		stateDirName = name
	}
}

// StateDirName reports the directory name in use, for diagnostics that want to
// tell the user where state lives.
func StateDirName() string { return stateDirName }

// StatePath returns the absolute state directory WITHOUT creating it, for a
// caller that only wants to name where state lives — a diagnostic reporting a
// path should not have the side effect of making one. Use StateDir before
// actually reading or writing there.
func StatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errs.Internal("cannot locate home directory: %v", err).WithCause(err)
	}
	return filepath.Join(home, stateDirName), nil
}

// StateDir returns the absolute state directory, creating it 0700 if missing —
// on the platforms where that mode means anything; see stateDirPerm.
// 0700 matters: profiles hold plaintext credentials, so no other local user may
// traverse in.
func StateDir() (string, error) {
	dir, err := StatePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		return "", errs.Internal("cannot create %s: %v", dir, err).WithCause(err)
	}
	return dir, nil
}
