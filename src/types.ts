export type Kind = "mcp" | "skills";
export type Page = Kind | "discover" | "agents" | "settings";
export type Theme = "system" | "light" | "dark";
export type Transport = "stdio" | "http" | "sse";
export interface ServerConfig {
  name: string;
  transport: Transport;
  command: string;
  args: string[];
  env: Record<string, string>;
  url: string;
  headers: Record<string, string>;
}
export interface Agent {
  id: string;
  name: string;
  home: string;
  mcpPath: string | null;
  skillsPath: string | null;
  detected: boolean;
  transports: Transport[];
}
export interface McpEntry extends ServerConfig {
  agents: string[];
  enabled: boolean;
  origin?: string;
  states: Record<string, string>;
}
export type SkillSource =
  | { kind: "github"; repo: string; ref: string; path: string }
  | { kind: "local"; dir: string };
export interface SkillEntry {
  name: string;
  description: string;
  source: SkillSource | null;
  agents: string[];
  enabled: boolean;
  hash: string;
  installed: number;
  updated: number;
  states: Record<string, string>;
}
export type Resource = McpEntry | SkillEntry;
export interface FoundMcp {
  server: ServerConfig;
  agents: string[];
}
export interface FoundSkill {
  name: string;
  description: string;
  at: { agent: string; path: string }[];
}
export interface Snapshot {
  autoConfig?: { digest: string; theme?: Theme | null };
  home: string;
  root: string;
  platform: string;
  version: string;
  agents: Agent[];
  mcp: McpEntry[];
  skills: SkillEntry[];
  foundMcp: FoundMcp[];
  foundSkills: FoundSkill[];
  issues: string[];
}
export interface MutationResult {
  snapshot: Snapshot;
  report: { changed: number; issues: string[] };
  message: string;
  log?: string;
  theme?: Theme | null;
}
export interface ConfigExport {
  path: string;
  mcpCount: number;
  skillCount: number;
}
export interface ConfigPreview extends ConfigExport {
  digest: string;
  exportedAt: number;
  issues: string[];
  theme?: Theme | null;
}
export interface MarketInput {
  key: string;
  place: "env" | "header" | "arg";
  label: string;
  hint: string;
  secret: boolean;
  required: boolean;
  format: string;
}
export interface MarketServer {
  id: string;
  title: string;
  description: string;
  publisher: string;
  homepage: string;
  server: ServerConfig;
  inputs: MarketInput[];
  featured: boolean;
}
export interface MarketSkill {
  source: string;
  skillId: string;
  name: string;
  installs: number;
  isOfficial: boolean;
}
export interface Discovery {
  servers: MarketServer[];
  skills: MarketSkill[];
  warnings: string[];
  cached: boolean;
}
export interface ScanResult {
  token: string;
  source: string;
  skills: {
    name: string;
    description: string;
    path: string;
    installed: boolean;
  }[];
}
export interface AppInfo {
  os: string;
  arch: string;
  version: string;
}
export type ProxyMode = "system" | "custom" | "direct";
export interface ProxySettings {
  mode: ProxyMode;
  url: string;
  noProxy: string;
}
export type Mutate = (
  method: string,
  params: Record<string, unknown>,
  label: string,
) => Promise<MutationResult>;
export type DialogState =
  | { type: "server"; entry?: McpEntry }
  | { type: "skill-source"; source?: string; preselect?: string }
  | { type: "market-server"; entry: MarketServer }
  | { type: "import"; kind: Kind; name: string }
  | { type: "delete"; kind: Kind; entry: Resource }
  | { type: "clear"; kind: Kind }
  | { type: "update"; kind: Kind; name?: string }
  | { type: "shortcuts" }
  | { type: "result"; title: string; result: MutationResult };
