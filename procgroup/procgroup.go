// Package procgroup stops the process groups that steps leave running in the
// background once the workflow that started them is over.
package procgroup

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// StopGrace is how long a background process is given to exit on SIGTERM
// before it is killed.
const StopGrace = 3 * time.Second

// proc is a process a step left running, along with the file its output goes
// to.
type proc struct {
	pid int
	log string
	// started is when the group's leader started, or 0 if it was already gone
	// when the step returned. It tells the leader apart from a later process
	// that happens to get the same pid.
	started int64
}

// Tracker records the processes that steps start in the background, so that
// they can be stopped when the workflow is over. An action runs in a plugin
// process that exits as soon as the step is done, and the background process
// is put in a session of its own to survive that, so nothing else would ever
// stop it. A nil Tracker records nothing.
type Tracker struct {
	mu    sync.Mutex
	procs []proc
	// pending counts actions still running. One can start a process
	// even after its step timed out, so stop gives them a moment to report.
	pending int
	// stopped is set once Stop has run; a process reported after that is
	// stopped as soon as it is.
	stopped bool
	// stopping is held for the whole of Stop, so that a second call, such
	// as one from a signal, returns only once the processes are gone.
	stopping sync.Mutex
	// afterStop are called by Stop once the processes are gone.
	afterStop []func()
}

// NewTracker returns a Tracker with nothing recorded.
func NewTracker() *Tracker {
	return &Tracker{}
}

// Begin is called before an action that can start a background process runs,
// and the function it returns once the action has returned and its process,
// if any, has been tracked. Stop waits a moment for such actions to report.
func (b *Tracker) Begin() func() {
	if b == nil {
		return func() {}
	}
	b.mu.Lock()
	b.pending++
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		b.pending--
		b.mu.Unlock()
	}
}

// Track records a process group a step started in the background, led by
// pid, and the file its output goes to, which is removed along with it. An
// empty log has no file to remove.
func (b *Tracker) Track(pid int, log string) {
	if b == nil || pid <= 0 {
		return
	}
	p := proc{pid: pid, log: log}
	if started, err := processStartTime(pid); err == nil {
		p.started = started
	}

	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		terminate([]proc{p})
		return
	}
	b.procs = append(b.procs, p)
	b.mu.Unlock()
}

// AfterStop has Stop call f once the processes are gone, each time it is
// called, for what else is to be undone when the workflow is over or probe
// is interrupted.
func (b *Tracker) AfterStop(f func()) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.afterStop = append(b.afterStop, f)
	b.mu.Unlock()
}

// StopOnSignal stops the recorded processes when probe is interrupted,
// terminated or hung up on, and then lets the signal end probe as it would
// have. The background processes are in sessions of their own, so the
// signal never reaches them, and nothing else would stop them. The first
// signal restores the default handling, so a second one ends probe at once.
// The returned function ends the watch.
func (b *Tracker) StopOnSignal() func() {
	if b == nil {
		return func() {}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			signal.Stop(ch)
			b.Stop()
			if s, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(os.Getpid(), s)
			}
			// The signal ends the process; finished is never closed, so the
			// workflow cannot return and exit before that.
		case <-done:
			close(finished)
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
		// A signal can end the workflow early, for example by killing the
		// plugin of a running step. Wait for its handling, so that probe
		// still ends by the signal after the cleanup.
		<-finished
	}
}

// Stop terminates every recorded process and removes its log file, and then
// calls what AfterStop was given.
func (b *Tracker) Stop() {
	if b == nil {
		return
	}
	b.stopping.Lock()
	defer b.stopping.Unlock()

	deadline := time.Now().Add(StopGrace)
	for b.pendingCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	b.mu.Lock()
	procs := b.procs
	b.procs = nil
	b.stopped = true
	after := b.afterStop
	b.mu.Unlock()

	terminate(procs)
	for _, f := range after {
		f()
	}
}

func (b *Tracker) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending
}

// terminate stops the given processes and removes their logs. Each process
// leads its own process group, so the whole group is signalled and the
// commands the shell started go with it. A group is only signalled while it
// is still the one the step started.
func terminate(procs []proc) {
	var owned []proc
	for _, p := range procs {
		if p.owned() {
			_ = syscall.Kill(-p.pid, syscall.SIGTERM)
			owned = append(owned, p)
		}
	}

	deadline := time.Now().Add(StopGrace)
	for _, p := range owned {
		for groupAlive(p.pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if groupAlive(p.pid) && p.owned() {
			_ = syscall.Kill(-p.pid, syscall.SIGKILL)
		}
	}

	for _, p := range procs {
		if p.log != "" {
			_ = os.Remove(p.log)
		}
	}
}

// owned reports whether the process group led by p.pid is still the one the
// step started. A pid is free for reuse once the whole group has gone, so a
// live process with that pid has to be the leader that was recorded. Without
// one, a live group is the leader's children carrying on: the system does not
// hand out a pid that a group still uses.
func (p proc) owned() bool {
	started, err := processStartTime(p.pid)
	if err == nil {
		return p.started != 0 && started == p.started
	}
	return groupAlive(p.pid)
}

// groupAlive reports whether any process in the group led by pid is left.
// A group that cannot be signalled is not one this run started.
func groupAlive(pid int) bool {
	return syscall.Kill(-pid, 0) == nil
}
