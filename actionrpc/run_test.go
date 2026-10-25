package actionrpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testActionDir holds the actions under testdata that the tests build.
var testActionDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "actionrpc-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testActionDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var (
	testActionsMu sync.Mutex
	testActions   = map[string]func() (string, error){}
)

// testAction returns the executable of the action under testdata named
// name, which is built once for all the tests that use it: building it
// takes long, the more so when other packages are built alongside.
func testAction(t *testing.T, name string) string {
	t.Helper()
	testActionsMu.Lock()
	build, ok := testActions[name]
	if !ok {
		build = sync.OnceValues(func() (string, error) {
			bin := filepath.Join(testActionDir, name)
			if out, err := exec.Command("go", "build", "-o", bin, "github.com/mozership/probe/testdata/"+name).CombinedOutput(); err != nil {
				return "", fmt.Errorf("go build: %w\n%s", err, out)
			}
			return bin, nil
		})
		testActions[name] = build
	}
	testActionsMu.Unlock()
	bin, err := build()
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

// That an action stopped once ctx is done exited by the signal is not an
// error, while that it exited by itself with one, or any other error, still
// is.
func TestPluginLogLeavesOutTheExitOfAStoppedAction(t *testing.T) {
	var buf bytes.Buffer
	inner := hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Debug})
	ctx, cancel := context.WithCancel(context.Background())
	l := newPluginLog(ctx, inner, "/bin/action", nil)

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

