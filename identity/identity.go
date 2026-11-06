// Package identity declares the identities THIS product supports.
//
// They live here rather than in pkg/api because an identity belongs to whoever
// supplies its provider — the composition root — and not to the foundation that
// root is composed from. pkg/api sits below every layer that could declare one,
// so a catalogue of concrete identities there would make one root's vocabulary
// part of the foundation's own API. pkg/api names only IdentityNone, the absence
// of an identity, which is nobody's choice of credential.
//
// The package is deliberately at the repository root rather than under
// editions/: a sub-domain's api/ package declares `const Identity = …` and must
// import it, and api/ is forbidden from importing editions/ (depguard
// api-no-upward — api/ is pure data, an edition composes it, never the reverse).
//
// Adding an identity is two steps: declare it here, and inject a provider for it
// from the edition. Assembly refuses a sub-domain whose identity has no
// registered provider, so a constant on its own does nothing.
package identity

import "github.com/mexcdevelop/mexc-cli/pkg/api"

// OpenAPI is the api-key identity: the request carries the API key and is signed
// per the Signing axis. Its provider is injected by editions/public.
const OpenAPI api.Identity = "openapi"
