# siu GUI

[siu](https://github.com/nicholasxjy/siu) 的桌面界面，统一管理 coding agent 的 MCP 服务与 Skills。基于 [mygo](https://github.com/egoist/mygo)、React 和 siu 的 Rust 核心，使用系统 WebView。

## 功能

- 以「资源 × Agent」矩阵分配 MCP 与 Skills，查看实际配置结果、兼容性和写入问题。
- 添加、编辑、导入、暂停、恢复、更新和删除 MCP；支持 stdio、HTTP、SSE，以及独立参数、环境变量和请求头。
- 从 GitHub、仓库子目录或本地文件夹扫描 Skills，选择安装、按来源分组、查看 SKILL.md、更新与撤回分配。
- 搜索 MCP Registry 与 skills.sh；从真实安装描述符生成表单，网络异常时明确提示缓存回退。
- 检测本机 Agent，查看配置位置；支持共享 Skills 目录、深浅主题、原生文件夹选择器和键盘操作。
- 与 siu 终端版共享资源库。启动与刷新只读取配置；分配、导入和同步由明确的用户操作触发。

配置状态表示写入结果。Agent 是否已重新载入配置、MCP 进程是否在线，由对应 Agent 决定。

## 开发

需要 Go 1.27+、Bun 1.4+、Node.js 24+、Rust 稳定版和 Git。首次运行会编译 Rust sidecar。

```sh
bun install --frozen-lockfile
bun run dev
```

`dev` 会准备本机 `siu-core`，然后由 mygo 启动桌面窗口和前端开发服务器。单独运行 `dev:web` 不能访问真实本机配置；生产应用通过 mygo IPC 调用后端。

```sh
bun run build
```

构建产物写入 `build/<os>-<arch>/`。macOS 可用 `bun run build --platform darwin/universal` 生成通用应用与 DMG。本次本机产物位于 `build/darwin-universal/`。平台依赖、打包、签名和跨平台构建说明见 [docs/building.md](docs/building.md)。

## 平台

| 平台    | 架构                  | 界面运行时         |
| ------- | --------------------- | ------------------ |
| macOS   | Apple Silicon / Intel | 系统 WKWebView     |
| Windows | x64 / ARM64           | Microsoft WebView2 |
| Linux   | x64 / ARM64           | GTK 3 与 WebKitGTK |

仓库包含六个平台目标的 CI 构建配置。macOS 通用包已生成，并在 Apple Silicon 上验证了原生启动、IPC、单实例与退出。Windows/Linux 的运行和安装器需要各自的 CI runner 验证，具体结果见 [docs/verification.md](docs/verification.md)。

## 数据与配置保护

默认资源库在 `~/.siu`，可通过 `SIU_HOME` 指定，与 siu CLI 使用相同的数据格式。Agent 配置路径遵循 siu 的环境变量设置。

首次修改配置前备份原文件；保留无关配置；已有同名但未托管的条目必须明确导入。Skills 使用指向资源库的目录链接，Windows 使用 junction。暂停保留资源定义与分配，删除撤回托管配置，清空操作需要确认。备份目录为资源库内的 `backups`。

凭据的配置预览默认遮蔽，编辑表单可单独显示。资源库和 Agent 配置仍是包含实际凭据的本机文件，沿用对应工具的存储格式。

网络遵循 `HTTP_PROXY`、`HTTPS_PROXY`、`ALL_PROXY` 和 `NO_PROXY`。应用更新通过发布包完成；MCP/Skills 的「更新」不会通过 Cargo 重装桌面应用。

## 测试

```sh
bun run typecheck
bun run test
bun run test:build
bun run test:core
go test -race ./...
bun run build:web
```

真实流程可通过 `bun run test:e2e` 和 `bun run test:online` 复现；两个脚本会自动创建隔离目录。首次运行前准备核心与浏览器，命令见 [验证记录](docs/verification.md)。

真实配置测试必须设置绝对路径的 `SIU_GUI_TEST_HOME`。该模式将资源库、Agent 检测与写入限制在隔离目录，并忽略指向真实 Agent 配置的路径覆盖。不得使用日常 HOME 执行会修改资源的测试。

开发专用的 `cmd/testhost` 将真实 Go/Rust 后端提供给浏览器验证，必须显式设置测试目录，只监听 loopback，并校验请求来源。启动方式与验证记录见 [docs/verification.md](docs/verification.md)。它不包含在桌面应用中。

## 结构

```text
src/                   React 界面、表单与前端测试
main.go, desktop.go    mygo 窗口、生命周期和原生 IPC
internal/backend/      NDJSON 进程桥接与平台进程管理
core/                  siu-core 服务及固定版本的 siu 上游模块
resources/             图标、许可证与构建时生成的平台 sidecar
scripts/               核心编译与打包
tools/                 第三方许可证收集
cmd/testhost/          仅用于隔离验证的本机测试服务
docs/                  协议、构建与验证说明
```

接口约定见 [docs/protocol.md](docs/protocol.md)。上游版本与必要的桌面适配见 [core/UPSTREAM.md](core/UPSTREAM.md)。

## 许可

MIT。siu 上游许可随源码保留在 `core/upstream/LICENSE`；主要组件说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
