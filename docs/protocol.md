# Go core protocol

The desktop and terminal apps call `internal/core` in-process. The desktop uses the generated mygo `Desktop.request` binding, which passes JSON strings through `Desktop.Request`. `cmd/siu-tui` calls `Service.Request` directly with the same envelopes. `cmd/testhost` exposes a loopback adapter for browser tests. [Core service](../internal/core/service.go) · [Desktop binding](../desktop.go) · [Test adapter](../cmd/testhost/main.go)

## Request and response

Send a JSON object containing a `method`, an optional `params` object, and an optional `id`. Missing `params` defaults to an empty object. Responses echo the `id` if supplied.

```json
{
  "id": 1,
  "method": "set_enabled",
  "params": { "kind": "skills", "name": "example", "enabled": false }
}
```

Success is `{ok:true,data}`. Invalid input and operation errors use `{ok:false,error:string}`. A mutation can succeed in saving desired state while individual Agent writes fail. Read `report.issues` and the returned `snapshot` before reporting completion.

Each service instance serializes write operations. Writes reopen the library to pick up changes already saved by another client. Use one writer at a time across processes. `snapshot` also checks whether a changed `config.json` needs to be restored under the service lock. Keep one service alive across related discovery and installation calls so it can retain scan tokens and MCP descriptors.

## Shared data shapes

The exact JSON fields and serialization are defined in Go. [DTO definitions](../internal/core/protocol.go)

| Type                     | Fields and behavior                                                                                                                                                                                                                 |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Snapshot`               | `home`, `root`, `platform`, `version`, `agents`, `mcp`, `skills`, `foundMcp`, `foundSkills`, `issues`, optional `autoConfig` (`digest`, optional `theme`). The desktop also receives `instructions`.                                |
| `Agent`                  | `id`, `name`, `home`, nullable `mcpPath` and `skillsPath`, `detected`, and `transports`. Assignment requires a detected, compatible Agent. The shared `agents` Skills target can be selected before its directory exists.           |
| `Server`                 | `name`, `transport` (`stdio`, `http`, `sse`), `command`, `args`, `env`, `url`, and `headers`. Responses normalize empty arrays and maps. Core responses provide real configuration values. Clients redact credentials for previews. |
| `McpEntry`               | `Server` fields plus `agents`, `enabled`, nullable `origin` and `states` by Agent ID.                                                                                                                                               |
| `SkillEntry`             | `name`, `description`, `source`, `agents`, `enabled`, `hash`, `installed`, `updated` and `states` by Agent ID. `source` is `null`, `{kind:"github",repo,ref,path}`, or `{kind:"local",dir}`.                                        |
| `FoundMcp`, `FoundSkill` | `{server:Server,agents:string[]}` and `{name,description,at:{agent,path}[]}` describe unmanaged resources found in Agent configurations.                                                                                            |
| `MutationResult`         | `{snapshot:Snapshot,report:{changed:number,issues:string[]},message:string,log?:string,theme?:string}`. The import operation may return `theme`.                                                                                    |

Per-Agent states are `on`, `off`, `paused`, `error`, or `unsupported:<reason>`. `on` confirms configuration on disk. Agent reload and MCP runtime health are handled by each Agent. Client errors and issues may include paths from the local machine.

## Resource library

`kind` is `mcp` or `skills`. Unless listed otherwise, these mutations return `MutationResult`.

| Method and params                              | Behavior                                                                                                                                                                        |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `snapshot {}`                                  | Read the library, unmanaged resources and observed Agent state. Apply new default backups as described below. Returns `Snapshot`.                                               |
| `save_server {server,agents,oldName?,origin?}` | Add or edit a server. `oldName` selects the entry to edit and allows renaming it. Editing retains the enabled state. The operation rejects unrelated unmanaged name collisions. |
| `set_agents {kind,name,agents}`                | Update assignments and retain the enabled state.                                                                                                                                |
| `set_enabled {kind,name,enabled}`              | Enable or pause a resource and retain its assignments.                                                                                                                          |
| `import {kind,name}`                           | Rescan unmanaged entries and bring the named resource into the library.                                                                                                         |
| `delete {kind,name}` / `clear {kind}`          | Remove one resource or all managed resources of a kind. Retract Siu-owned assignments and retain unmanaged resources and backups.                                               |
| `sync {}`                                      | Retry writing the current desired state to Agent configurations.                                                                                                                |
| `update {kind,name?}`                          | Update one resource, or all resources of that kind when `name` is omitted. Consult `report.issues` and `log` for individual failures.                                           |

### Discovery and Skills

- `discover {kind,query,refresh?}` returns `{servers:MarketServer[],skills:MarketSkill[],warnings:string[],cached:boolean}`. An empty MCP query returns featured entries. A nonempty MCP query also searches the official Registry. Skills queries use skills.sh, with cached results when available. Check `warnings` for degraded results.
- `MarketServer` contains `id`, `title`, `description`, `publisher`, `homepage`, `server`, `inputs`, and `featured`. Each input contains `key`, `place` (`env`, `header`, or `arg`), `label`, `hint`, `secret`, `required`, and `format`. A `MarketSkill` contains `source`, `skillId`, `name`, `installs`, and `isOfficial`.
- `install_server {id,values,agents,name?}` returns `MutationResult`. Use a featured or previously discovered `id`. `values` maps input keys to strings. The core fills the retained descriptor and validates required values and compatible targets.
- `scan_skills {source}` accepts a GitHub `owner/repo`, a GitHub URL, or a local directory. Use a platform-native absolute path or an explicit local prefix such as `./` or `~/`. A local `SKILL.md` resolves to its directory. The result is `{token,source,skills:{name,description,path,installed}[]}`. Each service retains at most eight scans for up to 30 minutes.
- `install_skills {token,paths,agents}` returns `MutationResult`. The `paths` must come from the retained scan. Existing and duplicate Skills are rejected.
- `release_scan {token}` returns `{}` and frees temporary data. `skill_content {name}` returns `{content,path}` for a managed Skill's `SKILL.md` up to 1 MiB.

## Proxy settings

`proxy_settings {}` returns `ProxySettings`. `set_proxy_settings {proxy}` validates and atomically stores the policy in `<root>/proxy.json`. `ProxySettings` has `mode`, `url`, and `noProxy`. The default mode is `system`, with empty `url` and `noProxy`. Proxy credentials travel through the explicit settings methods and backup files.

- `system` uses `NO_PROXY` bypasses, then `HTTP_PROXY` or `HTTPS_PROXY` with `ALL_PROXY` as fallback, then the host's configured proxy endpoints on macOS or Windows. Lowercase environment variables are accepted. Linux uses the environment for system routing. For PAC/WPAD-only environments, provide an explicit proxy endpoint.
- `custom` accepts `http://`, `https://`, `socks5://`, or `socks5h://`, with optional percent-encoded credentials and a comma-separated bypass list of hostnames, IPs or CIDRs.
- `direct` uses a direct connection. Changes affect subsequent requests. The Docker daemon needs its own proxy configuration for image transfers.

