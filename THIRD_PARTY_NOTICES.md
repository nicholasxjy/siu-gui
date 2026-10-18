# Third-party notices

Siu GUI is licensed under the [MIT License](LICENSE). The distribution includes source or compiled components from the projects below.

| Component                                                    | Version / revision                                                 | License             |
| ------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------- |
| [siu](https://github.com/nicholasxjy/siu)                    | `06333e8cbd6517b87bd918f351acf94fb678e20f`                         | MIT                 |
| [mygo](https://github.com/egoist/mygo)                       | Go `b7f6946547d96c506a740f8c255069b93ba8cc06`; CLI/runtime `0.5.5` | MIT                 |
| [React](https://github.com/facebook/react)                   | `19.3.1`                                                           | MIT                 |
| [Lucide](https://github.com/lucide-icons/lucide)             | `0.577.0`                                                          | ISC                 |
| [react-markdown](https://github.com/remarkjs/react-markdown) | `10.1.0`                                                           | MIT                 |
| [purego](https://github.com/ebitengine/purego)               | pinned by `go.sum`                                                 | Apache-2.0          |
| [junction](https://github.com/tesuji/junction)               | `2.1.0`                                                            | MIT                 |
| Go runtime and Rust dependencies                             | pinned by toolchains / lockfiles                                   | See bundled notices |

The vendored siu source retains its original license in [core/upstream/LICENSE](core/upstream/LICENSE). Its origin and modifications are documented in [core/UPSTREAM.md](core/UPSTREAM.md).

Full license text, notices and authorship records for the production frontend dependency tree, Go modules/runtime and Cargo dependencies are collected in [resources/THIRD_PARTY_LICENSES.txt](resources/THIRD_PARTY_LICENSES.txt), which is included as an application resource. Regenerate it with `bun run licenses` after dependency updates. The current report contains 191 component sections, including build and platform dependencies from Cargo metadata.

Vite, TypeScript, Vitest, Testing Library, Prettier, agent-browser and the mygo CLI are development tools. Their package licenses remain with their installed distributions; they are not bundled as application code. The OS WebView engines are supplied by the host platform.
