import type {
  Agent,
  Kind,
  McpEntry,
  Resource,
  ServerConfig,
  SkillEntry,
} from "./types";
const labels: Record<string, string> = {
  context7: "Context7",
  github: "GitHub",
  playwright: "Playwright",
  filesystem: "Filesystem",
  "sequential-thinking": "Sequential Thinking",
  figma: "Figma",
  fetch: "Fetch",
  "brave-search": "Brave Search",
  memory: "Memory",
};
const descriptions: Record<string, string> = {
  context7: "为代码提供最新文档与示例",
  github: "仓库、Issues 与 Pull requests",
  playwright: "让 Agent 操作和测试浏览器",
  filesystem: "在指定目录中读取和管理文件",
  "sequential-thinking": "把复杂问题拆成可追踪的步骤",
  figma: "读取设计上下文与组件信息",
  fetch: "获取网页内容并转换为可读文本",
  "brave-search": "网页搜索与本地搜索",
  memory: "持久保存知识与上下文",
};
const lookup = (values: Record<string, string>, key: string) =>
  Object.hasOwn(values, key) ? values[key] : undefined;
export const displayName = (name: string, kind: Kind = "mcp") =>
  kind === "mcp" ? lookup(labels, name.toLowerCase()) || name : name;
export const isMcp = (entry: Resource): entry is McpEntry =>
  "transport" in entry;
export const description = (entry: Resource) =>
  isMcp(entry)
    ? lookup(descriptions, entry.name.toLowerCase()) ||
      (entry.transport === "stdio"
        ? entry.command + " · " + entry.args.length + " 个参数"
        : String(redactServer(entry).url || "远程服务"))
    : entry.description;
export const groupName = (entry: SkillEntry) =>
  !entry.source
    ? "从 Agent 导入"
    : entry.source.kind === "github"
      ? entry.source.repo
      : entry.source.dir;
export const sourceLabel = (entry: Resource) =>
  isMcp(entry)
    ? entry.origin === "import"
      ? "从 Agent 导入"
      : entry.origin || "手动添加"
    : groupName(entry);
export const shortPath = (path: string, home: string) =>
  path === home
    ? "~"
    : path.startsWith(home + "/") ||
        path.startsWith(home + String.fromCharCode(92))
      ? "~" + path.slice(home.length)
      : path;
export const agentLabel = (agent: Agent) =>
  agent.id === "agents" ? "共享目录" : agent.name;
export const stateLabel = (state: string) =>
  state === "on"
    ? "已配置"
    : state === "paused"
      ? "已暂停"
      : state === "error"
        ? "写入未完成"
        : state.startsWith("unsupported:")
          ? state.slice(12)
          : "未配置";
export const emptyServer = (): ServerConfig => ({
  name: "",
  transport: "stdio",
  command: "",
  args: [],
  env: {},
  url: "",
  headers: {},
});
export function redactServer(server: ServerConfig): Record<string, unknown> {
  const args = [...server.args];
  for (let i = 0; i < args.length; i++) {
    if (/(?:token|secret|password|api[-_]?key)/i.test(args[i])) {
      const eq = args[i].indexOf("=");
      if (eq >= 0) args[i] = args[i].slice(0, eq + 1) + "••••••••";
      else if (i + 1 < args.length) args[++i] = "••••••••";
    }
  }
  let url = server.url;
  try {
    const u = new URL(url);
    if (u.username) u.username = "hidden";
    if (u.password) u.password = "hidden";
    for (const key of u.searchParams.keys())
      if (/token|secret|password|key/i.test(key))
        u.searchParams.set(key, "hidden");
    url = u.toString();
  } catch {
    /* A local command has no URL. */
  }
  return server.transport === "stdio"
    ? {
        command: server.command,
        args,
        ...(Object.keys(server.env).length
          ? {
              env: Object.fromEntries(
                Object.keys(server.env).map((k) => [k, "••••••••"]),
              ),
            }
          : {}),
      }
    : {
        type: server.transport,
        url,
        ...(Object.keys(server.headers).length
          ? {
              headers: Object.fromEntries(
                Object.keys(server.headers).map((k) => [k, "••••••••"]),
              ),
            }
          : {}),
      };
}
export const relativeTime = (stamp: number) => {
  if (!stamp) return "尚未更新";
  const days = Math.floor((Date.now() / 1000 - stamp) / 86400);
  return days <= 0 ? "今天" : days === 1 ? "昨天" : days + " 天前";
};
export const compatible = (agent: Agent, kind: Kind, transport = "stdio") =>
  kind === "skills"
    ? Boolean(agent.skillsPath)
    : Boolean(agent.mcpPath) &&
      agent.transports.includes(transport as McpEntry["transport"]);
