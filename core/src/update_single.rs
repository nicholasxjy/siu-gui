//! A single named MCP server or skill update, reusing upstream's package
//! plan / download logic without routing through `update_mcp`/
//! `update_skills`' "touch every entry" loop.
//!
//! Those two functions operate on `store.lib.mcp`/`store.lib.skills`
//! directly (never a filtered copy), so even a single-name update here
//! calls `Store::commit`/`Store::save` on the real, untouched store — every
//! other library entry is written back exactly as it was. Filtering a
//! *copy* of the library down to one entry before syncing, the obvious
//! alternative, is exactly what would make a single-item update erase the
//! rest: `sync` removes every previously-applied name that is no longer in
//! `lib.mcp`/`lib.skills`.

use anyhow::{Context, Result, bail};

use crate::library::{Source, Store, UpdateOutcome};
use crate::update::{self, Plan, Remote};

pub struct UpdateLine {
    pub line: String,
    pub failed: bool,
}

pub fn update_one_mcp(store: &mut Store, name: &str) -> Result<UpdateLine> {
    let s = store
        .server(name)
        .map(|e| e.server.clone())
        .with_context(|| format!("no server called {name}"))?;
    let net = update::Net;
    let (line, failed, changed) = match update::plan(&s) {
        Plan::Remote => ("remote, nothing to update".to_string(), false, false),
        Plan::Unknown => (
            format!("runs {}, not a package siu can update", s.command),
            false,
            false,
        ),
        Plan::Floating(spec) => (format!("{spec} follows its releases already"), false, false),
        Plan::Image(image) => match net.pull(&image) {
            Ok(()) => (format!("pulled {image}"), false, false),
            Err(e) => (format!("couldn't pull {image}: {e:#}"), true, false),
        },
        Plan::Package {
            eco,
            name: pkg_name,
            version,
            at,
            prefix,
            base,
            sep,
        } => match net.latest(eco, &pkg_name) {
            Err(e) => (format!("couldn't look up {pkg_name}: {e:#}"), true, false),
            Ok(v) if version.as_deref() == Some(v.as_str()) => {
                (format!("{pkg_name} {v} is the latest"), false, false)
            }
            Ok(v) => {
                let mut new = s.clone();
                new.args[at] = format!("{prefix}{base}{sep}{v}");
                store.edit_server(name, new)?;
                let from = version.unwrap_or_else(|| "unpinned".into());
                (format!("{pkg_name} {from} → {v}"), false, true)
            }
        },
    };
    if !changed {
        return Ok(UpdateLine { line, failed });
    }
    let report = store.commit()?;
    let failed = failed || !report.issues.is_empty();
    let line = if report.issues.is_empty() {
        line
    } else {
        format!("{line} ({} issue(s) writing it in)", report.issues.len())
    };
    Ok(UpdateLine { line, failed })
}

pub fn update_one_skill(store: &mut Store, name: &str) -> Result<UpdateLine> {
    let source = store
        .skill(name)
        .with_context(|| format!("no skill called {name}"))?
        .source
        .clone();
    let resolve = |store: &mut Store, name: &str, tree: &std::path::Path| match store
        .update_skill_from(name, tree)
    {
        Ok(UpdateOutcome::Updated) => ("updated".to_string(), false),
        Ok(UpdateOutcome::UpToDate) => ("up to date".to_string(), false),
        Err(e) => (format!("{e:#}"), true),
    };
    let (line, failed) = match source {
        None => ("imported, no source to update from".to_string(), false),
        Some(Source::Local { dir }) => resolve(store, name, &dir),
        Some(Source::Github {
            repo, reference, ..
        }) => match crate::github::download(&repo, &reference) {
            Ok((_guard, root)) => resolve(store, name, &root),
            Err(e) => (format!("couldn't download {repo}: {e:#}"), true),
        },
    };
    store.save()?;
    Ok(UpdateLine { line, failed })
}

/// Guards against a name that is neither a known server nor a known skill
/// so the caller gets a clean `{ok:false,error}` instead of a confusing
/// "no server called x" when the frontend actually meant a skill (or a
/// typo).
pub fn require_known(store: &Store, kind: &str, name: &str) -> Result<()> {
    let known = match kind {
        "mcp" => store.server(name).is_some(),
        "skills" => store.skill(name).is_some(),
        other => bail!("unknown kind {other:?}"),
    };
    if known {
        Ok(())
    } else {
        bail!("no {kind} entry called {name}")
    }
}