// go-plugin's own records of starting, calling and stopping an action's
// process are at trace level, whatever level go-plugin gives them, while the
// records of the action, which go-plugin logs through the logger it names
// after the process, keep theirs, without the timestamp go-plugin adds.
func TestPluginLogPutsGoPluginsRecordsAtTraceLevel(t *testing.T) {
	var buf bytes.Buffer
	inner := hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Trace, DisableTime: true})
	var l hclog.Logger = newPluginLog(context.Background(), inner, "/path/to/my-action", nil)

	l.Debug("starting plugin", "path", "/path/to/my-action")
	l.Info("plugin process exited", "id", 1)
	l.Log(hclog.Debug, "using plugin", "version", 1)
	l.Named("stdio").Debug("received EOF, stopping recv loop")
	l.Warn("plugin failed to exit gracefully")
	action := l.Named("my-action")
	action.Debug("retrying", "attempt", 2, "timestamp", "2026-10-24T15:54:26.523+0900")
	action.Info("connected")
	action.Trace("parameters received")

	want := []string{
		"[TRACE] starting plugin: path=/path/to/my-action",
		"[TRACE] plugin process exited: id=1",
		"[TRACE] using plugin: version=1",
		"[TRACE] stdio: received EOF, stopping recv loop",
		"[WARN]  plugin failed to exit gracefully",
		"[DEBUG] retrying: attempt=2",
		"[INFO]  connected",
		"[TRACE] parameters received",
	}
	got := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("log =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !l.IsDebug() || !l.Named("stdio").IsInfo() {
		t.Error("go-plugin's records should be logged when trace records are")
	}

	quiet := newPluginLog(context.Background(), hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Debug}), "/bin/a", nil)
	if quiet.IsDebug() {
		t.Error("go-plugin's debug records should not be logged when only debug records are")
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
func TestRunExecutableStopsAnActionThatHangs(t *testing.T) {
	bin := testAction(t, "hang-action")
	pidfile := filepath.Join(t.TempDir(), "pid")

	// ctx is done only once the action is in its call, however long it took
	// to start, so that it is the call that is given up on.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan struct{})
	canceled := make(chan time.Time, 1)
	go func() {
		defer close(canceled)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(pidfile); err == nil && len(data) > 0 {
				canceled <- time.Now()
				cancel()
				return
			}
			select {
			case <-returned:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		cancel()
	}()
	_, _, err := RunExecutable(ctx, bin, nil, Call{With: map[string]any{"pidfile": pidfile}}, hclog.NewNullLogger())
	stopped := time.Now()
	close(returned)
	at, ok := <-canceled
	if !ok {
		t.Fatalf("the action did not run: RunExecutable() error = %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("RunExecutable() error = %v, want ctx's", err)
	}
	if took := stopped.Sub(at); took > TermGrace+5*time.Second {
		t.Errorf("RunExecutable() returned %s after ctx was done", took)
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
func TestRunExecutableTellsWhyAnActionFailed(t *testing.T) {
	bin := testAction(t, "failing-action")

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
			_, _, err := RunExecutable(context.Background(), bin, nil, Call{With: map[string]any{"mode": tt.mode}}, hclog.NewNullLogger())
			if err == nil {
				t.Fatal("RunExecutable() succeeded, want an error")
			}
			if err.Error() != tt.want {
				t.Errorf("error = %q, want %q", err.Error(), tt.want)
			}
			// What gRPC carried the error in is kept for code to read.
			if got := status.Code(err); got != tt.wantCode {
				t.Errorf("status.Code() = %v, want %v", got, tt.wantCode)
			}
			// An action that returned Unavailable is told as it said it,
			// not as one that exited.
			if tt.mode == "unavailable" {
				var exitErr *ExitError
				if errors.As(err, &exitErr) {
					t.Errorf("error = %#v, want the action's own", exitErr)
				}
				if !fromAction(err) {
					t.Errorf("error = %v, want it marked as the action's own, so that it is not waited on", err)
				}
			}
		})
	}
}

// The cleanup of an action tells why it failed as a call of the action does.
func TestCleanupExecutableTellsWhyTheCleanupFailed(t *testing.T) {
	bin := testAction(t, "failing-action")
	tests := map[string]string{
		"error": "the widget would not go",
		"panic": "the action exited before it returned (exit status 2): panic: the widget exploded while cleaning up",
	}
	for runID, want := range tests {
		t.Run(runID, func(t *testing.T) {
			err := CleanupExecutable(context.Background(), bin, nil, CleanupCall{RunID: runID}, hclog.NewNullLogger())
			if err == nil || err.Error() != want {
				t.Errorf("CleanupExecutable() error = %v, want %q", err, want)
			}
		})
	}
}

// lateContext is a context whose deadline has passed while it is not done
// yet, as gRPC can find a context: it is done a moment after its deadline is
// first read, with the deadline exceeded.
type lateContext struct {
	context.Context
	once sync.Once
	done chan struct{}
}

func newLateContext() *lateContext {
	return &lateContext{Context: context.Background(), done: make(chan struct{})}
}

func (c *lateContext) Deadline() (time.Time, bool) {
	c.once.Do(func() { time.AfterFunc(100*time.Millisecond, func() { close(c.done) }) })
	return time.Now().Add(-time.Second), true
}

func (c *lateContext) Done() <-chan struct{} {
	return c.done
}

func (c *lateContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// A call that gRPC gave up on by the deadline is told as the deadline
// exceeded, even before ctx is done, and any other error as it is.
func TestCallErrorOfACallPastItsDeadline(t *testing.T) {
	gaveUp := status.Error(codes.DeadlineExceeded, "context deadline exceeded")
	ctx := newLateContext()
	if ctx.Err() != nil {
		t.Fatal("the context should not be done before its deadline is read")
	}
	p := &process{}
	if err := p.callError(ctx, nil, gaveUp); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("callError() = %v, want the deadline exceeded", err)
	}
	if !p.callFailed.Load() {
		t.Error("the call should be marked as failed")
	}

	failed := status.Error(codes.NotFound, "no such widget")
	if err := (&process{}).callError(context.Background(), nil, failed); err != failed {
		t.Errorf("callError() = %v, want %v", err, failed)
	}
}

// A step and a cleanup whose call gRPC gives up on by the deadline, before
// ctx is done, both return the deadline exceeded rather than what gRPC said.
func TestCallsPastTheirDeadlineReturnTheDeadlineExceeded(t *testing.T) {
	bin := testAction(t, "failing-action")
	calls := map[string]func(ctx context.Context) error{
		"step": func(ctx context.Context) error {
			_, _, err := RunExecutable(ctx, bin, nil, Call{With: map[string]any{}}, hclog.NewNullLogger())
			return err
		},
		"cleanup": func(ctx context.Context) error {
			return CleanupExecutable(ctx, bin, nil, CleanupCall{RunID: "run1"}, hclog.NewNullLogger())
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(newLateContext()); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("error = %v, want the deadline exceeded", err)
			}
		})
	}
}

