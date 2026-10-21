//! End-to-end protocol tests: spawn the real `siu-core` binary, talk
//! NDJSON to it over its actual stdin/stdout, and only ever point it at a
//! `tempfile::tempdir()` via `SIU_GUI_TEST_HOME`. None of these tests ever
//! read or write anything under the real home directory.

use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, ChildStdout, Command, Stdio};

use serde_json::{Value, json};

struct Proc {
    child: Child,
    stdin: ChildStdin,
    stdout: BufReader<ChildStdout>,
    next_id: u64,
}

impl Proc {
    fn start(home: &Path) -> Self {
        let exe = env!("CARGO_BIN_EXE_siu-core");
        let mut child = Command::new(exe)
            .env("SIU_GUI_TEST_HOME", home)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .expect("spawn siu-core");
        let stdin = child.stdin.take().unwrap();
        let stdout = BufReader::new(child.stdout.take().unwrap());
        Proc {
            child,
            stdin,
            stdout,
            next_id: 1,
        }
    }

    fn call(&mut self, method: &str, params: Value) -> Value {
        let id = self.next_id;
        self.next_id += 1;
        let req = json!({"id": id, "method": method, "params": params});
        let line = serde_json::to_string(&req).unwrap();
        // the request line itself must stay well-formed JSON on our side;
        // more importantly, every reply read back below must parse as
        // JSON too, which is the stdout-is-protocol-only guarantee.
        writeln!(self.stdin, "{line}").expect("write request");
        self.stdin.flush().expect("flush request");
        loop {
            let mut out = String::new();
            let n = self.stdout.read_line(&mut out).expect("read response line");
            assert!(n > 0, "siu-core exited without responding to {method}");
            let v: Value = serde_json::from_str(out.trim_end())
                .unwrap_or_else(|e| panic!("non-JSON line from siu-core: {out:?}: {e}"));
            if v.get("id") == Some(&json!(id)) {
                return v;
            }
        }
    }

    fn ok(&mut self, method: &str, params: Value) -> Value {
        let v = self.call(method, params);
        assert_eq!(v["ok"], json!(true), "{method} failed: {v:#}");
        v["data"].clone()
    }

    fn err(&mut self, method: &str, params: Value) -> String {
        let v = self.call(method, params);
        assert_eq!(v["ok"], json!(false), "{method} unexpectedly ok: {v:#}");
        v["error"].as_str().unwrap().to_string()
    }
}

impl Drop for Proc {
    fn drop(&mut self) {
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

fn claude_json(home: &Path) -> Value {
    serde_json::from_str(&std::fs::read_to_string(home.join(".claude.json")).unwrap()).unwrap()
}

fn mcp_states(entry: &Value) -> &Value {
    &entry["states"]
}

#[test]
fn proxy_settings_default_persist_and_reject_bad_input_without_touching_the_library() {
    let home = tempfile::tempdir().unwrap();
    let mut p = Proc::start(home.path());
    let default = json!({"mode":"system","url":"","noProxy":""});
    assert_eq!(p.ok("proxy_settings", json!({})), default);
    let custom =
        json!({"mode":"custom","url":"socks5h://127.0.0.1:1080","noProxy":"localhost,127.0.0.1"});
    assert_eq!(p.ok("set_proxy_settings", json!({"proxy":custom})), custom);
    let before = p.ok("snapshot", json!({}));
    let error = p.err(
        "set_proxy_settings",
        json!({"proxy":{"mode":"custom","url":"ftp://user:secret@localhost/path","noProxy":""}}),
    );
    assert!(!error.contains("secret"), "{error}");
    assert_eq!(p.ok("proxy_settings", json!({})), custom);
    assert_eq!(p.ok("snapshot", json!({})), before);
    drop(p);
    let mut p = Proc::start(home.path());
    assert_eq!(p.ok("proxy_settings", json!({})), custom);
    let direct = json!({"mode":"direct","url":"","noProxy":""});
    assert_eq!(p.ok("set_proxy_settings", json!({"proxy":direct})), direct);
    assert!(
        p.err("set_proxy_settings", json!({"proxy":{"mode":"unknown"}}))
            .contains("无效")
    );
    assert_eq!(p.ok("proxy_settings", json!({})), direct);
    assert_eq!(
        p.ok("set_proxy_settings", json!({"proxy":default})),
        default
    );
    assert_eq!(p.ok("proxy_settings", json!({})), default);
}

#[test]
fn snapshot_of_a_fresh_home_lists_every_known_agent_and_nothing_else() {
    let d = tempfile::tempdir().unwrap();
    let mut p = Proc::start(d.path());
    let snap = p.ok("snapshot", json!({}));
    assert_eq!(snap["mcp"], json!([]));
    assert_eq!(snap["skills"], json!([]));
    assert_eq!(snap["foundMcp"], json!([]));
    assert_eq!(snap["foundSkills"], json!([]));
    let ids: Vec<&str> = snap["agents"]
        .as_array()
        .unwrap()
        .iter()
        .map(|a| a["id"].as_str().unwrap())
        .collect();
    assert!(ids.contains(&"claude"));
    assert!(ids.contains(&"agents"));
    // nothing is detected in a brand-new, empty test home
    assert!(
        snap["agents"]
            .as_array()
            .unwrap()
            .iter()
            .all(|a| a["detected"] == json!(false))
    );
}

#[test]
fn save_server_writes_one_agent_without_disturbing_unmanaged_json() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    std::fs::write(
        d.path().join(".claude.json"),
        r#"{"numStartups":3,"mcpServers":{"mine":{"type":"stdio","command":"me","args":[],"env":{}}}}"#,
    )
    .unwrap();
    let mut p = Proc::start(d.path());

    let server = json!({
        "name": "ctx7", "transport": "http", "command": "", "args": [],
        "env": {}, "url": "https://mcp.context7.com/mcp", "headers": {},
    });
    let data = p.ok(
        "save_server",
        json!({"server": server, "agents": ["claude"]}),
    );
    assert_eq!(data["report"]["issues"], json!([]));

    let v = claude_json(d.path());
    assert_eq!(v["numStartups"], 3, "unmanaged top-level key must survive");
    assert_eq!(
        v["mcpServers"]["mine"]["command"], "me",
        "unmanaged entry must survive"
    );
    assert_eq!(
        v["mcpServers"]["ctx7"]["url"],
        "https://mcp.context7.com/mcp"
    );

    let snap = p.ok("snapshot", json!({}));
    let entry = snap["mcp"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["name"] == "ctx7")
        .unwrap();
    assert_eq!(mcp_states(entry)["claude"], "on");
}

#[test]
fn an_unmanaged_same_name_server_is_not_silently_overwritten() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    std::fs::write(
        d.path().join(".claude.json"),
        r#"{"mcpServers":{"ctx7":{"type":"stdio","command":"someone-elses","args":[],"env":{}}}}"#,
    )
    .unwrap();
    let mut p = Proc::start(d.path());
    let server = json!({
        "name": "ctx7", "transport": "http", "command": "", "args": [],
        "env": {}, "url": "https://mcp.context7.com/mcp", "headers": {},
    });
    let err = p.err(
        "save_server",
        json!({"server": server, "agents": ["claude"]}),
    );
    assert!(err.contains("ctx7"), "{err}");
    assert!(err.to_lowercase().contains("import"), "{err}");
    // the file was never touched
    assert_eq!(
        claude_json(d.path())["mcpServers"]["ctx7"]["command"],
        "someone-elses"
    );

    // importing it explicitly first is fine, and then editing it is too
    let found = p.ok("snapshot", json!({}));
    assert_eq!(found["foundMcp"].as_array().unwrap().len(), 1);
    p.ok("import", json!({"kind": "mcp", "name": "ctx7"}));
    let data = p.ok(
        "save_server",
        json!({"server": server, "agents": ["claude"], "oldName": "ctx7"}),
    );
    assert_eq!(data["report"]["issues"], json!([]));
    assert_eq!(
        claude_json(d.path())["mcpServers"]["ctx7"]["url"],
        "https://mcp.context7.com/mcp"
    );
}

#[test]
fn codex_toml_comments_and_other_tables_survive_a_save() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".codex")).unwrap();
    std::fs::write(
        d.path().join(".codex/config.toml"),
        "# my config\nmodel = \"gpt-5\"\n\n[profiles.fast]\nmodel = \"x\"\n",
    )
    .unwrap();
    let mut p = Proc::start(d.path());
    let server = json!({
        "name": "git", "transport": "stdio", "command": "uvx", "args": ["mcp-server-git"],
        "env": {}, "url": "", "headers": {},
    });
    p.ok(
        "save_server",
        json!({"server": server, "agents": ["codex"]}),
    );
    let text = std::fs::read_to_string(d.path().join(".codex/config.toml")).unwrap();
    assert!(text.starts_with("# my config\nmodel = \"gpt-5\""), "{text}");
    assert!(text.contains("[profiles.fast]"), "{text}");
    assert!(text.contains("[mcp_servers.git]"), "{text}");
}

