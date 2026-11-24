package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startReady runs cmd in the background, waiting for text in its log, and
// stops what is left of it when the test is over.
func startReady(t *testing.T, cmd, text, timeout string) *Result {
	t.Helper()
	r := NewReq()
	r.Cmd = cmd
	r.Background = true
	r.Ready = map[string]string{"log": text}
	r.Timeout = timeout
	result, err := r.Do()
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	cleanupBackground(t, result.Res.PID, result.Res.Log)
	return result
}

func TestDoReady(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
	}{
		{"written to stdout after a while", "sleep 0.3; echo 'listening on :8080'; sleep 30"},
		{"written to stderr", "echo 'listening on :8080' >&2; sleep 30"},
		{"written in pieces", "printf 'listen'; sleep 0.2; printf 'ing on :8080\\n'; sleep 30"},
		{"written by what the shell left running", "(sleep 0.3; echo 'listening on :8080'; sleep 30) &"},
		{"written just before the shell exits", "echo 'listening on :8080'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			result := startReady(t, tt.cmd, "listening on", "10s")
			if result.Status != -1 || result.Res.Code != -1 {
				t.Errorf("status = %d, code = %d, want -1 and -1 for a ready command", result.Status, result.Res.Code)
			}
			if result.Res.TimedOut {
				t.Error("timed_out should be false")
			}
			if result.Res.Stdout != "" {
				t.Errorf("stdout should stay empty for a ready command, got %q", result.Res.Stdout)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("waited %v, longer than the text took to appear", elapsed)
			}
		})
	}
}

func TestDoReadyExitsFirst(t *testing.T) {
	start := time.Now()
	result := startReady(t, "echo 'cannot bind :8080' >&2; exit 3", "listening on", "10s")
	if result.Status != 1 {
		t.Errorf("status = %d, want 1", result.Status)
	}
	if result.Res.Code != 3 {
		t.Errorf("code = %d, want the exit code 3", result.Res.Code)
	}
	if !strings.Contains(result.Res.Stdout, "cannot bind :8080") {
		t.Errorf("stdout should hold the log, got %q", result.Res.Stdout)
	}
	if result.Res.TimedOut {
		t.Error("timed_out should be false for a command that exited")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %v for a command that had exited", elapsed)
	}
}

func TestDoReadyTimesOut(t *testing.T) {
	result := startReady(t, "echo 'starting'; sleep 30", "listening on", "500ms")
	if result.Status != 1 {
		t.Errorf("status = %d, want 1", result.Status)
	}
	if !result.Res.TimedOut {
		t.Error("timed_out should be true")
	}
	if !strings.Contains(result.Res.Stdout, "starting") {
		t.Errorf("stdout should hold the log, got %q", result.Res.Stdout)
	}
	if !waitGone(result.Res.PID) {
		t.Error("a command that did not get ready in time should be stopped")
	}
}

func TestDoReadyRejected(t *testing.T) {
	tests := []struct {
		name       string
		background bool
		ready      map[string]string
		want       string
	}{
		{"not in the background", false, map[string]string{"log": "x"}, "ready is for a background command"},
		{"an unknown key", true, map[string]string{"file": "x"}, "ready takes log, not file"},
		{"no text", true, map[string]string{"log": ""}, "ready.log is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "ran")
			r := NewReq()
			r.Cmd = "touch " + marker
			r.Background = tt.background
			r.Ready = tt.ready
			_, err := r.Do()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(marker); err == nil {
				t.Error("the command should not run")
			}
		})
	}
}

func TestExecuteReady(t *testing.T) {
	ret, err := Execute(map[string]any{
		"cmd":        "echo ready; sleep 30",
		"background": true,
		"ready":      map[string]any{"log": "ready"},
	})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	res := ret["res"].(map[string]any)
	pid, _ := res["pid"].(int)
	log, _ := res["log"].(string)
	cleanupBackground(t, pid, log)
	if ret["status"] != -1 {
		t.Errorf("status = %v, want -1", ret["status"])
	}
	req := ret["req"].(map[string]any)
	if ready, _ := req["ready"].(map[string]string); ready["log"] != "ready" {
		t.Errorf("req.ready = %#v, want it carried through", req["ready"])
	}
}

func TestLogMatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	m := &logMatcher{path: path, text: []byte("listening on")}

	if m.found() {
		t.Fatal("found the text in a log that does not exist")
	}
	for _, chunk := range []string{"boot\nlis", "ten", "ing", " on :80\n"} {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(chunk)
		_ = f.Close()
		got := m.found()
		if want := chunk == " on :80\n"; got != want {
			t.Fatalf("after %q: found = %v, want %v", chunk, got, want)
		}
	}
}

func TestExecuteReadyRejected(t *testing.T) {
	tests := []struct {
		name  string
		ready any
		want  string
	}{
		{"an empty map", map[string]any{}, "ready.log is required"},
		{"null", nil, "ready must be a map"},
		{"a string", "listening on", "ready must be a map"},
		{"a number for log", map[string]any{"log": int64(8080)}, "ready.log must be a string"},
		{"an empty log", map[string]any{"log": ""}, "ready.log is required"},
		{"an unknown key", map[string]any{"file": "x"}, "ready takes log, not file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "ran")
			ret, err := Execute(map[string]any{
				"cmd":        "touch " + marker,
				"background": true,
				"ready":      tt.ready,
			})
			if res, ok := ret["res"].(map[string]any); ok {
				pid, _ := res["pid"].(int)
				log, _ := res["log"].(string)
				cleanupBackground(t, pid, log)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one containing %q", err, tt.want)
			}
			time.Sleep(100 * time.Millisecond)
			if _, err := os.Stat(marker); err == nil {
				t.Error("the command should not run")
			}
		})
	}
}

func TestWaitReadyExitAndDeadlineTogether(t *testing.T) {
	// A shell that has exited, with nothing left of its group, and a timeout
	// that has passed as well: select may pick either, and the exit came
	// first, so it is the exit that is reported, every time.
	cmd := exec.Command("/bin/sh", "-c", "exit 3")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Run(); err == nil {
		t.Fatal("expected the command to exit 3")
	}
	pid := cmd.Process.Pid
	if !waitGone(pid) {
		t.Fatal("the group should be gone")
	}
	exited := make(chan struct{})
	close(exited)
	log := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(log, []byte("starting\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for i := range 200 {
		if got := waitReady(log, "listening on", pid, exited, 0); got != readyExited {
			t.Fatalf("run %d: waitReady = %v, want readyExited", i, got)
		}
	}
}
