package procgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// startGroup starts cmd in a process group of its own, as the shell action
// does for a background step, and returns its pid.
func startGroup(t *testing.T, cmd string) int {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		t.Fatalf("start %q: %v", cmd, err)
	}
	// Reap the process once it is stopped, so that the group is really gone.
	go func() { _ = c.Wait() }()
	pid := c.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return pid
}

// waitGroupGone reports whether the group led by pid is gone within a second.
// A killed process stays visible until it is reaped, which happens on its
// own schedule.
func waitGroupGone(pid int) bool {
	deadline := time.Now().Add(time.Second)
	for groupAlive(pid) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

func TestTrackerNil(t *testing.T) {
	// A JobContext built without a tracker must not panic.
	var b *Tracker
	b.Track(123, "")
	b.Stop()
}

func TestTrackerStop(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "probe-shell-action.1.log")
	if err := os.WriteFile(log, []byte("output\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The shell stays as the group leader and sleep runs as its child: both
	// have to go.
	pid := startGroup(t, "sleep 60 & wait")

	b := NewTracker()
	b.Track(pid, log)
	b.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d is still running", pid)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("log %s was not removed: %v", log, err)
	}
	if len(b.procs) != 0 {
		t.Errorf("procs = %v, want none left", b.procs)
	}
}

func TestTrackerStopIgnoringTerm(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the grace period")
	}
	pid := startGroup(t, "trap '' TERM; sleep 60 & wait")
	// Give the shell time to install the trap.
	time.Sleep(200 * time.Millisecond)

	b := NewTracker()
	b.Track(pid, "")

	start := time.Now()
	b.Stop()
	elapsed := time.Since(start)

	if !waitGroupGone(pid) {
		t.Errorf("process group %d survived SIGKILL", pid)
	}
	if elapsed < StopGrace {
		t.Errorf("stopped after %v, want SIGKILL only after the %v grace", elapsed, StopGrace)
	}
}

func TestTrackerStopExited(t *testing.T) {
	// A process that already finished is skipped without waiting.
	pid := startGroup(t, "true")
	if !waitGroupGone(pid) {
		t.Fatalf("process group %d did not exit", pid)
	}

	b := NewTracker()
	b.Track(pid, "")

	start := time.Now()
	b.Stop()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("stop took %v for a process that had exited", elapsed)
	}
}

func TestProcessStartTime(t *testing.T) {
	a, err := processStartTime(os.Getpid())
	if err != nil {
		t.Fatalf("processStartTime(self) error: %v", err)
	}
	b, _ := processStartTime(os.Getpid())
	if a == 0 || a != b {
		t.Errorf("processStartTime(self) = %d then %d, want the same non-zero value", a, b)
	}

	pid := startGroup(t, "true")
	if !waitGroupGone(pid) {
		t.Fatalf("process %d did not exit", pid)
	}
	if _, err := processStartTime(pid); err == nil {
		t.Errorf("processStartTime(%d) succeeded for a process that has exited", pid)
	}
}

func TestTrackerStopReusedPid(t *testing.T) {
	// A process with the recorded pid that started at another time is not the
	// one the step started, and must be left alone.
	pid := startGroup(t, "sleep 60")
	started, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}

	b := NewTracker()
	b.procs = []proc{{pid: pid, started: started + 1}}
	b.Stop()

	if !groupAlive(pid) {
		t.Errorf("process group %d was signalled although its leader is not the recorded one", pid)
	}
}

func TestTrackerStopLeaderGone(t *testing.T) {
	// The shell exits at once and leaves sleep running in its group. The pid
	// cannot have been reused while the group lives, so the group is stopped.
	pid := startGroup(t, "sleep 60 & exit 0")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := processStartTime(pid); err != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !groupAlive(pid) {
		t.Fatalf("process group %d is gone before the test", pid)
	}

	b := NewTracker()
	b.Track(pid, "")
	b.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d is still running", pid)
	}
}

func TestTrackerTrackAfterStop(t *testing.T) {
	// A step that timed out can report its process after the workflow has
	// stopped the others; it is stopped right away.
	b := NewTracker()
	b.Stop()

	pid := startGroup(t, "sleep 60")
	b.Track(pid, "")

	if !waitGroupGone(pid) {
		t.Errorf("process group %d reported after stop is still running", pid)
	}
	if len(b.procs) != 0 {
		t.Errorf("procs = %v, want none kept after stop", b.procs)
	}
}

func TestTrackerStopWaitsForPending(t *testing.T) {
	// An action still running when the workflow ends gets a moment to report
	// the process it started.
	b := NewTracker()
	done := b.Begin()
	pid := startGroup(t, "sleep 60")
	go func() {
		time.Sleep(200 * time.Millisecond)
		b.Track(pid, "")
		done()
	}()

	b.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d started by a pending action is still running", pid)
	}
}

func TestTrackerAfterStop(t *testing.T) {
	// What is to be undone along with the processes is undone once they are
	// gone, in the order it was given, and again by a second stop, such as
	// the one at the end of a workflow a signal already stopped.
	b := NewTracker()
	pid := startGroup(t, "sleep 60")
	b.Track(pid, "")

	var calls []string
	b.AfterStop(func() {
		if groupAlive(pid) {
			t.Errorf("process group %d is still running when what follows the stop is called", pid)
		}
		calls = append(calls, "first")
	})
	b.AfterStop(func() { calls = append(calls, "second") })

	b.Stop()
	b.Stop()
	if want := []string{"first", "second", "first", "second"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}

	var none *Tracker
	none.AfterStop(func() { t.Error("a nil tracker calls nothing") })
	none.Stop()
}
