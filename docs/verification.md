# 验证记录

本记录包含 2026-10-18 的 v0.0.1 首次发布验证，以及后续下载代理和加载恢复修复的补充验证。环境为 macOS 27.0.1 / Apple Silicon，Go 1.27.2、Bun 1.4.3、Rust 稳定版。

所有真实 Agent 配置、资源库和 Skills 链接操作均使用临时绝对路径。进程、浏览器与原生应用检查显式设置 `SIU_GUI_TEST_HOME`；Rust 单元测试使用等价的 `Env::rooted(tempdir)`。测试未修改日常 Agent 配置。

## v0.0.2 代理设置与加载恢复验证

| 检查 | 结果 |
| --- | --- |
| Rust 单元与真实 NDJSON 协议 | 88 项通过，1 项联网探针默认忽略 |
| 前端测试 | 23 项通过 |
| 构建参数与发布产物测试 | 25 项通过 |
| Go 竞态测试 | 42 项通过 |
| TypeScript、Clippy（拒绝警告）、格式与差异检查 | 通过 |
| 正式界面与真实 Go/Rust 后端 | 26 项通过 |
| `SIU_TEST_SYSTEM_PROXY_ONLY=1 bun run test:online` | 6 项通过 |
| ARM64/通用应用、DMG、应用签名与镜像校验 | 通过 |

本地代理服务覆盖 HTTP CONNECT、百分号编码认证、凭据不转发给目标站、SOCKS5 远程 DNS、代理设置落盘后即时切换、重定向时重新检查绕过规则和明确直连。独立请求期限继续覆盖响应头/正文停滞，HTTP 工作线程数仍有上限。协议测试验证设置跨进程保留、默认系统模式和无效输入不覆盖已有配置。前端测试另覆盖保存失败保留输入、读取重试和损坏配置恢复默认值。

浏览器新增两项流程验证设置页面保存手动代理、重新进入页面保留地址，以及直连/系统模式切换。完整网络设置截图为 `.artifacts/e2e/07-proxy-settings.png`。

联网测试清除核心进程中的全部大小写代理环境变量，验证 macOS 原生系统代理独立生效。六项包括运行中切换代理、官方 Registry、离线缓存重启安装、skills.sh、npm/PyPI MCP 版本更新，以及 GitHub Skill 扫描/安装/更新/删除。测试没有启动 MCP 服务。Docker 命令会接收代理环境，但镜像由 Docker 引擎下载；引擎需要自己的代理配置。

本次重新生成 v0.0.2 ARM64 与通用应用：`build/darwin-arm64/Siu.app`、`build/darwin-arm64/Siu 0.0.2 arm64.dmg`、`build/darwin-universal/Siu.app` 和 `build/darwin-universal/Siu 0.0.2.dmg`。发行核心启动检查确认版本为 0.0.2，通用应用的主程序与核心均含 arm64/x86_64 架构，签名与 DMG 校验通过。第三方许可按六个桌面目标的实际依赖图收集，共 232 个组件。原生 Windows/Linux 系统代理、macOS/Windows PAC/WPAD 运行与 Intel 硬件测试仍需各自环境验证。以下保留首次发布的跨平台及原生窗口检查记录。

## 首次发布执行的检查

