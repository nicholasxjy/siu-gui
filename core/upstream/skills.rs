//! Skills: folders with a SKILL.md, found in a tree, hashed, copied and
//! linked into agents' skill folders.
//!
//! Vendored from upstream siu (see UPSTREAM.md). Deviation from upstream:
//! `link` on Windows creates a directory junction through the Windows API (no
//! administrator privilege required) instead of falling back to a full
//! copy, so an agent's "skill" really is siu's library copy and a later
//! update or removal affects it; the previous `#[cfg(not(unix))]` copy
//! fallback silently went stale on update/removal. A new `unlink` helper
//! removes a link safely on both platforms — a Windows junction must be
//! removed as a reparse point, preserving its target — and is used everywhere
//! a siu-made skill link is taken out (library.rs).

use std::fs;
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use sha2::{Digest, Sha256};

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct Meta {
    pub name: String,
    pub description: String,
}

/// The `name` and `description` from a SKILL.md's front matter.
pub fn parse_meta(text: &str) -> Meta {
    let mut meta = Meta::default();
    let mut lines = text.lines();
    if lines.next().map(str::trim) != Some("---") {
        return meta;
    }
    let mut current: Option<&str> = None;
    for line in lines {
        if line.trim() == "---" {
            break;
        }
        let indented = line.starts_with(' ') || line.starts_with('\t');
        if indented {
            // a folded or literal block continues the key before it
            if let Some(k) = current {
                let v = line.trim();
                let field = if k == "name" {
                    &mut meta.name
                } else {
                    &mut meta.description
                };
                if !v.is_empty() {
                    if !field.is_empty() {
                        field.push(' ');
                    }
                    field.push_str(v);
                }
            }
            continue;
        }
        current = None;
        let Some((k, v)) = line.split_once(':') else {
            continue;
        };
        let k = k.trim();
        if k != "name" && k != "description" {
            continue;
        }
        let v = v.trim();
        let field = if k == "name" {
            &mut meta.name
        } else {
            &mut meta.description
        };
        if v.is_empty() || v == ">" || v == "|" || v == ">-" || v == "|-" {
            current = Some(if k == "name" { "name" } else { "description" });
            continue;
        }
        *field = unquote(v);
    }
    meta
}

fn unquote(v: &str) -> String {
    let v = v.trim();
    for q in ['"', '\''] {
        if v.len() >= 2 && v.starts_with(q) && v.ends_with(q) {
            return v[1..v.len() - 1].replace("\\\"", "\"").replace("''", "'");
        }
    }
    v.to_string()
}

/// A skill found in a tree: its folder, relative to the tree's root.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Found {
    pub path: String,
    pub dir: PathBuf,
    pub meta: Meta,
}

impl Found {
    /// What the skill is called: its own name, else its folder's.
    pub fn name(&self) -> String {
        let n = sanitize(&self.meta.name);
        if n.is_empty() {
            sanitize(&last_part(&self.path))
        } else {
            n
        }
    }
}

pub fn last_part(p: &str) -> String {
    p.rsplit('/').next().unwrap_or(p).to_string()
}

