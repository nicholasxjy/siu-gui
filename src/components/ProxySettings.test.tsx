import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProxySettingsForm } from "./ProxySettings";
import { api } from "../api";
import type { ProxySettings } from "../types";

vi.mock("../api", () => ({
  api: { request: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

const DEFAULT: ProxySettings = { mode: "system", url: "", noProxy: "" };

function setup(settings: ProxySettings = DEFAULT) {
  vi.mocked(api.request).mockResolvedValue(settings);
  const notify = vi.fn();
  const result = render(<ProxySettingsForm busy="" notify={notify} />);
  return { notify, ...result };
}

describe("ProxySettingsForm", () => {
  beforeEach(() => {
    vi.mocked(api.request).mockReset();
  });

  it("默认以 system 模式加载，显示系统代理提示", async () => {
    setup({ mode: "system", url: "", noProxy: "" });
    expect(
      await screen.findByRole("combobox", { name: "代理模式" }),
    ).toHaveValue("system");
    expect(screen.getByText(/自动读取系统代理/)).toBeInTheDocument();
  });

  it("手动配置：切换到 custom 展示代理地址字段", async () => {
    setup({ mode: "system", url: "", noProxy: "" });
    await screen.findByRole("combobox", { name: "代理模式" });
    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "代理模式" }),
      "custom",
    );
    expect(screen.getByLabelText("代理地址")).toBeInTheDocument();
    expect(screen.getByLabelText("绕过代理")).toBeInTheDocument();
  });

  it("手动配置：保存时发送正确字段和协议", async () => {
    vi.mocked(api.request)
      .mockResolvedValueOnce({ mode: "system", url: "", noProxy: "" })
      .mockResolvedValueOnce({
        mode: "custom",
        url: "socks5://127.0.0.1:1080",
        noProxy: "localhost",
      });
    const notify = vi.fn();
    render(<ProxySettingsForm busy="" notify={notify} />);
    await screen.findByRole("combobox", { name: "代理模式" });

    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "代理模式" }),
      "custom",
    );
    // url field is type=password by default, use show button first
    await userEvent.click(screen.getByRole("button", { name: "显示代理地址" }));
    await userEvent.type(
      screen.getByLabelText("代理地址"),
      "socks5://127.0.0.1:1080",
    );
    await userEvent.type(screen.getByLabelText("绕过代理"), "localhost");
    await userEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(vi.mocked(api.request)).toHaveBeenCalledWith(
        "set_proxy_settings",
        {
          proxy: {
            mode: "custom",
            url: "socks5://127.0.0.1:1080",
            noProxy: "localhost",
          },
        },
      ),
    );
    await waitFor(() => expect(notify).toHaveBeenCalledWith("代理设置已保存"));
  });

  it("保存失败：保留输入并显示具体错误", async () => {
    vi.mocked(api.request)
      .mockResolvedValueOnce({ mode: "system", url: "", noProxy: "" })
      .mockRejectedValueOnce(new Error("网络不可达，无法验证代理地址"));
    const notify = vi.fn();
    render(<ProxySettingsForm busy="" notify={notify} />);
    await screen.findByRole("combobox", { name: "代理模式" });

    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "代理模式" }),
      "custom",
    );
    await userEvent.click(screen.getByRole("button", { name: "显示代理地址" }));
    await userEvent.type(
      screen.getByLabelText("代理地址"),
      "http://bad.proxy:9999",
    );
    await userEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "网络不可达，无法验证代理地址",
    );
    // Input is preserved
    expect(screen.getByLabelText("代理地址")).toHaveValue(
      "http://bad.proxy:9999",
    );
    expect(notify).not.toHaveBeenCalled();
  });

  it("加载失败后显示错误并可重试", async () => {
    vi.mocked(api.request)
      .mockRejectedValueOnce(new Error("后端连接失败"))
      .mockResolvedValueOnce({ mode: "system", url: "", noProxy: "" });
    render(<ProxySettingsForm busy="" notify={vi.fn()} />);

    expect(await screen.findByText(/后端连接失败/)).toBeInTheDocument();
    const retryBtn = screen.getByRole("button", { name: "重新读取" });
    await userEvent.click(retryBtn);

    expect(
      await screen.findByRole("combobox", { name: "代理模式" }),
    ).toHaveValue("system");
    expect(screen.queryByText(/后端连接失败/)).not.toBeInTheDocument();
  });

  it("损坏的设置可恢复为系统默认值并重新保存", async () => {
    vi.mocked(api.request)
      .mockRejectedValueOnce(new Error("无法读取代理设置"))
      .mockResolvedValueOnce(DEFAULT);
    render(<ProxySettingsForm busy="" notify={vi.fn()} />);
    await userEvent.click(
      await screen.findByRole("button", { name: "使用系统默认值" }),
    );
    await userEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(api.request).toHaveBeenCalledWith("set_proxy_settings", {
        proxy: DEFAULT,
      }),
    );
  });

  it("保存直连（direct）时明确传递 direct mode，不回落 system", async () => {
    vi.mocked(api.request)
      .mockResolvedValueOnce({ mode: "system", url: "", noProxy: "" })
      .mockResolvedValueOnce({ mode: "direct", url: "", noProxy: "" });
    const notify = vi.fn();
    render(<ProxySettingsForm busy="" notify={notify} />);
    await screen.findByRole("combobox", { name: "代理模式" });

    await userEvent.selectOptions(
      screen.getByRole("combobox", { name: "代理模式" }),
      "direct",
    );
    await userEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(vi.mocked(api.request)).toHaveBeenCalledWith(
        "set_proxy_settings",
        {
          proxy: { mode: "direct", url: "", noProxy: "" },
        },
      ),
    );
    await waitFor(() => expect(notify).toHaveBeenCalledWith("代理设置已保存"));
    // After save, mode should still be direct (not fallen back to system)
    expect(screen.getByRole("combobox", { name: "代理模式" })).toHaveValue(
      "direct",
    );
  });

  it("代理地址密码显示切换", async () => {
    setup({
      mode: "custom",
      url: "http://user:pass@127.0.0.1:7890",
      noProxy: "",
    });
    await screen.findByRole("combobox", { name: "代理模式" });

    const urlInput = screen.getByLabelText("代理地址");
    expect(urlInput).toHaveAttribute("type", "password");

    await userEvent.click(screen.getByRole("button", { name: "显示代理地址" }));
    expect(urlInput).toHaveAttribute("type", "text");

    await userEvent.click(screen.getByRole("button", { name: "隐藏代理地址" }));
    expect(urlInput).toHaveAttribute("type", "password");
  });
});
