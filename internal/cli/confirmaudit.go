package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/buildinfo"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// The write-confirmation audit trail: both faces' gates report here, and an
// edition supplies the sink (pkg/confirmaudit).
//
// The gate cannot force a non-interactive caller to relay a prompt to a human —
// it can only refuse and hand the pending write back. So the trail's job is to
// make the difference visible afterwards, which takes two things the stages alone
// do not give: a key pairing the refusal with the later --yes retry (separate
// processes), and which channel each step went through.

// Two rules govern every send:
//
//  1. A record is sent only after the thing it records has happened.
//  2. A record is never sent while the user is waiting on something. Nothing may
//     sit between an approval and the write it approves, or between a result and
//     the user reading it, so an accepted judgement's records go out last — after
//     the request has been issued and the command has produced its output
//     (reportConfirmed). Only a judgement that ends the command reports inline,
//     because by then there is nothing left to delay.
//
// Every send is therefore after the fact and on the caller's own goroutine. One
// judgement's records arrive in the order they happened, and the process exits as
// soon as the last of them returns.
const (
	// reportTimeout bounds one JUDGEMENT's records, not one record — see
	// sendRecords. A failure or a timeout is discarded, so this only bounds how long
	// the trail may delay a command that is already finished with the user's request.
	reportTimeout = 3 * time.Second
	// auditCommandMax bounds the recorded command line, so a pathological argument
	// cannot produce an unbounded record.
	auditCommandMax  = 512
	confirmKeyHexLen = 16
)

// record is one stage/channel pair a judgement produced.
type record struct {
	stage   confirmaudit.Stage
	channel confirmaudit.Channel
}

// sendRecords delivers ONE judgement's records under ONE deadline, best effort.
//
// The budget is per judgement, not per record. A judgement can produce two records,
// and giving each its own timeout makes an unreachable sink cost twice as much for
// one judgement — paid at process exit, after the user already has their result.
//
// Records go in the order they happened, which is also the order that fails safely.
// If the budget runs out mid-judgement the reminder is already sent and the
// confirmation is the one lost, which under-reports a write. The reverse — keeping
// the confirmation and losing the reminder — reads as a self-approving caller, so it
// would manufacture that finding against someone who did nothing wrong.
//
// A failure is discarded and a panic recovered, per record: both are an edition's
// code, and neither may take the user's command — nor the rest of the judgement —
// with it. The trail is evidence, not a gate.
func sendRecords(f *Factory, base confirmaudit.Event, rs ...record) {
	if f == nil || f.ConfirmReporter == nil || len(rs) == 0 {
		return
	}
	defer func() { _ = recover() }() // an edition's constructor may panic
	rep := f.ConfirmReporter()
	if rep == nil {
		return
	}
	// Background, not the command's context: a record must still go out on the
	// paths that are about to abort the command.
	ctx, cancel := context.WithTimeout(context.Background(), reportTimeout)
	defer cancel()
	for _, r := range rs {
		ev := base
		ev.Stage, ev.Channel = r.stage, r.channel
		func() {
			defer func() { _ = recover() }() // one bad send must not cost the rest
			_ = rep.Report(ctx, ev)
		}()
	}
}

// reportConfirmed records what an ACCEPTED judgement produced. The caller defers
// it, so it runs after the request has been issued and after the command has
// written its result — the trail then costs the user nothing, however slow the sink
// is.
//
// It carries the reminder too, for every channel that actually prompted. The
// prompt was answered before the request went out, so recording it is still after
// the fact; recording it inside the gate would have put a send in the one place a
// send must never be. assume-yes is the exception: no prompt happened, and the
// missing reminder is the signal that finds a self-approving caller.
//
// ch empty means there was nothing to confirm (a read, a dry-run).
func reportConfirmed(f *Factory, base confirmaudit.Event, ch confirmaudit.Channel) {
	switch ch {
	case "":
		return
	case confirmaudit.ChannelAssumeYes:
		sendRecords(f, base, record{confirmaudit.StageConfirmed, ch})
	default:
		sendRecords(f, base,
			record{confirmaudit.StageRemind, ch},
			record{confirmaudit.StageConfirmed, ch})
	}
}

