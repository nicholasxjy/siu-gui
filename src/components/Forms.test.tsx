import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ServerForm } from "./Forms";
import { fixture, result } from "../test/fixture";
import type { Mutate } from "../types";
const props = () => ({
  snapshot: fixture(),
  mutate: vi.fn<Mutate>().mockResolvedValue(result()),
  busy: "",
  onClose: vi.fn(),
  onDone: vi.fn(),
});
describe("真实配置编辑的保护", () => {
  it("保留带空格的独立参数和现有凭据，凭据默认隐藏", async () => {
    const p = props(),
      entry = p.snapshot.mcp[0];
    render(<ServerForm {...p} entry={entry} />);
    expect(screen.getByLabelText("环境变量值 1")).toHaveAttribute(
      "type",
      "password",
    );
    expect(screen.getByLabelText("参数 2")).toHaveValue(
      "/sandbox/My Tools/server.js",
    );
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    await waitFor(() => expect(p.mutate).toHaveBeenCalledOnce());
    expect(p.mutate.mock.calls[0][1]).toMatchObject({
      oldName: "context7",
      server: {
        args: ["-y", "/sandbox/My Tools/server.js"],
        env: { API_KEY: "fixture-private-value" },
      },
      agents: ["claude", "codex"],
    });
    expect(p.onDone).toHaveBeenCalledOnce();
  });
  it("拒绝重复环境变量，不发送可能丢失凭据的请求", async () => {
    const p = props();
    render(<ServerForm {...p} entry={p.snapshot.mcp[0]} />);
    await userEvent.click(screen.getByRole("button", { name: "添加环境变量" }));
    await userEvent.type(screen.getByLabelText("环境变量名称 2"), "API_KEY");
    await userEvent.type(
      screen.getByLabelText("环境变量值 2"),
      "another-value",
    );
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("名称重复");
    expect(p.mutate).not.toHaveBeenCalled();
  });
  it("保存失败时保留编辑内容并展示错误", async () => {
    const p = props();
    p.mutate.mockRejectedValue(
      new Error("配置文件不可写：/sandbox/.claude.json"),
    );
    render(<ServerForm {...p} entry={p.snapshot.mcp[0]} />);
    fireEvent.change(screen.getByLabelText("启动命令"), {
      target: { value: "custom-runner" },
    });
    await userEvent.click(screen.getByRole("button", { name: "保存配置" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "配置文件不可写",
    );
    expect(screen.getByLabelText("启动命令")).toHaveValue("custom-runner");
    expect(p.onDone).not.toHaveBeenCalled();
  });
  it("切换 SSE 时禁用不兼容目标，并移除原先的 Codex 选择", async () => {
    const p = props();
    render(<ServerForm {...p} entry={p.snapshot.mcp[0]} />);
    await userEvent.selectOptions(screen.getByLabelText("连接方式"), "sse");
    const codex = screen.getByRole("checkbox", { name: /Codex/ });
    expect(codex).toBeDisabled();
    expect(codex).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Claude Code/ })).toBeChecked();
  });
});
