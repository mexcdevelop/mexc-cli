package cli

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
)

// rowKind distinguishes the three tree levels so navigation and rendering can
// treat a domain header differently from an operation leaf.
type rowKind int

const (
	rowDomain rowKind = iota
	rowSub
	rowOp
)

type row struct {
	kind     rowKind
	depth    int
	label    string
	domainIx int
	subIx    int
	opIx     int // valid when kind == rowOp
}

type exploreModel struct {
	tree         []domainNode
	expandDomain map[int]bool
	expandSub    map[[2]int]bool
	cursor       int
	// scrollTop is the first tree row drawn. It exists because the program runs
	// on the ALTERNATE SCREEN, which has no scrollback: a tree longer than the
	// terminal cannot be scrolled back to by the terminal itself, so rows drawn
	// past the bottom are not merely off-view, they are unreachable. Before this,
	// height was measured and then never used, and expanding a domain with many
	// operations pushed the top of the list out of existence.
	scrollTop     int
	width, height int
	lastCopied    string
}

func newExploreModel(tree []domainNode) exploreModel {
	return exploreModel{
		tree:         tree,
		expandDomain: map[int]bool{},
		expandSub:    map[[2]int]bool{},
	}
}

func (m exploreModel) Init() tea.Cmd { return nil }

// visibleRows computes the flat list of rows currently shown, honoring expand
// state and the active filter. Navigation indexes into this slice.
func (m exploreModel) visibleRows() []row {
	var rows []row
	for di, d := range m.tree {
		rows = append(rows, row{kind: rowDomain, depth: 0, label: d.name, domainIx: di})
		if !m.expandDomain[di] {
			continue
		}
		for si, s := range d.subs {
			rows = append(rows, row{kind: rowSub, depth: 1, label: s.name, domainIx: di, subIx: si})
			if !m.expandSub[[2]int{di, si}] {
				continue
			}
			for oi, o := range s.ops {
				rows = append(rows, row{kind: rowOp, depth: 2, label: o.op.Name, domainIx: di, subIx: si, opIx: oi})
			}
		}
	}
	return rows
}

func (m exploreModel) currentOp() (opNode, bool) {
	rows := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return opNode{}, false
	}
	r := rows[m.cursor]
	if r.kind != rowOp {
		return opNode{}, false
	}
	return m.tree[r.domainIx].subs[r.subIx].ops[r.opIx], true
}

func (m exploreModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A resize changes the capacity, so the window is re-derived here too:
		// otherwise shrinking the terminal leaves the cursor outside it with no
		// key having been pressed.
		m.keepCursorVisible(len(m.visibleRows()))
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.visibleRows())-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		// Jumps, because a domain can hold over a hundred operations and j/k
		// alone is not navigation at that size.
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = len(m.visibleRows()) - 1
		case "pgdown", "ctrl+d":
			m.cursor += m.treeCapacity()
		case "pgup", "ctrl+u":
			m.cursor -= m.treeCapacity()
		case "l", "right", "enter":
			m.expandCurrent()
		case "h", "left":
			m.collapseCurrent()
		case "y":
			if n, ok := m.currentOp(); ok {
				m.lastCopied = commandSkeleton(n)
				_ = clipboard.WriteAll(m.lastCopied) // best-effort: ignore on headless / no-clipboard hosts
			}
		}
		// Clamp cursor after any key that may change visibleRows length
		// (expand, collapse, filter). Prevents cursor from going out of
		// bounds when rows are removed (e.g. collapse shrinks the list).
		n := len(m.visibleRows())
		if n > 0 {
			m.cursor = clamp(m.cursor, 0, n-1)
		} else {
			m.cursor = 0
		}
		// After the cursor is final, and not before: expand/collapse changes the
		// row count, so the window has to be derived from the list as it now is.
		m.keepCursorVisible(n)
	}
	return m, nil
}

// helpLines is how many lines the footer occupies. The tree must not claim them:
// on the alternate screen anything drawn past the last line is simply lost.
func (m exploreModel) helpLines() int {
	if m.lastCopied != "" {
		return 2
	}
	return 1
}

// treeCapacity is how many tree rows fit, once the footer and the blank line
// above it are subtracted.
//
// The fallback matters: bubbletea delivers WindowSizeMsg asynchronously, so the
// first View can run with height still 0. A capacity of 0 there would render an
// empty tree for one frame; 24 is the conventional terminal height and shows a
// plausible screenful until the real size arrives.
func (m exploreModel) treeCapacity() int {
	h := m.height
	if h <= 0 {
		h = 24
	}
	return max(1, h-m.helpLines()-1)
}

