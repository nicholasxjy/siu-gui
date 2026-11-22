#!/usr/bin/env bun
// `bun run build`: validates target platform(s), builds and packages the app
// with `mygo build`, and creates the macOS universal ZIP when requested.
//
// Usage:
//   bun run build                                   # this machine's platform
//   bun run build --platform darwin/arm64
//   bun run build -platform windows/amd64,linux/amd64
//   bun run build --platform darwin/universal        # arm64 + amd64, merged by mygo
//   bun run build --platform darwin/arm64 -- -skip-notarize   # extra mygo build flags

import { join, resolve } from "node:path";

export type GoOS = "darwin" | "windows" | "linux";
export type GoArch = "amd64" | "arm64";

export const REPO_ROOT = resolve(import.meta.dir, "..");

export const HOST: { goos: GoOS; goarch: GoArch } = {
  goos:
    process.platform === "darwin"
      ? "darwin"
      : process.platform === "win32"
        ? "windows"
        : "linux",
  goarch: process.arch === "arm64" ? "arm64" : "amd64",
};

const SUPPORTED_ARCHES: Record<GoOS, GoArch[]> = {
  darwin: ["arm64", "amd64"],
  windows: ["amd64", "arm64"],
  linux: ["amd64", "arm64"],
};

export interface PlatformEntry {
  raw: string;
  goos: GoOS;
  universal: boolean;
  goarch?: GoArch;
}

export function parsePlatformEntry(raw: string): PlatformEntry {
  const [osPart, archPart, extra] = raw.split("/");
  if (!osPart || !archPart || extra !== undefined) {
    throw new Error(
      `invalid -platform entry ${JSON.stringify(raw)}: want <goos>/<goarch>, e.g. darwin/arm64`,
    );
  }
  if (!(osPart in SUPPORTED_ARCHES)) {
    throw new Error(
      `unsupported platform ${JSON.stringify(raw)}: goos must be one of ${Object.keys(SUPPORTED_ARCHES).join(", ")}`,
    );
  }
  const goos = osPart as GoOS;
  if (archPart === "universal") {
    if (goos !== "darwin") {
      throw new Error(
        `unsupported platform ${JSON.stringify(raw)}: "universal" is only valid for darwin`,
      );
    }
    return { raw, goos, universal: true };
  }
  if (!SUPPORTED_ARCHES[goos].includes(archPart as GoArch)) {
    throw new Error(
      `unsupported platform ${JSON.stringify(raw)}: ${goos} supports ${SUPPORTED_ARCHES[goos].join(", ")}`,
    );
  }
  return { raw, goos, universal: false, goarch: archPart as GoArch };
}

interface ParsedArgs {
  platform?: string;
  passthrough: string[];
}

export function parseArgs(argv: string[]): ParsedArgs {
  const passthrough: string[] = [];
  let platform: string | undefined;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i]!;
    if (arg === "--platform" || arg === "-platform") {
      platform = argv[++i];
      if (!platform || platform.startsWith("-"))
        throw new Error("--platform requires a value");
      continue;
    }
    if (arg.startsWith("--platform=")) {
      platform = arg.slice("--platform=".length);
      continue;
    }
    if (arg.startsWith("-platform=")) {
      platform = arg.slice("-platform=".length);
      continue;
    }
    if (arg === "--") continue;
    passthrough.push(arg);
  }
  if (platform !== undefined && !platform.trim())
    throw new Error("--platform requires a value");
  return { platform, passthrough };
}

async function run(cmd: string[]): Promise<void> {
  const proc = Bun.spawn(cmd, {
    cwd: REPO_ROOT,
    stdout: "inherit",
    stderr: "inherit",
  });
  const code = await proc.exited;
  if (code !== 0) {
    throw new Error(`${cmd.join(" ")} exited with code ${code}`);
  }
}

async function main(): Promise<void> {
  const { platform, passthrough } = parseArgs(process.argv.slice(2));
  const platformArg = platform ?? `${HOST.goos}/${HOST.goarch}`;
  const entries = platformArg
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map(parsePlatformEntry);
  if (!entries.length) throw new Error("select at least one platform");
  if (entries.some((entry) => entry.universal) && process.platform !== "darwin")
    throw new Error("darwin/universal requires macOS and lipo");

  console.log(`[build] platform(s): ${platformArg}`);
  await run([
    "bun",
    "x",
    "mygo",
    "build",
    "-platform",
    platformArg,
    ...passthrough,
  ]);

  if (entries.some((entry) => entry.universal)) {
    const { version } = await Bun.file(join(REPO_ROOT, "package.json")).json();
    const directory = join(REPO_ROOT, "build", "darwin-universal");
    await run([
      "ditto",
      "-c",
      "-k",
      "--sequesterRsrc",
      "--keepParent",
      join(directory, "Siu.app"),
      join(directory, `siu-gui_${version}_macos_universal.zip`),
    ]);
  }
}

if (import.meta.main)
  main().catch((err: unknown) => {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  });
