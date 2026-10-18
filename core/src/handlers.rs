//! One function per protocol method. Every handler reopens `Store::open`
//! itself (never shares a cached `Store` across requests) so a read always
//! sees whatever the last writer — this process or the `siu` CLI running
//! alongside it — actually committed.

use std::collections::{BTreeSet, HashMap};

use serde_json::{Value, json};

use crate::cache;
use crate::env;
use crate::github;
use crate::guard;
use crate::library::{Report, Source, Store};
use crate::market;
use crate::protocol;
use crate::scan::Origin;
use crate::server::App;
use crate::skills;
use crate::update;
use crate::update_single;

type HResult = Result<Value, String>;

fn params_str(params: &Value, key: &str) -> Result<String, String> {
    params
        .get(key)
        .and_then(Value::as_str)
        .map(str::to_string)
        .ok_or_else(|| format!("{key} is required"))
}

fn opt_str(params: &Value, key: &str) -> Option<String> {
    params.get(key).and_then(Value::as_str).map(str::to_string)
}

fn parse_string_array(params: &Value, key: &str) -> Result<Vec<String>, String> {
    match params.get(key) {
        None | Some(Value::Null) => Ok(vec![]),
        Some(Value::Array(a)) => a
            .iter()
            .map(|v| {
                v.as_str()
                    .map(str::to_string)
                    .ok_or_else(|| format!("{key} must be an array of strings"))
            })
            .collect(),
        Some(_) => Err(format!("{key} must be an array of strings")),
    }
}

fn open_store() -> Result<(env::Resolved, Store), String> {
    let resolved = env::resolve();
    let store = Store::open(&resolved.env, resolved.root.clone()).map_err(|e| format!("{e:#}"))?;
    Ok((resolved, store))
}

pub fn dispatch(app: &App, method: &str, params: &Value) -> HResult {
    match method {
        "snapshot" => h_snapshot(app),
        "sync" => h_sync(app),
        "save_server" => h_save_server(app, params),
        "set_agents" => h_set_agents(app, params),
        "set_enabled" => h_set_enabled(app, params),
        "delete" => h_delete(app, params),
        "import" => h_import(app, params),
        "scan_skills" => h_scan_skills(app, params),
        "install_skills" => h_install_skills(app, params),
        "release_scan" => h_release_scan(app, params),
        "skill_content" => h_skill_content(params),
        "discover" => h_discover(app, params),
        "install_server" => h_install_server(app, params),
        "update" => h_update(app, params),
        "clear" => h_clear(app, params),
        other => Err(format!("unknown method {other:?}")),
    }
}

/// Methods that change the library or an agent's file: dispatched while
/// `App::write_lock` is held so writes are fully serialized while reads
/// (and network-bound discovery/scans) keep running concurrently.
pub fn mutates(method: &str) -> bool {
    matches!(
        method,
        "sync"
            | "save_server"
            | "set_agents"
            | "set_enabled"
            | "delete"
            | "import"
            | "install_skills"
            | "install_server"
            | "update"
            | "clear"
    )
}

fn h_snapshot(app: &App) -> HResult {
    let (resolved, store) = open_store()?;
    Ok(protocol::build_snapshot(
        &store,
        &resolved.env.home,
        &app.version,
    ))
}

fn prune_if_safe(store: &Store, report: &Report) {
    let pending_links = store.lib.applied.values().any(|applied| {
        applied
            .skills
            .iter()
            .any(|name| store.skill(name).is_none())
    });
    if report.issues.is_empty() && !pending_links {
        store.prune();
    }
}

#[cfg(test)]
#[test]
fn pruning_waits_for_all_saved_link_references() {
    let home = tempfile::tempdir().unwrap();
    let mut store = Store::open(
        &crate::agents::Env::rooted(home.path()),
        home.path().join(".siu"),
    )
    .unwrap();
    let directory = store.skill_dir("retained");
    std::fs::create_dir_all(&directory).unwrap();
    store
        .lib
        .applied
        .entry("future-agent".into())
        .or_default()
        .skills
        .insert("retained".into());
    let report = Report {
        changed: 0,
        issues: vec![],
    };
    prune_if_safe(&store, &report);
    assert!(directory.is_dir());
    store.lib.applied.clear();
    prune_if_safe(&store, &report);
    assert!(!directory.exists());
}

