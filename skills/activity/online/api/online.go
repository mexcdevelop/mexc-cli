// Package onlineapi declares the operations of the activity.online sub-domain
// (activity discovery: the online activity list, banner list and category
// tabs).
//
// These endpoints authenticate the caller by the API key they carry, so the
// sub-domain default carries the openapi identity and the CLI gateway HMAC
// signing scheme. They are served under the "/api/activity/openapi" prefix —
// which is why every Path below carries it. The resource is always
// activity.online; all operations are reads.
package onlineapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// --- Request body DTOs (the single source of truth for the body face) ---

// listReq is the JSON body of activity.online.list. memberId / language / ip are
// resolved upstream from the API key, so they are not caller inputs and are
// intentionally omitted here.
type listReq struct {
	Limit          int    `json:"limit" validate:"required" minval:"1" maxval:"15" desc:"Page size, 1-15"`
	LastRowNumber  *int64 `json:"lastRowNumber,omitempty" desc:"Row-number cursor: last row number of the previous page (omit for first page)"`
	RecommendTap   string `json:"recommendTap,omitempty" enum:"CONTRACT_TAP,SPOT_TAP,INVITE_TAP,FINANCE_TAP,FIAT_TAP,REGION_TAP,DEX_PLUS_TAP" desc:"Category tab filter"`
	ActivityStatus string `json:"activityStatus,omitempty" enum:"PREHEATING,UNDERWAY,SETTLEMENT,FINISHED,INVALID" desc:"Activity status filter"`
	LimitApplied   *bool  `json:"limitApplied,omitempty" desc:"Limit to activities the user has joined (My Activities)"`
}

// bannerListReq is the JSON body of activity.online.banner-list.
type bannerListReq struct {
	Limit         int    `json:"limit" validate:"required" minval:"1" maxval:"10" desc:"Page size, 1-10"`
	LastRowNumber *int64 `json:"lastRowNumber,omitempty" desc:"Row-number cursor: last row number of the previous page (omit for first page)"`
}

// --- Operations ---

// Operations returns all operations of this sub-domain.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "list", Method: "POST", Path: "/api/activity/openapi/activity_center_online/list",
			Access:  api.Read,
			Summary: "List user-visible online activities (cursor paginated)",
			Body:    listReq{},
		},
		{
			Name: "banner-list", Method: "POST", Path: "/api/activity/openapi/activity_center_online/banner_list",
			Access:  api.Read,
			Summary: "List header banner activities (cursor paginated)",
			Body:    bannerListReq{},
		},
		{
			Name: "category-list", Method: "GET", Path: "/api/activity/openapi/activity_center_online/category_list",
			Access:  api.Read,
			Summary: "List activity category tabs",
		},
	}
}

// Scope is the resource identifier of this sub-domain (matches SKILL.md frontmatter).
const Scope = "activity.online"

// Signed is this sub-domain's default signing scheme: the activity-center
// openapi endpoints are API-key authenticated and signed with the CLI gateway
// HMAC scheme.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default identity: openapi — the request carries
// the API key, covered by the Signed signature, and that is what identifies the
// caller.
const Identity = identity.OpenAPI
