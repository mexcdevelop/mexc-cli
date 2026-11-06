package api

// DomainSpec declares one domain and the subdomains it groups. It is the single
// source of truth shared by command assembly and the derived faces (schema /
// MCP / explore / skills), so they never drift: a subdomain is mounted as a
// command and inspectable from the same declaration.
//
// It lives in pkg/api (the port layer), not the assembly layer, so a
// composition root — including one in a separate module — can declare its
// business topology against a stable, importable type. Its fields are exported
// for the same reason.
type DomainSpec struct {
	Name  string
	Short string
	Subs  []Subdomain
}

// Subdomain describes a subdomain to be mounted: its command name, summary, its
// auth defaults, and the operations it declares. The auth defaults (Signed /
// Identity) are the subdomain-level facts an Operation inherits when it leaves
// the corresponding field at its zero value — the Operation itself carries no
// subdomain context, so inheritance can only resolve at assembly time, in the
// one layer that sees both the op and its subdomain. HostKey is a foreign key
// into the host table and is mandatory (validated at assembly time).
type Subdomain struct {
	Name     string
	Short    string
	Signed   Signing  // subdomain default signing scheme; must be concrete
	Identity Identity // subdomain default login identity; must be concrete
	HostKey  string   // foreign key into pkg/hosts; mandatory
	Ops      []Operation
}
