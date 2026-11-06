// Package cliname holds the name this binary was invoked as, so user-facing text
// can name the command the reader actually typed.
//
// Every product built on this module ships its own binary, and the shared layer
// is compiled into all of them. A hint that hard-codes one product's command
// name is therefore wrong in the others — and wrong in the ordinary case of a
// renamed or symlinked binary, which the command tree itself already handles
// (framework.Run derives the root command's name from argv[0]). This package is
// the same fact, made available to the code that writes prose rather than
// command paths.
//
// It is a package-level value set once by the composition root, matching
// config.SetStateDirName and hosts.SetDefaultEnv: a value every layer needs,
// known only to the entry point, and settable before anything reads it. The
// alternative — threading the name through every function that builds a hint —
// would change the signature of pure declaration checks in pkg/api that have no
// other reason to know what a CLI is.
package cliname

import "strings"

// defaultName is what an unset binary reports. It is the historical name of the
// first product built on this module, so nothing regresses when a caller has not
// set one (unit tests, `go run`, a library consumer).
const defaultName = "mexc-cli"

var name = defaultName

// Set records the name this binary was invoked as. framework.Run calls it from
// argv[0], the same source the root command's name comes from, so a hint and a
// command path can never disagree.
//
// An empty or path-like argument is ignored rather than stored: a hint reading
// "run `/usr/local/bin/mexc-cli schema`" or "run `. schema`" is worse than one
// naming the default. The caller passes a base name; this is the guard for when
// it does not.
func Set(n string) {
	n = strings.TrimSpace(n)
	if n == "" || strings.ContainsAny(n, `/\`) {
		return
	}
	name = n
}

// Name returns the command name to print in user-facing text.
func Name() string { return name }