#[test]
fn pausing_a_server_removes_it_but_keeps_its_agents_and_resuming_restores_it() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    let mut p = Proc::start(d.path());
    let server = json!({
        "name": "ctx7", "transport": "http", "command": "", "args": [],
        "env": {}, "url": "https://mcp.context7.com/mcp", "headers": {},
    });
    p.ok(
        "save_server",
        json!({"server": server, "agents": ["claude"]}),
    );
    assert!(claude_json(d.path())["mcpServers"].get("ctx7").is_some());

    p.ok(
        "set_enabled",
        json!({"kind": "mcp", "name": "ctx7", "enabled": false}),
    );
    assert!(claude_json(d.path())["mcpServers"].get("ctx7").is_none());
    let snap = p.ok("snapshot", json!({}));
    let entry = snap["mcp"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["name"] == "ctx7")
        .unwrap();
    assert_eq!(entry["enabled"], false);
    assert_eq!(mcp_states(entry)["claude"], "paused");
    // the agent is still picked, just not written while paused
    assert_eq!(entry["agents"], json!(["claude"]));

    p.ok(
        "set_enabled",
        json!({"kind": "mcp", "name": "ctx7", "enabled": true}),
    );
    assert!(claude_json(d.path())["mcpServers"].get("ctx7").is_some());
    let snap = p.ok("snapshot", json!({}));
    let entry = snap["mcp"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["name"] == "ctx7")
        .unwrap();
    assert_eq!(mcp_states(entry)["claude"], "on");
}

fn write_skill(dir: &Path, name: &str, body: &str) {
    std::fs::create_dir_all(dir).unwrap();
    std::fs::write(
        dir.join("SKILL.md"),
        format!("---\nname: {name}\ndescription: about {name}\n---\n{body}"),
    )
    .unwrap();
}

#[test]
fn skills_go_through_scan_install_update_and_delete() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    let tree = d.path().join("tree");
    write_skill(&tree.join("skills/pdf"), "pdf", "v1");
    let mut p = Proc::start(d.path());

    let scan = p.ok(
        "scan_skills",
        json!({"source": tree.join("skills/pdf").display().to_string()}),
    );
    let token = scan["token"].as_str().unwrap().to_string();
    let paths: Vec<String> = scan["skills"]
        .as_array()
        .unwrap()
        .iter()
        .map(|s| s["path"].as_str().unwrap().to_string())
        .collect();
    assert_eq!(paths, [""]);

    // install must reject a path that wasn't part of the scan
    let err = p.err(
        "install_skills",
        json!({"token": token, "paths": ["not-a-real-path"], "agents": ["claude"]}),
    );
    assert!(err.contains("part of this scan"), "{err}");

    let data = p.ok(
        "install_skills",
        json!({"token": token, "paths": paths, "agents": ["claude"]}),
    );
    assert_eq!(data["report"]["issues"], json!([]));
    let link = d.path().join(".claude/skills/pdf");
    assert!(std::fs::symlink_metadata(&link).is_ok());
    assert!(
        std::fs::read_to_string(link.join("SKILL.md"))
            .unwrap()
            .contains("v1")
    );

    let snap = p.ok("snapshot", json!({}));
    let entry = snap["skills"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["name"] == "pdf")
        .unwrap();
    assert_eq!(entry["states"]["claude"], "on");

    // a released or unknown token can no longer be used to install
    let err = p.err(
        "install_skills",
        json!({"token": token, "paths": paths, "agents": ["claude"]}),
    );
    assert!(err.contains("already has a skill"), "{err}");
    let bogus_err = p.err(
        "install_skills",
        json!({"token": "not-a-real-token", "paths": [""], "agents": ["claude"]}),
    );
    assert!(
        bogus_err.contains("expired") || bogus_err.contains("unknown"),
        "{bogus_err}"
    );

    // update refreshes from the same local source
    write_skill(&tree.join("skills/pdf"), "pdf", "v2");
    let data = p.ok("update", json!({"kind": "skills", "name": "pdf"}));
    assert!(
        data["log"].as_str().unwrap().contains("updated"),
        "{data:#}"
    );
    assert!(
        std::fs::read_to_string(link.join("SKILL.md"))
            .unwrap()
            .contains("v2")
    );

    // delete removes the link and prunes the library copy
    let data = p.ok("delete", json!({"kind": "skills", "name": "pdf"}));
    assert_eq!(data["report"]["issues"], json!([]));
    assert!(std::fs::symlink_metadata(&link).is_err());
    assert!(!d.path().join(".siu/skills/pdf").exists());
}

#[test]
fn scan_tokens_can_be_released_before_use() {
    let d = tempfile::tempdir().unwrap();
    let tree = d.path().join("tree");
    write_skill(&tree, "notes", "v1");
    let mut p = Proc::start(d.path());
    let scan = p.ok("scan_skills", json!({"source": tree.display().to_string()}));
    let token = scan["token"].as_str().unwrap().to_string();
    p.ok("release_scan", json!({"token": token}));
    let err = p.err(
        "install_skills",
        json!({"token": token, "paths": [""], "agents": []}),
    );
    assert!(err.contains("expired") || err.contains("unknown"), "{err}");
}

#[test]
fn clear_removes_every_entry_of_a_kind_in_one_commit() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    let mut p = Proc::start(d.path());
    for (name, url) in [
        ("a", "https://a.example.com/mcp"),
        ("b", "https://b.example.com/mcp"),
    ] {
        let server = json!({
            "name": name, "transport": "http", "command": "", "args": [],
            "env": {}, "url": url, "headers": {},
        });
        p.ok(
            "save_server",
            json!({"server": server, "agents": ["claude"]}),
        );
    }
    assert_eq!(
        claude_json(d.path())["mcpServers"]
            .as_object()
            .unwrap()
            .len(),
        2
    );

    let data = p.ok("clear", json!({"kind": "mcp"}));
    assert_eq!(data["snapshot"]["mcp"], json!([]));
    assert_eq!(data["report"]["issues"], json!([]));
    assert_eq!(claude_json(d.path())["mcpServers"], json!({}));

    // clearing again is a no-op, not an error
    let data = p.ok("clear", json!({"kind": "mcp"}));
    assert!(
        data["message"].as_str().unwrap().contains("No mcp"),
        "{data:#}"
    );
}

#[cfg(unix)]
#[test]
fn a_backup_failure_is_reported_as_a_real_error_state_not_a_false_on() {
    use std::os::unix::fs::PermissionsExt;

    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    // a backup is only attempted (and so can only fail) when there is an
    // existing file to back up before the first write touches it
    std::fs::write(d.path().join(".claude.json"), r#"{"mcpServers":{}}"#).unwrap();
    let mut p = Proc::start(d.path());
    let server = json!({
        "name": "ctx7", "transport": "http", "command": "", "args": [],
        "env": {}, "url": "https://mcp.context7.com/mcp", "headers": {},
    });

    // make the backups root unwritable so the first sync's backup must
    // fail before any write to .claude.json is attempted
    std::fs::create_dir_all(d.path().join(".siu/backups")).unwrap();
    std::fs::set_permissions(
        d.path().join(".siu/backups"),
        std::fs::Permissions::from_mode(0o500),
    )
    .unwrap();

    let before = std::fs::read_to_string(d.path().join(".claude.json")).ok();
    let data = p.ok(
        "save_server",
        json!({"server": server, "agents": ["claude"]}),
    );
    std::fs::set_permissions(
        d.path().join(".siu/backups"),
        std::fs::Permissions::from_mode(0o700),
    )
    .unwrap();

    let issues = data["report"]["issues"].as_array().unwrap();
    assert!(
        issues
            .iter()
            .any(|i| i.as_str().unwrap().contains("back up")),
        "{issues:?}"
    );
    // the snapshot must say "error", never a false "on", for the agent
    // the write never actually reached
    let entry = data["snapshot"]["mcp"]
        .as_array()
        .unwrap()
        .iter()
        .find(|e| e["name"] == "ctx7")
        .unwrap();
    assert_eq!(entry["states"]["claude"], "error", "{entry:#}");
    assert!(
        data["snapshot"]["issues"]
            .as_array()
            .unwrap()
            .iter()
            .any(|i| i.as_str().unwrap().contains("Claude Code")),
        "{:#}",
        data["snapshot"]["issues"]
    );
    // the file itself was never touched without a backup of it existing first
    assert_eq!(
        std::fs::read_to_string(d.path().join(".claude.json")).ok(),
        before
    );
}

#[test]
fn unknown_method_and_missing_params_are_reported_as_errors_not_crashes() {
    let d = tempfile::tempdir().unwrap();
    let mut p = Proc::start(d.path());
    let err = p.err("not_a_real_method", json!({}));
    assert!(err.contains("unknown method"), "{err}");
    let err = p.err("save_server", json!({}));
    assert!(err.contains("server"), "{err}");
    // the process must still be responsive after an error
    p.ok("snapshot", json!({}));
}

#[test]
fn concurrent_requests_all_get_their_own_matching_response() {
    let d = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(d.path().join(".claude")).unwrap();
    let mut p = Proc::start(d.path());
    // pipeline several requests before reading any response back, to
    // exercise the one-thread-per-request NDJSON loop rather than a
    // strictly request/response/request/response pattern
    let reqs: Vec<(u64, &str, Value)> = vec![
        (100, "snapshot", json!({})),
        (
            101,
            "save_server",
            json!({"server": {"name":"a","transport":"http","command":"","args":[],"env":{},"url":"https://a.example.com/mcp","headers":{}}, "agents": ["claude"]}),
        ),
        (102, "snapshot", json!({})),
        (103, "discover", json!({"kind": "mcp", "query": ""})),
    ];
    for (id, method, params) in &reqs {
        let line =
            serde_json::to_string(&json!({"id": id, "method": method, "params": params})).unwrap();
        writeln!(p.stdin, "{line}").unwrap();
    }
    p.stdin.flush().unwrap();
    let mut seen = std::collections::HashSet::new();
    for _ in 0..reqs.len() {
        let mut out = String::new();
        p.stdout.read_line(&mut out).unwrap();
        let v: Value = serde_json::from_str(out.trim_end()).unwrap();
        assert_eq!(v["ok"], json!(true), "{v:#}");
        seen.insert(v["id"].as_u64().unwrap());
    }
    assert_eq!(seen, [100, 101, 102, 103].into_iter().collect());
}

/// Scoped so these path-handling assertions don't depend on anything else
/// in the file; a local-path scan must reject a non-existent folder.
#[test]
fn scanning_a_missing_local_folder_is_an_error_not_an_empty_result() {
    let d = tempfile::tempdir().unwrap();
    let mut p = Proc::start(d.path());
    let missing: PathBuf = d.path().join("does-not-exist");
    let err = p.err(
        "scan_skills",
        json!({"source": missing.display().to_string()}),
    );
    assert!(err.contains("isn't a folder"), "{err}");
}

fn config_server(name: &str, command: &str) -> Value {
    json!({
        "name": name, "transport": "stdio", "command": command,
        "args": [], "env": {}, "url": "", "headers": {},
    })
}

fn config_add_server(p: &mut Proc, name: &str, command: &str, agents: &[&str]) {
    let result = p.ok(
        "save_server",
        json!({"server": config_server(name, command), "agents": agents}),
    );
    assert_eq!(result["report"]["issues"], json!([]), "{result:#}");
}

fn config_install_skill(p: &mut Proc, dir: &Path, agents: &[&str]) {
    let scan = p.ok("scan_skills", json!({"source": dir}));
    let paths: Vec<_> = scan["skills"]
        .as_array()
        .unwrap()
        .iter()
        .map(|skill| skill["path"].clone())
        .collect();
    assert!(!paths.is_empty(), "{scan:#}");
    let installed = p.ok(
        "install_skills",
        json!({"token": scan["token"], "paths": paths, "agents": agents}),
    );
    assert_eq!(installed["report"]["issues"], json!([]), "{installed:#}");
}

fn config_entry<'a>(snapshot: &'a Value, kind: &str, name: &str) -> &'a Value {
    snapshot[kind]
        .as_array()
        .unwrap()
        .iter()
        .find(|entry| entry["name"] == name)
        .unwrap_or_else(|| panic!("missing {kind} {name}: {snapshot:#}"))
}

