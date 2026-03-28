package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMissingConfigFile verifies that a missing config file exits with code 1
// and the error message contains the config path.
func TestMissingConfigFile(t *testing.T) {
	missingPath := "/nonexistent/path/to/config.yml"

	var stdout, stderr bytes.Buffer
	code := run([]string{"--config", missingPath}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, missingPath) {
		t.Errorf("expected output to contain path %q, got: %s", missingPath, combined)
	}
}

// TestInvalidConfig verifies that an invalid config file exits with code 1
// and produces an error message.
func TestInvalidConfig(t *testing.T) {
	// Write an invalid YAML config to a temp file.
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")
	// This YAML is syntactically valid but semantically invalid: a stack with no repo.
	invalidYAML := `
stacks:
  - name: test-stack
    path: stacks/test
`
	if err := os.WriteFile(cfgPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--config", cfgPath}, &stdout, &stderr)

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	combined := stdout.String() + stderr.String()
	if combined == "" {
		t.Error("expected non-empty error output for invalid config")
	}
}

// TestNoStacksConfigured verifies that the agent starts and shuts down cleanly
// when the config file has no stacks — no panic, exit code 0.
func TestNoStacksConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")
	if err := os.WriteFile(cfgPath, []byte("stacks: []\n"), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- run([]string{"--config", cfgPath}, &stdout, &stderr)
	}()

	time.Sleep(100 * time.Millisecond)

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("expected exit code 0 with no stacks, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not shut down within 5 seconds")
	}
}

// TestStartupBanner verifies that the startup log line is emitted with version,
// config path, stack count, and stack names.
func TestStartupBanner(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "stacks")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("creating state dir: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yml")
	cfgContent := `
defaults:
  work_dir: ` + stateDir + `
  poll_interval: 3600

stacks:
  - name: mystack
    repo: https://github.com/example/test.git
    path: stacks/mystack
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	done := make(chan struct {
		code   int
		stderr string
	}, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--config", cfgPath}, &stdout, &stderr)
		done <- struct {
			code   int
			stderr string
		}{code, stderr.String()}
	}()

	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	select {
	case result := <-done:
		if result.code != 0 {
			t.Errorf("expected exit code 0, got %d", result.code)
		}
		logs := result.stderr
		if !strings.Contains(logs, "steward starting") {
			t.Errorf("startup banner not found in logs:\n%s", logs)
		}
		if !strings.Contains(logs, cfgPath) {
			t.Errorf("config path not found in startup banner:\n%s", logs)
		}
		if !strings.Contains(logs, "mystack") {
			t.Errorf("stack name not found in startup banner:\n%s", logs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not shut down within 5 seconds")
	}
}

// TestSIGTERMCleanShutdown verifies that sending SIGTERM causes the run loop
// to exit cleanly. The test fails if shutdown takes longer than 5 seconds.
func TestSIGTERMCleanShutdown(t *testing.T) {
	// Create a minimal valid config.
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "stacks")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("creating state dir: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.yml")
	cfgContent := `
defaults:
  work_dir: ` + stateDir + `
  poll_interval: 3600

stacks:
  - name: test
    repo: https://github.com/example/test.git
    path: stacks/test
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		code := run([]string{"--config", cfgPath}, &stdout, &stderr)
		done <- code
	}()

	// Give run() a moment to start up and block on the signal.
	time.Sleep(100 * time.Millisecond)

	// Send SIGTERM to ourselves.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("expected exit code 0 on clean shutdown, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not shut down within 5 seconds after SIGTERM")
	}
}
