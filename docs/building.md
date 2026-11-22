# 构建与分发

## 工具链依赖

- Go 1.27.3+、Bun 1.4.4+、Node.js 26+（也支持 24.15+ 的 24.x）、Git。
- macOS：Xcode Command Line Tools（`hdiutil` 生成 DMG，`codesign` 签名）。
- Windows：mygo 按需下载 NSIS 生成安装器。
- Linux：GTK 3 与 WebKitGTK（`libgtk-3-0 libwebkit2gtk-4.1-0`）；中文字体可安装 `fonts-noto-cjk`。

## 开发与构建

```sh
bun install --frozen-lockfile
bun run dev      # 启动 mygo 桌面窗口、开发服务器和前端热更新
bun run build    # mygo 绑定 → TS 检查 → 前端构建 → Go 打包
```

Vite 仅绑定 `127.0.0.1:5173`；应用版本来自 `package.json`。

## 产物路径

| 平台          | 输出目录               | 主要产物                                |
| ------------- | ---------------------- | --------------------------------------- |
| macOS ARM64   | `build/darwin-arm64/`  | `Siu.app`、DMG                          |
| macOS Intel   | `build/darwin-amd64/`  | `Siu.app`、DMG                          |
| Windows x64   | `build/windows-amd64/` | 可执行目录、NSIS 安装器                 |
| Windows ARM64 | `build/windows-arm64/` | 可执行目录、NSIS 安装器                 |
| Linux x64     | `build/linux-amd64/`   | 可执行目录、`.deb`、`.tar.gz`、安装脚本 |
| Linux ARM64   | `build/linux-arm64/`   | 可执行目录、`.deb`、`.tar.gz`、安装脚本 |

Windows 分发时使用安装器或完整目录；Linux 安装脚本不代替系统安装 WebKitGTK。

## 指定平台与 Universal

```sh
bun run build --platform darwin/arm64
bun run build --platform darwin/universal   # 需要 macOS SDK 与 lipo
```

六个平台目标：`darwin/arm64`、`darwin/amd64`、`windows/amd64`、`windows/arm64`、`linux/amd64`、`linux/arm64`；发布工作流另构建 `darwin/universal`，同时提供 macOS 通用安装包。核心逻辑位于 `internal/core`，随 Go 主程序直接编译进应用二进制，不再需要单独编译或分发 sidecar；可靠的发布流程在目标系统原生 runner 上构建，见 [build.yml](../.github/workflows/build.yml)。

Universal 由 mygo 自动编译两种架构的 Go 主程序并合并为通用二进制。构建后使用 macOS `ditto` 将 `Siu.app` 打包为 ZIP，保留应用资源、符号链接和签名，产物位于 `build/darwin-universal/`。

## 进程内 Go 核心

siu 核心（`internal/core`）在 mygo 主进程内运行，通过绑定的 `Desktop.Request` 处理 JSON 协议请求：读操作与网络查询可并发执行，写操作通过互斥锁串行化并在每次请求时重新打开资源库，与 `siu` CLI 共享 `~/.siu` 数据。应用退出时自动清理扫描残留的临时目录。生产应用通过 mygo IPC 工作，不启动 HTTP 服务器；`cmd/testhost` 是独立测试程序，未编入生产应用。

## 应用图标

`resources/icon.svg` 是桌面图标、界面标志和 favicon 的唯一设计源文件。修改后安装 librsvg（macOS：`brew install librsvg`；Debian/Ubuntu：`sudo apt install librsvg2-bin`）并重新生成：

```sh
bun run generate:icon
```

打包时自动生成 macOS ICNS（最高 1024px）、Windows 七档图标（16–256px）和 Linux 桌面图标。普通开发和 CI 直接使用已提交的 PNG，无需安装 librsvg。

## 签名与发布

本机 macOS 构建使用 ad-hoc 签名，可用于本机验证。对外分发需按 [mygo 签名说明](https://github.com/egoist/mygo/blob/main/docs/distribution.md) 配置 Developer ID 及公证；CI 不嵌入任何证书。

推送 `v*` 标签后，工作流在所有构建成功后创建 **draft release** 并附上各平台安装包，不自动发布。发布脚本（`scripts/release.ts`）从构建目录选取当前版本安装包，输出带平台与架构的唯一文件名（如 `siu-gui_<version>_macos_arm64.dmg`）；缺失包或重复候选会阻止发布。CI 确认九个安装包和 macOS Universal ZIP 齐全后，为全部资产生成 `SHA256SUMS.txt`。

## 许可证

更新依赖后运行 `bun run licenses`，重新生成随应用分发的 `resources/THIRD_PARTY_LICENSES.txt`。

测试命令与验证范围见[验证指南](verification.md)。
