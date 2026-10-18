//! Retained `scan_skills` results: a scan token owns the downloaded
//! `TempDir` (or names a canonical local directory) long enough for
//! `install_skills` to use it, without the frontend ever handing back an
//! arbitrary filesystem path. Unused tokens expire after 30 minutes and at
//! most 8 are retained at once, the oldest evicted first.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use sha2::{Digest, Sha256};

use crate::skills::Found;

pub const TTL: Duration = Duration::from_secs(30 * 60);
pub const CAP: usize = 8;

#[derive(Clone)]
pub enum Origin {
    Github { repo: String, reference: String },
    Local,
}

pub struct Entry {
    // kept for future diagnostics (e.g. surfacing what a token points at);
    // not read anywhere yet.
    #[allow(dead_code)]
    pub source: String,
    #[allow(dead_code)]
    pub root: PathBuf,
    pub origin: Origin,
    pub found: Vec<Found>,
    created: Instant,
    // kept only to hold the temp directory alive for as long as the entry
    // is retained; a local-directory scan has none.
    _guard: Option<tempfile::TempDir>,
}

#[derive(Default)]
pub struct ScanManager {
    entries: Mutex<HashMap<String, Entry>>,
}

fn token() -> String {
    static COUNTER: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    let c = COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or_default();
    let mut h = Sha256::new();
    h.update(nanos.to_le_bytes());
    h.update(c.to_le_bytes());
    h.update(std::process::id().to_le_bytes());
    h.finalize()
        .iter()
        .take(16)
        .map(|b| format!("{b:02x}"))
        .collect()
}

impl ScanManager {
    pub fn new() -> Self {
        Self::default()
    }

    /// Stores a scan's result and returns its token. Expired entries are
    /// swept first; if still at capacity, the oldest entry is evicted.
    pub fn insert(
        &self,
        source: String,
        root: PathBuf,
        origin: Origin,
        found: Vec<Found>,
        guard: Option<tempfile::TempDir>,
    ) -> String {
        let mut entries = self.entries.lock().unwrap();
        entries.retain(|_, e| e.created.elapsed() < TTL);
        while entries.len() >= CAP {
            let Some(oldest) = entries
                .iter()
                .min_by_key(|(_, e)| e.created)
                .map(|(k, _)| k.clone())
            else {
                break;
            };
            entries.remove(&oldest);
        }
        let tok = token();
        entries.insert(
            tok.clone(),
            Entry {
                source,
                root,
                origin,
                found,
                created: Instant::now(),
                _guard: guard,
            },
        );
        tok
    }

    /// Runs `f` with the entry for `token`, after sweeping expired ones.
    /// `None` means the token never existed, already expired, or was
    /// released — callers should report that as "scan token expired".
    pub fn with<R>(&self, token: &str, f: impl FnOnce(&Entry) -> R) -> Option<R> {
        let mut entries = self.entries.lock().unwrap();
        entries.retain(|_, e| e.created.elapsed() < TTL);
        entries.get(token).map(f)
    }

    /// Drops a scan's retained temp data. Returns whether it existed.
    pub fn release(&self, token: &str) -> bool {
        let mut entries = self.entries.lock().unwrap();
        entries.remove(token).is_some()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn found(path: &str) -> Found {
        Found {
            path: path.into(),
            dir: PathBuf::from("/tmp/x").join(path),
            meta: crate::skills::Meta::default(),
        }
    }

    #[test]
    fn tokens_are_retained_and_released() {
        let m = ScanManager::new();
        let tok = m.insert(
            "a/b".into(),
            "/tmp/a".into(),
            Origin::Local,
            vec![found("pdf")],
            None,
        );
        assert!(m.with(&tok, |e| e.source.clone()).is_some());
        assert!(m.release(&tok));
        assert!(m.with(&tok, |e| e.source.clone()).is_none());
        assert!(!m.release(&tok));
    }

    #[test]
    fn at_most_eight_scans_are_retained() {
        let m = ScanManager::new();
        let mut tokens = vec![];
        for i in 0..10 {
            tokens.push(m.insert(
                format!("s{i}"),
                format!("/tmp/{i}").into(),
                Origin::Local,
                vec![],
                None,
            ));
            std::thread::sleep(std::time::Duration::from_millis(2));
        }
        let alive: usize = tokens
            .iter()
            .filter(|t| m.with(t, |_| ()).is_some())
            .count();
        assert_eq!(alive, CAP);
        // the two oldest were evicted, the newest kept
        assert!(m.with(&tokens[9], |_| ()).is_some());
        assert!(m.with(&tokens[0], |_| ()).is_none());
    }

    #[test]
    fn an_unknown_token_is_none() {
        let m = ScanManager::new();
        assert!(m.with("nope", |_| ()).is_none());
    }
}
