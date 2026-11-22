package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SkillMeta holds front-matter metadata extracted from SKILL.md.
type SkillMeta struct {
	Name        string
	Description string
}

// ParseSkillMeta parses YAML front matter `name` and `description` from SKILL.md.
func ParseSkillMeta(text string) SkillMeta {
	var meta SkillMeta
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta
	}
	current := ""
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if current != "" {
				v := strings.TrimSpace(line)
				if v != "" {
					if current == "name" {
						if meta.Name != "" {
							meta.Name += " "
						}
						meta.Name += v
					} else {
						if meta.Description != "" {
							meta.Description += " "
						}
						meta.Description += v
					}
				}
			}
			continue
		}
		current = ""
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if k != "name" && k != "description" {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" || v == ">" || v == "|" || v == ">-" || v == "|-" {
			current = k
			continue
		}
		u := unquoteFrontMatter(v)
		if k == "name" {
			meta.Name = u
		} else {
			meta.Description = u
		}
	}
	return meta
}

func unquoteFrontMatter(v string) string {
	v = strings.TrimSpace(v)
	for _, q := range []byte{'"', '\''} {
		if len(v) >= 2 && v[0] == q && v[len(v)-1] == q {
			inner := v[1 : len(v)-1]
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, "''", "'")
			return inner
		}
	}
	return v
}

// FoundSkillFile represents a skill discovered inside a source tree.
type FoundSkillFile struct {
	Path string
	Dir  string
	Meta SkillMeta
}

// Name returns the skill's sanitized name (from front matter or folder name).
func (f FoundSkillFile) Name() string {
	n := SanitizeSkillName(f.Meta.Name)
	if n != "" {
		return n
	}
	return SanitizeSkillName(lastPathPart(f.Path))
}

func lastPathPart(p string) string {
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

// SanitizeSkillName normalizes a name into a safe lowercase key/folder name.
func SanitizeSkillName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteByte('-')
		}
	}
	trimmed := strings.Trim(b.String(), "-_")
	if len(trimmed) > 64 {
		trimmed = trimmed[:64]
	}
	return trimmed
}

// FindSkills locates every folder containing SKILL.md under root (up to depth 6),
// not descending inside a skill's own folder.
func FindSkills(root string) []FoundSkillFile {
	var out []FoundSkillFile
	walkSkills(root, root, 0, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func walkSkills(root, dir string, depth int, out *[]FoundSkillFile) {
	if depth > 6 {
		return
	}
	skillMD := filepath.Join(dir, "SKILL.md")
	if isFile(skillMD) {
		b, _ := os.ReadFile(skillMD)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}
		if rel == "." {
			rel = ""
		}
		rel = filepath.ToSlash(rel)
		*out = append(*out, FoundSkillFile{
			Path: rel,
			Dir:  dir,
			Meta: ParseSkillMeta(string(b)),
		})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "target" {
			continue
		}
		if e.IsDir() {
			walkSkills(root, filepath.Join(dir, name), depth+1, out)
		}
	}
}

// HashDir computes a deterministic 16-hex-char SHA-256 prefix over all file
// paths and contents inside dir (skipping .git).
func HashDir(dir string) string {
	var files []string
	collectHashFiles(dir, dir, &files)
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		h.Write([]byte(rel))
		h.Write([]byte{0})
		data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		h.Write(data)
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:8])
}

func collectHashFiles(root, dir string, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		mode := e.Type()
		if mode.IsDir() {
			if name != ".git" {
				collectHashFiles(root, p, out)
			}
		} else if mode.IsRegular() {
			rel, err := filepath.Rel(root, p)
			if err != nil {
				rel = p
			}
			*out = append(*out, filepath.ToSlash(rel))
		}
	}
}

// CopyDir copies src to dst recursively, skipping .git and dereferencing file symlinks.
func CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("%s: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		from := filepath.Join(src, name)
		to := filepath.Join(dst, name)
		mode := e.Type()
		if mode.IsDir() {
			if name != ".git" {
				if err := CopyDir(from, to); err != nil {
					return err
				}
			}
		} else if mode.IsRegular() {
			if err := copyFile(from, to); err != nil {
				return err
			}
		} else if mode&os.ModeSymlink != 0 {
			if info, err := os.Stat(from); err == nil && info.Mode().IsRegular() {
				if err := copyFile(from, to); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("%s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("%s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("%s: %w", dst, err)
	}
	return nil
}

// ReplaceDir atomically replaces dst with a copy of src via a sibling folder.
func ReplaceDir(src, dst string) error {
	tmp := dst + ".siu-new"
	_ = os.RemoveAll(tmp)
	if err := CopyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// LinksTo reports whether p is a symlink/junction pointing to target.
func LinksTo(p, target string) bool {
	t, err := os.Readlink(p)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(t) {
		t = filepath.Join(filepath.Dir(p), t)
	}
	return samePath(t, target)
}

func samePath(a, b string) bool {
	ca, errA := filepath.EvalSymlinks(a)
	cb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return ca == cb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func isLink(p string) bool {
	_, err := os.Readlink(p)
	return err == nil
}
