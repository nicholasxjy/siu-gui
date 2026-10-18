// Runs the real Go/Rust service against a disposable home, then drives the UI.
// Prerequisites: bun run prepare:core; agent-browser and its Chromium installed.
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
  mkdtemp,
  mkdir,
  readFile,
  writeFile,
  rename,
  rm,
  stat,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import type { ChildProcess } from "node:child_process";
import type { Snapshot } from "../src/types";
const root = resolve(import.meta.dir, "..");
const home = await mkdtemp(join(tmpdir(), "siu-gui-e2e-"));
const artifacts = join(root, ".artifacts", "e2e");
await mkdir(artifacts, { recursive: true });
const session = "siu-gui-e2e-" + process.pid;
const children: ChildProcess[] = [];
const checks: string[] = [];
const core = join(
  root,
  "resources",
  (process.platform === "win32" ? "windows" : process.platform) +
    "-" +
    (process.arch === "x64" ? "amd64" : process.arch),
  "bin",
  process.platform === "win32" ? "siu-core.exe" : "siu-core",
);
const env = {
  ...process.env,
  SIU_GUI_TEST_HOME: home,
  SIU_GUI_CORE: core,
  SIU_GUI_BROWSER_TEST: "1",
};
const browserBin = process.env.AGENT_BROWSER_BIN || "npx";
const browserPrefix = process.env.AGENT_BROWSER_BIN
  ? []
  : ["--yes", "agent-browser"];
