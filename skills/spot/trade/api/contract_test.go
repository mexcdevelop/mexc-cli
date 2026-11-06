package tradeapi

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/signing"
)

// These tests keep the code declarations and the SKILL.md doc in sync — the spot
// a contributor is most likely to get wrong. Mirrors the marketapi template,
// with the subdomain name changed and extra checks for the write semantics that
// only this read+write subdomain exercises.

// Contract: every Operation declaration is well-formed (ValidateDecl covers
// core fields, enum values, params, and path placeholders), and the ops
// demonstrate the golden pattern: inherit the subdomain defaults (Sign /
// Identity omitted), never restate them per op.
//
// There used to be an explicit exception list here, naming the ops that opted out
// of the subdomain default. There is nothing to except now: every op inherits, so
// a list would have to name all of them — at which point it stops being a guard
// and becomes noise. The rule is back to its simple form, with the
// subdomain-level fact asserted separately below.

// Contract: every operation here is signed, and all of them by the same scheme.
// This is what makes the SKILL.md frontmatter `signed: yes` true.
//
// The assertion is on the scheme, not merely on "something signs", because this
// constant has held three different values — the spot api-key scheme, then none,
// now the gateway scheme — and each change silently rewrote what the frontmatter
// claims. Pinning it means a fourth change has to come here and be argued for.
func TestSubdomain_IsSignedByTheGatewayScheme(t *testing.T) {
	if Signed != signing.CliHMAC {
		t.Errorf("subdomain Signed=%q, want %q: these gateway endpoints verify a "+
			"client signature over the request", Signed, signing.CliHMAC)
	}
	for _, op := range Operations() {
		if got := effectiveSigned(op); got != Signed {
			t.Errorf("op %q resolves to signing scheme %q, want the subdomain default %q",
				op.Name, got, Signed)
		}
	}
}

func TestOps_FieldsValid(t *testing.T) {
	for _, op := range Operations() {
		if err := op.ValidateDecl(); err != nil {
			t.Errorf("op %q: %v", op.Name, err)
		}
		if op.Signed.Concrete() {
			t.Errorf("op %q overrides Sign; ops must inherit the subdomain default", op.Name)
		}
		if op.Identity.Concrete() {
			t.Errorf("op %q overrides Identity; ops must inherit the subdomain default", op.Name)
		}
	}
}

// Contract: an op that carries a request body must resolve to a scheme that can
// sign a body. The assembly layer (internal/cli.validateRegistry, via
// signer.CanSignBody) rejects a query-only scheme paired with a body and the whole
// binary then fails to start, so catch it here — at the subdomain level, where the
// fix belongs — instead of as a cascade of internal/cli test failures.
//
// This is no longer vacuous. Seven body-carrying placements now sit under a signed
// default, so the pairing this guards is live: swapping the subdomain scheme for a
// query-only one (the spot api-key scheme signs the query alone) breaks every
// placement, and it breaks them at startup rather than at call time.
func TestOps_BodyOpsResolveToABodyCapableScheme(t *testing.T) {
	bodyCapable := map[api.Signing]bool{signing.CliHMAC: true}
	for _, op := range Operations() {
		if op.Body == nil {
			continue
		}
		scheme := effectiveSigned(op)
		if scheme.Signs() && !bodyCapable[scheme] {
			t.Errorf("op %q carries a request body but resolves to signing scheme %q, "+
				"which does not cover a body; assembly rejects that pairing", op.Name, scheme)
		}
	}
}

// effectiveSigned mirrors the assembly-layer inheritance for this subdomain: a
// concrete op-level value overrides, the zero value inherits the Sign constant.
func effectiveSigned(op api.Operation) api.Signing {
	if op.Signed.Concrete() {
		return op.Signed
	}
	return Signed
}

// Contract: trade contains both reads and writes — assert it actually has at
// least one of each, so this stays the "mixed" sample it claims to be.
func TestOps_HasReadsAndWrites(t *testing.T) {
	var reads, writes int
	for _, op := range Operations() {
		if op.IsWrite() {
			writes++
		} else {
			reads++
		}
	}
	if reads == 0 || writes == 0 {
		t.Fatalf("trade must mix reads and writes, got reads=%d writes=%d", reads, writes)
	}
}

