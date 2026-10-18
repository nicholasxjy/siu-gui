import { afterEach, describe, expect, it } from "bun:test";
import { access, mkdtemp, readFile, writeFile } from "node:fs/promises";
import { rm } from "node:fs/promises";
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
async function fixture() {
  const path = await mkdtemp(join(tmpdir(), "siu-release-"));
  temporary.push(path);
  return { path, output: join(path, "staged") };
}

describe("release artifacts", () => {
  const targets = [
    ["darwin/amd64", "macos_x64", [".dmg"]],
    ["darwin/arm64", "macos_arm64", [".dmg"]],
    ["darwin/universal", "macos_universal", [".dmg"]],
    ["windows/amd64", "windows_x64", [".exe"]],
    ["windows/arm64", "windows_arm64", [".exe"]],
    ["linux/amd64", "linux_x64", [".deb", ".tar.gz"]],
    ["linux/arm64", "linux_arm64", [".deb", ".tar.gz"]],
  ] as const;
  for (const [platform, label, extensions] of targets)
    it("stages " + platform + " with a unique download name", async () => {
      const { path, output } = await fixture();
      for (const extension of extensions) {
        await writeFile(
          join(path, "Siu 0.0.1 package" + extension),
          platform + extension,
        );
        await writeFile(join(path, "Siu 0.0.10 package" + extension), "stale");
      }
      if (platform.startsWith("windows"))
        await writeFile(join(path, "Siu.exe"), "unpackaged");
      const files = await stageReleaseArtifacts({
        platform,
        version: "0.0.1",
        buildDirectory: path,
        outputDirectory: output,
      });
      expect(files.map((file) => basename(file))).toEqual(
        extensions.map(
          (extension) =>
            "siu-gui_0.0.1_" +
            label +
            (extension === ".exe" ? "_setup.exe" : extension),
        ),
      );
      for (let i = 0; i < files.length; i++)
        expect(await readFile(files[i]!, "utf8")).toBe(
          platform + extensions[i]!,
        );
    });
  it("rejects an incomplete Linux package set before staging any file", async () => {
    const { path, output } = await fixture();
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
  it("rejects ambiguous packages and adjacent patch versions", async () => {
    const { path, output } = await fixture();
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
    const { path, output } = await fixture();
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
