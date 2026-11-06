# Generated protobuf code

`spotpb/` holds the Go types for the spot WebSocket v3 push messages. The files
were produced by `protoc-gen-go` v1.36.11 with `protoc` v5.28.3 from the spot v3
`.proto` definitions, laid out under a `v3/` directory, and their field comments
were then translated to English (the repository is English-only).

The public message definitions live in
[mexcdevelop/websocket-proto](https://github.com/mexcdevelop/websocket-proto).
They are not a drop-in source for these files: that repository has a flat
layout and its batch messages are shaped differently, so regenerating from it
changes type names, not just comments. Treat a regeneration as a code change
and review the codecs in `internal/transport/ws/` against it.

## Changing the Go package path

The `go_package` option is serialized into each file's raw descriptor, behind a
length prefix. A text substitution therefore compiles and then panics in the
package `init` the moment the new path has a different length. To move the
package without regenerating, decode the `*_rawDesc` constant into a
`descriptorpb.FileDescriptorProto`, set `Options.GoPackage`, marshal it back
deterministically, and re-render the constant the way `protoc-gen-go` does (one
`strconv.Quote`d chunk per line, split after each `\n` byte). Check that an
unchanged descriptor round-trips byte for byte before trusting the output, and
run `go test ./internal/transport/ws/...` afterwards.