// Contract: the write semantics that the retry layer and MCP hints depend on.
// Every placement must be non-idempotent (never auto-retried) and
// non-destructive; cancel must be idempotent and destructive; modify
// is the one placement-adjacent write that is BOTH destructive and
// non-idempotent, because it is a cancel-and-replace — that pairing is what
// grades it TierHeavy in the write-confirmation flow and what SKILL.md warns
// about, so it is pinned here rather than left to the defaults by accident.
func TestOps_WriteSemantics(t *testing.T) {
	want := map[string]struct {
		idempotent  bool
		destructive bool
	}{
		"place-oco":            {idempotent: false, destructive: false},
		"place-trigger":        {idempotent: false, destructive: false},
		"place-market-trigger": {idempotent: false, destructive: false},
		"place-track":          {idempotent: false, destructive: false},
		"place":                {idempotent: false, destructive: false},
		"place-market":         {idempotent: false, destructive: false},
		"modify":               {idempotent: false, destructive: true},
		"cancel":               {idempotent: true, destructive: true},
	}
	byName := map[string]api.Operation{}
	for _, op := range Operations() {
		byName[op.Name] = op
	}
	for name, w := range want {
		op, ok := byName[name]
		if !ok {
			t.Errorf("expected write op %q to exist", name)
			continue
		}
		if op.IdempotentHint() != w.idempotent {
			t.Errorf("op %q IdempotentHint=%v, want %v", name, op.IdempotentHint(), w.idempotent)
		}
		if op.DestructiveHint() != w.destructive {
			t.Errorf("op %q DestructiveHint=%v, want %v", name, op.DestructiveHint(), w.destructive)
		}
	}
}

// dedupeKeyField is the ONE body field that may present itself as a dedupe key:
// the backend dedupes a placement on clientOrderId + member.
const dedupeKeyField = "clientOrderId"

// Contract: clientOrderId is the only body field that claims to dedupe, and it
// always claims it.
//
// Both directions have gone wrong here, in opposite ways, and both mislead an agent
// at the same moment — the ambiguous timeout, where the only decision is "resend or
// re-query".
//
// The false positive: place-market-trigger exposed the DTO's `nonce` field as a
// "client dedupe nonce". Nothing dedupes on it — the same nonce sent twice came back
// as two successful placements with two different orderIds — so an agent that
// believed it resent a placement and got a second live order. Hence a field that is
// not clientOrderId may not describe itself as a dedupe or idempotency key, and the
// invented names carrying the same suggestion are refused outright.
//
// The false negative: the doc used to say clientOrderId "is not used by the
// business, so placements have no idempotency key", which steered agents away from
// the one field that does protect them — leaving them unable to prevent a duplicate
// and told never to resend, i.e. with no recovery at all. Hence clientOrderId must
// SAY what it is. A description that goes quiet about the property is exactly how
// the fact got lost the first time.
//
// The desc is checked and not just the name, because that is the half that reaches
// an agent as prose: the name lands in `schema` and in the MCP input schema, the
// desc in the tool description. Nested fields are walked too.
//
// Note what this does NOT assert: that a placement is idempotent. It is not, and
// TestOps_WriteSemantics pins that. The key is OPTIONAL, so the transport cannot
// know whether a given call carried one and must never auto-retry a placement.
func TestBodies_DedupeKeyIsClientOrderIDOnly(t *testing.T) {
	// Names that announce a dedupe key without being the real one. `nonce` is the
	// one that shipped; the rest are the plausible spellings of the same mistake.
	bannedNames := []string{"nonce", "idempotencykey", "requestid", "dedupekey"}
	// Prefixes, so one entry covers a family: "dedup" catches dedupe / dedupes /
	// deduplication, "idempot" catches idempotent / idempotency.
	claimWords := []string{"dedup", "idempot"}
	claimsDedupe := func(desc string) bool {
		desc = strings.ToLower(desc)
		for _, w := range claimWords {
			if strings.Contains(desc, w) {
				return true
			}
		}
		return false
	}

	var walk func(op string, path string, fields []api.BodyField)
	walk = func(op, path string, fields []api.BodyField) {
		for _, f := range fields {
			name := strings.ToLower(strings.ReplaceAll(f.Name, "_", ""))
			for _, banned := range bannedNames {
				if name == banned {
					t.Errorf("op %q declares body field %q%s: %s is the only dedupe key here, and a "+
						"field suggesting a second one tells an agent it may resend a placement "+
						"that is not deduped", op, f.Name, path, dedupeKeyField)
				}
			}
			switch {
			case f.Name == dedupeKeyField && !claimsDedupe(f.Desc):
				t.Errorf("op %q: %s must state that it is the dedupe key, got desc %q — a silent "+
					"description is how the doc came to tell agents not to send it",
					op, dedupeKeyField, f.Desc)
			case f.Name != dedupeKeyField && claimsDedupe(f.Desc):
				t.Errorf("op %q: body field %q%s describes itself as %q, but only %s dedupes; "+
					"nothing else here may promise it", op, f.Name, path, f.Desc, dedupeKeyField)
			}
			if len(f.Fields) > 0 {
				walk(op, path+" (under "+f.Name+")", f.Fields)
			}
		}
	}
	for _, op := range Operations() {
		if op.Body == nil {
			continue
		}
		walk(op.Name, "", op.BodyFields())
	}
}

