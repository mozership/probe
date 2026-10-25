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

// pendingWait is how long the trackers of these tests wait for a pending
// action, long enough for one on a loaded machine.
const pendingWait = 30 * time.Second

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

// waitGroupGone reports whether the group led by pid is gone within ten
// seconds, which a loaded machine can take. A killed process stays visible
// until it is reaped, which happens on its own schedule.
func waitGroupGone(pid int) bool {
	deadline := time.Now().Add(10 * time.Second)
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
	var tr *Tracker
	tr.Begin()()
	tr.Track(123, "")
	tr.Stop()
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

	tr := NewTracker(pendingWait)
	tr.Track(pid, log)
	tr.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d is still running", pid)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("log %s was not removed: %v", log, err)
	}
	if len(tr.procs) != 0 {
		t.Errorf("procs = %v, want none left", tr.procs)
	}
}

func TestTrackerStopIgnoringTerm(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the grace period")
	}
	// The shell says when the trap is installed and sleep has inherited it.
	// A fixed pause would not cover a slow start on a loaded machine, and a
	// shell signalled before its trap would exit at once.
	ready := filepath.Join(t.TempDir(), "ready")
	pid := startGroup(t, "trap '' TERM; sleep 60 & : > '"+ready+"'; wait")
	deadline := time.Now().Add(time.Minute)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the shell did not install its trap")
		}
		time.Sleep(20 * time.Millisecond)
	}

	tr := NewTracker(pendingWait)
	tr.Track(pid, "")

	start := time.Now()
	tr.Stop()
	elapsed := time.Since(start)

	if !waitGroupGone(pid) {
		t.Errorf("process group %d survived SIGKILL", pid)
	}
	if elapsed < TermGrace {
		t.Errorf("stopped after %v, want SIGKILL only after the %v grace", elapsed, TermGrace)
	}
}

func TestTrackerStopExited(t *testing.T) {
	// A process that already finished is skipped without waiting.
	pid := startGroup(t, "true")
	if !waitGroupGone(pid) {
		t.Fatalf("process group %d did not exit", pid)
	}

	tr := NewTracker(pendingWait)
	tr.Track(pid, "")

	start := time.Now()
	tr.Stop()
	// Waiting out the grace is what a skipped process must not do; anything
	// shorter is a loaded machine.
	if elapsed := time.Since(start); elapsed >= TermGrace {
		t.Errorf("stop took %v for a process that had exited, want less than the %v grace", elapsed, TermGrace)
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

	tr := NewTracker(pendingWait)
	tr.procs = []proc{{pid: pid, started: started + 1}}
	tr.Stop()

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

	tr := NewTracker(pendingWait)
	tr.Track(pid, "")
	tr.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d is still running", pid)
	}
}

func TestTrackerTrackAfterStop(t *testing.T) {
	// A step that timed out can report its process after the workflow has
	// stopped the others; it is stopped right away.
	tr := NewTracker(pendingWait)
	tr.Stop()

	pid := startGroup(t, "sleep 60")
	tr.Track(pid, "")

	if !waitGroupGone(pid) {
		t.Errorf("process group %d reported after stop is still running", pid)
	}
	if len(tr.procs) != 0 {
		t.Errorf("procs = %v, want none kept after stop", tr.procs)
	}
}

func TestTrackerStopWaitsForPending(t *testing.T) {
	// An action still running when the workflow ends gets a moment to report
	// the process it started.
	tr := NewTracker(pendingWait)
	done := tr.Begin()
	pid := startGroup(t, "sleep 60")
	go func() {
		time.Sleep(200 * time.Millisecond)
		tr.Track(pid, "")
		done()
	}()

	tr.Stop()

	if !waitGroupGone(pid) {
		t.Errorf("process group %d started by a pending action is still running", pid)
	}
}

func TestTrackerStopPendingWaitBounded(t *testing.T) {
	// An action that never returns holds Stop up only for the wait the
	// tracker was given, and a process it reports after that is stopped at
	// once.
	wait := 300 * time.Millisecond
	tr := NewTracker(wait)
	done := tr.Begin()
	defer done()

	start := time.Now()
	tr.Stop()
	if elapsed := time.Since(start); elapsed < wait || elapsed >= pendingWait {
		t.Errorf("stop took %v, want the %v wait for the pending action and no more than %v", elapsed, wait, pendingWait)
	}

	pid := startGroup(t, "sleep 60")
	tr.Track(pid, "")
	if !waitGroupGone(pid) {
		t.Errorf("process group %d reported after the wait is still running", pid)
	}
}

func TestTrackerAfterStop(t *testing.T) {
	// What is to be undone along with the processes is undone once they are
	// gone, in the order it was given, and again by a second stop, such as
	// the one at the end of a workflow a signal already stopped.
	tr := NewTracker(pendingWait)
	pid := startGroup(t, "sleep 60")
	tr.Track(pid, "")

	var calls []string
	tr.AfterStop(func() {
		if groupAlive(pid) {
			t.Errorf("process group %d is still running when what follows the stop is called", pid)
		}
		calls = append(calls, "first")
	})
	tr.AfterStop(func() { calls = append(calls, "second") })

	tr.Stop()
	tr.Stop()
	if want := []string{"first", "second", "first", "second"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}

	var none *Tracker
	none.AfterStop(func() { t.Error("a nil tracker calls nothing") })
	none.Stop()
}
