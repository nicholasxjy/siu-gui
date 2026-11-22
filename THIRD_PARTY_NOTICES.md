# Third-party notices

Siu is distributed under the MIT License. The desktop and terminal archives include the license texts for their dependencies in `THIRD_PARTY_LICENSES.txt`. This file is generated from the dependency tree and also includes the Go runtime and the bundled icon notices. [Siu license](LICENSE) · [Bundled notices](resources/THIRD_PARTY_LICENSES.txt)

| Component                                                                                                | Where it is used                                                                  | License    |
| -------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ---------- |
| [siu](https://github.com/nicholasxjy/siu)                                                                | Upstream resource management and v1 backup format, ported into the shared Go core | MIT        |
| [MyGO](https://github.com/egoist/mygo)                                                                   | Desktop window, WebView and bindings                                              | MIT        |
| [React](https://github.com/facebook/react), [react-markdown](https://github.com/remarkjs/react-markdown) | Desktop interface and Markdown rendering                                          | MIT        |
| [Lucide](https://github.com/lucide-icons/lucide)                                                         | Desktop interface icons                                                           | ISC        |
| [tview](https://github.com/rivo/tview)                                                                   | Terminal widgets and layout                                                       | MIT        |
| [tcell](https://github.com/gdamore/tcell)                                                                | Terminal input and rendering                                                      | Apache-2.0 |

The Go port retains the upstream siu license. The generator records the source revisions for the core port and the backup module. Dependency versions are recorded in the Go and Bun manifests and lockfiles. [Upstream license](internal/core/UPSTREAM_LICENSE) · [License generator](tools/licenses.ts) · [Go modules](go.mod) · [Bun lockfile](bun.lock)

Most Agent icons come from the pinned LobeHub Icons collection. Pi uses its official logo, and Droid uses the Factory favicon. The respective owners retain their names and marks. [Icon sources and attributions](docs/agent-icons-research.md)

Run `bun run licenses` after updating dependencies to refresh the bundled notices. [Build and distribution guide](docs/building.md)
