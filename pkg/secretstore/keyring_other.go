//go:build !darwin

package secretstore

// Everywhere but macOS the wrapping key lives in a local file and the OS keyring
// is never asked. The reasoning is on keyringUsable in secretstore.go; the short
// version is that on Linux the keyring is a D-Bus service the target environments
// do not run, and asking anyway costs an autolaunched daemon, a timeout and
// macOS's "denied looks empty" ambiguity in exchange for nothing.
//
// Written out rather than left to a zero value: a reader who opens this file to
// find out what their platform does should get an answer here, not have to infer
// one from a default declared elsewhere.
//
// A constant rather than an assignment into keyringUsable — see the note in
// keyring_darwin.go.
const platformKeyringUsable = false
