#!/usr/bin/env bun
// Builds the Rust siu-core sidecar (core/) in release mode and stages it
// where the Go side expects it to find it:
//
//   - resources/<goos>-<goarch>/bin/siu-core(.exe)  the packaged resources
//     mygo ships the app with (docs/distribution.md#platform-resources;
//     backend.NewWithExecutable prefers this over the embedded fallback).
//   - internal/backend/bundle/siu-core.bin           the go:embed fallback
//     for `go run`/`go test`/`mygo dev` before resources exist for every
//     target (internal/backend/embed.go); only staged for the *host*
//     platform, since the embed is whatever bytes are on disk when `go
//     build` runs for a given GOOS/GOARCH.
//
// `bun run prepare:core` (this script, no args) builds for the host
// platform. `bun run build` (scripts/build.ts) calls buildCore for every
// platform it packages before invoking `mygo build`. Every process here
// is spawned with an argv array -- no shell is ever used.

import { access, chmod, copyFile, mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

export const REPO_ROOT = fileURLToPath(new URL("..", import.meta.url));

export type GoOS = "darwin" | "linux" | "windows";
export type GoArch = "arm64" | "amd64";

export interface Target {
  goos: GoOS;
  goarch: GoArch;
}

export const HOST: Target = hostTarget();

function hostTarget(): Target {
  const goos: GoOS =
    process.platform === "darwin"
      ? "darwin"
      : process.platform === "win32"
        ? "windows"
        : "linux";
  const goarch: GoArch = process.arch === "arm64" ? "arm64" : "amd64";
  return { goos, goarch };
}

function rustTriple(target: Target, libc: "musl" | "gnu"): string {
  switch (target.goos) {
    case "darwin":
      return target.goarch === "arm64"
        ? "aarch64-apple-darwin"
        : "x86_64-apple-darwin";
    case "windows":
      return target.goarch === "arm64"
        ? "aarch64-pc-windows-msvc"
        : "x86_64-pc-windows-msvc";
    case "linux": {
      const arch = target.goarch === "arm64" ? "aarch64" : "x86_64";
      return `${arch}-unknown-linux-${libc}`;
    }
  }
}

function coreBinaryName(goos: GoOS): string {
  return goos === "windows" ? "siu-core.exe" : "siu-core";
}

/** Absolute path of resources/<goos>-<goarch>/bin/<binary>, where buildCore
 *  stages the sidecar for target. */
export function resourceBinPath(target: Target): string {
  return join(
    REPO_ROOT,
    "resources",
    `${target.goos}-${target.goarch}`,
    "bin",
    coreBinaryName(target.goos),
  );
}

/** Runs cmd (an argv array; no shell) and throws if it exits non-zero. */
export async function run(
  cmd: string[],
  cwd: string = REPO_ROOT,
): Promise<void> {
  const proc = Bun.spawn(cmd, {
    cwd,
    stdin: "inherit",
    stdout: "inherit",
    stderr: "inherit",
  });
  const code = await proc.exited;
  if (code !== 0) {
    throw new Error(`command failed (exit ${code}): ${cmd.join(" ")}`);
  }
}

async function tryRunQuiet(
  cmd: string[],
  cwd: string = REPO_ROOT,
): Promise<boolean> {
  try {
    const proc = Bun.spawn(cmd, {
      cwd,
      stdin: "ignore",
      stdout: "ignore",
      stderr: "ignore",
    });
    return (await proc.exited) === 0;
  } catch {
    return false;
  }
}

export async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

async function verifyCoreStartup(path: string): Promise<void> {
  const home = await mkdtemp(join(tmpdir(), "siu-core-release-"));
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const process = Bun.spawn([path], {
      env: { ...globalThis.process.env, SIU_GUI_TEST_HOME: home },
      stdin: "pipe",
      stdout: "pipe",
      stderr: "pipe",
    });
    timeout = setTimeout(() => process.kill("SIGKILL"), 15_000);
    process.stdin.write(
      JSON.stringify({ id: 1, method: "snapshot", params: {} }) + "\n",
    );
    process.stdin.end();
    const [code, stdout, stderr] = await Promise.all([
      process.exited,
      new Response(process.stdout).text(),
      new Response(process.stderr).text(),
    ]);
    if (code !== 0)
      throw new Error(`release siu-core exited ${code}: ${stderr}`);
    const response = JSON.parse(stdout.trim());
    const version = (await Bun.file(join(REPO_ROOT, "package.json")).json())
      .version;
    if (response.id !== 1 || !response.ok || response.data?.version !== version)
      throw new Error(
        "release siu-core returned an invalid snapshot or version",
      );
    console.log(`[core] release startup and snapshot verified (${version})`);
  } finally {
    if (timeout) clearTimeout(timeout);
    await rm(home, { recursive: true, force: true });
  }
}

