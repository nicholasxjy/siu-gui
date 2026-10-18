import {
  Plug2,
  Sparkles,
  Compass,
  Settings2,
  Search,
  Plus,
  RefreshCw,
  Check,
  ChevronRight,
  ChevronDown,
  LockKeyhole,
  Code2,
  Folder,
  Globe,
  BrainCircuit,
  Pause,
  Play,
  Trash2,
  Pencil,
  Info,
  X,
  ExternalLink,
  Sun,
  Moon,
  ArrowRight,
  Download,
  BookOpen,
  Monitor,
  Terminal,
  GitBranch,
  TriangleAlert,
  LoaderCircle,
  Eye,
  EyeOff,
  Copy,
  RotateCcw,
  FileCode2,
} from "lucide-react";
import type { Agent, Kind } from "./types";
const icons = {
  plug: Plug2,
  skill: Sparkles,
  compass: Compass,
  settings: Settings2,
  search: Search,
  plus: Plus,
  refresh: RefreshCw,
  check: Check,
  chevron: ChevronRight,
  down: ChevronDown,
  lock: LockKeyhole,
  code: Code2,
  folder: Folder,
  globe: Globe,
  brain: BrainCircuit,
  pause: Pause,
  play: Play,
  trash: Trash2,
  edit: Pencil,
  info: Info,
  close: X,
  external: ExternalLink,
  sun: Sun,
  moon: Moon,
  arrow: ArrowRight,
  download: Download,
  book: BookOpen,
  monitor: Monitor,
  terminal: Terminal,
  branch: GitBranch,
  warning: TriangleAlert,
  spinner: LoaderCircle,
  eye: Eye,
  eyeOff: EyeOff,
  copy: Copy,
  undo: RotateCcw,
  file: FileCode2,
};
export type IconName = keyof typeof icons;
export function Icon({
  name,
  className = "",
}: {
  name: IconName;
  className?: string;
}) {
  const Component = icons[name];
  return (
    <Component
      aria-hidden="true"
      size={18}
      strokeWidth={1.65}
      className={className + (name === "spinner" ? " spin" : "")}
    />
  );
}
export function Brand() {
  return (
    <span className="brand-symbol" aria-hidden="true">
      <svg viewBox="0 0 24 24">
        <path d="M5 7h8a3 3 0 0 1 0 6H9a3 3 0 0 0 0 6h10M5 3v4m14 12v3" />
      </svg>
    </span>
  );
}
const letters: Record<string, string> = {
  claude: "✳",
  codex: "⌘",
  gemini: "✦",
  cursor: "◩",
  pi: "π",
  opencode: "o",
  copilot: "◉",
  droid: "d",
  grok: "g",
  antigravity: "A",
  "claude-desktop": "✳",
  agents: "↗",
};
export function Avatar({ agent }: { agent: Pick<Agent, "id" | "name"> }) {
  return (
    <span className={"avatar " + agent.id} aria-hidden="true">
      {letters[agent.id] || agent.name.slice(0, 1)}
    </span>
  );
}
export function ToolIcon({
  name,
  kind = "mcp",
  large = false,
}: {
  name: string;
  kind?: Kind;
  large?: boolean;
}) {
  const lower = name.toLowerCase();
  let style = "folder",
    glyph: IconName = "plug";
  if (kind === "skills") {
    style = "skill";
    glyph = "skill";
  } else if (lower.includes("context7")) {
    style = "context";
  } else if (lower.includes("github")) {
    style = "github";
    glyph = "branch";
  } else if (lower.includes("playwright")) {
    style = "playwright";
    glyph = "globe";
  } else if (lower.includes("filesystem")) glyph = "folder";
  else if (lower.includes("thinking") || lower.includes("memory")) {
    style = "thinking";
    glyph = "brain";
  } else if (lower.includes("figma")) {
    style = "figma";
    glyph = "code";
  } else if (lower.includes("search")) glyph = "compass";
  else if (lower.includes("fetch")) glyph = "external";
  return (
    <span
      className={"tool-icon " + style + (large ? " large" : "")}
      aria-hidden="true"
    >
      {style === "context" ? "C7" : <Icon name={glyph} />}
    </span>
  );
}
