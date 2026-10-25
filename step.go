package probe

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/fielderr"
	"github.com/mozership/probe/internal/vareval"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

const (
	// DefaultStepTimeout is the default timeout for action execution
	DefaultStepTimeout = 5 * time.Minute
)

type Step struct {
	Name      string            `yaml:"name"`
	ID        string            `yaml:"id,omitempty"`
	Uses      string            `yaml:"uses" validate:"required"`
	With      map[string]any    `yaml:"with"`
	Test      string            `yaml:"test"`
	Echo      string            `yaml:"echo"`
	Vars      map[string]any    `yaml:"vars"`
	Iteration Iteration         `yaml:"iteration"`
	Wait      string            `yaml:"wait,omitempty"`
	SkipIf    string            `yaml:"skipif,omitempty"`
	Outputs   map[string]string `yaml:"outputs,omitempty"`
	Retry     *StepRetry        `yaml:"retry,omitempty"`
	Timeout   Interval          `yaml:"timeout,omitempty"`
}

// stepRun is one run of a step: with one entry of its iteration, in one run
// of its job. What the run finds out and leaves is kept here, and the step is
// left as it is written, so that the runs of a job repeated at once share it
// and a run starts with nothing an earlier one left.
type stepRun struct {
	*Step
	expr         *expr.Expr
	err          error
	ctx          StepContext
	retryAttempt int
	// attempt is the attempt of the action running now, from 1, which the
	// action is told about.
	attempt int
	// expandedName is the name with its templates evaluated.
	expandedName string
	startedAt    time.Time
	failure      *StepFailure
	// idx is where the run is among the steps the job ran, counting each
	// entry of an iteration.
	idx int
	// pos is where the step is among the steps the job is written with,
	// and ordinal which entry of its iteration it is running with. placed
	// is whether the job set them, as it does before it runs a step.
	pos, ordinal int
	placed       bool
	// templateErr holds the templates of the step's name and vars that
	// could not be evaluated, so that the step fails rather than run with
	// them.
	templateErr error
	// contract is the check the action made of the last attempt against a
	// contract, or nil when it checked it against none.
	contract *actionrpc.ContractCheck
	// given is the step's with, its templates evaluated, as the action was
	// given it; nil until they are.
	given map[string]any
}

// newStepRun begins a run of st whose templates ev evaluates, or a new
// evaluator when ev is nil.
func newStepRun(st *Step, ev *expr.Expr) *stepRun {
	if ev == nil {
		ev = &expr.Expr{}
	}
	return &stepRun{Step: st, expr: ev}
}

// writtenName is the name the step is written with, or a stand-in when it
// has none.
func (st *stepRun) writtenName() string {
	if st.Name == "" {
		return "Unknown Step"
	}
	return st.Name
}

func (st *stepRun) Do(jCtx *JobContext) {
	// 1. Preparation phase: validation, wait, skip check
	name, shouldContinue := st.prepare(jCtx)
	if !shouldContinue {
		return
	}

	if st.templateErr != nil {
		st.handleActionError(st.templateErr, name, jCtx)
		return
	}

	// 2. Action execution phase
	st.startedAt = time.Now()
	result, err := st.executeAction(name, jCtx)
	if err != nil {
		st.handleActionError(err, name, jCtx)
		return
	}

	// 3. Result processing phase
	st.processActionResult(result, jCtx)

	// 4. Finalization phase: test, echo, output save, result creation
	st.finalize(name, jCtx)
}

// prepare handles step preparation: validation, skip check, and wait
// Returns (stepName, shouldContinue)
func (st *stepRun) prepare(jCtx *JobContext) (string, bool) {
	// Evaluate step name. A name that cannot be evaluated is shown as
	// written, and fails the step unless it is skipped.
	name, err := st.expr.EvalTemplate(st.writtenName(), st.ctx)
	if err != nil {
		name = st.writtenName()
		st.templateErr = errors.Join(&expr.FieldError{Path: "name", Err: err}, st.templateErr)
	}

	st.expandedName = name
	jCtx.Printer.StepStart(jCtx.CurrentJobID, name)

	// Check if step should be skipped BEFORE waiting
	if st.shouldSkip(jCtx) {
		st.handleSkip(name, "Skip condition: "+st.SkipIf, false, jCtx)
		return name, false
	}

	// Handle wait only if step is not skipped
	st.handleWait(jCtx)

	return name, true
}

// executeAction executes the step action and returns the result
// If retry is configured, it will retry until status == 0 or max attempts reached
func (st *stepRun) executeAction(name string, jCtx *JobContext) (actionResult, error) {
	st.given = nil
	expW, err := st.expr.EvalTemplateMap(st.With, st.ctx)
	if err != nil {
		// The credentials that could be evaluated are learned, so that an
		// error about another value cannot show them, and the details of a
		// credential's own error are left out.
		jCtx.Printer.Masker().Learn(expW)
		return actionResult{}, redactCredentialErrors(fielderr.Prefix(err, "with."), st.With)
	}

	st.given = expW

	runner := jCtx.runner()
	st.attempt = 1

	// An action that does not keep to every kind of guard the run is under
	// is not run under it, unless the person running probe allowed it.
	if !jCtx.Guard.Runs(st.Uses) {
		flags := make([]string, 0, 2)
		for _, kind := range jCtx.Guard.Missing(st.Uses) {
			flags = append(flags, "--"+kind)
		}
		return actionResult{}, actionrpc.Refuse("the action %s does not keep to %s, which the run is under; let it run with --allow-action %s", st.Uses, strings.Join(flags, " and "), st.Uses)
	}

	// If no retry configuration, execute once
	if st.Retry == nil {
		return st.executeSingleAction(runner, expW, jCtx, false)
	}

	// Execute with retry logic
	return st.executeActionWithRetry(runner, expW, jCtx, name)
}

