package probe

import (
	"fmt"
	"strings"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
)

type Job struct {
	Name  string   `yaml:"name" validate:"required"`
	ID    string   `yaml:"id,omitempty"`
	Needs []string `yaml:"needs,omitempty"`
	// Always runs the job once the jobs it needs are over, whether or not
	// they succeeded, as a job that removes what they made has to.
	Always bool    `yaml:"always,omitempty"`
	Steps  []*Step `yaml:"steps" validate:"required"`
	// FailFast skips the steps after one that failed, as steps that only
	// work on what an earlier one made would fail for its reason.
	FailFast bool    `yaml:"fail_fast,omitempty"`
	Repeat   *Repeat `yaml:"repeat"`
	Defaults any     `yaml:"defaults"`
	SkipIf   string  `yaml:"skipif,omitempty"`
}

// run runs the job. It reports whether a step failed separately from the
// error that kept the job from running at all, such as an invalid step.
func (j *Job) run(ctx JobContext) (failed bool, err error) {
	ctx.CurrentJobID = j.ID
	// Each run of the job starts with no state, as a repeated run is a run
	// of its own and an embedded job a job of its own.
	ctx.states = newActionStates()

	// Use local pointer instead of j.ctx to avoid race conditions in async mode
	ctxPtr := &ctx
	ev := &expr.Expr{}

	if err := j.expandJobName(ev, ctxPtr); err != nil {
		ctx.Result.recordFailure(failureConfig)
		ctx.Printer.PrintError("job %q: name: %v", j.Name, err)
		return false, NewExecutionError("job_start", "failed to expand job name", err)
	}

	// Check if job should be skipped
	if j.shouldSkip(ev, *ctxPtr) {
		j.handleSkip(*ctxPtr)
		return false, nil
	}

	j.executeSteps(ev, ctxPtr)
	return ctxPtr.Failed, nil
}

// ApplyDefaults merges the job's defaults into the with of every step that
// uses the action they are keyed by. A value the step sets itself wins, and
// nested objects are merged key by key.
func (j *Job) ApplyDefaults() {
	j.applyDefaults(j.Defaults)
}

// applyDefaults merges defaults, the with of each action by its uses, into
// the with of every step of the job that uses the action, as ApplyDefaults
// does the job's own. A workflow's defaults are applied with it after the
// job's, so that they set only what neither the step nor the job did.
func (j *Job) applyDefaults(defaults any) {
	byAction, ok := defaults.(map[string]any)
	if !ok {
		return
	}
	for uses, values := range byAction {
		defaults, ok := values.(map[string]any)
		if !ok {
			continue
		}
		for _, s := range j.Steps {
			if s == nil || s.Uses != uses {
				continue
			}
			if s.With == nil {
				s.With = make(map[string]any)
			}
			mergeDefaults(s.With, defaults)
		}
	}
}

// mergeDefaults sets each key of defaults that data lacks, recursing into
// objects that both have.
func mergeDefaults(data, defaults map[string]any) {
	for key, defaultValue := range defaults {
		if _, exists := data[key]; !exists {
			// A step keeps a copy of its own: what is merged into it
			// later, from the defaults further out, must not reach the
			// other steps the value was given to.
			data[key] = cloneValue(defaultValue)
			continue
		}
		if nestedDefault, ok := defaultValue.(map[string]any); ok {
			if nestedData, ok := data[key].(map[string]any); ok {
				mergeDefaults(nestedData, nestedDefault)
			}
		}
	}
}

// cloneValue returns v with its maps and lists copied, at any depth.
func cloneValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = cloneValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	}
	return v
}

// expandJobName evaluates the job name for the run ctx is of, which keeps
// it, and gives it to the job's result. The job keeps the name as written,
// so that each run evaluates it anew.
func (j *Job) expandJobName(ev *expr.Expr, ctx *JobContext) error {
	if j.Name == "" {
		ctx.jobName = "Unknown Job"
		return nil
	}

	name, err := ev.EvalTemplate(j.Name, *ctx)
	if err != nil {
		return err
	}

	ctx.jobName = name

	// Update the job name in the result as well. Async repeat shares the
	// JobResult across iterations, so writers must take the mutex.
	if ctx.Result != nil {
		if jobResult, exists := ctx.Result.Jobs[j.ID]; exists {
			jobResult.mutex.Lock()
			jobResult.JobName = name
			jobResult.mutex.Unlock()
		}
	}

	return nil
}

