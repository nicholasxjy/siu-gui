# siu GUI

[siu](https://github.com/nicholasxjy/siu) 的桌面界面，统一管理 coding agent 的 MCP 服务与 Skills。基于 [mygo](https://github.com/egoist/mygo)、React 和 siu Rust 核心，使用系统 WebView。

[下载安装包](https://github.com/nicholasxjy/siu-gui/releases/latest) · [更新记录](CHANGELOG.md)

## Demo

MCP 服务：通过「资源 × Agent」矩阵分配工具，查看配置与同步状态。

![MCP 服务管理与 Agent 分配矩阵（浅色主题）](docs/images/mcp-light.png)

<details>
<summary>Skills 管理 · 深色主题</summary>

![Skills 来源分组与 Agent 分配（深色主题）](docs/images/skills-dark.png)

</details>

截图使用隔离示例数据，由真实界面与 Go/Rust 后端运行生成。

## 功能

- **MCP 管理**：添加、编辑、导入、分配、暂停和更新服务，支持 stdio、HTTP、SSE。
- **Skills 管理**：扫描 GitHub 仓库或本地目录，选择安装、按来源分组、预览 SKILL.md 并更新。
- **资源发现**：搜索 MCP Registry 与 skills.sh，按安装描述符填写配置，网络异常时提示缓存回退。
- **本机集成**：检测 Agent、共享 siu CLI 资源库，支持深浅主题、系统或手动代理和原生文件夹选择。

支持 macOS、Windows、Linux 的 x64 / ARM64；macOS 另提供通用构建。平台依赖见[构建说明](docs/building.md)。

## 使用说明

- 资源库默认在 `~/.siu`，可用 `SIU_HOME` 指定。与 siu CLI 共用数据；启动和刷新只读取配置。
- 修改配置前备份原文件并保留无关内容；同名未托管资源需明确导入。备份保存在资源库的 `backups/`。
- 「已配置」表示配置写入成功，Agent 的重载与 MCP 运行状态由对应 Agent 决定。
- 凭据预览默认遮蔽，本机配置文件仍保存实际值。网络策略可在「设置 → 网络」中切换并立即生效；Docker 镜像下载需另配 Docker 引擎代理。

## 开发

工具链与平台依赖见[构建说明](docs/building.md)。

```sh
bun install --frozen-lockfile
bun run dev
```

`dev` 会编译 Rust 核心并启动桌面窗口；单独运行 `dev:web` 无法访问本机配置。`bun run build` 的产物位于 `build/<os>-<arch>/`。

- [构建与分发](docs/building.md)：工具链、打包、签名与发布。
- [验证指南](docs/verification.md)：测试命令、隔离环境与验证范围。
- [接口协议](docs/protocol.md)：桌面 IPC、核心方法与数据结构。
- [上游来源](core/UPSTREAM.md)：固定版本与桌面适配。

## 许可

[MIT](LICENSE)。第三方组件与上游许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
