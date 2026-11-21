import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Runtime } from "mygo-runtime";
import { api } from "./api";
import { APP_VERSION } from "./version";

const callWith = vi.fn();

beforeEach(() => {
  callWith.mockReset();
  window.mygo = { callWith } as unknown as Runtime;
});

afterEach(() => {
  delete window.mygo;
});

describe("desktop API boundary", () => {
  it("decodes a core response and preserves the request envelope", async () => {
    callWith.mockResolvedValue(
      JSON.stringify({ ok: true, data: { enabled: true } }),
    );
    await expect(
      api.request("set_enabled", { name: "example", enabled: true }),
    ).resolves.toEqual({ enabled: true });
    expect(callWith).toHaveBeenCalledWith(
      "Desktop.Request",
      [
        JSON.stringify({
          method: "set_enabled",
          params: { name: "example", enabled: true },
        }),
      ],
      undefined,
    );
  });

  it("reports core errors without treating them as successful mutations", async () => {
    callWith.mockResolvedValue(
      JSON.stringify({ ok: false, error: "configuration changed" }),
    );
    await expect(api.request("import_config")).rejects.toThrow(
      "configuration changed",
    );
  });

  it("provides the recovery message when a core error has no detail", async () => {
    callWith.mockResolvedValue(JSON.stringify({ ok: false }));
    await expect(api.request("snapshot")).rejects.toThrow(
      "操作未完成，请重新读取配置后重试。",
    );
  });

  it("preserves transport errors and never retries a mutation", async () => {
    const failure = new Error("request timed out; outcome unknown");
    callWith.mockRejectedValue(failure);
    await expect(api.request("delete", { name: "example" })).rejects.toBe(
      failure,
    );
    expect(callWith).toHaveBeenCalledTimes(1);
  });

  it("returns cancellation from native pickers as an empty selection", async () => {
    callWith.mockResolvedValue("");
    await expect(api.pickDirectory()).resolves.toBe("");
    await expect(api.pickConfigFile()).resolves.toBe("");
    await expect(api.pickConfigDestination()).resolves.toBe("");
  });

  it("keeps browser previews away from native operations", async () => {
    delete window.mygo;
    await expect(api.info()).resolves.toEqual({
      os: "",
      arch: "",
      version: APP_VERSION,
    });
    await expect(api.request("snapshot")).rejects.toThrow(
      "请通过 siu 桌面应用启动界面",
    );
    await expect(api.pickDirectory()).rejects.toThrow(
      "请在桌面应用中使用文件夹选择器。",
    );
    await expect(api.pickConfigFile()).rejects.toThrow(
      "请在桌面应用中选择 JSON 配置文件。",
    );
    await expect(api.pickConfigDestination()).rejects.toThrow(
      "请在桌面应用中选择 JSON 保存位置。",
    );
    await expect(api.openPath("config.json")).rejects.toThrow(
      "请在桌面应用中打开配置文件。",
    );
    await api.setTheme("dark");
    expect(callWith).not.toHaveBeenCalled();
  });

  it("rejects non-web URLs before invoking native shell integration", async () => {
    await expect(api.openURL("file:///tmp/config.json")).rejects.toThrow(
      "仅支持 HTTP 或 HTTPS 链接。",
    );
    expect(callWith).not.toHaveBeenCalled();
  });
});
