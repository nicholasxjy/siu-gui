// Package backend multiplexes JSON-RPC-like requests from the mygo desktop
// service over a persistent, lazily started siu-core sidecar process
// communicating via newline-delimited JSON on stdin/stdout.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// maxScanTokenSize bounds a single NDJSON line; the protocol requires
// support for responses of at least 16MiB.
const maxScanTokenSize = 32 * 1024 * 1024

// maxStderrCapture bounds the diagnostic tail kept from the core's stderr
// for inclusion in process-exit error messages.
const maxStderrCapture = 64 * 1024

// shutdownGraceTimeout is how long Close waits for a graceful exit (via
// closing stdin) before forcibly killing the process tree. It is a var,
// not a const, so tests can shrink it.
var shutdownGraceTimeout = 2 * time.Second

// killProcessTreeFunc indirects to the platform killProcessTree so tests
// can simulate a failed kill without relying on real OS-level failures.
var killProcessTreeFunc = killProcessTree

// errClientClosed is returned by Request and ensureStarted once Close has
// been called; it is a var so both call sites compare/report identically.
var errClientClosed = errors.New("siu-core client is closed")

// Client talks to a persistent siu-core sidecar process. The zero value is
// not usable; construct with New or NewWithExecutable. A Client is safe for
// concurrent use.
type Client struct {
	explicitPath string

	mu            sync.Mutex // guards proc lifecycle (start/stop) below
	proc          *process
	generationSeq uint64

	nextID int64 // atomic

	pendingMu sync.Mutex
	pending   map[int64]pendingEntry
	closed    bool

	// ensureStartedHook, when non-nil, runs at the very top of ensureStarted
	// before c.mu is acquired. It exists solely so tests can deterministically
	// land a Request in the middle of ensureStarted while a concurrent Close
	// runs to completion, without relying on goroutine-scheduling luck.
	ensureStartedHook func()
}

type pendingEntry struct {
	ch         chan rpcResult
	generation uint64
}

type rpcResult struct {
	data string
	err  error
}

// process holds everything tied to one running siu-core instance (one
// "generation"). A Client may start several processes over its lifetime:
// one per (re)start after a crash.
type process struct {
	cmd        *exec.Cmd
	stdinMu    sync.Mutex
	stdin      io.WriteCloser
	stderrBuf  *boundedBuffer
	generation uint64

	done    chan struct{} // closed once the process has exited and been reaped
	exitErr error         // valid only after done is closed
}

type inboundPayload struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type outboundEnvelope struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// New creates a Client that lazily starts the embedded siu-core fallback
// (or the SIU_GUI_CORE override, if set) on first Request.
func New() *Client {
	return NewWithExecutable("")
}

// NewWithExecutable creates a Client that prefers the packaged sidecar at
// path (e.g. resolved via mygo.App.Path(PathResources), typically
// resources/<os>-<arch>/bin/siu-core(.exe)) over the embedded fallback. An
// empty path behaves exactly like New. SIU_GUI_CORE, when set, always wins
// over both, for tests and development.
func NewWithExecutable(path string) *Client {
	return &Client{
		explicitPath: path,
		pending:      make(map[int64]pendingEntry),
	}
}

// Request sends {method,params} from payload with a fresh monotonic id,
// waits for the matching NDJSON response, and returns its raw JSON text
// verbatim (including whatever "ok"/"data"/"error" shape siu-core sent).
// Error is non-nil only for client/transport-level failures: invalid
// payload, a closed client, an unexpected process exit, or ctx
// cancellation/timeout -- never for an application-level {ok:false}
// response, which callers parse themselves from the returned string.
func (c *Client) Request(ctx context.Context, payload string) (string, error) {
	var in inboundPayload
	if err := json.Unmarshal([]byte(payload), &in); err != nil {
		return "", fmt.Errorf("invalid request payload: %w", err)
	}
	if in.Method == "" {
		return "", errors.New("invalid request payload: method is required")
	}
	params := in.Params
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}

	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return "", errClientClosed
	}
	c.pendingMu.Unlock()

	p, err := c.ensureStarted()
	if err != nil {
		return "", err
	}

	id := atomic.AddInt64(&c.nextID, 1)
	line, err := json.Marshal(outboundEnvelope{ID: id, Method: in.Method, Params: params})
	if err != nil {
		return "", fmt.Errorf("encode request: %w", err)
	}
	line = append(line, '\n')

	ch := make(chan rpcResult, 1)
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return "", errClientClosed
	}
	c.pending[id] = pendingEntry{ch: ch, generation: p.generation}
	c.pendingMu.Unlock()

	cleanup := func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}

	p.stdinMu.Lock()
	_, writeErr := p.stdin.Write(line)
	p.stdinMu.Unlock()
	if writeErr != nil {
		cleanup()
		return "", fmt.Errorf("write request to siu-core: %w", writeErr)
	}

	select {
	case res := <-ch:
		return res.data, res.err
	case <-p.done:
		cleanup()
		return "", p.exitErr
	case <-ctx.Done():
		cleanup()
		return "", fmt.Errorf("request canceled or timed out before a response was received (%w); the operation's outcome is unknown, refresh state before assuming it did or did not apply", ctx.Err())
	}
}

