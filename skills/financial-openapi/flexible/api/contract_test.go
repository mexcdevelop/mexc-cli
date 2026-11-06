// Contract test: keeps the operation descriptors (this file) and the SKILL.md
// in sync, ensuring the command / method / path / access never drift.
// Run with `go test ./...`.
package flexibleapi_test

import (
	"testing"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
	flexibleapi "github.com/mexcdevelop/mexc-cli/skills/financial-openapi/flexible/api"
)

// TestOperations verifies each operation's properties match what the SKILL.md
// should declare (access, name, method, path). It is a compile-time check, not
// a runtime one — the test fails if the descriptor contradicts itself, which
// would also break the command assembly.
func TestOperations(t *testing.T) {
	ops := flexibleapi.Operations()
	if len(ops) != 18 {
		t.Fatalf("expected 18 operations, got %d", len(ops))
	}

	// Map expected operations by name
	expected := map[string]struct {
		access  api.Access
		method  string
		path    string
		hasBody bool
	}{
		"auto-pledge-setting": {
			access: api.Write,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/auto_pledge/setting",
		},
		"auto-pledge-setting-query": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/auto_pledge/setting/query",
		},
		"auto-pledge-financial-setting": {
			access:  api.Write,
			method:  "POST",
			path:    "/api/v3/financial/cli/flexible/auto_pledge/financial/setting",
			hasBody: true,
		},
		"auto-pledge-financial-list": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/auto_pledge/financial_list",
		},
		"member-pledge-orders-page": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/pledge_orders/page",
		},
		"member-redeemable-list": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/redeemable/list",
		},
		"member-redeem-orders-page": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/redeem_orders/page",
		},
		"member-profit-record-page": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/profit_record/page",
		},
		"apr-history": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/apr/history",
		},
		"detail": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/detail",
		},
		"member-pledge-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/pledge_orders/currency",
		},
		"member-redeem-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/redeem_orders/currency",
		},
		"member-profit-currency": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/profit_record/currency",
		},
		"auto-pledge-apr": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/auto_pledge/apr",
		},
		"auto-pledge-kyc-level": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/auto_pledge/kyc_level",
		},
		"member-position-detail": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/position_detail",
		},
		"member-profit-record-summary": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/profit_record_summary",
		},
		"member-interest-detail": {
			access: api.Read,
			method: "GET",
			path:   "/api/v3/financial/cli/flexible/member/interest_detail",
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
	if flexibleapi.Scope != "financial-openapi.flexible" {
		t.Errorf("Scope mismatch: got %q, want %q", flexibleapi.Scope, "financial-openapi.flexible")
	}
	if flexibleapi.Signed != signing.CliHMAC {
		t.Errorf("Signed mismatch: got %v, want %v", flexibleapi.Signed, signing.CliHMAC)
	}
	if flexibleapi.Identity != identity.OpenAPI {
		t.Errorf("Identity mismatch: got %v, want %v", flexibleapi.Identity, identity.OpenAPI)
	}
}

func TestHistoryTimeParametersUseUnixMilliseconds(t *testing.T) {
	expected := map[string]bool{"member-pledge-orders-page": true, "member-redeem-orders-page": true, "member-profit-record-page": true}
	for _, op := range flexibleapi.Operations() {
		if !expected[op.Name] {
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
