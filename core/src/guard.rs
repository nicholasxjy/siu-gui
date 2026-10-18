//! Guards applied before a mutation ever touches the library:
//! - `validate_targets` rejects unknown agent ids and agents the chosen
//!   kind (or, for MCP, the given server) is incompatible with. The
//!   shared `agents` target is only ever valid for skills, and — per the
//!   protocol — may be picked even when `~/.agents` doesn't exist yet.
//! - `check_mcp_collision` refuses to let a `save_server` silently take
//!   over an MCP entry some other tool already put in an agent's config
//!   under the same name; the caller must `import` it explicitly first.

use std::collections::BTreeSet;

use crate::library::Store;
use crate::mcp::{self, Server};

pub fn validate_targets(
    kind: &str,
    ids: &[String],
    store: &Store,
    server: Option<&Server>,
) -> Result<BTreeSet<String>, String> {
    let mut out = BTreeSet::new();
    for id in ids {
        let Some(agent) = store.agent(id) else {
            return Err(format!("unknown agent {id:?}"));
        };
        match kind {
            "mcp" => {
                if id == "agents" {
                    return Err("~/.agents (shared) is not an MCP target".into());
                }
                let Some(file) = &agent.mcp else {
                    return Err(format!("{} has no MCP config siu can write", agent.name));
                };
                if let Some(s) = server
                    && let Some(why) = mcp::unsupported(file.format, s)
                {
                    return Err(format!("{}: {why}", agent.name));
                }
                if !agent.detected() {
                    return Err(format!("{} isn't detected", agent.name));
                }
            }
            "skills" => {
                if agent.skills.is_none() {
                    return Err(format!("{} has no skills folder", agent.name));
                }
                // the shared ~/.agents target may be picked before its
                // folder exists; every other agent must already be detected
                if id != "agents" && !agent.detected() {
                    return Err(format!("{} isn't detected", agent.name));
                }
            }
            other => return Err(format!("unknown kind {other:?}")),
        }
        out.insert(id.clone());
    }
    Ok(out)
}

/// Refuses an add/rename/retarget that would silently overwrite an MCP
/// entry some other tool put in an agent's config under the same name.
/// An agent already recorded as siu's own for that name (in
/// `lib.applied`) is fine — that's an edit, not a collision.
pub fn check_mcp_collision(
    store: &Store,
    name: &str,
    agents: &BTreeSet<String>,
) -> Result<(), String> {
    for id in agents {
        let Some(agent) = store.agent(id) else {
            continue;
        };
        let Some(file) = &agent.mcp else { continue };
        let claimed = store
            .lib
            .applied
            .get(id)
            .is_some_and(|a| a.mcp.contains(name));
        if claimed {
            continue;
        }
        // an unreadable file surfaces its own error when the real write
        // happens; don't block the mutation on it here
        let Ok(existing) = mcp::read(file) else {
            continue;
        };
        if existing.contains_key(name) {
            return Err(format!(
                "{} already has an MCP server called {name} that siu doesn't manage; import it explicitly first",
                agent.name
            ));
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::agents::Env;
    use crate::mcp::Transport;
    use std::fs;

    fn store(home: &std::path::Path) -> Store {
        fs::create_dir_all(home.join(".claude")).unwrap();
        Store::open(&Env::rooted(home), home.join(".siu")).unwrap()
    }

    #[test]
    fn shared_agents_is_only_a_skills_target() {
        let d = tempfile::tempdir().unwrap();
        let s = store(d.path());
        assert!(validate_targets("mcp", &["agents".into()], &s, None).is_err());
        assert!(validate_targets("skills", &["agents".into()], &s, None).is_ok());
    }

    #[test]
    fn an_undetected_agent_is_rejected_except_the_shared_one() {
        let d = tempfile::tempdir().unwrap();
        let s = store(d.path());
        assert!(validate_targets("mcp", &["codex".into()], &s, None).is_err());
        assert!(validate_targets("skills", &["codex".into()], &s, None).is_err());
    }

    #[test]
    fn an_incompatible_server_is_rejected_for_its_agent() {
        let d = tempfile::tempdir().unwrap();
        fs::create_dir_all(d.path().join(".config/Claude")).unwrap();
        let s = Store::open(&Env::rooted(d.path()), d.path().join(".siu")).unwrap();
        let sse = Server {
            name: "x".into(),
            transport: Transport::Sse,
            url: "https://x.dev/sse".into(),
            ..Default::default()
        };
        assert!(validate_targets("mcp", &["claude-desktop".into()], &s, Some(&sse)).is_err());
    }

    #[test]
    fn an_unmanaged_same_name_entry_blocks_the_add() {
        let d = tempfile::tempdir().unwrap();
        fs::create_dir_all(d.path().join(".claude")).unwrap();
        fs::write(
            d.path().join(".claude.json"),
            r#"{"mcpServers":{"ctx7":{"type":"stdio","command":"other","args":[],"env":{}}}}"#,
        )
        .unwrap();
        let s = Store::open(&Env::rooted(d.path()), d.path().join(".siu")).unwrap();
        let agents = BTreeSet::from(["claude".to_string()]);
        assert!(check_mcp_collision(&s, "ctx7", &agents).is_err());
        assert!(check_mcp_collision(&s, "unrelated", &agents).is_ok());
    }

    #[test]
    fn an_entry_already_claimed_by_siu_is_not_a_collision() {
        let d = tempfile::tempdir().unwrap();
        fs::create_dir_all(d.path().join(".claude")).unwrap();
        fs::write(
            d.path().join(".claude.json"),
            r#"{"mcpServers":{"ctx7":{"type":"stdio","command":"me","args":[],"env":{}}}}"#,
        )
        .unwrap();
        let mut s = Store::open(&Env::rooted(d.path()), d.path().join(".siu")).unwrap();
        s.lib
            .applied
            .entry("claude".into())
            .or_default()
            .mcp
            .insert("ctx7".into());
        let agents = BTreeSet::from(["claude".to_string()]);
        assert!(check_mcp_collision(&s, "ctx7", &agents).is_ok());
    }
}