// Contract: cancel's Summary spells out the account-wide mode, and every
// selector stays optional.
//
// The two facts belong together. All four modes are chosen by which params are
// PRESENT, and the widest one is selected by supplying NOTHING — which is why
// every param is optional, and which also means the confirmation prompt for
// "cancel this order" and the one for "empty the account" differ only in that the
// second lists no params. The Summary is the only line the prompt shows on its
// own (confirmDisplay titles the prompt with it), so it is the only place that can
// tell the user the account-wide form exists before they approve one.
//
// This is the ceiling of what a declaration can do here: it cannot say which mode
// THIS call selected, so a param dropped on a retry still widens the reach with
// nothing in the prompt to mark it. SKILL.md carries that warning for the agent.
func TestCancel_SummarySpellsOutTheAccountWideMode(t *testing.T) {
	var cancel api.Operation
	for _, op := range Operations() {
		if op.Name == "cancel" {
			cancel = op
		}
	}
	if cancel.Name == "" {
		t.Fatal("spot.trade must declare a cancel op")
	}

	for _, want := range []string{"no parameters", "every open order"} {
		if !strings.Contains(cancel.Summary, want) {
			t.Errorf("cancel's summary must mention %q — it is the only line the "+
				"confirmation prompt shows on its own: %q", want, cancel.Summary)
		}
	}
	for _, p := range cancel.Params {
		if p.Required {
			t.Errorf("cancel --%s must stay optional: all four modes are selected by "+
				"which params are present", p.Name)
		}
	}
}

