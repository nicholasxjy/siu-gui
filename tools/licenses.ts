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
      blocks.join("\n\n"),
  );
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
await license("siu-gui", pkg.version, root, "MIT");
await license(
  "siu upstream",
  "06333e8cbd6517b87bd918f351acf94fb678e20f",
  join(root, "core", "upstream"),
  "MIT",
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
const metadata = JSON.parse(
  command([
    "cargo",
    "metadata",
    "--format-version",
    "1",
    "--locked",
    "--manifest-path",
    "core/Cargo.toml",
  ]),
);
// Cargo metadata includes Android/iOS packages even though Siu only ships
// desktop builds. Collect the union of actual dependency graphs for our
// six supported targets, so notices match the code we distribute.
const desktopPackages = new Set<string>();
for (const target of [
  "aarch64-apple-darwin",
  "x86_64-apple-darwin",
  "aarch64-pc-windows-msvc",
  "x86_64-pc-windows-msvc",
  "aarch64-unknown-linux-gnu",
  "x86_64-unknown-linux-gnu",
]) {
  const graph = JSON.parse(
    command([
      "cargo",
      "metadata",
      "--format-version",
      "1",
      "--locked",
      "--manifest-path",
      "core/Cargo.toml",
      "--filter-platform",
      target,
    ]),
  );
  const dependencies = new Map<string, string[]>(
    graph.resolve.nodes.map((node: { id: string; deps: { pkg: string }[] }) => [
      node.id,
      node.deps.map((dep) => dep.pkg),
    ]),
  );
  const seen = new Set<string>();
  const pending: string[] = [graph.resolve.root];
  while (pending.length) {
    const id = pending.pop()!;
    if (seen.has(id)) continue;
    seen.add(id);
    desktopPackages.add(id);
    pending.push(...(dependencies.get(id) || []));
  }
}
const winapi = metadata.packages.find(
  (p: { name: string }) => p.name === "winapi",
);
for (const p of metadata.packages
  .filter(
    (p: { id: string; source: string | null }) =>
      p.source !== null && desktopPackages.has(p.id),
  )
  .sort((a: { name: string }, b: { name: string }) =>
    a.name.localeCompare(b.name),
  )) {
  const source = p.name.startsWith("winapi-") && winapi ? winapi : p;
  await license(
    p.name,
    p.version,
    dirname(source.manifest_path),
    p.license || "",
  );
}
if (missing.length)
  throw new Error("Missing license files:\n" + missing.join("\n"));
await mkdir(join(root, "resources"), { recursive: true });
await writeFile(
  join(root, "resources", "THIRD_PARTY_LICENSES.txt"),
  "Siu GUI — third party license notices\n\n" + sections.join("\n\n"),
);
console.log("Collected " + sections.length + " component license sections.");
