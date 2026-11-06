// Package errs defines the typed errors and machine-readable envelope every
// product built on this module emits.
//
// Every error is parsed by an AI agent to
// decide its next action, so errors must be structured, actionable, and
// specific. The envelope has four fixed fields (plus message):
//
//	type    the broad category: validation / network / internal / api
//	subtype the subcategory, for precise agent routing
//	param   points only at the user input that actually failed (e.g. --symbol); omitted if none
//	hint    recovery guidance for the agent (plain language telling it how to fix the next step)
//
// The error envelope goes to stderr: stdout is data, stderr is everything else.
package errs

import (
	"errors"
	"fmt"
	"time"
)

// Type is the broad error category. Each category maps to a stable process exit
// code (see ExitCode).
type Type string

const (
	TypeValidation Type = "validation" // user input / request structure error
	TypeNetwork    Type = "network"    // transport-layer failure (connection, timeout, DNS)
	TypeInternal   Type = "internal"   // internal error in this program (should not happen)
	TypeAPI        Type = "api"        // business error returned by the MEXC API
)

// Common subtype constants. subtype is an open set; to add one, simply add a
// new constant.
const (
	SubtypeInvalidArgument        = "invalid_argument"         // argument format / value is illegal
	SubtypeMissingArgument        = "missing_argument"         // a required argument is missing
	SubtypeFailedPrecondition     = "failed_precondition"      // a precondition is not met (e.g. missing credentials)
	SubtypeNotFound               = "not_found"                // a named resource (e.g. a profile) does not exist
	SubtypeInsecurePermission     = "insecure_permission"      // an on-disk credential has unsafe file/dir permissions
	SubtypeNetworkTransport       = "network_transport"        // transport failure
	SubtypeRateLimited            = "rate_limited"             // throttled (429) by the upstream: retryable after a short backoff
	SubtypeClientRateLimited      = "client_rate_limited"      // the CLI's own throttle refused to wait for a local send slot: reduce frequency, NOT auto-retried
	SubtypeIPBanned               = "ip_banned"                // IP auto-banned (418): NOT retryable — back off for the full ban window
	SubtypeUpstreamError          = "upstream_error"           // upstream API returned a non-zero code
	SubtypeUnexpected             = "unexpected"               // catch-all internal error
	SubtypeLoginRequired          = "login_required"           // an identity needs an interactive login before it can be used
	SubtypePermissionDenied       = "permission_denied"        // the identity is authenticated but lacks the RBAC permission the operation requires
	SubtypeDependencyMissing      = "dependency_missing"       // a required external tool (e.g. npx/Node.js) is not installed
	SubtypeConfirmationRequired   = "confirmation_required"    // a write needs the user's explicit confirmation and no interactive channel is available (agent must relay to the user, then retry with --yes)
	SubtypeInteractiveRequired    = "interactive_required"     // the action must be performed by a person in an interactive session, because something has to be entered that must not pass through a caller (agent must relay to the user, then retry)
	SubtypeSubscriptionRejected   = "subscription_rejected"    // a WS subscription (or a futures WS login) was explicitly rejected by the server
	SubtypeSubscriptionAckTimeout = "subscription_ack_timeout" // the server never acknowledged a WS subscription within the ack window

)

// ExitConfirmationRequired is the dedicated exit code for a confirmation_required
// error. It is distinct from the ordinary validation code (2) so an agent or
// script can route on "needs the user's explicit confirmation, then retry" as
// its own outcome, rather than as one more validation failure.
const ExitConfirmationRequired = 10

// ExitInteractiveRequired is the dedicated exit code for an interactive_required
// error, and is deliberately NOT ExitConfirmationRequired.
//
// The two look alike — both mean "a human has to do something" — but the retry
// differs, and that is what a caller routes on. A confirmation is satisfied by
// the SAME caller re-running with --yes once the user says go. This one is not
// satisfiable by the caller at all: something must be typed that must never pass
// through it, so the user runs the command themselves and the caller retries
// only afterwards. Folding it into 10 would hand `--yes` retry logic a case
// where --yes does nothing, which loops silently instead of failing.
const ExitInteractiveRequired = 11

