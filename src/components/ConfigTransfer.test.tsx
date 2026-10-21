import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { api } from "../api";
import { fixture, result } from "../test/fixture";
import type {
  ConfigExport,
  ConfigPreview,
  Mutate,
  MutationResult,
} from "../types";
import { ConfigTransfer } from "./ConfigTransfer";

vi.mock("../api", () => ({
  api: {
    request: vi.fn(),
    pickConfigFile: vi.fn(),
    pickConfigDestination: vi.fn(),
    setTheme: vi.fn().mockResolvedValue(undefined),
    info: vi
      .fn()
      .mockResolvedValue({ os: "darwin", arch: "arm64", version: "test" }),
  },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

const backup: ConfigPreview = {
  path: "/sandbox/backup.json",
  digest: "backup-digest",
  exportedAt: 1792454400,
  mcpCount: 3,
  skillCount: 5,
  issues: ["目标机器未检测到 Docker，请先安装"],
  theme: "dark",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function props() {
  return {
    snapshot: fixture(),
    theme: "light" as const,
    busy: "",
    mutate: vi.fn<Mutate>().mockResolvedValue({ ...result(), theme: "dark" }),
    onDone: vi.fn(),
  };
}

beforeEach(() => {
  vi.mocked(api.request).mockReset();
  vi.mocked(api.pickConfigFile).mockReset().mockResolvedValue(backup.path);
  vi.mocked(api.pickConfigDestination)
    .mockReset()
    .mockResolvedValue("/sandbox/export.json");
});

describe("配置迁移", () => {
  it.each(["导出 JSON", "导入 JSON"])(
    "%s：取消原生选择器不发送请求或显示成功",
    async (label) => {
      vi.mocked(api.pickConfigFile).mockResolvedValue("");
      vi.mocked(api.pickConfigDestination).mockResolvedValue("");
      const p = props();
      render(<ConfigTransfer {...p} />);
      await userEvent.click(screen.getByRole("button", { name: label }));
      expect(api.request).not.toHaveBeenCalled();
      expect(p.mutate).not.toHaveBeenCalled();
      expect(p.onDone).not.toHaveBeenCalled();
      expect(screen.queryByRole("status")).not.toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: label })).toBeEnabled();
    },
  );

  it("选择器打开期间锁定两个按钮，连续点击只打开一次", async () => {
    const picker = deferred<string>();
    vi.mocked(api.pickConfigDestination).mockReturnValue(picker.promise);
    render(<ConfigTransfer {...props()} />);
    const exportButton = screen.getByRole("button", { name: "导出 JSON" });
    fireEvent.click(exportButton);
    fireEvent.click(exportButton);
    fireEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    expect(exportButton).toBeDisabled();
    expect(screen.getByRole("button", { name: "导入 JSON" })).toBeDisabled();
    expect(api.pickConfigDestination).toHaveBeenCalledOnce();
    expect(api.pickConfigFile).not.toHaveBeenCalled();
    await act(async () => picker.resolve(""));
    expect(exportButton).toBeEnabled();
  });

  it("由后端导出文件，传递主题并等待写入完成再显示实际数量与路径", async () => {
    const exported = deferred<ConfigExport>();
    vi.mocked(api.request).mockReturnValue(exported.promise);
    const p = props();
    render(<ConfigTransfer {...p} />);
    await userEvent.click(screen.getByRole("button", { name: "导出 JSON" }));
    expect(api.request).toHaveBeenCalledExactlyOnceWith("export_config", {
      path: "/sandbox/export.json",
      theme: "light",
    });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "导入 JSON" })).toBeDisabled();
    await act(async () =>
      exported.resolve({
        path: "/sandbox/export.json",
        mcpCount: 2,
        skillCount: 4,
      }),
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "已导出 2 个 MCP 服务、4 个 Skills",
    );
    expect(screen.getByRole("status")).toHaveTextContent("~/export.json");
    expect(p.mutate).not.toHaveBeenCalled();
    expect(p.onDone).not.toHaveBeenCalled();
  });

  it.each(["picker", "export", "preview"])(
    "%s 失败时保留行内错误，允许重新选择文件",
    async (failure) => {
      if (failure === "picker")
        vi.mocked(api.pickConfigFile).mockRejectedValue(
          new Error("选择器无法打开"),
        );
      else
        vi.mocked(api.request).mockRejectedValue(new Error("配置文件不可读写"));
      const p = props();
      render(<ConfigTransfer {...p} />);
      await userEvent.click(
        screen.getByRole("button", {
          name: failure === "export" ? "导出 JSON" : "导入 JSON",
        }),
      );
      expect(await screen.findByRole("alert")).toHaveTextContent(
        failure === "picker" ? "选择器无法打开" : "配置文件不可读写",
      );
      expect(screen.queryByRole("status")).not.toBeInTheDocument();
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "导入 JSON" })).toBeEnabled();
      expect(p.onDone).not.toHaveBeenCalled();
    },
  );

  it("预览展示数量、时间和提示，取消不恢复，也不显示配置内容或凭据", async () => {
    vi.mocked(api.request).mockResolvedValue({
      ...backup,
      env: { API_KEY: "backup-private-key" },
    });
    const p = props();
    const { container } = render(
      <div className="app-content">
        <ConfigTransfer {...p} />
      </div>,
    );
    await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    const dialog = await screen.findByRole("dialog", { name: "恢复配置" });
    expect(api.request).toHaveBeenCalledExactlyOnceWith("preview_config", {
      path: backup.path,
    });
    expect(dialog).toHaveTextContent("当前：1 个 MCP 服务 · 1 个 Skills");
    expect(dialog).toHaveTextContent("备份：3 个 MCP 服务 · 5 个 Skills");
    expect(dialog).toHaveTextContent(
      new Date(backup.exportedAt * 1000).toLocaleString(),
    );
    expect(dialog).toHaveTextContent(backup.issues[0]);
    expect(dialog).toHaveTextContent("替换全部 siu 管理的 MCP 与 Skills");
    expect(dialog).toHaveTextContent("其他未纳管的条目");
    expect(dialog).toHaveTextContent("backups");
    expect(document.body).not.toHaveTextContent("backup-private-key");
    expect(document.body).not.toHaveTextContent("fixture-private-value");
    expect(container.querySelector(".app-content")).not.toContainElement(
      dialog,
    );
    expect(p.mutate).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(p.mutate).not.toHaveBeenCalled();
    expect(p.onDone).not.toHaveBeenCalled();
  });

  it("确认后只恢复一次，使用预览返回的路径和 digest 并转交结果", async () => {
    vi.mocked(api.request).mockResolvedValue({
      ...backup,
      path: "/sandbox/normalized.json",
    });
    const pending = deferred<MutationResult>();
    const p = props();
    p.mutate.mockReturnValue(pending.promise);
    render(<ConfigTransfer {...p} />);
    await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    const confirm = await screen.findByRole("button", { name: "确认恢复" });
    fireEvent.click(confirm);
    fireEvent.click(confirm);
    expect(p.mutate).toHaveBeenCalledExactlyOnceWith(
      "import_config",
      { path: "/sandbox/normalized.json", digest: backup.digest },
      "恢复配置",
    );
    expect(screen.getByRole("button", { name: "正在恢复…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(p.onDone).not.toHaveBeenCalled();
    const restored: MutationResult = { ...result(), theme: "dark" };
    await act(async () => pending.resolve(restored));
    expect(p.onDone).toHaveBeenCalledExactlyOnceWith(restored);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("恢复失败保留预览和错误，不应用主题或报告成功，允许重新选择", async () => {
    vi.mocked(api.request).mockResolvedValue(backup);
    const p = props();
    p.mutate.mockRejectedValue(new Error("备份已变更，请重新预览"));
    render(<ConfigTransfer {...p} />);
    await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    await userEvent.click(
      await screen.findByRole("button", { name: "确认恢复" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "备份已变更，请重新预览",
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(p.onDone).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "备份已变更，请重新预览",
    );
    await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    expect(
      await screen.findByRole("button", { name: "确认恢复" }),
    ).toBeEnabled();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("遵循全局 busy，预览期间发生全局操作时禁用确认与关闭", async () => {
    vi.mocked(api.request).mockResolvedValue(backup);
    const p = props();
    const { rerender } = render(<ConfigTransfer {...p} busy="同步配置" />);
    expect(screen.getByRole("button", { name: "导出 JSON" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "导入 JSON" })).toBeDisabled();
    rerender(<ConfigTransfer {...p} />);
    await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
    await screen.findByRole("dialog");
    rerender(<ConfigTransfer {...p} busy="同步配置" />);
    expect(screen.getByRole("button", { name: "确认恢复" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
    expect(p.mutate).not.toHaveBeenCalled();
  });
});

describe("配置恢复与应用状态", () => {
  it.each(["dark", null] as const)(
    "通过 mutate 恢复资源与代理，并处理备份主题 %s",
    async (theme) => {
      localStorage.setItem("siu.theme.v1", "light");
      let imported = false;
      const restored = result();
      restored.snapshot.mcp = [];
      restored.report.issues = ["Codex 配置写入失败，请检查文件权限"];
      restored.theme = theme;
      vi.mocked(api.request).mockImplementation(
        async <T,>(method: string): Promise<T> => {
          switch (method) {
            case "snapshot":
              return fixture() as T;
            case "proxy_settings":
              return {
                mode: imported ? "direct" : "system",
                url: "",
                noProxy: "",
              } as T;
            case "preview_config":
              return backup as T;
            case "import_config":
              imported = true;
              return restored as T;
            default:
              throw new Error("Unexpected request: " + method);
          }
        },
      );
      render(<App />);
      await screen.findByRole("heading", { name: "MCP 服务" });
      await userEvent.click(screen.getByRole("button", { name: "设置" }));
      expect(
        await screen.findByRole("combobox", { name: "代理模式" }),
      ).toHaveValue("system");
      await userEvent.selectOptions(
        screen.getByRole("combobox", { name: "代理模式" }),
        "custom",
      );
      await userEvent.type(
        screen.getByLabelText("代理地址"),
        "http://draft:7890",
      );
      await userEvent.click(screen.getByRole("button", { name: "导入 JSON" }));
      await screen.findByRole("dialog", { name: "恢复配置" });
      fireEvent.keyDown(window, { key: "1" });
      expect(screen.getByRole("heading", { name: "设置" })).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "确认恢复" }));
      const dialog = await screen.findByRole("dialog", {
        name: "操作完成，有项目需要处理",
      });
      expect(dialog).toHaveTextContent(restored.report.issues[0]);
      expect(api.request).toHaveBeenCalledWith("import_config", {
        path: backup.path,
        digest: backup.digest,
      });
      await waitFor(() =>
        expect(api.setTheme).toHaveBeenLastCalledWith(theme ?? "light"),
      );
      expect(localStorage.getItem("siu.theme.v1")).toBe(theme ?? "light");
      await userEvent.click(
        within(dialog).getByRole("button", { name: "关闭" }),
      );
      expect(
        await screen.findByRole("combobox", { name: "代理模式" }),
      ).toHaveValue("direct");
      expect(api.request).toHaveBeenCalledTimes(5);
      expect(
        screen.getByRole("button", { name: "清空MCP 服务" }),
      ).toBeDisabled();
      expect(screen.getByRole("alert")).toHaveTextContent(
        restored.report.issues[0],
      );
    },
    10_000,
  );
});
