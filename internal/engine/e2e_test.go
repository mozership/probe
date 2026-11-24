package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEndToEndExitCodes(t *testing.T) {
	tests := []struct {
		name         string
		workflowPath string
		expectedCode int
	}{
		{
			name:         "success workflow with hello action returns exit code 0",
			workflowPath: "testdata/success-hello.yml",
			expectedCode: 0,
		},
		{
			name:         "failure workflow with hello action returns exit code 1",
			workflowPath: "testdata/failure-hello.yml",
			expectedCode: 1,
		},
		{
			name:         "success workflow with embedded action returns exit code 0",
			workflowPath: "testdata/embedded-success-workflow.yml",
			expectedCode: 0,
		},
		{
			name:         "failure workflow with embedded action returns exit code 1",
			workflowPath: "testdata/embedded-failure-workflow.yml",
			expectedCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "run", "github.com/mozership/probe/cmd/probe", tt.workflowPath)
			cmd.Dir = filepath.Join("..", "..")
			_, err := cmd.CombinedOutput()

			var exitCode int
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				} else {
					t.Fatalf("unexpected error type: %v", err)
				}
			} else {
				exitCode = 0
			}

			if exitCode != tt.expectedCode {
				t.Errorf("expected exit code %d, got %d", tt.expectedCode, exitCode)
			}
		})
	}
}

func TestEndToEndBackgroundStoppedAfterWorkflow(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	logFile := filepath.Join(dir, "log")
	cleanupRecorded(t, pidFile, logFile)
	workflow := filepath.Join(dir, "workflow.yml")
	yml := fmt.Sprintf(`name: background
jobs:
- name: server
  steps:
  - name: start a command that never ends
    id: server
    uses: shell
    with:
      cmd: "echo $$ > %s; exec sleep 300"
      background: true
    outputs:
      log: res.log
  - name: record the log path
    uses: shell
    with:
      cmd: "echo '{{outputs.server.log}}' > %s; sleep 0.2"
    test: res.code == 0
`, pidFile, logFile)
	if err := os.WriteFile(workflow, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("go", "run", "github.com/mozership/probe/cmd/probe", workflow).CombinedOutput()
	if err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("background command did not start: %v\n%s", err, out)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatalf("pid file = %q: %v", pidData, err)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Errorf("background process %d is still running after the workflow", pid)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("log path was not recorded: %v\n%s", err, out)
	}
	log := strings.TrimSpace(string(logData))
	if !strings.Contains(log, "probe-shell-action.") {
		t.Fatalf("recorded log path = %q", log)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		_ = os.Remove(log)
		t.Errorf("log %s was left behind: %v", log, err)
	}
}

func TestEndToEndBackgroundStoppedOnInterrupt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds probe")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/mozership/probe/cmd/probe").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	pidFile := filepath.Join(dir, "pid")
	logFile := filepath.Join(dir, "log")
	cleanupRecorded(t, pidFile, logFile)
	workflow := filepath.Join(dir, "workflow.yml")
	yml := fmt.Sprintf(`name: interrupted
jobs:
- name: server
  steps:
  - name: start a command that never ends
    id: server
    uses: shell
    with:
      cmd: "echo $$ > %s; exec sleep 300"
      background: true
    outputs:
      log: res.log
  - name: record the log path
    uses: shell
    with:
      cmd: "echo '{{outputs.server.log}}' > %s"
  - name: take a long time
    uses: shell
    with:
      cmd: "sleep 60"
      timeout: 2m
`, pidFile, logFile)
	if err := os.WriteFile(workflow, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Run probe in a group of its own and interrupt the whole group, as
	// Ctrl+C in a terminal does.
	cmd := exec.Command(bin, workflow)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })

	deadline := time.Now().Add(30 * time.Second)
	for {
		if data, err := os.ReadFile(logFile); err == nil && len(data) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the workflow did not reach its long step")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited: //nolint:errcheck // ended by SIGINT, Wait errs: the state is checked below
	case <-time.After(15 * time.Second):
		t.Fatal("probe did not exit after SIGINT")
	}
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Errorf("probe exited with %v, want it ended by SIGINT", cmd.ProcessState)
	}

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		t.Fatal(err)
	}
	if !waitGroupGone(pid) {
		t.Errorf("background process %d is still running after the interrupt", pid)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if log := strings.TrimSpace(string(logData)); log != "" {
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			_ = os.Remove(log)
			t.Errorf("log %s was left behind: %v", log, err)
		}
	}
}

// cleanupRecorded stops the background command whose pid the workflow wrote
// to pidFile, and removes the log whose path it wrote to logFile, however the
// test ends. Those live outside the test's directory, and a test that fails
// early must not leave behind what it was checking for.
func cleanupRecorded(t *testing.T, pidFile, logFile string) {
	t.Helper()
	t.Cleanup(func() {
		if data, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
		if data, err := os.ReadFile(logFile); err == nil {
			if log := strings.TrimSpace(string(data)); log != "" {
				_ = os.Remove(log)
			}
		}
	})
}
