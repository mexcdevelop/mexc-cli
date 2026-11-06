// Contract test: keeps the operation descriptors (this file) and the SKILL.md
// in sync, ensuring the command / method / path / access never drift.
// Run with `go test ./...`.
package commonapi_test

import (
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
	commonapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/common/api"
)

// TestOperations verifies each operation's properties match what the SKILL.md
// should declare (access, name, method, path). It is a compile-time check, not
// a runtime one — the test fails if the descriptor contradicts itself, which
// would also break the command assembly.
func TestOperations(t *testing.T) {
	ops := commonapi.Operations()
	if len(ops) != 11 {
		t.Fatalf("expected 11 operations, got %d", len(ops))
	}

	// Map expected operations by name
	expected := map[string]struct {
		access api.Access
		method string
		path   string
	}{
		"products-list-v2": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/products/list/V2",
		},
		"products-max-apr": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/products/max_apr",
		},
		"products-by-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/products/by_currency",
		},
		"member-assets-v2": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/assets/v2",
		},
		"member-positions": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/positions",
		},
		"member-positions-by-product-v2": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/positions_by_product/v2",
		},
		"member-recommend": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/recommend",
		},
		"member-pledge-orders": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/pledge_orders",
		},
		"member-redeem-orders": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/redeem_orders",
		},
		"member-pledge-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/pledge_orders/currency",
		},
		"member-redeem-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/common/member/redeem_orders/currency",
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
	}
}

// TestScopeConstants verifies the subdomain's constants match what the registry
// and frontmatter expect (scope, signing, identity).
func TestScopeConstants(t *testing.T) {
	if commonapi.Scope != "financial-openapi.common" {
		t.Errorf("Scope mismatch: got %q, want %q", commonapi.Scope, "financial-openapi.common")
	}
	if commonapi.Signed != signing.CliHMAC {
		t.Errorf("Signed mismatch: got %v, want %v", commonapi.Signed, signing.CliHMAC)
	}
	if commonapi.Identity != identity.OpenAPI {
		t.Errorf("Identity mismatch: got %v, want %v", commonapi.Identity, identity.OpenAPI)
	}
}

func TestHistoryTimeParametersUseUnixMilliseconds(t *testing.T) {
	expected := map[string]bool{"member-pledge-orders": true, "member-redeem-orders": true}
	for _, op := range commonapi.Operations() {
		if !expected[op.Name] {
			continue
		}
		timeParamCount := 0
		for _, param := range op.Params {
			if param.Name != "startTime" && param.Name != "endTime" {
				continue
			}
			timeParamCount++
			if !param.Required || param.Pattern != "[0-9]{13}" {
				t.Errorf("%s --%s must be a required 13-digit Unix-millisecond timestamp", op.Name, param.Name)
			}
		}
		if timeParamCount != 2 {
			t.Errorf("%s must declare startTime and endTime", op.Name)
		}
		if err := op.Validate(map[string]string{"startTime": "1783900800000", "endTime": "1786579199000"}); err != nil {
			t.Errorf("%s rejected valid Unix milliseconds: %v", op.Name, err)
		}
		if err := op.Validate(map[string]string{"startTime": "2026-01-01T00:00:00Z", "endTime": "1786579199000"}); err == nil {
			t.Errorf("%s accepted an ISO 8601 timestamp", op.Name)
		}
		delete(expected, op.Name)
	}
	if len(expected) != 0 {
		t.Errorf("history operations not checked: %v", expected)
	}
}
