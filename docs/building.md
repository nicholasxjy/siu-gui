# 构建与分发

## 工具链

- Go 1.27+、Bun 1.4+、Node.js 24+、Rust 稳定版、Git。
- macOS 需要 Xcode Command Line Tools；生成 DMG 使用系统 `hdiutil`，签名使用 `codesign`。
- Windows 使用 Rust MSVC 工具链和 Visual Studio C++ Build Tools。mygo 在需要时下载 NSIS 生成安装器。
- Linux 界面运行需要 GTK 3 与 WebKitGTK。Ubuntu 可安装 `libgtk-3-0 libwebkit2gtk-4.1-0`。

```sh
bun install --frozen-lockfile
bun run dev
```

`dev` 首先编译并放置本机 Rust sidecar，再启动 mygo 的窗口、开发服务器和前端热更新。Vite 仅绑定 `127.0.0.1:5173`。

## 本机打包

```sh
bun run build
```

构建脚本依次完成 Rust release 编译、平台资源放置、mygo 绑定生成、TypeScript 检查、前端构建和 Go 打包。应用版本来自 `package.json`；核心有自己的 Cargo 版本和上游来源记录。

| 平台          | 默认输出目录           | 主要产物                                |
| ------------- | ---------------------- | --------------------------------------- |
| macOS ARM64   | `build/darwin-arm64/`  | `Siu.app`、`Siu 0.0.1 arm64.dmg`        |
| macOS Intel   | `build/darwin-amd64/`  | `.app`、`.dmg`                          |
| Windows x64   | `build/windows-amd64/` | 可执行目录、NSIS 安装器                 |
| Windows ARM64 | `build/windows-arm64/` | 可执行目录、NSIS 安装器                 |
| Linux x64     | `build/linux-amd64/`   | 可执行目录、`.deb`、`.tar.gz`、安装脚本 |
| Linux ARM64   | `build/linux-arm64/`   | 可执行目录、`.deb`、`.tar.gz`、安装脚本 |

Windows 的应用可执行文件需要同目录的资源；分发时使用安装器或完整目录。Linux 归档里的安装脚本不会代替系统安装 WebKitGTK。

## 平台目标

```sh
bun run build --platform darwin/arm64
bun run build --platform darwin/amd64
bun run build --platform windows/amd64
bun run build --platform windows/arm64
bun run build --platform linux/amd64
bun run build --platform linux/arm64
```

Go/mygo 支持交叉编译，Rust sidecar 还需要对应目标的标准库、链接器与 SDK。可靠的发布流程是在目标系统与架构的 runner 上构建；仓库的 [build.yml](../.github/workflows/build.yml) 为六个目标分别配置了原生 runner。

在 macOS 上可生成通用应用：

```sh
bun run build --platform darwin/universal
```

这会编译两种 Rust 核心，并合并嵌入的备用核心。mygo 自动将两个平台资源目录中的 sidecar 合并为通用二进制，同时合并 Go 主程序。该流程需要 macOS 的 SDK 与 `lipo`。

Linux 的 Rust 核心默认使用 GNU/glibc，与桌面 GTK/WebKitGTK 环境一致。发行构建使用原生 Linux runner：x64 为 Ubuntu 22.04，ARM64 为 Ubuntu 24.04。

构建脚本会在本机目标编译完成后，用隔离目录执行发行核心并读取真实 `snapshot`；启动失败或版本不一致会阻止打包。x86_64 musl 在首轮 CI 中出现启动崩溃，因此发行版使用 GNU。`CORE_LINUX_LIBC=musl` 仅供自备工具链的定制构建，并须通过同样的启动检查。

GNU 产物受构建机 glibc 版本限制。Linux 图形运行环境需要 GTK 3、WebKitGTK 4.1；中文字体可安装 `fonts-noto-cjk`。

## 核心的放置与生命周期

`bun run prepare:core` 编译本机目标，使用 Cargo lockfile，并生成：

```text
resources/<os>-<arch>/bin/siu-core[.exe]
internal/backend/bundle/siu-core.bin
```

mygo 打包时将当前平台资源平铺到应用的资源目录。Go 优先使用 `bin/siu-core`，使它能随应用一起签名；开发和直接 `go run` 时可以使用嵌入的备用副本。`SIU_GUI_CORE` 是显式的开发/测试覆盖。

核心按需启动，使用 stdin/stdout NDJSON 通信。请求通过编号匹配；崩溃不自动重试写入，下次请求可以重启进程；取消或超时会明确提示结果未知。应用退出时关闭 stdin，必要时回收整个进程树。

生产应用通过 mygo IPC 工作，不启动 HTTP 服务器。`cmd/testhost` 是独立测试程序，要求隔离目录，未编入生产应用。

## 签名与发布

本机 macOS 构建使用 ad-hoc 签名，可用于本机验证。对外分发需要按 [mygo 的签名与公证说明](https://github.com/egoist/mygo/blob/main/docs/distribution.md) 配置 Developer ID、公证或 Windows 代码签名；CI 没有嵌入任何证书。

推送 `v*` 标签时，工作流在所有构建成功后创建 **draft release**，并附上 DMG、Windows 安装器和 Linux 包。工作流本身不会发布草稿，也不会自动上传本机产物。

`scripts/release.ts` 从目标构建目录选择当前版本的安装包，输出带平台与架构的唯一名称，例如 `siu-gui_0.0.1_macos_arm64.dmg`、`siu-gui_0.0.1_windows_x64_setup.exe`。缺少包或出现重复候选会阻止发布。CI 会为全部资产生成 `SHA256SUMS.txt`。确认草稿内容后可正式发布；本机生成的 macOS 通用包可以按同样命名规则追加到该版本。

## 验证与许可证

```sh
bun run test
bun run test:build
cargo test --locked --manifest-path core/Cargo.toml
go test -race ./...
bun run build:web
```

Windows ARM64 的 CI 运行 `go test ./...`，其余目标运行竞态检测。可用平台与本次实际执行范围见 [verification.md](verification.md)。

```sh
bun run licenses
```

该命令根据已安装的生产前端依赖、Go 模块和 Cargo lockfile 生成 `resources/THIRD_PARTY_LICENSES.txt`。文件随应用资源分发；添加或更新依赖后应重新生成。
