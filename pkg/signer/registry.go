// Package signer is the signing PORT: the interface a scheme satisfies, the
// optional capabilities it may also satisfy, and the registry an edition
// injects its schemes into.
//
// It carries no algorithm. Which scheme a product signs with, and how that
// scheme computes its digest, belong to the product — its schemes are named in
// signing/ and implemented in its own adapter package, the same way its
// identities are. This package only needs to ask a scheme what it is and what
// it can do, without knowing that any particular scheme exists.
package signer

import (
	"time"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

// Signer is one complete signing scheme. It mutates the RequestSpec in place to
// add whatever the scheme requires (timestamps, a signature, ...). It consumes
// credential.Credentials, so signing is decoupled from identity: any identity
// that can produce credentials can feed any scheme.
//
// Which parts of a request a scheme covers is the scheme's own business, decided
// inside Sign. A scheme that signs the query only leaves any body out of its
// canonical form; one that cannot sign a shape at all returns an error rather
// than signing incorrectly. This package deliberately offers no way to ask a
// scheme in advance: the assembly layer would then have to hold a product's
// signing limitations, and a capability nothing consults is worse than none —
// a scheme could declare it and believe itself guarded.
type Signer interface {
	Scheme() api.Signing
	Sign(spec *api.RequestSpec, cred credential.Credentials, now time.Time) error
}

// ParamReserver is an optional Signer capability: a scheme that injects its own
// query parameters (a timestamp, a signature, ...) names them here so the
// assembly layer can reject a business Param that would collide with — and be
// silently overwritten by — a framework-injected one.
type ParamReserver interface {
	ReservedParams() []string
}

// HeaderReserver is an optional Signer capability: a scheme that injects
// protocol headers names them here so declaration validation and redirect
// handling can protect them just as they protect identity headers.
type HeaderReserver interface {
	ReservedHeaders() []string
}

// DryRunPreparer is an optional Signer capability that describes a signed
// request without loading credentials or computing a replayable signature.
type DryRunPreparer interface {
	PrepareDryRun(spec *api.RequestSpec)
}

var registry = map[api.Signing]Signer{}

// Register adds a signer; called from each scheme's init().
func Register(s Signer) { registry[s.Scheme()] = s }

// Get returns the signer for a scheme.
func Get(scheme api.Signing) (Signer, bool) { s, ok := registry[scheme]; return s, ok }

// ReservedParams returns the query parameter names that scheme injects (and
// would therefore silently overwrite a colliding business Param), or nil.
func ReservedParams(scheme api.Signing) []string {
	if s, ok := Get(scheme); ok {
		if pr, ok := s.(ParamReserver); ok {
			return pr.ReservedParams()
		}
	}
	return nil
}

// ReservedHeaders returns the protocol header names that scheme injects (and
// would therefore silently overwrite a colliding business Param), or nil.
func ReservedHeaders(scheme api.Signing) []string {
	if s, ok := Get(scheme); ok {
		if hr, ok := s.(HeaderReserver); ok {
			return hr.ReservedHeaders()
		}
	}
	return nil
}
