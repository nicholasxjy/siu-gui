import { useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import { api, errorMessage } from "../api";
import type { Agent, DialogState, Kind, Resource, Snapshot } from "../types";
import {
  agentLabel,
  description,
  displayName,
  isMcp,
  redactServer,
  relativeTime,
  shortPath,
  sourceLabel,
  stateLabel,
} from "../utils";
import { Avatar, Icon, ToolIcon } from "../icons";
import { Note } from "./Modal";
interface Props {
  entry: Resource;
  kind: Kind;
  snapshot: Snapshot;
  open: boolean;
  onClose: () => void;
  onAllocation: (entry: Resource, agent: Agent) => void;
  onPause: (entry: Resource) => void;
  onDialog: (dialog: DialogState) => void;
  busy: string;
  notify: (message: string) => void;
}
export function Inspector({
  entry,
  kind,
  snapshot,
  open,
  onClose,
  onAllocation,
  onPause,
  onDialog,
  busy,
  notify,
}: Props) {
  const [tab, setTab] = useState("overview"),
    [content, setContent] = useState<{ content: string; path: string } | null>(
      null,
    ),
    [contentError, setContentError] = useState("");
  useEffect(() => {
    setTab("overview");
    setContent(null);
    setContentError("");
  }, [entry.name, kind]);
  useEffect(() => {
    if (tab !== "config" || kind !== "skills") return;
    let active = true;
    setContent(null);
    setContentError("");
    void api
      .request<{ content: string; path: string }>("skill_content", {
        name: entry.name,
      })
      .then((value) => {
        if (active) setContent(value);
      })
      .catch((e) => {
        if (active) setContentError(errorMessage(e));
      });
    return () => {
      active = false;
    };
  }, [tab, entry.name, kind, isMcp(entry) ? "" : entry.hash]);
  const agents = snapshot.agents.filter(
      (a) =>
        (a.detected || entry.agents.includes(a.id)) &&
        (kind === "mcp" ? a.mcpPath : a.skillsPath) &&
        (a.id !== "agents" || entry.agents.includes("agents")),
    ),
    configured = Object.values(entry.states).filter((x) => x === "on").length,
    failures = Object.values(entry.states).filter((x) => x === "error").length;
  const openPath = (path: string) =>
    void api.openPath(path).catch((e) => notify(errorMessage(e)));
  return (
    <aside
      className={"inspector" + (open ? " open" : "")}
      aria-label="资源详情"
    >
      <div className="inspector-content">
        <div className="inspector-top">
          <div className="inspector-heading">
            <ToolIcon name={entry.name} kind={kind} large />
            <div className="actions">
              <button
                className="icon-button close-inspector"
                onClick={onClose}
                aria-label="关闭详情"
              >
                <Icon name="close" />
              </button>
              <button
                className="icon-button"
                disabled={Boolean(busy)}
                onClick={() =>
                  onDialog(
                    isMcp(entry)
                      ? { type: "server", entry }
                      : { type: "update", kind: "skills", name: entry.name },
                  )
                }
                aria-label={kind === "mcp" ? "编辑服务" : "更新 Skill"}
              >
                <Icon name={kind === "mcp" ? "edit" : "refresh"} />
              </button>
            </div>
          </div>
          <h2 title={entry.name}>{displayName(entry.name, kind)}</h2>
          <div className="meta-line">
            <Icon name={kind === "skills" ? "branch" : "plug"} />
            <span title={sourceLabel(entry)}>
              {shortPath(sourceLabel(entry), snapshot.home)}
            </span>
          </div>
          <p className="description">
            {description(entry) || "该资源没有提供描述，可查看配置或来源文件。"}
          </p>
          <span className="pill">
            <Icon
              name={
                kind === "skills"
                  ? "skill"
                  : isMcp(entry) && entry.transport !== "stdio"
                    ? "external"
                    : "terminal"
              }
            />
            {isMcp(entry)
              ? entry.transport === "stdio"
                ? "stdio 本地命令"
                : entry.transport.toUpperCase() + " 远程服务"
              : "Skill"}
          </span>
        </div>
        <div className="detail-tabs" role="tablist" aria-label="资源详情内容">
          <button
            role="tab"
            aria-selected={tab === "overview"}
            className={"detail-tab " + (tab === "overview" ? "active" : "")}
            onClick={() => setTab("overview")}
          >
            概览
          </button>
          <button
            role="tab"
            aria-selected={tab === "config"}
            className={"detail-tab " + (tab === "config" ? "active" : "")}
            onClick={() => setTab("config")}
          >
            {kind === "mcp" ? "配置" : "内容预览"}
          </button>
        </div>
        {tab === "overview" ? (
          <>
            <div className="detail-section">
              <div className="section-title">
                分配给 Agent<span>{entry.agents.length} 个目标</span>
              </div>
              {agents.length ? (
                agents.map((a) => {
                  const chosen = entry.agents.includes(a.id),
                    state = entry.states[a.id] || "off",
                    blocked = state.startsWith("unsupported:");
                  return (
                    <div className="agent-setting" key={a.id}>
                      <Avatar agent={a} />
                      <div className="agent-setting-info">
                        <div className="agent-setting-name">
                          {agentLabel(a)}
                          {!a.detected ? (
                            <span className="inline-label">未检测到</span>
                          ) : null}
                        </div>
                        <div
                          className={
                            "agent-setting-path " +
                            (state === "error" ? "error-text" : "")
                          }
                          title={
                            state === "error"
                              ? stateLabel(state)
                              : (kind === "mcp" ? a.mcpPath : a.skillsPath) ||
                                ""
                          }
                        >
                          {blocked
                            ? stateLabel(state)
                            : state === "error"
                              ? "写入未完成"
                              : shortPath(
                                  (kind === "mcp" ? a.mcpPath : a.skillsPath) ||
                                    "",
                                  snapshot.home,
                                )}
                        </div>
                      </div>
                      <button
                        role="switch"
                        aria-checked={state === "on"}
                        disabled={Boolean(busy) || (blocked && !chosen)}
                        className={
                          "switch " +
                          (state === "on"
                            ? "checked"
                            : state === "paused"
                              ? "checked paused-switch"
                              : state === "error"
                                ? "error-switch"
                                : "")
                        }
                        aria-label={
                          displayName(entry.name, kind) +
                          " 分配给 " +
                          agentLabel(a)
                        }
                        title={stateLabel(state)}
                        onClick={() => onAllocation(entry, a)}
                      />
                    </div>
                  );
                })
              ) : (
                <Note>
                  未检测到可用 Agent。安装并首次运行 Agent
                  后，点击侧栏重新扫描。
                </Note>
              )}
              {kind === "skills" ? (
                <div className="shared-box">
                  {entry.agents.includes("agents")
                    ? "已分配到共享目录。部分 Agent 也会读取此目录，请留意重复展示。"
                    : "共享目录 " +
                      shortPath(
                        snapshot.home + "/.agents/skills",
                        snapshot.home,
                      ) +
                      " 可在添加 Skills 时单独选择。"}
                </div>
              ) : null}
            </div>
            <div
              className={
                "detail-status " +
                (!entry.enabled
                  ? "paused-note"
                  : failures
                    ? "error-note"
                    : !configured
                      ? "neutral-note"
                      : "")
              }
            >
              <Icon
                name={
                  !entry.enabled
                    ? "pause"
                    : failures
                      ? "warning"
                      : configured
                        ? "check"
                        : "info"
                }
              />
              {!entry.enabled
                ? "已暂停，Agent 分配已保留"
                : failures
                  ? failures + " 个目标写入未完成"
                  : configured
                    ? "已配置到 " + configured + " 个目标"
                    : "尚未分配给 Agent"}
            </div>
            <div className="detail-section">
              <Note>
                {entry.enabled
                  ? "分配变更会写入本机配置。Agent 是否需要重载取决于对应客户端。"
                  : "恢复会重新写入全部已选目标，无需再次选择。"}
              </Note>
              {!isMcp(entry) ? (
                <div className="last-updated">
                  上次更新：{relativeTime(entry.updated)}
                </div>
              ) : null}
            </div>
          </>
        ) : (
          <div className="detail-section">
            {isMcp(entry) ? (
              <>
                <div className="section-title">
                  配置预览<span>凭据已隐藏</span>
                </div>
                <pre className="config-preview">
                  {JSON.stringify(redactServer(entry), null, 2)}
                </pre>
                <div className="note" style={{ marginTop: 15 }}>
                  <Icon name="lock" />
                  <span>
                    环境变量和请求头的值默认隐藏。编辑时可逐项查看与修改。
                  </span>
                </div>
                <div className="section-title" style={{ marginTop: 23 }}>
                  配置文件
                </div>
                {snapshot.agents
                  .filter((a) => entry.agents.includes(a.id) && a.mcpPath)
                  .map((a) => (
                    <button
                      className="path-button"
                      key={a.id}
                      onClick={() => openPath(a.mcpPath!)}
                      title={a.mcpPath!}
                    >
                      <Icon name="file" />
                      <span>{shortPath(a.mcpPath!, snapshot.home)}</span>
                      <Icon name="external" />
                    </button>
                  ))}
              </>
            ) : contentError ? (
              <Note warning>
                {contentError}
                <button
                  className="text-button"
                  onClick={() => {
                    setTab("overview");
                    queueMicrotask(() => setTab("config"));
                  }}
                >
                  重试读取
                </button>
              </Note>
            ) : content ? (
              <>
                <div className="skill-doc">
                  <ReactMarkdown
                    skipHtml
                    disallowedElements={["img"]}
                    components={{
                      a: ({ href, children }) => (
                        <a
                          href={href}
                          onClick={(e) => {
                            e.preventDefault();
                            if (href)
                              void api
                                .openURL(href)
                                .catch((error) => notify(errorMessage(error)));
                          }}
                        >
                          {children}
                        </a>
                      ),
                    }}
                  >
                    {content.content.slice(0, 60000)}
                  </ReactMarkdown>
                </div>
                {content.content.length > 60000 ? (
                  <Note>
                    内容较长，仅预览前 60,000 个字符。可打开源文件查看全部。
                  </Note>
                ) : null}
                <button
                  className="btn small wide"
                  style={{ marginTop: 15 }}
                  onClick={() => openPath(content.path)}
                >
                  <Icon name="external" />
                  打开 SKILL.md
                </button>
              </>
            ) : (
              <div className="inline-loading">
                <Icon name="spinner" />
                正在读取 SKILL.md…
              </div>
            )}
          </div>
        )}
      </div>
      <div className="detail-bottom">
        <button
          className="btn"
          disabled={Boolean(busy)}
          onClick={() => onPause(entry)}
        >
          <Icon name={entry.enabled ? "pause" : "play"} />
          {entry.enabled ? "暂停所有分配" : "恢复所有分配"}
        </button>
        <button
          className="icon-button"
          disabled={Boolean(busy)}
          onClick={() => onDialog({ type: "delete", kind, entry })}
          aria-label={"删除 " + displayName(entry.name, kind)}
          title="删除"
        >
          <Icon name="trash" />
        </button>
      </div>
    </aside>
  );
}