// Contract: place-track's trackAmplitude is guarded on the one axis a declaration
// can guard — the local range — and its desc carries the conversion for the
// mistake the range cannot catch.
//
// The field is a FRACTION, so 5% is 0.05 and 0.5 is 50%. Two different mistakes
// hide behind that, and only one is a range error: 5 and 50 are outside (0,1] and
// are refused before a round trip, but 0.5 is a value the backend accepts — the
// order rests, looks normal, and triggers ten times later than the user intended.
// Nothing catches that second one at runtime: the confirmation prompt echoes the
// raw fraction back, which is exactly what a caller who meant 5% expects to see.
// So the desc has to state the conversion, and it has to reach the caller through
// `schema` / --help BEFORE the call, not after.
func TestPlaceTrack_AmplitudeRangeAndDesc(t *testing.T) {
	var track api.Operation
	for _, op := range Operations() {
		if op.Name == "place-track" {
			track = op
		}
	}
	if track.Name == "" {
		t.Fatal("spot.trade must declare a place-track op")
	}

	// (a) The declared range: what the pattern must refuse, and what it must pass.
	full := func(amp string) string {
		return `{"currency":"BTC","market":"USDT","tradeType":"SELL","orderType":"TRACK_MARKET_ORDER",` +
			`"orderSource":"CLI","quantity":"0.001","trackAmplitude":"` + amp + `"}`
	}
	for _, c := range []struct {
		amp   string
		valid bool
	}{
		{"0.05", true},   // 5% — the everyday case
		{"0.5", true},    // 50% — legal, and the dangerous one; the prompt handles it
		{"1", true},      // the inclusive upper bound
		{"1.0", true},    // same bound, spelled with decimals
		{"0.0001", true}, // the smallest value that survives 4-decimal truncation
		{"0.1234", true},
		{"5", false},       // percent-shaped: the mistake the range CAN catch
		{"50", false},      // ditto
		{"0", false},       // excluded lower bound
		{"0.0", false},     // same, spelled with a decimal
		{"0.0000", false},  // truncates to zero
		{"0.00001", false}, // 5 decimals: truncates to zero
		{"1.0001", false},  // above the bound
		{"1.5", false},
		{"-0.5", false},
	} {
		err := track.ValidateBody(full(c.amp))
		if c.valid && err != nil {
			t.Errorf("trackAmplitude %q must be accepted, got %v", c.amp, err)
		}
		if !c.valid && err == nil {
			t.Errorf("trackAmplitude %q must be refused locally", c.amp)
		}
	}

	// (b) The desc: it must name the unit and give the conversion in both
	// directions, because a caller who reads only the range learns nothing about
	// what 0.5 means. This is the field's last line of defence.
	var desc string
	for _, f := range track.BodyFields() {
		if f.Name == "trackAmplitude" {
			desc = f.Desc
		}
	}
	if desc == "" {
		t.Fatal("trackAmplitude must carry a desc: it is the only place the fraction is explained")
	}
	if !strings.Contains(desc, "FRACTION") {
		t.Errorf("trackAmplitude desc must say the value is a fraction, not a percent: %q", desc)
	}
	for _, want := range []string{"0.05 = 5%", "0.5 = 50%"} {
		if !strings.Contains(desc, want) {
			t.Errorf("trackAmplitude desc must spell out %q — the two values one keystroke "+
				"apart are what gets confused: %q", want, desc)
		}
	}
}

