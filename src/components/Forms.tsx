import { useEffect, useId, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { api, errorMessage } from "../api";
import { Avatar, Icon, ToolIcon } from "../icons";
import type {
  DialogState,
  Kind,
  MarketServer,
  McpEntry,
  Mutate,
  MutationResult,
  Resource,
  ScanResult,
  ServerConfig,
  Snapshot,
  Transport,
} from "../types";
import {
  agentLabel,
  compatible,
  displayName,
  emptyServer,
  shortPath,
} from "../utils";
import { Modal, Note } from "./Modal";
interface Common {
  snapshot: Snapshot;
  mutate: Mutate;
  busy: string;
  onClose: () => void;
  onDone: (result: MutationResult, title?: string) => void;
}
export function Targets({
  snapshot,
  kind,
  transport = "stdio",
  selected,
  onChange,
  disabled = false,
}: {
  snapshot: Snapshot;
  kind: Kind;
  transport?: Transport;
  selected: string[];
  onChange: (ids: string[]) => void;
  disabled?: boolean;
}) {
  const available = snapshot.agents.filter(
      (a) =>
        a.detected &&
        a.id !== "agents" &&
        (kind === "mcp" ? a.mcpPath : a.skillsPath),
    ),
    all = available
      .filter((a) => compatible(a, kind, transport))
      .map((a) => a.id),
    shared = snapshot.agents.find((a) => a.id === "agents");
  return (
    <>
      <div className="section-title">
        分配给 Agent
        <span>
          <button
            type="button"
            className="text-button"
            disabled={disabled}
            onClick={() => onChange(all)}
          >
            全选
          </button>
          <span className="quiet"> / </span>
          <button
            type="button"
            className="text-button"
            disabled={disabled}
            onClick={() => onChange([])}
          >
            清空
          </button>
        </span>
      </div>
      {available.length ? (
        available.map((a) => {
          const allowed = compatible(a, kind, transport);
          return (
            <label
              className={"checkbox-row" + (!allowed ? " unavailable" : "")}
              key={a.id}
            >
              <input
                type="checkbox"
                value={a.id}
                checked={allowed && selected.includes(a.id)}
                disabled={disabled || !allowed}
                onChange={(e) =>
                  onChange(
                    e.target.checked
                      ? [...selected, a.id]
                      : selected.filter((id) => id !== a.id),
                  )
                }
              />
              <Avatar agent={a} />
              <span>{a.name}</span>
              <span className="path">
                {allowed
                  ? shortPath(
                      (kind === "mcp" ? a.mcpPath : a.skillsPath) || "",
                      snapshot.home,
                    )
                  : "不支持 " + transport.toUpperCase()}
              </span>
            </label>
          );
        })
      ) : (
        <Note>
          未检测到可用 Agent。可以先保存到资源库，安装并启动 Agent 后再分配。
        </Note>
      )}
      {kind === "skills" && shared ? (
        <details className="shared-box" open={selected.includes("agents")}>
          <summary>共享目录（可选）</summary>
          <p>部分 Agent 也会读取此目录，同时选择可能产生重复展示。</p>
          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={selected.includes("agents")}
              disabled={disabled}
              onChange={(e) =>
                onChange(
                  e.target.checked
                    ? [...selected, "agents"]
                    : selected.filter((id) => id !== "agents"),
                )
              }
            />
            <span>{shortPath(shared.skillsPath || "", snapshot.home)}</span>
          </label>
        </details>
      ) : null}
    </>
  );
}
function defaultTargets(
  snapshot: Snapshot,
  kind: Kind,
  transport: Transport = "stdio",
) {
  return snapshot.agents
    .filter(
      (a) => a.detected && a.id !== "agents" && compatible(a, kind, transport),
    )
    .map((a) => a.id);
}
interface Pair {
  id: string;
  key: string;
  value: string;
}
const toPairs = (values: Record<string, string>) =>
  Object.entries(values).map(([key, value], i) => ({
    id: "original-" + i,
    key,
    value,
  }));
export function pairsToMap(
  pairs: Pick<Pair, "key" | "value">[],
): Record<string, string> {
  const result: Record<string, string> = Object.create(null);
  for (const pair of pairs) {
    if (!pair.key.trim() && !pair.value) continue;
    const key = pair.key.trim();
    if (!key) throw new Error("请填写每一项的名称。");
    if (Object.hasOwn(result, key)) throw new Error("名称重复：" + key);
    result[key] = pair.value;
  }
  return result;
}
function MapFields({
  label,
  rows,
  onChange,
}: {
  label: string;
  rows: Pair[];
  onChange: (rows: Pair[]) => void;
}) {
  const prefix = useId(),
    nextId = useRef(0),
    [visible, setVisible] = useState<Set<string>>(new Set());
  return (
    <div className="field">
      <div className="field-label">
        {label}
        <span className="quiet">可选</span>
      </div>
      {rows.map((row, index) => (
        <div className="pair-row" key={row.id}>
          <input
            aria-label={label + "名称 " + (index + 1)}
            placeholder={label === "请求头" ? "Authorization" : "API_KEY"}
            value={row.key}
            onChange={(e) =>
              onChange(
                rows.map((x) =>
                  x.id === row.id ? { ...x, key: e.target.value } : x,
                ),
              )
            }
          />
          <div className="secret-input">
            <input
              aria-label={label + "值 " + (index + 1)}
              placeholder="值"
              type={visible.has(row.id) ? "text" : "password"}
              autoComplete="off"
              value={row.value}
              onChange={(e) =>
                onChange(
                  rows.map((x) =>
                    x.id === row.id ? { ...x, value: e.target.value } : x,
                  ),
                )
              }
            />
            <button
              type="button"
              className="icon-button"
              aria-label={visible.has(row.id) ? "隐藏值" : "显示值"}
              onClick={() =>
                setVisible((prev) => {
                  const next = new Set(prev);
                  next.has(row.id) ? next.delete(row.id) : next.add(row.id);
                  return next;
                })
              }
            >
              <Icon name={visible.has(row.id) ? "eyeOff" : "eye"} />
            </button>
          </div>
          <button
            type="button"
            className="icon-button"
            onClick={() => onChange(rows.filter((x) => x.id !== row.id))}
            aria-label={"移除" + label + " " + (index + 1)}
          >
            <Icon name="close" />
          </button>
        </div>
      ))}
      <button
        type="button"
        className="btn small"
        onClick={() =>
          onChange([
            ...rows,
            { id: prefix + "-" + nextId.current++, key: "", value: "" },
          ])
        }
      >
        <Icon name="plus" />
        添加{label}
      </button>
    </div>
  );
}
export function ServerForm({ entry, ...props }: Common & { entry?: McpEntry }) {
  const [server, setServer] = useState<ServerConfig>(() =>
      entry
        ? {
            name: entry.name,
            transport: entry.transport,
            command: entry.command || "",
            args: [...(entry.args || [])],
            env: entry.env || {},
            url: entry.url || "",
            headers: entry.headers || {},
          }
        : emptyServer(),
    ),
    [targets, setTargets] = useState(() =>
      entry
        ? entry.agents.filter((id) =>
            props.snapshot.agents.some(
              (a) =>
                a.id === id &&
                a.detected &&
                compatible(a, "mcp", entry.transport),
            ),
          )
        : defaultTargets(props.snapshot, "mcp"),
    ),
    [env, setEnv] = useState(() => toPairs(entry?.env || {})),
    [headers, setHeaders] = useState(() => toPairs(entry?.headers || {})),
    [error, setError] = useState("");
  const id = useId(),
    patch = (value: Partial<ServerConfig>) =>
      setServer((prev) => ({ ...prev, ...value }));
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    try {
      const config = {
        ...server,
        name: server.name.trim(),
        command: server.command.trim(),
        url: server.url.trim(),
        env: pairsToMap(env),
        headers: pairsToMap(headers),
      };
      const result = await props.mutate(
        "save_server",
        {
          server: config,
          agents: targets,
          ...(entry ? { oldName: entry.name } : {}),
        },
        entry ? "保存 " + entry.name : "添加 " + config.name,
      );
      props.onDone(result);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Modal
      title={entry ? "编辑 MCP 服务" : "添加 MCP 服务"}
      subtitle={
        entry
          ? "修改后同步到已选择的 Agent。"
          : "填写连接配置，再选择提供给哪些 Agent。"
      }
      busy={Boolean(props.busy)}
      onClose={props.onClose}
      large
      footer={
        <>
          <button
            className="btn"
            disabled={Boolean(props.busy)}
            onClick={props.onClose}
          >
            取消
          </button>
          <button
            className="btn primary"
            form={id}
            disabled={Boolean(props.busy)}
            type="submit"
          >
            <Icon name={props.busy ? "spinner" : "check"} />
            {props.busy ? "正在保存…" : entry ? "保存配置" : "添加并配置"}
          </button>
        </>
      }
    >
      <form id={id} onSubmit={submit}>
        <fieldset disabled={Boolean(props.busy)}>
          <div className="field-row">
            <div className="field">
              <label htmlFor={id + "name"}>服务名称</label>
              <input
                id={id + "name"}
                value={server.name}
                required
                maxLength={64}
                pattern="[a-zA-Z0-9][a-zA-Z0-9_-]*"
                title="使用字母、数字、短横线或下划线，以字母或数字开头"
                placeholder="例如 context7"
                onChange={(e) => patch({ name: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor={id + "transport"}>连接方式</label>
              <select
                id={id + "transport"}
                value={server.transport}
                onChange={(e) => {
                  const transport = e.target.value as Transport;
                  patch({ transport });
                  setTargets((prev) =>
                    prev.filter((id) =>
                      props.snapshot.agents.some(
                        (a) => a.id === id && compatible(a, "mcp", transport),
                      ),
                    ),
                  );
                }}
              >
                <option value="stdio">stdio · 本地命令</option>
                <option value="http">HTTP · 远程地址</option>
                <option value="sse">SSE · 远程地址</option>
              </select>
            </div>
          </div>
          {server.transport === "stdio" ? (
            <>
              <div className="field">
                <label htmlFor={id + "command"}>启动命令</label>
                <input
                  id={id + "command"}
                  required
                  value={server.command}
                  placeholder="npx / uvx / docker / 可执行文件路径"
                  onChange={(e) => patch({ command: e.target.value })}
                />
              </div>
              <div className="field">
                <div className="field-label">参数</div>
                {server.args.map((arg, index) => (
                  <div className="argument-row" key={index}>
                    <span className="argument-index">{index + 1}</span>
                    <input
                      aria-label={"参数 " + (index + 1)}
                      value={arg}
                      onChange={(e) =>
                        patch({
                          args: server.args.map((x, i) =>
                            i === index ? e.target.value : x,
                          ),
                        })
                      }
                    />
                    <button
                      type="button"
                      className="icon-button"
                      onClick={() =>
                        patch({
                          args: server.args.filter((_, i) => i !== index),
                        })
                      }
                      aria-label={"移除参数 " + (index + 1)}
                    >
                      <Icon name="close" />
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  className="btn small"
                  onClick={() => patch({ args: [...server.args, ""] })}
                >
                  <Icon name="plus" />
                  添加参数
                </button>
                <small>每项一个参数。路径包含空格时无需添加引号。</small>
              </div>
              <MapFields label="环境变量" rows={env} onChange={setEnv} />
            </>
          ) : (
            <>
              <div className="field">
                <label htmlFor={id + "url"}>服务地址</label>
                <input
                  id={id + "url"}
                  type="url"
                  required
                  value={server.url}
                  placeholder="https://example.com/mcp"
                  onChange={(e) => patch({ url: e.target.value })}
                />
              </div>
              <MapFields label="请求头" rows={headers} onChange={setHeaders} />
            </>
          )}
          <div className="modal-divider" />
          <Targets
            snapshot={props.snapshot}
            kind="mcp"
            transport={server.transport}
            selected={targets}
            onChange={setTargets}
          />
          {server.transport === "sse" ? (
            <Note>Codex 和 Pi 不支持 SSE，已排除这两个目标。</Note>
          ) : null}
          {error ? (
            <div className="form-error" role="alert">
              {error}
            </div>
          ) : null}
        </fieldset>
      </form>
    </Modal>
  );
}
function SkillSourceForm({
  source = "",
  preselect,
  ...props
}: Common & { source?: string; preselect?: string }) {
  const [input, setInput] = useState(source),
    [scan, setScan] = useState<ScanResult | null>(null),
    [paths, setPaths] = useState<string[]>([]),
    [targets, setTargets] = useState(() =>
      defaultTargets(props.snapshot, "skills"),
    ),
    [scanning, setScanning] = useState(false),
    [error, setError] = useState("");
  const alive = useRef(true),
    token = useRef(""),
    id = useId();
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      if (token.current)
        void api
          .request("release_scan", { token: token.current })
          .catch(() => {});
    };
  }, []);
  const start = async (event?: FormEvent) => {
    event?.preventDefault();
    if (!input.trim()) return;
    setScanning(true);
    setError("");
    try {
      if (token.current) {
        void api
          .request("release_scan", { token: token.current })
          .catch(() => {});
        token.current = "";
      }
      const value = await api.request<ScanResult>("scan_skills", {
        source: input.trim(),
      });
      if (!alive.current) {
        void api
          .request("release_scan", { token: value.token })
          .catch(() => {});
        return;
      }
      token.current = value.token;
      setScan(value);
      setPaths(
        value.skills
          .filter(
            (x) =>
              !x.installed &&
              (!preselect ||
                x.name === preselect ||
                x.path.split("/").at(-1) === preselect),
          )
          .map((x) => x.path),
      );
      if (!value.skills.length)
        setError("来源中未找到 SKILL.md。请确认目录或仓库地址。");
    } catch (e) {
      if (alive.current) setError(errorMessage(e));
    } finally {
      if (alive.current) setScanning(false);
    }
  };
  const install = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    if (!scan || !paths.length) {
      setError("请至少选择一个尚未安装的 Skill。");
      return;
    }
    try {
      props.onDone(
        await props.mutate(
          "install_skills",
          { token: scan.token, paths, agents: targets },
          "安装所选 Skills",
        ),
      );
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  const pick = async () => {
    try {
      const path = await api.pickDirectory();
      if (path) setInput(path);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Modal
      title={scan ? "选择要安装的 Skills" : "添加 Skills"}
      subtitle={
        scan
          ? "确认来源中的具体技能，再选择目标 Agent。"
          : "从 GitHub 仓库、链接或本地目录添加。"
      }
      onClose={props.onClose}
      busy={Boolean(props.busy)}
      large={Boolean(scan)}
      footer={
        <>
          <button
            className="btn"
            disabled={Boolean(props.busy)}
            onClick={props.onClose}
          >
            {scanning ? "取消读取" : "取消"}
          </button>
          {scan ? (
            <button
              className="btn primary"
              form={id}
              type="submit"
              disabled={Boolean(props.busy) || !paths.length}
            >
              <Icon name={props.busy ? "spinner" : "download"} />
              {props.busy ? "正在安装…" : "安装 " + paths.length + " 个 Skill"}
            </button>
          ) : (
            <button
              className="btn primary"
              form={id}
              type="submit"
              disabled={scanning || !input.trim()}
            >
              <Icon name={scanning ? "spinner" : "search"} />
              {scanning ? "正在查找…" : "查找 Skills"}
            </button>
          )}
        </>
      }
    >
      <form id={id} onSubmit={scan ? install : start}>
        <fieldset disabled={Boolean(props.busy)}>
          {scan ? (
            <>
              <div className="scan-source">
                <Icon name="branch" />
                <span title={scan.source}>{scan.source}</span>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => {
                    setScan(null);
                    setError("");
                  }}
                >
                  更改来源
                </button>
              </div>
              <div className="scan-list">
                {scan.skills.map((skill) => (
                  <label
                    className={
                      "checkbox-row skill-choice" +
                      (skill.installed ? " unavailable" : "")
                    }
                    key={skill.path}
                  >
                    <input
                      type="checkbox"
                      disabled={skill.installed || Boolean(props.busy)}
                      checked={paths.includes(skill.path)}
                      onChange={(e) =>
                        setPaths((prev) =>
                          e.target.checked
                            ? [...prev, skill.path]
                            : prev.filter((p) => p !== skill.path),
                        )
                      }
                    />
                    <div>
                      <strong>{skill.name}</strong>
                      <p>{skill.description || skill.path || "来源根目录"}</p>
                    </div>
                    {skill.installed ? (
                      <span className="pill">已安装</span>
                    ) : null}
                  </label>
                ))}
              </div>
              <div className="modal-divider" />
              <Targets
                snapshot={props.snapshot}
                kind="skills"
                selected={targets}
                onChange={setTargets}
              />
            </>
          ) : (
            <>
              <div className="field">
                <label htmlFor={id + "source"}>来源</label>
                <div className="source-input">
                  <input
                    id={id + "source"}
                    required
                    value={input}
                    disabled={scanning}
                    onChange={(e) => setInput(e.target.value)}
                    placeholder="owner/repo、GitHub 链接或本地目录"
                  />
                  <button
                    type="button"
                    className="btn"
                    disabled={scanning}
                    onClick={() => void pick()}
                    aria-label="选择本地 Skills 文件夹"
                  >
                    <Icon name="folder" />
                  </button>
                </div>
                <small>支持仓库内的子目录和指向 SKILL.md 的链接。</small>
              </div>
              <Note>
                一个仓库可能包含多个 Skill。查找完成后可以选择需要安装的项目。
              </Note>
              {scanning ? (
                <div className="inline-loading">
                  <Icon name="spinner" />
                  正在读取来源，首次下载可能需要一些时间…
                </div>
              ) : null}
            </>
          )}
          {error ? (
            <div className="form-error" role="alert">
              {error}
            </div>
          ) : null}
        </fieldset>
      </form>
    </Modal>
  );
}
function MarketServerForm({
  entry,
  ...props
}: Common & { entry: MarketServer }) {
  const [values, setValues] = useState<Record<string, string>>({}),
    [targets, setTargets] = useState(() =>
      defaultTargets(props.snapshot, "mcp", entry.server.transport),
    ),
    [name, setName] = useState(entry.server.name),
    [error, setError] = useState(""),
    [visible, setVisible] = useState<Set<string>>(new Set());
  const id = useId();
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError("");
    try {
      props.onDone(
        await props.mutate(
          "install_server",
          { id: entry.id, values, agents: targets, name: name.trim() },
          "安装 " + entry.title,
        ),
      );
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Modal
      title={"安装 " + entry.title}
      subtitle="确认来源、连接配置和目标 Agent。"
      busy={Boolean(props.busy)}
      onClose={props.onClose}
      footer={
        <>
          <button
            className="btn"
            disabled={Boolean(props.busy)}
            onClick={props.onClose}
          >
            取消
          </button>
          <button
            className="btn primary"
            type="submit"
            form={id}
            disabled={Boolean(props.busy)}
          >
            <Icon name={props.busy ? "spinner" : "download"} />
            {props.busy ? "正在安装…" : "安装并配置"}
          </button>
        </>
      }
    >
      <form id={id} onSubmit={submit}>
        <fieldset disabled={Boolean(props.busy)}>
          <div className="modal-tool">
            <ToolIcon name={entry.server.name} />
            <div>
              <h3>{entry.title}</h3>
              <p>
                {entry.publisher}
                {entry.homepage ? " · " + entry.homepage : ""}
              </p>
            </div>
          </div>
          <div className="field">
            <label htmlFor={id + "name"}>服务名称</label>
            <input
              id={id + "name"}
              value={name}
              required
              maxLength={64}
              pattern="[a-zA-Z0-9][a-zA-Z0-9_-]*"
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          {entry.inputs.map((input, index) => (
            <div
              className="field"
              key={input.place + ":" + input.key + ":" + index}
            >
              <label htmlFor={id + "input" + index}>
                {input.label}
                {input.required ? (
                  <span className="required">必填</span>
                ) : (
                  <span className="quiet">可选</span>
                )}
              </label>
              <div className="secret-input">
                <input
                  id={id + "input" + index}
                  required={input.required}
                  type={
                    input.secret && !visible.has(input.key)
                      ? "password"
                      : "text"
                  }
                  autoComplete="off"
                  value={values[input.key] || ""}
                  placeholder={input.hint}
                  onChange={(e) =>
                    setValues((prev) => ({
                      ...prev,
                      [input.key]: e.target.value,
                    }))
                  }
                />
                {input.secret ? (
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={
                      visible.has(input.key) ? "隐藏凭据" : "显示凭据"
                    }
                    onClick={() =>
                      setVisible((prev) => {
                        const next = new Set(prev);
                        next.has(input.key)
                          ? next.delete(input.key)
                          : next.add(input.key);
                        return next;
                      })
                    }
                  >
                    <Icon name={visible.has(input.key) ? "eyeOff" : "eye"} />
                  </button>
                ) : null}
              </div>
              {input.hint ? <small>{input.hint}</small> : null}
            </div>
          ))}
          <div className="modal-divider" />
          <Targets
            snapshot={props.snapshot}
            kind="mcp"
            transport={entry.server.transport}
            selected={targets}
            onChange={setTargets}
          />
          <div style={{ marginTop: 16 }}>
            <Note>
              只修改 siu 管理的条目。首次修改 Agent 配置前会保留备份。
            </Note>
          </div>
          {error ? (
            <div className="form-error" role="alert">
              {error}
            </div>
          ) : null}
        </fieldset>
      </form>
    </Modal>
  );
}
function Confirm({
  title,
  subtitle,
  action,
  children,
  run,
  ...props
}: Common & {
  title: string;
  subtitle: string;
  action: string;
  children: ReactNode;
  run: () => Promise<MutationResult>;
}) {
  const [error, setError] = useState("");
  const submit = async () => {
    setError("");
    try {
      props.onDone(await run(), title);
    } catch (e) {
      setError(errorMessage(e));
    }
  };
  return (
    <Modal
      title={title}
      subtitle={subtitle}
      busy={Boolean(props.busy)}
      onClose={props.onClose}
      footer={
        <>
          <button
            className="btn"
            disabled={Boolean(props.busy)}
            onClick={props.onClose}
          >
            取消
          </button>
          <button
            className={
              "btn " +
              (action === "删除资源" || action === "确认清空"
                ? "danger"
                : "primary")
            }
            disabled={Boolean(props.busy)}
            onClick={() => void submit()}
          >
            {props.busy ? <Icon name="spinner" /> : null}
            {props.busy ? "正在处理…" : action}
          </button>
        </>
      }
    >
      {children}
      {error ? (
        <div className="form-error" role="alert">
          {error}
        </div>
      ) : null}
    </Modal>
  );
}
export function Dialogs({
  dialog,
  ...props
}: Common & { dialog: DialogState }) {
  switch (dialog.type) {
    case "server":
      return <ServerForm {...props} entry={dialog.entry} />;
    case "skill-source":
      return (
        <SkillSourceForm
          {...props}
          source={dialog.source}
          preselect={dialog.preselect}
        />
      );
    case "market-server":
      return <MarketServerForm {...props} entry={dialog.entry} />;
    case "import": {
      const found =
          dialog.kind === "mcp"
            ? props.snapshot.foundMcp.find((x) => x.server.name === dialog.name)
            : props.snapshot.foundSkills.find((x) => x.name === dialog.name),
        ids = found
          ? "server" in found
            ? found.agents
            : found.at.map((x) => x.agent)
          : [];
      return (
        <Confirm
          {...props}
          title={"导入 " + dialog.name}
          subtitle="把已有配置纳入 siu 管理，以后可以统一分配和维护。"
          action="导入管理"
          run={() =>
            props.mutate(
              "import",
              { kind: dialog.kind, name: dialog.name },
              "导入 " + dialog.name,
            )
          }
        >
          <div className="modal-tool">
            <ToolIcon name={dialog.name} kind={dialog.kind} />
            <div>
              <h3>{dialog.name}</h3>
              <p>
                发现于{" "}
                {ids
                  .map(
                    (id) =>
                      props.snapshot.agents.find((a) => a.id === id)?.name ||
                      id,
                  )
                  .join("、")}
              </p>
            </div>
          </div>
          <Note>
            {dialog.kind === "mcp"
              ? "导入后保留当前配置和已有分配。以后在 siu 中移除或暂停时，会修改对应条目。"
              : "原目录将备份，然后使用指向 siu 资源库的链接替换。以后可在 siu 中统一暂停、更新和移除。"}
          </Note>
        </Confirm>
      );
    }
    case "delete":
      return (
        <Confirm
          {...props}
          title={"删除 " + displayName(dialog.entry.name, dialog.kind) + "？"}
          subtitle="从资源库移除，并撤回 siu 向 Agent 写入的内容。"
          action="删除资源"
          run={() =>
            props.mutate(
              "delete",
              { kind: dialog.kind, name: dialog.entry.name },
              "删除 " + dialog.entry.name,
            )
          }
        >
          <div className="section-title">影响范围</div>
          <div className="impact-row">
            {dialog.entry.agents.length ? (
              dialog.entry.agents.map((id) => (
                <span className="pill" key={id}>
                  {agentLabel(
                    props.snapshot.agents.find((a) => a.id === id) ||
                      ({ id, name: id } as Snapshot["agents"][number]),
                  )}
                </span>
              ))
            ) : (
              <span className="subtitle">未分配给任何 Agent</span>
            )}
          </div>
          <Note>
            如果只是暂时不用，可以选择「暂停」来保留配置与 Agent 分配。
          </Note>
        </Confirm>
      );
    case "clear": {
      const entries =
        dialog.kind === "mcp" ? props.snapshot.mcp : props.snapshot.skills;
      return (
        <Confirm
          {...props}
          title={
            "清空" +
            (dialog.kind === "mcp" ? " MCP 资源库" : " Skills 资源库") +
            "？"
          }
          subtitle={"将删除 " + entries.length + " 项资源，并撤回分配。"}
          action="确认清空"
          run={() => props.mutate("clear", { kind: dialog.kind }, "清空资源库")}
        >
          <Note warning>
            清空后需要重新添加资源。已有配置备份会保留；Agent 中未纳入 siu
            管理的内容会保留。
          </Note>
          <div className="impact-row" style={{ marginTop: 16 }}>
            {entries.map((entry) => (
              <span className="pill" key={entry.name}>
                {displayName(entry.name, dialog.kind)}
              </span>
            ))}
          </div>
        </Confirm>
      );
    }
    case "update": {
      const entries: Resource[] =
        dialog.kind === "mcp" ? props.snapshot.mcp : props.snapshot.skills;
      return (
        <Confirm
          {...props}
          title={
            "更新" +
            (dialog.name
              ? " " + displayName(dialog.name, dialog.kind)
              : dialog.kind === "mcp"
                ? " MCP 服务"
                : "全部 Skills")
          }
          subtitle={
            dialog.kind === "mcp"
              ? "更新包的固定版本，并重新拉取 Docker 镜像。"
              : "从各自的 GitHub 仓库或本地目录更新文件。"
          }
          action="开始更新"
          run={() =>
            props.mutate(
              "update",
              {
                kind: dialog.kind,
                ...(dialog.name ? { name: dialog.name } : {}),
              },
              "更新" +
                (dialog.name
                  ? " " + dialog.name
                  : dialog.kind === "mcp"
                    ? " MCP 服务"
                    : "全部 Skills"),
            )
          }
        >
          <Note>
            {dialog.kind === "mcp"
              ? "远程服务、@latest 等标签和版本范围保持原有配置。更新结果逐项显示。"
              : "更新后的文件将提供给已分配的 Agent。没有来源的导入项会跳过，并在结果中说明。"}
          </Note>
          <div className="section-title" style={{ marginTop: 20 }}>
            影响范围
          </div>
          <div className="impact-row">
            {entries
              .filter((x) => !dialog.name || x.name === dialog.name)
              .map((x) => (
                <span className="pill" key={x.name}>
                  {displayName(x.name, dialog.kind)}
                </span>
              ))}
          </div>
        </Confirm>
      );
    }
    case "result":
      return (
        <Modal
          title={
            dialog.result.report.issues.length
              ? "操作完成，有项目需要处理"
              : "操作结果"
          }
          subtitle={dialog.title}
          onClose={props.onClose}
          large
          footer={
            <button className="btn primary" onClick={props.onClose}>
              关闭
            </button>
          }
        >
          {dialog.result.report.issues.length ? (
            <Note warning>
              {dialog.result.report.issues.map((issue, i) => (
                <p key={i}>{issue}</p>
              ))}
            </Note>
          ) : (
            <div className="result-success">
              <Icon name="check" />
              {dialog.result.message || "操作已完成"}
            </div>
          )}
          {dialog.result.log ? (
            <pre className="operation-log">{dialog.result.log}</pre>
          ) : null}
        </Modal>
      );
    case "shortcuts":
      return (
        <Modal
          title="快捷键"
          subtitle="常用操作，尽量不打断工作。"
          onClose={props.onClose}
          footer={
            <button className="btn primary" onClick={props.onClose}>
              知道了
            </button>
          }
        >
          <div className="shortcuts">
            {[
              ["搜索当前页面", "/"],
              ["MCP 服务", "1"],
              ["Skills", "2"],
              ["发现", "3"],
              ["添加资源", "A"],
              ["刷新资源库", "⌘ / Ctrl + R"],
              ["关闭弹窗", "Esc"],
              ["查看快捷键", "?"],
            ].map(([label, key]) => (
              <div className="shortcut-row" key={key}>
                <span>{label}</span>
                <kbd>{key}</kbd>
              </div>
            ))}
          </div>
        </Modal>
      );
  }
}