// expiringContext is a context that runs out of time when expire is called,
// as one with a deadline does once the deadline comes. Its deadline is the
// time it expired, and it has none before.
type expiringContext struct {
	context.Context
	mu   sync.Mutex
	at   time.Time
	done chan struct{}
}

func newExpiringContext() *expiringContext {
	return &expiringContext{Context: context.Background(), done: make(chan struct{})}
}

// expire has the context run out of time, unless it already has, and
// returns when it did.
func (c *expiringContext) expire() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() {
		c.at = time.Now()
		close(c.done)
	}
	return c.at
}

func (c *expiringContext) Deadline() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at, !c.at.IsZero()
}

func (c *expiringContext) Done() <-chan struct{} {
	return c.done
}

func (c *expiringContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// A cleanup that is still in its call when its time runs out is killed
// where it is, without the SIGTERM and the grace the action of a step is
// given, and the deadline exceeded is returned.
func TestCleanupExecutableKillsAnActionThatHangs(t *testing.T) {
	bin := testAction(t, "hang-action")
	pidfile := filepath.Join(t.TempDir(), "pid")

	// ctx runs out of time only once the action is in its cleanup, however
	// long it took to start, so that it is the call that is given up on.
	ctx := newExpiringContext()
	returned := make(chan struct{})
	expired := make(chan time.Time, 1)
	go func() {
		defer close(expired)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if data, err := os.ReadFile(pidfile); err == nil && len(data) > 0 {
				expired <- ctx.expire()
				return
			}
			select {
			case <-returned:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		ctx.expire()
	}()
	err := CleanupExecutable(ctx, bin, nil, CleanupCall{RunID: pidfile}, hclog.NewNullLogger())
	stopped := time.Now()
	close(returned)
	at, ok := <-expired
	if !ok {
		t.Fatalf("the action did not reach its cleanup: CleanupExecutable() error = %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CleanupExecutable() error = %v, want the deadline exceeded", err)
	}
	if took := stopped.Sub(at); took >= TermGrace {
		t.Errorf("CleanupExecutable() returned %s after the deadline, want less than the grace of %s", took, TermGrace)
	}

	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("the action is still running: kill(%d, 0) = %v", pid, err)
	}
	if _, err := os.Stat(pidfile + ".term"); err == nil {
		t.Error("the action was sent SIGTERM, want it killed")
	}
}

// An executable that does not serve an action is told not to have started,
// with what it wrote of why.
func TestRunExecutableTellsWhyAnActionDidNotStart(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "broken")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho 'cannot read the config' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := RunExecutable(context.Background(), exe, nil, Call{With: map[string]any{}}, hclog.NewNullLogger())
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

// go-plugin's record of a process it killed for not exiting gracefully is
// left out once the call failed, as the error tells what became of the
// action, and told otherwise, of an action that hangs when it is stopped.
func TestPluginLogLeavesOutTheKillAfterAFailedCall(t *testing.T) {
	var buf bytes.Buffer
	inner := hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Warn})
	failed := &atomic.Bool{}
	l := newPluginLog(context.Background(), inner, "/bin/action", failed)

	l.Warn("plugin failed to exit gracefully", "call", "succeeded")
	failed.Store(true)
	l.Warn("plugin failed to exit gracefully", "call", "failed")
	l.Warn("another warning")

	got := buf.String()
	for _, want := range []string{"call=succeeded", "another warning"} {
		if !strings.Contains(got, want) {
			t.Errorf("log should have %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "call=failed") {
		t.Errorf("log should leave out the kill after a failed call, got:\n%s", got)
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
			name:  "a line longer than is kept",
			write: []string{strings.Repeat("x", maxStderrLine), strings.Repeat("y", 100), "\n", "next\n"},
			want:  strings.Repeat("x", maxStderrLine) + "\nnext",
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
