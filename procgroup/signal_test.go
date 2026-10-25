package procgroup

import (
	"bufio"
	"errors"
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

// signalChildEnv has the test binary run as the probe that a signal ends,
// rather than run the tests. Its value is the log file of the process group
// the child tracks.
const signalChildEnv = "PROCGROUP_TEST_SIGNAL_CHILD_LOG"

func TestMain(m *testing.M) {
	if log := os.Getenv(signalChildEnv); log != "" {
		os.Exit(runSignalChild(log))
	}
	os.Exit(m.Run())
}

// runSignalChild watches for signals the way a workflow does, starts a
// process group in the background as the shell action does and tracks it,
// and prints the group's pid once it is ready for the signal. It returns
// only if no signal ends it in time.
func runSignalChild(log string) int {
	tr := NewTracker()
	end := tr.StopOnSignal()

	c := exec.Command("/bin/sh", "-c", "sleep 60 & wait")
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	go func() { _ = c.Wait() }()
	tr.Track(c.Process.Pid, log)
	fmt.Println(c.Process.Pid)

	time.Sleep(2 * time.Minute)
	end()
	tr.Stop()
	return 3
}

func TestStopOnSignal(t *testing.T) {
	log := filepath.Join(t.TempDir(), "probe-shell-action.1.log")
	if err := os.WriteFile(log, []byte("output\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), signalChildEnv+"="+log)
	child.Stderr = os.Stderr
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	t.Cleanup(func() { _ = child.Process.Kill() })

	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(out).ReadString('\n')
		ready <- strings.TrimSpace(line)
	}()
	var pid int
	select {
	case line := <-ready:
		if pid, err = strconv.Atoi(line); err != nil {
			t.Fatalf("the child printed %q, want the pid of its process group", line)
		}
	case <-time.After(time.Minute):
		t.Fatal("the child did not start its process group")
	}
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	if !groupAlive(pid) {
		t.Fatalf("process group %d is gone before the signal", pid)
	}

	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// The group is in a session of its own, so the signal reaches only the
	// child, which stops the group within the grace before it ends.
	select {
	case err = <-exited:
	case <-time.After(time.Minute):
		t.Fatal("the child did not exit on SIGTERM")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the child exited with %v, want it ended by SIGTERM", err)
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Errorf("the child exited with %v, want it ended by SIGTERM", err)
	}
	if !waitGroupGone(pid) {
		t.Errorf("process group %d is still running after the child ended", pid)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("log %s was not removed: %v", log, err)
	}
}

func TestStopOnSignalEnd(t *testing.T) {
	// Without a signal, ending the watch returns at once and leaves the
	// processes to the Stop that follows it.
	pid := startGroup(t, "sleep 60")
	tr := NewTracker()
	tr.Track(pid, "")

	end := tr.StopOnSignal()
	ended := make(chan struct{})
	go func() {
		end()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(30 * time.Second):
		t.Fatal("ending the watch without a signal did not return")
	}
	if !groupAlive(pid) {
		t.Errorf("process group %d was stopped by ending the watch", pid)
	}

	var none *Tracker
	none.StopOnSignal()()
}
