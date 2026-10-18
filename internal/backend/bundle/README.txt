This directory is embedded into the siu-gui Go binary via //go:embed bundle/*.

The platform build script stages the compiled Rust siu-core sidecar here as
"siu-core.bin" before `go build`. This README is a placeholder so the embed
pattern always has at least one match; it is never treated as the core
binary itself.

If "siu-core.bin" is missing when the embedded fallback is used at runtime,
siu-gui reports a clear error and refuses to pretend the sidecar started.
Run `bun run prepare:core` to build and stage it, then rebuild siu-gui.
