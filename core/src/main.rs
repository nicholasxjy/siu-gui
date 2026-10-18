//! siu-core: the NDJSON sidecar behind the siu-gui desktop app.
//!
//! Vendored upstream siu modules live physically under `core/upstream/`
//! (see `core/UPSTREAM.md`) but are declared here with `#[path]` as plain
//! crate-root modules — `crate::agents`, `crate::library`, `crate::mcp`,
//! and so on, exactly as they are inside upstream's own `main.rs`. That
//! keeps every internal `crate::library::...`/`crate::mcp::...` reference
//! inside the vendored files unchanged from upstream, so a diff against
//! the original file stays small and reviewable; only the deviations
//! documented at the top of each vendored file (and in UPSTREAM.md) are
//! real changes. TUI-only modules (`app`, `ui`, `theme`, the TUI's own
//! `tests`) are not vendored at all — siu-core never draws a terminal UI.

// `#[allow(dead_code)]`: these are vendored verbatim-as-possible, kept at
// their full upstream API surface (e.g. `Store::toggle_server_agent`,
// `update::latest`) even where siu-core's own protocol methods don't
// currently call every one of them, rather than trimming the vendored
// file down to only what's used today.
#[path = "../upstream/agents.rs"]
#[allow(dead_code)]
mod agents;
#[path = "../upstream/cache.rs"]
mod cache;
#[path = "../upstream/github.rs"]
mod github;
#[path = "../upstream/library.rs"]
#[allow(dead_code)]
mod library;
#[path = "../upstream/market.rs"]
#[allow(dead_code)]
mod market;
#[path = "../upstream/mcp.rs"]
mod mcp;
#[path = "../upstream/net.rs"]
mod net;
#[path = "../upstream/skills.rs"]
mod skills;
#[path = "../upstream/update.rs"]
#[allow(dead_code)]
mod update;

mod env;
mod guard;
mod handlers;
mod protocol;
mod proxy;
mod registry_cache;
mod scan;
mod server;
mod state;
mod update_single;

fn main() {
    server::run();
}