// Contract: the OrderType code -> name table is identical in all three places it
// appears — the declared Enum (names), deal-detail's parameter help, and the
// batch-filter documentation in SKILL.md.
//
// The table is duplicated on purpose: an agent that hits "--orderType value \"1\"
// is not allowed" needs the translation on the parameter (schema / --help), and a
// reader planning a batch cancel needs it in the doc. Duplication without a check
// is how the doc came to send callers down a path that could not work, so the
// copies are pinned to each other and to the backend enum here.
//
// The integers are pinned as literals rather than derived: they come from the
// backend's OrderType enum, which this repository cannot read, so a change to a
// number must be a deliberate edit here and not a silent follow-along.
func TestOrderTypeCodeTable_MatchesEnumAndSkillDoc(t *testing.T) {
	type entry struct {
		code int
		name string
	}
	// The backend OrderType enum, in its declaration order.
	want := []entry{
		{-1, "TRIGGER_ORDER"}, {1, "LIMIT_ORDER"}, {2, "POST_ONLY"},
		{3, "IMMEDIATE_OR_CANCEL"}, {4, "FILL_OR_KILL"}, {5, "MARKET_ORDER"},
		{100, "STOP_LIMIT_ORDER"}, {101, "OCO_STOP_LIMIT_ORDER"},
		{102, "OCO_LIMIT_ORDER"}, {103, "STOP_MARKET_ORDER"},
		{104, "TRACK_LIMIT_ORDER"}, {105, "TRACK_MARKET_ORDER"},
	}

	// (a) The declared Enum carries exactly these names, in the same order — it is
	// what deal-detail accepts, so a missing name is an operation that cannot be
	// called for that order type.
	if len(orderTypeValues) != len(want) {
		t.Fatalf("orderTypeValues has %d names, the enum has %d", len(orderTypeValues), len(want))
	}
	for i, w := range want {
		if orderTypeValues[i] != w.name {
			t.Errorf("orderTypeValues[%d] = %q, want %q", i, orderTypeValues[i], w.name)
		}
	}

	// (b) The constant behind deal-detail's parameter help.
	pairRe := regexp.MustCompile(`(-?\d+) ([A-Z_]+)`)
	got := pairRe.FindAllStringSubmatch(orderTypeCodeTable, -1)
	if len(got) != len(want) {
		t.Fatalf("orderTypeCodeTable has %d pairs, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i][1] != strconv.Itoa(w.code) || got[i][2] != w.name {
			t.Errorf("orderTypeCodeTable pair %d = %s %s, want %d %s", i, got[i][1], got[i][2], w.code, w.name)
		}
	}

	// (c) deal-detail's help must actually carry it, and must say which spelling it
	// wants — the whole failure was a caller passing the code in good faith.
	var help string
	for _, op := range Operations() {
		if op.Name != "deal-detail" {
			continue
		}
		for _, p := range op.Params {
			if p.Name == "orderType" {
				help = p.Desc
			}
		}
	}
	if help == "" {
		t.Fatal("deal-detail must declare an orderType param")
	}
	if !strings.Contains(help, orderTypeCodeTable) {
		t.Error("deal-detail's orderType help must carry the code -> name table")
	}
	if !strings.Contains(help, "NAME") || !strings.Contains(help, "NOT the integer code") {
		t.Errorf("orderType help must say it takes the NAME, not the row's code: %q", help)
	}

	// (d) The SKILL.md batch-filter line must hold the same table.
	doc, err := os.ReadFile("../SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	var line string
	for _, l := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(l, "- `orderTypes`: comma-separated") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("SKILL.md must document the orderTypes code list")
	}
	docPairs := regexp.MustCompile("`(-?\\d+)` ([A-Z_]+)").FindAllStringSubmatch(line, -1)
	if len(docPairs) != len(want) {
		t.Fatalf("SKILL.md code table has %d pairs, want %d", len(docPairs), len(want))
	}
	for i, w := range want {
		if docPairs[i][1] != strconv.Itoa(w.code) || docPairs[i][2] != w.name {
			t.Errorf("SKILL.md pair %d = %s %s, want %d %s", i, docPairs[i][1], docPairs[i][2], w.code, w.name)
		}
	}
}

// Contract: an enum this subdomain accepts by NAME, whose sibling operations
// spell the same axis as an integer CODE, must say so in its Desc.
//
// The pairing is real and silent: current-orders / history-orders / cancel take
// tradeType and orderTypes as codes (1, 2, 104) while deals and deal-detail take
// the names, so "read the type off a listing row and pass it on" is refused every
// time — and it is a systematic mistake, not a typo, so it repeats.
//
// The Desc is where it has to be explained, because the Desc is the only text the
// framework can put in front of the caller at the moment of failure: a rejected
// enum value is reported with the allowed set plus this Desc
// (api.Operation.Validate). The framework itself cannot diagnose the mistake —
// naming a code table there would put a product's vocabulary in the shared core
// (check-boundary Invariant 16) — so the guarantee lives here, in the layer that
// owns the vocabulary.
func TestNameSpelledEnums_DescWarnsAboutTheCodeSpelling(t *testing.T) {
	// Listed, not detected: which axes this subdomain spells two ways is its own
	// knowledge, not something to infer from the shape of a value.
	byName := map[string]string{
		"deals":       "tradeType",
		"deal-detail": "orderType",
	}
	for opName, paramName := range byName {
		var p api.Param
		var found bool
		for _, op := range Operations() {
			if op.Name != opName {
				continue
			}
			for _, cand := range op.Params {
				if cand.Name == paramName {
					p, found = cand, true
				}
			}
		}
		if !found {
			t.Errorf("op %q must declare a %q param", opName, paramName)
			continue
		}
		if len(p.Enum) == 0 {
			t.Errorf("%s --%s must declare its allowed names as an Enum", opName, paramName)
		}
		if !strings.Contains(p.Desc, "NAME") {
			t.Errorf("%s --%s help must say it takes the NAME: %q", opName, paramName, p.Desc)
		}
		if !strings.Contains(strings.ToLower(p.Desc), "code") {
			t.Errorf("%s --%s help must warn that the listings spell this as a code: %q",
				opName, paramName, p.Desc)
		}
	}
}

