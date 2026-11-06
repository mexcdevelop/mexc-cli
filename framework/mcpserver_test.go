package framework

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
)

// TestValidateUnattendedPairing pins the assembly-time pairing: an unattended
// build must declare a confirmation-audit decision, so the confirmation gate is
// never turned off while a real sink stays alive to accrue records of
// confirmations that never happened.
func TestValidateUnattendedPairing(t *testing.T) {
	// Attended (the zero value): the pairing does not apply.
	if err := validateUnattendedPairing(Edition{}); err != nil {
		t.Fatalf("an attended build must pass regardless of ConfirmReporter: %v", err)
	}
	// Unattended with no reporter declared: refused.
	if err := validateUnattendedPairing(Edition{Unattended: true}); err == nil {
		t.Fatal("an unattended build with no ConfirmReporter must fail assembly")
	}
	// Unattended with a reporter declared: accepted (that the resolved sink is
	// Discard is asserted where the sink is built, not here).
	ed := Edition{
		Unattended:      true,
		ConfirmReporter: func(Deps) confirmaudit.Reporter { return confirmaudit.Discard{} },
	}
	if err := validateUnattendedPairing(ed); err != nil {
		t.Fatalf("an unattended build with a declared reporter must pass: %v", err)
	}
}

// TestServerCapabilitiesPassthrough pins that the Edition's capability value is
// forwarded verbatim: nil stays nil (so the SDK's inference — and the stdio face —
// is untouched), and a set value is handed on unchanged.
func TestServerCapabilitiesPassthrough(t *testing.T) {
	if serverCapabilities(Edition{}) != nil {
		t.Fatal("nil MCPCapabilities must pass through as nil so SDK inference stands")
	}
	caps := &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: false}}
	if serverCapabilities(Edition{MCPCapabilities: caps}) != caps {
		t.Fatal("a set MCPCapabilities must pass through verbatim")
	}
}
