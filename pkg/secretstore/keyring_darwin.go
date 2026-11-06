//go:build darwin

package secretstore

// macOS has a system keyring this product can reach, and go-keyring speaks to it
// through the `security` binary — no daemon to be missing, no session bus to be
// absent. It is also the one platform where the wrapping key can live OUTSIDE the
// state directory, which is what makes the package doc's promise true here: copy
// ~/.mexc elsewhere and the ciphertext arrives useless.
//
// See keyringUsable in secretstore.go for why the other platforms answer false.
//
// A constant rather than an assignment into keyringUsable: the test suite
// overrides that var (see TestMain), so an assignment would leave the build-tag
// wiring itself unassertable — a wrong tag here would be invisible. A constant
// stays readable underneath the override.
const platformKeyringUsable = true
