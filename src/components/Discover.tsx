import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import type { DialogState, Discovery, Kind, Snapshot } from "../types";
import { Avatar, Brand, Icon, ToolIcon } from "../icons";
import { Note } from "./Modal";
interface Props {
  snapshot: Snapshot;
  onDialog: (dialog: DialogState) => void;
  onOpen: (kind: Kind, name: string) => void;
  busy: string;
}
const categories = [
  { label: "推荐", mcp: "", skills: "" },
  { label: "文档与搜索", mcp: "search", skills: "documentation" },
  { label: "开发与自动化", mcp: "browser", skills: "testing" },
  { label: "设计", mcp: "figma", skills: "design" },
];
export function Discover({ snapshot, onDialog, onOpen, busy }: Props) {
  const [kind, setKind] = useState<Kind>("mcp"),
    [query, setQuery] = useState(""),
    [debounced, setDebounced] = useState(""),
    [data, setData] = useState<Discovery | null>(null),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(""),
    [revision, setRevision] = useState(0);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(query.trim()), 300);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    let current = true;
    setLoading(true);
    setError("");
    setData(null);
    void api
      .request<Discovery>("discover", {
        kind,
        query: debounced,
        refresh: revision > 0,
      })
      .then((value) => {
        if (current) setData(value);
      })
      .catch((e) => {
        if (current) setError(errorMessage(e));
      })
      .finally(() => {
        if (current) setLoading(false);
      });
    return () => {
      current = false;
    };
  }, [kind, debounced, revision]);
  const changeKind = (next: Kind) => {
    setKind(next);
    setQuery("");
    setDebounced("");
    setRevision(0);
  };
  const detected = snapshot.agents
      .filter((a) => a.detected && a.id !== "agents")
      .slice(0, 4),
    count = kind === "mcp" ? data?.servers.length : data?.skills.length;
  return (
    <>
      <header className="page-head">
        <div>
          <div className="heading-row">
            <h1>发现</h1>
          </div>
          <p className="subtitle">为你的 Agent 找到合适的工具和技能。</p>
        </div>
        <div className="actions">
          <button
            data-add
            className="btn"
            disabled={Boolean(busy)}
            onClick={() =>
              onDialog({ type: kind === "mcp" ? "server" : "skill-source" })
            }
          >
            <Icon name="plus" />
            {kind === "mcp" ? "手动添加" : "从来源添加"}
          </button>
        </div>
      </header>
      <div className="page-scroll">
        <div className="discover-toolbar">
          <div className="segmented" role="tablist" aria-label="发现资源类型">
            <button
              role="tab"
              aria-selected={kind === "mcp"}
              className={kind === "mcp" ? "active" : ""}
              onClick={() => changeKind("mcp")}
            >
              MCP 服务
            </button>
            <button
              role="tab"
              aria-selected={kind === "skills"}
              className={kind === "skills" ? "active" : ""}
              onClick={() => changeKind("skills")}
            >
              Skills
            </button>
          </div>
          <label className="search discover-search">
            <Icon name="search" />
            <input
              data-search
              aria-label="搜索发现资源"
              placeholder={
                kind === "mcp"
                  ? "在 MCP Registry 搜索服务…"
                  : "在 skills.sh 搜索技能…"
              }
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <kbd>/</kbd>
          </label>
          <button
            className="icon-button"
            disabled={loading}
            onClick={() => setRevision((x) => x + 1)}
            aria-label="刷新发现结果"
            title="刷新发现结果"
          >
            <Icon name={loading ? "spinner" : "refresh"} />
          </button>
        </div>
        {!query ? (
          <div className="discover-intro">
            <div>
              <h2>好用的能力，放在一起。</h2>
              <p>
                发现、安装，再分配给你常用的 Agent。
                <br />
                已有的工具和配置，也可以一起管理。
              </p>
            </div>
            <div className="connection-art" aria-hidden="true">
              <ToolIcon
                name={kind === "mcp" ? "context7" : "skill"}
                kind={kind}
                large
              />
              <span className="connection-line" />
              <Brand />
              <span className="connection-line" />
              <div className="connection-nodes">
                {detected.map((a) => (
                  <Avatar key={a.id} agent={a} />
                ))}
              </div>
            </div>
          </div>
        ) : null}
        <div className="category-row">
          {categories.map((c) => (
            <button
              className={"category " + (query === c[kind] ? "active" : "")}
              key={c.label}
              onClick={() => setQuery(c[kind])}
            >
              {c.label}
            </button>
          ))}
          <span className="source-label">
            <Icon name="external" />
            {kind === "mcp" ? "MCP Registry" : "skills.sh"}
            {data?.cached ? " · 缓存结果" : ""}
            {count !== undefined ? " · " + count + " 项" : ""}
          </span>
        </div>
        {data?.warnings.length ? (
          <div className="discovery-warning">
            <Note warning>
              {data.warnings.map((warning, i) => (
                <p key={i}>{warning}</p>
              ))}
            </Note>
          </div>
        ) : null}
        {loading ? (
          <div className="empty loading-state" role="status">
            <Icon name="spinner" />
            <h2>正在查找{kind === "mcp" ? "工具" : "技能"}</h2>
            <p>
              从{kind === "mcp" ? " MCP Registry" : " skills.sh"} 获取最新结果…
            </p>
          </div>
        ) : error ? (
          <div className="empty">
            <Icon name="warning" />
            <h2>暂时无法获取资源</h2>
            <p className="error-description">{error}</p>
            <button className="btn" onClick={() => setRevision((x) => x + 1)}>
              <Icon name="refresh" />
              重试
            </button>
          </div>
        ) : data && count ? (
          <div className="cards">
            {kind === "mcp"
              ? data.servers.map((item) => {
                  const installed = snapshot.mcp.find(
                    (x) => x.name === item.server.name || x.origin === item.id,
                  );
                  return (
                    <article className="market-card" key={item.id}>
                      <div className="card-top">
                        <ToolIcon name={item.server.name} />
                        <div>
                          <h3 title={item.title}>{item.title}</h3>
                          <div className="card-source" title={item.publisher}>
                            {item.publisher || item.id}
                          </div>
                        </div>
                        {item.homepage ? (
                          <button
                            className="icon-button card-link"
                            onClick={() =>
                              void api
                                .openURL(item.homepage)
                                .catch((e) => setError(errorMessage(e)))
                            }
                            title="查看来源"
                            aria-label={"查看 " + item.title + " 来源"}
                          >
                            <Icon name="external" />
                          </button>
                        ) : null}
                      </div>
                      <p>
                        {item.description ||
                          "查看来源与配置，确认是否适合你的 Agent。"}
                      </p>
                      <div className="card-bottom">
                        <span className="pill">{item.server.transport}</span>
                        {installed ? (
                          <button
                            className="btn small ghost"
                            onClick={() => onOpen("mcp", installed.name)}
                          >
                            <span className="installed-label">
                              <Icon name="check" />
                              已添加
                            </span>
                            <Icon name="chevron" />
                          </button>
                        ) : (
                          <button
                            className="btn small"
                            disabled={Boolean(busy)}
                            onClick={() =>
                              onDialog({ type: "market-server", entry: item })
                            }
                          >
                            <Icon name="plus" />
                            安装
                          </button>
                        )}
                      </div>
                    </article>
                  );
                })
              : data.skills.map((item) => {
                  const installed = snapshot.skills.find(
                    (x) =>
                      x.name === item.name ||
                      x.name === item.skillId ||
                      (x.source?.kind === "github" &&
                        x.source.repo === item.source &&
                        x.source.path.split("/").at(-1) === item.skillId),
                  );
                  return (
                    <article
                      className="market-card"
                      key={item.source + "/" + item.skillId}
                    >
                      <div className="card-top">
                        <ToolIcon name={item.name} kind="skills" />
                        <div>
                          <h3 title={item.name}>{item.name}</h3>
                          <div className="card-source" title={item.source}>
                            {item.source}
                          </div>
                        </div>
                        <button
                          className="icon-button card-link"
                          onClick={() =>
                            void api
                              .openURL(
                                "https://skills.sh/" +
                                  item.source +
                                  "/" +
                                  encodeURIComponent(item.skillId),
                              )
                              .catch((e) => setError(errorMessage(e)))
                          }
                          title="查看来源"
                          aria-label={"查看 " + item.name + " 来源"}
                        >
                          <Icon name="external" />
                        </button>
                      </div>
                      <p>
                        从此仓库选择并安装所需
                        Skill。安装前可查看具体说明与分配目标。
                      </p>
                      <div className="card-bottom">
                        <span className="skill-installs">
                          {item.installs > 0
                            ? new Intl.NumberFormat("zh-CN", {
                                notation: "compact",
                              }).format(item.installs) + " 次安装"
                            : "Skill"}
                          {item.isOfficial ? " · 来源已标记官方" : ""}
                        </span>
                        {installed ? (
                          <button
                            className="btn small ghost"
                            onClick={() => onOpen("skills", installed.name)}
                          >
                            <span className="installed-label">
                              <Icon name="check" />
                              已添加
                            </span>
                            <Icon name="chevron" />
                          </button>
                        ) : (
                          <button
                            className="btn small"
                            disabled={Boolean(busy)}
                            onClick={() =>
                              onDialog({
                                type: "skill-source",
                                source: item.source,
                                preselect: item.skillId,
                              })
                            }
                          >
                            <Icon name="plus" />
                            安装
                          </button>
                        )}
                      </div>
                    </article>
                  );
                })}
          </div>
        ) : (
          <div className="empty">
            <Icon name="search" />
            <h2>暂时没有找到相关资源</h2>
            <p>试试其他关键词，或从链接手动添加。</p>
          </div>
        )}
        <div className="market-footer">
          <Icon name="info" />
          <span>安装前可以确认来源、配置内容与目标 Agent。</span>
        </div>
      </div>
    </>
  );
}
