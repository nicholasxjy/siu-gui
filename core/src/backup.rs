//! Desktop protocol adapter for siu's portable configuration backups.
//! Keep format validation and restore behavior in the vendored implementation.

use std::path::Path;

use anyhow::{Context, Result};
use serde_json::{Value, json};

use crate::library::Store;
use crate::{portable_backup, protocol};

pub(crate) use crate::portable_backup::AppliedConfig;

/// Only a pending default restore takes the write lock. Ordinary snapshots
/// remain available while unrelated downloads or mutations are in progress.
/// The shared implementation checks the receipt again after taking the lock.
pub fn apply_default(
    app: &crate::server::App,
    resolved: &crate::env::Resolved,
) -> Result<(Option<AppliedConfig>, Vec<String>)> {
    let path = resolved.root.join("config.json");
    let bytes = match portable_backup::read_bounded(&path, portable_backup::MAX_BACKUP_BYTES) {
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
    let current_digest = portable_backup::digest(&bytes);
    drop(bytes);
    if let Some(applied) = portable_backup::applied_config(&resolved.root)?
        && applied.digest == current_digest
    {
        return Ok((Some(applied), vec![]));
    }
    let _guard = app.write_lock.lock().unwrap_or_else(|p| p.into_inner());
    let mut store = Store::open(&resolved.env, resolved.root.clone())?;
    if let Some(restored) = portable_backup::apply_default(&mut store, &resolved.env.home)? {
        // Keep the restore report and its theme even if the receipt could not
        // be saved; the shared core reports that failure and retries next time.
        return Ok((
            Some(AppliedConfig {
                digest: restored.digest,
                theme: restored.theme,
            }),
            restored.report.issues,
        ));
    }
    let applied = if path.exists() {
        portable_backup::applied_config(&resolved.root)?
    } else {
        None
    };
    Ok((applied, vec![]))
}

pub fn export(store: &Store, home: &Path, path: &Path, theme: Option<String>) -> Result<Value> {
    let exported =
        portable_backup::export_with_theme(store, home, path, theme).context("无法导出配置备份")?;
    Ok(json!({"path": path, "mcpCount": exported.mcp, "skillCount": exported.skills}))
}

pub fn preview(store: &Store, home: &Path, path: &Path) -> Result<Value> {
    let preview = portable_backup::preview(store, home, path).context("无法预览配置备份")?;
    Ok(json!({
        "path": path, "digest": preview.digest, "exportedAt": preview.exported_at,
        "mcpCount": preview.mcp, "skillCount": preview.skills,
        "issues": preview.issues, "theme": preview.theme,
    }))
}

pub fn import(
    store: &mut Store,
    home: &Path,
    version: &str,
    path: &Path,
    expected: &str,
) -> Result<Value> {
    let restored =
        portable_backup::import(store, home, path, expected).context("无法恢复配置备份")?;
    let mut result = protocol::mutation_result(
        store,
        home,
        version,
        &restored.report,
        "配置已恢复",
        Some(format!("恢复前的配置已备份到 {}", restored.saved.display())),
    );
    result["theme"] = json!(restored.theme);
    Ok(result)
}