// treeWindow returns the half-open range of rows to draw, and whether each
// overflow marker fits.
//
// The markers are counted AGAINST the capacity rather than drawn on top of it:
// on top, they push the bottom row off the screen, which is the very failure they
// exist to announce.
//
// And when the screen is too small for both a row and a marker, the ROW wins. A
// marker is information about what is missing; the row under the cursor is the
// thing being looked at. This is why the caller is told whether each marker fits
// instead of deciding for itself — an earlier version forced a content row with
// max(1, rows) and let the markers overflow, and a 3-line terminal rendered 4
// lines.
func (m exploreModel) treeWindow(total int) (start, end int, markAbove, markBelow bool) {
	capacity := m.treeCapacity()
	if total <= capacity {
		return 0, total, false, false
	}
	start = clamp(m.scrollTop, 0, total-1)
	rows := capacity
	if start > 0 && rows > 1 {
		rows-- // a row goes to the "more above" marker
		markAbove = true
	}
	if start+rows < total && rows > 1 {
		rows-- // and one to "more below"
		markBelow = true
	}
	return start, min(total, start+rows), markAbove, markBelow
}

// keepCursorVisible scrolls by the MINIMUM needed to bring the cursor into the
// window. Minimum, not centred: centring redraws every row for a one-row move,
// which reads as the list jumping under the hand.
//
// The loop converges because each pass either contains the cursor or moves the
// window towards it, and the capacity can only shift by the one or two rows the
// markers take. Three passes is slack over the two that can ever be needed.
func (m *exploreModel) keepCursorVisible(total int) {
	if total <= 0 {
		m.scrollTop = 0
		return
	}
	for range 3 {
		start, end, _, _ := m.treeWindow(total)
		if m.cursor >= start && m.cursor < end {
			return
		}
		if m.cursor < start {
			m.scrollTop = m.cursor
		} else {
			m.scrollTop += m.cursor - end + 1
		}
		m.scrollTop = clamp(m.scrollTop, 0, total-1)
	}
}

// treePaneWidth is the width of the navigation column. Named because THREE
// places have to agree on it: the style that pads to it, the truncation that
// keeps a row inside it, and the detail pane that gets the rest of the screen.
const treePaneWidth = 28

// truncateToWidth keeps a string to w display columns, marking the cut.
//
// It exists because lipgloss's Width WRAPS rather than truncates, and a wrapped
// row is TWO lines — which silently breaks the one-row-one-line assumption the
// whole viewport rests on. Measured with real operation names
// (asset-book-order-deal-fee-total is 31 columns before the indent): a 24-line
// terminal rendered 31 lines, so the viewport fixed nothing for the domains whose
// names are long, which is most of them.
//
// Truncating rather than widening the pane: the tree is a column for NAVIGATING,
// and the full id is already in the detail pane beside it. Measured in display
// columns rather than runes, because the danger marker is not one column wide
// everywhere.
func truncateToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)+"…") > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// screenWidth is the terminal width, with treeCapacity's fallback for the frame
// before the first WindowSizeMsg lands.
func screenWidth(width int) int {
	if width <= 0 {
		return 80
	}
	return width
}

// treeWidth is the navigation column's width, which shrinks with the terminal so
// the two panes together never exceed it.
func treeWidth(width int) int {
	return min(treePaneWidth, screenWidth(width))
}

// detailWidth is exactly what the tree leaves, and may be nothing.
//
// NOT floored at some readable minimum: an earlier max(20, …) here guaranteed an
// overflow on a narrow terminal — beside a 28-column tree at 40 columns the space
// left is 11, and claiming 20 put 47 columns on a 40-column screen. A detail pane
// that is too narrow to read is a degenerate terminal's problem; a pane that
// wraps corrupts the layout the viewport just fixed.
func detailWidth(width int) int {
	return max(0, screenWidth(width)-treeWidth(width)-1)
}

// cursorMarker prefixes the selected row. Named because the tests assert on it:
// it is emitted only for a row inside the drawn window, which makes its presence
// in the render the machine-checkable form of "the user can see where they are".
const cursorMarker = "▸ "

// clamp returns v clamped to the inclusive range [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *exploreModel) expandCurrent() {
	rows := m.visibleRows()
	if m.cursor >= len(rows) {
		return
	}
	r := rows[m.cursor]
	switch r.kind {
	case rowDomain:
		m.expandDomain[r.domainIx] = true
	case rowSub:
		m.expandSub[[2]int{r.domainIx, r.subIx}] = true
	}
}

func (m *exploreModel) collapseCurrent() {
	rows := m.visibleRows()
	if m.cursor >= len(rows) {
		return
	}
	r := rows[m.cursor]
	switch r.kind {
	case rowDomain:
		m.expandDomain[r.domainIx] = false
	case rowSub:
		m.expandSub[[2]int{r.domainIx, r.subIx}] = false
	}
}

