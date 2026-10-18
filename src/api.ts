import { call, isMyGo } from "mygo-runtime";
import { APP_VERSION } from "./version";
import type { AppInfo, Theme } from "./types";
declare const __SIU_BROWSER_TEST__: boolean;
const testing =
  import.meta.env.DEV &&
  typeof __SIU_BROWSER_TEST__ !== "undefined" &&
  __SIU_BROWSER_TEST__;
export const api = {
  async request<T>(
    method: string,
    params: Record<string, unknown> = {},
  ): Promise<T> {
    const payload = JSON.stringify({ method, params });
    let raw: string;
    if (isMyGo()) raw = await call<string>("Desktop.Request", payload);
    else if (testing) {
      const response = await fetch("/__test/request", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: payload,
      });
      if (!response.ok) throw new Error(await response.text());
      raw = await response.text();
    } else
      throw new Error(
        "请通过 siu 桌面应用启动界面。浏览器预览无法访问本机配置。开发时请运行 bun run dev。",
      );
    const result = JSON.parse(raw) as { ok: boolean; data: T; error?: string };
    if (!result.ok)
      throw new Error(result.error || "操作未完成，请重新读取配置后重试。");
    return result.data;
  },
  async info(): Promise<AppInfo> {
    if (isMyGo()) return call<AppInfo>("Desktop.Info");
    if (testing) {
      const r = await fetch("/__test/info");
      if (!r.ok) throw new Error(await r.text());
      return r.json();
    }
    return { os: "", arch: "", version: APP_VERSION };
  },
  async pickDirectory(): Promise<string> {
    if (!isMyGo()) throw new Error("请在桌面应用中使用文件夹选择器。");
    return call<string>("Desktop.PickDirectory");
  },
  async openPath(path: string): Promise<void> {
    if (!isMyGo()) throw new Error("请在桌面应用中打开配置文件。");
    await call("Desktop.OpenPath", path);
  },
  async openURL(url: string): Promise<void> {
    const parsed = new URL(url);
    if (!["http:", "https:"].includes(parsed.protocol))
      throw new Error("仅支持 HTTP 或 HTTPS 链接。");
    if (isMyGo()) await call("Desktop.OpenURL", parsed.toString());
    else window.open(parsed.toString(), "_blank", "noopener,noreferrer");
  },
  async setTheme(theme: Theme): Promise<void> {
    if (isMyGo()) await call("Desktop.SetTheme", theme);
  },
};
export const errorMessage = (error: unknown) =>
  error instanceof Error ? error.message : String(error);
