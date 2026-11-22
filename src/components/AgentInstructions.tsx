import { useId, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, errorMessage } from "../api";
import { Avatar, Icon } from "../icons";
import type {
  AgentInstruction,
  Mutate,
  MutationResult,
  Snapshot,
} from "../types";
import { Modal, Note } from "./Modal";

interface EditorState {
  item: AgentInstruction;
  mode: "create" | "edit";
  content: string;
  revision?: string;
  loading: boolean;
  loadError: string;
  saveError: string;
}

const statusMeta: Record<
  AgentInstruction["status"],
  { label: string; className: string }
> = {
  available: { label: "已存在", className: "status-pill available" },
  missing: { label: "未创建", className: "status-pill neutral" },
  unavailable: { label: "不可用", className: "status-pill danger" },
};

export function AgentInstructions({
  snapshot,
  busy,
  mutate,
  onDone,
  onRefresh,
}: {
  snapshot: Snapshot;
  busy: string;
  mutate: Mutate;
  onDone?: (result: MutationResult, title?: string) => void;
  onRefresh: () => void;
}) {
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [deleting, setDeleting] = useState<AgentInstruction | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [pending, setPending] = useState(false);

  const loadId = useRef(0);
  const formId = useId();
  const textareaId = useId();

  const items = snapshot.instructions ?? [];
  const resolveAgent = (id: string) =>
    snapshot.agents.find((a) => a.id === id) ?? { id, name: id };

  const working = Boolean(busy) || pending;

  const loadContent = async (item: AgentInstruction) => {
    const req = ++loadId.current;
    setEditor({
      item,
      mode: "edit",
      content: "",
      revision: "",
      loading: true,
      loadError: "",
      saveError: "",
    });
    try {
      const data = await api.request<{ content: string; revision: string }>(
        "instruction_content",
        { id: item.id },
      );
      if (req !== loadId.current) return;
      setEditor((prev) =>
        prev && prev.item.id === item.id
          ? {
              ...prev,
              content: data.content,
              revision: data.revision,
              loading: false,
              loadError: "",
            }
          : prev,
      );
    } catch (e) {
      if (req !== loadId.current) return;
      setEditor((prev) =>
        prev && prev.item.id === item.id
          ? { ...prev, loading: false, loadError: errorMessage(e) }
          : prev,
      );
    }
  };

  const openCreate = (item: AgentInstruction) => {
    if (working) return;
    loadId.current++;
    setEditor({
      item,
      mode: "create",
      content: "",
      loading: false,
      loadError: "",
      saveError: "",
    });
  };

  const closeEditor = () => {
    if (working) return;
    loadId.current++;
    setEditor(null);
  };

  const closeDelete = () => {
    if (working) return;
    setDeleting(null);
    setDeleteError("");
  };

  const submitSave = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor || editor.loading || editor.loadError || working) return;
    setPending(true);
    setEditor((prev) => (prev ? { ...prev, saveError: "" } : prev));
    const agent = resolveAgent(editor.item.agent);
    const label =
      (editor.mode === "create" ? "添加 " : "保存 ") +
      agent.name +
      " " +
      editor.item.name;
    const params: Record<string, unknown> =
      editor.mode === "edit"
        ? {
            id: editor.item.id,
            content: editor.content,
            revision: editor.revision,
          }
        : {
            id: editor.item.id,
            content: editor.content,
          };
    try {
      const res = await mutate("save_instruction", params, label);
      setEditor(null);
      onDone?.(res, label);
    } catch (e) {
      setEditor((prev) =>
        prev ? { ...prev, saveError: errorMessage(e) } : prev,
      );
    } finally {
      setPending(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleting || working) return;
    setPending(true);
    setDeleteError("");
    const agent = resolveAgent(deleting.agent);
    const label = "删除 " + agent.name + " " + deleting.name;
    try {
      const res = await mutate(
        "delete_instruction",
        { id: deleting.id },
        label,
      );
      setDeleting(null);
      onDone?.(res, label);
    } catch (e) {
      setDeleteError(errorMessage(e));
    } finally {
      setPending(false);
    }
  };

  const editorAgent = editor ? resolveAgent(editor.item.agent) : null;
  const deletingAgent = deleting ? resolveAgent(deleting.agent) : null;

  return (
    <>
      <header className="page-head">
        <div>
          <h1>AGENTS.md 管理</h1>
          <p className="subtitle">保存后直接写入本机的用户级指令文件。</p>
        </div>
        <div className="actions">
          <button
            type="button"
            className="btn"
            disabled={working}
            onClick={onRefresh}
          >
            <Icon name="refresh" />
            重新扫描
          </button>
        </div>
      </header>
      <div className="page-scroll">
        {items.length ? (
          <ul className="instruction-list" aria-label="用户级指令文件列表">
            {items.map((item) => {
              const agent = resolveAgent(item.agent);
              const status = statusMeta[item.status];
              return (
                <li className="instruction-row" key={item.id}>
                  <div className="instruction-main">
                    <Avatar agent={agent} />
                    <div className="instruction-info">
                      <div className="instruction-meta">
                        <span className="instruction-agent">{agent.name}</span>
                        <code className="instruction-file">{item.name}</code>
                        <span className={status.className}>{status.label}</span>
                      </div>
                      <p className="instruction-path">{item.path}</p>
                      {item.status === "unavailable" && item.issue ? (
                        <p className="instruction-issue">{item.issue}</p>
                      ) : null}
                    </div>
                  </div>
                  {item.status === "available" ? (
                    <div className="instruction-actions">
                      <button
                        type="button"
                        className="btn small"
                        disabled={working}
                        aria-label={"编辑 " + agent.name + " " + item.name}
                        onClick={() => !working && void loadContent(item)}
                      >
                        <Icon name="edit" />
                        编辑
                      </button>
                      <button
                        type="button"
                        className="btn danger small"
                        disabled={working}
                        aria-label={"删除 " + agent.name + " " + item.name}
                        onClick={() => {
                          if (working) return;
                          setDeleteError("");
                          setDeleting(item);
                        }}
                      >
                        <Icon name="trash" />
                        删除
                      </button>
                    </div>
                  ) : item.status === "missing" ? (
                    <div className="instruction-actions">
                      <button
                        type="button"
                        className="btn small"
                        disabled={working}
                        aria-label={"添加 " + agent.name + " " + item.name}
                        onClick={() => openCreate(item)}
                      >
                        <Icon name="plus" />
                        添加
                      </button>
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ul>
        ) : (
          <div className="empty">
            <Icon name="file" />
            <h2>暂无可管理的指令文件</h2>
            <p>
              未检测到支持用户级 Markdown 指令文件的本机 Agent。
              <br />
              安装并运行 Agent 后，点击「重新扫描」查看。
            </p>
          </div>
        )}
      </div>

      {editor && editorAgent ? (
        <Modal
          title={
            (editor.mode === "create" ? "添加 " : "编辑 ") + editor.item.name
          }
          subtitle={editorAgent.name + " · 用户级指令文件"}
          busy={working}
          large
          onClose={closeEditor}
          footer={
            <>
              <button
                type="button"
                className="btn"
                disabled={working}
                onClick={closeEditor}
              >
                取消
              </button>
              <button
                type="submit"
                form={formId}
                className="btn primary"
                disabled={
                  working || editor.loading || Boolean(editor.loadError)
                }
              >
                <Icon name={pending ? "spinner" : "check"} />
                {pending ? "正在保存…" : "保存"}
              </button>
            </>
          }
        >
          <p className="instruction-modal-path">{editor.item.path}</p>
          {editor.loading ? (
            <div className="inline-loading" role="status">
              <Icon name="spinner" />
              正在读取文件内容…
            </div>
          ) : editor.loadError ? (
            <>
              <div className="form-error" role="alert">
                {editor.loadError}
              </div>
              <button
                type="button"
                className="btn small"
                disabled={working}
                onClick={() => void loadContent(editor.item)}
              >
                <Icon name="refresh" />
                重新读取
              </button>
            </>
          ) : (
            <form id={formId} onSubmit={(e) => void submitSave(e)}>
              <fieldset disabled={working}>
                <div className="field">
                  <label htmlFor={textareaId}>Markdown</label>
                  <textarea
                    id={textareaId}
                    className="instruction-editor"
                    value={editor.content}
                    onChange={(e) =>
                      setEditor((prev) =>
                        prev ? { ...prev, content: e.target.value } : prev,
                      )
                    }
                    autoCorrect="off"
                    spellCheck={false}
                    placeholder="输入 Markdown 指令内容…"
                  />
                </div>
                {editor.saveError ? (
                  <div className="form-error" role="alert">
                    {editor.saveError}
                  </div>
                ) : null}
              </fieldset>
            </form>
          )}
        </Modal>
      ) : null}

      {deleting && deletingAgent ? (
        <Modal
          title={"删除 " + deleting.name + "？"}
          subtitle={deletingAgent.name + " · 用户级指令文件"}
          busy={working}
          onClose={closeDelete}
          footer={
            <>
              <button
                type="button"
                className="btn"
                disabled={working}
                onClick={closeDelete}
              >
                取消
              </button>
              <button
                type="button"
                className="btn danger"
                disabled={working}
                onClick={() => void confirmDelete()}
              >
                <Icon name={pending ? "spinner" : "trash"} />
                {pending ? "正在删除…" : "确认删除"}
              </button>
            </>
          }
        >
          <Note warning>将永久删除以下本机文件，此操作无法撤销。</Note>
          <div className="instruction-delete-path">
            <Icon name="file" />
            <code>{deleting.path}</code>
          </div>
          {deleteError ? (
            <div className="form-error" role="alert">
              {deleteError}
            </div>
          ) : null}
        </Modal>
      ) : null}
    </>
  );
}
