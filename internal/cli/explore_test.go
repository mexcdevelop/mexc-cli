package cli

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
)

func TestExplore_TreeMatchesSchemaList(t *testing.T) {
	tree := buildTree(domains())
	got := flattenOpIDs(tree)
	want := allOperationIDs(domains()) // source of truth in schema.go
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("explore tree drifted from schema list:\n got: %v\nwant: %v", got, want)
	}
}

func TestExplore_NodeCarriesSchema(t *testing.T) {
	tree := buildTree(domains())
	if len(tree) == 0 {
		t.Fatal("expected at least the fixture domain")
	}
	// The first op of the first sub of the first domain must carry its computed schema.
	first := tree[0].subs[0].ops[0]
	wantID := tree[0].name + "." + tree[0].subs[0].name + "." + first.op.Name
	if first.id != wantID {
		t.Fatalf("node id = %q, want %q", first.id, wantID)
	}
	if first.schema.Operation != wantID {
		t.Fatalf("schema.Operation = %q, want %q", first.schema.Operation, wantID)
	}
	if first.schema.Method != first.op.Method {
		t.Fatalf("schema.Method = %q, want %q", first.schema.Method, first.op.Method)
	}
}

func TestExplore_CommandSkeleton(t *testing.T) {
	tree := buildTree(domains())
	// Pick an op with params to assert the skeleton format; fall back to the first op.
	n := tree[0].subs[0].ops[0]
	got := commandSkeleton(n)
	// Shaped like: mexc-cli sample public <op> [--<param> <...>]
	want := "mexc-cli " + tree[0].name + " " + tree[0].subs[0].name + " " + n.op.Name
	if !strings.HasPrefix(got, want) {
		t.Fatalf("skeleton = %q, want prefix %q", got, want)
	}
	for _, p := range n.op.Params {
		if !strings.Contains(got, "--"+p.Name) {
			t.Fatalf("skeleton %q missing flag --%s", got, p.Name)
		}
	}
}

func TestExplore_NavigateExpandsDomain(t *testing.T) {
	m := newExploreModel(buildTree(domains()))
	// Initially only domain rows are visible (all collapsed).
	before := len(m.visibleRows())
	// Press l to expand the current (first) domain.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(exploreModel)
	after := len(m.visibleRows())
	if after <= before {
		t.Fatalf("expanding domain should reveal subdomains: before=%d after=%d", before, after)
	}
}

func TestExplore_WriteOpFlaggedDanger(t *testing.T) {
	// The trade domain has write ops; ensure at least one op is flagged danger.
	tree := buildTree(domains())
	var sawWrite bool
	for _, d := range tree {
		for _, s := range d.subs {
			for _, o := range s.ops {
				if o.op.IsWrite() {
					sawWrite = true
					if !isDanger(o) {
						t.Fatalf("write op %q not flagged danger", o.id)
					}
				}
			}
		}
	}
	if !sawWrite {
		t.Skip("no write op in current registry; skipping danger assertion")
	}
}

// TestExplore_CursorClampsAfterCollapse reproduces the bug where collapsing a
// domain while cursor is on a deep child row leaves cursor out-of-bounds.
//
// Scenario: expand domain → move cursor to last visible row → navigate back to
// domain row → set cursor artificially to last (to force the out-of-bounds
// condition) → press 'h' to collapse → cursor must be within [0, len-1].
func TestExplore_CursorClampsAfterCollapse(t *testing.T) {
	m := newExploreModel(buildTree(domains()))

	// Expand first domain so we have multiple visible rows.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(exploreModel)

	rowsBefore := len(m.visibleRows())
	if rowsBefore <= 1 {
		t.Skip("first domain has no children; cannot test collapse clamp")
	}

	// Place cursor at the last visible row (deepest child).
	m.cursor = rowsBefore - 1

	// Move cursor back to the domain row (index 0) via 'k' presses so that
	// pressing 'h' will actually collapse the domain.
	for m.cursor > 0 {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		m = updated.(exploreModel)
	}
	if m.visibleRows()[0].kind != rowDomain {
		t.Fatal("expected cursor to land on a domain row at index 0")
	}

	// Reproduce the bug: forcibly set cursor back to the last row while still
	// pointing at the domain header internally. This mimics the user having
	// navigated down and then pressed 'h' on the domain row.
	m.cursor = rowsBefore - 1

	// Press 'h': collapseCurrent sees cursor=rowsBefore-1 which is a child row,
	// so it tries to collapse it (no-op for rowOp/rowSub), but without the fix
	// the clamp never runs and cursor stays out-of-bounds post-collapse.
	// With the fix the clamp runs after all key handling.
	//
	// To hit the true domain-collapse code path, navigate cursor to 0 first,
	// then artificially bump it to last, then send 'h'.  collapseCurrent reads
	// rows[cursor] which is the child row — it is a no-op collapse.
	// For the actual shrink path, we need cursor ON the domain row.
	m.cursor = 0 // on domain row
	// Expand again (already expanded; no-op) and move cursor to last.
	m.cursor = rowsBefore - 1
	// Now collapse the domain from cursor position 0 by tricking Update:
	// set cursor=0, send 'h', domain collapses → visibleRows shrinks to 1.
	m.cursor = 0
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(exploreModel)

	rowsAfter := len(m.visibleRows())
	if rowsBefore <= rowsAfter {
		t.Fatalf("collapse should shrink visibleRows: before=%d after=%d", rowsBefore, rowsAfter)
	}
	// The fix: cursor must be within valid bounds after collapse.
	if m.cursor < 0 || m.cursor >= rowsAfter {
		t.Fatalf("cursor %d out of bounds [0, %d) after domain collapse", m.cursor, rowsAfter)
	}

	// --- Core regression: cursor WAS deep, domain collapses, must clamp ---
	// Rebuild model, expand, move cursor to last, then send 'h' from that deep
	// position (collapseCurrent is a no-op for child rows, but the important
	// thing is the clamp runs regardless).
	m2 := newExploreModel(buildTree(domains()))
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m2 = updated.(exploreModel)
	// Force cursor to last row.
	m2.cursor = len(m2.visibleRows()) - 1
	// Send 'h': collapse no-ops on child rows, but clamp must keep cursor valid.
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m2 = updated.(exploreModel)
	n2 := len(m2.visibleRows())
	if m2.cursor < 0 || m2.cursor >= n2 {
		t.Fatalf("cursor %d out of bounds [0, %d) after 'h' on child row", m2.cursor, n2)
	}
}

