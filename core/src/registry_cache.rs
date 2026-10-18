//! Disk cache for MCP Registry search descriptors. Metadata stays usable for
//! installation after restarting the app, including when the registry is offline.
use crate::{cache, market::MarketServer};
use sha2::{Digest, Sha256};
use std::path::Path;
use std::time::Duration;

fn key(query: &str) -> String {
    let digest = Sha256::digest(query.trim().to_lowercase().as_bytes());
    let hex: String = digest.iter().map(|byte| format!("{byte:02x}")).collect();
    format!("mcp-registry-{hex}")
}

pub fn search(
    dir: &Path,
    query: &str,
    refresh: bool,
    fetch: impl FnOnce() -> anyhow::Result<Vec<MarketServer>>,
) -> (Vec<MarketServer>, Vec<String>, bool) {
    let key = key(query);
    let previous = cache::read::<Vec<MarketServer>>(dir, &key, Duration::from_secs(3600));
    if !refresh && previous.as_ref().is_some_and(|p| p.fresh) {
        return (previous.unwrap().data, vec![], true);
    }
    match fetch() {
        Ok(list) => {
            cache::write(dir, &key, &list);
            trim(dir);
            (list, vec![], false)
        }
        Err(error) => match previous {
            Some(cached) => (
                cached.data,
                vec![format!("MCP Registry: {error:#}; using cached results")],
                true,
            ),
            None => (vec![], vec![format!("MCP Registry: {error:#}")], false),
        },
    }
}

fn trim(dir: &Path) {
    let Ok(entries) = std::fs::read_dir(dir) else {
        return;
    };
    let mut files: Vec<_> = entries
        .filter_map(Result::ok)
        .filter(|e| {
            e.file_name().to_string_lossy().starts_with("mcp-registry-")
                && e.path().extension().is_some_and(|e| e == "json")
        })
        .filter_map(|e| {
            e.metadata()
                .ok()
                .and_then(|m| m.modified().ok())
                .map(|t| (t, e.path()))
        })
        .collect();
    files.sort_by_key(|(time, _)| *time);
    let excess = files.len().saturating_sub(100);
    for (_, path) in files.into_iter().take(excess) {
        let _ = std::fs::remove_file(path);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn cached_descriptors_survive_restart_and_offline_refresh() {
        let dir = tempfile::tempdir().unwrap();
        let offered = vec![crate::market::featured().remove(0)];
        let (first, warnings, cached) =
            search(dir.path(), "Context7", false, || Ok(offered.clone()));
        assert_eq!(first, offered);
        assert!(warnings.is_empty());
        assert!(!cached);
        let (fresh, warnings, cached) = search(dir.path(), " context7 ", false, || {
            panic!("fresh cache should avoid network")
        });
        assert_eq!(fresh, offered);
        assert!(warnings.is_empty());
        assert!(cached);
        let (offline, warnings, cached) =
            search(dir.path(), "context7", true, || anyhow::bail!("offline"));
        assert_eq!(offline, offered);
        assert!(warnings[0].contains("offline"));
        assert!(cached);
    }
    #[test]
    fn unavailable_registry_without_cache_reports_problem() {
        let dir = tempfile::tempdir().unwrap();
        let (list, warnings, cached) = search(dir.path(), "unknown", false, || {
            anyhow::bail!("unavailable")
        });
        assert!(list.is_empty());
        assert!(!warnings.is_empty());
        assert!(!cached);
    }
    #[test]
    fn cache_limit_preserves_unrelated_files() {
        let dir = tempfile::tempdir().unwrap();
        for n in 0..105 {
            cache::write(dir.path(), &format!("mcp-registry-{n}"), &vec![n]);
        }
        std::fs::write(dir.path().join("skills-popular.json"), "retained").unwrap();
        trim(dir.path());
        assert_eq!(std::fs::read_dir(dir.path()).unwrap().count(), 101);
        assert_eq!(
            std::fs::read_to_string(dir.path().join("skills-popular.json")).unwrap(),
            "retained"
        );
    }
}