async function run(
  args: string[],
  options: { env?: NodeJS.ProcessEnv; input?: string } = {},
) {
  return new Promise<string>((done, fail) => {
    const p = spawn(args[0], args.slice(1), {
      cwd: root,
      env: options.env || env,
      stdio: ["pipe", "pipe", "pipe"],
    });
    let out = "",
      err = "";
    p.stdout!.on("data", (d) => (out += d));
    p.stderr!.on("data", (d) => (err += d));
    p.once("error", fail);
    p.once("exit", (code) =>
      code === 0
        ? done(out)
        : fail(
            new Error(args[0] + " failed (" + code + "): " + err + " " + out),
          ),
    );
    p.stdin!.end(options.input);
  });
}
async function browser(...args: string[]) {
  return run([browserBin, ...browserPrefix, "--session", session, ...args]);
}
async function js(source: string) {
  return browser("eval", "-b", Buffer.from(source).toString("base64"));
}
async function until(
  fn: () => Promise<boolean>,
  label: string,
  timeout = 20000,
) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    if (await fn()) return;
    await Bun.sleep(100);
  }
  throw new Error("Timed out: " + label);
}
async function request<T>(
  method: string,
  params: Record<string, unknown> = {},
): Promise<T> {
  const r = await fetch("http://127.0.0.1:47832/__test/request", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Origin: "http://127.0.0.1:5173",
    },
    body: JSON.stringify({ method, params }),
  });
  if (!r.ok) throw new Error("HTTP " + r.status + ": " + (await r.text()));
  const v = (await r.json()) as { ok: boolean; data: T; error?: string };
  if (!v.ok) throw new Error(v.error);
  return v.data;
}
async function state() {
  return request<Snapshot>("snapshot");
}
async function check(label: string, fn: () => unknown | Promise<unknown>) {
  await fn();
  checks.push(label);
  console.log("PASS " + label);
}
function background(args: string[], name: string) {
  const file = Bun.file(join(artifacts, name + ".log"));
  const stream = spawn(args[0], args.slice(1), {
    cwd: root,
    env,
    detached: process.platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  stream.stdout!.on("data", (d) => {
    output += d;
    void Bun.write(file, output);
  });
  stream.stderr!.on("data", (d) => {
    output += d;
    void Bun.write(file, output);
  });
  children.push(stream);
  return stream;
}
async function click(name: string) {
  await browser("find", "role", "button", "click", "--name", name, "--exact");
}
async function fill(label: string, value: string) {
  await browser("find", "label", label, "fill", value);
}
async function waitText(text: string) {
  await browser("wait", "--text", text);
}
async function pauseOrResume(name: string, enabled: boolean) {
  await click(enabled ? "恢复所有分配" : "暂停所有分配");
  await until(async () => {
    const s = await state();
    const e =
      s.mcp.find((x) => x.name === name) ||
      s.skills.find((x) => x.name === name);
    return e?.enabled === enabled;
  }, "enable state " + name);
}
try {
  await stat(core);
  for (const dir of [".claude", ".codex", ".cursor/skills/unmanaged-skill"])
    await mkdir(join(home, dir), { recursive: true });
  const originalClaude = JSON.stringify(
    {
      theme: "fixture-preference",
      mcpServers: {
        "existing-claude": { command: "foreign-runner", args: [] },
      },
    },
    null,
    2,
  );
  await writeFile(join(home, ".claude.json"), originalClaude);
  await writeFile(
    join(home, ".codex/config.toml"),
    'model = "fixture-model"\n[mcp_servers.foreign_codex]\ncommand = "foreign-runner"\nargs = []\n',
  );
  await writeFile(join(home, ".cursor/mcp.json"), "{}");
  await writeFile(
    join(home, ".cursor/skills/unmanaged-skill/SKILL.md"),
    "---\nname: unmanaged-skill\ndescription: Existing skill\n---\n# Existing\n",
  );
  const source = join(home, ".fixtures", "My Skills", "local-design");
  await mkdir(source, { recursive: true });
  await writeFile(
    join(source, "SKILL.md"),
    "---\nname: local-design\ndescription: Local design workflow\n---\n# Local design\nInitial content.\n",
  );
  const host = join(
    home,
    process.platform === "win32" ? "testhost.exe" : "testhost",
  );
  await run(["go", "build", "-o", host, "./cmd/testhost"]);
  background([host], "backend");
  background(["bun", "run", "dev:web"], "frontend");
  await until(async () => {
    try {
      return (await fetch("http://127.0.0.1:47832/__test/info")).ok;
    } catch {
      return false;
    }
  }, "test backend startup");
  await until(async () => {
    try {
      return (await fetch("http://127.0.0.1:5173")).ok;
    } catch {
      return false;
    }
  }, "Vite startup");
  await check("读取快照不修改已有 Agent 配置", async () => {
    const s = await state();
    assert.equal(s.mcp.length, 0);
    assert.equal(
      s.agents.filter((a) => a.detected && a.id !== "agents").length,
      3,
    );
    assert.equal(
      await readFile(join(home, ".claude.json"), "utf8"),
      originalClaude,
    );
  });
  await check("测试服务拒绝其他网页来源", async () => {
    const r = await fetch("http://127.0.0.1:47832/__test/request", {
      method: "POST",
      headers: {
        Origin: "https://unrelated.example",
        "Content-Type": "application/json",
      },
      body: '{"method":"snapshot"}',
    });
    assert.equal(r.status, 403);
  });
  await browser("set", "viewport", "1440", "900");
  await browser("open", "http://127.0.0.1:5173");
  await waitText("把常用工具放进 siu");
  await check("空资源库可引导添加并发现未托管配置", async () => {
    await waitText("existing-claude");
    await browser("screenshot", join(artifacts, "01-first-run.png"));
  });
  await check("通过真实表单添加服务，参数空格与凭据完整保留", async () => {
    await browser("click", "[data-add]");
    await fill("服务名称", "fixture-mcp");
    await fill("启动命令", "node");
    await click("添加参数");
    await fill("参数 1", "/sandbox/My Tools/server.js");
    await click("添加环境变量");
    await fill("环境变量名称 1", "API_KEY");
    await fill("环境变量值 1", "fixture-secret");
    await click("添加并配置");
    await until(
      async () =>
        Boolean((await state()).mcp.find((x) => x.name === "fixture-mcp")),
      "MCP save",
    );
    const cfg = JSON.parse(await readFile(join(home, ".claude.json"), "utf8"));
    assert.equal(cfg.theme, "fixture-preference");
    assert.equal(
      cfg.mcpServers["fixture-mcp"].args[0],
      "/sandbox/My Tools/server.js",
    );
    assert.equal(cfg.mcpServers["fixture-mcp"].env.API_KEY, "fixture-secret");
    assert.ok(cfg.mcpServers["existing-claude"]);
  });
  await check("同名未托管配置不会被覆盖", async () => {
    await assert.rejects(
      request("save_server", {
        server: {
          name: "existing-claude",
          transport: "stdio",
          command: "overwriter",
          args: [],
          env: {},
          url: "",
          headers: {},
        },
        agents: ["claude"],
      }),
    );
    assert.equal(
      JSON.parse(await readFile(join(home, ".claude.json"), "utf8")).mcpServers[
        "existing-claude"
      ].command,
      "foreign-runner",
    );
  });
  await check("移除单个分配不影响其他 Agent", async () => {
    await click("fixture-mcp · Codex · 已配置");
    await until(
      async () =>
        !(await state()).mcp
          .find((x) => x.name === "fixture-mcp")!
          .agents.includes("codex"),
      "remove Codex",
    );
    assert.ok(
      JSON.parse(await readFile(join(home, ".claude.json"), "utf8")).mcpServers[
        "fixture-mcp"
      ],
    );
    assert.ok(
      !(await readFile(join(home, ".codex/config.toml"), "utf8")).includes(
        "fixture-mcp",
      ),
    );
    assert.ok(
      (await readFile(join(home, ".codex/config.toml"), "utf8")).includes(
        "fixture-model",
      ),
    );
  });
  await check("暂停撤回配置且保留分配，矩阵可恢复全部保留目标", async () => {
    await pauseOrResume("fixture-mcp", false);
    let s = await state();
    assert.deepEqual(s.mcp[0].agents.sort(), ["claude", "cursor"]);
    assert.ok(
      !JSON.parse(await readFile(join(home, ".claude.json"), "utf8"))
        .mcpServers["fixture-mcp"],
    );
    await click("fixture-mcp · Claude Code · 已暂停");
    await until(
      async () => Boolean((await state()).mcp[0].enabled),
      "resume retained assignments",
    );
    s = await state();
    assert.equal(s.mcp[0].states.cursor, "on");
  });
  await check("服务重命名撤回旧键并保留已有无关配置", async () => {
    await click("编辑服务");
    await fill("服务名称", "fixture-renamed");
    await click("保存配置");
    await until(
      async () =>
        Boolean((await state()).mcp.find((x) => x.name === "fixture-renamed")),
      "rename",
    );
    const cfg = JSON.parse(await readFile(join(home, ".claude.json"), "utf8"));
    assert.ok(cfg.mcpServers["fixture-renamed"]);
    assert.ok(!cfg.mcpServers["fixture-mcp"]);
    assert.ok(cfg.mcpServers["existing-claude"]);
  });
  await check("默认配置预览隐藏实际凭据", async () => {
    await browser("find", "role", "tab", "click", "--name", "配置", "--exact");
    const preview = await browser("get", "text", ".config-preview");
    assert.ok(!preview.includes("fixture-secret"));
    assert.ok(preview.includes("API_KEY"));
  });
  await browser("find", "role", "tab", "click", "--name", "概览", "--exact");
  await check("明确导入已有 MCP，原有字段与分配保持一致", async () => {
    await browser("find", "first", ".unmanaged button", "click");
    await browser("click", ".overlay .btn.primary");
    await until(
      async () =>
        Boolean((await state()).mcp.find((x) => x.name === "existing-claude")),
      "import",
    );
    assert.equal(
      JSON.parse(await readFile(join(home, ".claude.json"), "utf8")).mcpServers[
        "existing-claude"
      ].command,
      "foreign-runner",
    );
  });
  for (const [name, command, args, agents] of [
    [
      "context7",
      "npx",
      ["-y", "@upstash/context7-mcp@latest"],
      ["claude", "codex"],
    ],
    [
      "playwright",
      "npx",
      ["-y", "@playwright/mcp@latest"],
      ["codex", "cursor"],
    ],
    [
      "filesystem",
      "npx",
      ["-y", "@modelcontextprotocol/server-filesystem", home],
      ["claude"],
    ],
    [
      "memory",
      "npx",
      ["-y", "@modelcontextprotocol/server-memory"],
      ["claude", "cursor"],
    ],
  ] as [string, string, string[], string[]][]) {
    await request("save_server", {
      server: {
        name,
        transport: "stdio",
        command,
        args,
        env: {},
        url: "",
        headers: {},
      },
      agents,
    });
  }
  await browser("press", "Control+r");
  await waitText("Context7");
  await browser("screenshot", join(artifacts, "02-mcp-light.png"));
  await check("配置写入失败显示实际错误，恢复文件后可重试", async () => {
    await click("查看 fixture-renamed 详情");
    const cfg = join(home, ".claude.json");
    await rename(cfg, cfg + ".saved");
    await mkdir(cfg);
    try {
      await click("fixture-renamed · Claude Code · 已配置");
      await until(async () => {
        const s = await state();
        return (
          s.issues.length > 0 ||
          s.mcp.find((x) => x.name === "fixture-renamed")?.states.claude ===
            "error"
        );
      }, "write issue");
      await browser("wait", ".app-banner");
    } finally {
      await rm(cfg, { recursive: true });
      await rename(cfg + ".saved", cfg);
    }
    await click("重试写入");
    await until(async () => {
      const s = await state();
      return (
        !s.issues.length &&
        s.mcp.find((x) => x.name === "fixture-renamed")?.states.claude !==
          "error"
      );
    }, "retry actual write");
  });
  await browser("press", "2");
  await waitText("添加你的第一个 Skill");
  await check("本地路径扫描、选择安装与实际目录链接", async () => {
    await browser("click", "[data-add]");
    await fill("来源", join(home, ".fixtures", "My Skills"));
    await click("查找 Skills");
    await waitText("选择要安装的 Skills");
    await click("安装 1 个 Skill");
    await until(
      async () =>
        Boolean((await state()).skills.find((x) => x.name === "local-design")),
      "skill install",
    );
    const s = await state(),
      agent = s.agents.find((a) => a.id === "claude")!;
    assert.ok(
      (
        await stat(join(agent.skillsPath!, "local-design", "SKILL.md"))
      ).isFile(),
    );
    assert.equal(
      s.skills.find((x) => x.name === "local-design")?.states.claude,
      "on",
    );
  });
  await check("Skill 内容预览来自已安装的真实文件", async () => {
    await browser(
      "find",
      "role",
      "tab",
      "click",
      "--name",
      "内容预览",
      "--exact",
    );
    await waitText("Initial content.");
  });
  await check("从本地来源更新 Skill，分配立即读取更新后的内容", async () => {
    await writeFile(
      join(source, "SKILL.md"),
      "---\nname: local-design\ndescription: Updated local workflow\n---\n# Local design\nUpdated marker.\n",
    );
    await click("更新 Skill");
    await click("开始更新");
    await until(async () => {
      const s = await request<{ content: string }>("skill_content", {
        name: "local-design",
      });
      return s.content.includes("Updated marker.");
    }, "local source update");
    await waitText("操作结果");
    await click("关闭");
    const s = await state();
    assert.ok(
      (
        await readFile(
          join(
            s.agents.find((a) => a.id === "claude")!.skillsPath!,
            "local-design",
            "SKILL.md",
          ),
          "utf8",
        )
      ).includes("Updated marker."),
    );
  });
  await browser("find", "role", "tab", "click", "--name", "概览", "--exact");
  await browser("screenshot", join(artifacts, "03-skills-light.png"));
  await check("导入现有 Skill 创建受管链接并保留目录备份", async () => {
    await browser("find", "first", ".unmanaged button", "click");
    await browser("click", ".overlay .btn.primary");
    await until(
      async () =>
        Boolean(
          (await state()).skills.find((x) => x.name === "unmanaged-skill"),
        ),
      "skill import",
    );
    const s = await state();
    assert.equal(
      s.skills.find((x) => x.name === "unmanaged-skill")?.states.cursor,
      "on",
    );
    assert.ok((await stat(join(home, ".siu", "backups"))).isDirectory());
  });
  await click("查看 local-design 详情");
  await check("Skill 暂停与恢复不会丢失分配", async () => {
    await pauseOrResume("local-design", false);
    assert.equal(
      (await state()).skills.find((x) => x.name === "local-design")?.agents
        .length,
      3,
    );
    await pauseOrResume("local-design", true);
    assert.equal(
      (await state()).skills.find((x) => x.name === "local-design")?.states
        .claude,
      "on",
    );
  });
  await check("取消删除不会修改实际资源", async () => {
    await click("删除 local-design");
    await click("取消");
    assert.ok((await state()).skills.find((x) => x.name === "local-design"));
  });
  await check("删除 Skill 撤回链接并移出资源库", async () => {
    await click("删除 local-design");
    await click("删除资源");
    await until(
      async () =>
        !(await state()).skills.find((x) => x.name === "local-design"),
      "skill deletion",
    );
    await assert.rejects(stat(join(home, ".claude", "skills", "local-design")));
  });
  await browser("press", "3");
  await waitText("发现");
  await check("发现页加载真实 MCP 安装描述符", async () => {
    await browser("wait", ".market-card");
    await browser("screenshot", join(artifacts, "04-discover-light.png"));
    assert.ok((await browser("get", "count", ".market-card")).trim() !== "0");
  });
  await check("通过发现页表单安装真实 MCP 描述符", async () => {
    const data = await request<{
      servers: {
        id: string;
        title: string;
        server: { name: string };
        inputs: { required: boolean }[];
      }[];
    }>("discover", { kind: "mcp", query: "" });
    const current = await state();
    const candidate = data.servers.find(
      (server) =>
        !current.mcp.some((entry) => entry.name === server.server.name) &&
        server.inputs.every((input) => !input.required),
    );
    assert.ok(candidate);
    await browser(
      "click",
      '.market-card:has(h3[title="' + candidate.title + '"]) .card-bottom .btn',
    );
    await browser("click", ".overlay .modal-footer .primary");
    await until(
      async () =>
        Boolean(
          (await state()).mcp.find(
            (entry) => entry.name === candidate.server.name,
          ),
        ),
      "market installation",
    );
    const installed = (await state()).mcp.find(
      (entry) => entry.name === candidate.server.name,
    )!;
    assert.equal(installed.origin, candidate.id);
    assert.equal(installed.states.claude, "on");
  });
  await click("设置");
  await check("下载代理默认跟随系统并可保存手动配置", async () => {
    await browser("wait", 'select[aria-label="代理模式"]');
    assert.equal(
      (await request<{ mode: string }>("proxy_settings")).mode,
      "system",
    );
    await browser("select", 'select[aria-label="代理模式"]', "custom");
    await fill("代理地址", "http://127.0.0.1:7890");
    await fill("绕过代理", "localhost,127.0.0.1,*.local");
    await click("保存");
    await until(
      async () =>
        (await request<{ mode: string }>("proxy_settings")).mode === "custom",
      "save custom proxy",
    );
    const settings = JSON.parse(
      await readFile(join(home, ".siu/proxy.json"), "utf8"),
    );
    assert.equal(settings.url, "http://127.0.0.1:7890");
    assert.equal(settings.noProxy, "localhost,127.0.0.1,*.local");
    await js(
      'document.querySelector(".proxy-mode-select").closest("section").scrollIntoView({block:"center"})',
    );
    await browser("screenshot", join(artifacts, "07-proxy-settings.png"));
  });
  await check(
    "重新进入设置保留代理地址，直连与系统模式可即时切换",
    async () => {
      await browser("press", "1");
      await click("设置");
      await browser("wait", 'input[id="proxy-url"]');
      const value = await js('document.querySelector("#proxy-url").value');
      assert.ok(value.includes("http://127.0.0.1:7890"), value);
      await browser("select", 'select[aria-label="代理模式"]', "direct");
      await click("保存");
      await until(
        async () =>
          (await request<{ mode: string }>("proxy_settings")).mode === "direct",
        "save direct mode",
      );
      await browser("select", 'select[aria-label="代理模式"]', "system");
      await click("保存");
      await until(
        async () =>
          (await request<{ mode: string }>("proxy_settings")).mode === "system",
        "restore system proxy",
      );
    },
  );
  await click("深色");
  await browser("press", "1");
  await browser("screenshot", join(artifacts, "05-mcp-dark.png"));
  await check("主题切换持久化并应用到页面", async () => {
    const value = await js(
      '({theme:document.documentElement.dataset.theme,saved:localStorage.getItem("siu.theme.v1")})',
    );
    assert.ok(value.includes("dark"));
  });
  await check("最小窗口下键盘导航、添加与关闭对话框仍可用", async () => {
    await browser("set", "viewport", "960", "680");
    await browser("press", "2");
    assert.ok(
      (
        await js(
          'document.querySelector(\'.sidebar button[title="Skills"]\').getAttribute("aria-current")',
        )
      ).includes("page"),
    );
    await browser("press", "1");
    assert.ok(
      (
        await js(
          'document.querySelector(\'.sidebar button[title="MCP 服务"]\').getAttribute("aria-current")',
        )
      ).includes("page"),
    );
    await browser("press", "a");
    await until(
      async () =>
        (await js('!!document.querySelector(".overlay")')).includes("true"),
      "keyboard add dialog",
    );
    const fits = await js(
      '(()=>{const r=document.querySelector(".overlay .modal-footer .primary").getBoundingClientRect();return [r.height>0,r.top>=0,r.bottom<=innerHeight,r.right<=innerWidth,document.documentElement.scrollWidth<=innerWidth]})()',
    );
    assert.ok(!fits.includes("false"), fits);
    await browser("press", "Escape");
    await until(
      async () =>
        (await js('!document.querySelector(".overlay")')).includes("true"),
      "keyboard dismissal",
    );
    await browser("screenshot", join(artifacts, "06-mcp-small.png"));
    await browser("set", "viewport", "1440", "900");
  });
  await check("当前页面无已确认的 axe 无障碍违规", async () => {
    const raw = JSON.parse(await browser("a11y", "--json"));
    await Bun.write(join(artifacts, "a11y.json"), JSON.stringify(raw, null, 2));
    const data = raw.data || raw;
    assert.equal((data.violations || []).length, 0);
  });
  await check("清空确认保留未托管配置和其他资源类型", async () => {
    await click("设置");
    await click("清空MCP 服务");
    await waitText("清空 MCP 资源库？");
    await click("确认清空");
    await until(async () => (await state()).mcp.length === 0, "clear MCP");
    const s = await state();
    assert.ok(
      (await readFile(join(home, ".codex", "config.toml"), "utf8")).includes(
        "foreign_codex",
      ),
    );
    assert.ok(s.skills.find((x) => x.name === "unmanaged-skill"));
  });
  await Bun.write(
    join(artifacts, "result.json"),
    JSON.stringify(
      {
        ok: true,
        home,
        checks,
        platform: process.platform + "-" + process.arch,
      },
      null,
      2,
    ),
  );
  console.log(
    "Passed " +
      checks.length +
      " real end-to-end checks. Artifacts: " +
      artifacts,
  );
} catch (error) {
  console.error(error);
  try {
    await browser("screenshot", join(artifacts, "failure.png"));
    await Bun.write(
      join(artifacts, "failure-tree.txt"),
      await browser("snapshot"),
    );
  } catch {}
  await Bun.write(
    join(artifacts, "result.json"),
    JSON.stringify({ ok: false, home, checks, error: String(error) }, null, 2),
  );
  process.exitCode = 1;
} finally {
  try {
    await browser("close");
  } catch {}
  for (const child of children.reverse()) {
    try {
      if (process.platform === "win32")
        spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"]);
      else process.kill(-child.pid!, "SIGTERM");
    } catch {}
  }
}
