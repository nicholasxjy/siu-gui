import { afterEach, describe, expect, it } from "bun:test";
import {
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  stat,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  HOST,
  REPO_ROOT,
  parseArgs,
  parsePlatformEntry,
} from "../scripts/build";

const temporary: string[] = [];
afterEach(async () => {
  await Promise.all(
    temporary
      .splice(0)
      .map((path) => rm(path, { recursive: true, force: true })),
  );
});

const STUB_VERSION = "1.2.3-test";
const STUB_LICENSE = "MIT Stub License\n";
const STUB_NOTICES = "Third Party Stub Notices\n";
const STUB_MAIN_GO = `package main

import (
	"flag"
	"fmt"
	"runtime/debug"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()
	if *showVersion {
		cgo := "unknown"
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "CGO_ENABLED" {
					cgo = setting.Value
				}
			}
		}
		fmt.Printf("siu-tui %s cgo=%s\\n", version, cgo)
		return
	}
	fmt.Println(version)
}
`;

async function createIsolatedBuildRoot() {
  const root = await mkdtemp(join(tmpdir(), "siu-build-test-"));
  temporary.push(root);
  await mkdir(join(root, "scripts"), { recursive: true });
  await mkdir(join(root, "cmd", "siu-tui"), { recursive: true });
  await mkdir(join(root, "resources"), { recursive: true });
  await copyFile(
    join(REPO_ROOT, "scripts", "build.ts"),
    join(root, "scripts", "build.ts"),
  );
  await writeFile(
    join(root, "package.json"),
    JSON.stringify({
      name: "siu-project",
      version: STUB_VERSION,
      private: true,
      type: "module",
      scripts: { "build:tui": "bun scripts/build.ts --tui-only" },
    }),
  );
  await writeFile(
    join(root, "go.mod"),
    "module example.com/siu-tui-stub\n\ngo 1.20\n",
  );
  await writeFile(join(root, "cmd", "siu-tui", "main.go"), STUB_MAIN_GO);
  await writeFile(join(root, "LICENSE"), STUB_LICENSE);
  await writeFile(
    join(root, "resources", "THIRD_PARTY_LICENSES.txt"),
    STUB_NOTICES,
  );
  return root;
}

async function runCommand(cmd: string[], cwd: string) {
  const proc = Bun.spawn(cmd, { cwd, stdout: "pipe", stderr: "pipe" });
  const [stdout, stderr, code] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  return { code, stdout, stderr };
}

describe("desktop build targets", () => {
  for (const os of ["darwin", "windows", "linux"] as const)
    for (const arch of ["amd64", "arm64"] as const)
      it("resolves " + os + "/" + arch, () => {
        expect(parsePlatformEntry(os + "/" + arch)).toEqual({
          raw: os + "/" + arch,
          goos: os,
          universal: false,
          goarch: arch,
        });
      });
  it("resolves darwin/universal", () => {
    expect(parsePlatformEntry("darwin/universal")).toEqual({
      raw: "darwin/universal",
      goos: "darwin",
      universal: true,
    });
  });
  for (const target of [
    "darwin/x64",
    "darwin/arm64/extra",
    "windows/universal",
    "linux/386",
    "ios/arm64",
  ])
    it("rejects " + target, () =>
      expect(() => parsePlatformEntry(target)).toThrow(),
    );
  it("preserves platform lists and native CLI arguments", () => {
    expect(
      parseArgs(["--platform=windows/amd64,linux/arm64", "--", "-debug"]),
    ).toEqual({
      platform: "windows/amd64,linux/arm64",
      passthrough: ["-debug"],
    });
  });
  it("rejects a missing platform value", () => {
    for (const args of [
      ["--platform"],
      ["--platform="],
      ["--platform", "-debug"],
    ])
      expect(() => parseArgs(args)).toThrow();
  });
});

describe("build:tui CLI", () => {
  it("rejects darwin/universal and GUI passthrough flags", async () => {
    const root = await createIsolatedBuildRoot();
    for (const args of [
      ["run", "build:tui", "--platform", "darwin/universal"],
      ["run", "build:tui", "--platform", "darwin/arm64", "--", "-debug"],
    ]) {
      const result = await runCommand(["bun", ...args], root);
      expect(result.code).not.toBe(0);
    }
    expect(await readdir(root)).not.toContain("build");
  });

  it("builds, packs, and executes the native TUI stub archive", async () => {
    const root = await createIsolatedBuildRoot();
    const target = `${HOST.goos}/${HOST.goarch}`;
    const buildResult = await runCommand(
      ["bun", "run", "build:tui", "--platform", target],
      root,
    );
    expect(buildResult.code).toBe(0);

    const osLabel = HOST.goos === "darwin" ? "macos" : HOST.goos;
    const archLabel = HOST.goarch === "amd64" ? "x64" : HOST.goarch;
    const ext = HOST.goos === "windows" ? "zip" : "tar.gz";
    const archiveName = `siu-tui_${STUB_VERSION}_${osLabel}_${archLabel}.${ext}`;
    const targetDir = join(root, "build", "tui", `${HOST.goos}-${HOST.goarch}`);
    expect(await readdir(join(root, "build"))).toEqual(["tui"]);
    expect(await readdir(targetDir)).toEqual([archiveName]);

    const unpackDir = await mkdtemp(join(root, "unpack-"));
    const tar =
      process.platform === "win32"
        ? join(process.env.SystemRoot ?? "C:\\Windows", "System32", "tar.exe")
        : "tar";
    const unpack = await runCommand(
      [tar, "-x", "-f", join(targetDir, archiveName), "-C", unpackDir],
      root,
    );
    expect(unpack.code).toBe(0);

    const binaryName = HOST.goos === "windows" ? "siu-tui.exe" : "siu-tui";
    const entries = await readdir(unpackDir, { withFileTypes: true });
    expect(entries.every((item) => item.isFile())).toBe(true);
    expect(entries.map((item) => item.name).sort()).toEqual(
      [binaryName, "LICENSE", "THIRD_PARTY_LICENSES.txt"].sort(),
    );
    expect(await readFile(join(unpackDir, "LICENSE"), "utf8")).toBe(
      STUB_LICENSE,
    );
    expect(
      await readFile(join(unpackDir, "THIRD_PARTY_LICENSES.txt"), "utf8"),
    ).toBe(STUB_NOTICES);

    if (process.platform !== "win32") {
      const binStat = await stat(join(unpackDir, binaryName));
      expect(binStat.mode & 0o111).not.toBe(0);
      const licenseStat = await stat(join(unpackDir, "LICENSE"));
      expect(licenseStat.mode & 0o111).toBe(0);
    }

    const execResult = await runCommand(
      [join(unpackDir, binaryName), "--version"],
      unpackDir,
    );
    expect(execResult.code).toBe(0);
    expect(execResult.stdout.trim()).toBe(`siu-tui ${STUB_VERSION} cgo=0`);
  }, 90_000);
});
