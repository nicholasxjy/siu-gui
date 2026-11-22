# Siu project guide

Siu manages MCP servers and Skills across coding agents. The React/mygo desktop app and the Go terminal app (`siu-tui`) use the same in-process Go core. Both use the upstream `siu` on-disk library format.

## Development and verification

- Install frontend dependencies with `bun install --frozen-lockfile`, then launch the desktop app with `bun run dev`. Run the headless terminal app with `go run ./cmd/siu-tui`. See [build and distribution](docs/building.md) for toolchain, platform dependencies, packaging, and releases.
- Before manually launching either client for testing, set `SIU_PROJECT_TEST_HOME` to a fresh absolute scratch path. This isolates both the library and agent directories, including from `SIU_HOME` and per-agent overrides. Startup and refresh can restore a backup and write agent configuration. See [isolated test setup](docs/verification.md#用隔离目录手动验证) for sample agent directories and packaged-app checks.
- Run `go test -race ./...` for Go changes (`go test ./...` on Windows ARM64), `bun run typecheck` and `bun run test` for frontend changes, and `bun run test:build` for packaging changes. See [verification](docs/verification.md) for browser E2E, online discovery, and native checks. Browser E2E uses the real Go core through the loopback-only `cmd/testhost` adapter, with substitutes for native dialogs.

## Where changes belong

- Put shared resource behavior in `internal/core/`. Start with `service.go` for request dispatch and `protocol.go` for JSON types. When changing the request or response contract, check the React callers in `src/`, the terminal callers in `cmd/siu-tui/`, and [the protocol reference](docs/protocol.md).
- Keep native window and dialog integration in `main.go` and `desktop.go`, and desktop UI state and workflow logic in `src/useApp.ts` and `src/components/`. The frontend calls the core through `src/api.ts`. After changing a mygo-bound method, run `bun run generate` and include the regenerated `src/mygo.ts`.
- Keep `cmd/siu-tui/` headless. It uses `core.NewTerminalService`, which omits user-level instruction-file management. Follow the existing Simplified Chinese copy in both clients when changing user-facing text.

## Shared data contracts

- Preserve compatibility with the upstream library and v1 backups. Use `internal/core/testdata/siu-backup-v1.json` when checking backup changes. Restore replaces managed resources, preserves unrelated agent configuration, and takes a safety backup before writing. See [backup and recovery](docs/backup.md).
- Use one writer process at a time when sharing a library across clients. In both clients, use the returned `snapshot` and surface `report.issues` after a mutation. A successful response can save the desired state while individual agent writes fail.
- Keep credentials redacted in configuration previews and use dummy values in fixtures. Backup exports contain plaintext MCP and proxy credentials and should be treated as private files.
