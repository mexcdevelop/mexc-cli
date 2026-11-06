// Package participateapi declares the operations of the activity.participate
// sub-domain (a logged-in user registers for an activity).
//
// The endpoint authenticates the caller by the API key it carries and takes a
// JSON body, so the sub-domain carries the openapi identity and the CLI gateway
// HMAC signing scheme. The caller's own account is resolved upstream and is never
// a body field. Served under the "/api/activity/openapi" prefix — which is why
// the Path below carries it. The resource is activity.participate.
package participateapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// --- Request body DTOs (the single source of truth for the body face) ---

// registerReq is the JSON body of activity.participate.register. The member is
// taken from the session, so linkFlag (the activity link identifier) is the only
// caller input.
type registerReq struct {
	LinkFlag string `json:"linkFlag" validate:"required" desc:"Activity link identifier to register for"`
}

// --- Operations ---

// Operations returns all operations of this sub-domain.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "register", Method: "POST", Path: "/api/activity/openapi/lego/register",
			Access: api.Write,
			// Registering adds a record without unmaking anything.
			Destructive: api.Ptr(false),
			Summary:     "Register the logged-in user for an activity",
			Body:        registerReq{},
		},
	}
}

// Scope is the resource identifier of this sub-domain (matches SKILL.md frontmatter).
const Scope = "activity.participate"

// Signed is this sub-domain's default signing scheme: the register openapi
// endpoint is API-key authenticated and signed with the CLI gateway HMAC
// scheme.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), forwarded by the gateway as
// the member.
const Identity = identity.OpenAPI
