package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/workplace-as-code/terraform-provider-msteams/bridge"
)

// Environment variable names used to pass secrets to the bridge scripts.
//
// Secrets are never put on the command line: process command lines are readable
// by every local user (ps, /proc/<pid>/cmdline) and are recorded by Windows
// process-creation auditing (event 4688) and by most EDR agents. A child
// process environment is only readable by the same user or an administrator.
const EnvBridgeCertificatePassword = "TEAMS_BRIDGE_CERTIFICATE_PASSWORD"

// Tunables, overridable through environment variables.
const (
	// EnvBridgeDir points the provider at an on-disk copy of the bridge scripts
	// instead of the embedded ones. Useful when editing PowerShell locally
	// without rebuilding the provider.
	EnvBridgeDir = "TEAMS_BRIDGE_DIR"
	// EnvScriptTimeout is a Go duration string (e.g. "10m") capping one script run.
	EnvScriptTimeout = "TEAMS_SCRIPT_TIMEOUT"
	// EnvMaxConcurrency caps how many PowerShell processes run at once.
	EnvMaxConcurrency = "TEAMS_MAX_CONCURRENCY"
	// EnvBridgeMode selects how scripts are run: "persistent" (default) reuses
	// long-lived PowerShell processes; "oneshot" starts one process per call.
	EnvBridgeMode = "TEAMS_BRIDGE_MODE"
	// EnvWorkerMaxRequests is how many requests a persistent process serves
	// before it is replaced by a fresh one.
	EnvWorkerMaxRequests = "TEAMS_WORKER_MAX_REQUESTS"

	DefaultScriptTimeout     = 10 * time.Minute
	DefaultMaxConcurrency    = 4
	DefaultWorkerMaxRequests = 200

	BridgeModePersistent = "persistent"
	BridgeModeOneShot    = "oneshot"
)

// Config holds the authentication parameters for the provider.
// Teams is cloud-only: there is no on-premises mode.
type Config struct {
	TenantID              string
	ClientID              string
	CertificatePath       string
	CertificatePassword   string
	CertificateThumbprint string
	Environment           string
}

// Client is the main provider client.
// It orchestrates calls to the PowerShell bridge scripts.
type Client struct {
	config    Config
	scriptDir string
	timeout   time.Duration

	// sem is a counting semaphore: a buffered channel whose capacity is the
	// maximum number of concurrent PowerShell processes. Terraform runs up to 10
	// operations in parallel by default, and each one opens its own Teams
	// session; without a cap that quickly hits Teams PowerShell throttling.
	sem chan struct{}

	// pool holds the persistent PowerShell processes. It is nil in one-shot mode.
	pool *pool

	// writeLocks serializes write operations per kind of Teams object (script
	// name, or cmdlet noun for the generic scripts -> chan struct{} of capacity 1). Teams keeps all policies of one type in a
	// single tenant document with optimistic concurrency, so two policies of the
	// same type created in parallel fail with "409 Conflict: Item has changed".
	// Reads stay parallel.
	writeLocks sync.Map
}

// New creates and validates a new Teams provider client.
// It requires pwsh (PowerShell 7+) with the MicrosoftTeams module, on Linux,
// macOS or Windows.
func New(ctx context.Context, config Config) (*Client, error) {
	if err := checkBinary(ctx, "pwsh"); err != nil {
		return nil, fmt.Errorf("the Microsoft Teams provider requires PowerShell 7+ (pwsh), install it from https://aka.ms/install-powershell: %w", err)
	}

	scriptDir, err := resolveScriptDir()
	if err != nil {
		return nil, fmt.Errorf("preparing PowerShell bridge scripts: %w", err)
	}

	timeout, err := durationFromEnv(EnvScriptTimeout, DefaultScriptTimeout)
	if err != nil {
		return nil, err
	}
	concurrency, err := intFromEnv(EnvMaxConcurrency, DefaultMaxConcurrency)
	if err != nil {
		return nil, err
	}
	maxRequests, err := intFromEnv(EnvWorkerMaxRequests, DefaultWorkerMaxRequests)
	if err != nil {
		return nil, err
	}

	c := &Client{
		config:    config,
		scriptDir: scriptDir,
		timeout:   timeout,
		sem:       make(chan struct{}, concurrency),
	}

	switch mode := os.Getenv(EnvBridgeMode); mode {
	case "", BridgeModePersistent:
		c.pool = newPool(c, concurrency, maxRequests)
	case BridgeModeOneShot:
		// keep c.pool nil
	default:
		return nil, fmt.Errorf("%s must be %q or %q, got %q", EnvBridgeMode, BridgeModePersistent, BridgeModeOneShot, mode)
	}
	return c, nil
}

// checkBinary verifies that a PowerShell executable is on the PATH.
func checkBinary(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, name, "-NoProfile", "-NonInteractive", "-Command", "exit 0").Run(); err != nil {
		return fmt.Errorf("%s not usable: %w", name, err)
	}
	return nil
}

