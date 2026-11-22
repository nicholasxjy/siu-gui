package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func registryCacheKey(query string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(query))))
	return "mcp-registry-" + hex.EncodeToString(sum[:])
}

// SearchRegistryCached searches the MCP Registry with a 1-hour disk cache and
// offline fallback, trimming old cache entries beyond 100 files.
func SearchRegistryCached(
	dir string,
	query string,
	refresh bool,
	fetch func() ([]MarketServer, error),
) (servers []MarketServer, warnings []string, cached bool) {
	key := registryCacheKey(query)
	prev := ReadCache[[]MarketServer](dir, key, time.Hour)
	if !refresh && prev != nil && prev.Fresh {
		return prev.Data, nil, true
	}
	list, err := fetch()
	if err == nil {
		WriteCache(dir, key, list)
		trimRegistryCache(dir)
		return list, nil, false
	}
	if prev != nil {
		return prev.Data, []string{fmt.Sprintf("MCP Registry: %v; using cached results", err)}, true
	}
	return nil, []string{fmt.Sprintf("MCP Registry: %v", err)}, false
}

func trimRegistryCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type timedFile struct {
		mod  time.Time
		path string
	}
	var files []timedFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "mcp-registry-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, timedFile{mod: info.ModTime(), path: filepath.Join(dir, name)})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.Before(files[j].mod)
	})
	if len(files) > 100 {
		for _, f := range files[:len(files)-100] {
			_ = os.Remove(f.path)
		}
	}
}
