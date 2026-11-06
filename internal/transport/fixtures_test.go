package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/config"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
	"github.com/mexcdevelop/mexc-cli/pkg/hosts"
	"github.com/mexcdevelop/mexc-cli/pkg/signer"
)

// testQueryScheme is a scheme name this package invents for its own signer
// double, never a product's — the openapi identity provider and any product's
// signer live in editions/public and are not self-registered via init(), so
// the transport tests need their own of each to exercise the signed-openapi
// send path.
const testQueryScheme api.Signing = "test-query-hmac"

// The signer does a real HMAC (computed locally) so TestSend_SignedOpenAPI can
// assert a real, non-placeholder signature.
func init() {
	credential.Register(testOpenAPIProvider{})
	signer.Register(testQuerySigner{})
}

const testAPIKeyHeader = "X-MEXC-APIKEY"

type testOpenAPIProvider struct{}

func (testOpenAPIProvider) Realm() credential.Realm { return credential.Realm(identity.OpenAPI) }

// Resolve returns fixed credentials: a provider now holds its own, rather than
// reading them off Config, which no longer carries any.
func (testOpenAPIProvider) Resolve(config.Config, hosts.Env) (credential.Credentials, error) {
	return credential.Credentials{APIKey: "k", Secret: "s"}, nil
}
func (testOpenAPIProvider) Inject(spec *api.RequestSpec, cred credential.Credentials) error {
	if spec.Header == nil {
		spec.Header = map[string]string{}
	}
	spec.Header[testAPIKeyHeader] = cred.APIKey
	return nil
}

// testHMACHex mirrors what a query-signing scheme does, so this package can
// exercise the signing hook without importing a product's scheme.
func testHMACHex(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

type testQuerySigner struct{}

func (testQuerySigner) Scheme() api.Signing { return testQueryScheme }
func (testQuerySigner) Sign(spec *api.RequestSpec, cred credential.Credentials, now time.Time) error {
	if spec.Query == nil {
		spec.Query = map[string]string{}
	}
	spec.Query["timestamp"] = strconv.FormatInt(now.UnixMilli(), 10)
	spec.Query["signature"] = testHMACHex(cred.Secret, api.CanonicalQuery(spec.Query))
	return nil
}
