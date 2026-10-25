package probe

import (
	"sync"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/report"
)

// StatusType represents the status of execution
type StatusType int

const (
	StatusSuccess StatusType = iota
	StatusError
	StatusWarning
	StatusSkipped
)

// StepRepeatCounter tracks the execution results of repeated steps
type StepRepeatCounter struct {
	// ID is the id of the step, by which the test of the repeated job reads
	// its runs.
	ID           string
	SuccessCount int
	FailureCount int
	// SkippedCount is the runs in which the step was not run: an earlier
	// step of the run failed in a job with fail_fast, or its iteration
	// came to no entry. They are neither successes nor failures of the
	// step.
	SkippedCount int
	Name         string
	// Test is the test of the step the counter is of, empty when it has
	// none. The counter is kept by the index of the step as it ran, which
	// an iteration makes another than its index in the job, so what the
	// step's result needs of the step is kept here.
	Test        string
	LastResult  bool
	RepeatTotal int // Total number of times the step should be repeated
	// Echoes are what the step's echo came to in each run, as it is.
	Echoes []string
	// RTs is the time in seconds the action took in each run that it
	// returned one in, whether or not the step's test held.
	RTs []float64
	// Warmup is the number of runs before those counted, which the counts
	// and the times leave out.
	Warmup int
	// Tested is whether the job has a test over its runs, which decides
	// whether it passed in place of the runs of its steps.
	Tested bool
	// Failure is why the first iteration that failed did, so that the step's
	// result keeps the kind of failure, such as an action error, rather than
	// only the count.
	Failure *StepFailure
	// Checked is whether an action checked a response against a contract,
	// which checks the step as a test does, or the step is one that passes
	// without being checked, as one of the hello action does.
	Checked bool
	// Contract is what the first iteration's response was matched to in
	// its contract.
	Contract *report.Contract
}

// StepResult represents the result of a step execution
type StepResult struct {
	Index int
	// ID is the id of the step, or step_ and where it is written in the job
	// when it has none; see Step.repeatID.
	ID        string
	Name      string
	Status    StatusType
	StartedAt time.Time
	RT        string
	RTSec     float64
	WaitTime  string
	// Echo is what the step's echo came to, as it is: the report indents
	// and colors it. HasEcho is whether the step has an echo, which may
	// come to nothing.
	Echo          string
	HasEcho       bool
	Report        string
	HasTest       bool
	RetryAttempt  int                // Number of attempts made (0 = retry not performed, 1 = succeeded first try)
	RetryMax      int                // Maximum retry attempts configured
	RepeatCounter *StepRepeatCounter // For repeat execution information
	// The fields below feed the report files. Unlike StartedAt and RT they are
	// recorded whether or not --timing is set, and they carry no terminal styling.
	Test    string        // The test expression, empty when the step has none
	Elapsed time.Duration // Wall time from the action's start to the result
	Failure *StepFailure  // Why the step failed; nil unless Status is StatusError
	// Contract is what the response was matched to in the contract the
	// action checked it against; nil when it checked none.
	Contract *report.Contract
}

// Failure kinds recorded on a StepFailure. They are the kinds a report
// carries, so they are defined there.
const (
	FailureAssertion = report.FailureAssertion
	FailureTestError = report.FailureTestError
	FailureTestType  = report.FailureTestType
	FailureAction    = report.FailureAction
	FailureTemplate  = report.FailureTemplate

	FailureContractResponse = report.FailureContractResponse
	FailureContractRequest  = report.FailureContractRequest
	FailureRefused          = report.FailureRefused
)

// StepFailure describes a failed step without terminal formatting, so that
// reports can serialize it.
type StepFailure struct {
	Kind    string
	Message string
	// Request and Response are what the action sent and received. They are left
	// nil when the action asked not to be dumped (res.dump: false).
	Request  map[string]any
	Response map[string]any
	// Violations are what broke the contract, when Kind is a contract's.
	Violations []report.Violation
	// refused is what the guard of the run refused, when Kind is
	// FailureRefused, so that why is read from it rather than the message.
	refused *actionrpc.Refused
	// clause is the condition of the test that is false, when Kind is
	// FailureAssertion and it could be told, as it is: the report hides the
	// secrets in it as it shows it, as it does in Request and Response; got
	// is what the test came to, when Kind is FailureTestType. The report
	// shows them under the step.
	clause *expr.Clause
	got    any
}

// JobResult stores execution results for a job
type JobResult struct {
	JobName string
	JobID   string
	// Status is how the job ended: StatusSuccess, StatusError or
	// StatusSkipped, which a job skipped by its skipif or by a job it needs
	// is. It is StatusSuccess while the job runs.
	Status      StatusType
	StartTime   time.Time
	EndTime     time.Time
	StepResults []StepResult // Store all step results for this job
	// RepeatTest is the outcome of the test of a repeated job over its
	// runs; nil when it has none.
	RepeatTest *RepeatTestResult
	// BaselineTest is the outcome of the test of a repeated job against the
	// run compared to; nil when it has none.
	BaselineTest *RepeatTestResult
	mutex        sync.Mutex
}

// Succeeded reports whether the job did not fail, which a skipped job did
// not. Callers hold jr.mutex.
func (jr *JobResult) Succeeded() bool {
	return jr.Status != StatusError
}

// RepeatTestResult is the outcome of the test of a repeated job.
type RepeatTestResult struct {
	Test   string
	Passed bool
	// Failure is why the test failed; nil when it passed.
	Failure *StepFailure
	// Skipped is why the test was not checked, which a test against the run
	// compared to is not when there is nothing to compare to; empty when it
	// was checked.
	Skipped string
}

// Result manages execution results for multiple jobs
type Result struct {
	Jobs map[string]*JobResult
	// reporter is told as steps start and as results become final, so
	// that the report can be emitted incrementally. It is the one way the
	// run tells it anything. It may be nil, in which case nothing is told.
	reporter Reporter
	// failures holds the kinds of failure seen during the run, which decide
	// the exit code. Jobs run concurrently, so access goes through failuresMu.
	failures   map[string]bool
	failuresMu sync.Mutex
}

// SetReporter installs the reporter told when steps start and when steps
// and jobs finish.
func (rs *Result) SetReporter(r Reporter) {
	rs.reporter = r
}

// notifyStepStart tells the reporter that a step of the job of jobID
// began. The buffered reporter shows it by the spinner, the stream reporter
// by a line of progress. A nil Result, which some contexts run without,
// tells nothing.
func (rs *Result) notifyStepStart(jobID, stepName string) {
	if rs == nil || rs.reporter == nil {
		return
	}
	rs.reporter.StepStart(jobID, stepName)
}

// notifyJobDone tells the reporter that a job's report block is final.
func (rs *Result) notifyJobDone(jobID string) {
	if rs.reporter == nil {
		return
	}
	if jr, exists := rs.Jobs[jobID]; exists {
		rs.reporter.JobDone(jobID, jr)
	}
}

// NewResult creates a new Result instance
func NewResult() *Result {
	return &Result{
		Jobs: make(map[string]*JobResult),
	}
}

// AddStepResult adds a StepResult to the specified job result
func (rs *Result) AddStepResult(jobID string, stepResult StepResult) {
	jr, exists := rs.Jobs[jobID]
	if !exists {
		return
	}

	jr.mutex.Lock()
	jr.StepResults = append(jr.StepResults, stepResult)
	jr.mutex.Unlock()

	// Notify outside the job lock: the reporter takes its own lock and may
	// render other jobs, so holding this one here would nest the two.
	if rs.reporter != nil {
		rs.reporter.StepDone(jobID, stepResult)
	}
}
