import { copyFile, mkdir, readFile, readdir } from "node:fs/promises";
import { join } from "node:path";
import { parseArgs, parsePlatformEntry } from "./build";
import { REPO_ROOT } from "./core";

export async function stageReleaseArtifacts(options: {
  platform: string;
  version: string;
  buildDirectory: string;
  outputDirectory: string;
}): Promise<string[]> {
  const entry = parsePlatformEntry(options.platform);
  if (!/^\d+\.\d+\.\d+(?:-[\da-zA-Z.-]+)?$/.test(options.version))
    throw new Error("Invalid release version");
  const os = entry.goos === "darwin" ? "macos" : entry.goos;
  const arch = entry.universal
    ? "universal"
    : entry.goarch === "amd64"
      ? "x64"
      : entry.goarch;
  const extensions =
    entry.goos === "darwin"
      ? [".dmg"]
      : entry.goos === "windows"
        ? [".exe"]
        : [".deb", ".tar.gz"];
  const files = (
    await readdir(options.buildDirectory, { withFileTypes: true })
  ).filter((file) => file.isFile());
  const escaped = options.version.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const version = new RegExp("(^|[^\\d.])" + escaped + "($|[^\\d.])");
  const plans = extensions.map((extension) => {
    const candidates = files.filter(
      (file) =>
        file.name.toLowerCase().endsWith(extension) &&
        version.test(file.name.slice(0, -extension.length)),
    );
    if (candidates.length !== 1)
      throw new Error(
        "Expected one " +
          options.version +
          " " +
          extension +
          " package for " +
          options.platform +
          ", found " +
          candidates.length,
      );
    const suffix = extension === ".exe" ? "_setup.exe" : extension;
    return {
      source: join(options.buildDirectory, candidates[0]!.name),
      target: join(
        options.outputDirectory,
        "siu-gui_" + options.version + "_" + os + "_" + arch + suffix,
      ),
    };
  });
  await mkdir(options.outputDirectory, { recursive: true });
  for (const plan of plans) await copyFile(plan.source, plan.target);
  return plans.map((plan) => plan.target);
}

async function main() {
  const { platform, passthrough } = parseArgs(process.argv.slice(2));
  if (!platform || passthrough.length)
    throw new Error("Usage: bun scripts/release.ts --platform <os>/<arch>");
  const entry = parsePlatformEntry(platform);
  const version = (
    JSON.parse(await readFile(join(REPO_ROOT, "package.json"), "utf8")) as {
      version: string;
    }
  ).version;
  const files = await stageReleaseArtifacts({
    platform,
    version,
    buildDirectory: join(
      REPO_ROOT,
      "build",
      entry.goos + "-" + (entry.universal ? "universal" : entry.goarch),
    ),
    outputDirectory: join(REPO_ROOT, "release-assets"),
  });
  for (const file of files) console.log(file);
}

if (import.meta.main)
  main().catch((error) => {
    console.error(error);
    process.exitCode = 1;
  });