fn h_sync(app: &App) -> HResult {
    let (resolved, mut store) = open_store()?;
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    prune_if_safe(&store, &report);
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        "Synced",
        None,
    ))
}

fn h_save_server(app: &App, params: &Value) -> HResult {
    let server_v = params.get("server").ok_or("server is required")?;
    let server = protocol::decode_server(server_v)?;
    server.validate().map_err(|e| format!("{e:#}"))?;
    let agents = parse_string_array(params, "agents")?;
    let old_name = opt_str(params, "oldName");
    let origin = opt_str(params, "origin");

    let (resolved, mut store) = open_store()?;
    let targets = guard::validate_targets("mcp", &agents, &store, Some(&server))?;
    guard::check_mcp_collision(&store, &server.name, &targets)?;

    match &old_name {
        Some(old) => {
            if store.server(old).is_none() {
                return Err(format!("no server called {old}"));
            }
            store
                .edit_server(old, server.clone())
                .map_err(|e| format!("{e:#}"))?;
            store
                .set_server_agents(&server.name, targets)
                .map_err(|e| format!("{e:#}"))?;
        }
        None => {
            if store.server(&server.name).is_some() {
                return Err(format!(
                    "the library already has a server called {}",
                    server.name
                ));
            }
            store
                .add_server(server.clone(), targets, origin)
                .map_err(|e| format!("{e:#}"))?;
        }
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        format!("Saved {}", server.name),
        None,
    ))
}

fn h_set_agents(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let name = params_str(params, "name")?;
    let agents = parse_string_array(params, "agents")?;
    let (resolved, mut store) = open_store()?;
    let server_for_check = if kind == "mcp" {
        Some(
            store
                .server(&name)
                .ok_or_else(|| format!("no server called {name}"))?
                .server
                .clone(),
        )
    } else {
        None
    };
    let targets = guard::validate_targets(&kind, &agents, &store, server_for_check.as_ref())?;
    match kind.as_str() {
        "mcp" => {
            guard::check_mcp_collision(&store, &name, &targets)?;
            store
                .set_server_agents(&name, targets)
                .map_err(|e| format!("{e:#}"))?;
        }
        "skills" => {
            store
                .set_skill_agents(&name, targets)
                .map_err(|e| format!("{e:#}"))?;
        }
        other => return Err(format!("unknown kind {other:?}")),
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        "Updated agents",
        None,
    ))
}

fn h_set_enabled(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let name = params_str(params, "name")?;
    let enabled = params
        .get("enabled")
        .and_then(Value::as_bool)
        .ok_or("enabled is required")?;
    let (resolved, mut store) = open_store()?;
    match kind.as_str() {
        "mcp" => {
            let current = store
                .server(&name)
                .ok_or_else(|| format!("no server called {name}"))?
                .enabled;
            if current != enabled {
                store
                    .toggle_server_enabled(&name)
                    .map_err(|e| format!("{e:#}"))?;
            }
        }
        "skills" => {
            let current = store
                .skill(&name)
                .ok_or_else(|| format!("no skill called {name}"))?
                .enabled;
            if current != enabled {
                store
                    .toggle_skill_enabled(&name)
                    .map_err(|e| format!("{e:#}"))?;
            }
        }
        other => return Err(format!("unknown kind {other:?}")),
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        "Updated",
        None,
    ))
}

fn h_delete(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let name = params_str(params, "name")?;
    let (resolved, mut store) = open_store()?;
    match kind.as_str() {
        "mcp" => store.remove_server(&name).map_err(|e| format!("{e:#}"))?,
        "skills" => store.remove_skill(&name).map_err(|e| format!("{e:#}"))?,
        other => return Err(format!("unknown kind {other:?}")),
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    // prune the skill's own library folder only once the sync that removed
    // its agent links actually succeeded, so a failed unlink never leaves a
    // dangling link pointing at a folder siu just deleted
    if kind == "skills" {
        prune_if_safe(&store, &report);
    }
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        format!("Deleted {name}"),
        None,
    ))
}

