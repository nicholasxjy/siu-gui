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
