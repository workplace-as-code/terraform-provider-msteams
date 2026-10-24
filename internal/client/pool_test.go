package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/workplace-as-code/terraform-provider-msteams/bridge"
)

// ---------------------------------------------------------------------------
// Fake host: the test binary re-executed as "pwsh". It speaks the
// bridge_host.ps1 protocol so pool behavior can be tested without PowerShell.
// ---------------------------------------------------------------------------

func TestHelperFakeHost(t *testing.T) {
	if os.Getenv("FAKE_HOST") != "1" {
		t.Skip("helper process")
	}
	nonce := os.Getenv("TEAMS_BRIDGE_NONCE")
	if os.Getenv("FAKE_NO_READY") == "1" {
		fmt.Fprintln(os.Stderr, "module import failed")
		os.Exit(1)
	}
	send := func(r hostResponse) {
		raw, _ := json.Marshal(r)
		fmt.Printf("%s %s\n", nonce, base64.StdEncoding.EncodeToString(raw))
	}
	send(hostResponse{ID: 0, Ready: true})

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		raw, _ := base64.StdEncoding.DecodeString(in.Text())
		var req hostRequest
		_ = json.Unmarshal(raw, &req)
		switch req.Script {
		case "fail.ps1":
			send(hostResponse{ID: req.ID, ExitCode: 1, Stderr: "throttled by Teams"})
		case "sleep.ps1":
			time.Sleep(30 * time.Second)
		case "die.ps1":
			os.Exit(3)
		case "noise.ps1":
			fmt.Println("WARNING: text written straight to the console")
			send(hostResponse{ID: req.ID, Stdout: `{"ok":true}`})
		case "secret.ps1":
			send(hostResponse{ID: req.ID, Stdout: os.Getenv(EnvBridgeCertificatePassword)})
		default: // echo: report the process id so tests can check reuse
			send(hostResponse{ID: req.ID, Stdout: fmt.Sprintf("%d %s", os.Getpid(), req.Params["Action"])})
		}
	}
	os.Exit(0)
}

