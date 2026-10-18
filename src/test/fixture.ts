import type { MutationResult, Snapshot } from "../types";
export function fixture(): Snapshot {
  return {
    home: "/sandbox",
    root: "/sandbox/.siu",
    platform: "darwin",
    version: "0.0.5",
    agents: [
      {
        id: "claude",
        name: "Claude Code",
        home: "/sandbox/.claude",
        mcpPath: "/sandbox/.claude.json",
        skillsPath: "/sandbox/.claude/skills",
        detected: true,
        transports: ["stdio", "http", "sse"],
      },
      {
        id: "codex",
        name: "Codex",
        home: "/sandbox/.codex",
        mcpPath: "/sandbox/.codex/config.toml",
        skillsPath: "/sandbox/.codex/skills",
        detected: true,
        transports: ["stdio", "http"],
      },
      {
        id: "cursor",
        name: "Cursor",
        home: "/sandbox/.cursor",
        mcpPath: "/sandbox/.cursor/mcp.json",
        skillsPath: "/sandbox/.cursor/skills",
        detected: true,
        transports: ["stdio", "http", "sse"],
      },
      {
        id: "agents",
        name: "~/.agents (shared)",
        home: "/sandbox/.agents",
        mcpPath: null,
        skillsPath: "/sandbox/.agents/skills",
        detected: false,
        transports: [],
      },
    ],
    mcp: [
      {
        name: "context7",
        transport: "stdio",
        command: "npx",
        args: ["-y", "/sandbox/My Tools/server.js"],
        env: { API_KEY: "fixture-private-value" },
        headers: {},
        url: "",
        agents: ["claude", "codex"],
        enabled: true,
        origin: "featured",
        states: { claude: "on", codex: "on", cursor: "off" },
      },
    ],
    skills: [
      {
        name: "frontend-design",
        description: "界面设计工作流程",
        source: {
          kind: "github",
          repo: "anthropics/skills",
          ref: "HEAD",
          path: "skills/frontend-design",
        },
        agents: ["claude"],
        enabled: true,
        hash: "hash",
        installed: 1,
        updated: 1,
        states: { claude: "on", codex: "off", cursor: "off" },
      },
    ],
    foundMcp: [],
    foundSkills: [],
    issues: [],
  };
}
export const result = (snapshot = fixture()): MutationResult => ({
  snapshot,
  report: { changed: 1, issues: [] },
  message: "已保存",
});
