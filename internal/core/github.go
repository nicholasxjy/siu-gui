package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SpecKind distinguishes GitHub repositories from local directories.
type SpecKind int

const (
	SpecGithub SpecKind = iota
	SpecLocal
)

// Spec identifies where skills are to be scanned from.
type Spec struct {
	Kind      SpecKind
	Repo      string
	Reference string
	Subpath   string
	LocalDir  string
}

func isWindowsPath(s string) bool {
	if len(s) >= 3 && ((s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')) &&
		s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	return strings.HasPrefix(s, `\\`)
}

// ParseSpec parses `owner/repo`, `owner/repo/path`, a github.com URL (with
// `/tree/<ref>/<path>` or `/blob/<ref>/<path>`), a Windows path, or a local folder.
func ParseSpec(input, home string) (Spec, bool) {
	s := strings.TrimSpace(input)
	s = strings.TrimRight(s, `/\`)
	if s == "" {
		return Spec{}, false
	}
	looksLocal := strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "~/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, `.\`) ||
		s == "~" ||
		isWindowsPath(s) ||
		strings.Contains(s, `\`)
	if looksLocal {
		s = stripTrailingSkillMD(s)
		var p string
		if rest, ok := strings.CutPrefix(s, "~"); ok {
			p = filepath.Join(home, strings.TrimLeft(rest, `/\`))
		} else {
			p = s
		}
		return Spec{Kind: SpecLocal, LocalDir: p}, true
	}

	s = strings.TrimSuffix(s, ".git")
	rest := s
	for _, pfx := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if after, ok := strings.CutPrefix(rest, pfx); ok {
			rest = after
			break
		}
	}
	rawParts := strings.Split(rest, "/")
	var parts []string
	for _, p := range rawParts {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) < 2 {
		return Spec{}, false
	}
	for _, p := range parts[:2] {
		if !isRepoSegment(p) {
			return Spec{}, false
		}
	}
	repo := parts[0] + "/" + parts[1]
	ref := "HEAD"
	var subParts []string
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		ref = parts[3]
		subParts = parts[4:]
	} else {
		subParts = parts[2:]
	}
	subpath := strings.Join(subParts, "/")
	subpath = strings.TrimSuffix(subpath, "/SKILL.md")
	subpath = strings.TrimSuffix(subpath, "SKILL.md")
	subpath = strings.TrimRight(subpath, "/")
	return Spec{
		Kind:      SpecGithub,
		Repo:      repo,
		Reference: ref,
		Subpath:   subpath,
	}, true
}

func isRepoSegment(p string) bool {
	if p == "" {
		return false
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if !isASCIIAlnum(c) && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

func stripTrailingSkillMD(s string) string {
	suffixLen := len("SKILL.md") + 1
	if len(s) > suffixLen {
		head := s[:len(s)-suffixLen]
		tail := s[len(s)-suffixLen:]
		if (tail[0] == '/' || tail[0] == '\\') && strings.EqualFold(tail[1:], "SKILL.md") {
			return head
		}
	}
	return s
}

// DownloadGitHub downloads a GitHub repository tarball and unpacks it into a
// new temporary directory. Caller owns cleaning up tempDir.
func DownloadGitHub(ctx context.Context, repo, ref string) (tempDir string, repoRoot string, err error) {
	rawURL := fmt.Sprintf("https://codeload.github.com/%s/tar.gz/%s", repo, URLEncode(ref))
	data, err := HTTPGet(ctx, rawURL, "application/gzip", 256<<20)
	if err != nil {
		return "", "", fmt.Errorf("couldn't download %s: %w", repo, err)
	}
	tmp, err := os.MkdirTemp("", "siu-*")
	if err != nil {
		return "", "", err
	}
	if err := unpackTarGz(data, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", "", fmt.Errorf("couldn't unpack %s: %w", repo, err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", "", err
	}
	for _, e := range entries {
		if e.IsDir() {
			return tmp, filepath.Join(tmp, e.Name()), nil
		}
	}
	_ = os.RemoveAll(tmp)
	return "", "", fmt.Errorf("%s's tarball was empty", repo)
}

func unpackTarGz(data []byte, dst string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	const maxTotalBytes = 512 << 20
	var totalBytes int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeXHeader || hdr.Typeflag == tar.TypeXGlobalHeader || hdr.Typeflag == tar.TypeGNULongName || hdr.Typeflag == tar.TypeGNULongLink {
			continue
		}
		clean := filepath.Clean(filepath.FromSlash(hdr.Name))
		if clean == "." || clean == "" {
			continue
		}
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive path %q", hdr.Name)
		}
		target := filepath.Join(absDst, clean)
		if !pathHasPrefix(target, absDst) {
			return fmt.Errorf("archive path escapes root: %q", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(f, io.LimitReader(tr, maxTotalBytes-totalBytes+1))
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			totalBytes += n
			if totalBytes > maxTotalBytes {
				return errors.New("tarball exceeds size limit")
			}
		case tar.TypeSymlink:
			// Only allow relative symlinks that stay inside the unpacked tree.
			if filepath.IsAbs(hdr.Linkname) {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(target), filepath.FromSlash(hdr.Linkname)))
			if !pathHasPrefix(resolved, absDst) {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			_ = os.Symlink(hdr.Linkname, target)
		}
	}
	return nil
}
