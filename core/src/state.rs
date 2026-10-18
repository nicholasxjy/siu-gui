//! Per-(entry, agent) state for the Snapshot DTO: `on`, `off`, `paused`,
//! `error`, or `unsupported:<reason>`.
//!
//! `Store::server_state`/`skill_state` (upstream) only say what the
//! library *wants*: whether an agent is picked and the entry enabled.
//! That is not enough to answer the protocol's requirement that `on`
//! "means successfully configured, never runtime health" — a desired "on"
//! that a prior sync failed to write (a partial `commit` failure, a stale
//! applied record, a hand-edited config) must not be reported as success.
//! These functions additionally read the agent's actual parsed MCP
//! definition / Skills symlink and the library's own `applied` record
//! before calling anything "on".

use std::collections::BTreeMap;

use crate::agents::{Agent, McpFormat};
use crate::library::{McpEntry, SkillEntry, State, Store};
use crate::mcp::{self, Server};
use crate::skills;

pub fn mcp_states(store: &Store, e: &McpEntry) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    for agent in &store.agents {
        let state = match store.server_state(e, agent) {
            State::Off | State::Paused if mcp_remains(store, e, agent) => "error".to_string(),
            State::Off => "off".to_string(),
            State::Paused => "paused".to_string(),
            State::Unsupported(reason) => format!("unsupported:{reason}"),
            State::On if !agent.detected() => "unsupported:not detected".to_string(),
            State::On => {
                let applied = store
                    .lib
                    .applied
                    .get(agent.id)
                    .is_some_and(|a| a.mcp.contains(&e.server.name));
                let actual = agent
                    .mcp
                    .as_ref()
                    .and_then(|f| mcp::read(f).ok())
                    .and_then(|m| m.get(&e.server.name).cloned());
                match actual {
                    Some(s)
                        if applied
                            && agent
                                .mcp
                                .as_ref()
                                .is_some_and(|file| same_server(&s, &e.server, file.format)) =>
                    {
                        "on".to_string()
                    }
                    _ => "error".to_string(),
                }
            }
        };
        out.insert(agent.id.to_string(), state);
    }
    out
}

/// Compares what actually matters to the user, not exact struct equality:
/// an encode/decode round trip can normalize details (e.g. a format whose
/// remote shape has no `sse` distinct from `http`).
fn same_server(a: &Server, b: &Server, format: McpFormat) -> bool {
    mcp::encode(format, a) == mcp::encode(format, b)
}

fn mcp_remains(store: &Store, entry: &McpEntry, agent: &Agent) -> bool {
    store
        .lib
        .applied
        .get(agent.id)
        .is_some_and(|applied| applied.mcp.contains(&entry.server.name))
        && agent.mcp.as_ref().is_none_or(|file| {
            mcp::read(file)
                .map(|entries| entries.contains_key(&entry.server.name))
                .unwrap_or(true)
        })
}

fn skill_remains(store: &Store, entry: &SkillEntry, agent: &Agent) -> bool {
    store
        .lib
        .applied
        .get(agent.id)
        .is_some_and(|applied| applied.skills.contains(&entry.name))
        && agent.skills.as_ref().is_none_or(|dir| {
            skills::links_to(&dir.join(&entry.name), &store.skill_dir(&entry.name))
        })
}

pub fn skill_states(store: &Store, e: &SkillEntry) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    for agent in &store.agents {
        let state = match store.skill_state(e, agent) {
            State::Off | State::Paused if skill_remains(store, e, agent) => "error".to_string(),
            State::Off => "off".to_string(),
            State::Paused => "paused".to_string(),
            State::Unsupported(reason) => format!("unsupported:{reason}"),
            State::On if !agent.detected() => "unsupported:not detected".to_string(),
            State::On => {
                let applied = store
                    .lib
                    .applied
                    .get(agent.id)
                    .is_some_and(|a| a.skills.contains(&e.name));
                let linked = agent.skills.as_ref().is_some_and(|dir| {
                    skills::links_to(&dir.join(&e.name), &store.skill_dir(&e.name))
                });
                if applied && linked {
                    "on".to_string()
                } else {
                    "error".to_string()
                }
            }
        };
        out.insert(agent.id.to_string(), state);
    }
    out
}

