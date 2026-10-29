package client

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// hostScript is the persistent host started by the pool (bridge/scripts/bridge_host.ps1).
const hostScript = "bridge_host.ps1"

// tokenRecycleMargin is how long before its access token expires a worker
// stops taking requests, when a newer token is available. MSAL renews a token
// from its cache 5 minutes before it expires, but an identity endpoint (IMDS,
// Azure Arc) can hand out a cached token that is already inside this margin:
// see acquire.
const tokenRecycleMargin = 3 * time.Minute

// workerStartTimeout bounds how long a new PowerShell process may take to
// report ready. Connecting to Teams happens later, on the first request.
const workerStartTimeout = 2 * time.Minute

// errWorkerUnavailable means the request never reached a worker, so it is safe
// to retry it on a fresh one.
var errWorkerUnavailable = errors.New("bridge worker unavailable")

// hostRequest and hostResponse mirror the JSON protocol documented in bridge_host.ps1.
type hostRequest struct {
	ID     int               `json:"id"`
	Script string            `json:"script"`
	Params map[string]string `json:"params"`
}

type hostResponse struct {
	ID       int    `json:"id"`
	Ready    bool   `json:"ready"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// pool keeps long-lived PowerShell processes ("workers") running bridge_host.ps1.
//
// Each worker opens its Teams connection on its first request and reuses it
// for the following ones. The pool never holds more workers than the client's
// concurrency limit, because a worker is only created or taken while holding
// one of the client's semaphore slots.
type pool struct {
	client      *Client
	maxRequests int
	idle        chan *worker // workers ready for a new request
}

func newPool(c *Client, size, maxRequests int) *pool {
	return &pool{client: c, maxRequests: maxRequests, idle: make(chan *worker, size)}
}

// run sends one request to a worker and returns the script's stdout.
// The caller must hold a semaphore slot.
func (p *pool) run(ctx context.Context, script string, params map[string]string) ([]byte, error) {
	resp, err := p.tryRun(ctx, script, params)
	if errors.Is(err, errWorkerUnavailable) && ctx.Err() == nil {
		// The request was never delivered (e.g. an idle worker had died), so a
		// retry cannot apply a change twice.
		resp, err = p.tryRun(ctx, script, params)
	}
	if err != nil {
		return nil, err
	}
	if resp.ExitCode != 0 {
		return nil, fmt.Errorf("PowerShell script %s failed: %s", script, strings.TrimSpace(resp.Stderr))
	}
	return []byte(resp.Stdout), nil
}

func (p *pool) tryRun(ctx context.Context, script string, params map[string]string) (*hostResponse, error) {
	w, err := p.acquire(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := w.call(ctx, script, params)
	switch {
	case err != nil:
		// Timeout, cancellation, crash or protocol error: the worker's state is
		// unknown, so it is never reused.
		w.kill()
	case resp.ExitCode != 0:
		// A failed script may leave a half-open or expired Teams session
		// behind. Recycle the worker: the next request reconnects cleanly.
		w.shutdown()
	case w.served >= p.maxRequests:
		// Recycle periodically to bound memory growth and session age.
		w.shutdown()
	default:
		p.release(w)
	}
	return resp, err
}

// acquire returns a live idle worker, or starts a new one.
//
// A worker connected with an access token that expires soon is retired here,
// just before it would take a request: Teams would refuse the request once
// the token expires, and the PowerShell session cannot renew the token by
// itself. Its replacement starts with a fresh token. When the token source
// has nothing newer, the worker is kept: its replacement would connect with
// the same token, and every request would restart PowerShell.
func (p *pool) acquire(ctx context.Context) (*worker, error) {
	for {
		select {
		case w := <-p.idle:
			if !w.alive() {
				w.kill()
				continue
			}
			if !w.tokenExpiresOn.IsZero() && time.Until(w.tokenExpiresOn) < tokenRecycleMargin && p.client.hasNewerToken(ctx, w.tokenExpiresOn) {
				w.shutdown()
				continue
			}
			return w, nil
		default:
			return p.client.startWorker(ctx)
		}
	}
}

// hasNewerToken reports whether the token source now returns a token that
// expires after expiresOn. An error counts as yes: the replacement worker
// then reports it, as any worker that cannot get a token does.
func (c *Client) hasNewerToken(ctx context.Context, expiresOn time.Time) bool {
	token, err := c.config.TokenSource.Token(ctx)
	return err != nil || token.ExpiresOn.After(expiresOn)
}

func (p *pool) release(w *worker) {
	select {
	case p.idle <- w:
	default:
		w.shutdown() // cannot happen with the semaphore in place, but never leak
	}
}

// worker is one long-lived PowerShell process running bridge_host.ps1.
type worker struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses chan hostResponse // closed when the process stdout reaches EOF
	stderr    *tailBuffer       // process-level stderr, for startup failures
	nonce     string
	nextID    int
	served    int
	closeOnce sync.Once

	// tokenExpiresOn is when the access token the process connected with
	// expires; zero with certificate authentication.
	tokenExpiresOn time.Time
}

// startWorker launches a new PowerShell host process and waits until it is ready.
func (c *Client) startWorker(ctx context.Context) (*worker, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, err
	}
	nonce := hex.EncodeToString(nonceBytes)

	env, tokenExpiresOn, err := c.buildEnv(ctx)
	if err != nil {
		return nil, err
	}
	binary, args := c.hostArgs()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(env, "TEAMS_BRIDGE_NONCE="+nonce)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{max: 8192}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: starting %s: %v", errWorkerUnavailable, binary, err)
	}

	w := &worker{cmd: cmd, stdin: stdin, responses: make(chan hostResponse, 1), stderr: stderr, nonce: nonce, tokenExpiresOn: tokenExpiresOn}
	go w.readLoop(stdout)

	// Wait for the {"id":0,"ready":true} handshake.
	timer := time.NewTimer(workerStartTimeout)
	defer timer.Stop()
	select {
	case resp, ok := <-w.responses:
		if !ok || !resp.Ready {
			w.kill()
			return nil, fmt.Errorf("PowerShell bridge host failed to start: %s", w.stderr.String())
		}
		return w, nil
	case <-timer.C:
		w.kill()
		return nil, fmt.Errorf("PowerShell bridge host did not start within %s: %s", workerStartTimeout, w.stderr.String())
	case <-ctx.Done():
		w.kill()
		return nil, ctx.Err()
	}
}

// readLoop turns protocol lines from stdout into responses. Lines without the
// nonce prefix are console noise and are dropped.
func (w *worker) readLoop(stdout io.Reader) {
	defer close(w.responses)
	reader := bufio.NewReader(stdout)
	prefix := w.nonce + " "
	for {
		line, err := reader.ReadString('\n')
		if strings.HasPrefix(line, prefix) {
			payload, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			var resp hostResponse
			if decErr == nil && json.Unmarshal(payload, &resp) == nil {
				w.responses <- resp
			} else {
				w.responses <- hostResponse{ID: -1}
			}
		}
		if err != nil {
			_ = w.cmd.Wait() // reap the process once its stdout is drained
			return
		}
	}
}

// call sends one request and waits for its response.
func (w *worker) call(ctx context.Context, script string, params map[string]string) (*hostResponse, error) {
	w.nextID++
	id := w.nextID
	raw, err := json.Marshal(hostRequest{ID: id, Script: filepath.Base(script), Params: params})
	if err != nil {
		return nil, err
	}
	line := base64.StdEncoding.EncodeToString(raw) + "\n"
	if _, err := io.WriteString(w.stdin, line); err != nil {
		return nil, fmt.Errorf("%w: %v", errWorkerUnavailable, err)
	}

	select {
	case resp, ok := <-w.responses:
		if !ok {
			return nil, fmt.Errorf("PowerShell bridge host exited while running %s: %s", filepath.Base(script), w.stderr.String())
		}
		if resp.ID != id {
			return nil, fmt.Errorf("PowerShell bridge protocol error: expected response %d, got %d", id, resp.ID)
		}
		w.served++
		return &resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// alive reports whether an idle worker's process is still running. An idle
// worker has no pending response, so anything readable means it is unusable.
func (w *worker) alive() bool {
	select {
	case <-w.responses:
		return false
	default:
		return true
	}
}

// shutdown closes stdin: the host disconnects from Teams and exits on its own.
func (w *worker) shutdown() {
	w.closeOnce.Do(func() { _ = w.stdin.Close() })
}

// kill stops the process immediately.
func (w *worker) kill() {
	w.shutdown()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
}

// tailBuffer is a concurrency-safe writer keeping only the last max bytes.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}
