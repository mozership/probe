package probe

import (
	"context"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/actionrpc"
)

// cleanupTimeout is how long an action is given to clean up after a run.
const cleanupTimeout = 30 * time.Second

// runningActionWait bounds how long run waits for the actions still running
// to return, so that what they leave is cleaned up too. One whose step timed
// out is being stopped, and returns once it has exited on SIGTERM within
// actionrpc.TermGrace, or a second after that, once it is given up on.
const runningActionWait = actionrpc.TermGrace + time.Second

// cleanups holds the external actions to call once a run is over, for each
// to remove what its steps left behind: those whose action.yml sets cleanup
// and that a step ran. An action is called once however many steps ran it.
// An action runs in a process that exits as soon as its step is done, so
// it cannot do that on its own. A nil cleanups holds nothing.
type cleanups struct {
	runID string
	// clean has the action that uses names clean up after the run.
	clean func(uses string) error
	// warn tells the user of a cleanup that failed.
	warn func(format string, args ...any)

	mu sync.Mutex
	// owed are the actions to call, each as a step's uses names it in full,
	// a local one as an absolute path, in the order they were first run.
	owed []string
	// running counts the steps of those actions that have not returned. One
	// can leave something behind even after its step timed out, so run
	// gives them a moment.
	running int
	// ran is set once run has called the actions; one that comes to be owed
	// after that is called as soon as it is.
	ran bool
	// passedOn is where the actions were passed on to, once they were:
	// the run this one is part of, which calls them when it is over. One
	// that comes to be owed after that is passed on as well.
	passedOn *actionrpc.Cleanups
	// written are the actions as a step's uses writes them, by their names
	// in full, for their log records to be named as the step's are.
	written map[string]string
}

// newCleanups returns the cleanups of the run runID, whose actions log and
// warn through printer. trace shows how the actions' processes are run as
// well, in a verbose run.
func newCleanups(runID string, printer *Printer, trace bool) *cleanups {
	c := &cleanups{runID: runID, warn: printer.LogWarn}
	c.clean = func(uses string) error {
		// The action was resolved when its step ran, so this fetches
		// nothing, unless a job the run embeds ran it in its own process.
		exe, err := actionref.Resolve(uses, "")
		if err != nil {
			return err
		}
		log := newActionLog(c.nameOf(uses), os.Stderr, printer.Masker(), actionLogLevel(printer.verbose, trace), printer.verbose)
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		return actionrpc.CleanupExecutable(ctx, exe.Path, exe.SHA256, actionrpc.CleanupCall{RunID: runID}, log)
	}
	return c
}

// begin is called before a step runs an action that is to clean up, and
// the function it returns once the action has returned. The action is owed
// its cleanup from here, whatever becomes of the step: one that times out,
// or is stopped by a signal, may have left something all the same.
func (c *cleanups) begin(uses string) func() {
	if c == nil {
		return func() {}
	}
	c.mu.Lock()
	switch {
	case c.passedOn != nil:
		c.passedOn.Add(uses)
	case !slices.Contains(c.owed, uses):
		c.owed = append(c.owed, uses)
	}
	c.running++
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		c.running--
		late := c.ran
		c.mu.Unlock()
		// The cleanup has been and gone, and missed what this step left.
		if late {
			c.call(uses)
		}
	}
}

// owe adds actions that a step ran in a process of its own, as the embedded
// action runs those of its job, and that are to clean up.
func (c *cleanups) owe(uses ...string) {
	if c == nil {
		return
	}
	var late []string
	c.mu.Lock()
	for _, u := range uses {
		switch {
		case c.passedOn != nil:
			c.passedOn.Add(u)
		case c.ran:
			late = append(late, u)
		case !slices.Contains(c.owed, u):
			c.owed = append(c.owed, u)
		}
	}
	c.mu.Unlock()
	for _, u := range late {
		c.call(u)
	}
}

// passOn hands the actions to to, the cleanups of the run this one is part
// of, which calls them once it is over; run then calls none. It does
// nothing after run, as when a signal ended this run first.
func (c *cleanups) passOn(to *actionrpc.Cleanups) {
	if c == nil || to == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ran {
		return
	}
	to.Add(c.owed...)
	c.owed = nil
	c.passedOn = to
}

// run calls each action once, the last one first run first, as what a later
// step started may use what an earlier one did. A cleanup that fails is
// told and changes nothing else: the run is over, and its result stands.
func (c *cleanups) run() {
	if c == nil {
		return
	}
	deadline := time.Now().Add(runningActionWait)
	for c.runningCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}

	c.mu.Lock()
	if c.ran || c.passedOn != nil {
		c.mu.Unlock()
		return
	}
	owed := c.owed
	c.owed = nil
	c.ran = true
	c.mu.Unlock()

	for _, uses := range slices.Backward(owed) {
		c.call(uses)
	}
}

// writtenAs records that a step's uses writes the action named uses in full
// as written.
func (c *cleanups) writtenAs(uses, written string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.written == nil {
		c.written = make(map[string]string)
	}
	c.written[uses] = written
}

// nameOf returns the action named uses in full as a step's uses writes it,
// or as it is when no step of this run wrote it, as one an embedded job ran.
func (c *cleanups) nameOf(uses string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if w, ok := c.written[uses]; ok {
		return w
	}
	return uses
}

func (c *cleanups) runningCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *cleanups) call(uses string) {
	if err := c.clean(uses); err != nil {
		c.warn("the cleanup of %s failed, so what its steps left behind in the run %s may still be there: %s", uses, c.runID, err)
	}
}