[Proxy implementation](../internal/core/proxy.go)

## Backup transfer

All three methods take an absolute `path`. The format is `siu-gui-backup` v1 and includes desired MCP and Skills state, Skill files, source paths, and proxy policy, with optional desktop theme. It carries credentials and uses private atomic writes for exports. Limits are 128 MiB for the JSON, 64 MiB for decoded Skill files, and 20,000 files. [Backup implementation](../internal/core/backup.go) · [User guide](backup.md)

| Method and params             | Result and behavior                                                                                                                                                                                                                                                                      |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `export_config {path,theme?}` | `{path,mcpCount,skillCount}`. The permitted location within the library is `<root>/config.json`. Destinations in Agent MCP or Skills configuration are protected.                                                                                                                        |
| `preview_config {path}`       | `{path,digest,exportedAt,mcpCount,skillCount,issues,theme}`. `theme` can be `null`. Validate the backup and check for collisions. Keep the SHA-256 digest for import.                                                                                                                    |
| `import_config {path,digest}` | `MutationResult`. Verify the original file's digest, take a safety backup in `backups/config-*.json`, replace the managed state, and sync Agents. Preserve unrelated unmanaged resources. Local persistence failures roll back staged changes. Use `sync` to retry partial Agent writes. |

`snapshot` checks `<root>/config.json` and applies new or changed content using the same restore path. The shared `config.applied.json` receipt prevents reapplying an unchanged file. Exporting the default backup leaves the receipt untouched. Invalid files appear in `snapshot.issues`.

The terminal service omits instructions from snapshots and skips a top-level `instructions` field if present in an input backup. The digest still covers the original bytes. Current exports from both clients contain managed resources and omit instruction files. The desktop applies a restored theme if the backup includes one.

## Desktop-only calls

The desktop `instructions` snapshot array lists user-level Markdown files as `{id,agent,name,path,status,issue?}`. Status is `available`, `disabled`, `missing`, or `unavailable`.

- `instruction_content {id}` returns `{content,revision}` for an `available` or `disabled` UTF-8 file up to 1 MiB.
- `save_instruction {id,content,revision?}` returns `MutationResult`. Omit `revision` to create a `missing` file. Supply it when editing an `available` or `disabled` file (preserving its disabled state) to detect concurrent changes.
- `set_instruction_enabled {id,enabled}` returns `MutationResult`. Disabling renames `<path>` to `<path>.siu-disabled` so Agents stop loading it; enabling renames it back.
- `delete_instruction {id}` returns `MutationResult`. Deletes the active or `.siu-disabled` file; deleting a symlink removes the link itself. The core resolves IDs against the currently supported files.

The generated mygo client also exposes native helpers for picking a directory, choosing an import file or export destination, opening an existing path or an HTTP(S) URL, reading app info, and setting the desktop theme. Dialog cancellation returns an empty path. [Generated client](../src/mygo.ts)