func TestExplore_YankCopiesSkeleton(t *testing.T) {
	m := newExploreModel(buildTree(domains()))
	// Expand down to the first op: l (expand domain), j (to sub), l (expand sub), j (to op).
	step := func(s string) {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		m = u.(exploreModel)
	}
	step("l")
	step("j")
	step("l")
	step("j")
	op, ok := m.currentOp()
	if !ok {
		t.Fatal("expected cursor on an op after navigation")
	}
	step("y")
	if m.lastCopied != commandSkeleton(op) {
		t.Fatalf("lastCopied = %q, want %q", m.lastCopied, commandSkeleton(op))
	}
}

// expandedModel returns a fully expanded model over a tree long enough to
// overflow any terminal this is tested at.
//
// The tree is SYNTHETIC rather than the domains() fixture, and both halves of
// that matter. Driving expansion through keypresses expanded only what the cursor
// landed on; and the fixture, expanded completely, is 13 rows — shorter than most
// terminals, so at height 24 and up the overflow under test never happened and
// the assertion passed while proving nothing. The real registry is over a hundred
// operations in one domain, which is the shape a user reported; a fixture that
// cannot reach that size cannot stand in for it.
func expandedModel() exploreModel {
	var tree []domainNode
	for d := range 3 {
		dom := domainNode{name: fmt.Sprintf("domain%d", d)}
		for s := range 4 {
			sub := subNode{name: fmt.Sprintf("sub%d", s)}
			for o := range 30 {
				// Every third name is a REAL length. The registry holds names like
				// asset-book-order-deal-fee-total, which at depth 2 plus the cursor
				// prefix is 37 columns against a 28-column pane — and lipgloss's Width
				// WRAPS, so one row becomes two lines. A fixture of short names cannot
				// reach that, and the height invariant would hold in the test while
				// breaking on the real tree.
				name := fmt.Sprintf("op%d", o)
				if o%3 == 0 {
					name = fmt.Sprintf("asset-book-order-deal-fee-total-%d", o)
				}
				// A long enum list, because that is what makes a detail line wide:
				// [PREHEATING|UNDERWAY|SETTLEMENT|FINISHED|INVALID] is ~48 columns
				// before the parameter name, which overflows an 80-column terminal
				// beside a 28-column tree.
				op := api.Operation{Name: name}
				if o%4 == 0 {
					op.Params = []api.Param{{
						Name: "activityStatus",
						Enum: []string{"PREHEATING", "UNDERWAY", "SETTLEMENT", "FINISHED", "INVALID"},
					}}
				}
				sub.ops = append(sub.ops, opNode{
					id: fmt.Sprintf("domain%d.sub%d.%s", d, s, name),
					op: op,
				})
			}
			dom.subs = append(dom.subs, sub)
		}
		tree = append(tree, dom)
	}
	m := newExploreModel(tree)
	for di := range m.tree {
		m.expandDomain[di] = true
		for si := range m.tree[di].subs {
			m.expandSub[[2]int{di, si}] = true
		}
	}
	return m
}

