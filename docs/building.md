# 构建与分发

## 工具链依赖

- Go 1.27+、Bun 1.4+、Node.js 24+、Rust 稳定版、Git。
- macOS：Xcode Command Line Tools（`hdiutil` 生成 DMG，`codesign` 签名）。
- Windows：Rust MSVC 工具链与 Visual Studio C++ Build Tools；mygo 按需下载 NSIS 生成安装器。
- Linux：GTK 3 与 WebKitGTK（`libgtk-3-0 libwebkit2gtk-4.1-0`）；中文字体可安装 `fonts-noto-cjk`。

## 开发与构建

```sh
bun install --frozen-lockfile
bun run dev      # 编译并放置本机 Rust sidecar，再启动 mygo 窗口、开发服务器和前端热更新
bun run build    # release 编译 → 资源放置 → mygo 绑定 → TS 检查 → 前端构建 → Go 打包
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

六个平台目标：`darwin/arm64`、`darwin/amd64`、`windows/amd64`、`windows/arm64`、`linux/amd64`、`linux/arm64`；发布工作流另构建 `darwin/universal`，同时提供 macOS 通用安装包。Go/mygo 支持交叉编译，Rust sidecar 需要对应目标的标准库和链接器；可靠的发布流程在目标系统原生 runner 上构建，见 [build.yml](../.github/workflows/build.yml)。

Universal 会编译两种 Rust 核心，mygo 自动将两个平台的 sidecar 和 Go 主程序合并为通用二进制。

## Rust Sidecar

```sh
bun run prepare:core   # 编译本机目标（使用 Cargo lockfile）
```

生成：

```
resources/<os>-<arch>/bin/siu-core[.exe]
internal/backend/bundle/siu-core.bin
```

mygo 打包时将当前平台资源平铺到应用资源目录。Go 优先使用 `bin/siu-core`（随应用一起签名）；开发或 `go run` 时可使用嵌入的备用副本。`SIU_GUI_CORE` 可显式覆盖，供开发和测试使用。

核心按需启动，通过 stdin/stdout NDJSON 与 Go 通信。崩溃不自动重试写入，下次请求可重启进程；取消或超时会明确提示结果未知。应用退出时关闭 stdin，必要时回收整个进程树。生产应用通过 mygo IPC 工作，不启动 HTTP 服务器；`cmd/testhost` 是独立测试程序，未编入生产应用。

## GNU/glibc

Linux Rust 核心默认使用 GNU/glibc，与 GTK/WebKitGTK 环境一致。发行构建使用原生 Linux runner（x64：Ubuntu 22.04；ARM64：Ubuntu 24.04）。构建脚本会在本机目标编译后，以隔离目录启动核心并检查版本，失败则阻止打包。`CORE_LINUX_LIBC=musl` 仅供自备工具链的定制构建，须通过同样的启动检查。GNU 产物受构建机 glibc 版本限制。

## 应用图标

`resources/icon.svg` 是桌面图标、界面标志和 favicon 的唯一设计源文件。修改后安装 librsvg（macOS：`brew install librsvg`；Debian/Ubuntu：`sudo apt install librsvg2-bin`）并重新生成：

```sh
bun run generate:icon
```

打包时自动生成 macOS ICNS（最高 1024px）、Windows 七档图标（16–256px）和 Linux 桌面图标。普通开发和 CI 直接使用已提交的 PNG，无需安装 librsvg。

## 签名与发布

本机 macOS 构建使用 ad-hoc 签名，可用于本机验证。对外分发需按 [mygo 签名说明](https://github.com/egoist/mygo/blob/main/docs/distribution.md) 配置 Developer ID 及公证；CI 不嵌入任何证书。

推送 `v*` 标签后，工作流在所有构建成功后创建 **draft release** 并附上各平台安装包，不自动发布。发布脚本（`scripts/release.ts`）从构建目录选取当前版本安装包，输出带平台与架构的唯一文件名（如 `siu-gui_<version>_macos_arm64.dmg`）；缺失包或重复候选会阻止发布。CI 确认九个安装包齐全后，为全部资产生成 `SHA256SUMS.txt`。

## 许可证

更新依赖后运行 `bun run licenses`，重新生成随应用分发的 `resources/THIRD_PARTY_LICENSES.txt`。

测试命令与验证范围见[验证指南](verification.md)。
