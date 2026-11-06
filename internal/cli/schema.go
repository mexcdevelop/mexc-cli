package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mexcdevelop/mexc-cli/internal/output"
	"github.com/mexcdevelop/mexc-cli/pkg/api"
	"github.com/mexcdevelop/mexc-cli/pkg/cliname"
	"github.com/mexcdevelop/mexc-cli/pkg/errs"
)

// newSchemaCmd builds the `schema` command: it prints the machine-readable
// parameter structure of an operation so an agent can inspect it before calling,
// rather than guessing the --<flag> layout. It reads from the same
// domain registry that assembles the command tree, so the two never drift, and
// it never touches the network — it is a pure lookup over declared Operations.
//
//	<cli> schema <domain>.<sub>.<op>      # print one operation's schema
//	<cli> schema                          # list every inspectable operation id
func newSchemaCmd(f *Factory, fe *finalErr) *cobra.Command {
	return &cobra.Command{
		Use:   "schema [<domain>.<sub>.<operation>]",
		Short: "Print an operation's parameter schema (JSON), or list all operation ids",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return output.JSON(f.Out, map[string]any{"operations": allOperationIDs(f.Domains)})
			}
			schema, err := lookupSchema(f.Domains, args[0])
			if err != nil {
				writeAndRecord(f, fe, err)
				return err
			}
			return output.JSON(f.Out, schema)
		},
	}
}

// lookupSchema resolves a fully-qualified id (<domain>.<sub>.<operation>) to its
// Operation schema, returning a structured validation error the agent can route
// on if the id is malformed or unknown.
func lookupSchema(ds []api.DomainSpec, id string) (api.Schema, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 3 {
		return api.Schema{}, errs.Validation(errs.SubtypeInvalidArgument,
			"operation id %q must be <domain>.<sub>.<operation>", id).
			WithHint("run `%s schema` with no argument to list valid ids", cliname.Name())
	}
	domainName, subName, opName := parts[0], parts[1], parts[2]
	for _, d := range ds {
		if d.Name != domainName {
			continue
		}
		for _, sd := range d.Subs {
			if sd.Name != subName {
				continue
			}
			for _, op := range sd.Ops {
				if op.Name == opName {
					// The schema reports the EFFECTIVE sign/identity — subdomain
					// inheritance resolved here, where both sides are in hand —
					// so an agent always sees concrete values.
					return op.Schema(id, effectiveSigned(op, sd), effectiveIdentity(op, sd)), nil
				}
			}
		}
	}
	return api.Schema{}, errs.Validation(errs.SubtypeInvalidArgument,
		"unknown operation %q", id).
		WithHint("run `%s schema` with no argument to list valid ids", cliname.Name())
}

// allOperationIDs returns every <domain>.<sub>.<operation> id known to the
// binary, sorted, so an agent can discover what is inspectable.
func allOperationIDs(ds []api.DomainSpec) []string {
	var ids []string
	for _, d := range ds {
		for _, sd := range d.Subs {
			for _, op := range sd.Ops {
				ids = append(ids, d.Name+"."+sd.Name+"."+op.Name)
			}
		}
	}
	sort.Strings(ids)
	return ids
}