fn h_import(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let name = params_str(params, "name")?;
    let (resolved, mut store) = open_store()?;
    match kind.as_str() {
        "mcp" => {
            let found = store
                .found_servers()
                .into_iter()
                .find(|f| f.server.name == name)
                .ok_or_else(|| format!("{name} isn't an unmanaged MCP server right now"))?;
            store.import_server(&found).map_err(|e| format!("{e:#}"))?;
        }
        "skills" => {
            let found = store
                .found_skills()
                .into_iter()
                .find(|f| f.name == name)
                .ok_or_else(|| format!("{name} isn't an unmanaged skill right now"))?;
            store.import_skill(&found).map_err(|e| format!("{e:#}"))?;
        }
        other => return Err(format!("unknown kind {other:?}")),
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        format!("Imported {name}"),
        None,
    ))
}

fn h_scan_skills(app: &App, params: &Value) -> HResult {
    let source = params_str(params, "source")?;
    let (resolved, store) = open_store()?;
    let spec = github::parse_spec(&source, &resolved.env.home)
        .ok_or("use owner/repo, a github.com URL, or a local path")?;
    match spec {
        github::Spec::Local(dir) => {
            if !dir.is_dir() {
                return Err(format!("{} isn't a folder", dir.display()));
            }
            let canon = std::fs::canonicalize(&dir).unwrap_or(dir);
            let found = skills::find(&canon);
            if found.is_empty() {
                return Err(format!("no SKILL.md under {}", canon.display()));
            }
            let token = app
                .scans
                .insert(source.clone(), canon, Origin::Local, found.clone(), None);
            Ok(protocol::encode_scan_result(
                &token, &source, &found, &store,
            ))
        }
        github::Spec::Github {
            repo,
            reference,
            subpath,
        } => {
            let (guard, root) =
                github::download(&repo, &reference).map_err(|e| format!("{e:#}"))?;
            let found: Vec<_> = skills::find(&root)
                .into_iter()
                .filter(|f| {
                    subpath.is_empty()
                        || f.path == subpath
                        || f.path.starts_with(&format!("{subpath}/"))
                })
                .collect();
            if found.is_empty() {
                return Err(format!(
                    "no SKILL.md found in {repo}{}",
                    if subpath.is_empty() {
                        String::new()
                    } else {
                        format!("/{subpath}")
                    }
                ));
            }
            let token = app.scans.insert(
                source.clone(),
                root,
                Origin::Github { repo, reference },
                found.clone(),
                Some(guard),
            );
            Ok(protocol::encode_scan_result(
                &token, &source, &found, &store,
            ))
        }
    }
}

fn h_install_skills(app: &App, params: &Value) -> HResult {
    let token = params_str(params, "token")?;
    let paths = parse_string_array(params, "paths")?;
    let agents = parse_string_array(params, "agents")?;
    if paths.is_empty() {
        return Err("pick at least one skill".into());
    }

    let (resolved, mut store) = open_store()?;
    let targets = guard::validate_targets("skills", &agents, &store, None)?;

    let selected = app.scans.with(&token, |entry| {
        let mut picked = vec![];
        for p in &paths {
            match entry.found.iter().find(|f| &f.path == p) {
                Some(f) => picked.push(f.clone()),
                None => return Err(format!("{p} isn't part of this scan")),
            }
        }
        Ok::<_, String>((picked, entry.origin.clone()))
    });
    let Some(selected) = selected else {
        return Err("scan token expired or unknown".into());
    };
    let (picked, origin) = selected?;

    let names = store
        .install_skills(
            &picked,
            |f| match &origin {
                Origin::Github { repo, reference } => Some(Source::Github {
                    repo: repo.clone(),
                    reference: reference.clone(),
                    path: f.path.clone(),
                }),
                Origin::Local => Some(Source::Local { dir: f.dir.clone() }),
            },
            &targets,
        )
        .map_err(|e| format!("{e:#}"))?;
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        format!("Installed {}", names.join(", ")),
        None,
    ))
}

fn h_release_scan(app: &App, params: &Value) -> HResult {
    let token = params_str(params, "token")?;
    app.scans.release(&token);
    Ok(json!({}))
}

