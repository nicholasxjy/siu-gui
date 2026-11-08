import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
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
        onClearAgent={vi.fn()}
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
  it("搜索和状态筛选后，详情跟随可见资源，空结果不保留旧详情", async () => {
    const snapshot = fixture();
    snapshot.mcp.push({ ...snapshot.mcp[0], name: "github", enabled: false });
    render(
      <Library
        snapshot={snapshot}
        kind="mcp"
        agentFilter=""
        onClearAgent={vi.fn()}
        selected="context7"
        onSelect={vi.fn()}
        onDialog={vi.fn()}
        onDiscover={vi.fn()}
        onAllocation={vi.fn()}
        onPause={vi.fn()}
        busy=""
        notify={vi.fn()}
      />,
    );
    const user = userEvent.setup();
    await user.type(
      screen.getByRole("textbox", { name: "搜索 MCP 服务" }),
      "github",
    );
    expect(
      within(screen.getByRole("complementary", { name: "资源详情" })).getByRole(
        "heading",
        { name: "GitHub" },
      ),
    ).toBeInTheDocument();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "筛选资源状态" }),
      "enabled",
    );
    expect(screen.getByText("没有匹配的服务")).toBeInTheDocument();
    expect(
      screen.queryByRole("complementary", { name: "资源详情" }),
    ).not.toBeInTheDocument();
    await user.click(
      within(screen.getByText("没有匹配的服务").parentElement!).getByRole(
        "button",
        { name: "清除筛选" },
      ),
    );
    expect(screen.getByRole("textbox", { name: "搜索 MCP 服务" })).toHaveValue(
      "",
    );
    expect(
      within(screen.getByRole("complementary", { name: "资源详情" })).getByRole(
        "heading",
        { name: "Context7" },
      ),
    ).toBeInTheDocument();
  });
  it("Skills 共用 Agent 表头，折叠一个来源不隐藏其他来源", async () => {
    const snapshot = fixture();
    const first = snapshot.skills[0];
    snapshot.skills.push({
      ...first,
      name: "code-review",
      source: {
        kind: "github",
        repo: "example/workflows",
        ref: "HEAD",
        path: "code-review",
      },
    });
    render(
      <Library
        snapshot={snapshot}
        kind="skills"
        agentFilter=""
        onClearAgent={vi.fn()}
        selected="frontend-design"
        onSelect={vi.fn()}
        onDialog={vi.fn()}
        onDiscover={vi.fn()}
        onAllocation={vi.fn()}
        onPause={vi.fn()}
        busy=""
        notify={vi.fn()}
      />,
    );
    expect(screen.getAllByRole("table")).toHaveLength(1);
    expect(screen.getAllByRole("columnheader")).toHaveLength(4);
    await userEvent.click(
      screen.getByRole("button", { name: /anthropics\/skills/ }),
    );
    expect(
      screen.queryByRole("button", { name: "查看 frontend-design 详情" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "查看 code-review 详情" }),
    ).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: /anthropics\/skills/ }),
    );
    expect(
      screen.getByRole("button", { name: "查看 frontend-design 详情" }),
    ).toBeInTheDocument();
  });
  it("窄屏详情使用模态焦点，Esc 返回资源，编辑时先关闭详情", async () => {
    const media = window.matchMedia("(max-width: 1180px)");
    vi.spyOn(window, "matchMedia").mockReturnValue({ ...media, matches: true });
    const onDialog = vi.fn();
    render(
      <div className="app-content">
        <Library
          snapshot={fixture()}
          kind="mcp"
          agentFilter=""
          onClearAgent={vi.fn()}
          selected="context7"
          onSelect={vi.fn()}
          onDialog={onDialog}
          onDiscover={vi.fn()}
          onAllocation={vi.fn()}
          onPause={vi.fn()}
          busy=""
          notify={vi.fn()}
        />
      </div>,
    );
    const user = userEvent.setup();
    const resource = screen.getByRole("button", { name: "查看 Context7 详情" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(resource);
    const drawer = screen.getByRole("dialog", { name: "资源详情" });
    expect(drawer).toContainElement(document.activeElement as HTMLElement);
    expect(document.querySelector<HTMLElement>(".app-content")!.inert).toBe(
      true,
    );
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(resource).toHaveFocus();
    expect(document.querySelector<HTMLElement>(".app-content")!.inert).toBe(
      false,
    );
    await user.click(resource);
    await user.click(screen.getByRole("button", { name: "编辑服务" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(onDialog).toHaveBeenCalledWith(
      expect.objectContaining({ type: "server" }),
    );
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
