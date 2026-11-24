// Package outcome holds what a run of a workflow came to: the result of each
// step and job, as it is reported, and the kinds of failure that decide the
// exit code.
package outcome

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mozership/probe/action"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/internal/mask"
	"github.com/mozership/probe/internal/notes"
	"github.com/mozership/probe/internal/report"
)

// Status is how a step or a job ended.
type Status int

const (
	StatusSuccess Status = iota
	StatusError
	StatusWarning
	StatusSkipped
)

// RepeatCounter tracks the runs of a step of a repeated job.
type RepeatCounter struct {
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
	Failure *Failure
	// Checked is whether an action checked a response against a contract,
	// which checks the step as a test does, or the step is one that passes
	// without being checked, as one of the hello action does.
	Checked bool
	// Contract is what the first iteration's response was matched to in
	// its contract.
	Contract *report.Contract
	// Notes are those of the first run that failed, or of the last run
	// while none has, and NotesRun is that run, counted from 1 without the
	// warmup runs. NotesWarned is whether a run's notes were warned of, so
	// that a repeated job warns of them once.
	Notes       []notes.Note
	NotesRun    int
	NotesWarned bool
}

// KeepNotes keeps the notes of run, a run of the step that failed or not,
// as those of the step, while no run has failed: the notes are those of the
// run the failure is of, so that the two tell of one run, and until one
// fails, those of the last run. The runs of an async job end in any order,
// so a run that passed replaces the notes only of an earlier run.
func (c *RepeatCounter) KeepNotes(n []notes.Note, run int, failed bool) {
	if !failed && run < c.NotesRun {
		return
	}
	c.Notes, c.NotesRun = n, run
}

// Step is what a step came to.
type Step struct {
	Index int
	// ID is the id of the step, or step_ and where it is written in the job
	// when it has none, as the step that ran tells it.
	ID        string
	Name      string
	Status    Status
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
	RetryAttempt  int            // Number of attempts made (0 = retry not performed, 1 = succeeded first try)
	RetryMax      int            // Maximum retry attempts configured
	RepeatCounter *RepeatCounter // For repeat execution information
	// The fields below feed the report files. Unlike StartedAt and RT they are
	// recorded whether or not --timing is set, and they carry no terminal styling.
	Test    string        // The test expression, empty when the step has none
	Elapsed time.Duration // Wall time from the action's start to the result
	Failure *Failure      // Why the step failed; nil unless Status is StatusError
	// Contract is what the response was matched to in the contract the
	// action checked it against; nil when it checked none.
	Contract *report.Contract
	// Notes are what the action left for people in res.notes; for a
	// repeated step, those of its counter.
	Notes []notes.Note
}

// Failure kinds recorded on a Failure. They are the kinds a report
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

// Failure describes a failed step without terminal formatting, so that
// reports can serialize it.
type Failure struct {
	Kind    string
	Message string
	// Request and Response are what the action sent and received. They are left
	// nil when the action asked not to be dumped (res.dump: false).
	Request  map[string]any
	Response map[string]any
	// Violations are what broke the contract, when Kind is a contract's.
	Violations []report.Violation
	// Refused is what the guard of the run refused, when Kind is
	// FailureRefused, so that why is read from it rather than the message.
	Refused *action.Refused
	// Clause is the condition of the test that is false, when Kind is
	// FailureAssertion and it could be told, as it is: the report hides the
	// secrets in it as it shows it, as it does in Request and Response; Got
	// is what the test came to, when Kind is FailureTestType. The report
	// shows them under the step.
	Clause *expr.Clause
	Got    any
	// Head is what Message has before the condition, when there is one, so
	// that the report files can tell the condition anew with the secrets
	// known by then; see ReportMessage.
	Head string
}

// ReportMessage is the message of f as the report files have it. With a
// condition of a test that is false, it is Head and the condition told anew
// from the condition as it is, masked by m before its values are cut: a
// secret a later step taught m would otherwise be left in the message as
// the part of it the cut kept, which no masker finds. Without a condition,
// or with no masker, it is Message.
func (f *Failure) ReportMessage(m *mask.Masker) string {
	if m == nil || f.Clause == nil {
		return f.Message
	}
	return f.Head + ClauseSummary(MaskClause(m, *f.Clause))
}

// Prefixed returns a copy of f whose message starts with prefix, in the
// report files as well.
func (f Failure) Prefixed(prefix string) *Failure {
	f.Message = prefix + f.Message
	f.Head = prefix + f.Head
	return &f
}

// Job is what a job came to: how it ended and the results of its steps.
type Job struct {
	JobName string
	JobID   string
	// Status is how the job ended: StatusSuccess, StatusError or
	// StatusSkipped, which a job skipped by its skipif or by a job it needs
	// is. It is StatusSuccess while the job runs.
	Status      Status
	StartTime   time.Time
	EndTime     time.Time
	StepResults []Step // Store all step results for this job
	// RepeatTest is the outcome of the test of a repeated job over its
	// runs; nil when it has none.
	RepeatTest *RepeatTest
	// BaselineTest is the outcome of the test of a repeated job against the
	// run compared to; nil when it has none.
	BaselineTest *RepeatTest
	Mu           sync.Mutex
}