// Contract: the SKILL.md operation table and the code Operations() agree on
// every field, both ways. Guards against doc/code drift on name, method, path,
// access, and signed — what makes the Operation the single source of truth.
func TestOps_MatchSkillTable(t *testing.T) {
	doc, err := os.ReadFile("../SKILL.md")
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(doc)

	if got := frontmatterField(content, "signed"); got != boolStr(Signed != api.SignNone) {
		t.Errorf("SKILL.md frontmatter signed=%q but subdomain Sign=%q", got, Signed)
	}
	if got := frontmatterField(content, "identity"); got != boolStr(Identity != api.IdentityNone) {
		t.Errorf("SKILL.md frontmatter identity=%q but code Identity=%q", got, Identity)
	}
	if got := frontmatterField(content, "scope"); got != Scope {
		t.Errorf("SKILL.md frontmatter scope=%q but code Scope=%q", got, Scope)
	}

	docRows := parseSkillTable(content)
	codeOps := map[string]api.Operation{}
	for _, op := range Operations() {
		codeOps[op.Name] = op
	}

	for name := range codeOps {
		if _, ok := docRows[name]; !ok {
			t.Errorf("op %q is in code but not in the SKILL.md operation table", name)
		}
	}
	for name, row := range docRows {
		op, ok := codeOps[name]
		if !ok {
			t.Errorf("op %q is in SKILL.md but has no matching Operation in code", name)
			continue
		}
		if row.method != op.Method {
			t.Errorf("op %q: SKILL.md method %q != code %q", name, row.method, op.Method)
		}
		if row.path != op.Path {
			t.Errorf("op %q: SKILL.md path %q != code %q", name, row.path, op.Path)
		}
		if row.access != string(op.Access) {
			t.Errorf("op %q: SKILL.md access %q != code %q", name, row.access, op.Access)
		}
		if want := effectiveSigned(op) != api.SignNone; row.signed != want {
			t.Errorf("op %q: SKILL.md signed=%v != effective sign %q", name, row.signed, effectiveSigned(op))
		}
		if want := Identity != api.IdentityNone; row.identity != want {
			t.Errorf("op %q: SKILL.md identity=%v != code %v", name, row.identity, want)
		}
		if row.idempotent != op.IdempotentHint() {
			t.Errorf("op %q: SKILL.md idempotent=%v != code %v", name, row.idempotent, op.IdempotentHint())
		}
	}
}

// skillRow is one parsed operation-table row.
type skillRow struct {
	method     string
	path       string
	access     string
	signed     bool
	identity   bool
	idempotent bool
}

var (
	skillRowRe = regexp.MustCompile("`mexc spot trade ([a-z0-9-]+)`")
	apiCellRe  = regexp.MustCompile("`([A-Z]+) (/[^`]+)`")
	fmFieldRe  = regexp.MustCompile(`(?m)^([a-z]+):\s*(.+?)\s*$`)
)

// parseSkillTable reads the operation table into name -> {method,path,access,signed,identity,idempotent}.
// Column layout: `Command | scope | access | signed | identity | idempotent | Endpoint | Description`.
func parseSkillTable(content string) map[string]skillRow {
	rows := map[string]skillRow{}
	yes := func(col string) bool { v := strings.TrimSpace(col); return v == "yes" || v == "true" }
	for _, line := range strings.Split(content, "\n") {
		m := skillRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		cols := strings.Split(line, "|")
		if len(cols) < 8 {
			continue
		}
		row := skillRow{
			access:     strings.TrimSpace(cols[3]),
			signed:     yes(cols[4]),
			identity:   yes(cols[5]),
			idempotent: yes(cols[6]),
		}
		if api := apiCellRe.FindStringSubmatch(cols[7]); api != nil {
			row.method = api[1]
			row.path = api[2]
		}
		rows[name] = row
	}
	return rows
}

func frontmatterField(content, key string) string {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return ""
	}
	for _, m := range fmFieldRe.FindAllStringSubmatch(parts[1], -1) {
		if m[1] == key {
			return m[2]
		}
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