// executeSingleAction executes action once without retry
// quiet marks an attempt whose failure will be retried, so that the action's
// own error records stay out of the log.
func (st *stepRun) executeSingleAction(runner ActionRunner, expW map[string]any, jCtx *JobContext, quiet bool) (actionResult, error) {
	// Determine timeout duration
	timeout := DefaultStepTimeout
	if st.Timeout.Duration > 0 {
		timeout = st.Timeout.Duration
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Execute action in goroutine
	type result struct {
		ret      map[string]any
		state    map[string]any
		contract *actionrpc.ContractCheck
		err      error
	}
	resultCh := make(chan result, 1)

	// Learn the credentials the action is about to send before it runs, so
	// that its own log records already have them hidden.
	masker := jCtx.Printer.Masker()
	masker.Learn(expW)

	// A process the action leaves running in the background is tracked here
	// rather than after the select, so that one started after the step timed
	// out is still stopped with the others. Any action can leave one, as it
	// says in what it returns.
	done := jCtx.background.Begin()
	// The call is made up here: an attempt that timed out is stopped, which
	// takes a while, in the background while the next one changes the step.
	// The action is given ctx, so that it is stopped once the step times out
	// rather than run on unseen.
	opts := RunOptions{Verbose: jCtx.Verbose, Trace: jCtx.Trace, Quiet: quiet, Masker: masker, BaseDir: jCtx.baseDir, cleanups: jCtx.cleanups, ctx: ctx}
	call := actionrpc.Call{With: expW, State: jCtx.states.get(st.Uses), Step: st.stepInfo(jCtx), Guard: jCtx.Guard, Actions: jCtx.actions, Processes: &actionrpc.Processes{}, Contract: &actionrpc.Contract{}}
	go func() {
		defer done()
		ret, state, err := runner.Run(st.Uses, call, opts)
		// One left running is stopped once the run is over, whether or not
		// the action failed after it started it.
		for _, p := range call.Processes.List() {
			jCtx.background.Track(p.PID, p.Log)
		}
		resultCh <- result{ret: ret, state: state, contract: call.Contract.Check(), err: err}
	}()

	// Wait for either completion or timeout. The state of an action that
	// timed out is not kept, as its result is not.
	timedOut := errors.New("action execution timed out after " + timeout.String())
	st.contract = nil
	select {
	case res := <-resultCh:
		// An action stopped for the timeout can return before the timeout
		// is seen here, with an error that says less, even before ctx's own
		// timer has gone off.
		if d, _ := ctx.Deadline(); res.err != nil && !time.Now().Before(d) {
			return actionResult{}, timedOut
		}
		if res.err != nil {
			return actionResult{}, res.err
		}
		if err := jCtx.states.set(st.Uses, res.state); err != nil {
			return actionResult{}, err
		}
		st.contract = res.contract
		return readActionResult(res.ret), nil
	case <-ctx.Done():
		return actionResult{}, timedOut
	}
}

// stepInfo tells an action about the step it runs for.
func (st *stepRun) stepInfo(jCtx *JobContext) actionrpc.Step {
	attempt := max(st.attempt, 1)
	return actionrpc.Step{
		RunID:   jCtx.runID,
		JobID:   jCtx.CurrentJobID,
		JobName: jCtx.jobName,
		Index:   st.idx,
		ID:      st.repeatID(),
		Name:    st.expandedName,
		Repeat:  jCtx.RepeatCurrent,
		Attempt: attempt,
	}
}

// executeActionWithRetry executes action with retry logic based on test assertion results.
// If no test is configured, retry is not performed and the action is executed once.
func (st *stepRun) executeActionWithRetry(runner ActionRunner, expW map[string]any, jCtx *JobContext, name string) (actionResult, error) {
	retry := st.Retry
	st.retryAttempt = 0

	// Initial delay if specified
	if retry.InitialDelay.Duration > 0 {
		if jCtx.Verbose {
			jCtx.Printer.LogDebug("Initial delay before first attempt: %v", retry.InitialDelay.Duration)
		}
		time.Sleep(retry.InitialDelay.Duration)
	}

	var lastResult actionResult
	var lastErr error

	// Retry loop
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		if jCtx.Verbose {
			jCtx.Printer.LogDebug("Executing action with retry: attempt %d/%d", attempt, retry.MaxAttempts)
		}

		// Only the final attempt reports a failure as such; before that the
		// step still has a chance to succeed. Whether a step without a test
		// is checked at all, by a contract, is known only from its first
		// result, so its first attempt reports a failure as a final one.
		st.attempt = attempt
		quiet := attempt < retry.MaxAttempts && (st.Test != "" || attempt > 1)
		result, err := st.executeSingleAction(runner, expW, jCtx, quiet)
		lastResult = result
		lastErr = err

		if err != nil {
			// A step without a test is retried only once a contract has
			// shown that something checks it. What the guard refused is
			// refused again, so it is not retried.
			if (st.Test == "" && attempt == 1) || actionrpc.IsRefused(err) {
				return result, err
			}
			// Action execution failed, retry
			if attempt < retry.MaxAttempts {
				if jCtx.Verbose {
					jCtx.Printer.LogDebug("Action execution failed (attempt %d), retrying after %v", attempt, retry.Interval.Duration)
				}
				time.Sleep(retry.Interval.Duration)
			}
			continue
		}

		// Process action result to set context for test evaluation
		st.processActionResult(result, jCtx)

		// A step that nothing checks runs once, as there is nothing a
		// retry could wait for.
		if !st.checked() {
			return result, nil
		}

		// A response that breaks its contract fails as a test does, and the
		// test is not evaluated, as check does not evaluate it.
		testOk := st.contractFailure() == nil
		if testOk && st.Test != "" {
			exprOut, err := st.evalTest()
			testOk = err == nil && exprOut == true
		}
		if testOk {
			if jCtx.Verbose {
				jCtx.Printer.LogDebug("Action succeeded on attempt %d", attempt)
			}
			st.retryAttempt = attempt
			return result, nil
		}

		// Test failed, retry if not the last attempt
		if attempt < retry.MaxAttempts {
			if jCtx.Verbose {
				jCtx.Printer.LogDebug("Test failed (attempt %d), retrying after %v", attempt, retry.Interval.Duration)
			}
			time.Sleep(retry.Interval.Duration)
		}
	}

	st.retryAttempt = retry.MaxAttempts

	// All attempts failed
	if jCtx.Verbose {
		jCtx.Printer.LogDebug("All retry attempts failed (%d attempts)", retry.MaxAttempts)
	}
	return lastResult, lastErr
}

