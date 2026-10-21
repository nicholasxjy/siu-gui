//! Portable, versioned backups. Only desired state travels between machines;
//! applied ownership records and links are always rebuilt on the destination.

use std::collections::BTreeSet;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result, bail, ensure};
use base64::{Engine, engine::general_purpose::STANDARD};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};

use crate::library::{McpEntry, SkillEntry, Source, Store};
use crate::{guard, library, mcp, protocol, proxy, skills};

const MAX_BACKUP_BYTES: u64 = 128 * 1024 * 1024;
const MAX_CONTENT_BYTES: usize = 64 * 1024 * 1024;
const MAX_FILES: usize = 20_000;

#[derive(Clone, Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
pub struct AppliedConfig {
    pub digest: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub theme: Option<String>,
}

fn read_applied_config(root: &Path) -> Result<Option<AppliedConfig>> {
    let path = root.join("config.applied.json");
    let bytes = match read_bounded(&path, 4096) {
        Ok(bytes) => bytes,
        Err(error)
            if error
                .downcast_ref::<std::io::Error>()
                .is_some_and(|e| e.kind() == std::io::ErrorKind::NotFound) =>
        {
            return Ok(None);
        }
        Err(error) => return Err(error).context("无法读取 config.applied.json"),
    };
    let applied: AppliedConfig = serde_json::from_slice(&bytes)
        .context("config.applied.json 无效，请移走该记录后重新加载配置")?;
    valid_theme(applied.theme.as_deref())?;
    Ok(Some(applied))
}

/// config.json is an explicitly supplied restore request. Only a changed
/// document applies again; subsequent snapshots remain ordinary reads.
/// Acquire the mutation lock only for a pending restore, preserving concurrent
/// snapshots while unrelated long-running updates are in progress.
pub fn apply_default(
    app: &crate::server::App,
    resolved: &crate::env::Resolved,
) -> Result<(Option<AppliedConfig>, Vec<String>)> {
    let path = resolved.root.join("config.json");
    let bytes = match read_bounded(&path, MAX_BACKUP_BYTES) {
        Ok(bytes) => bytes,
        Err(error)
            if error
                .downcast_ref::<std::io::Error>()
                .is_some_and(|e| e.kind() == std::io::ErrorKind::NotFound) =>
        {
            return Ok((None, vec![]));
        }
        Err(error) => return Err(error).context("无法读取 config.json"),
    };
    let current_digest = digest(&bytes);
    if let Some(applied) = read_applied_config(&resolved.root)?
        && applied.digest == current_digest
    {
        return Ok((Some(applied), vec![]));
    }
    let _guard = app.write_lock.lock().unwrap_or_else(|p| p.into_inner());
    let (backup, current_digest) = load(&path)?;
    if let Some(applied) = read_applied_config(&resolved.root)?
        && applied.digest == current_digest
    {
        return Ok((Some(applied), vec![]));
    }
    let mut store = Store::open(&resolved.env, resolved.root.clone())?;
    let result = import(
        &mut store,
        &resolved.env.home,
        &app.version,
        &path,
        &current_digest,
    )?;
    let issues = result["report"]["issues"]
        .as_array()
        .into_iter()
        .flatten()
        .filter_map(Value::as_str)
        .map(str::to_owned)
        .collect();
    Ok((
        Some(AppliedConfig {
            digest: current_digest,
            theme: backup.theme,
        }),
        issues,
    ))
}

#[derive(Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Backup {
    format: String,
    version: u32,
    exported_at: u64,
    source_home: String,
    source_root: String,
    platform: String,
    mcp: Vec<McpEntry>,
    skills: Vec<SavedSkill>,
    proxy: proxy::Settings,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    theme: Option<String>,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct SavedSkill {
    entry: SkillEntry,
    files: Vec<SavedFile>,
}

