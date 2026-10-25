package probe

import (
	"sync"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/procgroup"
)

// newBackground returns the tracker of the processes a run's steps leave in
// the background. Its Stop waits for a step that timed out as long as its
// action can take to return, so that a process the action reports late is
// stopped with the others.
func newBackground() *procgroup.Tracker {
	return procgroup.NewTracker(actionrpc.ExitWait)
}

// ResponseTime provides response time information for expressions
type ResponseTime struct {
	Duration string  `expr:"duration"`
	Sec      float64 `expr:"sec"`
}

// ExitStatus represents the unified status across all actions
type ExitStatus int

const (
	ExitStatusSuccess ExitStatus = 0 // Action succeeded
	ExitStatusFailure ExitStatus = 1 // Action failed
)

// Int returns the ExitStatus as an int for comparison
func (e ExitStatus) Int() int {
	return int(e)
}

// String returns the ExitStatus as a string
func (e ExitStatus) String() string {
	switch e {
	case ExitStatusSuccess:
		return "success"
	case ExitStatusFailure:
		return "failure"
	default:
		return "unknown"
	}
}

// StepContext provides context data for step expression evaluation
type StepContext struct {
	Vars        map[string]any `expr:"vars"`
	Res         map[string]any `expr:"res"`
	Req         map[string]any `expr:"req"`
	RT          ResponseTime   `expr:"rt"`
	Report      string         `expr:"report"`
	Outputs     map[string]any `expr:"outputs"`
	Status      int            `expr:"status"`
	RepeatIndex int            `expr:"repeat_index"`
}

// JobContext is what a run of a job runs with. It is copied for each run,
// and holds by value only what is the run's own; what the jobs of the
// workflow share is in runEnv, and what the runs of a repeated job share in
// jobRepeat, which a copy points to as the context it was copied from does.
type JobContext struct {
	*runEnv
	// repeat is what the runs of the job share when they are counted, as
	// they are for a repeated job, or nil when the job is run once as it is.
	repeat *jobRepeat

	Vars map[string]any `expr:"vars"`
	// Outputs are those the run reads and writes (accessible via expressions
	// as "outputs"): the workflow's, or a store of the run's own in front of
	// them for a run that overlaps others.
	Outputs *Outputs `expr:"outputs"`

	Failed bool
	// Current job ID for this context
	CurrentJobID string
	// RepeatCurrent is the index of the run, from 0, warmup runs included.
	RepeatCurrent int
	// The state each action keeps in this run of the job
	states *actionStates
	// The name of the job, with its templates evaluated
	jobName string
}

// runEnv is what every job of a workflow, and every run of each, runs with.
// It is set up once, before any job starts.
type runEnv struct {
	Config
	// Print writer
	Printer *Printer
	// Result for managing job-level output
	Result *Result
	// Job scheduler for managing job dependencies and execution
	JobScheduler *JobScheduler
	// Processes that steps left running in the background, stopped once the
	// workflow is over
	background *procgroup.Tracker
	// Actions to call once the workflow is over, for each to remove what
	// its steps left behind
	cleanups *cleanups
	// Directory of the workflow file, which local actions are relative to
	baseDir string
	// The run of probe, which actions are told about
	runID string
	// The names the workflow gives its external actions, which an action
	// that runs steps of its own is told
	actions map[string]string
}

// runner returns the ActionRunner the steps of the run run their actions
// with: the one the run was given, or a PluginActionRunner.
func (e *runEnv) runner() ActionRunner {
	if e.ActionRunner == nil {
		return &PluginActionRunner{}
	}
	return e.ActionRunner
}

// jobRepeat is what the runs of a repeated job share: how they are counted,
// and the counts of each step over them.
type jobRepeat struct {
	// total is the number of runs counted; warmup runs are not.
	total int
	// warmup is the number of runs before those counted; see Repeat.Warmup.
	warmup int
	// tested is whether the job has a test over its runs; see Repeat.Test.
	tested bool
	// baselineTested is whether the job has a test against the run compared
	// to; see Repeat.BaselineTest.
	baselineTested bool
	// baselineSkipped is why that test is skipped, or empty when it is
	// checked; see baselineSkip.
	baselineSkipped string
	// reads are the ids of the steps the tests over the runs read, which
	// they check as a step's own test does. Those that only a skipped test
	// reads are left out, as nothing checks them.
	reads map[string]bool

	mu sync.Mutex
	// counters are the counts of each step, by Step.counterKey.
	counters map[int]StepRepeatCounter
}

// newJobRepeat returns the counts for total runs of a job, warmup runs
// among them.
func newJobRepeat(total, warmup int) *jobRepeat {
	return &jobRepeat{total: total - warmup, warmup: warmup, counters: make(map[int]StepRepeatCounter)}
}

// SetFailed marks the job context as failed
func (j *JobContext) SetFailed() {
	j.Failed = true
}

// repeating reports whether the runs of the job are counted, rather than
// each step's result reported as it is.
func (j *JobContext) repeating() bool {
	return j.repeat != nil
}

// warmingUp reports whether the run is one of the warmup runs of a repeated
// job, which the job's result leaves out.
func (j *JobContext) warmingUp() bool {
	return j.repeat != nil && j.RepeatCurrent < j.repeat.warmup
}

// countedOnly reports whether a failure in the run is counted and fails
// nothing by itself: in a warmup run, or in a run of a job whose test over
// its runs decides whether it passed.
func (j *JobContext) countedOnly() bool {
	return j.warmingUp() || (j.repeat != nil && j.repeat.tested)
}

// recordFailure notes the kind of a failure of a step for the exit code of
// the run, unless the failure is only counted.
func (j *JobContext) recordFailure(kind string) {
	if j.countedOnly() {
		return
	}
	j.Result.recordFailure(kind)
}
