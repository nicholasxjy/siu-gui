# 构建与分发

构建目标使用 Go 的 `os/arch` 命名。`--platform` 默认取当前系统和架构，也接受逗号分隔的目标列表。发布文件名用 `macos` 和 `x64` 对应构建参数中的 `darwin` 和 `amd64`。应用版本取自 `package.json`。

## 终端版

在仓库根目录执行。运行源码只需 Go 1.27.3+，生成独立归档另需 Bun 1.4.4+ 和 BSD tar。macOS 和 Windows 使用系统自带的 `tar`，Debian/Ubuntu 可安装 `libarchive-tools` 提供 `bsdtar`。构建脚本可直接运行，无需先安装前端依赖。

```sh
go run ./cmd/siu-tui
bun run build:tui
bun run build:tui --platform linux/amd64,linux/arm64
```

支持 `darwin`、`windows`、`linux`，每个平台提供 `amd64` 和 `arm64`。构建使用 `CGO_ENABLED=0`，可在一个系统上交叉编译其他目标。按架构构建的归档位于 `build/tui/<goos>-<goarch>/`，文件名形如 `siu-tui_<version>_linux_arm64.tar.gz`。macOS 和 Linux 使用 `.tar.gz`，Windows 使用 `.zip`，包内包含可执行文件和许可。发行包的 `--version` 显示 `package.json` 中的版本。

## 桌面版

桌面开发需要 Go 1.27.3+、Bun 1.4.4+、Node.js 26（或 Node.js 24.15+ 的 24.x）和 Git。

| 构建主机 | 平台依赖                                                                                                                                                          |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| macOS    | Xcode Command Line Tools。DMG 由 `hdiutil` 生成，Universal 还需要 `lipo` 和 `ditto`。                                                                             |
| Windows  | mygo 按需下载 NSIS。桌面程序通过 WebView2 运行。                                                                                                                  |
| Linux    | GTK 3 和 WebKitGTK 4.1。在 Ubuntu 上安装 `libgtk-3-0`、`libwebkit2gtk-4.1-0`。需要中文字体时可安装 `fonts-noto-cjk`。普通单架构构建还需要 `bsdtar` 来生成终端包。 |

```sh
bun install --frozen-lockfile
bun run dev
bun run build --platform darwin/arm64
bun run build --platform darwin/universal
```

`bun run dev` 启动 mygo 窗口和本地 Vite 开发服务（`127.0.0.1:5173`）。`bun run build` 生成绑定、构建前端、编译 Go 主程序并打包，同时输出对应架构的独立终端包。桌面发布使用目标系统上的原生 runner。macOS Universal 目标合并两种架构的桌面程序，再用 `ditto` 生成 `.app` ZIP。macOS 的两个终端包使用各自的单架构目标构建。

| 目标                             | 桌面产物目录              | 主要产物                                |
| -------------------------------- | ------------------------- | --------------------------------------- |
| `darwin/arm64`、`darwin/amd64`   | `build/darwin-<arch>/`    | `Siu.app`、DMG                          |
| `darwin/universal`               | `build/darwin-universal/` | Universal `Siu.app`、DMG 和 ZIP         |
| `windows/amd64`、`windows/arm64` | `build/windows-<arch>/`   | 可执行目录、NSIS 安装器                 |
| `linux/amd64`、`linux/arm64`     | `build/linux-<arch>/`     | 可执行目录、`.deb`、`.tar.gz`、安装脚本 |

修改 `resources/icon.svg` 后，可用 librsvg 重新生成图标：macOS 安装 `librsvg`，Debian/Ubuntu 安装 `librsvg2-bin`，然后运行 `bun run generate:icon`。普通构建直接使用仓库内的 PNG。

## 签名与发布

macOS 桌面版本地构建使用 ad-hoc 签名。正式分发需要按 mygo 文档配置 Developer ID 签名和公证。 [mygo 分发说明](https://github.com/egoist/mygo/blob/v0.10.0/docs/distribution.md)

`bun scripts/release.ts --platform <os>/<arch>` 将该目标的桌面包与对应终端归档归集到 `release-assets/`。例如在 macOS ARM64 构建后运行 `bun scripts/release.ts --platform darwin/arm64`。单架构归集要求两类产物都已生成。Universal 归集包含桌面 DMG 和 ZIP。

推送与 `package.json` 版本一致的 `v*` 标签后，GitHub Actions 构建各平台产物、检查六个终端包及十个桌面包，并生成 `SHA256SUMS.txt`。随后创建草稿 Release，由维护者确认后发布。更新依赖后，运行 `bun run licenses` 生成随包分发的 `resources/THIRD_PARTY_LICENSES.txt`。 [发布工作流](../.github/workflows/build.yml) · [验证指南](verification.md)
