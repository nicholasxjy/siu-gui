#!/usr/bin/env bun
// `bun run build`: builds siu-core for every target platform (scripts/
// core.ts) before building and packaging the app with `mygo build`, so a
// target is never packaged with a stale or wrong-architecture sidecar
// (resources/<goos>-<goarch>/bin/siu-core, see
// docs/distribution.md#platform-resources).
//
// Usage:
//   bun run build                                   # this machine's platform
//   bun run build --platform darwin/arm64
//   bun run build -platform windows/amd64,linux/amd64
//   bun run build --platform darwin/universal        # arm64 + amd64, merged by mygo
//   bun run build --platform darwin/arm64 -- -skip-notarize   # extra mygo build flags
//
// Every target named in --platform/-platform is staged with its own
// siu-core before the single `mygo build -platform <...>` call that
// packages all of them (so the frontend is only built once); extra CLI
// arguments are forwarded to `mygo build` unchanged.

import { chmod } from "node:fs/promises";
import { join } from "node:path";
import {
  REPO_ROOT,
  HOST,
  buildCore,
  exists,
  resourceBinPath,
  run,
  type GoArch,
  type GoOS,
} from "./core.ts";

const SUPPORTED_ARCHES: Record<GoOS, GoArch[]> = {
  darwin: ["arm64", "amd64"],
  windows: ["amd64", "arm64"],
  linux: ["amd64", "arm64"],
};

interface PlatformEntry {
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

export function coreTargetsFor(
  entry: PlatformEntry,
): { goos: GoOS; goarch: GoArch }[] {
  if (entry.universal) {
    return [
      { goos: "darwin", goarch: "arm64" },
      { goos: "darwin", goarch: "amd64" },
    ];
  }
  return [{ goos: entry.goos, goarch: entry.goarch as GoArch }];
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
    if (arg === "--") continue; // conventional separator before passthrough flags
    passthrough.push(arg);
  }
  if (platform !== undefined && !platform.trim())
    throw new Error("--platform requires a value");
  return { platform, passthrough };
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

  const seen = new Set<string>();
  const coreTargets: { goos: GoOS; goarch: GoArch }[] = [];
  for (const entry of entries) {
    for (const t of coreTargetsFor(entry)) {
      const key = `${t.goos}/${t.goarch}`;
      if (!seen.has(key)) {
        seen.add(key);
        coreTargets.push(t);
      }
    }
  }

  console.log(`[build] platform(s): ${platformArg}`);
  console.log(
    `[build] core targets: ${coreTargets.map((t) => `${t.goos}/${t.goarch}`).join(", ")}`,
  );

  // Build and stage every target's siu-core *before* calling `mygo
  // build`, one target at a time, so a failure for one architecture
  // never leaves another target packaged with the wrong (or a stale)
  // sidecar.
  for (const target of coreTargets) {
    await buildCore(target);
    const path = resourceBinPath(target);
    if (!(await exists(path))) {
      throw new Error(
        `internal error: expected ${path} to exist after buildCore(${target.goos}/${target.goarch})`,
      );
    }
  }

  if (entries.some((entry) => entry.universal)) {
    const universal = join(
      REPO_ROOT,
      "internal",
      "backend",
      "bundle",
      "siu-core.bin",
    );
    await run([
      "lipo",
      "-create",
      resourceBinPath({ goos: "darwin", goarch: "arm64" }),
      resourceBinPath({ goos: "darwin", goarch: "amd64" }),
      "-output",
      universal,
    ]);
    await chmod(universal, 0o755);
    await run(["codesign", "--force", "--sign", "-", universal]);
  }

  console.log(
    "[build] all core binaries staged; building the app with mygo...",
  );
  // `bun x` resolves the project's own mygo-cli dependency the same way
  // `bunx` would, without a shell.
  await run([
    "bun",
    "x",
    "mygo",
    "build",
    "-platform",
    platformArg,
    ...passthrough,
  ]);
}

if (import.meta.main)
  main().catch((err: unknown) => {
    console.error(err instanceof Error ? err.message : String(err));
    process.exit(1);
  });