// Close stops accepting new requests, fails any in-flight ones, and
// terminates the sidecar process tree. It is idempotent and safe to call
// even if no process was ever started.
func (c *Client) Close() error {
	c.pendingMu.Lock()
	if c.closed {
		c.pendingMu.Unlock()
		return nil
	}
	c.closed = true
	pendingCopy := c.pending
	c.pending = make(map[int64]pendingEntry)
	c.pendingMu.Unlock()

	closedErr := errors.New("siu-core client closed")
	for _, entry := range pendingCopy {
		select {
		case entry.ch <- rpcResult{err: closedErr}:
		default:
		}
	}

	c.mu.Lock()
	p := c.proc
	c.proc = nil
	c.mu.Unlock()

	if p == nil {
		return nil
	}

	_ = p.stdin.Close() // best-effort graceful signal; core may exit on stdin EOF

	select {
	case <-p.done:
		return nil
	case <-time.After(shutdownGraceTimeout):
	}

	if err := killProcessTreeFunc(p.cmd); err != nil {
		// Do not block Close on p.done here: if the kill itself failed, the
		// process may never exit, and waiting unboundedly would hang Close
		// forever. waitLoop (already running in its own goroutine since
		// startProcessLocked) still owns reaping it and will close p.done
		// and fail any pending requests for this generation whenever the
		// process does eventually die; Close just reports the failure now.
		return fmt.Errorf("terminate siu-core process: %w", err)
	}
	<-p.done
	return nil
}

// ensureStarted returns the currently running process, starting a new one
// if none is running or the previous one has already exited.
func (c *Client) ensureStarted() (*process, error) {
	if c.ensureStartedHook != nil {
		c.ensureStartedHook()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.proc != nil {
		select {
		case <-c.proc.done:
			c.proc = nil // previous process exited; start fresh below
		default:
			return c.proc, nil
		}
	}

	// Close releases pendingMu (after setting closed) before it acquires
	// c.mu to tear down c.proc. Re-checking closed here, now that we hold
	// c.mu, closes the window where a Request could pass its early closed
	// check, get descheduled, let a concurrent Close finish entirely (which
	// found c.proc nil and returned), and only then start a brand-new
	// process that nothing will ever stop again.
	c.pendingMu.Lock()
	closed := c.closed
	c.pendingMu.Unlock()
	if closed {
		return nil, errClientClosed
	}

	p, err := c.startProcessLocked()
	if err != nil {
		return nil, err
	}
	c.proc = p
	return p, nil
}

func (c *Client) startProcessLocked() (*process, error) {
	path, err := resolveCoreExecutable(c.explicitPath)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(path)
	cmd.Env = os.Environ() // pass through unchanged: never rewrite HOME/CODEX_HOME/etc.
	configurePlatform(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create siu-core stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create siu-core stdout pipe: %w", err)
	}
	stderrBuf := newBoundedBuffer(maxStderrCapture)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start siu-core process (%s): %w", path, err)
	}

	generation := atomic.AddUint64(&c.generationSeq, 1)
	p := &process{
		cmd:        cmd,
		stdin:      stdin,
		stderrBuf:  stderrBuf,
		generation: generation,
		done:       make(chan struct{}),
	}

	go c.readLoop(p, stdout)
	go c.waitLoop(p)

	return p, nil
}

// readLoop dispatches each NDJSON response line to its matching pending
// request by id. Malformed lines are protocol diagnostics (core should put
// those on stderr instead) and are simply skipped, not treated as fatal.
func (c *Client) readLoop(p *process, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxScanTokenSize)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var head struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		line := make([]byte, len(raw)) // scanner reuses its buffer
		copy(line, raw)
		c.dispatch(p.generation, head.ID, string(line))
	}
	// EOF/scan errors here just mean stdout closed; the authoritative
	// failure (with exit status) is handled by waitLoop via cmd.Wait().
}

func (c *Client) dispatch(generation uint64, id int64, raw string) {
	c.pendingMu.Lock()
	entry, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if !ok || entry.generation != generation {
		return // unknown, already-abandoned (canceled), or stale-generation id
	}
	entry.ch <- rpcResult{data: raw}
}

// waitLoop reaps the process and fails every pending request that was
// sent to this generation with an explicit, non-retrying error.
func (c *Client) waitLoop(p *process) {
	waitErr := p.cmd.Wait()
	p.exitErr = c.exitError(p, waitErr)
	close(p.done)
	c.failPendingForGeneration(p.generation, p.exitErr)
}

func (c *Client) exitError(p *process, waitErr error) error {
	msg := "siu-core process exited unexpectedly"
	if waitErr != nil {
		msg += ": " + waitErr.Error()
	}
	if tail := p.stderrBuf.String(); tail != "" {
		msg += "; stderr: " + tail
	}
	msg += "; no changes were retried automatically, refresh state before retrying"
	return errors.New(msg)
}

func (c *Client) failPendingForGeneration(generation uint64, err error) {
	c.pendingMu.Lock()
	var toFail []chan rpcResult
	for id, entry := range c.pending {
		if entry.generation == generation {
			toFail = append(toFail, entry.ch)
			delete(c.pending, id)
		}
	}
	c.pendingMu.Unlock()
	for _, ch := range toFail {
		select {
		case ch <- rpcResult{err: err}:
		default:
		}
	}
}
