import { Component, useCallback, useEffect, useState } from "react";
import type { ErrorInfo, ReactNode } from "react";
import { Avatar, Brand, Icon } from "./icons";
import type { IconName } from "./icons";
import type {
  Agent,
  DialogState,
  Kind,
  MutationResult,
  Page,
  Resource,
} from "./types";
import { isMcp } from "./utils";
import { useApplication } from "./useApp";
import { Library } from "./components/Library";
import { Discover } from "./components/Discover";
import { AgentsPage, SettingsPage } from "./components/Pages";
import { Dialogs } from "./components/Forms";
class ErrorBoundary extends Component<
  { children: ReactNode },
  { error: string }
> {
  state = { error: "" };
  static getDerivedStateFromError(error: Error) {
    return { error: error.message };
  }
  componentDidCatch(_error: Error, _info: ErrorInfo) {}
  render() {
    return this.state.error ? (
      <div className="fatal-error">
        <Icon name="warning" />
        <h1>界面遇到问题</h1>
        <p>{this.state.error}</p>
        <button className="btn primary" onClick={() => location.reload()}>
          重新载入
        </button>
      </div>
    ) : (
      this.props.children
    );
  }
}
function Application() {
  const app = useApplication(),
    { snapshot, busy } = app;
  const [page, setPage] = useState<Page>("mcp"),
    [agentFilter, setAgentFilter] = useState(""),
    [selected, setSelected] = useState(""),
    [dialog, setDialog] = useState<DialogState | null>(null);
  const navigate = useCallback((next: Page) => {
    setPage(next);
    setAgentFilter("");
    setSelected("");
  }, []);
  const openResource = (kind: Kind, name: string) => {
    setPage(kind);
    setSelected(name);
    setAgentFilter("");
  };
  const openAgent = (kind: Kind, id: string) => {
    setPage(kind);
    setAgentFilter(id);
    setSelected(
      (kind === "mcp" ? snapshot?.mcp : snapshot?.skills)?.find((x) =>
        x.agents.includes(id),
      )?.name || "",
    );
  };
  const agentClick = (id: string) => {
    const kind = page === "skills" ? "skills" : "mcp";
    if (agentFilter === id) {
      setAgentFilter("");
      return;
    }
    openAgent(kind, id);
  };
  const perform = async (
    method: string,
    params: Record<string, unknown>,
    label: string,
  ) => {
    try {
      await app.mutate(method, params, label);
    } catch {
      /* A persistent error banner contains the details. */
    }
  };
  const onAllocation = (entry: Resource, agent: Agent) => {
    const kind = isMcp(entry) ? "mcp" : "skills",
      chosen = entry.agents.includes(agent.id);
    if (!entry.enabled && chosen) {
      void perform(
        "set_enabled",
        { kind, name: entry.name, enabled: true },
        "恢复 " + entry.name + " 的所有分配",
      );
      return;
    }
    void perform(
      "set_agents",
      {
        kind,
        name: entry.name,
        agents: chosen
          ? entry.agents.filter((x) => x !== agent.id)
          : [...entry.agents, agent.id],
      },
      (chosen ? "移除 " : "配置 ") + entry.name + " · " + agent.name,
    );
  };
  const onPause = (entry: Resource) =>
    void perform(
      "set_enabled",
      {
        kind: isMcp(entry) ? "mcp" : "skills",
        name: entry.name,
        enabled: !entry.enabled,
      },
      (entry.enabled ? "暂停 " : "恢复 ") + entry.name,
    );
  const done = (result: MutationResult, title = "") => {
    if (result.log || result.report.issues.length)
      setDialog({ type: "result", result, title: title || result.message });
    else setDialog(null);
  };
  const issues = [...new Set([...app.alert, ...(snapshot?.issues || [])])];
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (event.defaultPrevented || dialog) return;
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "r") {
        event.preventDefault();
        if (!busy) void app.refresh();
        return;
      }
      if (
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(
          (event.target as HTMLElement).tagName,
        )
      )
        return;
      if (event.key === "/") {
        const input = document.querySelector<HTMLInputElement>("[data-search]");
        if (input) {
          event.preventDefault();
          input.focus();
        }
      } else if (event.key === "1") navigate("mcp");
      else if (event.key === "2") navigate("skills");
      else if (event.key === "3") navigate("discover");
      else if (event.key.toLowerCase() === "a" && !busy) {
        document.querySelector<HTMLButtonElement>("[data-add]")?.click();
      } else if (event.key === "?") setDialog({ type: "shortcuts" });
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [dialog, busy, navigate, app.refresh]);
  const nav = (id: Page, glyph: IconName, label: string, count?: number) => (
    <button
      className={"nav-button " + (page === id && !agentFilter ? "active" : "")}
      onClick={() => navigate(id)}
      title={label}
      aria-current={page === id && !agentFilter ? "page" : undefined}
    >
      <Icon name={glyph} />
      <span className="nav-text">{label}</span>
      {count !== undefined ? <span className="nav-count">{count}</span> : null}
    </button>
  );
  return (
    <div className="window">
      <div className="app-content">
        <header className="titlebar">
          <div className="window-title">
            <strong>siu</strong>
            <span>/</span>
            <span>本机工作空间</span>
          </div>
          <div className="titlebar-right">
            <Icon name="lock" />
            <span>本地配置</span>
          </div>
        </header>
        <div className="shell">
          <aside className="sidebar" aria-label="应用导航">
            <div className="brand">
              <Brand />
              <span className="wordmark">siu</span>
              <span className="brand-caption">Agent 工具箱</span>
            </div>
            <p className="nav-label">我的资源</p>
            {nav("mcp", "plug", "MCP 服务", snapshot?.mcp.length)}
            {nav("skills", "skill", "Skills", snapshot?.skills.length)}
            <div className="nav-separator" />
            {nav("discover", "compass", "发现")}
            <div className="agents-heading">
              <span>本机 Agent</span>
              <div className="actions">
                <button
                  className="tiny-button"
                  disabled={Boolean(busy)}
                  onClick={() => void app.refresh()}
                  aria-label="重新扫描 Agent"
                  title="重新扫描"
                >
                  <Icon name="refresh" />
                </button>
                <button
                  className="tiny-button"
                  onClick={() => navigate("agents")}
                  aria-label="管理本机 Agent"
                >
                  <Icon name="settings" />
                </button>
              </div>
            </div>
            <div className="agents-list">
              {snapshot?.agents
                .filter((a) => a.detected && a.id !== "agents")
                .map((a) => (
                  <button
                    key={a.id}
                    className={
                      "nav-button agent-nav " +
                      (agentFilter === a.id ? "active" : "")
                    }
                    onClick={() => agentClick(a.id)}
                    title={"查看分配给 " + a.name + " 的资源"}
                  >
                    <Avatar agent={a} />
                    <span className="nav-text">{a.name}</span>
                    <span className="agent-dot" />
                  </button>
                ))}
              {snapshot &&
              !snapshot.agents.some((a) => a.detected && a.id !== "agents") ? (
                <button
                  className="nav-button"
                  onClick={() => navigate("agents")}
                >
                  <Icon name="plus" />
                  <span className="nav-text">添加你的 Agent</span>
                </button>
              ) : null}
            </div>
            <div className="sidebar-bottom">
              <div
                className={"saved " + (issues.length ? "has-issues" : "")}
                title={busy || issues[0] || "资源库已读取，变更时写入本机配置"}
              >
                {busy ? <Icon name="spinner" /> : <span />}
                <span className="saved-text">
                  {busy
                    ? "正在处理…"
                    : issues.length
                      ? "有配置需要处理"
                      : snapshot
                        ? "配置已读取"
                        : "正在连接…"}
                </span>
              </div>
              {nav("settings", "settings", "设置")}
              <button
                className="nav-button"
                onClick={() => setDialog({ type: "shortcuts" })}
                title="快捷键"
              >
                <Icon name="terminal" />
                <span className="nav-text">快捷键</span>
                <kbd className="shortcut">?</kbd>
              </button>
            </div>
          </aside>
          <main className="main" aria-busy={Boolean(busy)}>
            {snapshot ? (
              <>
                {issues.length ? (
                  <section className="app-banner" role="alert">
                    <Icon name="warning" />
                    <div>
                      <strong>部分配置需要处理</strong>
                      <details>
                        <summary>
                          {issues[0]}
                          {issues.length > 1
                            ? "（共 " + issues.length + " 项）"
                            : ""}
                        </summary>
                        {issues.map((issue, i) => (
                          <p key={i}>{issue}</p>
                        ))}
                      </details>
                    </div>
                    <button
                      className="btn small"
                      disabled={Boolean(busy)}
                      onClick={() => void perform("sync", {}, "重新同步配置")}
                    >
                      <Icon name="refresh" />
                      重试写入
                    </button>
                    <button
                      className="icon-button"
                      disabled={Boolean(busy)}
                      onClick={() => void app.refresh()}
                      aria-label="重新读取配置"
                    >
                      <Icon name="undo" />
                    </button>
                  </section>
                ) : null}
                {page === "mcp" || page === "skills" ? (
                  <Library
                    key={page + agentFilter}
                    snapshot={snapshot}
                    kind={page}
                    agentFilter={agentFilter}
                    selected={selected}
                    onSelect={setSelected}
                    onDialog={setDialog}
                    onDiscover={() => navigate("discover")}
                    onAllocation={onAllocation}
                    onPause={onPause}
                    busy={busy}
                    notify={app.notify}
                  />
                ) : page === "discover" ? (
                  <Discover
                    snapshot={snapshot}
                    onDialog={setDialog}
                    onOpen={openResource}
                    busy={busy}
                  />
                ) : page === "agents" ? (
                  <AgentsPage
                    snapshot={snapshot}
                    busy={busy}
                    onRefresh={() => void app.refresh()}
                    onOpen={openAgent}
                    notify={app.notify}
                  />
                ) : (
                  <SettingsPage
                    snapshot={snapshot}
                    info={app.info}
                    theme={app.theme}
                    setTheme={app.setTheme}
                    notify={app.notify}
                    busy={busy}
                    onDialog={setDialog}
                  />
                )}
              </>
            ) : (
              <div className="startup-state" role="status">
                <Icon name={app.loading ? "spinner" : "warning"} />
                <h1>{app.loading ? "正在读取本机资源库" : "无法读取资源库"}</h1>
                <p>
                  {app.loading
                    ? "检测 Agent 并读取 MCP 与 Skills 配置…"
                    : app.alert.join("\n")}
                </p>
                {!app.loading ? (
                  <button
                    className="btn primary"
                    onClick={() => void app.refresh()}
                  >
                    <Icon name="refresh" />
                    重新读取
                  </button>
                ) : null}
              </div>
            )}
          </main>
        </div>
      </div>
      {dialog && snapshot ? (
        <Dialogs
          key={
            dialog.type +
            (dialog.type === "server"
              ? dialog.entry?.name || "new"
              : dialog.type === "market-server"
                ? dialog.entry.id
                : dialog.type === "delete"
                  ? dialog.entry.name
                  : "")
          }
          dialog={dialog}
          snapshot={snapshot}
          mutate={app.mutate}
          busy={busy}
          onClose={() => setDialog(null)}
          onDone={done}
        />
      ) : null}
      <div aria-live="polite">
        {app.toast ? (
          <div className="toast">
            <Icon name="info" />
            <span>{app.toast}</span>
          </div>
        ) : null}
      </div>
      {busy ? (
        <div className="operation-status" role="status">
          <Icon name="spinner" />
          <span>{busy}…</span>
        </div>
      ) : null}
    </div>
  );
}
export default function App() {
  return (
    <ErrorBoundary>
      <Application />
    </ErrorBoundary>
  );
}
