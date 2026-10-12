package probe

import (
	"sync"
	"time"

	"github.com/linyows/probe/report"
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
	SuccessCount int
	FailureCount int
	// SkippedCount is the runs in which the step was not run, because an
	// earlier step of the run failed in a job with fail_fast. They are
	// neither successes nor failures of the step.
	SkippedCount int
	Name         string
	// Test is the test of the step the counter is of, empty when it has
	// none. The counter is kept by the index of the step as it ran, which
	// an iteration makes another than its index in the job, so what the
	// step's result needs of the step is kept here.
	Test        string
	LastResult  bool
	RepeatTotal int      // Total number of times the step should be repeated
	EchoOutputs []string // Formatted echo output captured per iteration
	// Failure is why the first iteration that failed did, so that the step's
	// result keeps the kind of failure, such as an action error, rather than
	// only the count.
	Failure *StepFailure
	// Checked is whether an action checked a response against a contract,
	// which checks the step as a test does.
	Checked bool
	// Contract is what the first iteration's response was matched to in
	// its contract.
	Contract *report.Contract
}

// StepResult represents the result of a step execution
type StepResult struct {
	Index         int
	Name          string
	Status        StatusType
	StartedAt     time.Time
	RT            string
	RTSec         float64
	WaitTime      string
	TestOutput    string
	EchoOutput    string
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
}

// JobResult stores execution results for a job
type JobResult struct {
	JobName     string
	JobID       string
	Status      string
	StartTime   time.Time
	EndTime     time.Time
	Success     bool
	StepResults []StepResult // Store all step results for this job
	mutex       sync.Mutex
}

// Result manages execution results for multiple jobs
type Result struct {
	Jobs map[string]*JobResult
	// reporter is notified as results become final so that the report can be
	// emitted incrementally. It may be nil, in which case nothing is streamed.
	reporter Reporter
	// failures holds the kinds of failure seen during the run, which decide
	// the exit code. Jobs run concurrently, so access goes through failuresMu.
	failures   map[string]bool
	failuresMu sync.Mutex
}

// SetReporter installs the reporter notified when steps and jobs finish.
func (rs *Result) SetReporter(r Reporter) {
	rs.reporter = r
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
