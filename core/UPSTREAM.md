# Upstream provenance

`core/upstream/` vendors nine modules from upstream **siu** (the terminal UI
at https://github.com/nicholasxjy/siu), at commit
`06333e8cbd6517b87bd918f351acf94fb678e20f` ("Release 0.0.5"):

- `agents.rs` — the agents siu knows and where each keeps MCP/Skills.
- `cache.rs` — small time-stamped JSON caches.
- `github.rs` — parsing a skill source spec and downloading a repo tarball.
- `library.rs` — the library: MCP servers, skills, and syncing them into agents.
- `market.rs` — featured MCP servers, the MCP Registry, and skills.sh.
- `mcp.rs` — reading/writing each agent's MCP config format.
- `net.rs` — the shared HTTP client.
- `skills.rs` — skill discovery, hashing, copying and linking.
- `update.rs` — `siu update`'s package/version planning and remote I/O.

Not vendored: `app.rs`, `ui.rs`, `theme.rs`, `main.rs`, and the TUI's own
`tests.rs` — these are the terminal UI itself and have no part in a
headless NDJSON service. `core/src/main.rs` declares the nine vendored
files as plain crate-root modules via `#[path = "../upstream/<file>.rs"]`,
so every internal `crate::library::...` / `crate::mcp::...` reference
inside them is unchanged from upstream and a diff against the original
file stays small and reviewable.

`core/upstream/LICENSE` is upstream's own MIT license, copied verbatim for
the vendored code's provenance; `core/LICENSE` covers the `siu-core` crate
itself (also MIT, same text, matching the repository's root `LICENSE`).

## Deviations from upstream, by file

The deviations below are kept separate from the GUI policy modules.

**`agents.rs`** — `Env::rooted` is no longer `#[cfg(test)]`-gated. siu-gui's
`SIU_GUI_TEST_HOME` isolation (`core/src/env.rs`) needs to build a fully
rooted `Env` at runtime, not only inside `cargo test`. No other behaviour
changed.

**`github.rs`** — `parse_spec` additionally recognises Windows absolute
paths (`C:\...`, `C:/...`), UNC paths (`\\server\share\...`), and any input
containing a backslash, as a local path rather than attempting to parse it
as a GitHub `owner/repo` spec (upstream's character-class check for
`owner`/`repo` already rejects a backslash, but fell through to `None`
instead of to `Spec::Local`). A local path that points straight at a
`SKILL.md` file now also normalizes to its parent folder, matching what
upstream already did for a GitHub `blob` URL ending the same way. Addresses
the gap: "github.parse_spec needs Windows drive/UNC/relative paths and
SKILL.md support."

**`skills.rs`** — `link` creates a Windows directory **junction**
(`junction` crate calling Windows APIs directly) instead of falling back to a full recursive
copy on any non-Unix target. A junction, unlike a symlink, needs no
administrator privilege on Windows. Direct API calls preserve spaces, Unicode and
shell characters in paths and keep command output out of the NDJSON stream.
Unlike upstream's previous
`#[cfg(not(unix))]` copy fallback — it stays a real link: a later skill
update or removal through siu's library actually affects what the agent
sees, instead of silently going stale. A new `unlink` helper removes a
siu-made link safely on both platforms: Windows junctions use `fs::remove_dir`,
which removes the junction's directory entry without deleting the target.
`junction::delete` only detaches the reparse point and leaves an empty directory,
so it is not used for unlinking. Windows file-type metadata also identifies
broken directory links. `fs::remove_file` rejects these directory links. Addresses the gap: "skills.rs's
`#[cfg(not(unix))]` copy_dir doesn't survive a Windows update/removal;
needs a Windows junction (no administrator) and a safe unlink."

**`library.rs`** — two deviations:

1. Every place that tears down a siu-made skill link (`import_skill`,
   both removal paths in `sync_skills`) now calls `skills::unlink` instead
   of `fs::remove_file` directly, so it's safe on a Windows junction too.
2. `backup` now returns a `Result` instead of swallowing its own I/O
   errors with `let _ = ...`, and `sync_mcp` aborts that agent's write
   (recording an issue, not touching the file) when the backup fails,
   instead of silently writing over the agent's config with no backup on
   disk to recover from. Addresses the gap: "upstream backup swallows
   exceptions; must not write without a backup."

**`mcp.rs`** — `write_atomic` now puts a brand-new file on user-only
permissions (`0o600` on Unix) at the moment of its first write, rather than
inheriting the process umask. `write_atomic` is the single place every
agent config write and the library's own `library.json`/cache files go
through, so this one change hardens every persisted file siu-core writes
that may carry a token, header value, or API key. Addresses the gap:
"apply reasonable user permissions to sensitive persisted files."
(`core/src/env.rs` separately hardens the _directory_ permissions of the
library root itself, best-effort, since `mcp.rs` only ever touches files.)

**`update.rs`** — `Net`, `Plan`, and `plan` are `pub(crate)` instead of
private. `core/src/update_single.rs` needs them to drive a single named
server's `update {kind:"mcp",name}` without reimplementing the npx/uvx/
docker package-spec parsing, and without going through `update_mcp`'s
"touch every server" loop, which would otherwise be the only way to reuse
this logic and would make needless network calls for every _other_ server
on a single-item update. No logic changed, only visibility. Addresses the
gap: "`update::Net` is private; a minimal visibility change lets it be
reused." The ignored network test now checks npm/PyPI package versions used by
MCP updates. The upstream siu CLI's Cargo self-update probe is outside the desktop
update path and is excluded from that test; desktop updates never install a CLI.

**`market.rs`** — `Place`, `Input` and `MarketServer` additionally derive
Serde serialization/deserialization so registry descriptors can be cached on disk
and remain installable after an application restart. Registry parsing and featured
server definitions retain upstream behavior.

**`cache.rs`, `net.rs`** — vendored verbatim, no changes.

## Gaps closed outside the vendored files (in `core/src/`)

A few of the understood gaps are deliberately **not** fixed by editing the
vendored upstream logic, because the right fix is a policy siu-core enforces
around `Store`, not a change to `Store` itself:

- **Snapshot states must reflect reality, not desired configuration.**
  Upstream's `Store::server_state`/`skill_state` only say what the library
  _wants_ (an agent picked, the entry enabled) — they never read the
  agent's actual file. `core/src/state.rs` additionally reads the agent's
  real parsed MCP entry (`mcp::read`) or real skill symlink
  (`skills::links_to`) _and_ cross-checks `Library.applied`, downgrading a
  desired-"on" entry to `"error"` whenever any of the three disagree (not
  yet synced, a prior partial sync failure, a hand-edited config). Per-entry
  `"error"`s are also surfaced in `Snapshot.issues`. Agent-format encoding
  comparisons include environment variables and headers. Failed pause/detach
  writes remain errors after reopening; applied entries removed from the library
  produce pending-removal issues. Skills folders are retained until all saved
  link references have been removed.
- **Don't silently take over an unmanaged same-name MCP entry.**
  `core/src/guard.rs::check_mcp_collision` reads the target agent's actual
  config before `save_server`/`set_agents` ever calls `add_server`/
  `edit_server`/`set_server_agents`, and refuses the mutation (asking for an
  explicit `import` instead) when an entry with that name exists there and
  isn't already recorded as siu's own in `Library.applied`.
- **Target/compatibility validation, and the shared Skills target.**
  `core/src/guard.rs::validate_targets` rejects an unknown agent id, an
  agent incompatible with the kind (or, for MCP, the specific server) being
  targeted, and an undetected agent — except the shared `agents` id for
  skills, which the protocol says may be picked before `~/.agents` exists.
  `agents` is never a valid MCP target (it has no MCP file at all).
- **Scan tokens, not arbitrary paths, drive `install_skills`.**
  `core/src/scan.rs::ScanManager` is the only place a GitHub download's
  `TempDir` (or a canonical local directory) is kept alive, bounded to 8
  retained scans (oldest evicted first) and a 30 minute TTL swept on every
  access. `install_skills` resolves `{token, paths}` against the scan's own
  recorded `Found` list — a path the scan never reported is rejected, not
  silently accepted as a filesystem path from the frontend.
- **A single-item `update` must not touch the rest of the library.**
  `core/src/update_single.rs` drives one named MCP server or skill's update
  directly against the real, unfiltered `Store` (never a copy reduced to one
  entry) so `Store::commit`/`Store::save` persists every other entry
  untouched; filtering `lib.mcp`/`lib.skills` down to one item first — the
  obvious alternative — is exactly what would make `sync` treat every
  _other_ previously-applied entry as something to remove.
- **Stdout is protocol only.** `core/src/server.rs` emits one JSON response
  line per request and does not log request bodies. `server::handle` wraps
  dispatch in `catch_unwind` and returns an error response on a panic.
  Snapshot diagnostics for malformed external configuration report its path
  without echoing parser snippets that might include credentials.

## `SIU_GUI_TEST_HOME`

`core/src/env.rs::resolve()` is the single place siu-core decides what
`Env` and library root to use for _every_ request (re-resolved every time,
never cached):

- If `SIU_GUI_TEST_HOME` is set and non-empty: the real `CLAUDE_CONFIG_DIR`,
  `CODEX_HOME`, `PI_CODING_AGENT_DIR`, `GROK_HOME`, and `SIU_HOME`
  environment variables are all ignored outright. `Env::rooted(test_home)`
  roots every agent path under the test home, and the library root is
  `<test home>/.siu` — never the real `SIU_HOME`, even if one happens to be
  set in the test process's environment.
- Otherwise: exactly upstream's own resolution — `Env::from_system()`, and
  the library root is `$SIU_HOME` (if set and non-empty) or else
  `~/.siu`.

Tests in this crate set `SIU_GUI_TEST_HOME` to a `tempfile::tempdir()` (or
construct `Env::rooted(...)` directly) and never touch a real home
directory.