fn config_read_json(path: &Path) -> Value {
    serde_json::from_slice(&std::fs::read(path).unwrap()).unwrap()
}

fn config_write_json(path: &Path, value: &Value) {
    std::fs::write(path, serde_json::to_vec_pretty(value).unwrap()).unwrap();
}

fn config_digest(path: &Path) -> String {
    use sha2::{Digest, Sha256};
    Sha256::digest(std::fs::read(path).unwrap())
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

fn config_file_bytes(skill: &Value, path: &str) -> Vec<u8> {
    use base64::{Engine, engine::general_purpose::STANDARD};
    let file = skill["files"]
        .as_array()
        .unwrap()
        .iter()
        .find(|file| file["path"] == path)
        .unwrap_or_else(|| panic!("missing embedded file {path}: {skill:#}"));
    STANDARD.decode(file["data"].as_str().unwrap()).unwrap()
}

fn config_safety_backups(home: &Path) -> Vec<PathBuf> {
    let root = home.join(".siu").join("backups");
    if !root.exists() {
        return vec![];
    }
    let mut paths: Vec<_> = std::fs::read_dir(root)
        .unwrap()
        .map(|entry| entry.unwrap().path())
        .filter(|path| {
            path.is_file()
                && path
                    .file_name()
                    .unwrap()
                    .to_string_lossy()
                    .starts_with("config-")
                && path.extension().is_some_and(|ext| ext == "json")
        })
        .collect();
    paths.sort();
    paths
}

#[derive(Debug, PartialEq, Eq)]
enum ConfigTreeEntry {
    Directory,
    File(Vec<u8>, u32),
    Link(PathBuf),
}

// Include ownership records, safety backups, links, bytes, and Unix modes so
// a rejected operation cannot quietly mutate any part of the destination.
fn config_tree(home: &Path) -> std::collections::BTreeMap<PathBuf, ConfigTreeEntry> {
    fn visit(
        root: &Path,
        dir: &Path,
        entries: &mut std::collections::BTreeMap<PathBuf, ConfigTreeEntry>,
    ) {
        for entry in std::fs::read_dir(dir).unwrap() {
            let path = entry.unwrap().path();
            let metadata = std::fs::symlink_metadata(&path).unwrap();
            let kind = if metadata.file_type().is_symlink() {
                ConfigTreeEntry::Link(std::fs::read_link(&path).unwrap())
            } else if metadata.is_dir() {
                visit(root, &path, entries);
                ConfigTreeEntry::Directory
            } else {
                #[cfg(unix)]
                let mode = {
                    use std::os::unix::fs::PermissionsExt;
                    metadata.permissions().mode() & 0o777
                };
                #[cfg(not(unix))]
                let mode = 0;
                ConfigTreeEntry::File(std::fs::read(&path).unwrap(), mode)
            };
            entries.insert(path.strip_prefix(root).unwrap().to_path_buf(), kind);
        }
    }
    let mut entries = std::collections::BTreeMap::new();
    visit(home, home, &mut entries);
    entries
}

fn config_assert_rejected_unchanged(
    p: &mut Proc,
    home: &Path,
    method: &str,
    params: Value,
) -> String {
    let snapshot = p.ok("snapshot", json!({}));
    let proxy = p.ok("proxy_settings", json!({}));
    let before = config_tree(home);
    let error = p.err(method, params);
    assert!(!error.is_empty(), "{method} must explain the rejection");
    assert_eq!(config_tree(home), before, "{method}: {error}");
    assert_eq!(p.ok("snapshot", json!({})), snapshot, "{method}: {error}");
    assert_eq!(
        p.ok("proxy_settings", json!({})),
        proxy,
        "{method}: {error}"
    );
    error
}

#[test]
fn config_export_and_preview_embed_binary_files_proxy_theme_and_only_desired_state() {
    let home = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(home.path().join(".claude")).unwrap();
    let skill = home.path().join("sources/portable");
    write_skill(&skill, "portable", "portable skill body");
    std::fs::create_dir_all(skill.join("assets")).unwrap();
    let binary = [0, 255, 128, b'\n', b'\r', 0, 1, 254];
    std::fs::write(skill.join("assets/opaque.bin"), binary).unwrap();
    std::fs::write(skill.join("assets/empty.bin"), []).unwrap();
    let mut p = Proc::start(home.path());
    config_add_server(&mut p, "portable", "portable-command", &["claude"]);
    config_install_skill(&mut p, &skill, &["claude"]);
    let proxy = json!({"mode":"custom","url":"socks5h://127.0.0.1:1080","noProxy":"localhost,.example.test"});
    p.ok("set_proxy_settings", json!({"proxy": proxy}));
    assert!(
        config_read_json(&home.path().join(".siu/library.json"))["applied"]["claude"].is_object()
    );

    for (index, theme) in [None, Some("dark"), Some("light"), Some("system")]
        .into_iter()
        .enumerate()
    {
        let path = exports.path().join(format!("portable-{index}.json"));
        let mut params = json!({"path": path});
        if let Some(theme) = theme {
            params["theme"] = json!(theme);
        }
        let before = config_tree(home.path());
        let result = p.ok("export_config", params);
        assert_eq!(
            result,
            json!({"path": path, "mcpCount": 1, "skillCount": 1})
        );
        assert_eq!(config_tree(home.path()), before, "export must be read-only");
        let backup = config_read_json(&path);
        assert_eq!(backup["format"], "siu-gui-backup");
        assert_eq!(backup["version"], 1);
        assert_eq!(backup["sourceHome"], json!(home.path()));
        assert_eq!(backup["sourceRoot"], json!(home.path().join(".siu")));
        assert_eq!(backup["proxy"], proxy);
        assert_eq!(backup["theme"], json!(theme));
        assert!(backup.get("applied").is_none(), "{backup:#}");
        assert!(backup["skills"][0]["entry"].get("applied").is_none());
        assert_eq!(
            config_file_bytes(&backup["skills"][0], "assets/opaque.bin"),
            binary
        );
        assert_eq!(
            config_file_bytes(&backup["skills"][0], "assets/empty.bin"),
            Vec::<u8>::new()
        );
        assert_eq!(
            config_file_bytes(&backup["skills"][0], "SKILL.md"),
            std::fs::read(skill.join("SKILL.md")).unwrap()
        );
        let preview = p.ok("preview_config", json!({"path": path}));
        assert_eq!(preview["path"], json!(path));
        assert_eq!(preview["mcpCount"], 1);
        assert_eq!(preview["skillCount"], 1);
        assert!(preview["exportedAt"].as_u64().is_some_and(|time| time > 0));
        assert!(!preview["digest"].as_str().unwrap().is_empty());
        assert_eq!(preview["issues"], json!([]));
        assert_eq!(preview["theme"], json!(theme));
        assert_eq!(p.ok("preview_config", json!({"path": path})), preview);
        assert_eq!(
            config_tree(home.path()),
            before,
            "preview must be read-only"
        );
    }
}

#[test]
fn config_import_replaces_managed_entries_retracts_destination_ownership_and_backs_up_current_state()
 {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    for home in [source.path(), destination.path()] {
        std::fs::create_dir_all(home.join(".claude")).unwrap();
    }
    let mut from = Proc::start(source.path());
    for (name, agents) in [
        ("kept", vec!["claude"]),
        ("paused", vec!["claude"]),
        ("unassigned", vec![]),
    ] {
        config_add_server(&mut from, name, "from-backup", &agents);
        let skill = source.path().join("sources").join(name);
        write_skill(&skill, name, "from backup");
        config_install_skill(&mut from, &skill, &agents);
    }
    for kind in ["mcp", "skills"] {
        from.ok(
            "set_enabled",
            json!({"kind": kind, "name": "paused", "enabled": false}),
        );
    }
    let restored_proxy =
        json!({"mode":"custom","url":"http://127.0.0.1:8090","noProxy":"localhost"});
    from.ok("set_proxy_settings", json!({"proxy": restored_proxy}));
    // A stale source-machine ownership record must never travel in a backup.
    let source_library = source.path().join(".siu/library.json");
    let mut library = config_read_json(&source_library);
    library["applied"]["claude"]["mcp"] = json!(["source-owned-only"]);
    library["applied"]["claude"]["skills"] = json!(["source-owned-only"]);
    config_write_json(&source_library, &library);
    let path = exports.path().join("replacement.json");
    from.ok("export_config", json!({"path": path, "theme": "light"}));

    let unmanaged = json!({"type":"stdio","command":"unmanaged","args":[],"env":{}});
    std::fs::write(
        destination.path().join(".claude.json"),
        serde_json::to_vec(&json!({
            "numStartups": 17, "preferences": {"keep": true},
            "mcpServers": {"source-owned-only": unmanaged},
        }))
        .unwrap(),
    )
    .unwrap();
    let unmanaged_skill = destination.path().join(".claude/skills/source-owned-only");
    write_skill(&unmanaged_skill, "source-owned-only", "unmanaged body");
    let unmanaged_bytes = std::fs::read(unmanaged_skill.join("SKILL.md")).unwrap();
    let mut to = Proc::start(destination.path());
    for name in ["kept", "paused", "unassigned", "extra"] {
        config_add_server(&mut to, name, "destination-old", &["claude"]);
        let skill = destination.path().join("sources").join(name);
        write_skill(&skill, name, "destination old skill");
        config_install_skill(&mut to, &skill, &["claude"]);
    }
    let old_proxy = json!({"mode":"direct","url":"","noProxy":""});
    to.ok("set_proxy_settings", json!({"proxy": old_proxy}));
    let previous_library = config_read_json(&destination.path().join(".siu/library.json"));
    let previous_skill =
        std::fs::read(destination.path().join(".siu/skills/kept/SKILL.md")).unwrap();
    let previous_backups = config_safety_backups(destination.path());
    let preview = to.ok("preview_config", json!({"path": path}));
    assert_eq!(preview["issues"], json!([]));
    let result = to.ok(
        "import_config",
        json!({"path": path, "digest": preview["digest"]}),
    );
    assert_eq!(result["theme"], "light");
    assert_eq!(result["report"]["issues"], json!([]), "{result:#}");
    assert!(result["message"].is_string());
    assert!(result["report"]["changed"].as_u64().unwrap() > 0);
    let snapshot = &result["snapshot"];
    assert_eq!(snapshot["mcp"].as_array().unwrap().len(), 3);
    assert_eq!(snapshot["skills"].as_array().unwrap().len(), 3);
    for kind in ["mcp", "skills"] {
        assert_eq!(
            config_entry(snapshot, kind, "kept")["states"]["claude"],
            "on"
        );
        let paused = config_entry(snapshot, kind, "paused");
        assert_eq!(paused["enabled"], false);
        assert_eq!(paused["agents"], json!(["claude"]));
        assert_eq!(paused["states"]["claude"], "paused");
        let unassigned = config_entry(snapshot, kind, "unassigned");
        assert_eq!(unassigned["enabled"], true);
        assert_eq!(unassigned["agents"], json!([]));
        assert_eq!(unassigned["states"]["claude"], "off");
    }
    let actual = claude_json(destination.path());
    assert_eq!(actual["numStartups"], 17);
    assert_eq!(actual["preferences"], json!({"keep": true}));
    assert_eq!(actual["mcpServers"]["kept"]["command"], "from-backup");
    assert_eq!(actual["mcpServers"]["source-owned-only"], unmanaged);
    for name in ["paused", "unassigned", "extra"] {
        assert!(actual["mcpServers"].get(name).is_none(), "{actual:#}");
        assert!(
            std::fs::symlink_metadata(destination.path().join(".claude/skills").join(name))
                .is_err()
        );
    }
    assert!(!destination.path().join(".siu/skills/extra").exists());
    assert_eq!(
        std::fs::read(unmanaged_skill.join("SKILL.md")).unwrap(),
        unmanaged_bytes
    );
    assert!(
        std::fs::symlink_metadata(&unmanaged_skill)
            .unwrap()
            .is_dir()
    );
    let managed = destination.path().join(".siu/skills/kept");
    let linked = destination.path().join(".claude/skills/kept");
    assert_eq!(
        std::fs::canonicalize(&linked).unwrap(),
        std::fs::canonicalize(&managed).unwrap()
    );
    assert!(
        std::fs::read_to_string(linked.join("SKILL.md"))
            .unwrap()
            .contains("from backup")
    );
    let restored_library = config_read_json(&destination.path().join(".siu/library.json"));
    assert_eq!(
        restored_library["applied"]["claude"]["mcp"],
        json!(["kept"])
    );
    assert_eq!(
        restored_library["applied"]["claude"]["skills"],
        json!(["kept"])
    );
    assert_eq!(to.ok("proxy_settings", json!({})), restored_proxy);

    let backups = config_safety_backups(destination.path());
    let added: Vec<_> = backups
        .iter()
        .filter(|path| !previous_backups.contains(path))
        .collect();
    assert_eq!(added.len(), 1, "{backups:?}");
    let safety = config_read_json(added[0]);
    assert_eq!(safety["mcp"], previous_library["mcp"]);
    assert_eq!(safety["skills"].as_array().unwrap().len(), 4);
    assert_eq!(safety["proxy"], old_proxy);
    let old_skill = safety["skills"]
        .as_array()
        .unwrap()
        .iter()
        .find(|skill| skill["entry"]["name"] == "kept")
        .unwrap();
    assert_eq!(config_file_bytes(old_skill, "SKILL.md"), previous_skill);
    assert!(safety.get("applied").is_none());
    assert!(
        result["log"]
            .as_str()
            .unwrap()
            .contains(added[0].to_str().unwrap())
    );
    drop(to);
    let mut to = Proc::start(destination.path());
    assert_eq!(to.ok("proxy_settings", json!({})), restored_proxy);
    assert_eq!(to.ok("snapshot", json!({})), *snapshot);
}

#[test]
fn config_import_rebases_home_and_separate_siu_root_prefixes_without_rewriting_unrelated_strings() {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    for home in [source.path(), destination.path()] {
        std::fs::create_dir_all(home.join(".claude")).unwrap();
    }
    let mut from = Proc::start(source.path());
    config_add_server(&mut from, "paths", "runner", &["claude"]);
    for name in ["home-source", "root-source"] {
        let skill = source.path().join("sources").join(name);
        write_skill(&skill, name, "embedded body");
        config_install_skill(&mut from, &skill, &["claude"]);
    }
    let path = exports.path().join("paths.json");
    from.ok("export_config", json!({"path": path}));
    let mut backup = config_read_json(&path);
    // A real exported document can have SIU_HOME outside the old HOME.
    // Reconstruct that source-machine layout without changing process env.
    let old_home = source
        .path()
        .join("old-home")
        .to_string_lossy()
        .into_owned();
    let old_root = source
        .path()
        .join("separate-siu")
        .to_string_lossy()
        .into_owned();
    backup["sourceHome"] = json!(old_home);
    backup["sourceRoot"] = json!(old_root);
    backup["mcp"][0]["command"] = json!(format!("{old_root}/bin/runner"));
    backup["mcp"][0]["args"] = json!([
        format!("{old_home}/projects/with spaces/资料"),
        format!("{old_root}/skills/home-source"),
        format!("--cache={old_root}/cache"),
        old_home,
        old_root,
        format!("{old_home}-other/keep"),
        format!("prefix:{old_home}/keep"),
        format!("https://example.test/{old_home}/keep"),
    ]);
    backup["mcp"][0]["env"] = json!({
        "HOME_DATA": format!("{old_home}/data"),
        "SIU_DATA": format!("{old_root}/data"),
        "UNCHANGED": format!("{old_root}-other/data"),
    });
    let mut home_command = backup["mcp"][0].clone();
    home_command["name"] = json!("home-command");
    home_command["command"] = json!(format!("{old_home}/bin/runner"));
    backup["mcp"].as_array_mut().unwrap().push(home_command);
    for skill in backup["skills"].as_array_mut().unwrap() {
        let dir = if skill["entry"]["name"] == "home-source" {
            format!("{old_home}/sources/home-source")
        } else {
            format!("{old_root}/sources/root-source")
        };
        skill["entry"]["source"] = json!({"kind":"local","dir":dir});
    }
    config_write_json(&path, &backup);
    let mut to = Proc::start(destination.path());
    let preview = to.ok("preview_config", json!({"path": path}));
    let result = to.ok(
        "import_config",
        json!({"path": path, "digest": preview["digest"]}),
    );
    let server = config_entry(&result["snapshot"], "mcp", "paths");
    assert!(result.get("theme").is_none_or(Value::is_null));
    let home_command = config_entry(&result["snapshot"], "mcp", "home-command");
    assert_eq!(
        home_command["command"],
        json!(destination.path().join("bin").join("runner"))
    );
    assert_eq!(home_command["states"]["claude"], "on");
    assert_eq!(
        server["command"],
        json!(destination.path().join(".siu").join("bin").join("runner"))
    );
    assert_eq!(
        server["args"],
        json!([
            destination
                .path()
                .join("projects")
                .join("with spaces")
                .join("资料"),
            destination
                .path()
                .join(".siu")
                .join("skills")
                .join("home-source"),
            format!(
                "--cache={}",
                destination.path().join(".siu").join("cache").display()
            ),
            destination.path(),
            destination.path().join(".siu"),
            format!("{old_home}-other/keep"),
            format!("prefix:{old_home}/keep"),
            format!("https://example.test/{old_home}/keep"),
        ])
    );
    assert_eq!(
        server["env"],
        json!({
            "HOME_DATA": destination.path().join("data"),
            "SIU_DATA": destination.path().join(".siu").join("data"),
            "UNCHANGED": format!("{old_root}-other/data"),
        })
    );
    assert_eq!(
        claude_json(destination.path())["mcpServers"]["paths"]["command"],
        server["command"]
    );
    for (name, relative) in [
        ("home-source", "sources/home-source"),
        ("root-source", ".siu/sources/root-source"),
    ] {
        let skill = config_entry(&result["snapshot"], "skills", name);
        assert_eq!(
            skill["source"],
            json!({"kind":"local","dir":destination.path().join(relative.split('/').collect::<PathBuf>())})
        );
        assert_eq!(skill["states"]["claude"], "on");
        assert!(
            std::fs::read_to_string(
                destination
                    .path()
                    .join(".claude/skills")
                    .join(name)
                    .join("SKILL.md")
            )
            .unwrap()
            .contains("embedded body")
        );
    }
}

#[test]
fn config_import_retains_missing_known_agent_assignments_and_syncs_when_the_agent_appears() {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(source.path().join(".codex")).unwrap();
    let skill = source.path().join("sources/later");
    write_skill(&skill, "later", "works after installing Codex");
    let mut from = Proc::start(source.path());
    config_add_server(&mut from, "later", "later-command", &["codex"]);
    config_install_skill(&mut from, &skill, &["codex"]);
    let path = exports.path().join("missing-agent.json");
    from.ok("export_config", json!({"path": path}));
    let mut to = Proc::start(destination.path());
    let before = to.ok("snapshot", json!({}));
    let tree = config_tree(destination.path());
    let preview = to.ok("preview_config", json!({"path": path}));
    let issues = preview["issues"].as_array().unwrap();
    assert!(issues.iter().all(Value::is_string));
    assert!(
        issues
            .iter()
            .any(|issue| issue.as_str().unwrap().contains("Codex")),
        "{preview:#}"
    );
    assert_eq!(to.ok("snapshot", json!({})), before);
    assert_eq!(config_tree(destination.path()), tree);
    let result = to.ok(
        "import_config",
        json!({"path": path, "digest": preview["digest"]}),
    );
    assert!(
        result["report"]["issues"]
            .as_array()
            .unwrap()
            .iter()
            .any(|issue| issue.as_str().unwrap().contains("Codex")),
        "{result:#}"
    );
    for kind in ["mcp", "skills"] {
        let entry = config_entry(&result["snapshot"], kind, "later");
        assert_eq!(entry["agents"], json!(["codex"]));
        assert_eq!(entry["states"]["codex"], "unsupported:not detected");
    }
    assert!(!destination.path().join(".codex").exists());
    std::fs::create_dir_all(destination.path().join(".codex")).unwrap();
    let synced = to.ok("sync", json!({}));
    assert_eq!(synced["report"]["issues"], json!([]), "{synced:#}");
    for kind in ["mcp", "skills"] {
        assert_eq!(
            config_entry(&synced["snapshot"], kind, "later")["states"]["codex"],
            "on"
        );
    }
    assert!(
        std::fs::read_to_string(destination.path().join(".codex/config.toml"))
            .unwrap()
            .contains("later-command")
    );
    assert!(
        std::fs::read_to_string(destination.path().join(".codex/skills/later/SKILL.md"))
            .unwrap()
            .contains("works after installing Codex")
    );
}

#[test]
fn config_import_creates_shared_skill_links_when_dot_agents_is_absent() {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(source.path().join(".agents")).unwrap();
    let skill = source.path().join("sources/shared");
    write_skill(&skill, "shared", "shared without an installed agent");
    let mut from = Proc::start(source.path());
    config_install_skill(&mut from, &skill, &["agents"]);
    let path = exports.path().join("shared.json");
    from.ok("export_config", json!({"path": path}));
    let mut to = Proc::start(destination.path());
    assert!(!destination.path().join(".agents").exists());
    let preview = to.ok("preview_config", json!({"path": path}));
    assert_eq!(preview["issues"], json!([]));
    let result = to.ok(
        "import_config",
        json!({"path": path, "digest": preview["digest"]}),
    );
    assert_eq!(result["report"]["issues"], json!([]), "{result:#}");
    let entry = config_entry(&result["snapshot"], "skills", "shared");
    assert_eq!(entry["agents"], json!(["agents"]));
    assert_eq!(entry["states"]["agents"], "on");
    let link = destination.path().join(".agents/skills/shared");
    assert_eq!(
        std::fs::canonicalize(&link).unwrap(),
        std::fs::canonicalize(destination.path().join(".siu/skills/shared")).unwrap()
    );
    assert!(
        std::fs::read_to_string(link.join("SKILL.md"))
            .unwrap()
            .contains("shared without an installed agent")
    );
}

#[cfg(unix)]
#[test]
fn config_import_preserves_binary_assets_and_executable_bits_on_unix() {
    use std::os::unix::fs::PermissionsExt;
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    for home in [source.path(), destination.path()] {
        std::fs::create_dir_all(home.join(".claude")).unwrap();
    }
    let skill = source.path().join("sources/toolbox");
    write_skill(&skill, "toolbox", "binary and executable assets");
    std::fs::create_dir_all(skill.join("scripts")).unwrap();
    std::fs::create_dir_all(skill.join("assets")).unwrap();
    let script = b"#!/bin/sh\nprintf 'portable\\n'\n";
    let binary = [0, 255, 254, 128, 0, b'\r', b'\n', 1, 2, 3];
    std::fs::write(skill.join("scripts/run.sh"), script).unwrap();
    std::fs::set_permissions(
        skill.join("scripts/run.sh"),
        std::fs::Permissions::from_mode(0o751),
    )
    .unwrap();
    std::fs::write(skill.join("assets/data.bin"), binary).unwrap();
    std::fs::set_permissions(
        skill.join("assets/data.bin"),
        std::fs::Permissions::from_mode(0o640),
    )
    .unwrap();
    let mut from = Proc::start(source.path());
    config_install_skill(&mut from, &skill, &["claude"]);
    let path = exports.path().join("binary.json");
    from.ok("export_config", json!({"path": path, "theme": "dark"}));
    let backup = config_read_json(&path);
    let files = backup["skills"][0]["files"].as_array().unwrap();
    assert_eq!(
        files
            .iter()
            .find(|file| file["path"] == "scripts/run.sh")
            .unwrap()["executable"],
        true
    );
    assert_eq!(
        files
            .iter()
            .find(|file| file["path"] == "assets/data.bin")
            .unwrap()["executable"],
        false
    );
    assert_eq!(
        config_file_bytes(&backup["skills"][0], "assets/data.bin"),
        binary
    );
    // Import must use embedded bytes, even when every source copy is gone.
    drop(from);
    source.close().unwrap();
    let mut to = Proc::start(destination.path());
    let preview = to.ok("preview_config", json!({"path": path}));
    let result = to.ok(
        "import_config",
        json!({"path": path, "digest": preview["digest"]}),
    );
    assert_eq!(result["theme"], "dark");
    assert_eq!(result["report"]["issues"], json!([]), "{result:#}");
    for relative in [".siu/skills/toolbox", ".claude/skills/toolbox"] {
        let root = destination.path().join(relative);
        assert_eq!(std::fs::read(root.join("assets/data.bin")).unwrap(), binary);
        assert_eq!(std::fs::read(root.join("scripts/run.sh")).unwrap(), script);
        assert_ne!(
            std::fs::metadata(root.join("scripts/run.sh"))
                .unwrap()
                .permissions()
                .mode()
                & 0o111,
            0
        );
        assert_eq!(
            std::fs::metadata(root.join("assets/data.bin"))
                .unwrap()
                .permissions()
                .mode()
                & 0o111,
            0
        );
        assert!(std::fs::symlink_metadata(&root).is_ok());
    }
    assert!(
        std::fs::symlink_metadata(destination.path().join(".claude/skills/toolbox"))
            .unwrap()
            .file_type()
            .is_symlink()
    );
}

fn config_rejection_fixture() -> (tempfile::TempDir, tempfile::TempDir, Proc, PathBuf, Value) {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    for home in [source.path(), destination.path()] {
        std::fs::create_dir_all(home.join(".claude")).unwrap();
    }
    let mut from = Proc::start(source.path());
    config_add_server(&mut from, "incoming", "incoming-command", &["claude"]);
    let incoming = source.path().join("sources/incoming");
    write_skill(&incoming, "incoming", "incoming backup skill");
    config_install_skill(&mut from, &incoming, &["claude"]);
    from.ok(
        "set_proxy_settings",
        json!({"proxy":{"mode":"custom","url":"http://127.0.0.1:9090","noProxy":"localhost"}}),
    );
    let path = exports.path().join("incoming.json");
    from.ok("export_config", json!({"path":path,"theme":"system"}));
    let backup = config_read_json(&path);
    std::fs::write(
        destination.path().join(".claude.json"),
        r#"{"unmanagedSetting":{"preserve":true},"mcpServers":{"unmanaged":{"type":"stdio","command":"keep-me","args":[],"env":{}}}}"#,
    ).unwrap();
    let mut to = Proc::start(destination.path());
    config_add_server(&mut to, "current", "current-command", &["claude"]);
    let current = destination.path().join("sources/current");
    write_skill(&current, "current", "current destination skill");
    config_install_skill(&mut to, &current, &["claude"]);
    to.ok(
        "set_proxy_settings",
        json!({"proxy":{"mode":"direct","url":"","noProxy":""}}),
    );
    (destination, exports, to, path, backup)
}

fn config_reject_preview_and_import(p: &mut Proc, home: &Path, path: &Path) {
    config_assert_rejected_unchanged(p, home, "preview_config", json!({"path":path}));
    config_assert_rejected_unchanged(
        p,
        home,
        "import_config",
        json!({"path":path,"digest":config_digest(path)}),
    );
}

#[test]
fn config_invalid_format_versions_content_and_settings_fail_before_any_mutation() {
    let (home, _exports, mut p, path, backup) = config_rejection_fixture();
    std::fs::write(&path, b"this is not a JSON backup").unwrap();
    config_reject_preview_and_import(&mut p, home.path(), &path);
    for (field, invalid) in [
        ("format", json!("some-other-backup")),
        ("version", json!(999)),
        ("sourceHome", json!("")),
        ("sourceRoot", json!("")),
        ("theme", json!("neon")),
        (
            "proxy",
            json!({"mode":"custom","url":"ftp://127.0.0.1:21","noProxy":""}),
        ),
    ] {
        let mut invalid_backup = backup.clone();
        invalid_backup[field] = invalid;
        config_write_json(&path, &invalid_backup);
        config_reject_preview_and_import(&mut p, home.path(), &path);
    }
    let mut invalid_backup = backup.clone();
    invalid_backup["skills"][0]["files"][0]["data"] = json!("%%%not-base64%%%");
    config_write_json(&path, &invalid_backup);
    config_reject_preview_and_import(&mut p, home.path(), &path);
    let mut invalid_backup = backup.clone();
    invalid_backup["skills"][0]["files"] = json!([]);
    config_write_json(&path, &invalid_backup);
    config_reject_preview_and_import(&mut p, home.path(), &path);
    let mut invalid_backup = backup.clone();
    invalid_backup["mcp"][0]["command"] = json!("");
    config_write_json(&path, &invalid_backup);
    config_reject_preview_and_import(&mut p, home.path(), &path);
    // A malformed request must leave the process usable for a later restore.
    config_write_json(&path, &backup);
    let preview = p.ok("preview_config", json!({"path":path}));
    let restored = p.ok(
        "import_config",
        json!({"path":path,"digest":preview["digest"]}),
    );
    assert_eq!(
        config_entry(&restored["snapshot"], "mcp", "incoming")["states"]["claude"],
        "on"
    );
}

#[test]
fn config_unsafe_skill_paths_and_conflicting_embedded_files_are_rejected_before_writes() {
    let (home, exports, mut p, path, backup) = config_rejection_fixture();
    let outside = exports.path().join("outside.json");
    std::fs::write(&outside, b"outside resource stays intact").unwrap();
    for unsafe_path in [
        "../escaped.txt".to_string(),
        outside.to_string_lossy().into_owned(),
        "C:\\escape.txt".to_string(),
        "scripts\\..\\escape.txt".to_string(),
        "./escape.txt".to_string(),
        "nested//escape.txt".to_string(),
        "scripts/../../escape.txt".to_string(),
        "NUL.txt".to_string(),
    ] {
        let mut invalid = backup.clone();
        invalid["skills"][0]["files"]
            .as_array_mut()
            .unwrap()
            .push(json!({
                "path": unsafe_path, "data": "AAECAw==", "executable": false,
            }));
        config_write_json(&path, &invalid);
        config_reject_preview_and_import(&mut p, home.path(), &path);
        assert_eq!(
            std::fs::read(&outside).unwrap(),
            b"outside resource stays intact"
        );
    }
    for name in ["../escaped", "nested/skill", "C:\\escaped"] {
        let mut invalid = backup.clone();
        invalid["skills"][0]["entry"]["name"] = json!(name);
        config_write_json(&path, &invalid);
        config_reject_preview_and_import(&mut p, home.path(), &path);
    }
    for extra_files in [
        json!([{"path":"SKILL.md","data":"AA==","executable":false}]),
        json!([
            {"path":"assets","data":"AA==","executable":false},
            {"path":"assets/data.bin","data":"AA==","executable":false},
        ]),
    ] {
        let mut invalid = backup.clone();
        invalid["skills"][0]["files"]
            .as_array_mut()
            .unwrap()
            .extend(extra_files.as_array().unwrap().iter().cloned());
        config_write_json(&path, &invalid);
        config_reject_preview_and_import(&mut p, home.path(), &path);
    }
}

#[test]
fn config_unknown_agents_and_invalid_target_kinds_are_rejected_before_any_mutation() {
    let (home, _exports, mut p, path, backup) = config_rejection_fixture();
    for kind in ["mcp", "skills"] {
        let mut invalid = backup.clone();
        let entry = if kind == "mcp" {
            &mut invalid["mcp"][0]
        } else {
            &mut invalid["skills"][0]["entry"]
        };
        entry["agents"] = json!(["claude", "unknown-source-machine-agent"]);
        config_write_json(&path, &invalid);
        let error = config_assert_rejected_unchanged(
            &mut p,
            home.path(),
            "preview_config",
            json!({"path":path}),
        );
        assert!(error.contains("unknown-source-machine-agent"), "{error}");
        config_assert_rejected_unchanged(
            &mut p,
            home.path(),
            "import_config",
            json!({"path":path,"digest":config_digest(&path)}),
        );
    }
    let mut invalid = backup.clone();
    invalid["mcp"][0]["agents"] = json!(["agents"]);
    config_write_json(&path, &invalid);
    config_reject_preview_and_import(&mut p, home.path(), &path);
    let mut invalid = backup.clone();
    invalid["skills"][0]["entry"]["agents"] = json!(["claude-desktop"]);
    config_write_json(&path, &invalid);
    config_reject_preview_and_import(&mut p, home.path(), &path);
}

#[test]
fn config_unmanaged_mcp_name_collision_blocks_preview_and_import_without_claiming_source_ownership()
{
    let (home, _exports, mut p, path, _) = config_rejection_fixture();
    let mut agent = claude_json(home.path());
    agent["mcpServers"]["incoming"] =
        json!({"type":"stdio","command":"someone-elses-command","args":[],"env":{}});
    config_write_json(&home.path().join(".claude.json"), &agent);
    let error = config_assert_rejected_unchanged(
        &mut p,
        home.path(),
        "preview_config",
        json!({"path":path}),
    );
    assert!(error.contains("incoming"), "{error}");
    let error = config_assert_rejected_unchanged(
        &mut p,
        home.path(),
        "import_config",
        json!({"path":path,"digest":config_digest(&path)}),
    );
    assert!(error.contains("incoming"), "{error}");
    assert_eq!(
        claude_json(home.path())["mcpServers"]["incoming"]["command"],
        "someone-elses-command"
    );
}

#[test]
fn config_changed_digest_or_missing_digest_cannot_replace_the_previewed_configuration() {
    let (home, _exports, mut p, path, _) = config_rejection_fixture();
    let preview = p.ok("preview_config", json!({"path":path}));
    config_assert_rejected_unchanged(&mut p, home.path(), "import_config", json!({"path":path}));
    config_assert_rejected_unchanged(
        &mut p,
        home.path(),
        "import_config",
        json!({"path":path,"digest":"wrong-digest"}),
    );
    // Even a whitespace-only change invalidates confirmation of exact bytes.
    let mut bytes = std::fs::read(&path).unwrap();
    bytes.extend_from_slice(b"\n ");
    std::fs::write(&path, bytes).unwrap();
    config_assert_rejected_unchanged(
        &mut p,
        home.path(),
        "import_config",
        json!({"path":path,"digest":preview["digest"]}),
    );
    let fresh = p.ok("preview_config", json!({"path":path}));
    assert_ne!(fresh["digest"], preview["digest"]);
    assert_eq!(fresh["mcpCount"], preview["mcpCount"]);
    assert_eq!(fresh["skillCount"], preview["skillCount"]);
    let restored = p.ok(
        "import_config",
        json!({"path":path,"digest":fresh["digest"]}),
    );
    assert_eq!(restored["theme"], "system");
    assert_eq!(restored["report"]["issues"], json!([]), "{restored:#}");
    assert_eq!(
        config_entry(&restored["snapshot"], "mcp", "incoming")["states"]["claude"],
        "on"
    );
    assert_eq!(config_safety_backups(home.path()).len(), 1);
}

#[test]
fn config_export_rejects_relative_paths_live_library_agent_files_and_skill_directories() {
    let (home, _exports, mut p, _, _) = config_rejection_fixture();
    let asset = home.path().join(".siu/skills/current/asset.json");
    std::fs::write(&asset, b"live skill asset").unwrap();
    for target in [
        PathBuf::from("relative.json"),
        home.path().join(".siu/library.json"),
        home.path().join(".siu/new-export.json"),
        home.path().join(".claude.json"),
        home.path().join(".claude/skills/current/asset.json"),
    ] {
        config_assert_rejected_unchanged(
            &mut p,
            home.path(),
            "export_config",
            json!({"path":target}),
        );
    }
    for method in ["preview_config", "import_config"] {
        config_assert_rejected_unchanged(
            &mut p,
            home.path(),
            method,
            json!({"path":"relative.json","digest":"unused"}),
        );
    }
}

// Exported by unmodified siu 1a3a7ef. Only source paths, timestamp and
// platform are normalized; the fixture deliberately has no GUI theme.
const SIU_BACKUP_V1: &str = include_str!("fixtures/siu-backup-v1.json");

#[test]
fn config_cli_fixture_restores_and_reexports_without_losing_portable_state() {
    let home = tempfile::tempdir().unwrap();
    let exports = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(home.path().join(".claude")).unwrap();
    let path = exports.path().join("cli.json");
    std::fs::write(&path, SIU_BACKUP_V1).unwrap();
    let mut p = Proc::start(home.path());
    let preview = p.ok("preview_config", json!({"path":path}));
    assert_eq!(preview["mcpCount"], 2);
    assert_eq!(preview["skillCount"], 1);
    assert!(preview["theme"].is_null());
    assert_eq!(preview["issues"], json!([]));
    let restored = p.ok(
        "import_config",
        json!({"path":path,"digest":preview["digest"]}),
    );
    assert!(restored["theme"].is_null());
    assert_eq!(restored["report"]["issues"], json!([]));
    let snapshot = &restored["snapshot"];
    assert_eq!(
        config_entry(snapshot, "mcp", "paused")["states"]["claude"],
        "paused"
    );
    assert_eq!(
        config_entry(snapshot, "mcp", "portable")["states"]["claude"],
        "on"
    );
    let skill = config_entry(snapshot, "skills", "portable");
    for agent in ["claude", "agents"] {
        assert_eq!(skill["states"][agent], "on");
    }
    assert_eq!(
        skill["source"]["dir"],
        json!(home.path().join("sources").join("portable"))
    );
    assert_eq!(
        std::fs::read(home.path().join(".siu/skills/portable/assets/opaque.bin")).unwrap(),
        [0, 255, 128, 10, 13, 0]
    );
    let source: Value = serde_json::from_str(SIU_BACKUP_V1).unwrap();
    assert_eq!(p.ok("proxy_settings", json!({})), source["proxy"]);

    let roundtrip = exports.path().join("gui.json");
    p.ok("export_config", json!({"path":roundtrip,"theme":"dark"}));
    let output = config_read_json(&roundtrip);
    assert_eq!(output["format"], source["format"]);
    assert_eq!(output["version"], source["version"]);
    assert_eq!(output["theme"], "dark");
    assert_eq!(output["proxy"], source["proxy"]);
    assert!(output.get("applied").is_none());
    for (original, exported) in source["mcp"]
        .as_array()
        .unwrap()
        .iter()
        .zip(output["mcp"].as_array().unwrap())
    {
        assert_eq!(exported["name"], original["name"]);
        assert_eq!(exported["enabled"], original["enabled"]);
        assert_eq!(exported["agents"], original["agents"]);
        assert_eq!(exported["origin"], original["origin"]);
        assert_eq!(exported["env"]["TOKEN"], original["env"]["TOKEN"]);
        assert_eq!(exported["env"]["FILE"], json!(home.path().join("data")));
        assert_eq!(
            exported["args"][1],
            format!(
                "--data={}",
                home.path()
                    .join(".siu")
                    .join("skills")
                    .join("portable")
                    .display()
            )
        );
    }
    for file in source["skills"][0]["files"].as_array().unwrap() {
        let name = file["path"].as_str().unwrap();
        assert_eq!(
            config_file_bytes(&output["skills"][0], name),
            config_file_bytes(&source["skills"][0], name)
        );
        #[cfg(unix)]
        assert_eq!(
            output["skills"][0]["files"]
                .as_array()
                .unwrap()
                .iter()
                .find(|f| f["path"] == name)
                .unwrap()["executable"],
            file["executable"]
        );
    }
}

#[test]
fn default_config_honors_a_theme_free_cli_receipt_and_preserves_local_edits() {
    let home = tempfile::tempdir().unwrap();
    let mut p = Proc::start(home.path());
    config_add_server(&mut p, "local", "local-runner", &[]);
    let config = home.path().join(".siu/config.json");
    std::fs::write(&config, SIU_BACKUP_V1).unwrap();
    let receipt = json!({"digest":config_digest(&config)});
    config_write_json(&home.path().join(".siu/config.applied.json"), &receipt);
    let snapshot = p.ok("snapshot", json!({}));
    assert_eq!(snapshot["mcp"].as_array().unwrap().len(), 1);
    assert_eq!(snapshot["mcp"][0]["name"], "local");
    assert_eq!(snapshot["autoConfig"], receipt);
    assert!(config_safety_backups(home.path()).is_empty());
    drop(p);
    let mut p = Proc::start(home.path());
    assert_eq!(p.ok("snapshot", json!({})), snapshot);
}

#[test]
fn default_config_export_is_unapplied_until_restore_and_explicit_import_records_it() {
    let home = tempfile::tempdir().unwrap();
    let mut p = Proc::start(home.path());
    config_add_server(&mut p, "portable", "portable-runner", &[]);
    let config = home.path().join(".siu/config.json");
    p.ok("export_config", json!({"path":config}));
    assert!(!home.path().join(".siu/config.applied.json").exists());
    assert!(config_safety_backups(home.path()).is_empty());
    let preview = p.ok("preview_config", json!({"path":config}));
    p.ok(
        "import_config",
        json!({"path":config,"digest":preview["digest"]}),
    );
    assert_eq!(
        config_read_json(&home.path().join(".siu/config.applied.json")),
        json!({"digest":preview["digest"]})
    );
    p.ok(
        "set_enabled",
        json!({"kind":"mcp","name":"portable","enabled":false}),
    );
    let snapshot = p.ok("snapshot", json!({}));
    assert_eq!(snapshot["mcp"][0]["enabled"], false);
    assert_eq!(config_safety_backups(home.path()).len(), 1);
}

#[test]
fn default_config_receipt_write_failure_preserves_the_restore_report_and_retries() {
    let home = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(home.path().join(".claude")).unwrap();
    let mut p = Proc::start(home.path());
    config_add_server(&mut p, "previous", "previous-runner", &[]);
    let config = home.path().join(".siu/config.json");
    let mut backup: Value = serde_json::from_str(SIU_BACKUP_V1).unwrap();
    backup["theme"] = json!("dark");
    config_write_json(&config, &backup);
    // Block only the receipt's atomic write, after the library and links
    // have been restored, without relying on OS permissions or timing.
    let blocked = home.path().join(".siu/config.applied.json.siu-tmp");
    std::fs::create_dir(&blocked).unwrap();
    let restored = p.ok("snapshot", json!({}));
    assert_eq!(restored["autoConfig"]["theme"], "dark");
    assert_eq!(restored["autoConfig"]["digest"], config_digest(&config));
    assert_eq!(
        config_entry(&restored, "mcp", "portable")["states"]["claude"],
        "on"
    );
    assert!(restored["issues"].as_array().unwrap().iter().any(|i| {
        i.as_str()
            .unwrap()
            .contains("couldn't note config.json as applied")
    }));
    assert!(!home.path().join(".siu/config.applied.json").exists());
    assert_eq!(config_safety_backups(home.path()).len(), 1);
    std::fs::remove_dir(blocked).unwrap();
    let retried = p.ok("snapshot", json!({}));
    assert_eq!(retried["issues"], json!([]));
    assert_eq!(
        config_read_json(&home.path().join(".siu/config.applied.json")),
        retried["autoConfig"]
    );
    assert_eq!(config_safety_backups(home.path()).len(), 2);
}

#[cfg(unix)]
#[test]
fn config_export_protects_missing_agent_config_through_a_symlinked_home() {
    use std::os::unix::fs::symlink;
    let home = tempfile::tempdir().unwrap();
    let aliases = tempfile::tempdir().unwrap();
    let alias = aliases.path().join("home");
    symlink(home.path(), &alias).unwrap();
    let mut p = Proc::start(&alias);
    let target = home.path().join(".claude.json");
    assert!(!target.exists());
    p.err("export_config", json!({"path":target}));
    assert!(!target.exists());
}

#[test]
fn default_config_restores_on_snapshot_once_and_reapplies_changed_contents() {
    let source = tempfile::tempdir().unwrap();
    let destination = tempfile::tempdir().unwrap();
    for home in [source.path(), destination.path()] {
        std::fs::create_dir_all(home.join(".claude")).unwrap();
    }
    let mut from = Proc::start(source.path());
    config_add_server(&mut from, "portable", "portable-runner", &["claude"]);
    let skill = source.path().join("sources/portable");
    write_skill(&skill, "portable", "embedded default configuration");
    config_install_skill(&mut from, &skill, &["claude"]);
    let exported = source.path().join(".siu/config.json");
    from.ok("export_config", json!({"path":exported,"theme":"dark"}));

    let mut to = Proc::start(destination.path());
    config_add_server(&mut to, "previous", "previous-runner", &["claude"]);
    let config = destination.path().join(".siu/config.json");
    std::fs::copy(&exported, &config).unwrap();
    let first = to.ok("snapshot", json!({}));
    assert_eq!(first["mcp"].as_array().unwrap().len(), 1);
    assert_eq!(
        config_entry(&first, "mcp", "portable")["states"]["claude"],
        "on"
    );
    assert_eq!(
        config_entry(&first, "skills", "portable")["states"]["claude"],
        "on"
    );
    assert_eq!(first["autoConfig"]["theme"], "dark");
    assert_eq!(first["autoConfig"]["digest"], config_digest(&config));
    assert!(
        std::fs::read_to_string(destination.path().join(".claude/skills/portable/SKILL.md"))
            .unwrap()
            .contains("embedded default configuration")
    );
    assert!(
        claude_json(destination.path())["mcpServers"]
            .get("previous")
            .is_none()
    );
    let backups = config_safety_backups(destination.path());
    assert_eq!(backups.len(), 1);
    assert_eq!(config_read_json(&backups[0])["mcp"][0]["name"], "previous");
    assert_eq!(
        config_read_json(&destination.path().join(".siu/config.applied.json")),
        first["autoConfig"]
    );

    // Local changes survive refresh and a new backend process while config
    // bytes stay identical, without accumulating redundant safety backups.
    to.ok(
        "set_enabled",
        json!({"kind":"mcp","name":"portable","enabled":false}),
    );
    let paused = to.ok("snapshot", json!({}));
    assert_eq!(
        config_entry(&paused, "mcp", "portable")["states"]["claude"],
        "paused"
    );
    drop(to);
    let mut to = Proc::start(destination.path());
    assert_eq!(to.ok("snapshot", json!({})), paused);
    assert_eq!(config_safety_backups(destination.path()), backups);

    let mut changed = config_read_json(&config);
    changed["mcp"][0]["command"] = json!("changed-runner");
    changed["theme"] = json!("light");
    config_write_json(&config, &changed);
    let reapplied = to.ok("snapshot", json!({}));
    assert_eq!(
        config_entry(&reapplied, "mcp", "portable")["command"],
        "changed-runner"
    );
    assert_eq!(
        config_entry(&reapplied, "mcp", "portable")["states"]["claude"],
        "on"
    );
    assert_eq!(reapplied["autoConfig"]["theme"], "light");
    assert_ne!(
        reapplied["autoConfig"]["digest"],
        first["autoConfig"]["digest"]
    );
    assert_eq!(config_safety_backups(destination.path()).len(), 2);
    std::fs::remove_file(&config).unwrap();
    let removed = to.ok("snapshot", json!({}));
    assert!(removed.get("autoConfig").is_none());
    assert_eq!(removed["mcp"], reapplied["mcp"]);
}

#[test]
fn invalid_default_config_reports_an_issue_and_preserves_existing_configuration() {
    let home = tempfile::tempdir().unwrap();
    std::fs::create_dir_all(home.path().join(".claude")).unwrap();
    let mut p = Proc::start(home.path());
    config_add_server(&mut p, "current", "current-runner", &["claude"]);
    let before = p.ok("snapshot", json!({}));
    let library = std::fs::read(home.path().join(".siu/library.json")).unwrap();
    let agent = std::fs::read(home.path().join(".claude.json")).unwrap();
    let config = home.path().join(".siu/config.json");
    std::fs::write(&config, b"invalid JSON with private-value").unwrap();
    let invalid = p.ok("snapshot", json!({}));
    assert_eq!(invalid["mcp"], before["mcp"]);
    assert!(
        invalid["issues"]
            .as_array()
            .unwrap()
            .iter()
            .any(|s| s.as_str().unwrap().contains("config.json 自动恢复失败"))
    );
    assert!(!invalid.to_string().contains("private-value"));
    assert_eq!(
        std::fs::read(home.path().join(".siu/library.json")).unwrap(),
        library
    );
    assert_eq!(
        std::fs::read(home.path().join(".claude.json")).unwrap(),
        agent
    );
    assert!(!home.path().join(".siu/config.applied.json").exists());
    assert!(config_safety_backups(home.path()).is_empty());
    std::fs::remove_file(&config).unwrap();
    assert_eq!(p.ok("snapshot", json!({})), before);
}

#[test]
fn config_export_rejects_invalid_theme_and_non_json_destinations() {
    let (home, exports, mut p, _, _) = config_rejection_fixture();
    let path = exports.path().join("invalid-theme.json");
    config_assert_rejected_unchanged(
        &mut p,
        home.path(),
        "export_config",
        json!({"path":path,"theme":"neon"}),
    );
    assert!(!path.exists());
    let path = exports.path().join("not-json.txt");
    config_assert_rejected_unchanged(&mut p, home.path(), "export_config", json!({"path":path}));
    assert!(!path.exists());
}

#[cfg(unix)]
fn config_assert_symlink_export_rejected(relative_target: &str) {
    use std::os::unix::fs::symlink;
    let (home, exports, mut p, _, _) = config_rejection_fixture();
    let target = home.path().join(relative_target);
    if !target.exists() {
        std::fs::create_dir_all(target.parent().unwrap()).unwrap();
        std::fs::write(&target, b"live skill JSON asset").unwrap();
    }
    let before = std::fs::read(&target).unwrap();
    let alias = exports.path().join("outside-export.json");
    symlink(&target, &alias).unwrap();
    config_assert_rejected_unchanged(&mut p, home.path(), "export_config", json!({"path":alias}));
    assert_eq!(std::fs::read(&target).unwrap(), before);
    assert_eq!(std::fs::read_link(&alias).unwrap(), target);
}

#[cfg(unix)]
#[test]
fn config_symlink_export_destination_cannot_overwrite_the_live_library() {
    config_assert_symlink_export_rejected(".siu/library.json");
}

#[cfg(unix)]
#[test]
fn config_symlink_export_destination_cannot_overwrite_an_agent_mcp_file() {
    config_assert_symlink_export_rejected(".claude.json");
}

#[cfg(unix)]
#[test]
fn config_symlink_export_destination_cannot_overwrite_an_agent_skill_asset() {
    config_assert_symlink_export_rejected(".claude/skills/current/asset.json");
}

#[cfg(unix)]
#[test]
fn config_export_rejects_symlinked_parent_directories_into_the_library_and_agent_skills() {
    use std::os::unix::fs::symlink;
    let (home, exports, mut p, _, _) = config_rejection_fixture();
    for (index, target) in [".siu", ".claude/skills"].into_iter().enumerate() {
        let alias = exports.path().join(format!("directory-alias-{index}"));
        symlink(home.path().join(target), &alias).unwrap();
        config_assert_rejected_unchanged(
            &mut p,
            home.path(),
            "export_config",
            json!({"path":alias.join("export.json")}),
        );
    }
}