var (
	dangerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // red
	signedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // yellow
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// isDanger reports whether an op is a write (and thus shown with a red ⚠),
// extending the write-warning grading into the TUI.
func isDanger(n opNode) bool { return n.op.IsWrite() }

func (m exploreModel) View() string {
	left := m.renderTree()
	right := m.capLines(m.renderDetail())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	// The footer is a rendered line like any other and must fit the width. It grew
	// to 84 columns when the paging keys were listed in it, which wrapped on an
	// 80-column terminal and cost a row the vertical budget had already spent —
	// the same bug as a wrapped tree row, arriving through the help text.
	width := screenWidth(m.width)
	help := dimStyle.Render(truncateToWidth("j/k move · g/G ends · PgUp/PgDn page · l/h expand · y copy · q quit", width))
	if m.lastCopied != "" {
		help = dimStyle.Render(truncateToWidth("copied: "+m.lastCopied, width)) + "\n" + help
	}
	return body + "\n" + help
}

func (m exploreModel) renderTree() string {
	var b strings.Builder
	rows := m.visibleRows()
	start, end, markAbove, markBelow := m.treeWindow(len(rows))
	// The markers are not decoration. A silently truncated list is
	// indistinguishable from a complete one, and on the alternate screen there is
	// no scrollback to discover otherwise — which is exactly how the missing rows
	// went unnoticed.
	if markAbove {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for i, r := range rows[start:end] {
		i += start
		cursor := "  "
		if i == m.cursor {
			cursor = cursorMarker
		}
		// Truncated BEFORE styling. Styling first would leave ANSI escape
		// sequences straddling the cut, and the budget is for VISIBLE columns:
		// two go to the cursor prefix.
		text := strings.Repeat("  ", r.depth) + r.label
		danger := false
		if r.kind == rowOp {
			o := m.tree[r.domainIx].subs[r.subIx].ops[r.opIx]
			if isDanger(o) {
				text += " ⚠"
				danger = true
			}
		}
		text = truncateToWidth(text, treeWidth(m.width)-2)
		if danger {
			text = dangerStyle.Render(text)
		}
		b.WriteString(cursor + text + "\n")
	}
	if markBelow {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-end)) + "\n")
	}
	return lipgloss.NewStyle().Width(treeWidth(m.width)).Render(b.String())
}

// capLines truncates a pane to the capacity the tree is held to.
//
// lipgloss.JoinHorizontal takes the height of the TALLER column, so a detail
// pane longer than the screen pushes the footer off exactly as an unbounded tree
// did — a second instance of the same bug, reached through an operation that
// declares many parameters rather than a domain that holds many operations.
func (m exploreModel) capLines(s string) string {
	capacity := m.treeCapacity()
	// Width first: a detail line wider than the space beside the tree wraps in the
	// terminal, and a wrapped line costs a row the vertical budget has already
	// been spent on. A params line carrying an enum list
	// ([PREHEATING|UNDERWAY|SETTLEMENT|FINISHED|INVALID]) reaches ~70 columns,
	// which overflows an 80-column terminal beside a 28-column tree.
	width := detailWidth(m.width)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = truncateToWidth(l, width)
	}
	s = strings.Join(lines, "\n")
	if len(lines) <= capacity {
		return s
	}
	// With room for only one line, that line is the marker: content truncated to
	// nothing plus a marker was two lines for a budget of one.
	if capacity <= 1 {
		return dimStyle.Render(fmt.Sprintf("… %d more lines", len(lines)))
	}
	kept := lines[:capacity-1]
	hidden := len(lines) - len(kept)
	return strings.Join(kept, "\n") + "\n" + dimStyle.Render(fmt.Sprintf("… %d more lines", hidden))
}

func (m exploreModel) renderDetail() string {
	n, ok := m.currentOp()
	if !ok {
		return dimStyle.Render("select an operation")
	}
	s := n.schema
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", n.id)
	access := string(s.Access)
	if s.Write {
		access = dangerStyle.Render(access + " ⚠")
	}
	fmt.Fprintf(&b, "access   %s\n", access)
	signed := "no"
	if s.Signed {
		signed = signedStyle.Render("yes")
	}
	fmt.Fprintf(&b, "signed   %s\n", signed)
	fmt.Fprintf(&b, "identity %s\n", s.Identity)
	fmt.Fprintf(&b, "method   %s\n", s.Method)
	fmt.Fprintf(&b, "path     %s\n", s.Path)
	if len(s.Params) > 0 {
		b.WriteString("params\n")
		for _, p := range s.Params {
			req := "optional"
			if p.Required {
				req = "required"
			}
			line := fmt.Sprintf("  %-12s %s", p.Name, req)
			if len(p.Enum) > 0 {
				line += " [" + strings.Join(p.Enum, "|") + "]"
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// commandSkeleton produces the copyable invocation skeleton for an op. It never
// fills real values — it shows how to call, not a runnable request — keeping
// explore a browser, not a request generator.
func commandSkeleton(n opNode) string {
	parts := strings.SplitN(n.id, ".", 3)
	cmd := cliname.Name() + " " + strings.Join(parts, " ")
	for _, p := range n.op.Params {
		placeholder := "<...>"
		if len(p.Enum) > 0 {
			placeholder = "<" + strings.Join(p.Enum, "|") + ">"
		}
		seg := " --" + p.Name + " " + placeholder
		if !p.Required {
			seg = " [--" + p.Name + " " + placeholder + "]"
		}
		cmd += seg
	}
	return cmd
}
