// Generates the license text shipped as a regular mygo resource.
import {
  readFile,
  readdir,
  realpath,
  stat,
  mkdir,
  writeFile,
} from "node:fs/promises";
import { join, dirname, resolve } from "node:path";
const root = resolve(import.meta.dir, "..");
const sections: string[] = [];
const missing: string[] = [];
const decoder = new TextDecoder();
function command(args: string[]) {
  const p = Bun.spawnSync(args, { cwd: root });
  if (p.exitCode !== 0) throw new Error(decoder.decode(p.stderr));
  return decoder.decode(p.stdout).trim();
}
function addSection(name: string, version: string, spdx: string, body: string) {
  sections.push(
    "=".repeat(78) +
      "\n" +
      name +
      " " +
      version +
      (spdx ? " [" + spdx + "]" : "") +
      "\n" +
      "=".repeat(78) +
      "\n\n" +
      body,
  );
}
async function license(name: string, version: string, path: string, spdx = "") {
  const names = (await readdir(path))
    .filter((name) =>
      /^(licen[cs]e|copying|copyright|notice|authors|patents)([._-]|$)/i.test(
        name,
      ),
    )
    .sort();
  const blocks: string[] = [];
  for (const name of names) {
    const file = join(path, name),
      info = await stat(file);
    if (info.isFile())
      blocks.push(name + "\n" + (await readFile(file, "utf8")));
    else if (info.isDirectory())
      for (const child of (await readdir(file)).sort()) {
        const childPath = join(file, child);
        if ((await stat(childPath)).isFile())
          blocks.push(
            name + "/" + child + "\n" + (await readFile(childPath, "utf8")),
          );
      }
  }
  if (!blocks.length) missing.push(name + " " + version + " (" + spdx + ")");
  addSection(name, version, spdx, blocks.join("\n\n"));
}
async function locate(name: string, from: string): Promise<string> {
  let dir = from;
  while (true) {
    const candidate = join(dir, "node_modules", name, "package.json");
    try {
      return await realpath(candidate);
    } catch {}
    const parent = dirname(dir);
    if (parent === dir) throw new Error("Cannot locate package " + name);
    dir = parent;
  }
}
const seen = new Set<string>();
async function npm(name: string, from: string) {
  const file = await locate(name, from),
    pkg = JSON.parse(await readFile(file, "utf8"));
  const key = pkg.name + "@" + pkg.version;
  if (seen.has(key)) return;
  seen.add(key);
  await license(
    pkg.name,
    pkg.version,
    dirname(file),
    typeof pkg.license === "string" ? pkg.license : "",
  );
  for (const dep of Object.keys(pkg.dependencies || {}).sort())
    await npm(dep, dirname(file));
}
const pkg = JSON.parse(await readFile(join(root, "package.json"), "utf8"));
await license(
  "Agent icon notices",
  "LobeHub ea434b96e75813517b377825e01c0df61dee8836; Pi pi.dev",
  join(root, "resources", "agents"),
);
await license("siu-gui", pkg.version, root, "MIT");
addSection(
  "siu upstream",
  "06333e8cbd6517b87bd918f351acf94fb678e20f; backup.rs: 1a3a7ef32a3c4dd9115da2e0b31fdc6680b79fdf",
  "MIT",
  "UPSTREAM_LICENSE\n" +
    (await readFile(join(root, "internal", "core", "UPSTREAM_LICENSE"), "utf8")),
);
for (const name of Object.keys(pkg.dependencies).sort()) await npm(name, root);
for (const line of command([
  "go",
  "list",
  "-m",
  "-f",
  "{{.Path}}|{{.Version}}|{{.Dir}}",
  "all",
]).split("\n")) {
  const [name, version, path] = line.split("|");
  if (version && path) await license(name, version, path);
}
await license(
  "Go runtime",
  command(["go", "env", "GOVERSION"]),
  command(["go", "env", "GOROOT"]),
  "BSD-3-Clause",
);
if (missing.length)
  throw new Error("Missing license files:\n" + missing.join("\n"));
await mkdir(join(root, "resources"), { recursive: true });
await writeFile(
  join(root, "resources", "THIRD_PARTY_LICENSES.txt"),
  "Siu GUI — third party license notices\n\n" + sections.join("\n\n"),
);
console.log("Collected " + sections.length + " component license sections.");