// Error is this module's typed error. It implements the error interface and
// carries the envelope fields.
type Error struct {
	Type    Type   // required: broad category
	Subtype string // required: subcategory
	Message string // required: human-readable description
	Param   string // optional: the user input that actually failed (e.g. --symbol)
	Hint    string // recommended: recovery guidance for the agent
	Code    int    // optional: upstream API code (only meaningful for TypeAPI)
	// RetryAfter is the backoff the upstream asked for (from the Retry-After
	// header on a 429/418). Zero means none was given. The retry loop uses it as
	// the floor for its wait, so an honored throttle is not under-waited.
	RetryAfter time.Duration
	cause      error // original error, for errors.Unwrap traversal
}

func (e *Error) Error() string {
	if e.Param != "" {
		return fmt.Sprintf("[%s/%s] %s (%s)", e.Type, e.Subtype, e.Message, e.Param)
	}
	return fmt.Sprintf("[%s/%s] %s", e.Type, e.Subtype, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithParam annotates the user input that actually failed. Name only the field
// the caller actually passed in.
func (e *Error) WithParam(p string) *Error { e.Param = p; return e }

// WithHint sets the recovery guidance for the agent.
func (e *Error) WithHint(format string, a ...any) *Error {
	e.Hint = fmt.Sprintf(format, a...)
	return e
}

// WithCause keeps the original error for errors.Is / errors.As traversal.
func (e *Error) WithCause(err error) *Error { e.cause = err; return e }

// WithRetryAfter records the backoff the upstream requested (Retry-After).
func (e *Error) WithRetryAfter(d time.Duration) *Error { e.RetryAfter = d; return e }

// Validation constructs a validation error (a user input / request structure problem).
func Validation(subtype, format string, a ...any) *Error {
	return &Error{Type: TypeValidation, Subtype: subtype, Message: fmt.Sprintf(format, a...)}
}

// Network constructs a network error (a transport-layer failure).
func Network(format string, a ...any) *Error {
	return &Error{Type: TypeNetwork, Subtype: SubtypeNetworkTransport, Message: fmt.Sprintf(format, a...)}
}

// Internal constructs an internal error (a bug in this program that should not happen).
func Internal(format string, a ...any) *Error {
	return &Error{Type: TypeInternal, Subtype: SubtypeUnexpected, Message: fmt.Sprintf(format, a...)}
}

// API constructs an api error (a business error returned by the MEXC API).
func API(code int, subtype, format string, a ...any) *Error {
	return &Error{Type: TypeAPI, Subtype: subtype, Message: fmt.Sprintf(format, a...), Code: code}
}

// InteractiveRequired constructs the typed error an action returns when it can
// only be performed by a person in an interactive session — entering a
// credential, say, where the value must not travel through the caller that would
// otherwise supply it. It is a validation error with the dedicated
// interactive_required subtype, which ExitCode maps to ExitInteractiveRequired.
//
// Distinct from ConfirmationRequired: that one wants the user's ASSENT, and the
// caller proceeds itself once given it. This one wants the user's INPUT, and the
// caller must stand aside entirely.
func InteractiveRequired(format string, a ...any) *Error {
	return &Error{Type: TypeValidation, Subtype: SubtypeInteractiveRequired, Message: fmt.Sprintf(format, a...)}
}

// ConfirmationRequired constructs the typed error a write operation returns when
// it needs the user's explicit confirmation but no interactive channel is
// available (an agent shelling out with no tty; see the write-confirmation
// design). It is a validation error with the dedicated confirmation_required
// subtype, which ExitCode maps to ExitConfirmationRequired so the caller can
// tell "confirm and retry" apart from an ordinary input error.
func ConfirmationRequired(format string, a ...any) *Error {
	return &Error{Type: TypeValidation, Subtype: SubtypeConfirmationRequired, Message: fmt.Sprintf(format, a...)}
}

// ExitCode maps the broad error category to a stable process exit code, for
// scripts and agents to route on. Anything that is not an *Error is treated as
// internal (5).
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if !errors.As(err, &e) {
		return 5
	}
	// A confirmation-required error carries its own exit code so an agent/script
	// can route on "needs the user's explicit confirmation, then retry" distinctly
	// from a plain validation failure.
	if e.Subtype == SubtypeConfirmationRequired {
		return ExitConfirmationRequired
	}
	// Same reason, different retry — see ExitInteractiveRequired.
	if e.Subtype == SubtypeInteractiveRequired {
		return ExitInteractiveRequired
	}
	switch e.Type {
	case TypeValidation:
		return 2
	case TypeNetwork:
		return 4
	case TypeAPI:
		return 6
	default:
		return 5
	}
}
