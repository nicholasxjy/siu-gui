#!/usr/bin/env bun
// `bun run build`: builds desktop packages with mygo and standalone TUI archives.
// `bun run build:tui`: builds only standalone TUI archives.
//
// Usage:
//   bun run build                                   # this machine's platform
//   bun run build --platform darwin/arm64
//   bun run build -platform windows/amd64,linux/amd64
//   bun run build --platform darwin/universal        # arm64 + amd64, merged by mygo
//   bun run build --platform darwin/arm64 -- -skip-notarize   # extra mygo build flags

import {
  copyFile,
  mkdir,
  mkdtemp,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import { basename, dirname, join, resolve } from "node:path";

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

type SingleArchPlatformEntry = {
  raw: string;
  goos: GoOS;
  universal: false;
  goarch: GoArch;
};

export type PlatformEntry =
  SingleArchPlatformEntry | { raw: string; goos: "darwin"; universal: true };

export function releaseTarget(entry: PlatformEntry): string {
  const os = entry.goos === "darwin" ? "macos" : entry.goos;
  const arch = entry.universal
    ? "universal"
    : entry.goarch === "amd64"
      ? "x64"
      : entry.goarch;
  return `${os}_${arch}`;
}

export function tuiArchivePath(
  buildRoot: string,
  entry: SingleArchPlatformEntry,
  version: string,
): string {
  const extension = entry.goos === "windows" ? "zip" : "tar.gz";
  return join(
    buildRoot,
    "tui",
    `${entry.goos}-${entry.goarch}`,
    `siu-tui_${version}_${releaseTarget(entry)}.${extension}`,
  );
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

async function run(
  cmd: string[],
  options: { cwd?: string; env?: NodeJS.ProcessEnv } = {},
): Promise<void> {
  const proc = Bun.spawn(cmd, {
    cwd: REPO_ROOT,
    stdout: "inherit",
    stderr: "inherit",
    ...options,
  });
  const code = await proc.exited;
  if (code !== 0) {
    throw new Error(`${cmd.join(" ")} exited with code ${code}`);
  }
}

async function buildTui(entry: SingleArchPlatformEntry, version: string) {
  const archive = tuiArchivePath(join(REPO_ROOT, "build"), entry, version);
  const directory = dirname(archive);
  await mkdir(directory, { recursive: true });
  const stage = await mkdtemp(join(directory, ".staging-"));
  const binary = entry.goos === "windows" ? "siu-tui.exe" : "siu-tui";
  const notices = "THIRD_PARTY_LICENSES.txt";
  const tar =
    process.platform === "win32"
      ? join(process.env.SystemRoot ?? "C:\\Windows", "System32", "tar.exe")
      : process.platform === "darwin"
        ? "/usr/bin/tar"
        : "bsdtar";
  try {
    await run(
      [
        "go",
        "build",
        "-trimpath",
        "-ldflags",
        `-X main.version=${version}`,
        "-o",
        join(stage, binary),
        "./cmd/siu-tui",
      ],
      {
        env: {
          ...process.env,
          GOOS: entry.goos,
          GOARCH: entry.goarch,
          CGO_ENABLED: "0",
        },
      },
    );
    await copyFile(join(REPO_ROOT, "LICENSE"), join(stage, "LICENSE"));
    await copyFile(join(REPO_ROOT, "resources", notices), join(stage, notices));
    // Set Unix permissions in the archive even when cross-building on Windows.
    await writeFile(
      join(stage, "archive.mtree"),
      `#mtree\n${binary} type=file mode=0755\nLICENSE type=file mode=0644\n${notices} type=file mode=0644\n`,
    );
    await run([tar, "-a", "-cf", basename(archive), "@archive.mtree"], {
      cwd: stage,
    });
    await rename(join(stage, basename(archive)), archive);
    console.log(`[build] ${archive}`);
  } finally {
    await rm(stage, { recursive: true, force: true });
  }
}

async function main(): Promise<void> {
  const argv = process.argv.slice(2);
  const tuiOnly = argv[0] === "--tui-only";
  const { platform, passthrough } = parseArgs(tuiOnly ? argv.slice(1) : argv);
  const platformArg = platform ?? `${HOST.goos}/${HOST.goarch}`;
  const entries = platformArg
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map(parsePlatformEntry);
  if (!entries.length) throw new Error("select at least one platform");
  const universal = entries.some((entry) => entry.universal);
  if (tuiOnly && (universal || passthrough.length))
    throw new Error(
      "build:tui accepts only architecture-specific --platform targets",
    );
  if (universal && process.platform !== "darwin")
    throw new Error("darwin/universal requires macOS and lipo");

  const { version } = await Bun.file(join(REPO_ROOT, "package.json")).json();
  console.log(`[build] platform(s): ${platformArg}`);
  if (!tuiOnly)
    await run([
      "bun",
      "x",
      "mygo",
      "build",
      "-platform",
      platformArg,
      ...passthrough,
    ]);

  if (universal) {
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

  for (const entry of entries)
    if (!entry.universal) await buildTui(entry, version);
}

if (import.meta.main)
  main().catch((err: unknown) => {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  });
