import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AgentInstructions } from "./AgentInstructions";
import { api } from "../api";
import { fixture, result } from "../test/fixture";
import type { AgentInstruction, Mutate, Snapshot } from "../types";

vi.mock("../api", () => ({
  api: { request: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : String(e)),
}));

const sampleInstructions = (): AgentInstruction[] => [
  {
    id: "claude:user",
    agent: "claude",
    name: "CLAUDE.md",
    path: "/sandbox/.claude/CLAUDE.md",
    status: "available",
  },
  {
    id: "codex:override",
    agent: "codex",
    name: "AGENTS.override.md",
    path: "/sandbox/.codex/AGENTS.override.md",
    status: "disabled",
  },
  {
    id: "codex:user",
    agent: "codex",
    name: "AGENTS.md",
    path: "/sandbox/.codex/AGENTS.md",
    status: "missing",
  },
  {
    id: "cursor:user",
    agent: "cursor",
    name: "AGENTS.md",
    path: "/sandbox/.cursor/AGENTS.md",
    status: "unavailable",
    issue: "目标路径是目录，无法作为 Markdown 文件读取",
  },
];

function setup(instructions?: AgentInstruction[]) {
  const snapshot: Snapshot = {
    ...fixture(),
    ...(instructions !== undefined ? { instructions } : {}),
  };
  const mutate = vi.fn<Mutate>().mockResolvedValue(result(snapshot));
  const onDone = vi.fn();
  const onRefresh = vi.fn();
  render(
    <div className="app-content">
      <AgentInstructions
        snapshot={snapshot}
        busy=""
        mutate={mutate}
        onDone={onDone}
        onRefresh={onRefresh}
      />
    </div>,
  );
  return { snapshot, mutate, onDone, onRefresh };
}

