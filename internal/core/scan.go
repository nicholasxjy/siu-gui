package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ScanTTL = 30 * time.Minute
	ScanCap = 8
)

// ScanOrigin identifies whether a retained scan came from GitHub or a local dir.
type ScanOrigin struct {
	Kind      string // "github" or "local"
	Repo      string
	Reference string
}

// ScanEntry holds a retained scan_skills result and optional temp dir to clean up.
type ScanEntry struct {
	Source  string
	Root    string
	Origin  ScanOrigin
	Found   []FoundSkillFile
	created time.Time
	tempDir string
}

// ScanManager retains scan tokens across scan_skills -> install_skills calls,
// expiring unused tokens after ScanTTL and capping retained entries at ScanCap.
type ScanManager struct {
	mu      sync.Mutex
	entries map[string]*ScanEntry
}

var scanCounter atomic.Uint64

func NewScanManager() *ScanManager {
	return &ScanManager{entries: map[string]*ScanEntry{}}
}

func nextScanToken() string {
	c := scanCounter.Add(1)
	nanos := uint64(time.Now().UnixNano())
	pid := uint32(os.Getpid())
	var buf [20]byte
	binary.LittleEndian.PutUint64(buf[0:8], nanos)
	binary.LittleEndian.PutUint64(buf[8:16], c)
	binary.LittleEndian.PutUint32(buf[16:20], pid)
	sum := sha256.Sum256(buf[:])
	return hex.EncodeToString(sum[:16])
}

func (m *ScanManager) sweepLocked(now time.Time) {
	for tok, e := range m.entries {
		if now.Sub(e.created) >= ScanTTL {
			if e.tempDir != "" {
				_ = os.RemoveAll(e.tempDir)
			}
			delete(m.entries, tok)
		}
	}
}

// Insert stores a scan result and returns its token.
func (m *ScanManager) Insert(source, root string, origin ScanOrigin, found []FoundSkillFile, tempDir string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	m.sweepLocked(now)
	for len(m.entries) >= ScanCap {
		var oldestTok string
		var oldestTime time.Time
		first := true
		for tok, e := range m.entries {
			if first || e.created.Before(oldestTime) {
				oldestTok = tok
				oldestTime = e.created
				first = false
			}
		}
		if first {
			break
		}
		if evicted := m.entries[oldestTok]; evicted != nil && evicted.tempDir != "" {
			_ = os.RemoveAll(evicted.tempDir)
		}
		delete(m.entries, oldestTok)
	}
	tok := nextScanToken()
	m.entries[tok] = &ScanEntry{
		Source:  source,
		Root:    root,
		Origin:  origin,
		Found:   found,
		created: now,
		tempDir: tempDir,
	}
	return tok
}

// With runs fn with the entry for token if present and unexpired.
func (m *ScanManager) With(token string, fn func(*ScanEntry)) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(time.Now())
	e, ok := m.entries[token]
	if !ok {
		return false
	}
	fn(e)
	return true
}

// Release removes a retained scan and deletes its temp directory.
func (m *ScanManager) Release(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[token]
	if !ok {
		return false
	}
	if e.tempDir != "" {
		_ = os.RemoveAll(e.tempDir)
	}
	delete(m.entries, token)
	return true
}

// Close removes all retained scan temporary directories (e.g. on app exit).
func (m *ScanManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for tok, e := range m.entries {
		if e.tempDir != "" {
			_ = os.RemoveAll(e.tempDir)
		}
		delete(m.entries, tok)
	}
}
