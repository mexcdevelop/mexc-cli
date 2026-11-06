// Package config holds a product's on-disk state and the effective
// configuration of a single invocation.
//
// Two things live here, and only these two, because only these are shared by
// every product built on this module:
//
//   - Where state is kept (statedir.go). The directory is the PRODUCT's choice
//     — see Edition.StateDir — because nothing in it is shareable between
//     products installed under the same binary name.
//   - The per-invocation Config below: the safety mode and the routing env.
//
// Credentials are deliberately NOT here. How a product identifies itself — an
// api key from a saved profile, a login session, something else — is the
// business of the credential.Provider its edition injects, and that provider
// resolves them itself. Config once carried an api key and secret, which meant
// every product compiled two fields only one of them could ever fill, and the
// shared engine loading a profile it had no business knowing about.
//
// The endpoint is likewise absent: it is resolved at request time from Env
// through the host table (pkg/hosts), with --base-url as the escape hatch that
// pins a full URL and skips resolution entirely.
package config

// Config is the effective configuration for a single invocation, assembled from
// persisted settings, environment variables and flags (lowest to highest
// precedence).
type Config struct {
	// ReadOnly rejects every write operation: --read-only or MEXC_READ_ONLY=true.
	ReadOnly bool
	// Env is the selected deployment env, consumed by the host table. Its legal
	// values are exactly the envs this product's registered hosts serve, which is
	// why it is persisted per product (see statedir.go). Plain string rather than
	// hosts.Env to avoid a config -> hosts import cycle; the transport casts it.
	Env string
}
