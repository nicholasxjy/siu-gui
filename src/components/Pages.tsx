import { useState } from "react";
import { api, errorMessage } from "../api";
import { APP_VERSION } from "../version";
import type {
  AppInfo,
  DialogState,
  Kind,
  Mutate,
  MutationResult,
  Snapshot,
  Theme,
} from "../types";
import { Avatar, Icon } from "../icons";
import { agentLabel, shortPath } from "../utils";
import { Note } from "./Modal";
import { ProxySettingsForm } from "./ProxySettings";
import { ConfigTransfer } from "./ConfigTransfer";
export function AgentsPage({
  snapshot,
  busy,
  onRefresh,
  onOpen,
  notify,
}: {
  snapshot: Snapshot;
  busy: string;
  onRefresh: () => void;
  onOpen: (kind: Kind, agent: string) => void;
  notify: (message: string) => void;
}) {
  const detected = snapshot.agents.filter(
      (a) => a.detected && a.id !== "agents",
    ),
    undetected = snapshot.agents.filter(
      (a) => !a.detected && a.id !== "agents",
    );
  const openPath = (path: string) =>
    void api.openPath(path).catch((e) => notify(errorMessage(e)));
  return (
    <>
      <header className="page-head">
        <div>
          <div className="heading-row">
            <h1>本机 Agent</h1>
            <span className="number-badge">{detected.length}</span>
          </div>
          <p className="subtitle">
            查看已检测到的 Agent，以及 siu 管理的配置位置。
          </p>
        </div>
        <div className="actions">
          <button className="btn" disabled={Boolean(busy)} onClick={onRefresh}>
            <Icon name="refresh" />
            重新扫描
          </button>
        </div>
      </header>
      <div className="page-scroll">
        {detected.length ? (
          <div className="agent-page-grid">
            {detected.map((a) => (
              <article className="agent-card" key={a.id}>
                <div className="agent-card-header">
                  <Avatar agent={a} />
                  <h2>{a.name}</h2>
                  <span className="detected">已检测到</span>
                </div>
                <dl>
                  <dt>MCP 配置</dt>
                  <dd>
                    {a.mcpPath ? (
                      <button
                        className="text-path"
                        onClick={() => openPath(a.mcpPath!)}
                        title={a.mcpPath}
                      >
                        {shortPath(a.mcpPath, snapshot.home)}
                        <Icon name="external" />
                      </button>
                    ) : (
                      "不支持"
                    )}
                  </dd>
                  <dt>Skills 目录</dt>
                  <dd>
                    {a.skillsPath ? (
                      <button
                        className="text-path"
                        onClick={() => openPath(a.skillsPath!)}
                        title={a.skillsPath}
                      >
                        {shortPath(a.skillsPath, snapshot.home)}
                        <Icon name="external" />
                      </button>
                    ) : (
                      "不支持"
                    )}
                  </dd>
                  <dt>协议支持</dt>
                  <dd>
                    {a.transports.length
                      ? a.transports
                          .map((x) => (x === "stdio" ? x : x.toUpperCase()))
                          .join(" / ")
                      : "无 MCP 配置"}
                  </dd>
                </dl>
                <div className="agent-card-footer">
                  <span>
                    {snapshot.mcp.filter((x) => x.states[a.id] === "on").length}{" "}
                    MCP 服务 ·{" "}
                    {
                      snapshot.skills.filter((x) => x.states[a.id] === "on")
                        .length
                    }{" "}
                    Skills
                  </span>
                  <button
                    className="btn small"
                    onClick={() => onOpen(a.mcpPath ? "mcp" : "skills", a.id)}
                  >
                    查看分配
                    <Icon name="chevron" />
                  </button>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <div className="empty">
            <Icon name="monitor" />
            <h2>还没有检测到 Agent</h2>
            <p>
              安装并首次运行一个 Agent，再点击「重新扫描」。
              <br />
              siu 根据 Agent 的配置目录识别本机安装情况。
            </p>
            <button className="btn" onClick={onRefresh}>
              <Icon name="refresh" />
              重新扫描
            </button>
          </div>
        )}
        <div className="note" style={{ maxWidth: 1000, marginTop: 21 }}>
          <Icon name="info" />
          <span>
            仅对检测到的 Agent 提供分配。共享 Skills
            目录可在安装时单独选择。Claude Desktop 仅支持 stdio MCP。
          </span>
        </div>
        {undetected.length ? (
          <section className="undetected-agents">
            <h2>其他支持的 Agent</h2>
            <p>安装并启动后，siu 会识别以下配置目录。</p>
            {undetected.map((a) => (
              <div className="undetected-row" key={a.id}>
                <Avatar agent={a} />
                <span>{agentLabel(a)}</span>
                <code title={a.home}>{shortPath(a.home, snapshot.home)}</code>
                <span className="quiet">未检测到</span>
              </div>
            ))}
          </section>
        ) : null}
      </div>
    </>
  );
}
export function SettingsPage({
  snapshot,
  info,
  theme,
  setTheme,
  notify,
  busy,
  onDialog,
  mutate,
  onDone,
}: {
  snapshot: Snapshot;
  info: AppInfo;
  theme: Theme;
  setTheme: (theme: Theme) => void;
  notify: (message: string) => void;
  busy: string;
  onDialog: (dialog: DialogState) => void;
  mutate: Mutate;
  onDone: (result: MutationResult, title?: string) => void;
}) {
  const [proxyGeneration, setProxyGeneration] = useState(0);
  const restored = (result: MutationResult) => {
    setProxyGeneration((generation) => generation + 1);
    if (result.theme) setTheme(result.theme);
    onDone(result, "恢复配置");
  };
  const open = (path: string) =>
      void api.openPath(path).catch((e) => notify(errorMessage(e))),
    visit = (url: string) =>
      void api.openURL(url).catch((e) => notify(errorMessage(e)));
  return (
    <>
      <header className="page-head">
        <div>
          <div className="heading-row">
            <h1>设置</h1>
          </div>
          <p className="subtitle">让 siu 适合你的工作习惯。</p>
        </div>
      </header>
      <div className="page-scroll">
        <div className="settings">
          <section className="settings-section">
            <h2>外观</h2>
            <div className="setting-row">
              <div>
                <h3>界面主题</h3>
                <p>更改后同时应用到窗口与界面。</p>
              </div>
              <div className="theme-choices">
                {(
                  [
                    { value: "system", label: "跟随系统", icon: "monitor" },
                    { value: "light", label: "浅色", icon: "sun" },
                    { value: "dark", label: "深色", icon: "moon" },
                  ] as const
                ).map((t) => (
                  <button
                    key={t.value}
                    aria-pressed={theme === t.value}
                    className={
                      "theme-choice " + (theme === t.value ? "active" : "")
                    }
                    onClick={() => setTheme(t.value)}
                  >
                    <Icon name={t.icon} />
                    {t.label}
                  </button>
                ))}
              </div>
            </div>
            <div className="setting-row">
              <div>
                <h3>界面语言</h3>
                <p>界面使用简体中文，资源内容保留其原始语言。</p>
              </div>
              <span className="pill">简体中文</span>
            </div>
          </section>
          <ProxySettingsForm
            key={proxyGeneration}
            busy={busy}
            notify={notify}
          />
          <ConfigTransfer
            snapshot={snapshot}
            theme={theme}
            busy={busy}
            mutate={mutate}
            onDone={restored}
          />
          <section className="settings-section">
            <h2>本机数据</h2>
            <div className="setting-row">
              <div>
                <h3>资源库目录</h3>
                <p>
                  与 siu 终端版共享资源定义、Skills 文件和写入记录。
                  <br />
                  通过 SIU_HOME 环境变量设置其他位置。
                </p>
                <code>{shortPath(snapshot.root, snapshot.home)}</code>
              </div>
              <button className="btn small" onClick={() => open(snapshot.root)}>
                <Icon name="folder" />
                打开目录
              </button>
            </div>
            <div className="setting-row">
              <div>
                <h3>配置备份</h3>
                <p>首次修改配置前保留原文件，导入 Skill 时备份原目录。</p>
                <code>{shortPath(snapshot.root, snapshot.home)}/backups</code>
              </div>
              <button
                className="btn small"
                onClick={() => open(snapshot.root + "/backups")}
              >
                <Icon name="folder" />
                查看备份
              </button>
            </div>
            <div style={{ marginTop: 16 }}>
              <Note>
                配置状态表示写入结果。部分 Agent
                需要重新载入配置或重新启动，才会使用更新后的工具。
              </Note>
            </div>
          </section>
          <section className="settings-section">
            <h2>资源管理</h2>
            {(
              [
                ["mcp", "MCP 服务", snapshot.mcp.length],
                ["skills", "Skills", snapshot.skills.length],
              ] as const
            ).map(([kind, label, count]) => (
              <div className="setting-row" key={kind}>
                <div>
                  <h3>清空 {label}</h3>
                  <p>删除资源库中的 {count} 项资源，并撤回 siu 写入的分配。</p>
                </div>
                <button
                  className="btn danger small"
                  disabled={Boolean(busy) || count === 0}
                  onClick={() => onDialog({ type: "clear", kind })}
                >
                  <Icon name="trash" />
                  清空{label}
                </button>
              </div>
            ))}
          </section>
          <section className="settings-section">
            <h2>关于</h2>
            <div className="setting-row">
              <div>
                <h3>
                  siu GUI{" "}
                  <span className="quiet">{info.version || APP_VERSION}</span>
                </h3>
                <p>
                  统一管理 coding agent 的 MCP 与 Skills。
                  <br />
                  {info.os || snapshot.platform} {info.arch} · siu 核心{" "}
                  {snapshot.version}
                </p>
              </div>
              <div className="about-actions">
                <button
                  className="btn small"
                  onClick={() =>
                    visit("https://github.com/nicholasxjy/siu-gui/releases")
                  }
                >
                  <Icon name="download" />
                  查看新版本
                </button>
                <button
                  className="btn small"
                  onClick={() =>
                    visit("https://github.com/nicholasxjy/siu-gui")
                  }
                >
                  <Icon name="external" />
                  GitHub
                </button>
              </div>
            </div>
            <div className="about-credit">
              <button
                className="text-button"
                onClick={() => visit("https://github.com/egoist/mygo")}
              >
                基于 mygo
              </button>
              <span>·</span>
              <button
                className="text-button"
                onClick={() => visit("https://github.com/nicholasxjy/siu")}
              >
                siu 核心
              </button>
              <span>·</span>
              <span>MIT License</span>
            </div>
          </section>
        </div>
      </div>
    </>
  );
}