fn h_skill_content(params: &Value) -> HResult {
    let name = params_str(params, "name")?;
    let (_, store) = open_store()?;
    if store.skill(&name).is_none() {
        return Err(format!("no skill called {name}"));
    }
    let path = store.skill_dir(&name).join("SKILL.md");
    const LIMIT: u64 = 1 << 20;
    let meta = std::fs::metadata(&path).map_err(|e| format!("{e}"))?;
    if meta.len() > LIMIT {
        return Err("SKILL.md is larger than 1 MiB".into());
    }
    let content = std::fs::read_to_string(&path).map_err(|e| format!("{e}"))?;
    Ok(json!({"content": content, "path": path.display().to_string()}))
}

fn h_discover(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let query = params
        .get("query")
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_string();
    let refresh = params
        .get("refresh")
        .and_then(Value::as_bool)
        .unwrap_or(false);
    let resolved = env::resolve();
    let mut warnings: Vec<String> = vec![];

    match kind.as_str() {
        "mcp" => {
            let mut servers = market::featured_matching(&query);
            let mut from_cache = false;
            if !query.trim().is_empty() {
                let (list, problems, cached) = crate::registry_cache::search(
                    &resolved.root.join("cache"),
                    &query,
                    refresh,
                    || market::search_registry(&query),
                );
                servers.extend(list);
                warnings.extend(problems);
                from_cache = cached;
            }
            let mut seen = BTreeSet::new();
            servers.retain(|server| seen.insert(server.id.clone()));
            {
                let mut cache = app.market_cache.lock().unwrap();
                for m in &servers {
                    cache.retain(|(id, _)| id != &m.id);
                    cache.push((m.id.clone(), m.clone()));
                }
                const CAP: usize = 300;
                while cache.len() > CAP {
                    cache.remove(0);
                }
            }
            Ok(json!({
                "servers": servers.iter().map(protocol::encode_market_server).collect::<Vec<_>>(),
                "skills": Vec::<Value>::new(),
                "warnings": warnings,
                "cached": from_cache,
            }))
        }
        "skills" => {
            let cache_dir = resolved.root.join("cache");
            let ttl = std::time::Duration::from_secs(3600);
            let cached_file =
                cache::read::<Vec<market::MarketSkill>>(&cache_dir, "skills-popular", ttl);
            let need_fetch = refresh || cached_file.as_ref().map(|c| !c.fresh).unwrap_or(true);
            let mut index: Vec<market::MarketSkill> = vec![];
            let mut from_cache = false;
            if need_fetch {
                match market::popular_skills() {
                    Ok(list) if !list.is_empty() => {
                        cache::write(&cache_dir, "skills-popular", &list);
                        index = list;
                    }
                    Ok(_) => {
                        warnings.push("skills.sh: its page has no list of skills".into());
                        if let Some(c) = cached_file {
                            index = c.data;
                            from_cache = true;
                        }
                    }
                    Err(e) => {
                        warnings.push(format!("skills.sh: {e:#}"));
                        if let Some(c) = cached_file {
                            index = c.data;
                            from_cache = true;
                        }
                    }
                }
            } else if let Some(c) = cached_file {
                index = c.data;
                from_cache = true;
            }
            let skills = if query.trim().is_empty() {
                market::popular_view(&index)
            } else {
                match market::search_skills(&query) {
                    Ok(remote) => market::merge(remote, &market::search_local(&index, &query, 50)),
                    Err(e) => {
                        warnings.push(format!("skills.sh: {e:#}"));
                        from_cache = true;
                        market::search_local(&index, &query, 50)
                    }
                }
            };
            Ok(json!({
                "servers": Vec::<Value>::new(),
                "skills": skills,
                "warnings": warnings,
                "cached": from_cache,
            }))
        }
        other => Err(format!("unknown kind {other:?}")),
    }
}

