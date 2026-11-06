package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// fakeInvalidator is a credential provider registered under a test-only identity
// that implements credential.AuthInvalidator. It lets these tests verify Send's
// generic hook — that a login identity gets the chance to recognize its own auth
// failure and remap the error — without depending on any concrete session
// provider. Resolve/Inject are no-ops so the request reaches the transport
// unchanged.
type fakeInvalidator struct{ realm credential.Realm }

func (f fakeInvalidator) Realm() credential.Realm { return f.realm }

func (fakeInvalidator) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{}, nil
}

func (fakeInvalidator) Inject(*api.RequestSpec, credential.Credentials) error { return nil }

func (fakeInvalidator) OnRequestError(_ *api.RequestSpec, _ hosts.Env, err error) error {
	var e *errs.Error
	if errors.As(err, &e) && e.Type == errs.TypeAPI && e.Code == 401 {
		return errs.Validation(errs.SubtypeLoginRequired, "remapped by fake invalidator")
	}
	return nil
}

// fakeHostResolver is a provider implementing credential.HostResolver, to verify
// Send resolves the base from the identity's own store and refuses a --base-url
// override for it (the host-book policy, exercised without a concrete session
// provider).
type fakeHostResolver struct {
	realm credential.Realm
	host  string
}

func (f fakeHostResolver) Realm() credential.Realm { return f.realm }
func (fakeHostResolver) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{}, nil
}
func (fakeHostResolver) Inject(*api.RequestSpec, credential.Credentials) error { return nil }
func (f fakeHostResolver) ResolveHost(*api.RequestSpec, hosts.Env) (string, error) {
	return f.host, nil
}

func okServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A HostResolver identity resolves its base from its own store, so a request with
// no --base-url reaches the resolver-provided host.
func TestSend_HostResolverProvidesBase(t *testing.T) {
	srv := okServer(t)
	const id api.Identity = "test-hostresolver-base"
	credential.Register(fakeHostResolver{realm: credential.Realm(id), host: srv.URL})

	sp := spec("GET", "/ping", false)
	sp.Identity = id
	c := newSendClient(config.Config{}, "") // no --base-url; the resolver supplies the host

	if _, err := c.Send(context.Background(), sp, nil); err != nil {
		t.Fatalf("Send via resolver host = %v", err)
	}
}