func fakeHostClient(t *testing.T, size, maxRequests int, timeout time.Duration) *Client {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake pwsh uses a POSIX shell script")
	}
	dir := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\nFAKE_HOST=1 exec %q -test.run=TestHelperFakeHost\n", os.Args[0])
	if err := os.WriteFile(filepath.Join(dir, "pwsh"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := &Client{config: Config{CertificatePassword: "pfx-secret"}, scriptDir: dir, timeout: timeout, sem: make(chan struct{}, size)}
	c.pool = newPool(c, size, maxRequests)
	t.Cleanup(func() {
		for {
			select {
			case w := <-c.pool.idle:
				w.kill()
			default:
				return
			}
		}
	})
	return c
}

func run(t *testing.T, c *Client, script string) (string, error) {
	t.Helper()
	out, err := c.RunScript(context.Background(), script, map[string]string{"Action": "Get"})
	return string(out), err
}

func pidOf(t *testing.T, out string) string {
	t.Helper()
	return strings.Fields(out)[0]
}

func TestPoolReusesProcess(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	first, err := run(t, c, "echo.ps1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		out, err := run(t, c, "echo.ps1")
		if err != nil {
			t.Fatal(err)
		}
		if pidOf(t, out) != pidOf(t, first) {
			t.Fatalf("request %d ran in a new process", i)
		}
	}
}

func TestPoolRecyclesAfterMaxRequests(t *testing.T) {
	c := fakeHostClient(t, 1, 2, time.Minute)
	a, _ := run(t, c, "echo.ps1")
	b, _ := run(t, c, "echo.ps1")
	d, _ := run(t, c, "echo.ps1")
	if pidOf(t, a) != pidOf(t, b) || pidOf(t, b) == pidOf(t, d) {
		t.Fatalf("expected reuse then recycle, got pids %s %s %s", pidOf(t, a), pidOf(t, b), pidOf(t, d))
	}
}

func TestPoolScriptFailureRecyclesWorker(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	before, _ := run(t, c, "echo.ps1")
	_, err := run(t, c, "fail.ps1")
	if err == nil || !strings.Contains(err.Error(), "throttled by Teams") {
		t.Fatalf("expected script stderr in error, got %v", err)
	}
	after, err := run(t, c, "echo.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if pidOf(t, before) == pidOf(t, after) {
		t.Fatal("worker was reused after a failed script")
	}
}

func TestPoolIgnoresConsoleNoise(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	out, err := run(t, c, "noise.ps1")
	if err != nil || out != `{"ok":true}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestPoolCrashMidRequestIsAnError(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	if _, err := run(t, c, "die.ps1"); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatalf("expected crash error, got %v", err)
	}
	if _, err := run(t, c, "echo.ps1"); err != nil {
		t.Fatalf("pool did not recover after a crash: %v", err)
	}
}

func TestPoolTimeoutKillsWorker(t *testing.T) {
	c := fakeHostClient(t, 1, 100, 500*time.Millisecond)
	start := time.Now()
	_, err := run(t, c, "sleep.ps1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not interrupt the request")
	}
	if len(c.pool.idle) != 0 {
		t.Fatal("timed-out worker was returned to the pool")
	}
}

func TestPoolStartupFailureReportsStderr(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	t.Setenv("FAKE_NO_READY", "1")
	_, err := run(t, c, "echo.ps1")
	if err == nil || !strings.Contains(err.Error(), "module import failed") {
		t.Fatalf("expected startup stderr, got %v", err)
	}
}

func TestPoolPassesSecretsThroughEnvironment(t *testing.T) {
	c := fakeHostClient(t, 1, 100, time.Minute)
	out, err := run(t, c, "secret.ps1")
	if err != nil || out != "pfx-secret" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	_, args := c.hostArgs()
	if strings.Contains(strings.Join(args, " "), "pfx-secret") {
		t.Fatal("secret leaked into the host command line")
	}
}

func TestPoolNeverExceedsConcurrency(t *testing.T) {
	c := fakeHostClient(t, 2, 100, time.Minute)
	pids := make(chan string, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			out, err := c.RunScript(context.Background(), "echo.ps1", nil)
			if err != nil {
				errs <- err
				return
			}
			pids <- strings.Fields(string(out))[0]
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		select {
		case p := <-pids:
			seen[p] = true
		case err := <-errs:
			t.Fatal(err)
		}
	}
	if len(seen) > 2 {
		t.Fatalf("%d processes used, limit is 2", len(seen))
	}
}

// ---------------------------------------------------------------------------
// Real host: bridge_host.ps1 run by the real pwsh, with stub scripts in place
// of Teams. Skipped when pwsh is not installed.
// ---------------------------------------------------------------------------

func realHostClient(t *testing.T, scripts map[string]string) *Client {
	t.Helper()
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not installed")
	}
	dir := t.TempDir()
	host, err := fs.ReadFile(bridge.Scripts, bridge.ScriptsRoot+"/"+hostScript)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{hostScript: string(host)}
	for k, v := range scripts {
		files[k] = v
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c := &Client{config: Config{TenantID: "contoso", CertificatePassword: "pfx-secret"}, scriptDir: dir, timeout: time.Minute, sem: make(chan struct{}, 1)}
	c.pool = newPool(c, 1, 100)
	t.Cleanup(func() {
		select {
		case w := <-c.pool.idle:
			w.kill()
		default:
		}
	})
	return c
}

// stubParams is the param block every bridge script declares.
const stubParams = `param(
    [string]$TenantId, [string]$ClientId, [string]$CertificatePath,
    [string]$CertificatePassword, [string]$CertificateThumbprint, [string]$Environment,
    [Parameter(Mandatory)][string]$Action, [string]$Value = ""
)
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
. "$PSScriptRoot/auth.ps1"
`

func TestRealHostEndToEnd(t *testing.T) {
	c := realHostClient(t, map[string]string{
		// Stub auth: counts connections in a global, like a real session would persist.
		"auth.ps1": `if (-not (Get-Variable -Name connections -Scope Global -ErrorAction SilentlyContinue)) { $global:connections = 0 }
if (-not (Get-Variable -Name connected -Scope Global -ErrorAction SilentlyContinue)) { $global:connections++; $global:connected = $true }
if (-not $CertificatePassword) { $CertificatePassword = $env:TEAMS_BRIDGE_CERTIFICATE_PASSWORD }`,
		"probe.ps1": stubParams + `
switch ($Action) {
    "Info"     { @{ pid = $PID; connections = $global:connections; tenant = $TenantId; secret = $CertificatePassword; value = $Value } | ConvertTo-Json -Compress }
    "NotFound" { exit 0 }
    "Fail"     { [Console]::Error.WriteLine("ERROR in probe Get: access denied"); exit 1 }
    "Throw"    { throw "boom from script" }
    "Noisy"    { Write-Warning "a warning"; Write-Host "host text"; [Console]::Out.WriteLine("raw console"); '{"ok":true}' }
    "Leak"     { $leaked = "x"; '{}' }
    "ReadLeak" { if (Get-Variable leaked -ErrorAction SilentlyContinue) { '{"leak":true}' } else { '{"leak":false}' } }
}`,
	})
	ctx := context.Background()
	call := func(action, value string) (string, error) {
		out, err := c.RunScript(ctx, "probe.ps1", map[string]string{"Action": action, "Value": value})
		return string(out), err
	}
	info := func(value string) map[string]any {
		t.Helper()
		out, err := call("Info", value)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("bad JSON %q: %v", out, err)
		}
		return m
	}

	first := info(`quotes " and, commas — é`)
	if first["value"] != `quotes " and, commas — é` {
		t.Errorf("parameter not passed intact: %v", first["value"])
	}
	if first["tenant"] != "contoso" || first["secret"] != "pfx-secret" {
		t.Errorf("connection parameters not passed: %v", first)
	}
	second := info("")
	if first["pid"] != second["pid"] || second["connections"] != float64(1) {
		t.Errorf("expected one process and one connection, got %v then %v", first, second)
	}

	if out, err := call("NotFound", ""); err != nil || out != "" {
		t.Errorf("not found: out=%q err=%v", out, err)
	}
	if out, err := call("Noisy", ""); err != nil || out != `{"ok":true}` {
		t.Errorf("console noise leaked into output: out=%q err=%v", out, err)
	}
	if _, err := call("Leak", ""); err != nil {
		t.Fatal(err)
	}
	if out, _ := call("ReadLeak", ""); out != `{"leak":false}` {
		t.Errorf("script variables leaked between requests: %s", out)
	}

	if _, err := call("Fail", ""); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("expected stderr from failing script, got %v", err)
	}
	if _, err := call("Throw", ""); err == nil || !strings.Contains(err.Error(), "boom from script") {
		t.Errorf("expected exception message, got %v", err)
	}
	if after := info(""); after["pid"] == first["pid"] {
		t.Error("worker was not recycled after a failure")
	}

	if _, err := c.RunScript(ctx, "../etc/passwd.ps1", map[string]string{"Action": "Get"}); err == nil {
		t.Error("path traversal accepted")
	}
}

// TestRealHostKeepsDateStrings checks that an ISO 8601 parameter reaches the
// script as the exact string sent. ConvertFrom-Json would turn it into a
// DateTime and lose the time zone on the way back to a string.
func TestRealHostKeepsDateStrings(t *testing.T) {
	c := realHostClient(t, map[string]string{
		"auth.ps1": ``,
		"echo.ps1": stubParams + `@{ value = $Value } | ConvertTo-Json -Compress`,
	})
	const date = "2030-01-01T00:00:00Z"
	out, err := c.RunScript(context.Background(), "echo.ps1", map[string]string{"Action": "Get", "Value": date})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"value":"` + date + `"}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}