// handleActionError handles a step that failed before its test could run:
// the action returned an error, or a template the action needed could not be
// evaluated, in which case the action did not run.
func (st *stepRun) handleActionError(err error, name string, jCtx *JobContext) {
	kind := failureKindOf(err)
	// The error is kept as it is: what the action, the template or the
	// guard said is what tells a person why the step failed.
	st.err = err
	// The step's result shows the error, so it is told as it happens only
	// in a verbose run, where it reads in the run's order beside the
	// exchange. Otherwise it would show twice, and once for each run of a
	// repeated step, and break into the spinner's line.
	if jCtx.Verbose {
		switch kind {
		case FailureTemplate:
			jCtx.Printer.PrintError("Template evaluation failed: %v", err)
		case FailureRefused:
			jCtx.Printer.PrintError("%v", err)
		default:
			jCtx.Printer.PrintError("Action execution failed: %v", err)
		}
	}
	jCtx.SetFailed()
	jCtx.recordFailure(kind)

	// Create and add step result for failed action execution. An action
	// that failed returned no request, and what the step gave it is shown
	// in its place, so that the run still tells what the step asked for.
	if jCtx.Verbose {
		if len(st.ctx.Req) == 0 && len(st.given) > 0 {
			jCtx.Printer.PrintGivenRequest(st.idx, name, st.given)
		} else {
			jCtx.Printer.PrintRequestResponse(st.idx, name, st.ctx.Req, st.ctx.Res, st.ctx.RT.Duration)
		}
	}

	// Handle repeat execution
	if jCtx.repeating() {
		st.handleRepeatExecution(jCtx, name, true) // true = hasError
		return
	}

	// Standard execution: create result for failed step
	stepResult := st.createFailedStepResult(name, jCtx)

	// Add step result to workflow buffer
	if jCtx.Result != nil {
		jCtx.Result.AddStepResult(jCtx.CurrentJobID, stepResult)
	}

	if jCtx.Verbose {
		jCtx.Printer.PrintSeparator()
	}
}

// processActionResult sets the context the step's test and echo are
// evaluated in from what the action returned.
func (st *stepRun) processActionResult(r actionResult, jCtx *JobContext) {
	// A response can hand out a credential too, such as a session cookie.
	jCtx.Printer.Masker().Learn(r.Res)

	st.ctx.Req = r.Req
	st.ctx.Res = r.Res
	st.ctx.Status = r.Status
	st.ctx.RT = r.RT
}

// finalize handles the final phase: test, echo, output save, and result creation
func (st *stepRun) finalize(name string, jCtx *JobContext) {
	if jCtx.Verbose {
		jCtx.Printer.PrintRequestResponse(st.idx, name, st.ctx.Req, st.ctx.Res, st.ctx.RT.Duration)
	}

	// Outputs are saved before the test and echo, as always; a repeated job
	// used to skip this, so its later steps could not read its earlier ones.
	st.saveOutputs(jCtx)

	// Handle repeat execution
	if jCtx.repeating() {
		st.handleRepeatExecution(jCtx, name, false) // false = no error
		return
	}

	// Standard execution: create result
	stepResult := st.createStepResult(name, jCtx)

	// Add step result to workflow buffer
	if jCtx.Result != nil {
		jCtx.Result.AddStepResult(jCtx.CurrentJobID, stepResult)
	}

	if jCtx.Verbose {
		jCtx.Printer.PrintSeparator()
	}
}

