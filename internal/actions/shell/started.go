package shell

import (
	"os"
	"sync"
	"syscall"
	"time"
)

// stopGrace is how long a background command is given to exit on SIGTERM
// before it is killed, as the workflow does.
const stopGrace = 3 * time.Second

// startedProc is a background command this process started.
type startedProc struct {
	pid int
	log string
	// exited is set once the command's shell has exited and been reaped,
	// after which its pid may be given to another process.
	exited bool
}

// started records the background commands this process started. The
// workflow learns of one only when the step's result reaches it, and an
// interrupt can end this process before then, so the commands are known
// here too.
var started struct {
	sync.Mutex
	procs []*startedProc
}

// StopStarted stops the background commands this process started, with
// their process groups, and removes their logs. It waits for a command that
// is being started, and a command started afterwards is not seen; it is
// meant for a process that is about to end because of a signal.
func StopStarted() {
	started.Lock()
	var groups []int
	var logs []string
	for _, p := range started.procs {
		logs = append(logs, p.log)
		if p.owned() {
			groups = append(groups, p.pid)
		}
	}
	started.procs = nil
	started.Unlock()

	stopGroups(groups)
	for _, log := range logs {
		_ = os.Remove(log)
	}
}

// stopGroups sends SIGTERM to each process group led by one of pids, and
// SIGKILL to those still alive stopGrace later.
func stopGroups(pids []int) {
	for _, pid := range pids {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(stopGrace)
	for _, pid := range pids {
		for groupAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if groupAlive(pid) {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
}

// owned reports whether the group led by p.pid is still the one this
// process started. Until the shell is reaped its pid cannot be reused. After
// that, a live group with no process of that pid is its children carrying
// on, since a pid is not handed out while a group uses it.
func (p *startedProc) owned() bool {
	if !p.exited {
		return true
	}
	return syscall.Kill(p.pid, 0) != nil && groupAlive(p.pid)
}

func groupAlive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}
