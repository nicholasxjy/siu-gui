package backend

import (
	"strings"
	"sync"
)

// boundedBuffer captures the tail of a stream up to a fixed byte budget.
// It is used only for the core process's own stderr diagnostics, never for
// request/response payloads, which may contain credentials.
type boundedBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newBoundedBuffer(max int) *boundedBuffer {
	return &boundedBuffer{max: max}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
