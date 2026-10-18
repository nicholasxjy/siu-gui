import { describe, expect, it } from "bun:test";
import {
  coreTargetsFor,
  parseArgs,
  parsePlatformEntry,
} from "../scripts/build";
import { HOST, parseCoreArgs, resourceBinPath } from "../scripts/core";

describe("desktop build targets", () => {
  for (const os of ["darwin", "windows", "linux"] as const)
    for (const arch of ["amd64", "arm64"] as const)
      it("resolves " + os + "/" + arch, () => {
        const entry = parsePlatformEntry(os + "/" + arch);
        expect(coreTargetsFor(entry)).toEqual([{ goos: os, goarch: arch }]);
        expect(
          resourceBinPath({
            goos: os as "darwin" | "windows" | "linux",
            goarch: arch as "amd64" | "arm64",
          }),
        ).toContain(os + "-" + arch);
      });
  it("builds both core architectures for a universal macOS app", () => {
    expect(coreTargetsFor(parsePlatformEntry("darwin/universal"))).toEqual([
      { goos: "darwin", goarch: "arm64" },
      { goos: "darwin", goarch: "amd64" },
    ]);
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
  it("validates explicit core targets and defaults to the host", () => {
    expect(parseCoreArgs([])).toEqual(HOST);
    expect(parseCoreArgs(["--os=windows", "--arch=arm64"])).toEqual({
      goos: "windows",
      goarch: "arm64",
    });
    expect(() => parseCoreArgs(["--os=ios"])).toThrow();
    expect(() => parseCoreArgs(["--arch=x64"])).toThrow();
    expect(() => parseCoreArgs(["--unknown=value"])).toThrow();
  });
});
