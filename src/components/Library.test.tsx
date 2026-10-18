import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Library } from "./Library";
import { Inspector } from "./Inspector";
import { Modal } from "./Modal";
import { fixture } from "../test/fixture";
describe("资源管理反馈", () => {
  it("根据检测结果生成矩阵，写入失败显示为错误状态而非已配置", () => {
    const snapshot = fixture();
    snapshot.mcp[0].states.codex = "error";
    const onAllocation = vi.fn();
    render(
      <Library
        snapshot={snapshot}
        kind="mcp"
        agentFilter=""
        selected="context7"
        onSelect={vi.fn()}
        onDialog={vi.fn()}
        onDiscover={vi.fn()}
        onAllocation={onAllocation}
        onPause={vi.fn()}
        busy=""
        notify={vi.fn()}
      />,
    );
    const cell = screen.getByRole("button", {
      name: "Context7 · Codex · 写入未完成",
    });
    expect(cell).toHaveAttribute("aria-pressed", "false");
    expect(
      screen.getByRole("button", { name: "Context7 · Claude Code · 已配置" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getAllByRole("columnheader")).toHaveLength(4);
  });
  it("配置预览不暴露环境变量、请求头和令牌参数的值", async () => {
    const snapshot = fixture(),
      entry = snapshot.mcp[0];
    entry.args = ["--token", "secret-argument"];
    entry.headers = { Authorization: "Bearer secret-header" };
    render(
      <Inspector
        snapshot={snapshot}
        entry={entry}
        kind="mcp"
        open
        onClose={vi.fn()}
        onAllocation={vi.fn()}
        onPause={vi.fn()}
        onDialog={vi.fn()}
        busy=""
        notify={vi.fn()}
      />,
    );
    await userEvent.click(screen.getByRole("tab", { name: "配置" }));
    expect(screen.getByText(/凭据已隐藏/)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("fixture-private-value");
    expect(document.body.textContent).not.toContain("secret-argument");
    expect(document.body.textContent).not.toContain("secret-header");
  });
  it("有写入任务时 Esc 不关闭确认窗口，空闲时可关闭", async () => {
    const onClose = vi.fn(),
      view = render(
        <Modal title="保存配置" onClose={onClose} busy>
          <p>正在处理</p>
        </Modal>,
      );
    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
    view.rerender(
      <Modal title="保存配置" onClose={onClose}>
        <p>完成</p>
      </Modal>,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledOnce();
  });
});