fn h_install_server(app: &App, params: &Value) -> HResult {
    let id = params_str(params, "id")?;
    let values_map: HashMap<String, String> = params
        .get("values")
        .and_then(Value::as_object)
        .map(|m| {
            m.iter()
                .filter_map(|(k, v)| v.as_str().map(|s| (k.clone(), s.to_string())))
                .collect()
        })
        .unwrap_or_default();
    let agents = parse_string_array(params, "agents")?;
    let rename = opt_str(params, "name");

    let m = {
        let cached = {
            let cache = app.market_cache.lock().unwrap();
            cache
                .iter()
                .find(|(cid, _)| cid == &id)
                .map(|(_, m)| m.clone())
        };
        cached
            .or_else(|| market::featured().into_iter().find(|m| m.id == id))
            .ok_or_else(|| format!("{id} isn't a known market server; discover it first"))?
    };
    for input in &m.inputs {
        if input.required
            && values_map
                .get(&input.key)
                .map(|v| v.trim().is_empty())
                .unwrap_or(true)
        {
            return Err(format!("{} is required", input.label));
        }
    }
    let (resolved, mut store) = open_store()?;
    let ordered: Vec<String> = m
        .inputs
        .iter()
        .map(|i| values_map.get(&i.key).cloned().unwrap_or_default())
        .collect();
    let mut server = m.fill(&ordered, &resolved.env.home);
    if let Some(n) = &rename {
        server.name = n.clone();
    }
    server.validate().map_err(|e| format!("{e:#}"))?;
    if store.server(&server.name).is_some() {
        return Err(format!(
            "the library already has a server called {}",
            server.name
        ));
    }
    let targets = guard::validate_targets("mcp", &agents, &store, Some(&server))?;
    guard::check_mcp_collision(&store, &server.name, &targets)?;
    store
        .add_server(server.clone(), targets, Some(id.clone()))
        .map_err(|e| format!("{e:#}"))?;
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        format!("Installed {}", server.name),
        None,
    ))
}

fn h_update(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let name = opt_str(params, "name");
    let (resolved, mut store) = open_store()?;
    let mut issues = vec![];
    let log = match (kind.as_str(), &name) {
        ("mcp", Some(n)) => {
            update_single::require_known(&store, "mcp", n).map_err(|e| format!("{e:#}"))?;
            let line =
                update_single::update_one_mcp(&mut store, n).map_err(|e| format!("{e:#}"))?;
            if line.failed {
                issues.push(line.line.clone());
            }
            format!("{n}  {}", line.line)
        }
        ("skills", Some(n)) => {
            update_single::require_known(&store, "skills", n).map_err(|e| format!("{e:#}"))?;
            let line =
                update_single::update_one_skill(&mut store, n).map_err(|e| format!("{e:#}"))?;
            if line.failed {
                issues.push(line.line.clone());
            }
            format!("{n}  {}", line.line)
        }
        ("mcp", None) => {
            let mut buf = vec![];
            let failed = update::update_mcp(&mut store, &update::Net, &mut buf)
                .map_err(|e| format!("{e:#}"))?;
            if failed > 0 {
                issues.push(format!("{failed} MCP server(s) failed to update"));
            }
            String::from_utf8_lossy(&buf).into_owned()
        }
        ("skills", None) => {
            let mut buf = vec![];
            let failed = update::update_skills(&mut store, &update::Net, &mut buf)
                .map_err(|e| format!("{e:#}"))?;
            if failed > 0 {
                issues.push(format!("{failed} skill(s) failed to update"));
            }
            String::from_utf8_lossy(&buf).into_owned()
        }
        (other, _) => return Err(format!("unknown kind {other:?}")),
    };
    let report = Report { changed: 0, issues };
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        "Updated",
        Some(log),
    ))
}

fn h_clear(app: &App, params: &Value) -> HResult {
    let kind = params_str(params, "kind")?;
    let (resolved, mut store) = open_store()?;
    let names: Vec<String> = match kind.as_str() {
        "mcp" => store
            .lib
            .mcp
            .iter()
            .map(|e| e.server.name.clone())
            .collect(),
        "skills" => store.lib.skills.iter().map(|e| e.name.clone()).collect(),
        other => return Err(format!("unknown kind {other:?}")),
    };
    for n in &names {
        match kind.as_str() {
            "mcp" => store.remove_server(n).map_err(|e| format!("{e:#}"))?,
            "skills" => store.remove_skill(n).map_err(|e| format!("{e:#}"))?,
            _ => unreachable!(),
        }
    }
    let report = store.commit().map_err(|e| format!("{e:#}"))?;
    if kind == "skills" {
        prune_if_safe(&store, &report);
    }
    let message = if names.is_empty() {
        format!("No {kind} entries to clear")
    } else {
        format!(
            "Cleared {} {kind} entr{}",
            names.len(),
            if names.len() == 1 { "y" } else { "ies" }
        )
    };
    Ok(protocol::mutation_result(
        &store,
        &resolved.env.home,
        &app.version,
        &report,
        message,
        None,
    ))
}
