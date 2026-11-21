package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type stampedCache[T any] struct {
	At   uint64 `json:"at"`
	Data T      `json:"data"`
}

// Cached holds a cached payload and whether it is still within maxAge.
type Cached[T any] struct {
	Data  T
	Fresh bool
}

// CachePath returns the JSON cache file path for name inside dir.
func CachePath(dir, name string) string {
	return filepath.Join(dir, name+".json")
}

// ReadCache reads a stamped JSON cache file; returns nil if absent or invalid.
func ReadCache[T any](dir, name string, maxAge time.Duration) *Cached[T] {
	raw, err := os.ReadFile(CachePath(dir, name))
	if err != nil {
		return nil
	}
	var s stampedCache[T]
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	now := NowUnix()
	var age uint64
	if now >= s.At {
		age = now - s.At
	}
	fresh := age < uint64(maxAge.Seconds())
	return &Cached[T]{Data: s.Data, Fresh: fresh}
}

// WriteCache atomically writes a stamped cache file; failures are ignored.
func WriteCache[T any](dir, name string, data T) {
	raw, err := json.Marshal(stampedCache[T]{At: NowUnix(), Data: data})
	if err != nil {
		return
	}
	_ = WriteAtomic(CachePath(dir, name), raw)
}
