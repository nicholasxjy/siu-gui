//! Resolves the `Env` and library root siu-core runs with.
//!
//! Normal operation follows upstream siu exactly: `Env::from_system()` plus
//! `SIU_HOME` (or `~/.siu`). When `SIU_GUI_TEST_HOME` is set and non-empty,
//! every real per-agent directory override (`CLAUDE_CONFIG_DIR`,
//! `CODEX_HOME`, `PI_CODING_AGENT_DIR`, `GROK_HOME`) and `SIU_HOME` itself
//! are ignored outright: the environment is rooted entirely under the test
//! home, and the library root defaults to `<test home>/.siu`. This gives
//! integration tests (and any other embedder) a way to run siu-core
//! without ever touching the real machine's agent configuration.

use std::path::PathBuf;

use crate::agents::Env;

pub const TEST_HOME_VAR: &str = "SIU_GUI_TEST_HOME";

pub struct Resolved {
    pub env: Env,
    pub root: PathBuf,
}

pub fn resolve() -> Resolved {
    if let Some(test_home) = std::env::var_os(TEST_HOME_VAR).filter(|v| !v.is_empty()) {
        let home = PathBuf::from(test_home);
        let _ = std::fs::create_dir_all(&home);
        let env = Env::rooted(&home);
        let root = home.join(".siu");
        harden(&home);
        harden(&root);
        return Resolved { env, root };
    }
    let env = Env::from_system();
    let root = std::env::var_os("SIU_HOME")
        .filter(|v| !v.is_empty())
        .map(PathBuf::from)
        .unwrap_or_else(|| env.home.join(".siu"));
    harden(&root);
    Resolved { env, root }
}

/// Best-effort: the library root (and the test home) hold MCP server
/// definitions and, after a sync, copies of agent config that may carry
/// tokens — keep the directory itself user-only where the platform
/// supports it. Never fails the caller; permissions are a hardening
/// measure, not a correctness requirement.
#[cfg(unix)]
fn harden(dir: &std::path::Path) {
    use std::fs;
    use std::os::unix::fs::PermissionsExt;
    let _ = fs::create_dir_all(dir);
    if let Ok(meta) = fs::metadata(dir) {
        let _ = fs::set_permissions(
            dir,
            fs::Permissions::from_mode(0o700 | (meta.permissions().mode() & !0o777)),
        );
    }
}

#[cfg(not(unix))]
fn harden(_dir: &std::path::Path) {}