#[derive(Serialize, Deserialize)]
#[serde(deny_unknown_fields)]
struct SavedFile {
    path: String,
    /// Binary-safe, including scripts and non-text resources.
    data: String,
    executable: bool,
}

fn valid_theme(theme: Option<&str>) -> Result<()> {
    ensure!(
        theme.is_none_or(|t| matches!(t, "system" | "light" | "dark")),
        "备份中的界面主题无效"
    );
    Ok(())
}

/// Validate independently of the current OS: a JSON created on Unix must
/// not become an absolute path, ADS, or device name when opened on Windows.
fn relative_path(path: &str) -> Result<PathBuf> {
    ensure!(!path.is_empty() && path.len() <= 4096, "备份文件路径无效");
    let mut out = PathBuf::new();
    for part in path.split('/') {
        let stem = part.split('.').next().unwrap_or("").to_ascii_uppercase();
        ensure!(
            !part.is_empty()
                && part != "."
                && part != ".."
                && !part.ends_with(['.', ' '])
                && !part
                    .chars()
                    .any(|c| c.is_control() || "\\:<>\"|?*".contains(c))
                && !matches!(stem.as_str(), "CON" | "PRN" | "AUX" | "NUL")
                && !(stem.len() == 4
                    && (stem.starts_with("COM") || stem.starts_with("LPT"))
                    && matches!(stem.as_bytes()[3], b'1'..=b'9')),
            "备份包含不支持的文件路径"
        );
        out.push(part);
    }
    ensure!(out.components().count() <= 64, "备份目录层级过深");
    Ok(out)
}

fn read_bounded(path: &Path, limit: u64) -> Result<Vec<u8>> {
    let mut bytes = Vec::new();
    fs::File::open(path)?
        .take(limit + 1)
        .read_to_end(&mut bytes)?;
    ensure!(
        bytes.len() as u64 <= limit,
        "配置备份或 Skills 文件超过大小限制"
    );
    Ok(bytes)
}

fn collect_files(
    root: &Path,
    dir: &Path,
    files: &mut Vec<SavedFile>,
    count: &mut usize,
    size: &mut usize,
    depth: usize,
) -> Result<()> {
    ensure!(depth <= 64, "Skills 目录层级过深");
    let mut entries = fs::read_dir(dir)?.collect::<std::io::Result<Vec<_>>>()?;
    entries.sort_by_key(|e| e.file_name());
    for entry in entries {
        if entry.file_name() == ".git" {
            continue;
        }
        let path = entry.path();
        let rel = path
            .strip_prefix(root)?
            .to_str()
            .context("Skills 文件名不是 UTF-8")?
            .replace(std::path::MAIN_SEPARATOR, "/");
        relative_path(&rel)?;
        let kind = entry.file_type()?;
        if kind.is_dir() {
            collect_files(root, &path, files, count, size, depth + 1)?;
        } else {
            if kind.is_symlink() {
                let target = fs::canonicalize(&path)?;
                ensure!(
                    target.starts_with(root) && target.is_file(),
                    "Skills 含有外部或目录符号链接，无法完整导出"
                );
            } else {
                ensure!(kind.is_file(), "Skills 含有无法导出的特殊文件");
            }
            *count += 1;
            ensure!(*count <= MAX_FILES, "Skills 文件数量超过 {MAX_FILES}");
            let bytes = read_bounded(&path, (MAX_CONTENT_BYTES - *size) as u64)?;
            *size += bytes.len();
            #[cfg(unix)]
            let executable = {
                use std::os::unix::fs::PermissionsExt;
                fs::metadata(&path)?.permissions().mode() & 0o111 != 0
            };
            #[cfg(not(unix))]
            let executable = false;
            files.push(SavedFile {
                path: rel,
                data: STANDARD.encode(bytes),
                executable,
            });
        }
    }
    Ok(())
}

