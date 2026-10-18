# 验证指南

## 常规检查命令

```sh
bun run typecheck
bun run test
bun run test:build
cargo test --locked --manifest-path core/Cargo.toml
go test -race ./...
bun run build:web
```

依次检查 TypeScript、前端、构建与发布脚本、Rust 单元与 NDJSON 协议、Go 竞态和生产前端构建。Windows ARM64 使用 `go test ./...`。

## 隔离路径

E2E 和联网测试脚本均自动创建临时隔离目录。手动测试配置读写时，必须将 `SIU_GUI_TEST_HOME` 设为不同于日常 HOME 的绝对路径；该模式会忽略指向真实 Agent 配置的路径覆盖。原生应用可在新隔离目录中启动：

```sh
SIU_GUI_TEST_HOME="$(mktemp -d)" ./build/darwin-universal/Siu.app/Contents/MacOS/Siu
```

`SIU_GUI_CORE` 可显式指定核心路径，供开发或测试覆盖。Rust 单元测试使用等价的 `Env::rooted(tempdir)`。

## E2E 运行前提

```sh
bun install --frozen-lockfile
bun run prepare:core
bunx agent-browser install
bun run test:e2e
```

E2E 脚本构建测试服务（`cmd/testhost`），绑定 `127.0.0.1:47832`；以 `SIU_GUI_BROWSER_TEST=1` 启动 Vite（`127.0.0.1:5173`），完成后关闭服务和浏览器。可通过 `AGENT_BROWSER_BIN` 替换浏览器 CLI。

`bun run test:online` 需要外网。Rust 联网探针（`online_mcp_package_versions`）默认忽略，单独运行：

```sh
cargo test --locked --manifest-path core/Cargo.toml online_mcp_package_versions -- --ignored
```

## macOS 原生验证命令

```sh
codesign --verify --deep --strict <path>/Siu.app
hdiutil verify <path>/Siu.dmg
lipo -archs <path>/Siu.app/Contents/MacOS/Siu
lipo -archs <path>/Siu.app/Contents/Resources/bin/siu-core
```

## 已验证范围

E2E 覆盖：真实界面与 Go/Rust 后端、MCP 全生命周期（添加/分配/暂停/重命名/删除）、Skills 扫描/安装/更新/删除、主题切换、键盘导航、基本可访问性（axe）、隔离写入与错误处理。

联网测试覆盖：官方 MCP Registry 搜索与离线缓存、skills.sh、GitHub Skill 扫描/安装/更新/删除；代理测试另覆盖 HTTP CONNECT、SOCKS5、系统代理切换及联网 MCP 版本探针。

此前在 macOS / Apple Silicon 上完成 Rust/Go 编译、单元/协议/浏览器测试、macOS ARM64 与通用包的构建、签名及镜像校验，并从只读 DMG 验证原生窗口、IPC、单实例和正常退出。

Intel 硬件运行、Windows/Linux 原生运行与安装器、Windows junction、各平台系统代理及 PAC/WPAD 的覆盖以对应环境的实际验证结果为准。

## 结果与截图

自动测试记录保存在 `.artifacts/`（不进入版本库）：

- `e2e/result.json`、界面截图及当前页面的 `a11y.json`。
- `online/result.json`：外部来源检查结果。

原生窗口、包签名和其他手动验证的记录可另存于此。README 展示用截图保存在 [`docs/images/`](images/)，使用隔离示例数据。axe 检查仅覆盖当时页面，不能代替全量或人工验证。

## 各平台 CI

[build.yml](../.github/workflows/build.yml) 为六个目标配置了原生 runner；Linux x64 另执行浏览器 E2E。发布工作流结果见 [GitHub Actions](https://github.com/nicholasxjy/siu-gui/actions/workflows/build.yml)，以各次发布 CI 的实际执行结果为准。
