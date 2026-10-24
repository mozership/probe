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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestOver(t *testing.T) {
	if over(context.Background()) {
		t.Error("a context without a deadline should not be over")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	if over(ctx) {
		t.Error("a context before its deadline should not be over")
	}
	cancel()
	if !over(ctx) {
		t.Error("a canceled context should be over")
	}
	past, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if !over(past) || !errors.Is(past.Err(), context.DeadlineExceeded) {
		t.Errorf("a context past its deadline should be over, with the deadline exceeded: %v", past.Err())
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

// An action's error is told as the action said it, whatever gRPC carried it
// in, and one whose process exited before it returned is told so, with how
// it exited and what it wrote of why.
func TestRunExecutableStepTellsWhyAnActionFailed(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "failing")
	if out, err := exec.Command("go", "build", "-o", bin, "github.com/mozership/probe/testdata/failing-action").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	tests := []struct {
		mode     string
		want     string
		wantCode codes.Code
	}{
		{mode: "error", want: "the widget broke", wantCode: codes.Unknown},
		{mode: "status", want: "no such widget", wantCode: codes.NotFound},
		{mode: "unavailable", want: "the widget is down", wantCode: codes.Unavailable},
		{mode: "panic", want: "the action exited before it returned (exit status 2): panic: the widget exploded", wantCode: codes.Unavailable},
		{mode: "exit", want: "the action exited before it returned (exit status 3): the widget is gone", wantCode: codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			start := time.Now()
			_, _, err := RunExecutableStep(bin, nil, Call{With: map[string]any{"mode": tt.mode}}, hclog.NewNullLogger())
			if err == nil {
				t.Fatal("RunExecutableStep() succeeded, want an error")
			}
			if err.Error() != tt.want {
				t.Errorf("error = %q, want %q", err.Error(), tt.want)
			}
			// What gRPC carried the error in is kept for code to read.
			if got := status.Code(err); got != tt.wantCode {
				t.Errorf("status.Code() = %v, want %v", got, tt.wantCode)
			}
			// An action that returned Unavailable is not waited for to
			// exit, as one that lost the connection is.
			if tt.mode == "unavailable" {
				if took := time.Since(start); took >= exitWait {
					t.Errorf("RunExecutableStep() took %s, want less than %s", took, exitWait)
				}
				var exitErr *ExitError
				if errors.As(err, &exitErr) {
					t.Errorf("error = %#v, want the action's own", exitErr)
				}
			}
		})
	}
}

// An executable that does not serve an action is told not to have started,
// with what it wrote of why.
func TestRunExecutableStepTellsWhyAnActionDidNotStart(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "broken")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'cannot read the config' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := RunExecutableStep(exe, nil, Call{With: map[string]any{}}, hclog.NewNullLogger())
	var startErr *StartError
	if !errors.As(err, &startErr) {
		t.Fatalf("error = %v, want a StartError", err)
	}
	if !strings.HasPrefix(err.Error(), "the action did not start: cannot read the config\n") {
		t.Errorf("error = %q, want it to start with why the action did not start", err.Error())
	}
	if startErr.Err == nil || !strings.Contains(err.Error(), startErr.Err.Error()) {
		t.Errorf("error = %q, want it to keep what go-plugin said", err.Error())
	}
}

// The log keeps how the process exited for the error of a call it did not
// return from, whether or not it shows it.
func TestStoppedLoggerKeepsHowTheProcessExited(t *testing.T) {
	exit := &exitStatus{}
	l := stoppedLogger{Logger: hclog.NewNullLogger(), ctx: context.Background(), exit: exit}
	l.Error("something else", "error", "not this")
	if got := exit.get(); got != "" {
		t.Errorf("exit status = %q after another record, want none", got)
	}
	l.Error("plugin process exited", "plugin", "/bin/x", "id", 1, "error", "exit status 2")
	if got := exit.get(); got != "exit status 2" {
		t.Errorf("exit status = %q, want %q", got, "exit status 2")
	}
}

// What a process writes to stderr is kept apart from its log records, and
// its first paragraph tells why it exited.
func TestStderrOutputReason(t *testing.T) {
	tests := []struct {
		name  string
		write []string
		want  string
	}{
		{
			name:  "nothing",
			write: nil,
			want:  "",
		},
		{
			name:  "log records only",
			write: []string{`{"@level":"error","@message":"failed"}`, "\n"},
			want:  "",
		},
		{
			name: "a panic among log records",
			write: []string{
				`{"@level":"debug","@message":"called"}`, "\n",
				"panic: boom", "\n",
				"[signal SIGSEGV: segmentation violation]", "\n",
				"", "\n",
				"goroutine 1 [running]:", "\n",
				"main.main()", "\n",
			},
			want: "panic: boom\n[signal SIGSEGV: segmentation violation]",
		},
		{
			name:  "a line written in parts, without its newline",
			write: []string{"\n\nfatal: ", "giving up"},
			want:  "fatal: giving up",
		},
		{
			name:  "a line that only starts as JSON",
			write: []string{"{not json\n"},
			want:  "{not json",
		},
		{
			name:  "more lines than are told",
			write: []string{strings.Repeat("line\n", maxReasonLines+5)},
			want:  strings.TrimSuffix(strings.Repeat("line\n", maxReasonLines), "\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &stderrOutput{}
			for _, w := range tt.write {
				if _, err := o.Write([]byte(w)); err != nil {
					t.Fatal(err)
				}
			}
			if got := o.reason(); got != tt.want {
				t.Errorf("reason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// What the process wrote to its stderr itself, such as a panic, tells why
// it exited before what the action wrote to os.Stderr does.
func TestActionStderrReasonPrefersTheProcessStderr(t *testing.T) {
	a := &actionStderr{}
	_, _ = a.synced.Write([]byte("written by the action\n"))
	if got := a.reason(); got != "written by the action" {
		t.Errorf("reason() = %q, want what the action wrote", got)
	}
	_, _ = a.fd.Write([]byte("panic: boom\n"))
	if got := a.reason(); got != "panic: boom" {
		t.Errorf("reason() = %q, want the panic", got)
	}
}