fn capture(store: &Store, home: &Path, theme: Option<String>) -> Result<Backup> {
    valid_theme(theme.as_deref())?;
    let mut saved = Vec::new();
    let (mut count, mut size) = (0, 0);
    for entry in &store.lib.skills {
        mcp::valid_name(&entry.name)?;
        relative_path(&entry.name)?;
        let root = fs::canonicalize(store.skill_dir(&entry.name))
            .with_context(|| format!("无法备份 Skill {}", entry.name))?;
        let mut files = Vec::new();
        collect_files(&root, &root, &mut files, &mut count, &mut size, 0)?;
        ensure!(
            files.iter().any(|f| f.path == "SKILL.md"),
            "Skill {} 缺少 SKILL.md",
            entry.name
        );
        saved.push(SavedSkill {
            entry: entry.clone(),
            files,
        });
    }
    Ok(Backup {
        format: "siu-gui-backup".into(),
        version: 1,
        exported_at: library::now(),
        source_home: home.to_str().context("用户目录不是 UTF-8")?.into(),
        source_root: store.root.to_str().context("资源库目录不是 UTF-8")?.into(),
        platform: std::env::consts::OS.into(),
        mcp: store.lib.mcp.clone(),
        skills: saved,
        proxy: proxy::load(&store.root)?,
        theme,
    })
}

fn encode(backup: &Backup) -> Result<Vec<u8>> {
    let bytes = serde_json::to_vec_pretty(backup)?;
    ensure!(
        bytes.len() as u64 <= MAX_BACKUP_BYTES,
        "配置备份超过 128 MiB 限制"
    );
    Ok(bytes)
}

fn digest(bytes: &[u8]) -> String {
    Sha256::digest(bytes)
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}

fn validate(backup: &Backup) -> Result<()> {
    ensure!(
        backup.format == "siu-gui-backup" && backup.version == 1,
        "不支持的 siu 配置备份格式或版本"
    );
    ensure!(
        !backup.source_home.is_empty() && !backup.source_root.is_empty(),
        "备份缺少原机器目录信息"
    );
    valid_theme(backup.theme.as_deref())?;
    backup.proxy.clone().validate()?;
    let mut names = BTreeSet::new();
    for entry in &backup.mcp {
        entry.server.validate().context("备份中的 MCP 配置无效")?;
        ensure!(
            names.insert(entry.server.name.to_ascii_lowercase()),
            "备份中的 MCP 名称重复"
        );
    }
    names.clear();
    let (mut count, mut size) = (0, 0usize);
    for saved in &backup.skills {
        mcp::valid_name(&saved.entry.name)?;
        relative_path(&saved.entry.name)?;
        ensure!(
            names.insert(saved.entry.name.to_ascii_lowercase()),
            "备份中的 Skill 名称重复"
        );
        let mut paths = BTreeSet::new();
        for file in &saved.files {
            relative_path(&file.path)?;
            ensure!(
                paths.insert(file.path.to_lowercase()),
                "备份中的文件路径重复"
            );
            count += 1;
            ensure!(count <= MAX_FILES, "Skills 文件数量超过 {MAX_FILES}");
            size = size
                .checked_add(
                    STANDARD
                        .decode(&file.data)
                        .context("备份文件内容编码无效")?
                        .len(),
                )
                .context("备份文件过大")?;
            ensure!(size <= MAX_CONTENT_BYTES, "Skills 文件总大小超过 64 MiB");
        }
        ensure!(
            saved.files.iter().any(|f| f.path == "SKILL.md"),
            "Skill 缺少 SKILL.md"
        );
        for file in &saved.files {
            let lower = file.path.to_lowercase();
            for (index, _) in lower.match_indices('/') {
                ensure!(
                    !paths.contains(&lower[..index]),
                    "备份中有文件与目录路径冲突"
                );
            }
        }
    }
    Ok(())
}