// createStepResult creates a StepResult from step execution
func (st *stepRun) createStepResult(name string, jCtx *JobContext) StepResult {
	result := StepResult{
		Index:    st.idx,
		ID:       st.repeatID(),
		Name:     name,
		HasTest:  st.checked() || st.needsNoCheck(),
		RT:       "",
		WaitTime: st.getWaitTimeForDisplay(),
		Test:     st.Test,
		Elapsed:  st.elapsed(),
	}

	if st.Retry != nil && st.retryAttempt > 0 {
		result.RetryAttempt = st.retryAttempt
		result.RetryMax = st.Retry.MaxAttempts
	}

	if jCtx.Timing {
		if !st.startedAt.IsZero() {
			result.StartedAt = st.startedAt
		}
		if st.ctx.RT.Duration != "" {
			result.RT = st.ctx.RT.Duration
			result.RTSec = st.ctx.RT.Sec
		}
	}
	if v, ok := st.ctx.Res["report"]; ok {
		if report, sok := v.(string); sok {
			result.Report = report
		}
	}

	result.Contract = st.contractMatched()

	if st.checked() {
		testOutput, ok := st.check(jCtx.Printer)
		if ok {
			result.Status = StatusSuccess
		} else {
			result.Status = StatusError
			result.TestOutput = testOutput
			result.Failure = st.failure
			jCtx.SetFailed()
			jCtx.Result.recordFailure(st.failure.Kind)
		}
	} else if st.needsNoCheck() {
		result.Status = StatusSuccess
	} else {
		result.Status = StatusWarning
	}

	if st.Echo != "" {
		result.EchoOutput = st.getEchoOutput()
	}

	return result
}

// getEchoOutput returns the echo output as string
func (st *stepRun) getEchoOutput() string {
	exprOut, err := st.expr.EvalTemplate(st.Echo, st.ctx)
	return echoDetail(exprOut, err)
}

func (st *stepRun) handleRepeatExecution(jCtx *JobContext, name string, hasError bool) {
	// Execute test first (outside of lock)
	hasTest := st.checked()
	testResult := true

	// If there was an error, always count as failure
	if hasError {
		testResult = false
	} else if hasTest {
		_, testResult = st.check(jCtx.Printer)
		if !testResult {
			jCtx.SetFailed()
			jCtx.recordFailure(st.failure.Kind)
		}
	}
	// The test of a warmup run is checked all the same, since a failure
	// skips the steps after it in a fail_fast job, but the run is counted in
	// nothing.
	if jCtx.warmingUp() {
		return
	}

	var failure *StepFailure
	switch {
	case hasError && st.err != nil:
		failure = st.newFailure(failureKindOf(st.err), st.err.Error())
	case hasTest && !testResult:
		failure = st.failure
	}

	// Evaluate echo before taking the lock so we can store the formatted
	// output on the counter alongside the success/failure increment.
	var echoRaw, echoFormatted string
	var echoErr error
	if st.Echo != "" {
		echoRaw, echoErr = st.expr.EvalTemplate(st.Echo, st.ctx)
		echoFormatted = echoDetail(echoRaw, echoErr)
	}

	// Update counter atomically with lock held throughout
	jCtx.repeat.mu.Lock()
	counter, exists := jCtx.repeat.counters[st.counterKey()]
	if !exists {
		counter = StepRepeatCounter{
			ID:          st.repeatID(),
			Name:        name,
			Test:        st.Test,
			RepeatTotal: jCtx.repeat.total,
		}
	}

	// Update counts based on result
	if hasError || (hasTest && !testResult) {
		counter.FailureCount++
	} else {
		counter.SuccessCount++
	}
	counter.LastResult = testResult
	// A run whose action ended in an error returned no time. What the
	// context holds then is that of an earlier attempt of a retry, if any.
	if !hasError && st.ctx.RT.Duration != "" {
		counter.RTs = append(counter.RTs, st.ctx.RT.Sec)
	}
	if st.contractChecked() || st.needsNoCheck() {
		counter.Checked = true
	}
	if counter.Contract == nil {
		counter.Contract = st.contractMatched()
	}
	if counter.Failure == nil {
		counter.Failure = failure
	}

	if st.Echo != "" {
		counter.EchoOutputs = append(counter.EchoOutputs, echoFormatted)
	}

	// Store updated counter back to map. The step's result is made from the
	// counter once every run is done (Executor.appendRepeatStepResults).
	jCtx.repeat.counters[st.counterKey()] = counter
	jCtx.repeat.mu.Unlock()

	if st.Echo != "" {
		if echoErr != nil {
			jCtx.Printer.LogError("Echo evaluation failed: %#v (input: %s)", echoErr, st.Echo)
		} else {
			jCtx.Printer.PrintEchoContent(echoRaw)
		}
	}
}

// contractChecked reports whether the action checked the step against a
// contract, such as an OpenAPI document.
func (st *stepRun) contractChecked() bool {
	return st.contract != nil
}

// contractMatched returns what the action matched the step to in the
// contract it checked it against, or nil when it checked none or the
// contract has nothing the step matches.
func (st *stepRun) contractMatched() *report.Contract {
	if st.contract == nil || st.contract.Matched == nil {
		return nil
	}
	m := st.contract.Matched
	return &report.Contract{Spec: m.Spec, Operation: m.Operation, Response: m.Response}
}

// checked reports whether anything checks the step: its test, or a contract
// the action checked the response against.
func (st *stepRun) checked() bool {
	return st.Test != "" || st.contractChecked()
}

// needsNoCheck reports whether the step passes without being checked: a step
// of the hello action, which does nothing that could fail, so that one there
// for its echo, or to publish what it is given, is not counted as untested.
// probe check does not warn of it either.
func (st *stepRun) needsNoCheck() bool {
	return st.Uses == "hello"
}