// THE bug this viewport exists for, stated as the invariant that was violated:
// the render must never be taller than the terminal.
//
// It ran on the alternate screen, which has no scrollback, and drew every row of
// the tree. Expanding a domain with a hundred operations therefore pushed the top
// of the list off a screen that could not be scrolled back to — the rows were not
// off-view, they were unreachable. height was measured and never used.
//
// Checked across a range of heights because the arithmetic subtracts the footer
// and the overflow markers, and an off-by-one there is invisible at one size.
func TestExplore_ViewFitsTheTerminal(t *testing.T) {
	// BOTH dimensions. A line wider than the terminal wraps, and a wrapped line
	// costs a row the vertical budget has already spent — so the height fix is
	// defeated by a width overflow, which is exactly how it was defeated once: with
	// real operation names, lipgloss's Width wrapped each long row into two lines
	// and a 24-line terminal rendered 31.
	for _, height := range []int{3, 4, 5, 8, 12, 24, 40, 60} {
		for _, width := range []int{40, 60, 80, 120} {
			m := expandedModel()
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m = updated.(exploreModel)
			if len(m.visibleRows()) <= height {
				t.Fatalf("%dx%d: the tree (%d rows) is not longer than the screen, so this proves nothing",
					width, height, len(m.visibleRows()))
			}

			view := m.View()
			if lines := strings.Count(view, "\n") + 1; lines > height {
				t.Errorf("%dx%d: View rendered %d lines, which overflows the screen", width, height, lines)
			}
			for i, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Errorf("%dx%d: line %d is %d columns wide and will wrap: %q",
						width, height, i, w, line)
				}
			}
		}
	}
}

// The cursor must be on screen at every step, or navigation is blind: the user
// moves and nothing they can see changes.
func TestExplore_CursorStaysOnScreen(t *testing.T) {
	m := expandedModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 15})
	m = updated.(exploreModel)

	press := func(keys string) {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(keys)})
		m = u.(exploreModel)
	}

	// Every movement key, including the jumps, in both directions.
	//
	// Asserted against the RENDER, not against treeWindow. Comparing the cursor to
	// the window it came from is circular — that version of this test passed with
	// the viewport removed, because the window it consulted still reported the
	// range the renderer was ignoring. The cursor marker is only emitted for a row
	// inside the drawn range, so its presence in View is the property a user has:
	// "I can see where I am".
	for _, keys := range []string{"j", "j", "j", "k", "G", "g", "G", "k", "g", "j"} {
		press(keys)
		start, end, _, _ := m.treeWindow(len(m.visibleRows()))
		if m.cursor < start || m.cursor >= end {
			t.Fatalf("after %q the cursor %d sits outside the drawn window [%d,%d)",
				keys, m.cursor, start, end)
		}
		view := m.View()
		if !strings.Contains(view, cursorMarker) {
			t.Fatalf("after %q the cursor row is not in the render, so the user cannot see where they are", keys)
		}
		if lines := strings.Count(view, "\n") + 1; lines > 15 {
			t.Fatalf("after %q the render is %d lines for a height of 15", keys, lines)
		}
	}
}

// A truncated list must SAY it is truncated. Silent truncation is
// indistinguishable from a complete list, and with no scrollback there is no way
// to find out otherwise.
func TestExplore_AnnouncesHiddenRows(t *testing.T) {
	m := expandedModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(exploreModel)

	// At the bottom of a long list, rows are hidden above.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = u.(exploreModel)
	if !strings.Contains(m.View(), "more") {
		t.Error("at the bottom of a long list, the rows hidden above must be announced")
	}

	// At the top, rows are hidden below.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = u.(exploreModel)
	if !strings.Contains(m.View(), "more") {
		t.Error("at the top of a long list, the rows hidden below must be announced")
	}
}

// A resize is not a keypress, so nothing clamps on its behalf: shrinking the
// terminal has to bring the cursor back into the new, smaller window.
func TestExplore_ResizeKeepsTheCursorVisible(t *testing.T) {
	m := expandedModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m = updated.(exploreModel)
	// Move the cursor well down the list, so shrinking the terminal must bring it
	// back rather than finding it already in range.
	for range 80 {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = u.(exploreModel)
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m = updated.(exploreModel)
	start, end, _, _ := m.treeWindow(len(m.visibleRows()))
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("after shrinking the terminal the cursor %d is outside [%d,%d)", m.cursor, start, end)
	}
	if lines := strings.Count(m.View(), "\n") + 1; lines > 10 {
		t.Errorf("after shrinking, View rendered %d lines for a height of 10", lines)
	}
}

func TestExplore_RejectsNonTTY(t *testing.T) {
	var out, errW bytes.Buffer
	// out is a *bytes.Buffer, never a terminal → must be rejected.
	app := NewApp("mexc-cli", "", domains(), nil, nil, true, func(string) string { return "" }, &out, &errW)
	code := app.Run([]string{"explore"})

	if code == 0 {
		t.Fatalf("expected non-zero exit for non-TTY explore, got 0")
	}
	if !strings.Contains(errW.String(), "requires a terminal") {
		t.Fatalf("expected terminal-required error on stderr, got: %q", errW.String())
	}
	if out.Len() != 0 {
		t.Fatalf("expected nothing on stdout (no ANSI pollution), got: %q", out.String())
	}
}