fn load(path: &Path) -> Result<(Backup, String)> {
    let bytes = read_bounded(path, MAX_BACKUP_BYTES).context("无法读取配置备份")?;
    let backup: Backup = serde_json::from_slice(&bytes)
        .map_err(|_| anyhow::anyhow!("无法解析 siu JSON 配置备份"))?;
    validate(&backup)?;
    Ok((backup, digest(&bytes)))
}

/// Rebase only complete path prefixes. Never substitute arbitrary occurrences
/// inside tokens/URLs. Root has priority when SIU_HOME is outside HOME.
fn rebase(value: &str, backup: &Backup, store: &Store, home: &Path) -> String {
    for (old, new) in [
        (backup.source_root.as_str(), store.root.as_path()),
        (backup.source_home.as_str(), home),
    ] {
        let old = old.trim_end_matches(['/', '\\']);
        if let Some(tail) = value.strip_prefix(old)
            && (tail.is_empty() || tail.starts_with(['/', '\\']))
        {
            let mut result = new.to_path_buf();
            for part in tail.split(['/', '\\']).filter(|p| !p.is_empty()) {
                result.push(part);
            }
            return result.to_string_lossy().into();
        }
    }
    if let Some((flag, path)) = value.split_once('=')
        && flag.starts_with('-')
    {
        return format!("{flag}={}", rebase(path, backup, store, home));
    }
    value.into()
}

fn restore_entries(
    backup: &Backup,
    store: &Store,
    home: &Path,
) -> (Vec<McpEntry>, Vec<SkillEntry>) {
    let mut servers = backup.mcp.clone();
    for entry in &mut servers {
        entry.server.command = rebase(&entry.server.command, backup, store, home);
        for arg in &mut entry.server.args {
            *arg = rebase(arg, backup, store, home);
        }
        for value in entry.server.env.values_mut() {
            *value = rebase(value, backup, store, home);
        }
    }
    let mut entries: Vec<_> = backup.skills.iter().map(|s| s.entry.clone()).collect();
    for entry in &mut entries {
        if let Some(Source::Local { dir }) = &mut entry.source {
            *dir = PathBuf::from(rebase(&dir.to_string_lossy(), backup, store, home));
        }
    }
    (servers, entries)
}

fn check_targets(
    store: &Store,
    servers: &[McpEntry],
    entries: &[SkillEntry],
) -> Result<Vec<String>> {
    let mut issues = BTreeSet::new();
    for (kind, name, ids, server) in servers
        .iter()
        .map(|e| ("MCP", e.server.name.as_str(), &e.agents, Some(&e.server)))
        .chain(
            entries
                .iter()
                .map(|e| ("Skill", e.name.as_str(), &e.agents, None)),
        )
    {
        for id in ids {
            let Some(agent) = store.agent(id) else {
                bail!("备份包含未知 Agent：{id}");
            };
            if let Some(server) = server {
                ensure!(
                    id != "agents" && agent.mcp.is_some(),
                    "备份包含无效的 MCP 分配"
                );
                if let Some(why) = mcp::unsupported(agent.mcp.as_ref().unwrap().format, server) {
                    issues.insert(format!(
                        "{kind} {name}：{} 不支持此配置（{why}），已保留分配",
                        agent.name
                    ));
                }
            } else {
                let dir = agent.skills.as_ref().context("备份包含无效的 Skill 分配")?;
                let at = dir.join(name);
                let owned_link = store
                    .lib
                    .applied
                    .get(id)
                    .is_some_and(|a| a.skills.contains(name))
                    && fs::read_link(&at).is_ok();
                if fs::symlink_metadata(&at).is_ok()
                    && !skills::links_to(&at, &store.skill_dir(name))
                    && !owned_link
                {
                    issues.insert(format!("Skill {name}：{} 已有未纳管的同名文件，将保留本机文件；请先纳管或移走后重新同步", agent.name));
                }
            }
            if !agent.detected() && id != "agents" {
                issues.insert(format!(
                    "{kind} {name}：本机未检测到 {}，已保留分配，安装后请重新同步",
                    agent.name
                ));
            }
        }
        if let Some(server) = server {
            guard::check_mcp_collision(store, name, ids).map_err(anyhow::Error::msg)?;
            if Path::new(&server.command).is_absolute() && !Path::new(&server.command).exists() {
                issues.insert(format!(
                    "MCP {name}：本机缺少配置中的可执行文件，请编辑启动命令"
                ));
            }
        }
    }
    Ok(issues.into_iter().collect())
}