// contractFailure returns why the request or the response breaks its
// contract, or nil when both keep to it or the action checked them against
// none.
func (st *stepRun) contractFailure() *StepFailure {
	if st.contract == nil || len(st.contract.Violations) == 0 {
		return nil
	}
	vs := make([]report.Violation, 0, len(st.contract.Violations))
	for _, v := range st.contract.Violations {
		vs = append(vs, report.Violation{In: v.In, Field: v.Field, Reason: v.Reason, Message: v.Message})
	}
	// A request that breaks the contract is the workflow's to fix, and the
	// response to it may break the contract only because of it, so the
	// request decides the kind.
	var inRequest, inResponse int
	for _, v := range vs {
		if v.In == "request" {
			inRequest++
		} else {
			inResponse++
		}
	}
	var kind, msg string
	switch {
	case inRequest == 0:
		kind, msg = FailureContractResponse, "the response breaks its contract in "+places(inResponse)
	case inResponse == 0:
		kind, msg = FailureContractRequest, "the request breaks its contract in "+places(inRequest)
	default:
		kind = FailureContractRequest
		msg = fmt.Sprintf("the request breaks its contract in %s, and the response in %s", places(inRequest), places(inResponse))
	}
	f := st.newFailure(kind, msg)
	f.Violations = vs
	return f
}

// places is n places in words, as in "in 1 place" or "in 2 places".
func places(n int) string {
	if n == 1 {
		return "1 place"
	}
	return fmt.Sprintf("%d places", n)
}

// check checks the step: the request and the response against their
// contract, then the test, which is not evaluated when either breaks it.
func (st *stepRun) check(printer *Printer) (string, bool) {
	if f := st.contractFailure(); f != nil {
		st.failure = f
		return contractDetail(printer.Masker(), f), false
	}
	if st.Test == "" {
		st.failure = nil
		return "", true
	}
	return st.DoTest(printer)
}

// evalTest evaluates the test expression and returns the raw result
// without generating any formatted output.
func (st *stepRun) evalTest() (any, error) {
	return st.expr.Eval(st.Test, st.ctx)
}

func (st *stepRun) DoTest(printer *Printer) (string, bool) {
	st.failure = nil

	exprOut, err := st.evalTest()
	if err != nil {
		st.failure = &StepFailure{Kind: FailureTestError, Message: err.Error()}
		printer.logTestError(st.Test, err)
		return testErrorDetail(err), false
	}

	boolOutput, boolOk := exprOut.(bool)
	if !boolOk {
		st.failure = &StepFailure{
			Kind:    FailureTestType,
			Message: fmt.Sprintf("test evaluated to %v (%T), not a boolean", exprOut, exprOut),
		}
		printer.LogDebug("%s", typeMismatchText(st.Test, exprOut))
		return typeMismatchDetail(st.Test, exprOut), false
	}

	if printer.verbose {
		printer.PrintTestResult(boolOutput, st.Test, st.ctx)
	}

	if !boolOutput {
		// Which condition of the test is false is told with what it
		// compared, unless the action keeps its response out of the report.
		message := "test evaluated to false"
		var detail string
		if clause, ok := st.expr.FalseClause(st.Test, st.ctx); ok {
			if dump, isBool := st.ctx.Res["dump"].(bool); isBool && !dump {
				clause.Operands = nil
			}
			// The message is cut before the report masks it, so the
			// secrets are hidden here, in the values themselves.
			clause = maskClause(printer.Masker(), clause)
			message += ": " + clauseSummary(clause)
			detail = falseClauseDetail(clause)
		}
		st.failure = st.newFailure(FailureAssertion, message)
		return detail + exchangeDetail(printer.Masker(), st.ctx.Req, st.ctx.Res), false
	}

	return "", true
}

// newFailure builds a StepFailure carrying the request and response, unless
// the action opted out of dumping them with res.dump: false.
func (st *stepRun) newFailure(kind, message string) *StepFailure {
	f := &StepFailure{Kind: kind, Message: message}
	if dump, ok := st.ctx.Res["dump"].(bool); ok && !dump {
		return f
	}
	f.Request = st.ctx.Req
	f.Response = st.ctx.Res
	return f
}

// elapsed is the time since the action started, or zero if it never did.
func (st *stepRun) elapsed() time.Duration {
	if st.startedAt.IsZero() {
		return 0
	}
	return time.Since(st.startedAt)
}

func (st *stepRun) SetCtx(j JobContext, override map[string]any) {
	// Use outputs from the unified Outputs structure
	var outputs map[string]any
	if j.Outputs != nil {
		outputs = j.Outputs.GetAll()
	}

	// Create context for step vars evaluation
	evalCtx := StepContext{
		Vars:        j.Vars,
		Outputs:     outputs,
		RepeatIndex: j.RepeatCurrent,
	}

	// Evaluate step-level vars with access to outputs. A var that cannot be
	// evaluated is left out, and fails the step when it runs.
	st.templateErr = nil
	evaluatedStepVars := make(map[string]any)
	if len(st.Vars) > 0 {
		ev := &expr.Expr{}
		var errs []error
		// Sorted so that the errors come in the same order on every run.
		for _, k := range slices.Sorted(maps.Keys(st.Vars)) {
			out, err := vareval.EvalValue(ev, k, st.Vars[k], evalCtx)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			evaluatedStepVars[k] = out
		}
		st.templateErr = errors.Join(errs...)
	}

	// Merge workflow vars with evaluated step vars
	vers := mergeMaps(j.Vars, evaluatedStepVars)
	if override != nil {
		vers = mergeMaps(vers, override)
	}

	st.ctx = StepContext{
		Vars:        vers,
		Outputs:     outputs,
		RepeatIndex: j.RepeatCurrent,
	}
}

