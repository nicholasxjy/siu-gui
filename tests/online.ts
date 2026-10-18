// Opt-in network integration checks. All Agent/library changes stay in a temporary home.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { HOST, REPO_ROOT, resourceBinPath } from "../scripts/core";
import type {
  Discovery,
  MutationResult,
  ScanResult,
  Snapshot,
} from "../src/types";

const home = await mkdtemp(join(tmpdir(), "siu-gui-online-"));
await mkdir(join(home, ".claude"), { recursive: true });
const core = process.env.SIU_GUI_CORE || resourceBinPath(HOST);
const checks: { name: string; ok: boolean; error?: string }[] = [];
function start(offline = false) {
  const child = spawn(core, [], {
    cwd: REPO_ROOT,
    env: {
      ...process.env,
      SIU_GUI_TEST_HOME: home,
      SIU_GUI_ROOT: join(home, ".siu"),
      ...(offline
        ? {
            HTTP_PROXY: "http://127.0.0.1:9",
            HTTPS_PROXY: "http://127.0.0.1:9",
            ALL_PROXY: "http://127.0.0.1:9",
            NO_PROXY: "",
          }
        : {}),
    },
    stdio: "pipe",
  });
  let next = 0;
  const pending = new Map<
    number,
    {
      resolve: (data: unknown) => void;
      reject: (error: Error) => void;
      timer: ReturnType<typeof setTimeout>;
    }
  >();
  createInterface({ input: child.stdout }).on("line", (line) => {
    const message = JSON.parse(line) as {
      id: number;
      ok: boolean;
      data: unknown;
      error?: string;
    };
    const request = pending.get(message.id);
    if (!request) return;
    pending.delete(message.id);
    clearTimeout(request.timer);
    if (message.ok) request.resolve(message.data);
    else request.reject(new Error(message.error || "Core request failed"));
  });
  child.on("error", (error) => {
    for (const request of pending.values()) {
      clearTimeout(request.timer);
      request.reject(error);
    }
    pending.clear();
  });
  child.on("exit", () => {
    for (const request of pending.values()) {
      clearTimeout(request.timer);
      request.reject(new Error("Core exited"));
    }
    pending.clear();
  });
  return {
    request: <T>(method: string, params: object = {}) =>
      new Promise<T>((resolve, reject) => {
        const id = ++next;
        const timer = setTimeout(() => {
          pending.delete(id);
          reject(new Error(method + " timed out"));
        }, 180000);
        pending.set(id, {
          resolve: (value) => resolve(value as T),
          reject,
          timer,
        });
        child.stdin.write(JSON.stringify({ id, method, params }) + "\n");
      }),
    close: async () => {
      if (child.exitCode !== null) return;
      const exit = once(child, "exit");
      child.stdin.end();
      const timer = setTimeout(() => child.kill("SIGKILL"), 5000);
      await exit;
      clearTimeout(timer);
    },
  };
}
let session = start();
async function check(name: string, action: () => Promise<void>) {
  try {
    await action();
    checks.push({ name, ok: true });
    console.log("PASS " + name);
  } catch (error) {
    checks.push({ name, ok: false, error: String(error) });
    throw error;
  }
}
try {
  let catalog: Discovery;
  await check("Official MCP Registry returns live search results", async () => {
    catalog = await session.request("discover", {
      kind: "mcp",
      query: "context7",
      refresh: true,
    });
    assert.equal(catalog.cached, false);
    assert.equal(catalog.warnings.length, 0, catalog.warnings.join("\n"));
    assert.ok(catalog.servers.some((server) => !server.featured));
  });
  await check(
    "Registry descriptors persist and install after an offline restart",
    async () => {
      await session.close();
      session = start(true);
      const cached = await session.request<Discovery>("discover", {
        kind: "mcp",
        query: "context7",
        refresh: true,
      });
      assert.equal(cached.cached, true);
      assert.ok(cached.warnings.length);
      const remote = cached.servers.find((server) => !server.featured)!;
      assert.ok(remote);
      const values = Object.fromEntries(
        remote.inputs
          .filter((input) => input.required)
          .map((input) => [input.key, "fixture-value"]),
      );
      const result = await session.request<MutationResult>("install_server", {
        id: remote.id,
        name: "offline-registry",
        values,
        agents: ["claude"],
      });
      assert.equal(
        result.report.issues.length,
        0,
        result.report.issues.join("\n"),
      );
      assert.equal(
        result.snapshot.mcp.find((server) => server.name === "offline-registry")
          ?.states.claude,
        "on",
      );
      const file = await readFile(join(home, ".claude.json"), "utf8");
      assert.ok(JSON.parse(file).mcpServers["offline-registry"]);
      await session.request("delete", {
        kind: "mcp",
        name: "offline-registry",
      });
      await session.close();
      session = start();
    },
  );
  await check("skills.sh returns searchable live Skills", async () => {
    const result = await session.request<Discovery>("discover", {
      kind: "skills",
      query: "react",
      refresh: true,
    });
    assert.equal(result.warnings.length, 0, result.warnings.join("\n"));
    assert.ok(result.skills.length > 0);
  });
  await check(
    "GitHub Skill scans, installs, updates and removes real linked files",
    async () => {
      const scan = await session.request<ScanResult>("scan_skills", {
        source:
          "https://github.com/vercel-labs/agent-skills/tree/main/skills/react-best-practices",
      });
      assert.ok(scan.skills.length);
      const skill = scan.skills[0]!;
      const installed = await session.request<MutationResult>(
        "install_skills",
        { token: scan.token, paths: [skill.path], agents: ["claude"] },
      );
      assert.equal(installed.report.issues.length, 0);
      const entry = installed.snapshot.skills.find(
        (entry) => entry.name === skill.name,
      )!;
      assert.equal(entry.source?.kind, "github");
      assert.equal(entry.states.claude, "on");
      const content = await readFile(
        join(home, ".claude", "skills", entry.name, "SKILL.md"),
        "utf8",
      );
      assert.ok(content.length > 100);
      const updated = await session.request<MutationResult>("update", {
        kind: "skills",
        name: entry.name,
      });
      assert.equal(
        updated.report.issues.length,
        0,
        updated.report.issues.join("\n"),
      );
      assert.equal(
        updated.snapshot.skills.find((skill) => skill.name === entry.name)
          ?.states.claude,
        "on",
      );
      await session.request("delete", { kind: "skills", name: entry.name });
      await session.request("release_scan", { token: scan.token });
      const final = await session.request<Snapshot>("snapshot");
      assert.equal(final.skills.length, 0);
      assert.equal(final.mcp.length, 0);
    },
  );
} catch (error) {
  console.error(error);
  process.exitCode = 1;
} finally {
  await session.close();
  await mkdir(join(REPO_ROOT, ".artifacts", "online"), { recursive: true });
  await writeFile(
    join(REPO_ROOT, ".artifacts", "online", "result.json"),
    JSON.stringify({ home, checks }, null, 2) + "\n",
  );
  console.log(
    JSON.stringify({
      home,
      passed: checks.filter((check) => check.ok).length,
      total: checks.length,
    }),
  );
}