const (
	faceCLI = "cli"
	faceMCP = "mcp"
)

// auditEvent builds the payload one judgement's records share; the report helpers
// fill Stage and Channel per site. cmdPath is the invoked command path, which only
// the CLI face has.
func auditEvent(f *Factory, op api.Operation, values map[string]string, data, cmdPath, face string, grade credential.ConfirmGrade) confirmaudit.Event {
	// From the resolved config, i.e. the value the transport routes with —
	// reading it elsewhere would let the record name a different deployment.
	env := f.Config().Env
	return confirmaudit.Event{
		ConfirmKey:    confirmKey(env, op, values, data),
		InvocationID:  newInvocationID(),
		Command:       auditCommand(cmdPath, op, values, data),
		RequestPath:   op.Path,
		RequestMethod: strings.ToUpper(op.Method),
		APIDesc:       op.Summary,
		CLIVersion:    buildinfo.Version,
		RiskTier:      string(op.RiskTier()),
		GradeCode:     grade.Code,
		GradeReason:   grade.Reason,
		Face:          face,
		Interactive:   ttyReachable(),
		AgentSource:   agentSource(f.Env),
		Env:           env,
	}
}

type maskedPair struct{ name, value string }

// maskedPairs returns the non-empty parameters in declaration order, Sensitive
// values masked. It is the ONE masking implementation — the prompt, the record
// and the fingerprint all render from it, so a newly Sensitive parameter cannot
// end up hidden in the prompt and readable in the trail.
func maskedPairs(op api.Operation, values map[string]string) []maskedPair {
	out := make([]maskedPair, 0, len(op.Params))
	for _, p := range op.Params {
		v := values[p.Name]
		if v == "" {
			continue
		}
		if p.Sensitive {
			v = api.SensitiveMask
		}
		out = append(out, maskedPair{name: p.Name, value: v})
	}
	return out
}

// keyInput is the canonical form confirmKey fingerprints. A JSON object, not
// joined lines: encoding/json sorts the map's keys, so the fingerprint is
// independent of declaration order, and it escapes values, so none can forge the
// boundary between two fields.
type keyInput struct {
	Env    string            `json:"env"`
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Params map[string]string `json:"params"`
	Body   string            `json:"body"`
}

// confirmKey fingerprints WHICH write this is, so the REMIND from one process and
// the CONFIRMED from the --yes retry pair up.
//
// Deliberately not inputs: the rendered command line (the two faces render it
// differently for one request), the operator (a sink keys that itself), and the
// time (the stages are minutes apart by design). The env IS an
// input — a reminder against a test deployment must not vouch for a confirmation
// against production. Sensitive values enter masked.
func confirmKey(env string, op api.Operation, values map[string]string, data string) string {
	params := make(map[string]string, len(op.Params))
	for _, p := range maskedPairs(op, values) {
		params[p.name] = p.value
	}
	in, err := json.Marshal(keyInput{
		Env:    env,
		Method: strings.ToUpper(op.Method),
		Path:   op.Path,
		Params: params,
		// Masked BEFORE hashing, like the params above. A sensitive value hashed
		// raw is recoverable: the fingerprint is short and everything else about
		// the request is in the record, so a low-entropy secret falls to a
		// candidate sweep. Masking also keeps a value that differs per invocation
		// from breaking the pairing this key exists for.
		Body: canonicalBodyHash(op.MaskSensitiveBody(data)),
	})
	if err != nil {
		// Unreachable (every field is a string), but an empty key would pair with
		// every other empty key, so fall back to the parts that cannot fail.
		in = []byte(env + "\x00" + op.Method + "\x00" + op.Path)
	}
	sum := sha256.Sum256(in)
	return hex.EncodeToString(sum[:])[:confirmKeyHexLen]
}