// handleWait processes the wait field and sleeps if necessary
func (st *stepRun) handleWait(jCtx *JobContext) {
	if st.Wait == "" {
		return
	}

	// Evaluate wait expression template first
	waitExpr, err := st.expr.EvalTemplate(st.Wait, st.ctx)
	if err != nil {
		jCtx.Printer.PrintError("wait expression evaluation error: %v", err)
		return
	}

	duration, err := parseWaitDuration(waitExpr)
	if err != nil {
		jCtx.Printer.PrintError("wait duration parsing error: %v", err)
		return
	}

	if duration > 0 {
		sleepWithMessage(duration, st.waitMessage(duration), jCtx.Printer.AddSpinnerSuffix)
	}
}

// waitMessage is what the spinner shows while the step waits for duration:
// the wait and the name of the step, evaluated as prepare evaluated it.
func (st *stepRun) waitMessage(duration time.Duration) string {
	msg := colorWarning().Sprintf("(%s wait)", st.formatWaitTime(duration))
	return fmt.Sprintf("%s %s", msg, st.expandedName)
}

func sleepWithMessage(d time.Duration, m string, fn func(m string)) {
	if d < time.Second {
		time.Sleep(d)
		return
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timer := time.NewTimer(d)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			fn(m)
		case <-timer.C:
			return
		}
	}
}

// parseWaitDuration parses wait string to time.Duration
func parseWaitDuration(wait string) (time.Duration, error) {
	// Check if it's a plain number (treat as seconds for backward compatibility)
	if matched, _ := regexp.MatchString(`^\d+$`, wait); matched {
		if seconds, err := strconv.Atoi(wait); err == nil {
			return time.Duration(seconds) * time.Second, nil
		}
		return 0, fmt.Errorf("invalid wait value: %s", wait)
	}

	// Parse as duration string (e.g., "1s", "500ms", "2m")
	duration, err := time.ParseDuration(wait)
	if err != nil {
		return 0, fmt.Errorf("invalid wait format: %s", wait)
	}

	return duration, nil
}

