# 依赖升级与验证记录

检查日期：**2026-11-07 UTC**。所有版本来自官方 npm 注册表、crates.io 或 Go 模块代理；只使用稳定版。下面比较依赖清单的升级前后声明版本，省略 npm 的兼容范围前缀；相同版本表示经核查已为最新。间接依赖由各包管理器解析并更新锁文件。

## npm / Bun

官方来源为各包的 [npm 注册表](https://registry.npmjs.org/) `latest` 元数据。完整查询记录保存在 `.artifacts/dependency-upgrade/registry-audit.json`。

| 依赖 | 升级前 | 升级后 |
| --- | --- | --- |
| [lucide-react](https://registry.npmjs.org/lucide-react/latest) | 0.577.0 | 1.70.0 |
| [mygo-runtime](https://registry.npmjs.org/mygo-runtime/latest) | 0.5.5 | 0.8.3 |
| [react](https://registry.npmjs.org/react/latest) | 19.3.1 | 19.3.1 |
| [react-dom](https://registry.npmjs.org/react-dom/latest) | 19.3.1 | 19.3.1 |
| [react-markdown](https://registry.npmjs.org/react-markdown/latest) | 10.1.0 | 10.1.0 |
| [@testing-library/jest-dom](https://registry.npmjs.org/%40testing-library%2Fjest-dom/latest) | 6.9.1 | 7.0.1 |
| [@testing-library/react](https://registry.npmjs.org/%40testing-library%2Freact/latest) | 16.3.0 | 16.3.3 |
| [@testing-library/user-event](https://registry.npmjs.org/%40testing-library%2Fuser-event/latest) | 14.6.1 | 14.6.8 |
| [@types/bun](https://registry.npmjs.org/%40types%2Fbun/latest) | 1.3.0 | 1.4.4 |
| [@types/node](https://registry.npmjs.org/%40types%2Fnode/latest) | 26.0.0 | 26.6.6 |
| [@types/react](https://registry.npmjs.org/%40types%2Freact/latest) | 19.0.0 | 19.3.0 |
| [@types/react-dom](https://registry.npmjs.org/%40types%2Freact-dom/latest) | 19.0.0 | 19.3.0 |
| [@vitejs/plugin-react](https://registry.npmjs.org/%40vitejs%2Fplugin-react/latest) | 6.0.0 | 6.2.1 |
| [agent-browser](https://registry.npmjs.org/agent-browser/latest) | 0.39.1 | 0.41.2 |
| [jsdom](https://registry.npmjs.org/jsdom/latest) | 28.0.0 | 31.0.0 |
| [mygo-cli](https://registry.npmjs.org/mygo-cli/latest) | 0.5.5 | 0.8.3 |
| [prettier](https://registry.npmjs.org/prettier/latest) | 3.6.0 | 3.9.10 |
| [typescript](https://registry.npmjs.org/typescript/latest) | 7.0.0 | 7.0.2 |
| [vite](https://registry.npmjs.org/vite/latest) | 8.3.4 | 8.3.6 |
| [vitest](https://registry.npmjs.org/vitest/latest) | 4.0.0 | 5.1.0 |

## Rust / Cargo

| 依赖 | 升级前 | 升级后 |
| --- | --- | --- |
| [anyhow](https://crates.io/api/v1/crates/anyhow) | 1.0.104 | 1.0.104 |
| [base64](https://crates.io/api/v1/crates/base64) | 0.23.1 | 0.23.1 |
| [dirs](https://crates.io/api/v1/crates/dirs) | 7.0.0 | 7.0.0 |
| [flate2](https://crates.io/api/v1/crates/flate2) | 1.1.10 | 1.1.11 |
| [proxy-watch](https://crates.io/api/v1/crates/proxy-watch) | 0.3.1 | 0.3.1 |
| [serde](https://crates.io/api/v1/crates/serde) | 1.0.229 | 1.0.229 |
| [serde_json](https://crates.io/api/v1/crates/serde_json) | 1.0.151 | 1.0.154 |
| [sha2](https://crates.io/api/v1/crates/sha2) | 0.11.0 | 0.11.1 |
| [tar](https://crates.io/api/v1/crates/tar) | 0.4.46 | 0.4.46 |
| [tempfile](https://crates.io/api/v1/crates/tempfile) | 3.27.0 | 3.27.0 |
| [toml_edit](https://crates.io/api/v1/crates/toml_edit) | 0.25.15 | 0.25.19 |
| [ureq](https://crates.io/api/v1/crates/ureq) | 3.4.2 | 3.5.1 |
| [junction](https://crates.io/api/v1/crates/junction) | 2.1.0 | 2.1.0 |

`tempfile` 同时作为开发依赖，两个声明均为 3.27.0；`toml_edit` 的锁定版本带 `+spec-1.1.0` 构建元数据，清单按 Cargo 惯例省略。

## Go

| 模块 | 升级前 | 升级后 |
| --- | --- | --- |
| [MyGO](https://proxy.golang.org/github.com/egoist/mygo/@latest) | v0.5.6-0.20261017195139-b7f6946547d9 | v0.8.3 |
| [purego](https://proxy.golang.org/github.com/ebitengine/purego/@latest) | v0.11.1 | v0.11.3 |
| [x/text](https://proxy.golang.org/golang.org/x/text/@latest) | v0.43.0 | v0.43.0 |
| [x/net](https://proxy.golang.org/golang.org/x/net/@latest) | 未在原 go.mod 中声明 | v0.62.0（MyGO 新引入的间接依赖） |

## MyGO 适配与应用优化

MyGO Go 模块、CLI、runtime 统一为 **0.8.3**，官方提交为 `5add3817c74bb7505d0b473bc5fb28abe2201b99`。配置、生成器、IPC、窗口生命周期、主题、原生对话框及平台资源布局已逐项对照官方文档和源码；结论与来源见 [MyGO API 审查](mygo-api-research.md)。

`src/api.ts` 现在通过生成的 `Desktop` 客户端调用 Go，使用生成器提供的方法签名和返回类型，避免在适配层重复维护字符串方法名。新增七项调用边界测试，覆盖请求封装、核心错误、超时不重试、取消选择、浏览器隔离及 URL 限制。生成器运行后 `src/mygo.ts` 内容一致。

Bun 的 CI 版本更新到 **1.4.4**，Node 更新到 **26**；Go 最低版本随 MyGO 提升到 **1.27.3**。jsdom 31 要求较新的 Node，项目文档推荐 **Node 26+**，同时说明支持 **24.15+ 的 24.x**。第三方许可证已通过 `bun run licenses` 重新生成。

## 验证结果

升级前，类型检查、前端测试、构建脚本测试、Rust 测试和 Go 竞态测试全部通过。完整输出位于 `.artifacts/dependency-upgrade/baseline-*.log`。

| 检查 | 状态与实际结果 |
| --- | --- |
| `bun install --frozen-lockfile` | 通过：依赖清单与锁文件一致 |
| `bun outdated` | 通过：没有过期的显式 npm 依赖 |
| `go list -m -u` 核对 go.mod 中全部依赖 | 通过：MyGO、purego、x/net、x/text 均为最新稳定版 |
| `bun run typecheck` | 通过 |
| `bun run test` | 通过：7 个文件、44 项测试 |
| `bun run test:build` | 通过：25 项构建与发布脚本测试 |
| `cargo test --locked --manifest-path core/Cargo.toml` | 通过：81 项单元测试、37 项协议测试；1 项联网探针按默认设置忽略 |
| `go test -race ./...` | 通过：全部 3 个包 |
| `bun run generate` | 通过：绑定内容一致 |
| `bun run build:web` | 通过：Vite 8.3.6 生产构建 |
| `bun run prepare:core` | 通过：release sidecar 启动与快照检查 |
| `bun run test:e2e` | 通过：真实 Go/Rust 后端的 28 项检查，含 MCP、Skills、代理、配置迁移、主题和无障碍检查 |
| `bun run build --platform darwin/arm64` | 通过：生成 `Siu.app` 与 `siu-0.0.4-arm64.dmg` |
| 原生 WebView 冒烟验证 | 通过：8 项检查，含启动、真实 IPC、深浅主题、三个原生选择器及取消返回、单实例复用、正常退出 |
| 应用签名、DMG 校验与架构检查 | 通过：签名与镜像校验成功，主程序和 Rust sidecar 均为 arm64 |
| `bun scripts/release.ts --platform darwin/arm64` | 通过：映射为 `siu-gui_0.0.4_macos_arm64.dmg` |
| `bun run licenses` / `git diff --check` | 通过 |

测试和构建输出保存在 `.artifacts/dependency-upgrade/`；浏览器 E2E 的结果与截图在 `.artifacts/e2e/`。

原生检查使用 Go overlay 构建当前应用，额外包含官方 automation 插件、内嵌生产前端以及隔离路径初始化。这些验证入口不进入生产源码或发布包。Rust 使用临时 `SIU_GUI_TEST_HOME`；MyGO 的 user-data/cache/logs 也使用同一个临时目录。原生截图和 JSON 检查结果保存在 `.artifacts/dependency-upgrade/`。

## 验证范围

本次实际运行环境为 **macOS / Apple Silicon**，工具链为 Bun 1.4.4、Go 1.27.3、Node 26.13.0、Rust 1.99.1。Windows、Linux、Intel Mac、Universal 的原生运行和打包未在本次执行；不将现有多平台脚本或测试样例视为这些平台的实际验证。

联网探针、签名公证、发布和部署未执行。上游文档中 Go 最低版本及配置文件名称的两处不一致，已在 API 审查报告中记录；实现遵循模块声明和实际配置加载器。