// validateRepeatIDs checks that no two steps of a job with a test over its
// runs are read by the same id under steps, one by its id and another, which
// has none, by where it is written; the test would read their runs as one.
// A loaded workflow gives a step without an id its default one, and refuses
// the same id twice, but a workflow made in code is given none.
func (j *Job) validateRepeatIDs() error {
	if j.Repeat == nil || (j.Repeat.Test == "" && j.Repeat.BaselineTest == "") {
		return nil
	}
	seen := make(map[string]int, len(j.Steps))
	for i, st := range j.Steps {
		if st == nil {
			continue
		}
		id := st.ID
		if id == "" {
			id = defaultStepID(i)
		}
		if k, ok := seen[id]; ok {
			return fmt.Errorf("steps %d and %d are both read as steps.%s by the tests over the runs; give one of them another id", k, i, id)
		}
		seen[id] = i
	}
	return nil
}

// executeSteps runs all steps in the job, handling iterations appropriately
func (j *Job) executeSteps(ev *expr.Expr, ctx *JobContext) {
	idx := 0
	for pos, st := range j.Steps {
		place := func(ordinal int) *stepRun {
			r := newStepRun(st, ev)
			r.pos, r.ordinal, r.placed = pos, ordinal, true
			return r
		}

		switch {
		case st.Iteration.Expr != "":
			j.executeStepWithIterationExpr(place, &idx, ctx)
		case len(st.Iteration.Entries) == 0:
			j.executeStep(place(0), &idx, ctx, nil)
		default:
			j.executeStepWithIterations(place, st.Iteration.Entries, &idx, ctx)
		}
	}
}

// executeStep executes a single step without iterations. In a job with
// fail_fast, a step after one that failed is reported as skipped instead.
func (j *Job) executeStep(st *stepRun, idx *int, ctx *JobContext, vars map[string]any) {
	st.idx = *idx
	*idx++
	st.SetCtx(*ctx, vars)
	if j.FailFast && ctx.Failed {
		st.skipAfterFailure(ctx)
		return
	}
	st.Do(ctx)
}

// executeStepWithIterations executes a step multiple times with different
// variable sets, each in a run of its own that place begins for the entry.
func (j *Job) executeStepWithIterations(place func(ordinal int) *stepRun, entries []map[string]any, idx *int, ctx *JobContext) {
	for ordinal, vars := range entries {
		j.executeStep(place(ordinal), idx, ctx, vars)
	}
}

// executeStepWithIterationExpr executes a step once for each entry its
// iteration comes to now, with the outputs published so far. A template
// that cannot be evaluated, or that does not come to a list of maps, fails
// the step, and a list without entries skips it: there is nothing to run
// it with. Neither is found out in a job with fail_fast after a step that
// failed, where the step is skipped as any other is.
func (j *Job) executeStepWithIterationExpr(place func(ordinal int) *stepRun, idx *int, ctx *JobContext) {
	st := place(0)
	if j.FailFast && ctx.Failed {
		j.executeStep(st, idx, ctx, nil)
		return
	}

	st.SetCtx(*ctx, nil)
	entries, err := st.iterationEntries()
	if err == nil && len(entries) > 0 {
		j.executeStepWithIterations(place, entries, idx, ctx)
		return
	}

	st.idx = *idx
	*idx++
	if err != nil {
		st.failBeforeRun(&expr.FieldError{Path: "iteration", Err: err}, ctx)
		return
	}
	st.skipWithoutRun("The iteration has no entry", ctx)
}

// validateSteps validates step configurations in the job
func (j *Job) validateSteps() error {
	stepIDs := make(map[string]int) // stepID -> stepIndex for duplicate check

	for i, step := range j.Steps {
		// Validate step ID format if provided
		if step != nil && step.ID != "" {
			if !isValidStepID(step.ID) {
				return fmt.Errorf("step %d: invalid step ID '%s' - only [a-z0-9_-] characters are allowed", i, step.ID)
			}

			// Check for duplicate step IDs within the job
			if existingIndex, exists := stepIDs[step.ID]; exists {
				return fmt.Errorf("step %d: duplicate step ID '%s' (already used in step %d)", i, step.ID, existingIndex)
			}
			stepIDs[step.ID] = i
		}

	}

	return nil
}

