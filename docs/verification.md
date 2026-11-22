# 测试与原生验证

## 常规检查

在仓库根目录运行。前端检查需要先执行 `bun install --frozen-lockfile`。 [工具链与平台依赖](building.md)

| 命令                                 | 覆盖范围                                                                          |
| ------------------------------------ | --------------------------------------------------------------------------------- |
| `go test -race ./...`                | Go 核心、桌面绑定、终端交互和测试服务。Windows ARM64 使用 `go test ./...`。       |
| `go test -race ./cmd/siu-tui`        | 在模拟终端中发送按键，验证 MCP 表单与凭据遮蔽、Skill 扫描与安装、备份确认和恢复。 |
| `go test ./internal/core`            | MCP 配置读写、Skills 链接、代理、发现缓存、指令文件、备份兼容与回滚。             |
| `bun run typecheck` / `bun run test` | TypeScript 类型和桌面界面的组件与行为。                                           |
| `bun run test:build`                 | 平台参数、独立终端包、发布命名与归集。                                            |
| `bun run build:web`                  | 生产前端构建。                                                                    |

Go 备份兼容样例位于 [internal/core/testdata/siu-backup-v1.json](../internal/core/testdata/siu-backup-v1.json)。测试覆盖上游导出的 v1 数据、路径映射、自动恢复摘要和 TUI 对指令文件的隔离。

## 用隔离目录手动验证

使用独立的绝对路径作为 `SIU_GUI_TEST_HOME`。共享核心会在其中查找 Agent 配置，并将资源库放在该目录下的 `.siu`，忽略日常的 `SIU_HOME` 和 Agent 路径环境变量。下面的示例预建一个 Claude Code 测试目录，可用于验证分配和实际写入。

```sh
siu_test_home="$(mktemp -d)"
mkdir -p "$siu_test_home/.claude"
SIU_GUI_TEST_HOME="$siu_test_home" go run ./cmd/siu-tui
```

也可以在解压目录用 `SIU_GUI_TEST_HOME="$siu_test_home" ./siu-tui` 测试发行包。桌面版使用相同环境变量，例如 `SIU_GUI_TEST_HOME="$siu_test_home" ./build/darwin-arm64/Siu.app/Contents/MacOS/Siu`。若要验证桌面版和终端版共用资源库，先退出第一个客户端，再用同一路径启动第二个。

## 浏览器与联网测试

```sh
bunx agent-browser install
bun run test:e2e
bun run test:online
```

- **浏览器 E2E** 通过 `agent-browser` 驱动 React 界面，并调用独立的 Go 测试服务。用例覆盖资源工作流、部分键盘与无障碍交互、备份和主题。原生文件选择器由测试入口代替，配置文件读写走真实 Go 核心。可用 `AGENT_BROWSER_BIN` 指定浏览器 CLI。Vite 使用 `127.0.0.1:5173`，测试服务使用 `127.0.0.1:47832`。
- **联网测试** 使用同一个 Go 测试服务，覆盖官方 MCP Registry、缓存后离线安装、skills.sh、GitHub Skill、代理设置和 npm / PyPI 版本查询。运行时需要访问外网，并占用 `127.0.0.1:47832`。

两个脚本都会创建隔离测试目录。浏览器记录、截图和无障碍报告在 `.artifacts/e2e/`，联网结果在 `.artifacts/online/result.json`。

## 发布包验证

- **终端版** 解包后检查 `siu-tui` 或 `siu-tui.exe`、`LICENSE` 和 `THIRD_PARTY_LICENSES.txt`。运行 `--version` 核对版本，再在目标系统的终端或 SSH 中用隔离目录验证界面和资源写入。
- **macOS 桌面版** 用 `codesign --verify --deep --strict` 检查 `Siu.app`，用 `hdiutil verify` 校验 DMG，用 `lipo -archs` 检查应用可执行文件的架构。需要验证原生对话框、WebView 和单实例行为时，直接启动对应的桌面包。
- **CI** 在六个单架构目标及 macOS Universal runner 上构建。单架构任务会解包并执行终端程序的 `--version`，Linux x64 另运行浏览器 E2E。 [工作流](../.github/workflows/build.yml) · [运行记录](https://github.com/nicholasxjy/siu-gui/actions/workflows/build.yml)
