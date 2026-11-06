// Package rewardapi declares the operations of the activity.reward sub-domain
// (unified reward center: a logged-in user's reward summary, paged reward
// detail, coupon list, and the reward-center activity-type list).
//
// All endpoints require a logged-in user: the caller is authenticated by the API
// key it carries, and its own account is resolved upstream rather than sent as a
// parameter. So the sub-domain default carries the openapi identity and the CLI
// gateway HMAC signing scheme, and every operation inherits it.
//
// These endpoints are served under the "/api/activity/openapi" prefix — which is
// why every Path below carries it. The resource is always activity.reward; all
// operations are reads.
package rewardapi

import (
	"github.com/mexcdevelop/mexc-cli/identity"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// --- Request body DTOs (the single source of truth for the body face) ---

// summaryReq is the JSON body of activity.reward.summary.
//
// Both bounds are required, which is stricter than the backend contract on
// purpose. The backend fills a last-30-days default only when BOTH are absent;
// a one-sided window is passed straight through unguarded, so the missing bound
// makes the filter match nothing and the query returns zero rows. The
// caller then gets a success response holding an empty summary that is
// indistinguishable from "this member has no rewards" — a silent wrong answer.
// Requiring both is what lets the CLI reject that locally instead. The default
// window is still reachable by passing it explicitly (now-30d .. now), and doing
// so is what makes a summary and its per-category drill-down reconcile, since
// the response does not echo the window back.
type summaryReq struct {
	StartTime int64 `json:"startTime" validate:"required" desc:"Start timestamp in ms; required, and must be sent together with endTime"`
	EndTime   int64 `json:"endTime" validate:"required" desc:"End timestamp in ms; required, and must be sent together with startTime"`
}

// rewardDetailReq is the JSON body of activity.reward.reward-detail. Paginated
// via page/pageSize. rewardCategory selects the tab and is required. Both time
// bounds are required for the same reason as on summary — see summaryReq.
type rewardDetailReq struct {
	Page           *int64   `json:"page,omitempty" minval:"1" desc:"Page number (1-based; defaults to 1)"`
	PageSize       *int64   `json:"pageSize,omitempty" minval:"1" maxval:"100" desc:"Page size, 1-100 (defaults to 20)"`
	RewardCategory string   `json:"rewardCategory" validate:"required" enum:"CONTRACT_REWARD,ASSET_REWARD,COUPON_REWARD" desc:"Reward tab: CONTRACT_REWARD=trial/bonus, ASSET_REWARD=token, COUPON_REWARD=coupon"`
	StartTime      int64    `json:"startTime" validate:"required" desc:"Start timestamp in ms; required, and must be sent together with endTime"`
	EndTime        int64    `json:"endTime" validate:"required" desc:"End timestamp in ms; required, and must be sent together with startTime"`
	ActivityTypes  []string `json:"activityTypes,omitempty" desc:"Activity type filter (multi-select; omit for all)"`
	ActivityName   string   `json:"activityName,omitempty" desc:"Activity name fuzzy match (i18n)"`
	UsageState     string   `json:"usageState,omitempty" enum:"UNUSED,USED,EXPIRED" desc:"Usage state (CONTRACT_REWARD / COUPON_REWARD only)"`
}

// couponListReq is the JSON body of activity.reward.coupon-list. memberId /
// language are session/header-driven and omitted. Both time bounds are required
// for the same reason as on summary — see summaryReq; the same unguarded filter
// applies to this operation's own time window.
type couponListReq struct {
	Page         *int64   `json:"page,omitempty" minval:"1" desc:"Page number (1-based; defaults to 1)"`
	PageSize     *int64   `json:"pageSize,omitempty" minval:"1" maxval:"100" desc:"Page size, 1-100 (defaults to 20)"`
	DisplayState string   `json:"displayState,omitempty" enum:"UNUSED,USED,EXPIRED" desc:"Coupon display state"`
	CouponTypes  []string `json:"couponTypes,omitempty" desc:"Coupon types (multi-select)"`
	ActivityName string   `json:"activityName,omitempty" desc:"Activity name fuzzy match"`
	StartTime    int64    `json:"startTime" validate:"required" desc:"Start timestamp in ms; required, and must be sent together with endTime"`
	EndTime      int64    `json:"endTime" validate:"required" desc:"End timestamp in ms; required, and must be sent together with startTime"`
}

// --- Operations ---

// Operations returns all operations of this sub-domain.
func Operations() []api.Operation {
	return []api.Operation{
		{
			Name: "summary", Method: "POST", Path: "/api/activity/openapi/reward_center/summary",
			Access:  api.Read,
			Summary: "User reward summary across trial / token / coupon categories",
			Body:    summaryReq{},
		},
		{
			Name: "reward-detail", Method: "POST", Path: "/api/activity/openapi/reward_center/reward_detail",
			Access:  api.Read,
			Summary: "Paginated user reward detail for one reward category",
			Body:    rewardDetailReq{},
		},
		{
			Name: "coupon-list", Method: "POST", Path: "/api/activity/openapi/reward_center/coupon_list",
			Access:  api.Read,
			Summary: "Paginated user coupon list",
			Body:    couponListReq{},
		},
		{
			Name: "reward-activity-type", Method: "GET", Path: "/api/activity/openapi/reward_center/reward_activity_type",
			Access:  api.Read,
			Summary: "List activity types that have joined the reward center",
		},
	}
}

// Scope is the resource identifier of this sub-domain (matches SKILL.md frontmatter).
const Scope = "activity.reward"

// Signed is this sub-domain's default signing scheme: the reward-center openapi
// endpoints are API-key authenticated and signed with the CLI gateway HMAC
// scheme. Every operation inherits it.
const Signed = signing.CliHMAC

// Identity is this sub-domain's default identity: openapi — the request carries
// the API key (verified via the Signed signature), forwarded by the gateway as
// the member. Every operation inherits it.
const Identity = identity.OpenAPI