| 检查                                                                                           | 本次结果                        | 主要覆盖                                                                                |
| ---------------------------------------------------------------------------------------------- | ------------------------------- | --------------------------------------------------------------------------------------- |
| `bun run typecheck`                                                                            | 通过                            | 界面、脚本、工具与测试的 TypeScript 检查                                                |
| `bun run test`                                                                                 | 13 项通过                       | 配置参数与凭据保留、脱敏、兼容性、并发写入、过期读取、错误后的实际快照、扫描 token 回收 |
| `bun run test:build`                                                                           | 25 项通过                       | 六个目标、通用 macOS 目标、构建参数校验与转发                                           |
| `cargo test --locked --manifest-path core/Cargo.toml`                                          | 78 项通过；1 项联网探针默认忽略 | 66 项单元测试与 12 项真实 NDJSON 协议测试                                               |
| `cargo clippy --manifest-path core/Cargo.toml --all-targets -- -D warnings`                    | 通过                            | Rust 静态检查                                                                           |
| `go test -race ./...`                                                                          | 42 项通过；1 项条件跳过         | IPC 多路响应、取消、进程关闭竞争、崩溃恢复、进程回收、原生辅助方法与隔离测试服务        |
| `bun run test:e2e`                                                                             | 24 项通过                       | 正式界面 + 真实 Go/Rust 后端 + 实际配置文件与目录链接                                   |
| `bun run test:online`                                                                          | 4 项通过                        | 官方 Registry、缓存后离线重启安装、skills.sh、GitHub Skill 生命周期                     |
| `cargo test --locked --manifest-path core/Cargo.toml online_mcp_package_versions -- --ignored` | 1 项通过                        | npm 与 PyPI 实际最新版本查询                                                            |
| `bun run build --platform darwin/universal`                                                    | 通过                            | 两种 Rust 核心、前端、两种 Go 主程序、通用二进制、签名、DMG                             |
| 六个 `GOOS/GOARCH` 的 `go build`                                                               | 全部通过                        | darwin / windows / linux × amd64 / arm64 的桌面 Go 代码编译                             |
| `actionlint .github/workflows/build.yml`                                                       | 通过                            | 六平台 CI 与 draft release 配置                                                         |

Go 的条件跳过项验证「未生成嵌入核心时的提示」。本机已放置真实核心，因此该场景在本次运行中跳过；全新检出且尚未准备核心的 CI 会执行它。

Rust 状态测试还验证了环境变量、HTTP 请求头被外部修改时显示不同步；暂停、取消分配或删除后仍残留的托管配置，在重新打开资源库后继续报告错误；Skills 的库目录会等到保存的链接引用全部撤回后再清理。

上游 CLI 的 Cargo 自更新探针不属于桌面更新路径。GUI 的联网版本探针只验证 MCP 包所使用的 npm/PyPI；此项调整记录在 [UPSTREAM.md](../core/UPSTREAM.md)。

## 正式界面的 24 项真实流程

1. 快照不改写已有 Agent 配置。
2. 浏览器测试服务拒绝其他网页来源。
3. 空库引导与未托管资源发现。
4. 通过表单添加 MCP，保留带空格的独立参数与凭据。
5. 拒绝覆盖同名未托管 MCP。
6. 单个 Agent 的取消分配不影响其他目标。
7. 暂停后撤回配置、保留分配，并从矩阵恢复。
8. 重命名时撤回旧键并保留无关配置。
9. 默认配置预览隐藏实际凭据。
10. 明确导入已有 MCP，保留现有字段与分配。
11. 写入失败显示实际错误，修复文件后重试。
12. 扫描本地 Skills、选择安装并创建真实目录链接。
13. SKILL.md 预览来自已安装文件。
14. 更新本地来源后，Agent 链接立即读取新内容。
15. 导入既有 Skill，保存目录备份并建立受管链接。
16. Skill 暂停与恢复保留分配。
17. 取消删除不改变资源。
18. 删除 Skill 撤回链接并移出库。
19. 发现页读取真实 MCP 安装描述符。
20. 通过发现页表单安装 MCP 并验证落盘配置。
21. 深浅主题切换与持久化。
22. 在 960×680 下使用 `1`、`2`、`a`、`Escape` 导航、添加和关闭；对话框操作按钮可见，页面无横向溢出。
23. 当前 MCP 深色页面的 axe 检查：0 个已确认违规。
24. 清空需要确认，并保留未托管内容与另一类资源。

浏览器使用正式 React 界面，通过开发专用 `cmd/testhost` 访问真实 Go 桥接与同一 Rust 核心。生产桌面应用使用 mygo IPC。

## 外部来源

- 官方 MCP Registry 返回实时搜索结果。
- Registry 描述符写入磁盘，重启核心并强制网络失败后仍可从缓存安装，实际 Agent 文件写入成功。
- skills.sh 返回可搜索的实时 Skills。
- 从 `vercel-labs/agent-skills` 的仓库子目录扫描、安装 React Skill，验证内容、更新、撤回链接与删除。

测试仅查询元数据、下载 Skill 内容或写入配置，不启动 MCP 服务。界面中的「已配置」表示配置写入结果。