/// Readable issues for every (entry, agent) pair whose state came out
/// `error` despite being desired on, so a partial failure is visible in
/// `Snapshot.issues` without the caller inspecting every `states` map.
pub fn collect_issues(
    kind: &str,
    name: &str,
    states: &BTreeMap<String, String>,
    agents: &[Agent],
    out: &mut Vec<String>,
) {
    for (id, state) in states {
        if state == "error" {
            let agent_name = agents
                .iter()
                .find(|a| a.id == id)
                .map(|a| a.name.to_string())
                .unwrap_or_else(|| id.clone());
            out.push(format!("{kind} {name}: not applied in {agent_name}"));
        }
    }
}

pub fn collect_orphan_issues(store: &Store, out: &mut Vec<String>) {
    for (id, applied) in &store.lib.applied {
        let agent = store.agents.iter().find(|agent| agent.id == id.as_str());
        let label = agent.map(|agent| agent.name).unwrap_or(id.as_str());
        for name in applied
            .mcp
            .iter()
            .filter(|name| store.server(name).is_none())
        {
            let remains = agent.and_then(|a| a.mcp.as_ref()).is_none_or(|file| {
                mcp::read(file)
                    .map(|entries| entries.contains_key(name))
                    .unwrap_or(true)
            });
            if remains {
                out.push(format!("MCP server {name}: pending removal from {label}"));
            }
        }
        for name in applied
            .skills
            .iter()
            .filter(|name| store.skill(name).is_none())
        {
            let remains = agent
                .and_then(|a| a.skills.as_ref())
                .is_none_or(|dir| skills::links_to(&dir.join(name), &store.skill_dir(name)));
            if remains {
                out.push(format!("Skill {name}: pending removal from {label}"));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::agents::Env;
    use crate::library::Source;
    use crate::mcp::Transport;
    use std::fs;

    fn ctx7() -> Server {
        Server {
            name: "ctx7".into(),
            transport: Transport::Http,
            url: "https://mcp.context7.com/mcp".into(),
            ..Default::default()
        }
    }

    #[test]
    fn credentials_participate_in_configuration_state() {
        for remote in [true, false] {
            let home = tempfile::tempdir().unwrap();
            fs::create_dir_all(home.path().join(".claude")).unwrap();
            let mut store =
                Store::open(&Env::rooted(home.path()), home.path().join(".siu")).unwrap();
            let mut server = ctx7();
            let field = if remote {
                server
                    .headers
                    .insert("Authorization".into(), "Bearer fixture-original".into());
                "headers"
            } else {
                server.transport = Transport::Stdio;
                server.command = "node".into();
                server
                    .env
                    .insert("API_KEY".into(), "fixture-original".into());
                "env"
            };
            let targets = store.default_server_agents(&server);
            store.add_server(server, targets, None).unwrap();
            store.commit().unwrap();
            let path = home.path().join(".claude.json");
            let mut config: serde_json::Value =
                serde_json::from_slice(&fs::read(&path).unwrap()).unwrap();
            let key = if remote { "Authorization" } else { "API_KEY" };
            config["mcpServers"]["ctx7"][field][key] = serde_json::json!("fixture-external-change");
            fs::write(path, serde_json::to_vec(&config).unwrap()).unwrap();
            assert_eq!(
                mcp_states(&store, store.server("ctx7").unwrap())["claude"],
                "error"
            );
        }
    }

    #[test]
    fn pending_pause_detach_and_removal_survive_reopening() {
        let home = tempfile::tempdir().unwrap();
        fs::create_dir_all(home.path().join(".claude")).unwrap();
        let env = Env::rooted(home.path());
        let root = home.path().join(".siu");
        let mut store = Store::open(&env, root.clone()).unwrap();
        let targets = store.default_server_agents(&ctx7());
        store.add_server(ctx7(), targets.clone(), None).unwrap();
        store.commit().unwrap();
        store.toggle_server_enabled("ctx7").unwrap();
        store.save().unwrap();
        let mut store = Store::open(&env, root.clone()).unwrap();
        assert_eq!(
            mcp_states(&store, store.server("ctx7").unwrap())["claude"],
            "error"
        );
        store.commit().unwrap();
        assert_eq!(
            mcp_states(&store, store.server("ctx7").unwrap())["claude"],
            "paused"
        );
        store.toggle_server_enabled("ctx7").unwrap();
        store.commit().unwrap();
        store.set_server_agents("ctx7", Default::default()).unwrap();
        assert_eq!(
            mcp_states(&store, store.server("ctx7").unwrap())["claude"],
            "error"
        );
        store.commit().unwrap();
        assert_eq!(
            mcp_states(&store, store.server("ctx7").unwrap())["claude"],
            "off"
        );
        store.set_server_agents("ctx7", targets).unwrap();
        store.commit().unwrap();
        store.remove_server("ctx7").unwrap();
        store.save().unwrap();
        let mut store = Store::open(&env, root).unwrap();
        let mut issues = vec![];
        collect_orphan_issues(&store, &mut issues);
        assert_eq!(issues.len(), 1);
        store.commit().unwrap();
        let mut issues = vec![];
        collect_orphan_issues(&store, &mut issues);
        assert!(issues.is_empty());
    }

    #[test]
    fn on_requires_the_real_file_and_the_applied_record_to_agree() {
        let d = tempfile::tempdir().unwrap();
        let home = d.path().to_path_buf();
        fs::create_dir_all(home.join(".claude")).unwrap();
        let mut store = Store::open(&Env::rooted(&home), home.join(".siu")).unwrap();
        let agents = store.default_server_agents(&ctx7());
        store.add_server(ctx7(), agents, None).unwrap();

        // not yet committed: the library wants it on, but nothing was ever
        // written, so the real state must be "error", never "on"
        let e = store.server("ctx7").unwrap().clone();
        let states = mcp_states(&store, &e);
        assert_eq!(states["claude"], "error", "{states:?}");

        store.commit().unwrap();
        let e = store.server("ctx7").unwrap().clone();
        let states = mcp_states(&store, &e);
        assert_eq!(states["claude"], "on", "{states:?}");

        // the applied record says claude has it, but someone hand-edited
        // the file to remove it: must read back as "error", not "on"
        fs::write(home.join(".claude.json"), "{}").unwrap();
        let states = mcp_states(&store, &e);
        assert_eq!(states["claude"], "error", "{states:?}");
    }

    #[test]
    fn skill_on_requires_a_real_link_matching_the_applied_record() {
        let d = tempfile::tempdir().unwrap();
        let home = d.path().to_path_buf();
        fs::create_dir_all(home.join(".claude")).unwrap();
        let mut store = Store::open(&Env::rooted(&home), home.join(".siu")).unwrap();
        let tree = home.join("tree");
        let skill_dir = tree.join("pdf");
        fs::create_dir_all(&skill_dir).unwrap();
        fs::write(
            skill_dir.join("SKILL.md"),
            "---\nname: pdf\ndescription: d\n---\n",
        )
        .unwrap();
        let found = crate::skills::find(&tree);
        let agents = store.default_skill_agents();
        store
            .install_skills(
                &found,
                |f| {
                    Some(Source::Github {
                        repo: "a/b".into(),
                        reference: "HEAD".into(),
                        path: f.path.clone(),
                    })
                },
                &agents,
            )
            .unwrap();
        let e = store.skill("pdf").unwrap().clone();
        let states = skill_states(&store, &e);
        assert_eq!(states["claude"], "error", "{states:?}");

        store.commit().unwrap();
        let states = skill_states(&store, &e);
        assert_eq!(states["claude"], "on", "{states:?}");

        store.toggle_skill_enabled("pdf").unwrap();
        assert_eq!(
            skill_states(&store, store.skill("pdf").unwrap())["claude"],
            "error"
        );
        store.commit().unwrap();
        assert_eq!(
            skill_states(&store, store.skill("pdf").unwrap())["claude"],
            "paused"
        );
        store.toggle_skill_enabled("pdf").unwrap();
        store.commit().unwrap();

        // the link got replaced by a real directory behind siu's back
        crate::skills::unlink(&home.join(".claude/skills/pdf")).unwrap();
        fs::create_dir_all(home.join(".claude/skills/pdf")).unwrap();
        let states = skill_states(&store, &e);
        assert_eq!(states["claude"], "error", "{states:?}");
    }
}
