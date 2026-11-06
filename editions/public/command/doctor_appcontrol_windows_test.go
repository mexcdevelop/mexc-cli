//go:build windows

package command

import (
	"strings"
	"testing"
)

// The verdict must split on whether the policy is actually BLOCKING, because the
// row's only purpose is to explain a failure. Reporting an off or evaluating
// policy as a warning would tell a working install to change a security setting
// for no reason; staying silent while it enforces leaves the user with `spawn
// UNKNOWN` and nothing connecting it to Windows.
func TestAppControlResult(t *testing.T) {
	tests := []struct {
		name    string
		state   int
		applies bool
		status  string
	}{
		{"enforcing", sacEnforcing, true, statusWarn},
		{"evaluating", sacEvaluating, true, statusPass},
		// Off is the overwhelmingly common case. It gets no row at all: the check
		// exists to explain a symptom, not to enumerate settings that are fine.
		{"off", sacOff, false, ""},
		// A value this build does not know is treated as off rather than guessed
		// at. Inventing a verdict from an unrecognised state would be worse than
		// saying nothing.
		{"unknown state", 99, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, applies := appControlResult(tt.state)
			if applies != tt.applies {
				t.Fatalf("state %d applies = %v, want %v", tt.state, applies, tt.applies)
			}
			if !applies {
				return
			}
			if got.Status != tt.status {
				t.Fatalf("state %d = %s, want %s (%s)", tt.state, got.Status, tt.status, got.Message)
			}
		})
	}
}

// The warning is the whole value of this check, so its content is pinned rather
// than merely its status. A reader arriving here has already seen `spawn
// UNKNOWN` and needs three things: the words they saw, the action that usually
// works, and a way to confirm the cause rather than take our word for it.
func TestAppControlWarningIsActionable(t *testing.T) {
	got, applies := appControlResult(sacEnforcing)
	if !applies {
		t.Fatal("an enforcing policy must produce a row")
	}
	for _, want := range []string{
		"spawn UNKNOWN", // the message the user actually saw
		"again",         // retry, which is what succeeded when this was measured
		"CodeIntegrity", // the log that proves it, so the claim is checkable
		"App & browser", // where the setting lives, for the case retrying does not fix
	} {
		if !strings.Contains(got.Hint, want) {
			t.Errorf("hint must mention %q, got: %s", want, got.Hint)
		}
	}
	// The message must not claim anything about THIS binary's signature. Whether
	// the shipped binaries are signed is a property of the release, and a claim
	// about it here would go stale the day signing lands.
	for _, bad := range []string{"unsigned binary this", "is not signed", "our binary"} {
		if strings.Contains(got.Message, bad) {
			t.Errorf("message must not assert this build's signature status: %s", got.Message)
		}
	}
}
