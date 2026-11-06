package adapter

import (
	"errors"

	"github.com/mexcdevelop/mexc-cli/editions/public/profile"
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
)

// APIKeyHeader is the request header that carries the API key. It lives with the
// identity that injects it, not in the shared config: it is this product's wire
// detail, meaningless to a product authenticating some other way.
const APIKeyHeader = "X-MEXC-APIKEY"

// OpenAPI is the api-key identity: it attaches the API key in a header (by
// identity — independent of whether the request is also signed). It is injected
// by this edition (edition.go) rather than self-registered, so no init() runs and
// an edition that does not want it simply does not inject it.
//
// It resolves credentials itself, from an on-disk profile. That used to happen in
// the shared engine, which loaded a profile before any command ran and handed the
// key and secret down inside config.Config — putting this product's credential
// mechanism, and two fields only it could fill, into every product built on the
// module. Where a credential comes from is the identity's business.
type OpenAPI struct {
	// SelectProfile names the profile to use for this invocation, or "" to use the
	// active one. The edition owns the flag behind it (see edition.go) because the
	// flag is this product's; nil means "always the active profile", which is what
	// a test or a non-CLI caller wants.
	SelectProfile func() string
}

func (OpenAPI) Realm() credential.Realm { return credential.Realm(identity.OpenAPI) }

// Resolve loads the selected profile's credentials.
//
// An explicitly selected profile that cannot be loaded is a hard error: the user
// named it, so failing quietly would run the command against the wrong account.
// A broken or absent ACTIVE profile instead reports "requires API credentials" —
// the same outcome as having none, which is the honest description and keeps the
// hint actionable.
func (o OpenAPI) Resolve(_ config.Config, _ hosts.Env) (credential.Credentials, error) {
	name := ""
	if o.SelectProfile != nil {
		name = o.SelectProfile()
	}
	explicit := name != ""

	if !explicit {
		active, err := profile.Active()
		if err != nil {
			return credential.Credentials{}, missingCredentials()
		}
		name = active
	}
	if name == "" {
		return credential.Credentials{}, missingCredentials()
	}

	p, err := profile.Load(name)
	if err != nil {
		if explicit {
			var e *errs.Error
			if errors.As(err, &e) {
				return credential.Credentials{}, e.WithHint(
					"check the name with `%s profile list`, or run `%s profile set %s ...`", cliname.Name(), cliname.Name(), name)
			}
			return credential.Credentials{}, err
		}
		return credential.Credentials{}, missingCredentials()
	}
	if p.APIKey == "" || p.SecretKey == "" {
		return credential.Credentials{}, missingCredentials()
	}
	return credential.Credentials{APIKey: p.APIKey, Secret: p.SecretKey}, nil
}

func missingCredentials() error {
	return errs.Validation(errs.SubtypeFailedPrecondition,
		"this operation requires API credentials").
		WithHint("ask the user to run `%s profile set <name>` in their own terminal (it prompts; "+
			"no flag, stdin or environment variable supplies a secret instead), then `%s profile use <name>`",
			cliname.Name(), cliname.Name())
}

// ReservedHeaders names the request header this identity injects (the api key),
// so a business Param cannot silently collide with it.
func (OpenAPI) ReservedHeaders() []string { return []string{APIKeyHeader} }

func (OpenAPI) Inject(spec *api.RequestSpec, cred credential.Credentials) error {
	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header[APIKeyHeader] = cred.APIKey
	return nil
}
