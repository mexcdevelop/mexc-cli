// Command mexc is the entry point for MEXC OpenAPI capabilities, primarily
// consumed by AI agents. It injects this edition and runs; everything below the
// composition root is generic machinery that knows nothing about this product's
// endpoints, identities or signing.
package main

import (
	"os"

	"github.com/mexcdevelop/mexc-cli/editions/public"
	"github.com/mexcdevelop/mexc-cli/framework"
	"github.com/mexcdevelop/mexc-cli/internal/transport/ws"
	"github.com/mexcdevelop/mexc-cli/pkg/transport"
)

func main() {
	ed := public.Edition()
	// Wired here, not in the edition (which may not import internal/) and not in
	// framework (whose consumers would all inherit the WS client).
	// The header name comes off the edition, so the two faces cannot disagree.
	ed.WSCollector = func(framework.Deps) transport.WSCollector {
		return ws.Collector{BuildVersionHeader: ed.BuildVersionHeader}
	}
	os.Exit(framework.Run(ed))
}
