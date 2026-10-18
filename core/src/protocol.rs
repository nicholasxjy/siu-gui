//! JSON encoding for the DTOs in docs/protocol.md. Every function here
//! builds a `serde_json::Value` directly (rather than through a derived
//! `Serialize` on a parallel struct) so optional `Server` fields are
//! always present with their empty default, as the protocol requires for
//! frontend convenience, instead of upstream's storage-oriented
//! `skip_serializing_if`.

use std::path::Path;

use serde_json::{Value, json};

use crate::agents::{Agent, McpFile};
use crate::library::{McpEntry, Report, SkillEntry, Store};
use crate::market;
use crate::mcp::{self, Server, Transport};
use crate::skills::Found;
use crate::state;

pub fn encode_server(s: &Server) -> Value {
    json!({
        "name": s.name,
        "transport": s.transport.label(),
        "command": s.command,
        "args": s.args,
        "env": s.env,
        "url": s.url,
        "headers": s.headers,
    })
}

pub fn decode_server(v: &Value) -> Result<Server, String> {
    serde_json::from_value::<Server>(v.clone()).map_err(|e| format!("invalid server: {e}"))
}

fn transports_for(file: Option<&McpFile>) -> Vec<&'static str> {
    let Some(file) = file else { return vec![] };
    [
        ("stdio", Transport::Stdio),
        ("http", Transport::Http),
        ("sse", Transport::Sse),
    ]
    .into_iter()
    .filter_map(|(label, t)| {
        let probe = Server {
            name: "probe".into(),
            transport: t,
            command: "x".into(),
            url: "https://x".into(),
            ..Default::default()
        };
        mcp::unsupported(file.format, &probe)
            .is_none()
            .then_some(label)
    })
    .collect()
}

pub fn encode_agent(a: &Agent) -> Value {
    json!({
        "id": a.id,
        "name": a.name,
        "home": a.home.display().to_string(),
        "mcpPath": a.mcp.as_ref().map(|f| f.path.display().to_string()),
        "skillsPath": a.skills.as_ref().map(|p| p.display().to_string()),
        "detected": a.detected(),
        "transports": transports_for(a.mcp.as_ref()),
    })
}

pub fn encode_mcp_entry(
    e: &McpEntry,
    states: &std::collections::BTreeMap<String, String>,
) -> Value {
    let mut v = encode_server(&e.server);
    let obj = v.as_object_mut().expect("encode_server returns an object");
    obj.insert("agents".into(), json!(e.agents));
    obj.insert("enabled".into(), json!(e.enabled));
    obj.insert("origin".into(), json!(e.origin));
    obj.insert("states".into(), json!(states));
    v
}

pub fn encode_skill_entry(
    e: &SkillEntry,
    states: &std::collections::BTreeMap<String, String>,
) -> Value {
    json!({
        "name": e.name,
        "description": e.description,
        "source": e.source,
        "agents": e.agents,
        "enabled": e.enabled,
        "hash": e.hash,
        "installed": e.installed,
        "updated": e.updated,
        "states": states,
    })
}

pub fn encode_market_input(i: &market::Input) -> Value {
    let place = match i.place {
        market::Place::Env => "env",
        market::Place::Header => "header",
        market::Place::Arg => "arg",
    };
    json!({
        "key": i.key,
        "place": place,
        "label": i.label,
        "hint": i.hint,
        "secret": i.secret,
        "required": i.required,
        "format": i.format,
    })
}

pub fn encode_market_server(m: &market::MarketServer) -> Value {
    json!({
        "id": m.id,
        "title": m.title,
        "description": m.description,
        "publisher": m.publisher,
        "homepage": m.homepage,
        "server": encode_server(&m.server),
        "inputs": m.inputs.iter().map(encode_market_input).collect::<Vec<_>>(),
        "featured": m.featured,
    })
}

pub fn encode_found(f: &Found, installed: bool) -> Value {
    json!({
        "name": f.name(),
        "description": f.meta.description,
        "path": f.path,
        "installed": installed,
    })
}

pub fn encode_scan_result(token: &str, source: &str, found: &[Found], store: &Store) -> Value {
    json!({
        "token": token,
        "source": source,
        "skills": found
            .iter()
            .map(|f| encode_found(f, store.skill(&f.name()).is_some()))
            .collect::<Vec<_>>(),
    })
}

