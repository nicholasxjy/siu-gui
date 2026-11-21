import { describe, expect, it } from "bun:test";
import { parseArgs, parsePlatformEntry } from "../scripts/build";

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
