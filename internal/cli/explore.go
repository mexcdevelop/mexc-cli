package cli

import (
	"io"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// newExploreCmd builds the `explore` command: an interactive, full-screen TUI
// for browsing every capability in the domains() registry. It is the human
// counterpart to `schema` (which emits JSON for agents); the two read the same
// registry so they cannot drift. It is internal-only and never sends a request.
//
//	<cli> explore   # browse domains -> subdomains -> operations
func newExploreCmd(f *Factory, fe *finalErr) *cobra.Command {
	return &cobra.Command{
		Use:   "explore",
		Short: "Browse MEXC capabilities in an interactive TUI (human-facing)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !isTerminal(f.Out) {
				err := errs.Validation(errs.SubtypeInvalidArgument,
					"explore is an interactive TUI and requires a terminal").
					WithHint("for machine consumption use `%s schema` (JSON)", cliname.Name())
				writeAndRecord(f, fe, err)
				return err
			}
			return runExplore(f, fe)
		},
	}
}

// isTerminal reports whether w is an interactive terminal. Only an *os.File
// backed by a real tty qualifies; a buffer (tests, pipes) never does.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// runExplore launches the Bubble Tea program over the registry tree. Reached
// only after the TTY guard in newExploreCmd, so a real terminal is guaranteed.
func runExplore(f *Factory, fe *finalErr) error {
	p := tea.NewProgram(newExploreModel(buildTree(f.Domains)), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		wrapped := errs.Validation(errs.SubtypeInvalidArgument, "explore TUI failed: %v", err)
		writeAndRecord(f, fe, wrapped)
		return wrapped
	}
	return nil
}

// opNode is one leaf: an operation plus its precomputed schema (method / path /
// access / signed / params). The schema is computed once at startup from the
// same op.Schema() call the `schema` command uses, so the TUI can never show a
// view that drifts from what an agent inspects.
type opNode struct {
	id     string
	op     api.Operation
	schema api.Schema
}

type subNode struct {
	name  string
	short string
	ops   []opNode
}

type domainNode struct {
	name  string
	short string
	subs  []subNode
}

// buildTree projects the domains() registry into the explore tree. It walks the
// exact same registry as command mounting and the schema command (the edition's
// domains.go, injected as Edition.Domains),
// so adding a domain there makes it appear here automatically — no edit to explore.
func buildTree(ds []api.DomainSpec) []domainNode {
	var tree []domainNode
	for _, d := range ds {
		dn := domainNode{name: d.Name, short: d.Short}
		for _, sd := range d.Subs {
			sn := subNode{name: sd.Name, short: sd.Short}
			for _, op := range sd.Ops {
				id := d.Name + "." + sd.Name + "." + op.Name
				sn.ops = append(sn.ops, opNode{
					id:     id,
					op:     op,
					schema: op.Schema(id, effectiveSigned(op, sd), effectiveIdentity(op, sd)),
				})
			}
			dn.subs = append(dn.subs, sn)
		}
		tree = append(tree, dn)
	}
	return tree
}

// flattenOpIDs returns every op id in the tree, sorted — must equal
// allOperationIDs() (schema.go). The TestExplore_TreeMatchesSchemaList guard
// locks this equivalence so the two views cannot silently diverge.
func flattenOpIDs(tree []domainNode) []string {
	var ids []string
	for _, d := range tree {
		for _, s := range d.subs {
			for _, o := range s.ops {
				ids = append(ids, o.id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
