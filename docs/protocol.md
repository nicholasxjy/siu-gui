# Desktop / core protocol

The Go mygo service exposes `Request(payload: string): Promise<string>`. Both strings contain JSON; the frontend owns the typed DTOs below. Go multiplexes a persistent bundled `siu-core` process over newline-delimited JSON. Stdout is protocol only; diagnostics go to stderr. Each request has a numeric `id`, `method`, and object `params`. Responses repeat `id` and contain either `{ok:true,data}` or `{ok:false,error:string}`. Methods must validate all input, including unknown names and unsupported Agent targets. All mutations are serialized and reopen the existing siu library before applying changes so refresh observes CLI changes. Do not automatically sync on startup or snapshot.

## DTOs

Use camelCase for new DTO keys. Existing siu MCP/Skill entries keep upstream serde field names.

`Server`: `{name,transport:"stdio"|"http"|"sse",command:string,args:string[],env:Record<string,string>,url:string,headers:Record<string,string>}`. Missing optional string/maps/array keys in existing libraries should be serialized with empty defaults for frontend convenience.

`Agent`: `{id,name,home,mcpPath:string|null,skillsPath:string|null,detected:boolean,transports:string[]}`. Return all known Agents so the UI can explain detection. Only detected compatible Agents may be selected, except shared Skills target `agents`, which can be deliberately selected even if its directory does not exist yet. Shared directory is not an MCP target.

`Snapshot`: `{home,root,platform,version,agents:Agent[],mcp:McpEntry[],skills:SkillEntry[],foundMcp:FoundMcp[],foundSkills:FoundSkill[],issues:string[]}`.

`McpEntry`: `Server & {agents:string[],enabled:boolean,origin?:string,states:Record<string,string>}`. `SkillEntry`: upstream `{name,description,source:null|{kind:"github",repo,ref,path}|{kind:"local",dir},agents,enabled,hash,installed,updated}` plus `states`. States are `on`, `off`, `paused`, `error`, or `unsupported:<reason>`. `on` means successfully configured, never runtime health. Inspect the Agent's actual parsed MCP definition / Skills link plus library applied record to avoid false success after partial sync failures. Include useful problems in `Snapshot.issues`.

`FoundMcp`: `{server:Server,agents:string[]}`. `FoundSkill`: `{name,description,at:{agent:string,path:string}[]}`.

`MutationResult`: `{snapshot:Snapshot,report:{changed:number,issues:string[]},message:string,log?:string}`. Failed individual writes return a result with issues and actual state, not an all-success toast. Library parse / invalid input errors use `{ok:false,error}`. Network update failures may be returned as report issues with log.

`MarketInput`: `{key,place:"env"|"header"|"arg",label,hint,secret:boolean,required:boolean,format:string}`. `MarketServer`: `{id,title,description,publisher,homepage,server:Server,inputs:MarketInput[],featured:boolean}`. `MarketSkill`: upstream serialized `{source,skillId,name,installs,isOfficial}`.

`ScanResult`: `{token:string,source:string,skills:{name,description,path:string,installed:boolean}[]}`. A scan token owns a TempDir for downloaded GitHub sources (or a canonical local directory); retain it across calls, expire unused tokens after 30 minutes, cap at 8 retained scans and explain expired token errors. The install operation must use this retained scan instead of accepting arbitrary source paths from the frontend.

## Methods and params

- `snapshot {}` -> Snapshot.
- `sync {}` -> MutationResult. Explicit retry of desired state after a partial write failure.
- `save_server {server:Server,agents:string[],oldName?:string,origin?:string}` -> MutationResult. Add or edit/rename without changing enabled state when editing. Reject unrelated unmanaged name collisions rather than overwriting them; explicit import is required.
- `set_agents {kind:"mcp"|"skills",name:string,agents:string[]}` -> MutationResult. Retain global enabled state.
- `set_enabled {kind,name,enabled:boolean}` -> MutationResult.
- `delete {kind,name}` -> MutationResult; prune only after successful sync and preserve external resources.
- `clear {kind:"mcp"|"skills"}` -> MutationResult. Remove every managed resource of this kind and retract its applied entries in one transaction; retain unmanaged resources and backups. The UI requires a confirmation showing the affected resource count.
- `import {kind,name}` -> MutationResult; rescan actual unmanaged entries rather than trusting supplied file paths.
- `scan_skills {source:string}` -> ScanResult. Accept GitHub `owner/repo`, URLs, and native local paths (Windows drive, UNC, `~`, relative paths included). SKILL.md selection can be normalized to its parent.
- `install_skills {token:string,paths:string[],agents:string[]}` -> MutationResult; match the retained scan, reject installed/duplicate names, no arbitrary path traversal.
- `release_scan {token:string}` -> `{}`. Drop downloaded temp data on cancellation.
- `skill_content {name:string}` -> `{content:string,path:string}`. Only managed library Skills; bounded 1 MiB; do not accept arbitrary path.
- `discover {kind:"mcp"|"skills",query:string,refresh?:boolean}` -> `{servers:MarketServer[],skills:MarketSkill[],warnings:string[],cached:boolean}`. MCP featured list first, Registry search for non-empty query, dedup. Skills popular / search using upstream, cache fallback with explicit warnings on network failure. Empty arrays on irrelevant branch. Search must not block local mutations.
- `install_server {id:string,values:Record<string,string>,agents:string[],name?:string}` -> MutationResult. Use actual previously discovered/featured server metadata; validate all required inputs; fill using upstream `MarketServer.fill`. Keep descriptor cache in memory from discovery, bounded. Do not accept arbitrary executable payload as a market result. Manual setup uses save_server.
- `update {kind:"mcp"|"skills",name?:string}` -> MutationResult. With name update only that resource; otherwise all. Use upstream update planning and report results. Never update/reinstall the GUI through cargo.

## Desktop helpers (Go service methods)

These are ordinary mygo bound methods, separate from the core protocol:

- `PickDirectory(): Promise<string>` returns a selected native directory, empty on cancel.
- `OpenPath(path:string): Promise<void>` opens an existing file/directory with the OS. Validate path, never invoke a shell.
- `OpenURL(url:string): Promise<void>` allow http/https only.
- `Info(): Promise<{os,arch,version}>`.
- `SetTheme(theme:"system"|"light"|"dark"): Promise<void>`.

No production HTTP API or localhost listener is needed. A development/test-only loopback harness may serve these requests for browser automation, with explicit test-home isolation, origin validation and no production build inclusion.