export interface BuildCoreOptions {
  /** "gnu" (default for linux) or "musl" for custom toolchains; ignored for darwin/windows. */
  linuxLibc?: "musl" | "gnu";
  /** Also stage internal/backend/bundle/siu-core.bin. Defaults to true
   *  only when target is the machine's own platform. */
  stageEmbedded?: boolean;
}

/** Builds core/ in release mode for target and stages the result; returns
 *  the resources path it was copied to. */
export async function buildCore(
  target: Target,
  options: BuildCoreOptions = {},
): Promise<string> {
  const libc: "musl" | "gnu" =
    options.linuxLibc ??
    (process.env.CORE_LINUX_LIBC === "musl" ? "musl" : "gnu");
  const triple = rustTriple(target, libc);
  const isHost = target.goos === HOST.goos && target.goarch === HOST.goarch;
  const stageEmbedded = options.stageEmbedded ?? isHost;

  console.log(
    `[core] building siu-core for ${target.goos}/${target.goarch} (${triple})`,
  );

  // Best-effort: adds the rustup target if it is missing. Harmless (and a
  // no-op) when rustup is not the active toolchain manager, or the target
  // is already installed; the musl *system* linker/libc this needs on
  // top of the rustup target cannot be installed this way, see below.
  await tryRunQuiet(["rustup", "target", "add", triple]);

  try {
    await run([
      "cargo",
      "build",
      "--release",
      "--locked",
      "--target",
      triple,
      "--manifest-path",
      join(REPO_ROOT, "core", "Cargo.toml"),
    ]);
  } catch (err) {
    if (target.goos === "linux" && libc === "musl") {
      console.error(
        [
          "",
          "siu-core 的 musl 构建失败，本机可能缺少 musl 工具链或对应 rustup target：",
          "  Debian/Ubuntu:  sudo apt-get install -y musl-tools",
          "  并确认已安装:    rustup target add " + triple,
          "",
          "若暂时无法安装 musl 工具链，可设置环境变量 CORE_LINUX_LIBC=gnu 改用",
          "glibc 构建（产物会依赖目标机器的 glibc 版本，务必参见 docs/building.md）。",
          "",
        ].join("\n"),
      );
    }
    throw err;
  }

  const builtPath = join(
    REPO_ROOT,
    "core",
    "target",
    triple,
    "release",
    coreBinaryName(target.goos),
  );
  if (!(await exists(builtPath))) {
    throw new Error(`cargo build reported success but ${builtPath} is missing`);
  }

  if (isHost) await verifyCoreStartup(builtPath);

  const destPath = resourceBinPath(target);
  await mkdir(join(destPath, ".."), { recursive: true });
  await copyFile(builtPath, destPath);
  if (target.goos !== "windows") {
    await chmod(destPath, 0o755);
  }
  console.log(`[core] staged ${destPath}`);

  if (stageEmbedded) {
    const bundlePath = join(
      REPO_ROOT,
      "internal",
      "backend",
      "bundle",
      "siu-core.bin",
    );
    await mkdir(join(bundlePath, ".."), { recursive: true });
    await copyFile(builtPath, bundlePath);
    if (process.platform !== "win32") {
      await chmod(bundlePath, 0o755);
    }
    console.log(
      `[core] staged ${bundlePath} (go:embed fallback for ${target.goos}/${target.goarch})`,
    );
  }

  return destPath;
}

export function parseCoreArgs(argv: string[]): Target {
  let goos: GoOS = HOST.goos;
  let goarch: GoArch = HOST.goarch;
  for (const arg of argv) {
    const eq = arg.replace(/^--?/, "");
    const [key, value] = eq.split("=");
    if (!value || !["os", "arch"].includes(key!))
      throw new Error("use --os=<darwin|windows|linux> --arch=<amd64|arm64>");
    if (key === "os") goos = value as GoOS;
    if (key === "arch") goarch = value as GoArch;
  }
  if (
    !["darwin", "windows", "linux"].includes(goos) ||
    !["arm64", "amd64"].includes(goarch)
  )
    throw new Error("unsupported core build target");
  return { goos, goarch };
}

if (import.meta.main) {
  const target = parseCoreArgs(process.argv.slice(2));
  buildCore(target).catch((err: unknown) => {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  });
}
