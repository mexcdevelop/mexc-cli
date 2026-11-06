package credential

import (
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

func TestCredentialsGrantsField(t *testing.T) {
	c := Credentials{Grants: map[string][]string{"s": {"a"}}}
	if c.Grants["s"][0] != "a" {
		t.Fatalf("Grants not carried: %+v", c)
	}
}

// gradingProvider is a Provider that also grades. It registers under its own
// realm so it cannot disturb anything else registered in this package.
type gradingProvider struct {
	grade     ConfirmGrade
	gotEnv    hosts.Env
	gotReq    *api.PermReq
	gotAccess api.Access
}

func (p *gradingProvider) Realm() Realm { return Realm("grading-test") }

func (p *gradingProvider) Resolve(config.Config, hosts.Env) (Credentials, error) {
	return Credentials{}, nil
}

func (p *gradingProvider) Inject(*api.RequestSpec, Credentials) error { return nil }

func (p *gradingProvider) ConfirmGrade(env hosts.Env, req *api.PermReq, access api.Access) ConfirmGrade {
	p.gotEnv, p.gotReq, p.gotAccess = env, req, access
	return p.grade
}

func TestGradeConfirm_AsksTheProviderAndPassesEveryArgument(t *testing.T) {
	p := &gradingProvider{grade: ConfirmGrade{Confirm: true, Reason: "because"}}
	Register(p)
	// This package has Register but no Unregister; the test is in-package, so it
	// undoes its own registration directly.
	t.Cleanup(func() { delete(registry, p.Realm()) })

	req := &api.PermReq{System: "site-a", Codes: []string{"a:read"}}
	got := GradeConfirm(p.Realm(), hosts.Env("prod"), req, api.Read)

	if !got.Confirm || got.Reason != "because" {
		t.Fatalf("grade = %+v, want {true because}", got)
	}
	if p.gotReq != req {
		t.Errorf("provider got req %v, want the one passed in", p.gotReq)
	}
	if p.gotAccess != api.Read {
		t.Errorf("provider got access %q, want %q", p.gotAccess, api.Read)
	}
	// The env has to travel: a provider holding per-env state would otherwise
	// grade against the wrong deployment.
	if p.gotEnv != hosts.Env("prod") {
		t.Errorf("provider got env %q, want prod", p.gotEnv)
	}
}

func TestGradeConfirm_GradesNothingWithoutTheCapabilityOrTheRequirement(t *testing.T) {
	p := &gradingProvider{grade: ConfirmGrade{Confirm: true, Reason: "because"}}
	Register(p)
	t.Cleanup(func() { delete(registry, p.Realm()) })
	req := &api.PermReq{System: "site-a", Codes: []string{"a:read"}}

	// A nil requirement has nothing to grade, even from a grading provider.
	if got := GradeConfirm(p.Realm(), hosts.Env("prod"), nil, api.Read); got.Confirm {
		t.Errorf("nil req graded %+v, want the zero value", got)
	}
	// An unregistered realm has no provider to ask.
	if got := GradeConfirm(Realm("nobody-registered-here"), hosts.Env("prod"), req, api.Read); got.Confirm {
		t.Errorf("unregistered realm graded %+v, want the zero value", got)
	}
}