// A --base-url override is REFUSED for a HostResolver identity: its host must come
// from its own vetted store, never argv (a credentialed request must not be
// redirected).
func TestSend_HostResolverRefusesBaseURLOverride(t *testing.T) {
	const id api.Identity = "test-hostresolver-refuse"
	credential.Register(fakeHostResolver{realm: credential.Realm(id), host: "https://ignored"})

	sp := spec("GET", "/x", false)
	sp.Identity = id
	c := newSendClient(config.Config{}, "https://attacker.example") // --base-url set

	_, err := c.Send(context.Background(), sp, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeInvalidArgument {
		t.Fatalf("err = %v, want invalid_argument (host override refused)", err)
	}
}

// A credentialed identity whose host comes from a --base-url override gets a
// heads-up warning (its api key is about to go somewhere the CLI did not choose).
func TestSend_WarnsOnOverriddenHostForIdentity(t *testing.T) {
	oldWarn := credential.Warnf
	var warned []string
	credential.Warnf = func(f string, a ...any) { warned = append(warned, fmt.Sprintf(f, a...)) }
	t.Cleanup(func() { credential.Warnf = oldWarn })

	const id api.Identity = "test-warn-override"
	credential.Register(fakeInvalidator{realm: credential.Realm(id)}) // credentialed, not a HostResolver

	sp := spec("GET", "/x", false)
	sp.Identity = id
	c := newSendClient(config.Config{}, okServer(t).URL) // --base-url = an override host

	if _, err := c.Send(context.Background(), sp, nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(warned) == 0 {
		t.Fatalf("expected an overridden-host warning for a credentialed identity")
	}
}

// A prod dry-run of a HostResolver op must NOT expose the resolved host in
// the plan — it shows a synthetic placeholder (a non-prod dry-run shows the real
// host). Guards the prod-host masking policy at the transport layer.
func TestSend_DryRunProdHidesResolvedHost(t *testing.T) {
	const id api.Identity = "test-dryrun-prod-host"
	credential.Register(fakeHostResolver{realm: credential.Realm(id), host: "https://real-prod-host.example"})

	sp := spec("GET", "/x", false)
	sp.Identity = id
	sp.Permission = &api.PermReq{System: "sys-a"}

	c := New(config.Config{}) // Env "" == prod
	c.DryRun = true
	plan, err := c.Send(context.Background(), sp, nil)
	if err != nil {
		t.Fatalf("dry-run send: %v", err)
	}
	if strings.Contains(plan.URL, "real-prod-host") {
		t.Fatalf("a prod dry-run must not expose the resolved host: %s", plan.URL)
	}
	if !strings.Contains(plan.URL, "recorded-host-for-sys-a") {
		t.Fatalf("expected the synthetic placeholder, got %s", plan.URL)
	}
}

func unauthorizedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"msg":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A concrete identity whose provider implements AuthInvalidator gets its 401
// remapped to login_required by Send's post-send hook.
func TestSend_AuthInvalidatorRemapsError(t *testing.T) {
	const id api.Identity = "test-invalidator-remap"
	credential.Register(fakeInvalidator{realm: credential.Realm(id)})

	sp := spec("GET", "/x", false)
	sp.Identity = id
	c := newSendClient(config.Config{}, unauthorizedServer(t).URL)

	_, err := c.Send(context.Background(), sp, nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeLoginRequired {
		t.Fatalf("err = %v, want a login_required remap from the invalidator", err)
	}
}

// fakeKeyProvider injects a credential as a custom header and declares it a
// reserved header, so tests can verify the redirect policy strips it before a
// cross-host hop (the X-MEXC-APIKEY leak class, without the real openapi provider).
type fakeKeyProvider struct{ realm credential.Realm }

func (f fakeKeyProvider) Realm() credential.Realm { return f.realm }
func (fakeKeyProvider) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{APIKey: "secret"}, nil
}
func (fakeKeyProvider) Inject(spec *api.RequestSpec, cred credential.Credentials) error {
	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header["X-Test-Key"] = cred.APIKey
	return nil
}
func (fakeKeyProvider) ReservedHeaders() []string { return []string{"X-Test-Key"} }

// The JSON path (Send) strips a request's credential header before following a
// redirect to a DIFFERENT host, so an API key never reaches a host the request
// was not authorized for.
func TestSend_StripsCredentialHeaderOnCrossHostRedirect(t *testing.T) {
	var leaked string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Test-Key")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	defer origin.Close()

	const id api.Identity = "test-crosshost-key"
	credential.Register(fakeKeyProvider{realm: credential.Realm(id)})
	sp := spec("GET", "/x", false)
	sp.Identity = id
	c := newSendClient(config.Config{}, origin.URL)

	var dst any
	if _, err := c.Send(context.Background(), sp, &dst); err != nil {
		t.Fatalf("send across a same-origin redirect: %v", err)
	}
	if leaked != "" {
		t.Fatalf("credential header leaked to the cross-host redirect target: %q", leaked)
	}
}

// SendStream (the download path) runs the same auth-invalidator hook as Send: a
// login identity's 401 on a streaming request is remapped to login_required, and
// no body is handed back.
func TestSendStream_AuthInvalidatorRemapsError(t *testing.T) {
	const id api.Identity = "test-stream-invalidator-remap"
	credential.Register(fakeInvalidator{realm: credential.Realm(id)})

	sp := binarySpec("/files/x/download")
	sp.Identity = id
	c := newSendClient(config.Config{}, unauthorizedServer(t).URL)

	res, _, err := c.SendStream(context.Background(), sp)
	if res != nil {
		t.Fatal("no body must be returned on an auth failure")
	}
	var e *errs.Error
	if !errors.As(err, &e) || e.Subtype != errs.SubtypeLoginRequired {
		t.Fatalf("err = %v, want a login_required remap on the streaming path", err)
	}
}

// The hook never fires for an unauthenticated request (IdentityNone / empty): the
// 401 stays a plain upstream api error, unchanged.
func TestSend_AuthHookSkipsNoneIdentity(t *testing.T) {
	c := newSendClient(config.Config{}, unauthorizedServer(t).URL)

	_, err := c.Send(context.Background(), spec("GET", "/x", false), nil)
	var e *errs.Error
	if !errors.As(err, &e) || e.Type != errs.TypeAPI || e.Subtype != errs.SubtypeUpstreamError {
		t.Fatalf("err = %v, want an unchanged upstream api error (no remap)", err)
	}
}