// RunScript executes a PowerShell bridge script and returns its stdout (JSON).
//
// script is the bare file name, e.g. "transport_rule.ps1". In persistent mode
// the request goes to a long-lived PowerShell process that keeps its Teams
// connection open; in one-shot mode a new process is started for this call.
// Either way the run is bounded by the caller's context (Terraform cancels it
// on Ctrl+C) and by the configured timeout.
func (c *Client) RunScript(ctx context.Context, script string, params map[string]string) ([]byte, error) {
	name := filepath.Base(script)

	// Writes to the same kind of Teams object run one at a time (see writeLocks).
	// The lock is taken before the semaphore so a waiting write does not hold
	// a PowerShell slot that a read could use.
	if action := params["Action"]; action != "" && action != "Get" {
		// Generic scripts (cs_object.ps1) handle several object types: lock per
		// cmdlet noun so that writes to different policy types stay parallel.
		key := name
		if noun := params["Noun"]; noun != "" {
			key = noun
		}
		lockAny, _ := c.writeLocks.LoadOrStore(key, make(chan struct{}, 1))
		lock := lockAny.(chan struct{})
		select {
		case lock <- struct{}{}:
			defer func() { <-lock }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// Acquire a slot in the semaphore, or give up if the context is cancelled.
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var out []byte
	var err error
	if c.pool != nil {
		out, err = c.pool.run(ctx, name, params)
	} else {
		out, err = c.runOneShot(ctx, name, params)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("PowerShell script %s timed out after %s (set %s to raise the limit)", name, c.timeout, EnvScriptTimeout)
	}
	return out, err
}

// runOneShot starts a dedicated PowerShell process for a single script run.
func (c *Client) runOneShot(ctx context.Context, name string, params map[string]string) ([]byte, error) {
	binary, args := c.buildArgs(filepath.Join(c.scriptDir, name), params)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = c.buildEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("PowerShell script %s failed: %s", name, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("failed to run PowerShell script %s: %w", name, err)
	}
	return stdout.Bytes(), nil
}

// connectionArgs returns the PowerShell binary and the command line that runs
// scriptPath with the non-secret connection parameters. Secrets are
// deliberately absent: they travel through the environment (see buildEnv).
func (c *Client) connectionArgs(scriptPath string) (string, []string) {
	return "pwsh", []string{
		"-NonInteractive", "-NoProfile",
		"-File", scriptPath,
		"-TenantId", c.config.TenantID,
		"-ClientId", c.config.ClientID,
		"-CertificatePath", c.config.CertificatePath,
		"-CertificateThumbprint", c.config.CertificateThumbprint,
		"-Environment", c.config.Environment,
	}
}

// hostArgs is the command line of a persistent bridge host process.
func (c *Client) hostArgs() (string, []string) {
	return c.connectionArgs(filepath.Join(c.scriptDir, hostScript))
}

// buildArgs is the one-shot command line: connection parameters followed by
// the script's own parameters.
func (c *Client) buildArgs(scriptPath string, params map[string]string) (string, []string) {
	binary, args := c.connectionArgs(scriptPath)

	// Sort keys so the command line is deterministic (easier to debug and test).
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-"+k, params[k])
	}
	return binary, args
}

// buildEnv returns the parent environment plus the secrets for auth.ps1.
func (c *Client) buildEnv() []string {
	// NO_COLOR stops PowerShell 7 from wrapping error messages in ANSI escape
	// codes, which would otherwise end up in Terraform diagnostics.
	return append(os.Environ(), "NO_COLOR=1", EnvBridgeCertificatePassword+"="+c.config.CertificatePassword)
}

// IsNotFound reports whether a bridge "Get" call signalled that the object no
// longer exists. By contract, bridge scripts exit 0 with empty stdout only in
// that case; every other failure exits non-zero and surfaces as an error.
// Callers must treat any other unparsable output as an error, never as "gone",
// otherwise a transient failure would silently drop the resource from state.
func IsNotFound(output []byte) bool {
	return len(bytes.TrimSpace(output)) == 0
}

// resolveScriptDir returns the directory the bridge scripts are run from.
func resolveScriptDir() (string, error) {
	if dir := os.Getenv(EnvBridgeDir); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "auth.ps1")); err != nil {
			return "", fmt.Errorf("%s=%q does not contain auth.ps1: %w", EnvBridgeDir, dir, err)
		}
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return extractScripts(base)
}

// extractScripts writes the embedded scripts under
// <base>/terraform-provider-msteams/<content-hash>/ and returns that path.
//
// The directory name is a hash of the scripts, so different provider versions
// never overwrite each other. Files are rewritten on every start through an
// atomic rename, so a modified or truncated copy on disk is always replaced by
// the trusted embedded content. Permissions are owner-only (0700/0600).
func extractScripts(base string) (string, error) {
	sub, err := fs.Sub(bridge.Scripts, bridge.ScriptsRoot)
	if err != nil {
		return "", err
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return "", err
	}

	files := make(map[string][]byte, len(entries))
	h := sha256.New()
	for _, e := range entries {
		data, err := fs.ReadFile(sub, e.Name())
		if err != nil {
			return "", err
		}
		files[e.Name()] = data
		h.Write([]byte(e.Name()))
		h.Write(data)
	}
	dir := filepath.Join(base, "terraform-provider-msteams", hex.EncodeToString(h.Sum(nil))[:16])

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}

	for name, data := range files {
		tmp, err := os.CreateTemp(dir, name+".*.tmp")
		if err != nil {
			return "", err
		}
		_, werr := tmp.Write(data)
		cerr := tmp.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(tmp.Name())
			return "", errors.Join(werr, cerr)
		}
		if err := os.Chmod(tmp.Name(), 0o600); err != nil {
			_ = os.Remove(tmp.Name())
			return "", err
		}
		if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
			_ = os.Remove(tmp.Name())
			return "", err
		}
	}
	return dir, nil
}

func durationFromEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration such as \"10m\", got %q", key, v)
	}
	return d, nil
}

func intFromEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, v)
	}
	return n, nil
}
