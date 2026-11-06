// Package toolref is the neutral descriptor a served tool surface hands to a
// product's transport: a tool's wire name and whether invoking it can change
// state, and nothing else.
//
// It exists so the shared engine can name a tool to a product WITHOUT teaching
// the product anything about how the tool was declared. RunMCPServer registers
// every tool on the server and hands this index to the product's ServeFunc; a
// product that attaches its own request middleware resolves a tool name through
// the index to learn the one semantic fact — read or write — that its shaping or
// auditing needs.
//
// It is deliberately not in pkg/api. pkg/api is the DECLARATION layer every
// business subdomain imports, and this is a runtime concern of one long-lived
// face; a subdomain that declares an operation should not carry a serving face's
// vocabulary in its dependency graph. It is deliberately minimal for the same
// reason the ports it feeds live in the product and not here: it names no
// protocol, no shaping and no auditing — only what any tool source can supply.
package toolref

// Ref identifies one tool to a product's transport, without saying where the
// tool came from.
type Ref struct {
	// Name is the tool's wire name, exactly as a caller spells it.
	Name string
	// Write is whether invoking this tool can change state. It is the one semantic
	// fact a product's middleware needs and the one every tool source can supply.
	Write bool
}
