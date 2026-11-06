// Package confirmaudit is the port for reporting the write-confirmation
// lifecycle to an audit sink.
//
// This layer decides WHEN a report is due and WHAT one says — derived from the
// confirmation gate, so it is the same in every product. A product supplies the
// wire: the endpoint, how the request authenticates, and who the acting operator
// is. It lives in pkg/ because an edition — including one in a separate module —
// implements Reporter.
package confirmaudit

import "context"

// Stage is which step of the confirmation lifecycle a record describes.
type Stage string

const (
	// StageRemind attests that the CLI ASKED for a confirmation: a prompt was
	// rendered and answered, or none could be and the pending write was handed back
	// to be relayed. Reported after the asking, never before.
	StageRemind Stage = "REMIND"
	// StageConfirmed attests that the user confirmed. Reported after the request has
	// been issued, and independent of what became of it — the stage is about the
	// user's act, not the request's fate, so it stands even if the upstream rejected
	// the write or it was never built.
	StageConfirmed Stage = "CONFIRMED"
	// StageCancelled is reported when the user refuses at the prompt. Distinct
	// from a reminder with no confirmation, which is also what an unanswered
	// prompt and an abandoned relay look like.
	StageCancelled Stage = "CANCELLED"
)

// Channel is HOW the confirmation was asked for, or how it was obtained.
//
// Stage alone cannot tell a caller that relayed the prompt to a human from one
// that approved on its own — both end in StageConfirmed.
type Channel string

const (
	// ChannelTTY is the interactive prompt on the controlling terminal.
	ChannelTTY Channel = "tty"
	// ChannelRefusal is the non-interactive path: no prompt could be rendered, so
	// the gate refused and handed the pending write back to be relayed. It
	// evidences that the CLI asked — not that the caller passed the question on.
	ChannelRefusal Channel = "refusal"
	// ChannelElicitation is the MCP host's own dialog. The host renders it, so the
	// calling agent cannot answer it on the user's behalf.
	ChannelElicitation Channel = "mcp-elicitation"
	// ChannelAssumeYes is a confirmation asserted on the invocation itself
	// (--yes / confirm=true) rather than obtained through a prompt.
	ChannelAssumeYes Channel = "assume-yes"
)

// Event is one audit record. Every field is filled by the shared layer; a
// Reporter maps it onto its product's request shape and adds only the operator
// identity its endpoint requires.
type Event struct {
	Stage   Stage
	Channel Channel

	// ConfirmKey identifies WHICH write this is: deterministic over the env, the
	// operation, its masked parameters and a canonical body hash, so the REMIND
	// from one process and the CONFIRMED from the --yes retry share it.
	ConfirmKey string
	// InvocationID identifies WHICH judgement this is: random, minted once per gate
	// entry and shared by that entry's records. Per entry, NOT per process — one
	// process can serve many judgements. One judgement reports a given stage once,
	// so a repeat is the same record rather than a new event.
	InvocationID string

	// Command is the invocation rebuilt from the declaration with sensitive values
	// masked — never the raw argv, which carries secrets in the clear.
	Command string
	// RequestPath is the operation's path TEMPLATE; concrete values are in Command.
	RequestPath   string
	RequestMethod string
	APIDesc       string
	CLIVersion    string
	RiskTier      string
	// GradeCode and GradeReason say why an identity demanded this confirmation,
	// or are empty when only the write semantics demanded it.
	//
	// Two fields, because a trail is queried and a person is informed on
	// different clocks: GradeCode is the stable key an audit groups by, and
	// GradeReason is prose that will be reworded. Keying a trail on the sentence
	// would lose its history the first time the sentence improves.
	//
	// Both are opaque here. The grading vocabulary belongs to whoever supplies
	// the identity, and a scale named in this port would be one product's
	// governance in a package every product imports.
	GradeCode   string
	GradeReason string
	Face        string // cli | mcp
	// Interactive is whether this caller could have been prompted at all — what
	// separates a person typing --yes from a non-interactive caller adding it.
	//
	// It takes BOTH that a controlling terminal opens and that the caller's own
	// streams are on one. The device alone does not decide it: a process an agent
	// shells out to inherits the person's terminal while the agent holds its
	// streams, so a device-only answer marked every self-approving agent as
	// interactive — the exact caller this field is here to single out.
	Interactive bool
	// AgentSource names the calling harness when it identifies itself. A hint, not
	// a credential: any caller can set or clear the variables it is read from.
	AgentSource string
	Env         string
}

// Reporter sends one Event.
//
// The caller WAITS for it, on its own goroutine, bounded by a short timeout, and
// then discards whatever it returns — the trail is evidence, not a gate on the
// write. So an implementation cannot fail a command, but it can hold one: it must
// return promptly, and must not retry past its context. The caller only ever calls
// it at a point where the user is waiting on nothing, which is why the timeout is
// the whole of the protection.
type Reporter interface {
	Report(ctx context.Context, ev Event) error
}

// Discard is the explicit no-op sink, so that not auditing is a decision written
// in one greppable place rather than an omission.
type Discard struct{}

// Report discards ev.
func (Discard) Report(context.Context, Event) error { return nil }
