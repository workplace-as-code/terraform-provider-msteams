package client

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtractScriptsWritesEmbeddedFilesPrivately(t *testing.T) {
	dir, err := extractScripts(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"auth.ps1", "cs_object.ps1"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not extracted: %v", name, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("script dir permissions = %o, want 700", info.Mode().Perm())
		}
	}
}

func TestExtractScriptsRepairsTamperedFile(t *testing.T) {
	base := t.TempDir()
	dir, err := extractScripts(base)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "auth.ps1")
	if err := os.WriteFile(target, []byte("malicious"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractScripts(base); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) == "malicious" {
		t.Fatal("tampered script was not replaced by the embedded copy")
	}
}

func TestSecretsAreNeverOnTheCommandLine(t *testing.T) {
	c := &Client{config: Config{TenantID: "t", ClientID: "c", CertificatePath: "/x.pfx", CertificatePassword: "s3cr3t-cert"}}
	_, args := c.buildArgs("/scripts/x.ps1", map[string]string{"Action": "Get"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "s3cr3t") {
		t.Errorf("secret leaked into args: %s", joined)
	}
	env := strings.Join(c.buildEnv(), "\n")
	if !strings.Contains(env, "s3cr3t") {
		t.Error("secret missing from child environment")
	}
}

func TestBuildArgsIsDeterministic(t *testing.T) {
	c := &Client{config: Config{}}
	params := map[string]string{"B": "2", "A": "1", "C": "3"}
	_, first := c.buildArgs("x.ps1", params)
	for i := 0; i < 20; i++ {
		_, again := c.buildArgs("x.ps1", params)
		if strings.Join(again, " ") != strings.Join(first, " ") {
			t.Fatal("argument order changes between calls")
		}
	}
	if !strings.HasSuffix(strings.Join(first, " "), "-A 1 -B 2 -C 3") {
		t.Errorf("params not sorted: %v", first)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(nil) || !IsNotFound([]byte(" \r\n")) {
		t.Error("empty output must mean not found")
	}
	if IsNotFound([]byte("WARNING: something")) || IsNotFound([]byte(`{"Name":"x"}`)) {
		t.Error("non-empty output must never mean not found")
	}
}

func TestEnvParsing(t *testing.T) {
	t.Setenv(EnvScriptTimeout, "")
	if d, _ := durationFromEnv(EnvScriptTimeout, time.Minute); d != time.Minute {
		t.Errorf("default not applied: %s", d)
	}
	t.Setenv(EnvScriptTimeout, "nope")
	if _, err := durationFromEnv(EnvScriptTimeout, time.Minute); err == nil {
		t.Error("invalid duration accepted")
	}
	t.Setenv(EnvMaxConcurrency, "0")
	if _, err := intFromEnv(EnvMaxConcurrency, 4); err == nil {
		t.Error("zero concurrency accepted")
	}
}

// fakePwsh puts a fake "pwsh" executable first on PATH. It runs the given
// shell body, which lets us test RunScript without PowerShell or Teams.
func fakePwsh(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake pwsh uses a POSIX shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pwsh"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func newTestClient(timeout time.Duration) *Client {
	return &Client{config: Config{}, scriptDir: "/scripts", timeout: timeout, sem: make(chan struct{}, 1)}
}

func TestRunScriptReturnsStdout(t *testing.T) {
	fakePwsh(t, `echo '{"ok":true}'`)
	out, err := newTestClient(time.Minute).RunScript(context.Background(), "x.ps1", nil)
	if err != nil || strings.TrimSpace(string(out)) != `{"ok":true}` {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRunScriptSurfacesStderrOnFailure(t *testing.T) {
	fakePwsh(t, `echo "throttled by Teams" >&2; exit 1`)
	_, err := newTestClient(time.Minute).RunScript(context.Background(), "x.ps1", nil)
	if err == nil || !strings.Contains(err.Error(), "throttled by Teams") {
		t.Fatalf("expected stderr in error, got %v", err)
	}
}

func TestRunScriptTimesOut(t *testing.T) {
	fakePwsh(t, `exec sleep 30`)
	start := time.Now()
	_, err := newTestClient(300*time.Millisecond).RunScript(context.Background(), "x.ps1", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout did not stop the process")
	}
}

func TestRunScriptPassesSecretsThroughEnvironment(t *testing.T) {
	fakePwsh(t, `echo "$`+EnvBridgeCertificatePassword+`"`)
	c := newTestClient(time.Minute)
	c.config.CertificatePassword = "from-env"
	out, err := c.RunScript(context.Background(), "x.ps1", nil)
	if err != nil || strings.TrimSpace(string(out)) != "from-env" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

// TestWritesToTheSameScriptAreSerialized runs concurrent calls through a fake
// pwsh that fails if another write is in progress, using a lock directory.
func TestWritesToTheSameScriptAreSerialized(t *testing.T) {
	lockDir := filepath.Join(t.TempDir(), "busy")
	fakePwsh(t, `case "$*" in
  *"-Action Get"*) sleep 0.2; echo '{}'; exit 0 ;;
esac
mkdir "`+lockDir+`" 2>/dev/null || { echo "concurrent write" >&2; exit 1; }
sleep 0.2
rmdir "`+lockDir+`"
echo '{}'`)
	c := newTestClient(time.Minute)
	c.sem = make(chan struct{}, 4)

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.RunScript(context.Background(), "cs_object.ps1", map[string]string{"Action": "Create"})
			errs <- err
		}()
	}
	start := time.Now()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.RunScript(context.Background(), "cs_object.ps1", map[string]string{"Action": "Get"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("write was not serialized: %v", err)
		}
	}
	// Four serialized writes take at least 0.8s.
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Fatalf("writes finished in %s, expected them to run one after another", elapsed)
	}
}