/// A name siu can use as a folder name and a key.
pub fn sanitize(s: &str) -> String {
    let n: String = s
        .trim()
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '-' || c == '_' {
                c.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect();
    let n = n.trim_matches(|c| c == '-' || c == '_').to_string();
    n.chars().take(64).collect()
}

/// Every skill under `root`, not looking inside a skill's own folder.
pub fn find(root: &Path) -> Vec<Found> {
    let mut out = vec![];
    walk(root, root, 0, &mut out);
    out.sort_by(|a, b| a.path.cmp(&b.path));
    out
}

fn walk(root: &Path, dir: &Path, depth: usize, out: &mut Vec<Found>) {
    if depth > 6 {
        return;
    }
    let skill_md = dir.join("SKILL.md");
    if skill_md.is_file() {
        let text = fs::read_to_string(&skill_md).unwrap_or_default();
        let rel = dir
            .strip_prefix(root)
            .unwrap_or(dir)
            .to_string_lossy()
            .replace('\\', "/");
        out.push(Found {
            path: rel,
            dir: dir.to_path_buf(),
            meta: parse_meta(&text),
        });
        return;
    }
    let Ok(rd) = fs::read_dir(dir) else { return };
    let mut entries: Vec<_> = rd.flatten().collect();
    entries.sort_by_key(|e| e.file_name());
    for e in entries {
        let name = e.file_name().to_string_lossy().to_string();
        if name.starts_with('.') || name == "node_modules" || name == "target" {
            continue;
        }
        if e.file_type().map(|t| t.is_dir()).unwrap_or(false) {
            walk(root, &e.path(), depth + 1, out);
        }
    }
}

/// A hash of a folder's files, their paths and contents.
pub fn hash_dir(dir: &Path) -> String {
    let mut files = vec![];
    collect_files(dir, dir, &mut files);
    files.sort();
    let mut h = Sha256::new();
    for rel in files {
        h.update(rel.as_bytes());
        h.update([0]);
        h.update(fs::read(dir.join(&rel)).unwrap_or_default());
        h.update([0]);
    }
    h.finalize()
        .iter()
        .take(8)
        .map(|b| format!("{b:02x}"))
        .collect()
}

fn collect_files(root: &Path, dir: &Path, out: &mut Vec<String>) {
    let Ok(rd) = fs::read_dir(dir) else { return };
    for e in rd.flatten() {
        let p = e.path();
        let Ok(t) = e.file_type() else { continue };
        if t.is_dir() {
            if e.file_name() != ".git" {
                collect_files(root, &p, out);
            }
        } else if t.is_file() {
            out.push(
                p.strip_prefix(root)
                    .unwrap_or(&p)
                    .to_string_lossy()
                    .replace('\\', "/"),
            );
        }
    }
}

pub fn copy_dir(src: &Path, dst: &Path) -> Result<()> {
    fs::create_dir_all(dst).with_context(|| format!("{}", dst.display()))?;
    for e in fs::read_dir(src)? {
        let e = e?;
        let t = e.file_type()?;
        let to = dst.join(e.file_name());
        if t.is_dir() {
            if e.file_name() != ".git" {
                copy_dir(&e.path(), &to)?;
            }
        } else if t.is_file() {
            fs::copy(e.path(), &to).with_context(|| format!("{}", to.display()))?;
        } else if t.is_symlink() {
            // a link inside a skill is copied as what it points at, when it can be
            if let Ok(meta) = fs::metadata(e.path())
                && meta.is_file()
            {
                fs::copy(e.path(), &to)?;
            }
        }
    }
    Ok(())
}

/// Replaces `dst` with a copy of `src`, by way of a sibling folder, so a
/// failed copy leaves the old one in place.
pub fn replace_dir(src: &Path, dst: &Path) -> Result<()> {
    let tmp = dst.with_extension("siu-new");
    let _ = fs::remove_dir_all(&tmp);
    copy_dir(src, &tmp)?;
    if dst.exists() {
        fs::remove_dir_all(dst)?;
    }
    fs::rename(&tmp, dst)?;
    Ok(())
}

/// Whether `p` is a link that points at `target`.
pub fn links_to(p: &Path, target: &Path) -> bool {
    match fs::read_link(p) {
        Ok(t) => {
            let t = if t.is_absolute() {
                t
            } else {
                p.parent().unwrap_or(Path::new("")).join(t)
            };
            same_path(&t, target)
        }
        Err(_) => false,
    }
}

fn same_path(a: &Path, b: &Path) -> bool {
    match (fs::canonicalize(a), fs::canonicalize(b)) {
        (Ok(a), Ok(b)) => a == b,
        _ => a == b,
    }
}

/// Points `link` at `target`: a symlink on Unix, a directory junction on
/// Windows (no administrator privilege required, unlike a symlink).
pub fn link(target: &Path, link: &Path) -> Result<()> {
    if let Some(dir) = link.parent() {
        fs::create_dir_all(dir)?;
    }
    #[cfg(unix)]
    std::os::unix::fs::symlink(target, link).with_context(|| format!("{}", link.display()))?;
    #[cfg(windows)]
    junction::create(target, link)
        .with_context(|| format!("couldn't create junction {}", link.display()))?;
    #[cfg(not(any(unix, windows)))]
    copy_dir(target, link)?;
    Ok(())
}

/// Removes a link siu made: a plain `remove_file` on Unix, but a Windows
/// junction (a directory reparse point) must be removed with
/// `remove_dir`, which `fs::remove_file` rejects.
pub fn unlink(p: &Path) -> Result<()> {
    #[cfg(windows)]
    {
        use std::os::windows::fs::FileTypeExt;
        let kind = fs::symlink_metadata(p)?.file_type();
        // RemoveDirectoryW removes the junction itself, including when its
        // target is missing. Deleting only the reparse point leaves an empty
        // directory behind and prevents a later assignment from recreating it.
        if kind.is_dir() || kind.is_symlink_dir() {
            return fs::remove_dir(p).with_context(|| format!("{}", p.display()));
        }
    }
    fs::remove_file(p).with_context(|| format!("{}", p.display()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn front_matter() {
        let m = parse_meta(
            "---\nname: pdf\ndescription: \"Read: and write PDFs\"\nlicense: MIT\n---\n# body\nname: no",
        );
        assert_eq!(
            m,
            Meta {
                name: "pdf".into(),
                description: "Read: and write PDFs".into()
            }
        );
        let m =
            parse_meta("---\nname: x\ndescription: >\n  folded over\n  two lines\nother: 1\n---\n");
        assert_eq!(m.description, "folded over two lines");
        assert_eq!(parse_meta("# no front matter"), Meta::default());
    }

    #[test]
    fn finds_skills_but_not_inside_one() {
        let d = tempfile::tempdir().unwrap();
        let mk = |p: &str, name: &str| {
            let dir = d.path().join(p);
            fs::create_dir_all(&dir).unwrap();
            fs::write(
                dir.join("SKILL.md"),
                format!("---\nname: {name}\ndescription: d\n---\n"),
            )
            .unwrap();
        };
        mk("skills/pdf", "pdf");
        mk("skills/pdf/inner", "inner");
        mk("skills/Web Design", "");
        mk(".hidden/x", "x");
        let found = find(d.path());
        let paths: Vec<_> = found.iter().map(|f| f.path.as_str()).collect();
        assert_eq!(paths, ["skills/Web Design", "skills/pdf"]);
        assert_eq!(found[0].name(), "web-design");
        assert_eq!(found[1].name(), "pdf");
    }

    #[test]
    fn hash_follows_content() {
        let d = tempfile::tempdir().unwrap();
        fs::write(d.path().join("SKILL.md"), "a").unwrap();
        let h1 = hash_dir(d.path());
        assert_eq!(h1, hash_dir(d.path()));
        fs::write(d.path().join("SKILL.md"), "b").unwrap();
        assert_ne!(h1, hash_dir(d.path()));
    }

    #[test]
    fn replace_and_link() {
        let d = tempfile::tempdir().unwrap();
        let src = d.path().join("src");
        fs::create_dir_all(src.join("sub")).unwrap();
        fs::write(src.join("SKILL.md"), "x").unwrap();
        fs::write(src.join("sub/f"), "y").unwrap();
        let dst = d.path().join("lib/x");
        fs::create_dir_all(&dst).unwrap();
        fs::write(dst.join("stale"), "z").unwrap();
        replace_dir(&src, &dst).unwrap();
        assert!(!dst.join("stale").exists());
        assert_eq!(fs::read_to_string(dst.join("sub/f")).unwrap(), "y");
        let l = d.path().join("agent/skills/x");
        link(&dst, &l).unwrap();
        assert!(links_to(&l, &dst));
        assert!(!links_to(&dst, &dst));
    }

    #[test]
    fn links_handle_spaces_unicode_and_shell_characters() {
        let d = tempfile::tempdir().unwrap();
        let target = d.path().join("lib/100%PATH% & 你好");
        let link_path = d.path().join("agent/70%TARGET% & 专业 skills/x");
        fs::create_dir_all(&target).unwrap();
        fs::write(target.join("SKILL.md"), "original").unwrap();
        link(&target, &link_path).unwrap();
        assert!(links_to(&link_path, &target));
        fs::write(target.join("SKILL.md"), "updated").unwrap();
        assert_eq!(
            fs::read_to_string(link_path.join("SKILL.md")).unwrap(),
            "updated"
        );
        unlink(&link_path).unwrap();
        assert!(target.join("SKILL.md").is_file());
        assert!(fs::symlink_metadata(link_path).is_err());
    }

    #[test]
    fn unlink_removes_a_link_when_its_target_is_missing() {
        let d = tempfile::tempdir().unwrap();
        let target = d.path().join("source");
        let installed = d.path().join("agent/skills/missing");
        fs::create_dir_all(&target).unwrap();
        link(&target, &installed).unwrap();
        fs::remove_dir(&target).unwrap();
        assert!(fs::symlink_metadata(&installed).is_ok());
        unlink(&installed).unwrap();
        assert!(fs::symlink_metadata(&installed).is_err());
    }

    #[test]
    fn unlink_removes_a_siu_made_link() {
        let d = tempfile::tempdir().unwrap();
        let target = d.path().join("lib/x");
        fs::create_dir_all(&target).unwrap();
        fs::write(target.join("SKILL.md"), "x").unwrap();
        let l = d.path().join("agent/skills/x");
        link(&target, &l).unwrap();
        assert!(fs::symlink_metadata(&l).is_ok());
        unlink(&l).unwrap();
        assert!(fs::symlink_metadata(&l).is_err());
        // the target itself is untouched
        assert!(target.join("SKILL.md").is_file());
    }
}