/// `Snapshot.issues` plus every per-entry issue collected while computing
/// `states`: an entry desired "on" but not actually applied in some agent
/// is surfaced here too, not only inside its `states` map.
pub fn build_snapshot(store: &Store, home: &Path, version: &str) -> Value {
    let mut issues = vec![];
    let mcp: Vec<Value> = store
        .lib
        .mcp
        .iter()
        .map(|e| {
            let states = state::mcp_states(store, e);
            state::collect_issues(
                "MCP server",
                &e.server.name,
                &states,
                &store.agents,
                &mut issues,
            );
            encode_mcp_entry(e, &states)
        })
        .collect();
    let skills: Vec<Value> = store
        .lib
        .skills
        .iter()
        .map(|e| {
            let states = state::skill_states(store, e);
            state::collect_issues("Skill", &e.name, &states, &store.agents, &mut issues);
            encode_skill_entry(e, &states)
        })
        .collect();
    let agents: Vec<Value> = store.agents.iter().map(encode_agent).collect();
    let found_mcp: Vec<Value> = store
        .found_servers()
        .into_iter()
        .map(|f| json!({"server": encode_server(&f.server), "agents": f.agents}))
        .collect();
    let found_skills: Vec<Value> = store
        .found_skills()
        .into_iter()
        .map(|f| {
            json!({
                "name": f.name,
                "description": f.description,
                "at": f.at.iter().map(|(agent, path)| json!({
                    "agent": agent,
                    "path": path.display().to_string(),
                })).collect::<Vec<_>>(),
            })
        })
        .collect();
    state::collect_orphan_issues(store, &mut issues);
    for agent in store.agents.iter().filter(|agent| agent.detected()) {
        let Some(file) = &agent.mcp else {
            continue;
        };
        if mcp::read(file).is_err() {
            issues.push(format!(
                "{}: cannot read MCP configuration at {}",
                agent.name,
                file.path.display()
            ));
        }
    }
    json!({
        "home": home.display().to_string(),
        "root": store.root.display().to_string(),
        "platform": std::env::consts::OS,
        "version": version,
        "agents": agents,
        "mcp": mcp,
        "skills": skills,
        "foundMcp": found_mcp,
        "foundSkills": found_skills,
        "issues": issues,
    })
}

pub fn mutation_result(
    store: &Store,
    home: &Path,
    version: &str,
    report: &Report,
    message: impl Into<String>,
    log: Option<String>,
) -> Value {
    let snapshot = build_snapshot(store, home, version);
    let mut issues = report.issues.clone();
    if let Some(observed) = snapshot.get("issues").and_then(Value::as_array) {
        for issue in observed.iter().filter_map(Value::as_str) {
            if !issues.iter().any(|existing| existing == issue) {
                issues.push(issue.to_string());
            }
        }
    }
    let mut v = json!({
        "snapshot": snapshot,
        "report": {"changed": report.changed, "issues": issues},
        "message": message.into(),
    });
    if let Some(log) = log {
        v.as_object_mut().unwrap().insert("log".into(), json!(log));
    }
    v
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn malformed_unmanaged_configuration_is_visible_without_echoing_its_contents() {
        let home = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(home.path().join(".codex")).unwrap();
        std::fs::write(
            home.path().join(".codex/config.toml"),
            "API_KEY = \"fixture-secret\n",
        )
        .unwrap();
        let store = Store::open(
            &crate::agents::Env::rooted(home.path()),
            home.path().join(".siu"),
        )
        .unwrap();
        let value = build_snapshot(&store, home.path(), "test");
        assert!(!value["issues"].as_array().unwrap().is_empty());
        assert!(!value["issues"].to_string().contains("fixture-secret"));
    }

    #[test]
    fn mutation_report_includes_observed_configuration_failures() {
        let home = tempfile::tempdir().unwrap();
        std::fs::create_dir_all(home.path().join(".claude")).unwrap();
        let env = crate::agents::Env::rooted(home.path());
        let mut store = Store::open(&env, home.path().join(".siu")).unwrap();
        store
            .add_server(
                Server {
                    name: "unapplied".into(),
                    command: "node".into(),
                    ..Default::default()
                },
                ["claude".to_string()].into_iter().collect(),
                None,
            )
            .unwrap();
        let value = mutation_result(
            &store,
            home.path(),
            "test",
            &Report {
                changed: 0,
                issues: vec![],
            },
            "Updated",
            None,
        );
        assert_eq!(value["snapshot"]["mcp"][0]["states"]["claude"], "error");
        assert!(!value["report"]["issues"].as_array().unwrap().is_empty());
    }
}
