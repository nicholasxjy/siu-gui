import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api, errorMessage } from "../api";
import { Icon } from "../icons";
import type {
  ConfigExport,
  ConfigPreview,
  Mutate,
  MutationResult,
  Snapshot,
  Theme,
} from "../types";
import { shortPath } from "../utils";
import { Modal, Note } from "./Modal";

type Operation = "export" | "preview" | "restore";

export function ConfigTransfer({
  snapshot,
  theme,
  busy,
  mutate,
  onDone,
}: {
  snapshot: Snapshot;
  theme: Theme;
  busy: string;
  mutate: Mutate;
  onDone: (result: MutationResult) => void;
}) {
  const [operation, setOperation] = useState<Operation | null>(null);
  const [preview, setPreview] = useState<ConfigPreview | null>(null);
  const [exported, setExported] = useState<ConfigExport | null>(null);
  const [error, setError] = useState("");
  const locked = useRef(false);
  const working = Boolean(busy) || operation !== null;
  const disabled = working || preview !== null;

  const run = async (next: Operation, action: () => Promise<void>) => {
    if (busy || locked.current) return;
    locked.current = true;
    setOperation(next);
    setError("");
    setExported(null);
    try {
      await action();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      locked.current = false;
      setOperation(null);
    }
  };
  const exportConfig = () =>
    run("export", async () => {
      const path = await api.pickConfigDestination();
      if (!path) return;
      setExported(
        await api.request<ConfigExport>("export_config", { path, theme }),
      );
    });
  const previewConfig = () =>
    run("preview", async () => {
      const path = await api.pickConfigFile();
      if (!path) return;
      setPreview(await api.request<ConfigPreview>("preview_config", { path }));
    });
  const restoreConfig = () => {
    if (!preview) return;
    return run("restore", async () => {
      const result = await mutate(
        "import_config",
        { path: preview.path, digest: preview.digest },
        "恢复配置",
      );
      setPreview(null);
      onDone(result);
    });
  };
  const close = () => {
    if (!busy && !locked.current) setPreview(null);
  };

  return (
    <section className="settings-section" aria-busy={working}>
      <h2>配置迁移</h2>
      <div className="setting-row">
        <div>
          <h3>JSON 配置备份</h3>
          <p>
            与 siu 终端版使用同一 JSON 格式，可互相导入，包含 MCP、Skills
            文件和代理设置。 桌面版导出时还会保存当前界面主题。
            <br />
            将备份保存或复制到
            <code>{shortPath(snapshot.root, snapshot.home)}/config.json</code>，
            两端启动或刷新时会自动恢复，共享恢复记录；相同内容只应用一次。
          </p>
        </div>
        <div className="actions">
          <button
            className="btn small"
            disabled={disabled}
            onClick={() => void exportConfig()}
          >
            <Icon name={operation === "export" ? "spinner" : "download"} />
            导出 JSON
          </button>
          <button
            className="btn small"
            disabled={disabled}
            onClick={() => void previewConfig()}
          >
            <Icon name={operation === "preview" ? "spinner" : "plus"} />
            导入 JSON
          </button>
        </div>
      </div>
      <div style={{ marginTop: 16 }}>
        <Note warning>
          JSON 包含 API 密钥、请求头和代理凭据，请妥善保管文件。
          <br />
          MCP 所需的 Node、Python、Docker
          及本地脚本等外部依赖，需在目标机器安装。
        </Note>
      </div>
      {exported ? (
        <div role="status" style={{ marginTop: 16 }}>
          <Note>
            已导出 {exported.mcpCount} 个 MCP 服务、{exported.skillCount} 个
            Skills 至 <code>{shortPath(exported.path, snapshot.home)}</code>
          </Note>
        </div>
      ) : null}
      {error && !preview ? (
        <div className="form-error" role="alert">
          {error}
        </div>
      ) : null}
      {preview
        ? createPortal(
            <Modal
              title="恢复配置"
              subtitle="请确认备份内容与替换范围。"
              busy={working}
              onClose={close}
              footer={
                <>
                  <button className="btn" disabled={working} onClick={close}>
                    取消
                  </button>
                  <button
                    className="btn danger"
                    disabled={working}
                    onClick={() => void restoreConfig()}
                  >
                    {operation === "restore" ? <Icon name="spinner" /> : null}
                    {operation === "restore" ? "正在恢复…" : "确认恢复"}
                  </button>
                </>
              }
            >
              <div className="modal-tool">
                <Icon name="folder" />
                <div>
                  <h3>JSON 配置备份</h3>
                  <p>{shortPath(preview.path, snapshot.home)}</p>
                  <p>
                    导出时间：
                    {new Date(preview.exportedAt * 1000).toLocaleString()}
                  </p>
                </div>
              </div>
              <div className="section-title">替换范围</div>
              <div className="impact-row">
                <span className="pill">
                  当前：{snapshot.mcp.length} 个 MCP 服务 ·{" "}
                  {snapshot.skills.length} 个 Skills
                </span>
                <span className="pill">
                  备份：{preview.mcpCount} 个 MCP 服务 · {preview.skillCount} 个
                  Skills
                </span>
              </div>
              <Note warning>
                恢复将用备份状态替换全部 siu 管理的 MCP 与 Skills，保留 Agent
                中其他未纳管的条目。代理设置也将恢复；备份包含主题时会同步应用，否则保留当前主题。
                <br />
                恢复前会自动将现有本机配置备份至资源库 backups 目录。
              </Note>
              {preview.issues.length ? (
                <div style={{ marginTop: 16 }}>
                  <Note warning>
                    <strong>需要注意</strong>
                    <ul>
                      {preview.issues.map((issue, index) => (
                        <li key={index}>{issue}</li>
                      ))}
                    </ul>
                  </Note>
                </div>
              ) : null}
              {error ? (
                <div className="form-error" role="alert">
                  {error}
                </div>
              ) : null}
            </Modal>,
            document.body,
          )
        : null}
    </section>
  );
}
