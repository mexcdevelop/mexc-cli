package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"validation", Validation(SubtypeInvalidArgument, "x"), 2},
		{"network", Network("x"), 4},
		{"internal", Internal("x"), 5},
		{"api", API(700, SubtypeUpstreamError, "x"), 6},
		{"plain error → internal", errors.New("boom"), 5},
		{"wrapped typed error", fmt.Errorf("ctx: %w", Network("x")), 4},
		{"confirmation_required → dedicated 10", ConfirmationRequired("needs --yes"), ExitConfirmationRequired},
		{"wrapped confirmation_required → 10", fmt.Errorf("ctx: %w", ConfirmationRequired("x")), ExitConfirmationRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestError_AsThroughCause(t *testing.T) {
	cause := errors.New("root cause")
	err := Internal("wrap").WithCause(cause)

	var e *Error
	if !errors.As(err, &e) {
		t.Fatal("errors.As failed to extract *Error")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is failed to find the wrapped cause")
	}
}

func TestError_Message(t *testing.T) {
	e := Validation(SubtypeInvalidArgument, "bad %s", "value").WithParam("--symbol")
	got := e.Error()
	want := "[validation/invalid_argument] bad value (--symbol)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestError_Builders(t *testing.T) {
	e := API(700, SubtypeUpstreamError, "upstream said no").WithHint("retry later")
	if e.Type != TypeAPI || e.Subtype != SubtypeUpstreamError || e.Code != 700 {
		t.Fatalf("unexpected fields: %+v", e)
	}
	if e.Hint != "retry later" {
		t.Fatalf("hint = %q", e.Hint)
	}
}

func TestPermissionDeniedSubtype(t *testing.T) {
	e := Validation(SubtypePermissionDenied, "nope")
	if e.Subtype != "permission_denied" {
		t.Fatalf("Subtype = %q, want permission_denied", e.Subtype)
	}
	if ExitCode(e) != 2 {
		t.Fatalf("ExitCode = %d, want 2 (validation)", ExitCode(e))
	}
}

// The two "a human must act" outcomes must stay distinguishable by exit code.
// They differ in who retries and how: a confirmation is satisfied by the SAME
// caller re-running with --yes, while interactive_required cannot be satisfied
// by the caller at all. Collapsing them would feed a case where --yes does
// nothing to retry logic built around --yes.
func TestExitCode_InteractiveIsDistinctFromConfirmation(t *testing.T) {
	conf := ConfirmationRequired("needs a nod")
	inter := InteractiveRequired("needs a person")

	if ExitCode(conf) != ExitConfirmationRequired {
		t.Errorf("confirmation exit = %d, want %d", ExitCode(conf), ExitConfirmationRequired)
	}
	if ExitCode(inter) != ExitInteractiveRequired {
		t.Errorf("interactive exit = %d, want %d", ExitCode(inter), ExitInteractiveRequired)
	}
	if ExitConfirmationRequired == ExitInteractiveRequired {
		t.Fatal("the two must not share a code — the retry differs")
	}
	// Neither may fall back to the plain validation code, which is what an agent
	// reads as "adjust your arguments and try again".
	for _, e := range []error{conf, inter} {
		if ExitCode(e) == 2 {
			t.Errorf("%v must not use the plain validation exit code", e)
		}
	}
	// Both are still validation errors in type — only the subtype routes them.
	if inter.Type != TypeValidation || inter.Subtype != SubtypeInteractiveRequired {
		t.Errorf("unexpected shape: %+v", inter)
	}
}