// formatWaitTime formats duration for display
func (st *stepRun) formatWaitTime(duration time.Duration) string {
	if duration < time.Second {
		return duration.String()
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}

// getWaitTimeForDisplay returns formatted wait time for display
func (st *stepRun) getWaitTimeForDisplay() string {
	if st.Wait == "" {
		return ""
	}

	// Note: st.ctx might not be fully initialized when this is called during result creation
	// So we need to handle template evaluation carefully
	waitExpr := st.Wait
	if st.expr != nil {
		if evaluated, err := st.expr.EvalTemplate(st.Wait, st.ctx); err == nil {
			waitExpr = evaluated
		}
	}

	duration, err := parseWaitDuration(waitExpr)
	if err != nil {
		// If template evaluation failed and we still have template syntax, return empty
		// This prevents display errors during result creation
		if strings.Contains(waitExpr, "{{") {
			return ""
		}
		return ""
	}

	return st.formatWaitTime(duration)
}

// shouldSkip evaluates the skipif expression and returns true if step should be skipped
func (st *stepRun) shouldSkip(jCtx *JobContext) bool {
	if st.SkipIf == "" {
		return false
	}

	result, err := st.expr.Eval(st.SkipIf, st.ctx)
	if err != nil {
		jCtx.Printer.PrintError("skipif evaluation error: %v", err)
		return false // Don't skip on evaluation error
	}

	boolResult, ok := result.(bool)
	if !ok {
		jCtx.Printer.PrintError("skipif expression must return boolean, got: %T", result)
		return false // Don't skip on type error
	}

	return boolResult
}

// counterEntries is how many entries of an iteration the counters of a step
// in a repeated job have keys for. An iteration takes no more entries than
// that (maxIterationEntries), so each entry has a key of its own.
const counterEntries = maxIterationEntries

// counterKey is what the counter of the step is kept by in a repeated job:
// where the step is written in the job, and which entry of its iteration it
// runs with. The index it runs with would not do: an iteration that comes to
// another number of entries in each run gives the steps after it another
// index in each, and their counters would then count one another's runs.
// A step run without a job, as a test runs one, is kept by its index.
func (st *stepRun) counterKey() int {
	if !st.placed {
		return st.idx
	}
	return st.pos*counterEntries + st.ordinal
}

// repeatID is the id the test of a repeated job reads the step's runs by,
// and a later run's report finds the step by: its id, or step_ and where it
// is written in the job when it has none, as a loaded workflow gives it.
// Every entry of an iteration has the same.
func (st *stepRun) repeatID() string {
	if st.ID != "" {
		return st.ID
	}
	pos := st.idx
	if st.placed {
		pos = st.pos
	}
	return defaultStepID(pos)
}

// defaultStepID is the id of a step without one, by where it is written in
// its job, as a loaded workflow gives it.
func defaultStepID(pos int) string {
	return fmt.Sprintf("step_%d", pos)
}

// skipAfterFailure reports the step as skipped without running it, as a job
// with fail_fast does each step after one that failed.
func (st *stepRun) skipAfterFailure(jCtx *JobContext) {
	st.skipWithoutRun("An earlier step of the job failed, and the job has fail_fast", jCtx)
}

// skipWithoutRun reports the step as skipped for reason without running any
// of it: for a step that failed before it, or for an iteration that came to
// no entry. Its name is the only thing of it that is evaluated. Neither is
// the step's own doing, as a skipif is, so in a repeated job the run counts
// as neither a success nor a failure of it.
func (st *stepRun) skipWithoutRun(reason string, jCtx *JobContext) {
	st.handleSkip(st.nameWithoutRun(), reason, true, jCtx)
}

// failBeforeRun reports the step as failed with err before any of it ran,
// as when what it would run with cannot be worked out.
func (st *stepRun) failBeforeRun(err error, jCtx *JobContext) {
	name := st.nameWithoutRun()
	jCtx.Printer.StepStart(jCtx.CurrentJobID, name)
	st.handleActionError(err, name, jCtx)
}

// nameWithoutRun evaluates and returns the name of a step that is reported
// without being run. A name that cannot be evaluated is shown as written.
func (st *stepRun) nameWithoutRun() string {
	name, err := st.expr.EvalTemplate(st.writtenName(), st.ctx)
	if err != nil {
		name = st.writtenName()
	}
	st.expandedName = name
	return name
}

// handleSkip handles the skipped step logic. reason says why it is skipped,
// for the verbose log, and notRun whether it is for something other than
// its own skipif: a step that failed before it, or an iteration without an
// entry.
func (st *stepRun) handleSkip(name, reason string, notRun bool, jCtx *JobContext) {
	// The step is logged and then recorded: a verbose run reports the
	// steps it skipped as any other run does.
	if jCtx.Verbose {
		jCtx.Printer.LogDebug("%s", colorWarning().Sprintf("--- Step %d: %s (SKIPPED)", st.idx, name))
		jCtx.Printer.LogDebug("%s", reason)
		jCtx.Printer.PrintSeparator()
	}

	// Handle repeat execution for skipped steps
	if jCtx.repeating() {
		st.handleSkipRepeatExecution(jCtx, name, notRun)
		return
	}

	// Create step result for skipped step
	stepResult := st.createSkippedStepResult(name, jCtx)

	// Add step result to workflow buffer
	if jCtx.Result != nil {
		jCtx.Result.AddStepResult(jCtx.CurrentJobID, stepResult)
	}
}

// handleSkipRepeatExecution handles skipped step in repeat mode. A step
// skipped by its own skipif did what it was written to do, and the run
// counts as a success of it. One skipped after a failure, or for an
// iteration without an entry, was given nothing to do, and the run counts
// as neither a success nor a failure of it.
func (st *stepRun) handleSkipRepeatExecution(jCtx *JobContext, name string, notRun bool) {
	if jCtx.warmingUp() {
		return
	}
	// Update counter atomically with lock held throughout
	jCtx.repeat.mu.Lock()
	counter, exists := jCtx.repeat.counters[st.counterKey()]
	if !exists {
		counter = StepRepeatCounter{
			ID:          st.repeatID(),
			Name:        name,
			Test:        st.Test,
			RepeatTotal: jCtx.repeat.total,
		}
	}

	if notRun {
		counter.SkippedCount++
	} else {
		// Count as successful (skipped is not a failure)
		counter.SuccessCount++
		counter.LastResult = true
	}

	// Store updated counter back to map. The step's result is made from the
	// counter once every run is done (Executor.appendRepeatStepResults).
	jCtx.repeat.counters[st.counterKey()] = counter
	jCtx.repeat.mu.Unlock()
}

// createSkippedStepResult creates a StepResult for a skipped step
func (st *stepRun) createSkippedStepResult(name string, jCtx *JobContext) StepResult {
	return StepResult{
		Index:    st.idx,
		ID:       st.repeatID(),
		Name:     name + " (SKIPPED)",
		Status:   StatusSkipped,
		RT:       "",
		WaitTime: st.getWaitTimeForDisplay(),
		HasTest:  false,
	}
}

// createFailedStepResult creates a StepResult for a failed step
func (st *stepRun) createFailedStepResult(name string, jCtx *JobContext) StepResult {
	result := StepResult{
		Index:    st.idx,
		ID:       st.repeatID(),
		Name:     name,
		Status:   StatusError,
		RT:       "",
		WaitTime: st.getWaitTimeForDisplay(),
		HasTest:  st.Test != "",
		Test:     st.Test,
		Elapsed:  st.elapsed(),
	}

	if st.Retry != nil && st.retryAttempt > 0 {
		result.RetryAttempt = st.retryAttempt
		result.RetryMax = st.Retry.MaxAttempts
	}

	if jCtx.Timing {
		if !st.startedAt.IsZero() {
			result.StartedAt = st.startedAt
		}
		if st.ctx.RT.Duration != "" {
			result.RT = st.ctx.RT.Duration
			result.RTSec = st.ctx.RT.Sec
		}
	}
	if v, ok := st.ctx.Res["report"]; ok {
		if report, sok := v.(string); sok {
			result.Report = report
		}
	}

	result.Contract = st.contractMatched()

	// Include error information if available
	if st.err != nil {
		result.TestOutput = indentDetail(st.err.Error())
		result.Failure = st.newFailure(failureKindOf(st.err), st.err.Error())
	}

	return result
}

// errCredentialTemplate replaces why the template of a credential could not
// be evaluated: the template, the excerpt of it in the error, and a value in
// the error, such as the input of a failed conversion, may each hold the
// credential.
var errCredentialTemplate = errors.New("the template could not be evaluated; the details are not shown, as the value is a credential")

// redactCredentialErrors replaces the error of each *expr.FieldError joined
// into err whose path goes through a credential, such as
// with.headers.authorization or with.password, keeping the path. So is that
// of a URL written in with whose user info holds a template, as the password
// of redis://app:{{vars.pw}}@host does.
func redactCredentialErrors(err error, with map[string]any) error {
	var errs []error
	for _, e := range unwrapJoined(err) {
		var fe *expr.FieldError
		// A key named by a template is a credential when it comes to name
		// one, as a header named by a var may. Only a template that could
		// not be evaluated is hidden: another error, such as two keys that
		// come to one, quotes no credential.
		var te *expr.TemplateError
		if errors.As(e, &fe) && errors.As(fe.Err, &te) && (pathHoldsCredential(fe.Path) || pathHoldsCredential(fe.EvaluatedPath) || urlCredentialTemplated(with, fe.Path)) {
			e = &expr.FieldError{Path: fe.Path, EvaluatedPath: fe.EvaluatedPath, Err: errCredentialTemplate}
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// templatePlaceholder stands for a template in a value read without them.
const templatePlaceholder = "\x00"

// urlCredentialTemplated reports whether the value at path, as with.url, is
// a URL written in with whose user info holds a template: the template, or
// why it could not be evaluated, may then quote the password. A URL whose
// templates are elsewhere, as most are, keeps its error. A value that cannot
// be found by its path is taken to hold one.
func urlCredentialTemplated(with map[string]any, path string) bool {
	keys := strings.Split(strings.TrimPrefix(path, "with."), ".")
	last := keys[len(keys)-1]
	if i := strings.IndexByte(last, '['); i >= 0 {
		last = last[:i]
	}
	if !mask.HoldsURL(last) {
		return false
	}
	var v any = with
	for _, key := range keys {
		name, index, _ := strings.Cut(key, "[")
		m, ok := v.(map[string]any)
		if !ok {
			return true
		}
		if v, ok = m[name]; !ok {
			return true
		}
		for index != "" {
			var n string
			n, index, _ = strings.Cut(index, "[")
			items, ok := v.([]any)
			i, err := strconv.Atoi(strings.TrimSuffix(n, "]"))
			if !ok || err != nil || i < 0 || i >= len(items) {
				return true
			}
			v = items[i]
		}
	}
	written, ok := v.(string)
	if !ok {
		return false
	}
	return strings.Contains(mask.Userinfo(expr.ReplaceTemplates(written, templatePlaceholder)), templatePlaceholder)
}

// pathHoldsCredential reports whether any key in path, such as
// headers.cookie[0], names a credential.
func pathHoldsCredential(path string) bool {
	for key := range strings.SplitSeq(path, ".") {
		if i := strings.IndexByte(key, '['); i >= 0 {
			key = key[:i]
		}
		if mask.HoldsCredential(key) {
			return true
		}
	}
	return false
}

// failureKindOf tells a template that could not be evaluated, and what the
// guard of the run refused, from an error of the action itself.
func failureKindOf(err error) string {
	if actionrpc.IsRefused(err) {
		return FailureRefused
	}
	var tErr *expr.TemplateError
	var fErr *expr.FieldError
	if errors.As(err, &tErr) || errors.As(err, &fErr) {
		return FailureTemplate
	}
	return FailureAction
}

// unwrapJoined returns the errors joined into err, or err alone.
func unwrapJoined(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}

// saveOutputs evaluates and saves step outputs to JobContext
func (st *stepRun) saveOutputs(jCtx *JobContext) {
	if len(st.Outputs) == 0 {
		return
	}

	// Evaluate each output expression
	outputs := make(map[string]any)
	for outputName, outputExpr := range st.Outputs {
		result, err := st.expr.Eval(outputExpr, st.ctx)
		if err != nil {
			jCtx.Printer.PrintError("output '%s' evaluation error: %v", outputName, err)
			continue // Skip this output but continue with others
		}
		outputs[outputName] = result
	}

	// Save outputs to the unified Outputs structure
	if jCtx.Outputs != nil {
		// A name taken before is a warning; the step's value is still kept.
		// A step id taken by an output name is an error: the step's outputs
		// cannot be read through it.
		if err := st.publishOutputs(jCtx, outputs); err != nil {
			for _, e := range unwrapJoined(err) {
				var taken *NameTakenError
				if errors.As(e, &taken) {
					jCtx.Printer.LogWarn("%v", taken)
				} else {
					jCtx.Printer.PrintError("Output conflict: %v", e)
				}
			}
		}
	}

	if jCtx.Verbose {
		jCtx.Printer.LogDebug("Step '%s' outputs saved: %v", st.repeatID(), outputs)
	}
}

// publishOutputs publishes outputs by their names, and under the step's id
// when it has one. A step without one is named in what is reported by its
// job and where it is written, which is the same each time it publishes.
func (st *stepRun) publishOutputs(jCtx *JobContext, outputs map[string]any) error {
	if st.ID != "" {
		return jCtx.Outputs.Set(st.ID, outputs)
	}
	return jCtx.Outputs.SetFlat(jCtx.CurrentJobID+"."+st.repeatID(), outputs)
}
