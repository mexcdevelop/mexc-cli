// Package buildinfo holds the build metadata each product stamps into its
// binary at link time.
//
// These variables exist to be set with `go build -ldflags -X`, and that is why
// they live in pkg/ rather than next to the code that reads them. -X names a
// symbol by its full import path, and the linker resolves it against the symbol
// table, not the type system: it will happily write to an unexported variable in
// another module's internal/ package, and — more dangerously — it prints NOTHING
// when the path does not resolve. A typo, or an upstream rename of the package
// that owns the variables, produces a clean build whose binary silently reports
// "dev / none / unknown".
//
// Since the repository split, the products that stamp these values are separate
// modules with their own build scripts. The injection point is therefore part of
// this module's contract with them, and a contract belongs on a stable, exported path
// that renaming an internal package cannot break. Hence:
//
//	go build -ldflags "-X github.com/mexcdevelop/mexc-cli/pkg/buildinfo.Version=v1.2.3 ..."
//
// The defaults are the dev values, so a plain `go build` / `go run` still works
// and reports itself honestly as an unstamped build.
package buildinfo

var (
	// Version is the normalized release version (e.g. "1.2.3"), or "dev"
	// when unstamped. Distribution launchers use standard Semantic Versioning;
	// the source Git tag keeps its leading v.
	Version = "dev"
	// Commit is the short commit hash the binary was built from.
	Commit = "none"
	// Date is the commit date in RFC3339-like form (2006-01-02T15:04:05Z).
	Date = "unknown"
)