describe("AgentInstructions", () => {
  beforeEach(() => {
    vi.mocked(api.request).mockReset();
  });

  it("渲染各状态条目，隐藏不可用项操作并展示 issue，兼容旧快照", () => {
    setup(sampleInstructions());
    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(4);

    expect(within(rows[0]).getByText("Claude Code")).toBeInTheDocument();
    expect(within(rows[0]).getByText("CLAUDE.md")).toBeInTheDocument();
    expect(
      within(rows[0]).getByText("/sandbox/.claude/CLAUDE.md"),
    ).toBeInTheDocument();
    expect(within(rows[0]).getByText("已启用")).toBeInTheDocument();
    expect(
      within(rows[0]).getByRole("button", { name: /停用/ }),
    ).toBeInTheDocument();
    expect(
      within(rows[0]).getByRole("button", { name: /编辑/ }),
    ).toBeInTheDocument();
    expect(
      within(rows[0]).getByRole("button", { name: /删除/ }),
    ).toBeInTheDocument();

    expect(rows[1]).toHaveClass("is-disabled");
    expect(within(rows[1]).getByText("Codex")).toBeInTheDocument();
    expect(within(rows[1]).getByText("AGENTS.override.md")).toBeInTheDocument();
    expect(within(rows[1]).getByText("已停用")).toBeInTheDocument();
    expect(
      within(rows[1]).getByRole("button", { name: /启用/ }),
    ).toBeInTheDocument();
    expect(
      within(rows[1]).getByRole("button", { name: /编辑/ }),
    ).toBeInTheDocument();
    expect(
      within(rows[1]).getByRole("button", { name: /删除/ }),
    ).toBeInTheDocument();

    expect(within(rows[2]).getByText("Codex")).toBeInTheDocument();
    expect(within(rows[2]).getByText("未创建")).toBeInTheDocument();
    expect(
      within(rows[2]).getByRole("button", { name: /添加/ }),
    ).toBeInTheDocument();

    expect(within(rows[3]).getByText("Cursor")).toBeInTheDocument();
    expect(within(rows[3]).getByText("不可用")).toBeInTheDocument();
    expect(
      within(rows[3]).getByText("目标路径是目录，无法作为 Markdown 文件读取"),
    ).toBeInTheDocument();
    expect(within(rows[3]).queryAllByRole("button")).toHaveLength(0);
  });

  it("旧快照不含 instructions 时展示空状态", () => {
    setup(undefined);
    expect(screen.getByText("暂无可管理的指令文件")).toBeInTheDocument();
  });

  it("按后端 snapshot.instructions 原样展示，即使对应 Agent 的通用 detected 为 false", () => {
    const snapshot = fixture();
    snapshot.agents.push({
      id: "gemini",
      name: "Gemini CLI",
      home: "/sandbox/.gemini",
      mcpPath: null,
      skillsPath: null,
      detected: false,
      transports: [],
    });
    snapshot.instructions = [
      {
        id: "gemini:user",
        agent: "gemini",
        name: "GEMINI.md",
        path: "/custom/gemini/GEMINI.md",
        status: "available",
      },
    ];
    render(
      <div className="app-content">
        <AgentInstructions
          snapshot={snapshot}
          busy=""
          mutate={vi.fn<Mutate>().mockResolvedValue(result(snapshot))}
          onRefresh={vi.fn()}
        />
      </div>,
    );
    expect(screen.getByText("Gemini CLI")).toBeInTheDocument();
    expect(screen.getByText("/custom/gemini/GEMINI.md")).toBeInTheDocument();
  });

  it("添加缺失文件时不传 revision，保存成功后关闭弹窗", async () => {
    const { mutate, onDone } = setup(sampleInstructions());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "添加 Codex AGENTS.md" }),
    );
    const textarea = screen.getByLabelText("Markdown");
    await user.type(textarea, "# Codex Rules");
    await user.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(mutate).toHaveBeenCalledOnce());
    const [method, params] = mutate.mock.calls[0];
    expect(method).toBe("save_instruction");
    expect(params).toEqual({ id: "codex:user", content: "# Codex Rules" });
    expect("revision" in params).toBe(false);
    expect(onDone).toHaveBeenCalledOnce();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("编辑已有文件时先读取内容并携带 revision 保存；保存失败保留草稿和弹窗且可取消", async () => {
    vi.mocked(api.request).mockResolvedValueOnce({
      content: "# Original",
      revision: "rev-1",
    });
    const { mutate } = setup(sampleInstructions());
    mutate.mockRejectedValueOnce(new Error("文件已在外部被修改"));
    const user = userEvent.setup();

    const editBtn = screen.getByRole("button", {
      name: "编辑 Claude Code CLAUDE.md",
    });
    await user.click(editBtn);

    const textarea = await screen.findByLabelText("Markdown");
    expect(vi.mocked(api.request)).toHaveBeenCalledWith("instruction_content", {
      id: "claude:user",
    });
    expect(textarea).toHaveValue("# Original");

    await user.clear(textarea);
    await user.type(textarea, "# Updated Draft");
    await user.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "文件已在外部被修改",
    );
    expect(mutate).toHaveBeenCalledWith(
      "save_instruction",
      { id: "claude:user", content: "# Updated Draft", revision: "rev-1" },
      expect.any(String),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByLabelText("Markdown")).toHaveValue("# Updated Draft");

    await user.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(editBtn).toHaveFocus();
  });

  it("永久删除弹窗展示完整路径并调用 delete_instruction", async () => {
    const { mutate, onDone } = setup(sampleInstructions());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "删除 Claude Code CLAUDE.md" }),
    );
    const dialog = screen.getByRole("dialog");
    expect(
      within(dialog).getByText("/sandbox/.claude/CLAUDE.md"),
    ).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "确认删除" }));
    await waitFor(() => expect(mutate).toHaveBeenCalledOnce());
    expect(mutate).toHaveBeenCalledWith(
      "delete_instruction",
      { id: "claude:user" },
      expect.any(String),
    );
    expect(onDone).toHaveBeenCalledOnce();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("点击停用和启用分别调用 set_instruction_enabled，失败后恢复按钮可用状态", async () => {
    const { mutate, onDone } = setup(sampleInstructions());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "停用 Claude Code CLAUDE.md" }),
    );
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1));
    expect(mutate).toHaveBeenLastCalledWith(
      "set_instruction_enabled",
      { id: "claude:user", enabled: false },
      "停用 Claude Code CLAUDE.md",
    );
    expect(onDone).toHaveBeenCalledTimes(1);

    const enableBtn = screen.getByRole("button", {
      name: "启用 Codex AGENTS.override.md",
    });
    await user.click(enableBtn);
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(2));
    expect(mutate).toHaveBeenLastCalledWith(
      "set_instruction_enabled",
      { id: "codex:override", enabled: true },
      "启用 Codex AGENTS.override.md",
    );
    expect(onDone).toHaveBeenCalledTimes(2);

    mutate.mockRejectedValueOnce(new Error("重命名失败"));
    await user.click(enableBtn);
    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(3));
    expect(onDone).toHaveBeenCalledTimes(2);
    expect(enableBtn).not.toBeDisabled();
  });

  it("已停用条目支持编辑与删除，并在弹窗副标题标明已停用", async () => {
    vi.mocked(api.request).mockResolvedValueOnce({
      content: "# Disabled Rules",
      revision: "rev-disabled",
    });
    const { mutate } = setup(sampleInstructions());
    const user = userEvent.setup();

    await user.click(
      screen.getByRole("button", { name: "编辑 Codex AGENTS.override.md" }),
    );
    const editDialog = await screen.findByRole("dialog");
    expect(
      within(editDialog).getByText("Codex · 用户级指令文件（已停用）"),
    ).toBeInTheDocument();
    expect(within(editDialog).getByLabelText("Markdown")).toHaveValue(
      "# Disabled Rules",
    );
    await user.click(within(editDialog).getByRole("button", { name: "取消" }));

    await user.click(
      screen.getByRole("button", { name: "删除 Codex AGENTS.override.md" }),
    );
    const deleteDialog = screen.getByRole("dialog");
    expect(
      within(deleteDialog).getByText("Codex · 用户级指令文件（已停用）"),
    ).toBeInTheDocument();
    await user.click(
      within(deleteDialog).getByRole("button", { name: "确认删除" }),
    );
    await waitFor(() => expect(mutate).toHaveBeenCalledOnce());
    expect(mutate).toHaveBeenCalledWith(
      "delete_instruction",
      { id: "codex:override" },
      "删除 Codex AGENTS.override.md",
    );
  });
});
