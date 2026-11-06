// Package signing declares the request-signing schemes THIS product supports.
//
// They live here rather than in pkg/api for the same reason identities do: a
// scheme belongs to whoever supplies its signer, not to the foundation below it.
// pkg/api names only api.SignNone, the absence of signing, which is nobody's
// choice of algorithm.
//
// The package sits at the repository root rather than under editions/ because a
// sub-domain's api/ package declares `const Signed = …` and must import it, and
// api/ is forbidden from importing editions/ (depguard api-no-upward).
//
// Adding a scheme is two steps: declare it here, and register a signer.Signer
// for it from the edition. Assembly refuses a sub-domain whose scheme has no
// registered signer, so a constant on its own does nothing.
package signing

import "github.com/mexcdevelop/mexc-cli/pkg/api"

// CliHMAC is the CLI gateway scheme. Its signer injects CLI protocol headers
// and signs the canonical external request before the gateway rewrites its path.
const CliHMAC api.Signing = "cli-hmac"
