# MyGO 0.8.3 API review

Checked on **2026-11-07 UTC** against the official repository, npm registry and Go module proxy. This report describes stable **v0.8.3**, commit **`5add3817c74bb7505d0b473bc5fb28abe2201b99`**. Dependency changes and observed verification results are recorded in [dependency-upgrade.md](dependency-upgrade.md).

## Version alignment

The latest stable Go module, CLI and runtime were **v0.8.3 / 0.8.3 / 0.8.3**. The Go proxy origin and both npm packages' `gitHead` agree on the commit above. MyGO's release script versions the library, CLI and npm packages together; there is no published matrix guaranteeing arbitrary mixed versions. The upgrade therefore aligns all three.

Sources: [GitHub release](https://github.com/egoist/mygo/releases/tag/v0.8.3), [Go metadata](https://proxy.golang.org/github.com/egoist/mygo/@v/v0.8.3.info), [CLI metadata](https://registry.npmjs.org/mygo-cli/0.8.3), [runtime metadata](https://registry.npmjs.org/mygo-runtime/0.8.3), [release version script](https://github.com/egoist/mygo/blob/v0.8.3/scripts/version.ts).

The previous Go pseudo-version resolved to unreleased commit `b7f6946547d96c506a740f8c255069b93ba8cc06`, after v0.5.5. Stable v0.8.3 includes it. Observed upstream main was `8065a78b72df15cc876f70a9e662fe2c5b225cca`, 11 commits ahead of stable; those unreleased changes are outside this upgrade. [Old version metadata](https://proxy.golang.org/github.com/egoist/mygo/@v/v0.5.6-0.20261017195139-b7f6946547d9.info), [immutable comparison with main](https://github.com/egoist/mygo/compare/5add3817c74bb7505d0b473bc5fb28abe2201b99...8065a78b72df15cc876f70a9e662fe2c5b225cca).

## Application API review

| Area | Stable API and application decision | Official source |
| --- | --- | --- |
| Toolchain | MyGO requires **Go 1.27.3+**. Update the module directive and build documentation. | [go.mod](https://github.com/egoist/mygo/blob/v0.8.3/go.mod) |
| Configuration | Existing `defineConfig`, JSON version import, URLs, commands, bindings, output and icon fields remain supported. Keep `mygo.config.ts`. New configuration fields are optional. | [Configuration](https://github.com/egoist/mygo/blob/v0.8.3/docs/configuration.md), [CLI declarations](https://github.com/egoist/mygo/blob/v0.8.3/packages/cli/index.d.ts) |
| Generation | `mygo generate` runs main with `MYGO_GENERATE`; `App.Run` generates and returns. Preserve the early guard before locking, native setup and backend startup. Generate the client rather than editing it. | [Bindings](https://github.com/egoist/mygo/blob/v0.8.3/docs/bindings.md), [App.Run](https://github.com/egoist/mygo/blob/v0.8.3/app.go) |
| IPC | `BindAs`, `call` and the existing service methods remain supported. Use generated `Desktop.request`, `Desktop.info` and native helpers in `src/api.ts`, replacing duplicated string method names and manually supplied return types. | [Runtime implementation](https://github.com/egoist/mygo/blob/v0.8.3/packages/runtime/src/index.ts), [generated client contract](https://github.com/egoist/mygo/blob/v0.8.3/docs/bindings.md#the-generated-client) |
| Cancellation | Bound methods run on goroutines. Their context is canceled when the calling page navigates or closes. Continue passing it to the Rust client with existing method-specific deadlines; do not retry mutations after uncertain transport failures. The core response remains an opaque JSON string across MyGO IPC. | [Bindings and context](https://github.com/egoist/mygo/blob/v0.8.3/docs/bindings.md#the-context-and-the-calling-window), [IPC implementation](https://github.com/egoist/mygo/blob/v0.8.3/ipc.go) |
| Lifecycle | `WhenReady`, single-instance locking, second-instance callbacks, macOS last-window/Dock behavior and `OnQuit` are retained. Keep existing lifecycle and backend cleanup. | [Application](https://github.com/egoist/mygo/blob/v0.8.3/docs/app.md), [single instance](https://github.com/egoist/mygo/blob/v0.8.3/single_instance.go) |
| Windows | Existing hidden title bar, runtime insets, minimum size, persisted state, background color and focus/restore methods remain valid. Keep the existing layout and window behavior. | [Windows](https://github.com/egoist/mygo/blob/v0.8.3/docs/windows.md), [window source](https://github.com/egoist/mygo/blob/v0.8.3/window.go) |
| Theme | `Theme.SetSource` and `light-dark(...)` remain supported. MyGO reflects appearance through `prefers-color-scheme`; retain frontend persistence and the existing validated Go theme helper. | [Desktop theme API](https://github.com/egoist/mygo/blob/v0.8.3/docs/native.md#dark-mode), [frontend](https://github.com/egoist/mygo/blob/v0.8.3/docs/frontend.md) |
| Dialogs | Keep `Dialog.Open`/`Save` with `Parent: CallerWindow(ctx)`, directory selection and JSON extension filters. Open returns no paths on cancellation; Save returns an empty string. Their options introduce no context-based cancellation API. | [Native dialogs](https://github.com/egoist/mygo/blob/v0.8.3/docs/native.md#dialogs), [dialog source](https://github.com/egoist/mygo/blob/v0.8.3/dialog.go) |
| Resources | Platform sidecars still flatten to `PathResources/bin/siu-core`. Keep both packaged and source-tree path resolution and the embedded fallback. | [Platform resources](https://github.com/egoist/mygo/blob/v0.8.3/docs/distribution.md#platform-resources) |
| Packaging | v0.7.0 changed DMGs and Windows installers to slug-based names. The existing release selector matches version and extension, so it accepts `siu-0.0.4-arm64.dmg` and preserves public `siu-gui_...` asset names. | [v0.7.0 notes](https://github.com/egoist/mygo/releases/tag/v0.7.0), [distribution](https://github.com/egoist/mygo/blob/v0.8.3/docs/distribution.md) |
| Native verification | The official automation plugin supports window listing, page evaluation and screenshots. Include it only in a verification build; use `App.SetPath` to isolate MyGO data, cache and logs as well as the Rust test home. | [Automation](https://github.com/egoist/mygo/blob/v0.8.3/docs/plugins/automation.md), [directories](https://github.com/egoist/mygo/blob/v0.8.3/docs/app.md#directories) |

The existing Go integration needs no signature migration or replacement IPC architecture. Generated bindings are unchanged after regeneration; the optimization is to consume those bindings throughout the frontend adapter.

## Observable platform changes and limits

Upstream fixes include macOS drag regions and quitting, Windows saved placement across displays and forced appearance, and Linux desktop appearance. These do not establish cross-platform validation for this application. Since v0.7.2, pinch zoom and swipe navigation are opt-in; since v0.6.0, several Windows browser shortcuts are disabled in production unless DevTools is enabled. Existing application Edit/View menu roles remain available. Preserve the framework defaults and verify desired behavior on the corresponding OS before release. [v0.6.0](https://github.com/egoist/mygo/releases/tag/v0.6.0), [v0.7.2](https://github.com/egoist/mygo/releases/tag/v0.7.2), [v0.7.4](https://github.com/egoist/mygo/releases/tag/v0.7.4), [v0.8.0](https://github.com/egoist/mygo/releases/tag/v0.8.0).

## Documentation inconsistencies

- The versioned getting-started page says Go 1.27+, while the README and module require **1.27.3**. Follow the module requirement. [Getting started](https://github.com/egoist/mygo/blob/v0.8.3/docs/getting-started.md), [module](https://github.com/egoist/mygo/blob/v0.8.3/go.mod).
- Some CLI help strings still call the configuration `mygo.json`; the loader, declarations and configuration documentation support **`mygo.config.ts`**. Keep the current format. [Generate help](https://github.com/egoist/mygo/blob/v0.8.3/cmd/mygo/generate.go), [configuration loader](https://github.com/egoist/mygo/blob/v0.8.3/cmd/mygo/config_ts.go).

The npm wrapper's Node >=18 declaration and the configuration evaluator's Node >=22.6 requirement describe different execution stages. This project uses Bun for configuration and a higher Node minimum for its test/browser dependencies.