## macOS 交付产物与原生检查

```text
build/darwin-universal/Siu.app
build/darwin-universal/Siu 0.0.1.dmg
```

应用约 48.0 MiB，DMG 约 15.1 MiB。`lipo -archs` 确认主程序及包内 `bin/siu-core` 都包含 `arm64`、`x86_64`。

已完成：

- `codesign --verify --deep --strict` 验证通过。
- `hdiutil verify` 验证镜像校验和通过。
- 以只读方式挂载 DMG，并从镜像内直接启动应用。
- 原生 WKWebView 窗口加载正式界面，读取隔离样例中的真实配置。
- 核心进程来自该只读应用包的 `Contents/Resources/bin/siu-core`。
- 再次启动返回成功，保留一个可见窗口。
- 正常退出返回 0，核心进程随应用退出。
- 原生窗口截图保存于 `.artifacts/native/01-native-window.png`。

原生 smoke test 覆盖启动、IPC、包内资源、单实例与退出。完成后已关闭测试应用并卸载镜像。系统文件夹对话框的选择操作、Dock 重开和系统主题变化需要进一步人工 GUI 覆盖。本次没有在 Intel 硬件运行应用。

## 平台范围

| 目标                | 本机已执行                                                 | 后续原生 runner 验证                                 |
| ------------------- | ---------------------------------------------------------- | ---------------------------------------------------- |
| macOS ARM64         | Rust/Go 编译、单元/协议/浏览器测试、应用和 DMG、原生 smoke | 已配置                                               |
| macOS Intel         | Rust/Go 编译、合入通用包、架构与签名检查                   | 运行与平台测试                                       |
| Windows x64 / ARM64 | Go 桌面代码交叉编译                                        | Rust 核心、junction 生命周期、WebView2、安装器与运行 |
| Linux x64 / ARM64   | Go 桌面代码交叉编译                                        | Rust GNU 核心、GTK/WebKitGTK、软件包与运行           |

[build.yml](../.github/workflows/build.yml) 为六个目标配置了原生 runner。Linux x64 还执行浏览器端到端流程。本记录只汇总本机阶段的检查。[发布 CI](https://github.com/nicholasxjy/siu-gui/actions/workflows/build.yml) 会在对应原生 runner 上生成其他系统的安装包并运行测试，执行结果以该次发布工作流为准。

## 复现

```sh
bun install --frozen-lockfile
bun run prepare:core
bunx agent-browser install
bun run test:e2e
bun run test:online
```

两个真实流程脚本会各自创建隔离目录。`test:online` 需要外网；常规 Rust 测试通过本地服务和模拟响应验证更新逻辑，联网探针单独运行。

E2E 脚本会构建测试服务，绑定 `127.0.0.1:47832`，以 `SIU_GUI_BROWSER_TEST=1` 启动 `127.0.0.1:5173` 上的 Vite，完成检查后关闭服务和浏览器。可通过 `AGENT_BROWSER_BIN` 替换浏览器 CLI。

原生应用可通过以下方式在新隔离目录中手动验证：

```sh
SIU_GUI_TEST_HOME="$(mktemp -d)" ./build/darwin-universal/Siu.app/Contents/MacOS/Siu
```

## 记录与截图

- `.artifacts/e2e/result.json`：24 项流程结果与隔离目录。
- `.artifacts/e2e/a11y.json`：当前页面的 axe 报告，包括需要人工复核的结果。
- `.artifacts/e2e/01-first-run.png` 至 `06-mcp-small.png`：正式界面截图。
- `.artifacts/online/result.json`：4 项外部来源检查。
- `.artifacts/native/result.json`：从只读 DMG 运行、资源路径、单实例与退出检查。
- `.artifacts/go-tests.jsonl`：Go 竞态检查的逐项结果与条件跳过记录。
- `.artifacts/cross/result.json`：六目标 Go 代码编译结果。

这些记录保留在本机，`.artifacts/` 不进入版本库。此前独立设计稿的原型测试不计入上述正式应用结果。axe 的结果限于当时检查的页面，不能代替全部页面、系统辅助功能或人工交互检查。