// isValidStepID validates step ID format: only [a-z0-9_-] allowed
func isValidStepID(id string) bool {
	if id == "" {
		return false
	}

	// Check each character
	for _, char := range id {
		if (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') &&
			char != '_' && char != '-' {
			return false
		}
	}

	return true
}

// shouldSkip evaluates the skipif expression and returns true if job should be skipped
func (j *Job) shouldSkip(ev *expr.Expr, ctx JobContext) bool {
	if j.SkipIf == "" {
		return false
	}

	// Create a step context for evaluation - same as SetCtx in step.go
	var outputs map[string]any
	if ctx.Outputs != nil {
		outputs = ctx.Outputs.GetAll()
	}

	// Create context for job skipif evaluation
	evalCtx := StepContext{
		Vars:    ctx.Vars,
		Outputs: outputs,
	}

	result, err := ev.Eval(j.SkipIf, evalCtx)
	if err != nil {
		ctx.Printer.PrintError("job skipif evaluation error: %v", err)
		return false // Don't skip on evaluation error
	}

	boolResult, ok := result.(bool)
	if !ok {
		ctx.Printer.PrintError("job skipif expression must return boolean, got: %T", result)
		return false // Don't skip on type error
	}

	return boolResult
}

// handleSkip handles the skipped job logic
func (j *Job) handleSkip(ctx JobContext) {
	if ctx.Verbose {
		var b debugBlock
		b.line("Job '%s' (SKIPPED)", ctx.jobName)
		b.line("Skip condition: %s", j.SkipIf)
		ctx.Printer.logDebugBlock(&b)
		ctx.Printer.PrintSeparator()
	}

	// Mark job as skipped in the result. Async repeat shares the
	// JobResult across iterations, so writers must take the mutex
	// to stay race-free with sibling iterations and Executor.finalize.
	if ctx.Result != nil {
		if jobResult, exists := ctx.Result.Jobs[j.ID]; exists {
			jobResult.mutex.Lock()
			jobResult.Status = StatusSkipped
			jobResult.mutex.Unlock()
		}
	}
}

// JobRun is the outcome of a job run on its own with RunStandalone.
type JobRun struct {
	// Success is true when every step passed or the job was skipped.
	Success bool
	// Outputs are the outputs the job's steps published.
	Outputs map[string]any
	// Report is the report of the job's steps.
	Report string
	// Err is set when the job could not run as it was written, such as when
	// a step is invalid or an action cannot be resolved. A step that fails
	// leaves it nil and Success false.
	Err error
	// Refused is set when the guard the job ran under refused a step, the
	// first one, so that the step that embeds the job is refused as well
	// rather than failing as a job whose test was false.
	Refused *actionrpc.Refused
	// Duration is how long the job took.
	Duration time.Duration
}

// StandaloneOption changes how RunStandalone runs a job.
type StandaloneOption func(*standaloneConfig)

type standaloneConfig struct {
	runID   string
	guard   actionrpc.Guard
	actions map[string]string
	builtin []string
	// cleanups are those of the run the job is part of.
	cleanups *actionrpc.Cleanups
	// needsNoCheck are the actions whose steps pass without a test.
	needsNoCheck []string
	// runner runs the actions of the job's steps; nil for the default.
	runner ActionRunner
}

// WithNeedsNoCheck tells the job the names of the actions whose steps pass
// without a test, as Config.NeedsNoCheck tells a workflow. Without it, a
// step that nothing checks is counted as untested whatever its action.
func WithNeedsNoCheck(names []string) StandaloneOption {
	return func(c *standaloneConfig) {
		c.needsNoCheck = names
	}
}

// WithActionRunner runs the actions of the job's steps with r, as
// Config.ActionRunner does those of a workflow.
func WithActionRunner(r ActionRunner) StandaloneOption {
	return func(c *standaloneConfig) {
		c.runner = r
	}
}

// WithCleanups runs the job as part of a run that cleans up once it is over,
// such as the run of the step that embeds it: the actions the job would have
// had clean up when it ended are named in c instead, and what its steps left
// behind stays for the rest of that run. Without it, or with a nil c, the
// job cleans up after itself.
func WithCleanups(c *actionrpc.Cleanups) StandaloneOption {
	return func(cfg *standaloneConfig) {
		cfg.cleanups = c
	}
}

// WithRunID runs the job as part of the run id names, such as the run of the
// step that embeds it, so that its actions are told the same run. Without it
// the job is a run of its own.
func WithRunID(id string) StandaloneOption {
	return func(c *standaloneConfig) {
		c.runID = id
	}
}

// WithGuard runs the job under guard, such as the guard of the run of the
// step that embeds it, so that a workflow cannot get round its guard by
// embedding a job.
func WithGuard(guard actionrpc.Guard) StandaloneOption {
	return func(c *standaloneConfig) {
		c.guard = guard
	}
}

// WithActions tells the steps of the job the names a workflow gives its
// external actions, such as those of the workflow that embeds it, for a job
// the job itself embeds to be read by. The job's own steps are read by them
// with UseActions.
func WithActions(names map[string]string) StandaloneOption {
	return func(c *standaloneConfig) {
		c.actions = names
	}
}

// WithBuiltinActions tells the job the names of the actions of Probe, so
// that a uses that names no action stops the job before any step runs.
// Without it, such a step fails when it is run.
func WithBuiltinActions(names []string) StandaloneOption {
	return func(c *standaloneConfig) {
		c.builtin = names
	}
}

// firstRefusal returns why the guard refused the first step of jr it
// refused, or nil when it refused none.
func firstRefusal(jr *JobResult) *actionrpc.Refused {
	for _, sr := range jr.StepResults {
		f := sr.Failure
		if sr.RepeatCounter != nil {
			f = sr.RepeatCounter.Failure
		}
		if f != nil && f.Kind == FailureRefused {
			reason := strings.TrimPrefix(f.Message, "refused: ")
			return &actionrpc.Refused{Reason: fmt.Sprintf("step %d %q of the embedded job: %s", sr.Index, sr.Name, reason)}
		}
	}
	return nil
}

// RunStandalone runs the job outside a workflow, as an embedded job is run.
// A local action is resolved relative to baseDir, the directory of the file
// the job comes from, or to the working directory when baseDir is empty.
func (j *Job) RunStandalone(vars map[string]any, printer *Printer, jobID, baseDir string, opts ...StandaloneOption) JobRun {
	cfg := standaloneConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	start := time.Now()
	j.ID = jobID
	runID := cfg.runID
	if runID == "" {
		runID = newRunID()
	}
	// The job runs outside Workflow.Start, which is what installs a masker.
	// One is installed here, so that credentials the job's own steps pass to
	// actions are learned and hidden as they are in a workflow.
	if printer.Masker() == nil {
		printer.SetMasker(mask.New(nil, nil))
	}
	result := NewResult()
	jr := &JobResult{
		JobName:   j.Name,
		JobID:     jobID,
		StartTime: start,
	}
	result.Jobs[jobID] = jr

	ctx := JobContext{
		runEnv: &runEnv{
			Config: Config{
				Verbose:      printer.verbose,
				Guard:        cfg.guard,
				NeedsNoCheck: cfg.needsNoCheck,
				ActionRunner: cfg.runner,
			},
			Printer:    printer,
			Result:     result,
			background: newBackground(),
			cleanups:   newCleanups(runID, printer, false),
			baseDir:    baseDir,
			runID:      runID,
			actions:    cfg.actions,
		},
		Vars:    vars,
		Outputs: NewOutputs(),
	}
	// This job runs inside the plugin process of the step that embeds it,
	// which exits once the job is done, so its background processes go then.
	// The watch for signals ends after the stop, as in a workflow. The
	// actions that clean up are called then as well, unless the job is over
	// by then and has passed them on to the run it is part of.
	ctx.background.AfterStop(ctx.cleanups.run)
	endWatch := ctx.background.StopOnSignal()
	defer endWatch()
	defer ctx.background.Stop()

	// External actions are resolved before any step runs, as a workflow
	// does, so that a bad reference stops the job before it changes anything.
	// What keeps the job from running is found before any step runs, as a
	// workflow finds it when it is loaded. The job shows only as failed in
	// the report, so the reason is printed here.
	var failed bool
	var err error
	if found := j.problems("", fmt.Sprintf("job %q", j.Name)); len(found) > 0 {
		err = NewExecutionError("job_start", "step validation failed", problemsError(found))
		ctx.Result.recordFailure(failureConfig)
		ctx.Printer.PrintError("%v", err)
	}
	if err == nil {
		ctx.Guard, err = resolveExternalActions([]*Job{j}, baseDir, ctx.Guard, cfg.builtin)
	}
	if err == nil {
		failed, err = j.run(ctx)
	}
	// What the steps left behind is for the rest of the run the job is
	// part of, as for the jobs after it in a workflow.
	ctx.cleanups.passOn(cfg.cleanups)

	run := JobRun{Err: err, Success: err == nil && !failed}
	if !run.Success {
		jr.Status = StatusError
	}

	run.Refused = firstRefusal(jr)

	run.Duration = time.Since(start)
	jr.EndTime = jr.StartTime.Add(run.Duration)
	run.Outputs = ctx.Outputs.GetAll()
	run.Report = newReportRenderer([]string{jobID}, nil, ctx.Printer.Masker()).GenerateReportOnlySteps(result)

	return run
}