// canonicalBodyHash hashes the body ignoring differences no backend would see —
// key order and whitespace — so a re-serialised identical request still pairs.
// UseNumber keeps numbers as their literals; through float64 two different large
// integers would fingerprint alike. A non-JSON body hashes as its trimmed bytes.
func canonicalBodyHash(data string) string {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return ""
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) == nil && !dec.More() {
		if canonical, err := json.Marshal(v); err == nil {
			trimmed = string(canonical)
		}
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:])
}

// auditCommand renders the invocation for the record: the command path, its
// non-empty parameters with Sensitive values masked, and the masked body.
// Rebuilt from the declaration, never from os.Args, which carries secrets in the
// clear.
func auditCommand(cmdPath string, op api.Operation, values map[string]string, data string) string {
	if cmdPath == "" {
		// The MCP face has no command path; name the binary and the operation so a
		// record from either face reads the same way.
		cmdPath = cliname.Name() + " " + op.Name
	}
	var b strings.Builder
	b.WriteString(cmdPath)
	for _, p := range maskedPairs(op, values) {
		fmt.Fprintf(&b, " --%s %s", p.name, p.value)
	}
	if op.Body != nil {
		if d := strings.TrimSpace(data); d != "" {
			fmt.Fprintf(&b, " --data %s", op.MaskSensitiveBody(d))
		}
	}
	return truncateBytes(b.String(), auditCommandMax)
}

// truncateBytes cuts s to at most limit BYTES without splitting a rune: the bound
// is on bytes, and half a rune at the boundary is not valid UTF-8.
func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// newInvocationID mints the id for ONE gate entry, shared by that entry's
// records. Per entry, not per process: the MCP face serves many judgements from
// one process, and a per-process value would make every one after the first look
// like a duplicate.
func newInvocationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Report anyway, unidentified: a failure to read randomness must not become
		// a hole in the trail.
		return ""
	}
	return hex.EncodeToString(b)
}

// Markers a harness sets about ITSELF. Nothing here identifies the person, and
// nothing is read from a terminal's own variables.
var agentMarkers = []struct{ key, source string }{
	{"CLAUDECODE", "claude-code"},
	{"CLAUDE_CODE_ENTRYPOINT", "claude-code"},
	{"CURSOR_TRACE_ID", "cursor"},
	{"GITHUB_ACTIONS", "github-actions"},
	{"GITLAB_CI", "gitlab-ci"},
}

// agentSource names the calling harness. A hint, never evidence: any caller can
// set or clear these. An unrecognised caller is reported as unknown, not guessed.
func agentSource(env Env) string {
	if env == nil {
		return "unknown"
	}
	for _, m := range agentMarkers {
		if env(m.key) != "" {
			return m.source
		}
	}
	return "unknown"
}

// ttyReachable reports whether this caller could have been prompted — the field
// separating a person typing --yes from a non-interactive caller adding it. It
// asks through the same function the gate uses, so the record cannot claim a
// channel the gate would not have used.
//
// IT USED TO ASK THE DEVICE, and that answered yes for every agent shell-out: the
// child inherits the person's terminal (see console.StdioIsTerminal), so the one
// caller this field exists to single out was recorded as interactive. For an
// audit that is the wrong direction — a self-approving agent wearing a human's
// mark, in the field kept to tell them apart.
//
// It is a plain delegation now rather than two checks, because the gate itself is
// one check: the prompt runs on stdio and opens no device, so there is no second
// way to be unpromptable. Keeping a device open here would make the record claim
// something the gate never consulted.
func ttyReachable() bool {
	return stdioIsTerminalFn()
}

// validateConfirmReporter refuses a build that mounts writes without a sink. An
// absent sink produces the same output as a working one, so the only safe
// direction is to fail assembly. Opting out is confirmaudit.Discard{}.
func validateConfirmReporter(ds []api.DomainSpec, present bool) error {
	if present || !hasWriteOp(ds) {
		return nil
	}
	return errs.Internal("this build mounts write operations but declares no write-confirmation audit sink").
		WithHint("set Edition.ConfirmReporter; a build that deliberately does not audit wires confirmaudit.Discard{}")
}

func hasWriteOp(ds []api.DomainSpec) bool {
	for _, d := range ds {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				if op.IsWrite() {
					return true
				}
			}
		}
	}
	return false
}