pub fn export(store: &Store, home: &Path, path: &Path, theme: Option<String>) -> Result<Value> {
    ensure!(path.is_absolute(), "请选择绝对路径的 JSON 文件");
    ensure!(
        path.extension()
            .and_then(|ext| ext.to_str())
            .is_some_and(|ext| ext.eq_ignore_ascii_case("json")),
        "请使用 .json 扩展名保存配置备份"
    );
    // Export must never overwrite live library data or an Agent config.
    let parent = fs::canonicalize(path.parent().context("无效的导出路径")?)?;
    let target = parent.join(path.file_name().context("无效的导出文件名")?);
    let target = fs::canonicalize(&target).unwrap_or(target);
    let root = fs::canonicalize(&store.root).unwrap_or(store.root.clone());
    ensure!(
        !target.starts_with(&root) || target == root.join("config.json"),
        "资源库内仅允许导出到 config.json，请选择其他保存位置"
    );
    for agent in &store.agents {
        if let Some(file) = &agent.mcp {
            let actual = fs::canonicalize(&file.path).unwrap_or(file.path.clone());
            ensure!(target != actual, "导出文件不能覆盖 Agent 的 MCP 配置");
        }
        if let Some(dir) = &agent.skills {
            let actual = fs::canonicalize(dir).unwrap_or(dir.clone());
            ensure!(
                !target.starts_with(actual),
                "导出文件不能保存在 Agent 的 Skills 目录"
            );
        }
    }
    let backup = capture(store, home, theme)?;
    validate(&backup)?;
    let mut file = tempfile::NamedTempFile::new_in(target.parent().unwrap())?;
    file.write_all(&encode(&backup)?)?;
    file.as_file().sync_all()?;
    file.persist(&target)?;
    Ok(json!({"path": path, "mcpCount": backup.mcp.len(), "skillCount": backup.skills.len()}))
}

pub fn preview(store: &Store, home: &Path, path: &Path) -> Result<Value> {
    let (backup, digest) = load(path)?;
    let (servers, entries) = restore_entries(&backup, store, home);
    let issues = check_targets(store, &servers, &entries)?;
    Ok(json!({
        "path": path, "digest": digest, "exportedAt": backup.exported_at,
        "mcpCount": servers.len(), "skillCount": entries.len(),
        "issues": issues, "theme": backup.theme,
    }))
}

fn restore_file(path: &Path, bytes: Option<&[u8]>) -> Result<()> {
    match bytes {
        Some(bytes) => mcp::write_atomic(path, bytes),
        None => match fs::remove_file(path) {
            Ok(()) => Ok(()),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(e) => Err(e.into()),
        },
    }
}

fn existing_bytes(path: &Path) -> Result<Option<Vec<u8>>> {
    match fs::read(path) {
        Ok(bytes) => Ok(Some(bytes)),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(e) => Err(e.into()),
    }
}

