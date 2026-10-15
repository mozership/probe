package probe

import (
	"sync"

	"github.com/mozership/probe/procgroup"
)

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

// JobContext provides context data for job execution
type JobContext struct {
	Vars map[string]any `expr:"vars"`
	Config
	Failed bool
	// Current job ID for this context
	CurrentJobID string
	// Repeat tracking
	IsRepeating   bool
	RepeatCurrent int
	RepeatTotal   int
	// RepeatWarmup is the number of runs before those counted; see
	// Repeat.Warmup. RepeatCurrent counts them, and RepeatTotal does not.
	RepeatWarmup int
	StepCounters map[int]StepRepeatCounter // step index -> counter
	countersMu   *sync.Mutex               // protects StepCounters for concurrent access
	// Print writer
	Printer *Printer
	// Result for managing job-level output
	Result *Result
	// Job scheduler for managing job dependencies and execution
	JobScheduler *JobScheduler
	// Shared outputs across all jobs (accessible via expressions as "outputs")
	Outputs *Outputs `expr:"outputs"`
	// Processes that steps left running in the background, stopped once the
	// workflow is over
	background *procgroup.Tracker
	// Actions to call once the workflow is over, for each to remove what
	// its steps left behind
	cleanups *cleanups
	// Directory of the workflow file, which local actions are relative to
	baseDir string
	// The state each action keeps in this run of the job
	states *actionStates
	// The run of probe, which actions are told about
	runID string
	// The name of the job, with its templates evaluated
	jobName string
	// The names the workflow gives its external actions, which an action
	// that runs steps of its own is told
	actions map[string]string
}

// SetFailed marks the job context as failed
func (j *JobContext) SetFailed() {
	j.Failed = true
}

// warmingUp reports whether the run is one of the warmup runs of a repeated
// job, which the job's result leaves out.
func (j *JobContext) warmingUp() bool {
	return j.RepeatCurrent < j.RepeatWarmup
}

// recordFailure notes the kind of a failure of a step for the exit code of
// the run, unless the run is a warmup run, which cannot fail the job.
func (j *JobContext) recordFailure(kind string) {
	if j.warmingUp() {
		return
	}
	j.Result.recordFailure(kind)
}
