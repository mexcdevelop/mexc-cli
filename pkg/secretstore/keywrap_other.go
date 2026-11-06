//go:build !windows

package secretstore

// The key file is stored as-is: 32 raw bytes, the format this package has always
// written.
//
// Two reasons, and the second is the binding one.
//
// It would buy nothing. Any wrap available here is one the same user can undo,
// and a process running as this user is explicitly outside what this package
// defends against (see WHAT IT IS NOT FOR in the package doc). On macOS the file
// only exists after a deliberate DowngradeToFile, whose whole premise is "keep the
// key here, protected by its mode"; on Linux there is no per-user OS wrap to reach
// for without taking on the keyring dependency the platform gate declined.
//
// And it would break what is already on disk. Key files exist on real macOS
// installs. A format change with no reader for the old one makes every profile
// sealed under them unreadable — the exact loss this package is built to avoid.
// Windows has no such files to protect: no Windows build could even compile until
// two commits ago, so none was ever written there, which is why it is free to
// start out wrapped.
// keyFileWrapped: the file IS the key. Anything that copies it copies the key,
// which is what the callers have to tell the user.
const keyFileWrapped = false

func wrapKey(_ Ref, key []byte) ([]byte, error) { return key, nil }

func unwrapKey(_ Ref, blob []byte) ([]byte, error) { return blob, nil }