pub fn import(
    store: &mut Store,
    home: &Path,
    version: &str,
    path: &Path,
    expected: &str,
) -> Result<Value> {
    let (backup, actual) = load(path)?;
    ensure!(
        actual == expected,
        "配置备份在预览后发生变化，请重新选择文件并确认"
    );
    let (servers, mut entries) = restore_entries(&backup, store, home);
    let warnings = check_targets(store, &servers, &entries)?;
    fs::create_dir_all(&store.root)?;
    let work = tempfile::Builder::new()
        .prefix(".restore-")
        .tempdir_in(&store.root)?;
    let staged = work.path().join("new");
    for (saved, entry) in backup.skills.iter().zip(&mut entries) {
        let dir = staged.join(&entry.name);
        for file in &saved.files {
            let to = dir.join(relative_path(&file.path)?);
            fs::create_dir_all(to.parent().unwrap())?;
            fs::write(&to, STANDARD.decode(&file.data)?)?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                fs::set_permissions(
                    &to,
                    fs::Permissions::from_mode(if file.executable { 0o700 } else { 0o600 }),
                )?;
            }
        }
        entry.hash = skills::hash_dir(&dir);
    }
    let previous_library = existing_bytes(&store.root.join("library.json"))?;
    let previous_proxy = existing_bytes(&store.root.join("proxy.json"))?;
    let previous_backup = capture(store, home, None)?;
    validate(&previous_backup)?;
    let bytes = encode(&previous_backup)?;
    let backups = store.root.join("backups");
    fs::create_dir_all(&backups)?;
    let mut safety = tempfile::Builder::new()
        .prefix(&format!("config-{}-", library::now()))
        .suffix(".json")
        .tempfile_in(backups)?;
    safety.write_all(&bytes)?;
    safety.as_file().sync_all()?;
    let (_, safety_path) = safety.keep()?;

    // Keep removed skill trees until sync has retracted every owned link.
    // Only incoming names are swapped, with an undo copy held until save.
    let skills_dir = store.skills_dir();
    if let Ok(meta) = fs::symlink_metadata(&skills_dir) {
        ensure!(
            !meta.file_type().is_symlink() && meta.is_dir(),
            "资源库 Skills 目录不能是符号链接或文件"
        );
    }
    fs::create_dir_all(&skills_dir)?;
    let old = work.path().join("old");
    fs::create_dir_all(&old)?;
    let mut swapped = Vec::new();
    let previous = store.lib.clone();
    let persist = (|| -> Result<()> {
        for entry in &entries {
            let target = store.skill_dir(&entry.name);
            if fs::symlink_metadata(&target).is_ok() {
                ensure!(
                    target.is_dir() && !fs::symlink_metadata(&target)?.file_type().is_symlink(),
                    "已有 Skill 目录无效"
                );
                fs::rename(&target, old.join(&entry.name))?;
            }
            swapped.push(entry.name.clone());
            fs::rename(staged.join(&entry.name), target)?;
        }
        store.lib.mcp = servers;
        store.lib.skills = entries;
        proxy::save(&store.root, backup.proxy.clone())?;
        store.save()?;
        Ok(())
    })();
    if let Err(error) = persist {
        store.lib = previous;
        let mut rollback_errors = Vec::new();
        for name in swapped.iter().rev() {
            let target = store.skill_dir(name);
            if target.exists()
                && let Err(e) = fs::remove_dir_all(&target)
            {
                rollback_errors.push(e.to_string());
            }
            if old.join(name).exists()
                && let Err(e) = fs::rename(old.join(name), target)
            {
                rollback_errors.push(e.to_string());
            }
        }
        for (name, bytes) in [
            ("library.json", previous_library.as_deref()),
            ("proxy.json", previous_proxy.as_deref()),
        ] {
            if let Err(e) = restore_file(&store.root.join(name), bytes) {
                rollback_errors.push(e.to_string());
            }
        }
        if !rollback_errors.is_empty() {
            // Preserve undo data if the filesystem also prevents rollback.
            let undo = work.keep();
            bail!(
                "恢复失败：{error:#}；回滚未完成：{}；备份：{}；临时恢复目录：{}",
                rollback_errors.join("；"),
                safety_path.display(),
                undo.display()
            );
        }
        bail!(
            "恢复失败，已回滚：{error:#}；原配置备份：{}",
            safety_path.display()
        );
    }
    // The shared Skills target can be selected before any Agent creates it.
    // Detection is evaluated dynamically by Store::sync.
    let mut setup_issues = Vec::new();
    if store
        .lib
        .skills
        .iter()
        .any(|e| e.enabled && e.agents.contains("agents"))
        && let Some(dir) = store.agent("agents").and_then(|a| a.skills.as_ref())
        && let Err(error) = fs::create_dir_all(dir)
    {
        setup_issues.push(format!("无法创建共享 Skills 目录：{error}"));
    }
    let mut report = store.sync();
    report.issues.extend(setup_issues);
    if let Err(error) = store.save() {
        report
            .issues
            .push(format!("保存同步记录失败：{error:#}，请重新同步"));
    }
    if report.issues.is_empty()
        && !store
            .lib
            .applied
            .values()
            .any(|a| a.skills.iter().any(|name| store.skill(name).is_none()))
    {
        store.prune();
    }
    // Remember an explicit/default config.json restore only after its local
    // desired state is durable. Partial Agent sync can be retried separately.
    if let (Ok(selected), Ok(default)) = (
        fs::canonicalize(path),
        fs::canonicalize(store.root.join("config.json")),
    ) && selected == default
    {
        let applied = AppliedConfig {
            digest: actual,
            theme: backup.theme.clone(),
        };
        if let Err(error) = mcp::write_atomic(
            &store.root.join("config.applied.json"),
            &serde_json::to_vec(&applied)?,
        ) {
            report
                .issues
                .push(format!("无法记录 config.json 已恢复状态：{error:#}"));
        }
    }
    report.issues.extend(warnings);
    report.changed += backup.mcp.len() + backup.skills.len();
    let mut result = protocol::mutation_result(
        store,
        home,
        version,
        &report,
        "配置已恢复",
        Some(format!("恢复前的配置已备份到 {}", safety_path.display())),
    );
    result["theme"] = json!(backup.theme);
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::agents::Env;

    fn store(home: &Path) -> Store {
        Store::open(&Env::rooted(home), home.join(".siu")).unwrap()
    }

    fn skill(store: &mut Store, name: &str, content: &str) {
        let dir = store.skill_dir(name);
        fs::create_dir_all(&dir).unwrap();
        fs::write(dir.join("SKILL.md"), content).unwrap();
        store.lib.skills.push(SkillEntry {
            name: name.into(),
            description: String::new(),
            source: None,
            agents: BTreeSet::new(),
            enabled: true,
            hash: skills::hash_dir(&dir),
            installed: 1,
            updated: 2,
        });
    }

    #[test]
    fn file_validation_rejects_traversal_and_cross_platform_collisions() {
        let temp = tempfile::tempdir().unwrap();
        let mut source = store(temp.path());
        skill(&mut source, "sample", "original");
        let mut backup = capture(&source, temp.path(), None).unwrap();
        for path in [
            "../escape",
            "/absolute",
            "C:/escape",
            "a\\b",
            "a/../b",
            "a//b",
            "NUL.txt",
            "a/COM1",
            "file:stream",
            "a/.",
            "a. ",
        ] {
            backup.skills[0].files[0].path = path.into();
            assert!(validate(&backup).is_err(), "accepted {path}");
        }
        backup.skills[0].files[0].path = "SKILL.md".into();
        backup.skills[0].files.push(SavedFile {
            path: "skill.MD".into(),
            data: STANDARD.encode(b"duplicate"),
            executable: false,
        });
        assert!(validate(&backup).is_err());
        backup.skills[0].files[1].path = "SKILL.md/child".into();
        assert!(validate(&backup).is_err());
        backup.skills[0].files[1].path = "valid.txt".into();
        backup.skills[0].files[1].data = "invalid base64!".into();
        assert!(validate(&backup).is_err());
        backup.skills[0].files.pop();
        assert!(validate(&backup).is_ok());
    }

    #[test]
    fn rebases_windows_and_unix_paths_without_substituting_credentials() {
        let temp = tempfile::tempdir().unwrap();
        let destination = store(temp.path());
        let mut backup = capture(&destination, temp.path(), None).unwrap();
        for (home, root, file) in [
            ("/old/home", "/separate/library", "/old/home/tools/file"),
            (r"C:\Users\old", r"D:\siu", r"C:\Users\old\tools\file"),
        ] {
            backup.source_home = home.into();
            backup.source_root = root.into();
            assert_eq!(
                rebase(file, &backup, &destination, temp.path()),
                temp.path().join("tools/file").to_string_lossy()
            );
            assert_eq!(
                rebase(
                    &format!("{root}/skills/sample"),
                    &backup,
                    &destination,
                    temp.path()
                ),
                destination.skill_dir("sample").to_string_lossy()
            );
            assert_eq!(
                rebase(
                    &format!("--path={file}"),
                    &backup,
                    &destination,
                    temp.path()
                ),
                format!("--path={}", temp.path().join("tools/file").display())
            );
            let token = format!("Bearer token-{home}/secret");
            assert_eq!(rebase(&token, &backup, &destination, temp.path()), token);
            let similar = format!("{home}-other/file");
            assert_eq!(
                rebase(&similar, &backup, &destination, temp.path()),
                similar
            );
        }
    }

    #[test]
    fn persistence_failure_rolls_back_replaced_skill_files_and_config() {
        let from = tempfile::tempdir().unwrap();
        let to = tempfile::tempdir().unwrap();
        let mut source = store(from.path());
        skill(&mut source, "alpha", "incoming alpha");
        skill(&mut source, "omega", "incoming omega");
        let path = from.path().join("backup.json");
        export(&source, from.path(), &path, None).unwrap();
        let mut destination = store(to.path());
        skill(&mut destination, "alpha", "original alpha");
        destination.save().unwrap();
        proxy::save(&destination.root, proxy::Settings::default()).unwrap();
        let library = fs::read(destination.root.join("library.json")).unwrap();
        let proxy = fs::read(destination.root.join("proxy.json")).unwrap();
        // An orphan file prevents the second directory swap after alpha has
        // already been replaced, independent of OS/root permission behavior.
        fs::write(destination.skills_dir().join("omega"), b"orphan").unwrap();
        let inspected = preview(&destination, to.path(), &path).unwrap();
        let error = import(
            &mut destination,
            to.path(),
            "test",
            &path,
            inspected["digest"].as_str().unwrap(),
        )
        .unwrap_err();
        assert!(error.to_string().contains("已回滚"), "{error:#}");
        assert_eq!(
            fs::read(destination.skill_dir("alpha").join("SKILL.md")).unwrap(),
            b"original alpha"
        );
        assert_eq!(
            fs::read(destination.skills_dir().join("omega")).unwrap(),
            b"orphan"
        );
        assert_eq!(
            fs::read(destination.root.join("library.json")).unwrap(),
            library
        );
        assert_eq!(
            fs::read(destination.root.join("proxy.json")).unwrap(),
            proxy
        );
        assert_eq!(destination.lib.skills.len(), 1);
        assert!(
            fs::read_dir(destination.root.join("backups"))
                .unwrap()
                .any(|e| e.unwrap().path().extension().is_some_and(|x| x == "json"))
        );
    }

    #[cfg(unix)]
    #[test]
    fn exports_private_files_even_when_replacing_a_public_file() {
        use std::os::unix::fs::PermissionsExt;
        let temp = tempfile::tempdir().unwrap();
        let source = store(temp.path());
        let path = temp.path().join("backup.json");
        fs::write(&path, b"old").unwrap();
        fs::set_permissions(&path, fs::Permissions::from_mode(0o644)).unwrap();
        export(&source, temp.path(), &path, None).unwrap();
        assert_eq!(
            fs::metadata(&path).unwrap().permissions().mode() & 0o777,
            0o600
        );
    }
}
