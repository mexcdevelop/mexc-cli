package adapter

// BuildVersionHeader is the header this product's gateway reads to learn which
// CLI build sent a request; the engine supplies the value from pkg/buildinfo.
//
// Not to be confused with signer_clihmac.go's X-MEXC-CLI-VERSION, which carries
// the signing PROTOCOL version — a constant across releases, and absent from
// unsigned operations because the signer is what injects it.
const BuildVersionHeader = "X-MEXC-CLI-BUILD-VERSION"
