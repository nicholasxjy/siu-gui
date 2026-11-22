import { afterEach, describe, expect, it } from "bun:test";
import {
  access,
  mkdir,
  mkdtemp,
  readFile,
  rm,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { stageReleaseArtifacts } from "../scripts/release";

const temporary: string[] = [];
afterEach(async () => {
  await Promise.all(
    temporary
      .splice(0)
      .map((path) => rm(path, { recursive: true, force: true })),
  );
});
async function fixture(platform: string) {
  const root = await mkdtemp(join(tmpdir(), "siu-release-"));
  temporary.push(root);
  const targetDir = platform.replace("/", "-");
  const path = join(root, "build", targetDir);
  const tuiDir = join(root, "build", "tui", targetDir);
  await mkdir(path, { recursive: true });
  return { path, tuiDir, output: join(root, "staged") };
}

async function writeTui(tuiDir: string, filename: string, content = "tui") {
  await mkdir(tuiDir, { recursive: true });
  await writeFile(join(tuiDir, filename), content);
}

describe("release artifacts", () => {
  const targets = [
    ["darwin/amd64", "macos_x64", [".dmg"], ".tar.gz"],
    ["darwin/arm64", "macos_arm64", [".dmg"], ".tar.gz"],
    ["darwin/universal", "macos_universal", [".dmg", ".zip"], null],
    ["windows/amd64", "windows_x64", [".exe"], ".zip"],
    ["windows/arm64", "windows_arm64", [".exe"], ".zip"],
    ["linux/amd64", "linux_x64", [".deb", ".tar.gz"], ".tar.gz"],
    ["linux/arm64", "linux_arm64", [".deb", ".tar.gz"], ".tar.gz"],
  ] as const;
  for (const [platform, label, extensions, tuiExtension] of targets)
    it("stages " + platform + " with a unique download name", async () => {
      const { path, tuiDir, output } = await fixture(platform);
      for (const extension of extensions) {
        await writeFile(
          join(path, "Siu 0.0.1 package" + extension),
          platform + extension,
        );
        await writeFile(join(path, "Siu 0.0.10 package" + extension), "stale");
      }
      if (platform.startsWith("windows"))
        await writeFile(join(path, "Siu.exe"), "unpackaged");
      const tuiFilename = tuiExtension
        ? "siu-tui_0.0.1_" + label + tuiExtension
        : null;
      if (tuiFilename) await writeTui(tuiDir, tuiFilename, platform + "-tui");

      const files = await stageReleaseArtifacts({
        platform,
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      });
      const expected = [
        ...extensions.map((extension) => [
          "siu-project_0.0.1_" +
            label +
            (extension === ".exe" ? "_setup.exe" : extension),
          platform + extension,
        ]),
        ...(tuiFilename ? [[tuiFilename, platform + "-tui"]] : []),
      ];
      expect(files.map((file) => basename(file))).toEqual(
        expected.map(([name]) => name!),
      );
      for (let i = 0; i < files.length; i++)
        expect(await readFile(files[i]!, "utf8")).toBe(expected[i]![1]!);
    });
  it("rejects an incomplete Linux package set before staging any file", async () => {
    const { path, output } = await fixture("linux/amd64");
    await writeFile(join(path, "siu_0.0.1.deb"), "deb");
    await expect(
      stageReleaseArtifacts({
        platform: "linux/amd64",
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      }),
    ).rejects.toThrow(".tar.gz");
    await expect(access(output)).rejects.toThrow();
  });
  it("rejects a Universal release without its ZIP before staging any file", async () => {
    const { path, output } = await fixture("darwin/universal");
    await writeFile(join(path, "Siu 0.0.1.dmg"), "dmg");
    await expect(
      stageReleaseArtifacts({
        platform: "darwin/universal",
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      }),
    ).rejects.toThrow(".zip");
    await expect(access(output)).rejects.toThrow();
  });
  it("rejects missing or wrongly named TUI archives before staging any file", async () => {
    const { path, tuiDir, output } = await fixture("linux/amd64");
    await writeFile(join(path, "Siu 0.0.1.deb"), "deb");
    await writeFile(join(path, "Siu 0.0.1.tar.gz"), "tar");
    await writeTui(tuiDir, "siu-tui_0.0.10_linux_x64.tar.gz");
    await expect(
      stageReleaseArtifacts({
        platform: "linux/amd64",
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      }),
    ).rejects.toThrow("siu-tui_0.0.1_linux_x64.tar.gz");
    await expect(access(output)).rejects.toThrow();
  });
  it("rejects ambiguous Linux tar.gz packages even when TUI archive exists", async () => {
    const { path, tuiDir, output } = await fixture("linux/amd64");
    await writeFile(join(path, "Siu 0.0.1.deb"), "deb");
    await writeFile(join(path, "Siu 0.0.1.tar.gz"), "gui-tar");
    await writeFile(join(path, "siu-tui_0.0.1_linux_x64.tar.gz"), "stray");
    await writeTui(tuiDir, "siu-tui_0.0.1_linux_x64.tar.gz");
    await expect(
      stageReleaseArtifacts({
        platform: "linux/amd64",
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      }),
    ).rejects.toThrow("found 2");
    await expect(access(output)).rejects.toThrow();
  });
  it("rejects ambiguous packages and adjacent patch versions", async () => {
    const { path, tuiDir, output } = await fixture("darwin/arm64");
    await writeTui(tuiDir, "siu-tui_0.0.1_macos_arm64.tar.gz");
    const options = {
      platform: "darwin/arm64",
      version: "0.0.1",
      buildDirectory: path,
      outputDirectory: output,
    };
    await writeFile(join(path, "Siu 0.0.10.dmg"), "old");
    await expect(stageReleaseArtifacts(options)).rejects.toThrow("found 0");
    await writeFile(join(path, "Siu 0.0.1.dmg"), "one");
    await writeFile(join(path, "Siu 0.0.1 copy.dmg"), "two");
    await expect(stageReleaseArtifacts(options)).rejects.toThrow("found 2");
  });
  it("rejects invalid targets and versions", async () => {
    const { path, output } = await fixture("linux/amd64");
    const options = {
      platform: "linux/amd64",
      version: "0.0.1",
      buildDirectory: path,
      outputDirectory: output,
    };
    await expect(
      stageReleaseArtifacts({ ...options, platform: "linux/../../outside" }),
    ).rejects.toThrow();
    await expect(
      stageReleaseArtifacts({ ...options, version: "../../outside" }),
    ).rejects.toThrow("version");
  });
});
