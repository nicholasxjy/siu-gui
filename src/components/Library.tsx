import { useDeferredValue, useEffect, useMemo, useState } from "react";
import type {
  Agent,
  DialogState,
  Kind,
  Resource,
  SkillEntry,
  Snapshot,
} from "../types";
import {
  agentLabel,
  description,
  displayName,
  groupName,
  isMcp,
  shortPath,
  stateLabel,
} from "../utils";
import { Avatar, Icon, ToolIcon } from "../icons";
import type { IconName } from "../icons";
import { Inspector } from "./Inspector";

interface Props {
  snapshot: Snapshot;
  kind: Kind;
  agentFilter: string;
  onClearAgent: () => void;
  selected: string;
  onSelect: (name: string) => void;
  onDialog: (dialog: DialogState) => void;
  onDiscover: () => void;
  onAllocation: (entry: Resource, agent: Agent) => void;
  onPause: (entry: Resource) => void;
  busy: string;
  notify: (message: string) => void;
}
export function Library({
  snapshot,
  kind,
  agentFilter,
  onClearAgent,
  selected,
  onSelect,
  onDialog,
  onDiscover,
  onAllocation,
  onPause,
  busy,
  notify,
}: Props) {
  const [query, setQuery] = useState(""),
    [status, setStatus] = useState("all"),
    [drawer, setDrawer] = useState(false),
    [collapsed, setCollapsed] = useState<Set<string>>(() => {
      try {
        return new Set(
          JSON.parse(
            localStorage.getItem("siu.collapsed.v1") || "[]",
          ) as string[],
        );
      } catch {
        return new Set();
      }
    });
  const deferred = useDeferredValue(query).trim().toLowerCase(),
    entries: Resource[] = kind === "mcp" ? snapshot.mcp : snapshot.skills;
  const agents = useMemo(
    () =>
      snapshot.agents
        .filter(
          (a) =>
            a.detected &&
            (kind === "mcp" ? a.mcpPath : a.skillsPath) &&
            a.id !== "agents",
        )
        .concat(
          kind === "skills" && entries.some((x) => x.agents.includes("agents"))
            ? snapshot.agents.filter((a) => a.id === "agents")
            : [],
        ),
    [snapshot.agents, entries, kind],
  );
  const shown = useMemo(
    () =>
      entries.filter(
        (entry) =>
          (
            entry.name +
            " " +
            description(entry) +
            " " +
            (!("transport" in entry) ? groupName(entry) : "")
          )
            .toLowerCase()
            .includes(deferred) &&
          (status === "all" ||
            (status === "paused"
              ? !entry.enabled
              : status === "error"
                ? Object.values(entry.states).includes("error")
                : entry.enabled)) &&
          (!agentFilter || entry.agents.includes(agentFilter)),
      ),
    [entries, deferred, status, agentFilter],
  );
  const entry = shown.find((x) => x.name === selected) || shown[0],
    filterAgent = snapshot.agents.find((x) => x.id === agentFilter),
    found =
      kind === "mcp"
        ? snapshot.foundMcp.map((x) => ({
            name: x.server.name,
            agents: x.agents,
          }))
        : snapshot.foundSkills.map((x) => ({
            name: x.name,
            agents: x.at.map((a) => a.agent),
          }));
  const groups =
    kind === "skills"
      ? [...new Set(shown.map((x) => groupName(x as SkillEntry)))]
      : [];
  useEffect(() => {
    try {
      localStorage.setItem("siu.collapsed.v1", JSON.stringify([...collapsed]));
    } catch {
      /* Optional display preference. */
    }
  }, [collapsed]);
  const pausedCount = entries.filter((x) => !x.enabled).length,
    errorCount = entries.filter((x) =>
      Object.values(x.states).includes("error"),
    ).length;
  const select = (name: string) => {
    onSelect(name);
    setDrawer(true);
  };
  const showUnmanaged =
    !deferred && !agentFilter && status === "all" && found.length > 0;
  const unmanagedPanel = showUnmanaged ? (
    <section className="unmanaged-panel" aria-label="本机发现的未托管资源">
      <div className="unmanaged-panel-head">
        <div className="unmanaged-panel-title">
          <Icon name="download" />
          <strong>在 Agent 中发现 {found.length} 项已有配置</strong>
          <span className="unmanaged-panel-sub">
            尚未由 siu 托管，可一键导入统一管理
          </span>
        </div>
      </div>
      <div className="unmanaged-list">
        {found.map((x) => (
          <div className="unmanaged" key={x.name}>
            <ToolIcon name={x.name} kind={kind} />
            <div className="unmanaged-info">
              <strong>{x.name}</strong>
              <p>
                来自{" "}
                {x.agents
                  .map(
                    (id) =>
                      snapshot.agents.find((a) => a.id === id)?.name || id,
                  )
                  .join("、")}
              </p>
            </div>
            <button
              className="btn small"
              disabled={Boolean(busy)}
              onClick={() => onDialog({ type: "import", kind, name: x.name })}
            >
              <Icon name="download" />
              导入管理
            </button>
          </div>
        ))}
      </div>
    </section>
  ) : null;
  const resourceRows = (items: Resource[]) =>
    items.map((item) => {
      const hasError = Object.values(item.states).includes("error");
      return (
        <tr
          key={item.name}
          className={
            (entry?.name === item.name ? "selected " : "") +
            (!item.enabled ? "is-paused" : "")
          }
        >
          <td
            onClick={(event) => {
              if ((event.target as HTMLElement).closest("button")) return;
              select(item.name);
            }}
          >
            <div className="resource-cell">
              <ToolIcon name={item.name} kind={kind} />
              <button
                className="resource-title"
                aria-pressed={entry?.name === item.name}
                onClick={() => select(item.name)}
                title={item.name}
                aria-label={"查看 " + displayName(item.name, kind) + " 详情"}
              >
                <span className="resource-name-row">
                  <span className="resource-name">
                    {displayName(item.name, kind)}
                  </span>
                  {isMcp(item) ? (
                    <span className="transport-tag">
                      {item.transport === "stdio"
                        ? "stdio"
                        : item.transport.toUpperCase()}
                    </span>
                  ) : null}
                  {!item.enabled ? (
                    <span className="inline-label">已暂停</span>
                  ) : null}
                  {hasError ? (
                    <span className="inline-label danger">写入未完成</span>
                  ) : null}
                </span>
                <span className="resource-description">
                  {description(item) || "暂无描述"}
                </span>
              </button>
            </div>
          </td>
          {agents.map((a) => {
            const state = item.states[a.id] || "off",
              chosen = item.agents.includes(a.id),
              unsupported = state.startsWith("unsupported:"),
              glyph: IconName =
                state === "on"
                  ? "check"
                  : state === "paused"
                    ? "pause"
                    : state === "error"
                      ? "warning"
                      : "check";
            return (
              <td key={a.id}>
                <button
                  disabled={Boolean(busy) || (unsupported && !chosen)}
                  className={
                    "allocation " +
                    (state === "on"
                      ? "on"
                      : state === "paused"
                        ? "paused"
                        : state === "error"
                          ? "write-error"
                          : unsupported
                            ? "is-unsupported"
                            : "")
                  }
                  onClick={() => onAllocation(item, a)}
                  aria-pressed={state === "on"}
                  aria-label={
                    displayName(item.name, kind) +
                    " · " +
                    agentLabel(a) +
                    " · " +
                    stateLabel(state)
                  }
                  title={
                    stateLabel(state) +
                    (unsupported && !chosen
                      ? ""
                      : chosen
                        ? "，点击" +
                          (!item.enabled ? "恢复全部 Agent 分配" : "取消分配")
                        : "，点击分配给 " + agentLabel(a))
                  }
                >
                  <span className="sr-only">{stateLabel(state)}</span>
                  {unsupported ? (
                    <span className="unsupported">—</span>
                  ) : state === "on" ||
                    state === "paused" ||
                    state === "error" ? (
                    <Icon name={glyph} />
                  ) : null}
                </button>
              </td>
            );
          })}
        </tr>
      );
    });
  const table = (
    <table
      className="matrix"
      style={{
        minWidth: `calc(var(--resource-column-width) + ${agents.length * 68}px)`,
      }}
    >
      <caption className="sr-only">资源与 Agent 配置分配矩阵</caption>
      <thead>
        <tr>
          <th scope="col">{kind === "mcp" ? "服务名称" : "Skill"}</th>
          {agents.map((a) => (
            <th scope="col" className="agent-col" key={a.id}>
              <Avatar agent={a} />
              <span className="agent-label" title={a.name}>
                {a.id === "claude"
                  ? "Claude"
                  : a.id === "agents"
                    ? "共享目录"
                    : a.name.replace(" CLI", "")}
              </span>
            </th>
          ))}
        </tr>
      </thead>
      {kind === "mcp" ? (
        <tbody>{resourceRows(shown)}</tbody>
      ) : (
        groups.map((group) => {
          const items = shown.filter(
            (item) => groupName(item as SkillEntry) === group,
          );
          return (
            <tbody key={group}>
              <tr className="group-row">
                <th scope="rowgroup" colSpan={agents.length + 1}>
                  <button
                    className="group-heading"
                    aria-expanded={!collapsed.has(group)}
                    onClick={() =>
                      setCollapsed((prev) => {
                        const next = new Set(prev);
                        next.has(group) ? next.delete(group) : next.add(group);
                        return next;
                      })
                    }
                  >
                    <Icon name={collapsed.has(group) ? "chevron" : "down"} />
                    <Icon
                      name={
                        group.includes("/") && !group.startsWith("/")
                          ? "branch"
                          : "folder"
                      }
                    />
                    <strong title={group}>
                      {shortPath(group, snapshot.home)}
                    </strong>
                    <span className="count">{items.length} 项</span>
                  </button>
                </th>
              </tr>
              {collapsed.has(group) ? null : resourceRows(items)}
            </tbody>
          );
        })
      )}
    </table>
  );
  return (
    <>
      <header className="page-head">
        <div>
          <div className="heading-row">
            <h1>
              {filterAgent ? agentLabel(filterAgent) + " 的 " : ""}
              {kind === "mcp" ? "MCP 服务" : "Skills"}
            </h1>
            <span className="number-badge">
              {agentFilter
                ? entries.filter((item) => item.agents.includes(agentFilter))
                    .length
                : entries.length}
            </span>
          </div>
          <p className="subtitle">
            {kind === "mcp"
              ? "统一管理工具，按 Agent 分配。"
              : "一次维护，在每个 Agent 中复用你的工作方式。"}
          </p>
        </div>
        <div className="actions">
          <button
            className="btn"
            disabled={Boolean(busy) || !entries.length}
            onClick={() => onDialog({ type: "update", kind })}
          >
            <Icon name="refresh" />
            {kind === "skills" ? "更新全部" : "更新服务"}
          </button>
          <button
            data-add
            className="btn primary"
            disabled={Boolean(busy)}
            onClick={() =>
              onDialog({ type: kind === "mcp" ? "server" : "skill-source" })
            }
          >
            <Icon name="plus" />
            {kind === "mcp" ? "添加服务" : "添加 Skill"}
          </button>
        </div>
      </header>
      <div className="workspace">
        <section
          className="library"
          aria-label={kind === "mcp" ? "MCP 服务资源列表" : "Skills 资源列表"}
        >
          <div className="library-tools">
            <label className="search">
              <Icon name="search" />
              <input
                data-search
                autoCorrect="off"
                aria-label={kind === "mcp" ? "搜索 MCP 服务" : "搜索 Skills"}
                placeholder={
                  kind === "mcp" ? "搜索名称或描述…" : "搜索 Skills 或来源…"
                }
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
              {query ? (
                <button
                  className="icon-button search-clear"
                  aria-label="清除搜索"
                  onClick={() => setQuery("")}
                >
                  <Icon name="close" />
                </button>
              ) : (
                <kbd>/</kbd>
              )}
            </label>
            <select
              className="filter"
              aria-label="筛选资源状态"
              value={status}
              onChange={(e) => setStatus(e.target.value)}
            >
              <option value="all">全部状态</option>
              <option value="enabled">已启用</option>
              <option value="paused">已暂停</option>
              <option value="error">写入未完成</option>
            </select>
          </div>
          <div className="library-context">
            <div className="context-summary">
              <span role="status">
                {shown.length} 项{kind === "mcp" ? "服务" : " Skills"}
                <span className="context-divider">·</span>
                {agents.length} 个可分配 Agent
              </span>
              {pausedCount > 0 && status !== "paused" ? (
                <button
                  className="status-pill amber"
                  onClick={() => setStatus("paused")}
                >
                  {pausedCount} 已暂停
                </button>
              ) : null}
              {errorCount > 0 && status !== "error" ? (
                <button
                  className="status-pill danger"
                  onClick={() => setStatus("error")}
                >
                  {errorCount} 需处理
                </button>
              ) : null}
            </div>
            <div className="context-actions">
              {filterAgent ? (
                <button className="filter-chip" onClick={onClearAgent}>
                  <Avatar agent={filterAgent} />
                  <span>仅看 {agentLabel(filterAgent)}</span>
                  <Icon name="close" />
                  <span className="sr-only">清除 Agent 筛选</span>
                </button>
              ) : null}
              {query || status !== "all" ? (
                <button
                  className="text-button"
                  onClick={() => {
                    setQuery("");
                    setStatus("all");
                  }}
                >
                  清除筛选
                </button>
              ) : null}
            </div>
          </div>
          <div className="list-scroll">
            {entries.length > 0 ? unmanagedPanel : null}
            {!entries.length ? (
              <div className="empty">
                <Icon name={kind === "mcp" ? "plug" : "skill"} />
                <h2>
                  {kind === "mcp"
                    ? "把常用工具放进 siu"
                    : "添加你的第一个 Skill"}
                </h2>
                <p>
                  {kind === "mcp"
                    ? "从发现页安装服务，或导入 Agent 中已有的配置。"
                    : "从 GitHub 或本地目录添加，按 Agent 复用工作流程。"}
                </p>
                <div className="empty-actions">
                  <button
                    className="btn primary"
                    onClick={
                      kind === "mcp"
                        ? onDiscover
                        : () => onDialog({ type: "skill-source" })
                    }
                  >
                    <Icon name={kind === "mcp" ? "compass" : "plus"} />
                    {kind === "mcp" ? "发现 MCP 服务" : "添加 Skill"}
                  </button>
                  {kind === "mcp" ? (
                    <button
                      className="btn"
                      onClick={() => onDialog({ type: "server" })}
                    >
                      <Icon name="plus" />
                      手动填写配置
                    </button>
                  ) : null}
                </div>
              </div>
            ) : !shown.length ? (
              <div className="empty">
                <Icon name="search" />
                <h2>没有匹配的{kind === "mcp" ? "服务" : " Skill"}</h2>
                <p>试试其他关键词，或清除当前筛选。</p>
                <button
                  className="btn"
                  onClick={() => {
                    setQuery("");
                    setStatus("all");
                    if (agentFilter) onClearAgent();
                  }}
                >
                  清除筛选
                </button>
              </div>
            ) : (
              table
            )}
            {entries.length === 0 ? unmanagedPanel : null}
          </div>
          <div className="list-footer">
            <span className="legend">
              <Icon name="check" />
              已配置
            </span>
            <span className="legend">
              <i className="legend-empty" />
              未配置
            </span>
            <span className="legend amber">
              <Icon name="pause" />
              已暂停
            </span>
            <span className="footer-right">
              点击行查看详情 · 点击方格开关对应 Agent
            </span>
          </div>
        </section>
        {entry ? (
          <Inspector
            entry={entry}
            kind={kind}
            snapshot={snapshot}
            open={drawer}
            onClose={() => setDrawer(false)}
            onAllocation={onAllocation}
            onPause={onPause}
            onDialog={(dialog) => {
              setDrawer(false);
              onDialog(dialog);
            }}
            busy={busy}
            notify={notify}
          />
        ) : null}
      </div>
    </>
  );
}
