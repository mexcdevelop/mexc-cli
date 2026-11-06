// Contract test: keeps the operation descriptors (this file) and the SKILL.md
// in sync, ensuring the command / method / path / access never drift.
// Run with `go test ./...`.
package contractholdapi_test

import (
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
	contractholdapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/contract-hold/api"
)

// TestOperations verifies each operation's properties match what the SKILL.md
// should declare (access, name, method, path). It is a compile-time check, not
// a runtime one — the test fails if the descriptor contradicts itself, which
// would also break the command assembly.
func TestOperations(t *testing.T) {
	ops := contractholdapi.Operations()
	if len(ops) != 10 {
		t.Fatalf("expected 10 operations, got %d", len(ops))
	}

	// Map expected operations by name
	expected := map[string]struct {
		access  api.Access
		method  string
		path    string
		hasBody bool
	}{
		"products-list": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/products/list",
		},
		"products-max-apr": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/products/max_apr",
		},
		"products-max-base-apr": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/products/max_base_apr",
		},
		"member-asset": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/member/asset",
		},
		"register": {
			access:  api.Write,
			method:  "POST",
			path:    "/api/v3/financial/cli/contract_hold/register",
			hasBody: true,
		},
		"register-query": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/register/query",
		},
		"member-profit-record": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/member/profit_record",
		},
		"member-profit-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/member/profit_record/currency",
		},
		"member-positions": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/contract_hold/member/positions",
		},
		"profit-calculator": {
			access:  api.Read,
			method:  "POST",
			path:    "/api/v3/financial/cli/contract_hold/profit_calculator/calculate",
			hasBody: true,
		},
	}

	for _, op := range ops {
		exp, ok := expected[op.Name]
		if !ok {
			t.Errorf("unexpected operation name: %q", op.Name)
			continue
		}

		if op.Access != exp.access {
			t.Errorf("operation %q: access mismatch: got %v, want %v", op.Name, op.Access, exp.access)
		}
		if op.Method != exp.method {
			t.Errorf("operation %q: method mismatch: got %q, want %q", op.Name, op.Method, exp.method)
		}
		if op.Path != exp.path {
			t.Errorf("operation %q: path mismatch: got %q, want %q", op.Name, op.Path, exp.path)
		}
		if exp.hasBody && op.Body == nil {
			t.Errorf("operation %q: expected Body but got nil", op.Name)
		}
		if !exp.hasBody && op.Body != nil {
			t.Errorf("operation %q: unexpected Body present", op.Name)
		}
	}
}

// TestScopeConstants verifies the subdomain's constants match what the registry
// and frontmatter expect (scope, signing, identity).
func TestScopeConstants(t *testing.T) {
	if contractholdapi.Scope != "financial-openapi.contract-hold" {
		t.Errorf("Scope mismatch: got %q, want %q", contractholdapi.Scope, "financial-openapi.contract-hold")
	}
	if contractholdapi.Signed != signing.CliHMAC {
		t.Errorf("Signed mismatch: got %v, want %v", contractholdapi.Signed, signing.CliHMAC)
	}
	if contractholdapi.Identity != identity.OpenAPI {
		t.Errorf("Identity mismatch: got %v, want %v", contractholdapi.Identity, identity.OpenAPI)
	}
}

func TestHistoryTimeParametersUseUnixMilliseconds(t *testing.T) {
	for _, op := range contractholdapi.Operations() {
		if op.Name != "member-profit-record" {
			continue
		}
		timeParamCount := 0
		for _, param := range op.Params {
			if param.Name != "startTime" && param.Name != "endTime" {
				continue
			}
			timeParamCount++
			if param.Pattern != "[0-9]{13}" {
				t.Errorf("%s --%s must be a 13-digit Unix-millisecond timestamp", op.Name, param.Name)
			}
		}
		if timeParamCount != 2 {
			t.Errorf("%s must declare startTime and endTime", op.Name)
		}
		if err := op.Validate(map[string]string{"startTime": "1783900800000", "endTime": "1786579199000"}); err != nil {
			t.Fatalf("member-profit-record rejected valid Unix milliseconds: %v", err)
		}
		if err := op.Validate(map[string]string{"startTime": "2026-01-01T00:00:00Z", "endTime": "1786579199000"}); err == nil {
			t.Fatal("member-profit-record accepted an ISO 8601 timestamp")
		}
		return
	}
	t.Fatal("member-profit-record was not declared")
}
