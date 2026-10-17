package actionrpc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
)

// That an action stopped once ctx is done exited by the signal is not an
// error, while that it exited by itself with one, or any other error, still
// is.
func TestStoppedLoggerLeavesOutTheExitOfAStoppedAction(t *testing.T) {
	var buf bytes.Buffer
	inner := hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Warn})
	ctx, cancel := context.WithCancel(context.Background())
	l := stoppedLogger{Logger: inner, ctx: ctx}

	l.Error("plugin process exited", "error", "exit status 2")
	l.Error("something else")
	cancel()
	l.Error("plugin process exited", "error", "signal: terminated")
	l.Error("another thing")

	got := buf.String()
	for _, want := range []string{"exit status 2", "something else", "another thing"} {
		if !strings.Contains(got, want) {
			t.Errorf("log should have %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "signal: terminated") {
		t.Errorf("log should leave out the exit of a stopped action, got:\n%s", got)
	}
}

// An action that does not return in time is stopped, given SIGTERM first so
// that it can stop what it started, and the call returns once ctx is done.
func TestRunExecutableStepContextStopsAnActionThatHangs(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hang")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/mozership/probe/testdata/hang-action").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	pidfile := filepath.Join(dir, "pid")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, _, err := RunExecutableStepContext(ctx, bin, nil, Call{With: map[string]any{"pidfile": pidfile}}, hclog.NewNullLogger())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("RunExecutableStepContext() error = %v, want the deadline exceeded", err)
	}
	if took := time.Since(start); took > 2*time.Second+StopGrace+5*time.Second {
		t.Errorf("RunExecutableStepContext() took %s", took)
	}

	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("the action did not run: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("the action is still running: kill(%d, 0) = %v", pid, err)
	}
	if _, err := os.Stat(pidfile + ".term"); err != nil {
		t.Errorf("the action was not sent SIGTERM: %v", err)
	}
}
