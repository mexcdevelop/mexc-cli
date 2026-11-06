// Package assets holds the compile-time-embedded SKILL.md trees at the repo root
// — the one place a //go:embed directive can reach the skills/ subtree (embed
// cannot escape its own package directory). An edition selects its FS via
// Edition.SkillsFS; framework.Run bridges them into the engine's skills command.
//
// The embed seam is the ONE build tag that survives the editions split: //go:embed
// reads files off disk regardless of the import graph, so code pruning (disjoint
// editions) cannot keep internal SKILL.md bytes out of the public binary — only a
// build tag can. Each FS has a real definition behind its tag and an empty stub
// behind the opposite tag, so `go build ./...` still compiles while a tagged build
// carries only its own skills. See scripts/check-boundary.py Invariant 9.
package assets

import "embed"

// CommonSkillsFS embeds mexc-common — the one skill that documents the shared
// layer itself (error envelopes, safety modes, downloads, security) rather than
// any product's business surface.
//
// It is a separate FS from PublicSkillsFS because it belongs to the shared layer,
// not to this edition's skill set: any edition composed against this module can
// mount it beside its own skills. Owning it here, next to the code it describes,
// means a behaviour change updates one copy — a consumer picks the doc up when it
// bumps this module, the same way it picks up the code.
//
//go:embed skills/mexc-common/SKILL.md
var CommonSkillsFS embed.FS

// PublicSkillsFS embeds the SKILL.md of visibility: public subdomains plus the
// public-only shared skill (mexc-auth — API-key credentials). mexc-common is NOT
// here; it lives in CommonSkillsFS above, and the public edition mounts both. This
// must NEVER use a broad glob (e.g. skills/*/*/SKILL.md): //go:embed reads files
// off disk regardless of the import graph, so a wildcard would sweep in whatever
// happens to be on disk rather than what this product deliberately ships. Every
// path below is a listed choice, and check-boundary Invariant 9 asserts the list
// matches the skills on disk exactly.
//
//go:embed skills/mexc-auth/SKILL.md
//go:embed skills/mexc-ws/SKILL.md
//go:embed skills/spot/marketv2/SKILL.md
//go:embed skills/spot/kline/SKILL.md
//go:embed skills/spot/market-search/SKILL.md
//go:embed skills/spot/account/SKILL.md
//go:embed skills/spot/trade/SKILL.md
//go:embed skills/spot/dca/SKILL.md
//go:embed skills/spot/convert/SKILL.md
//go:embed skills/spot/grid/SKILL.md
//go:embed skills/spot/fee/SKILL.md
//go:embed skills/spot/asset/SKILL.md
//go:embed skills/activity/online/SKILL.md
//go:embed skills/activity/reward/SKILL.md
//go:embed skills/activity/participate/SKILL.md
//go:embed skills/contract/market/SKILL.md
//go:embed skills/contract/account/SKILL.md
//go:embed skills/contract/trade/SKILL.md
//go:embed skills/contract/market/references/error-codes.md
//go:embed skills/contract/account/references/error-codes.md
//go:embed skills/contract/trade/references/error-codes.md
//go:embed skills/futures/market/SKILL.md
//go:embed skills/futures/account/SKILL.md
//go:embed skills/financial-openapi/common/SKILL.md
//go:embed skills/financial-openapi/flexible/SKILL.md
//go:embed skills/financial-openapi/fixed/SKILL.md
//go:embed skills/financial-openapi/contract-hold/SKILL.md
var PublicSkillsFS embed.FS