// Succeeded reports whether the job did not fail, which a skipped job did
// not. Callers hold jr.Mu.
func (jr *Job) Succeeded() bool {
	return jr.Status != StatusError
}

// RepeatTest is the outcome of the test of a repeated job.
type RepeatTest struct {
	Test   string
	Passed bool
	// Failure is why the test failed; nil when it passed.
	Failure *Failure
	// Skipped is why the test was not checked, which a test against the run
	// compared to is not when there is nothing to compare to; empty when it
	// was checked.
	Skipped string
}

// Run is what a run of a workflow came to: the result of each job, by its
// id, and the kinds of failure seen.
type Run struct {
	Jobs map[string]*Job
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
func (rs *Run) SetReporter(r Reporter) {
	rs.reporter = r
}

// NotifyStepStart tells the reporter that a step of the job of jobID
// began. The buffered reporter shows it by the spinner, the stream reporter
// by a line of progress. A nil Run, which some contexts run without,
// tells nothing.
func (rs *Run) NotifyStepStart(jobID, stepName string) {
	if rs == nil || rs.reporter == nil {
		return
	}
	rs.reporter.StepStart(jobID, stepName)
}

// NotifyJobDone tells the reporter that a job's report block is final.
func (rs *Run) NotifyJobDone(jobID string) {
	if rs.reporter == nil {
		return
	}
	if jr, exists := rs.Jobs[jobID]; exists {
		rs.reporter.JobDone(jobID, jr)
	}
}

// New returns a Run with no job.
func New() *Run {
	return &Run{
		Jobs: make(map[string]*Job),
	}
}

// AddStepResult adds sr to the job of jobID, and tells the reporter.
func (rs *Run) AddStepResult(jobID string, sr Step) {
	jr, exists := rs.Jobs[jobID]
	if !exists {
		return
	}

	jr.Mu.Lock()
	jr.StepResults = append(jr.StepResults, sr)
	jr.Mu.Unlock()

	// Notify outside the job lock: the reporter takes its own lock and may
	// render other jobs, so holding this one here would nest the two.
	if rs.reporter != nil {
		rs.reporter.StepDone(jobID, sr)
	}
}

// Reporter decides when the parts of a workflow report reach the user, and
// on which of stdout and stderr.
// Jobs run concurrently, so implementations are called from several
// goroutines and must serialize their own writes.
type Reporter interface {
	Start(name, description string)
	StepStart(jobID, stepName string)
	StepDone(jobID string, sr Step)
	JobDone(jobID string, jr *Job)
	Finish(rs *Run)
}

// RecordFailure notes that a failure of the given kind happened during the run.
func (rs *Run) RecordFailure(kind string) {
	if rs == nil || kind == "" {
		return
	}
	rs.failuresMu.Lock()
	defer rs.failuresMu.Unlock()
	if rs.failures == nil {
		rs.failures = make(map[string]bool)
	}
	rs.failures[kind] = true
}

// HasFailure reports whether a failure of the given kind happened during
// the run.
func (rs *Run) HasFailure(kind string) bool {
	rs.failuresMu.Lock()
	defer rs.failuresMu.Unlock()
	return rs.failures[kind]
}

// MaskClause returns c with the secrets m knows hidden in the condition and
// in what its operands evaluated to. The values are masked as data, before
// they are written out and cut: a secret that is cut, or whose quotes are
// escaped, is no longer the text the masker knows.
func MaskClause(m *mask.Masker, c expr.Clause) expr.Clause {
	out := expr.Clause{Expr: m.String(c.Expr)}
	for _, o := range c.Operands {
		out.Operands = append(out.Operands, expr.Operand{Expr: m.String(o.Expr), Value: m.Value(o.Value)})
	}
	return out
}

// MaxOperandLength is how much of a value a false condition compared is
// shown, as a whole response body may be one.
const MaxOperandLength = 200

// OperandText is an operand and its value on one line, as res.code = 500.
// A string is quoted, so that an empty one and one that ends with a newline
// can be seen, and anything else is written as JSON.
func OperandText(o expr.Operand) string {
	var value string
	switch v := o.Value.(type) {
	case nil:
		value = "nil"
	case string:
		value = strconv.Quote(v)
	default:
		if data, err := json.Marshal(v); err == nil {
			value = string(data)
		} else {
			value = fmt.Sprintf("%v", v)
		}
	}
	if r := []rune(value); len(r) > MaxOperandLength {
		value = string(r[:MaxOperandLength]) + "..."
	}
	return o.Expr + " = " + value
}

// ClauseSummary is a false condition on one line, for the message of a
// failure: res.code == 200 (res.code = 500).
func ClauseSummary(c expr.Clause) string {
	if len(c.Operands) == 0 {
		return c.Expr
	}
	parts := make([]string, len(c.Operands))
	for i, o := range c.Operands {
		parts[i] = OperandText(o)
	}
	return fmt.Sprintf("%s (%s)", c.Expr, strings.Join(parts, ", "))
}
