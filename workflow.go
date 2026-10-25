package probe

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mozership/probe/actionrpc"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/dag"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/suggest"
	"github.com/mozership/probe/internal/vars"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/procgroup"
	"github.com/mozership/probe/report"
)

type Workflow struct {
	Name        string         `yaml:"name" validate:"required"`
	Description string         `yaml:"description,omitempty"`
	Jobs        []Job          `yaml:"jobs" validate:"required"`
	Vars        map[string]any `yaml:"vars"`
	// Defaults are the with of each action, by its uses, for the steps of
	// every job. A job's own defaults come before them, and a step's with
	// before both.
	Defaults any `yaml:"defaults"`
	// Actions gives names to external actions, for the steps and the
	// defaults of the jobs to name them by. Load writes the action in place
	// of each name, so nothing that runs the workflow sees one.
	Actions map[string]string `yaml:"actions,omitempty"`
	// Secrets names environment variables whose values must not appear in
	// anything Probe prints or writes.
	Secrets    []string `yaml:"secrets,omitempty"`
	exitStatus int
	// failed is set to 1 by any job that does not succeed. Jobs run
	// concurrently, so it is only accessed atomically; exitStatus is derived
	// from it once every job is done. It is an int32 rather than an
	// atomic.Bool because a Workflow is copied by value when it is decoded.
	failed int32
	env    map[string]string
	// basePath is the directory containing the workflow file (used for resolving relative paths)
	basePath string
	// named are the names the workflow gives its external actions, as a job
	// it embeds is told them
	named map[string]string
	// Shared outputs across all jobs
	outputs *Outputs
	printer *Printer
	// runID names this run, for actions to tell where a request comes from
	runID string
	// selection is what the run was narrowed to with Config.Jobs; nil
	// when every job runs.
	selection *jobSelection
	// given are the vars the person running the workflow gave, by name,
	// each in place of what the workflow writes for it
	given map[string]string
}

// RunID returns the ID of the run Start began, which every action of the run
// is told about, or an empty string before Start.
func (w *Workflow) RunID() string {
	return w.runID
}

// newRunID returns a random ID for a run.
func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start executes the workflow with the given configuration
func (w *Workflow) Start(c Config) error {
	if err := w.validateRepeatLimits(); err != nil {
		return err
	}

	// The result, the report order and the scheduler all index jobs by ID,
	// so every job has one before anything keys off them. A loaded workflow
	// has them already; one made in code is given them here.
	if err := w.assignJobIDs(); err != nil {
		return err
	}
	// A loaded workflow was refused for these as it was loaded; one made in
	// code is refused here, before any job starts.
	if found := w.problems(); len(found) > 0 {
		return problemsError(found)
	}
	// A run narrowed to some jobs is the run of a workflow of those jobs
	// alone: the others are not run, reported or counted, and the actions
	// and files only they use are not fetched or read.
	// What a run of every job is refused for, a run of some is refused
	// for too, before the others are left out.
	if len(c.Jobs) > 0 {
		if err := w.checkAllJobs(c.Actions); err != nil {
			return err
		}
		jobs, sel, err := selectJobs(w.Jobs, c.Jobs)
		if err != nil {
			return err
		}
		w.Jobs, w.selection = jobs, sel
	}
	scheduler, err := w.initJobScheduler()
	if err != nil {
		return err
	}
	w.runID = newRunID()

	// Fetch external actions before any job starts, so that a bad reference
	// fails the run up front and a download does not count against a step's
	// timeout. A uses that names no action fails the run here as well. The
	// guard the jobs run under learns from each action.yml the kinds of guard
	// the action keeps to.
	if c.Guard, err = w.resolveExternalActions(c.Guard, c.Actions); err != nil {
		return err
	}

	// Collect all job IDs, for the order of the report
	jobIDs := make([]string, len(w.Jobs))
	for i, job := range w.Jobs {
		jobIDs[i] = job.ID
	}

	if w.printer == nil {
		w.printer = NewPrinter(c.Verbose)
	}

	// The variables read from files are those of the environment the
	// vars read, and a secret may be among them, so they are in place
	// before the masker is made. What the environment has comes first.
	if len(c.Env) > 0 {
		env := maps.Clone(c.Env)
		maps.Copy(env, w.Env())
		w.env = env
	}

	// Install the masker before anything is printed, the header included.
	w.printer.SetMasker(mask.New(w.Secrets, w.Env()))
	// A secret too short to mask inside other text is shown there as it is.
	// Say so once, by name, so that the user can lengthen it rather than
	// trust it to be hidden.
	for _, name := range w.printer.Masker().TooShort() {
		w.printer.LogWarn("the secret %s is shorter than %d characters, so it is masked only where it is a whole value; use a value of at least %d characters", name, mask.MinLength, mask.MinLength)
	}

	// A reporter tracks how far the report has been emitted, which is state
	// for this run alone, so every run gets a fresh one.
	render := newReportRenderer(jobIDs, c.Baseline, w.printer.Masker())
	reporter := newReporter(c.Output, w.printer, render)
	w.printer.SetReporter(reporter)

	startedAt := time.Now()
	reporter.Start(w.Name, w.Description)

	// Initialize shared outputs
	if w.outputs == nil {
		w.outputs = NewOutputs()
	}

	w.given = c.Vars
	vars, err := w.evalVars()
	if err != nil {
		// The caller prints this error itself, and a template's error can
		// quote the value of a declared secret.
		return &maskedError{err: err, masker: w.printer.Masker()}
	}

	// A job is named with the vars, as the report names it, where the run
	// says which job is which.
	namer := w.jobNamer(vars)
	if l, ok := reporter.(jobLabeler); ok {
		l.labelJobs(namer)
	}
	if w.selection != nil {
		// Said apart from the report, which is that of the jobs that run,
		// so that a run of some jobs is not taken for one of them all.
		w.printer.Fprintln(w.printer.errWriter, colorDim().Sprint(w.selection.describe(namer)))
	}

	// The job files the steps embed are read with the vars the run has, so
	// that the file checked is the one a step runs. The error names the
	// file, whose path a var may have made from a declared secret.
	if err := w.checkEmbeddedJobs(c.Actions, vars); err != nil {
		return &maskedError{err: err, masker: w.printer.Masker()}
	}

	ctx := w.newJobContext(c, vars, scheduler)
	// A process a step started in the background lives on after its step,
	// for later steps and jobs to use, but not after the workflow, and not
	// after probe is interrupted. The watch for signals is deferred first so
	// that it ends last, still on while the processes are being stopped.
	// The actions that clean up are called after that, so that what a step
	// left behind is removed once no process uses it, on a signal as well.
	ctx.background.AfterStop(ctx.cleanups.run)
	endWatch := ctx.background.StopOnSignal()
	defer endWatch()
	defer ctx.background.Stop()

	if err := w.startJobsWithDependencies(ctx); err != nil {
		return err
	}

	reporter.Finish(ctx.Result)
	w.exitStatus = ctx.Result.exitCode(atomic.LoadInt32(&w.failed) == 1)

	return w.writeReports(c.Reports, c.Baseline, ctx.Result, jobIDs, startedAt, time.Now())
}

// writeReports writes every requested report file, compared to baseline
// when it is not nil. A file that cannot be written does not stop the
// others; the errors are returned together.
func (w *Workflow) writeReports(targets []report.Target, baseline *report.Report, rs *Result, order []string, startedAt, finishedAt time.Time) error {
	if len(targets) == 0 {
		return nil
	}

	r := BuildReport(w.Name, w.Description, rs, order, startedAt, finishedAt)
	r.RunID = w.runID
	if w.selection != nil {
		r.Selection = w.selection.report()
	}
	r.Compare(baseline)
	r.Mask(w.printer.Masker())

	var errs []error
	for _, t := range targets {
		err := r.Write(t)
		if errors.Is(err, report.ErrNoStepSummary) || errors.Is(err, report.ErrStepSummaryFull) {
			// Outside GitHub Actions there is no summary to write to, and a
			// full one has no room; say so and carry on, so one command line
			// serves CI and a laptop and a full summary fails no run.
			w.printer.LogWarn("%v", err)
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// defaultJobID is the ID of the job at index i of a workflow, when it is
// given none.
func defaultJobID(i int) string {
	return fmt.Sprintf("job_%d", i)
}

// assignJobIDs gives each job without an ID the ID defaultJobID makes of
// where it is in the workflow. The ID a job is given is not one another job
// is given in the workflow, which a run could not tell from it.
func (w *Workflow) assignJobIDs() error {
	taken := make(map[string]int, len(w.Jobs))
	for i, job := range w.Jobs {
		if _, ok := taken[job.ID]; job.ID != "" && !ok {
			taken[job.ID] = i
		}
	}
	for i := range w.Jobs {
		job := &w.Jobs[i]
		if job.ID != "" {
			continue
		}
		id := defaultJobID(i)
		if k, ok := taken[id]; ok {
			return NewConfigurationError("duplicate_job_id",
				fmt.Sprintf("job %d has no ID, and '%s', the ID it would be given, is the ID of job %d; give job %d an ID",
					i, id, k, i), nil)
		}
		job.ID = id
		taken[id] = i
	}
	return nil
}

// checkAllJobs refuses the jobs for what a run of them all would refuse
// them for before any runs, and a run of some of them would not see in the
// jobs it leaves out: a need that is no job, needs in a cycle, and a uses
// that names no action of Probe or external one. The external actions are
// not fetched, which a run does for the jobs that run alone.
func (w *Workflow) checkAllJobs(builtin []string) error {
	if _, err := w.initJobScheduler(); err != nil {
		return err
	}
	for i := range w.Jobs {
		if err := checkBuiltinUses(&w.Jobs[i], builtin); err != nil {
			return err
		}
	}
	return nil
}

// checkCycle refuses jobs whose needs are in a cycle, as a run does.
func (w *Workflow) checkCycle() error {
	byID := make(map[string]*Job, len(w.Jobs))
	ids := make([]string, len(w.Jobs))
	for i := range w.Jobs {
		byID[w.Jobs[i].ID] = &w.Jobs[i]
		ids[i] = w.Jobs[i].ID
	}
	cycle := dag.DetectCycleFn(ids, func(id string) []string {
		if j, ok := byID[id]; ok {
			return j.Needs
		}
		return nil
	})
	if cycle != nil {
		return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

// jobNamer returns the name of the job of each id, with the templates in it
// evaluated with vars where they can be: a name that reads more than the
// vars, such as repeat_index, is as written.
func (w *Workflow) jobNamer(vars map[string]any) func(id string) string {
	ev := &expr.Expr{}
	env := map[string]any{"vars": vars}
	return func(id string) string {
		for _, j := range w.Jobs {
			if j.ID != id {
				continue
			}
			if name, err := ev.EvalTemplate(j.Name, env); err == nil {
				return name
			}
			return j.Name
		}
		return id
	}
}

// initJobScheduler creates and sets up the job scheduler with dependencies
func (w *Workflow) initJobScheduler() (*JobScheduler, error) {
	scheduler := NewJobScheduler()

	// Add all jobs to scheduler
	for i := range w.Jobs {
		if err := scheduler.AddJob(&w.Jobs[i]); err != nil {
			return nil, err
		}
	}

	// Validate dependencies
	if err := scheduler.ValidateDependencies(); err != nil {
		return nil, err
	}

	return scheduler, nil
}

// setupResult creates result for managing execution results
func (w *Workflow) setupResult() *Result {
	rs := NewResult()
	// Initialize job outputs
	for _, job := range w.Jobs {
		rs.Jobs[job.ID] = &JobResult{
			JobName:   job.Name,
			JobID:     job.ID,
			StartTime: time.Now(),
		}
	}

	return rs
}

// startJobsWithDependencies runs the jobs, each as soon as the jobs it needs
// are done, rather than once every job started with them is: a job does not
// wait for one it does not need. A job whose dependency failed is skipped as
// soon as that is known.
func (w *Workflow) startJobsWithDependencies(ctx JobContext) error {
	done := make(chan struct{})
	running := 0

	for {
		skippedJobs := ctx.JobScheduler.MarkJobsWithFailedDependencies()
		if ctx.Result != nil {
			w.updateSkippedJobsOutput(skippedJobs, ctx.Result)
		}

		running += w.processRunnableJobs(ctx.JobScheduler.GetRunnableJobs(), ctx, done)
		if running == 0 {
			break
		}

		// Whichever job ends, the jobs that need it may be able to run now.
		<-done
		running--
	}

	// With no job running, a job not over yet would never run: what it
	// waits for will not change any more.
	if pending := ctx.JobScheduler.PendingJobs(); len(pending) > 0 {
		return fmt.Errorf("jobs %s cannot run: the jobs they need will not be done", strings.Join(pending, ", "))
	}
	return nil
}

// updateSkippedJobsOutput updates the output for jobs that were skipped due to failed dependencies
func (w *Workflow) updateSkippedJobsOutput(skippedJobs []string, rs *Result) {
	for _, jobID := range skippedJobs {
		if jr, exists := rs.Jobs[jobID]; exists {
			jr.mutex.Lock()
			jr.EndTime = jr.StartTime // Set end time same as start time (0 duration)
			jr.Status = StatusSkipped
			jr.mutex.Unlock()

			// These jobs never reach Executor.finalize, so announce them here
			// or the streaming report would stall on them.
			rs.notifyJobDone(jobID)
		}
	}
}

// processRunnableJobs starts each of runnableJobs, and returns how many it
// started. Each job sends to done once it is over and its status says so.
func (w *Workflow) processRunnableJobs(runnableJobs []string, ctx JobContext, done chan<- struct{}) int {
	for _, jobID := range runnableJobs {
		job := ctx.JobScheduler.jobs[jobID]
		ctx.JobScheduler.StartJob(jobID)

		go func(j *Job) {
			defer func() { done <- struct{}{} }()

			executor := NewExecutor(w, j)
			success := executor.Execute(ctx)
			if !success {
				w.SetExitStatus(true)
			}
		}(job)
	}
	return len(runnableJobs)
}

func (w *Workflow) SetExitStatus(isErr bool) {
	if isErr {
		atomic.StoreInt32(&w.failed, 1)
	}
}

func (w *Workflow) Env() map[string]string {
	if len(w.env) == 0 {
		w.env = envMap()
	}
	return w.env
}

// evalVars evaluates the vars of the workflow, with the vars the person
// running it gave and the environment variables, as vars.Eval does. A var
// whose template cannot be evaluated is an error, which stops the workflow
// before any job starts.
func (w *Workflow) evalVars() (map[string]any, error) {
	return vars.Eval(w.Vars, w.given, w.Env())
}

// maskedError shows err with the secrets masker knows hidden, and unwraps to
// err, so that errors.As still finds what it holds.
type maskedError struct {
	err    error
	masker *mask.Masker
}

func (e *maskedError) Error() string {
	return e.masker.String(e.err.Error())
}

func (e *maskedError) Unwrap() error {
	return e.err
}

func (w *Workflow) newJobContext(c Config, vars map[string]any, scheduler *JobScheduler) JobContext {
	rs := w.setupResult()
	rs.SetReporter(w.printer.Reporter())

	return JobContext{
		runEnv: &runEnv{
			Config:       c,
			Printer:      w.printer,
			Result:       rs,
			JobScheduler: scheduler,
			background:   procgroup.NewTracker(),
			cleanups:     newCleanups(w.runID, w.printer, c.Trace),
			baseDir:      w.basePath,
			runID:        w.runID,
			actions:      w.named,
		},
		Vars:    vars,
		Outputs: w.outputs,
	}
}

// resolveExternalActions resolves every action the steps name outside Probe,
// and returns guard with the kinds of guard each of them keeps to. builtin
// are the names of the actions of Probe, which any other uses must be.
func (w *Workflow) resolveExternalActions(guard actionrpc.Guard, builtin []string) (actionrpc.Guard, error) {
	jobs := make([]*Job, len(w.Jobs))
	for i := range w.Jobs {
		jobs[i] = &w.Jobs[i]
	}
	return resolveExternalActions(jobs, w.basePath, guard, builtin)
}

// resolveExternalActions resolves every action the steps of jobs name
// outside Probe, with local ones relative to baseDir, and returns guard with
// the kinds of guard each of them keeps to, as its action.yml says. The
// executable of one that guard then does not let run is not fetched, as its
// step is refused without it.
//
// A uses that is not an external action must be one of builtin, the names of
// the actions of Probe: an action that does not exist is told here, by its
// name, rather than by the process that would have served it failing to
// start. When builtin is empty, that is not checked.
func resolveExternalActions(jobs []*Job, baseDir string, guard actionrpc.Guard, builtin []string) (actionrpc.Guard, error) {
	for _, job := range jobs {
		if err := checkBuiltinUses(job, builtin); err != nil {
			return guard, err
		}
		for _, st := range job.Steps {
			if !actionref.IsExternal(st.Uses) {
				continue
			}
			m, err := actionref.ReadManifest(st.Uses, baseDir)
			if err != nil {
				return guard, NewConfigurationError("resolve_action", "failed to resolve an external action", err).
					WithContext("uses", st.Uses)
			}
			// What this action.yml declares replaces any declaration the guard
			// came with, even when it declares nothing: a job of the embedded
			// action is given the guard of the step that embeds it, and its
			// ./foo may be another action than the one that declared.
			guard = guard.WithKeeps(st.Uses, m.Guard)
			if !guard.Runs(st.Uses) {
				continue
			}
			if _, err := actionref.Resolve(st.Uses, baseDir); err != nil {
				return guard, NewConfigurationError("resolve_action", "failed to resolve an external action", err).
					WithContext("uses", st.Uses)
			}
		}
	}
	return guard, nil
}

// checkBuiltinUses refuses a step of job whose uses is not an external
// action and not one of builtin, the names of the actions of Probe. When
// builtin is empty, nothing is refused.
func checkBuiltinUses(job *Job, builtin []string) error {
	if len(builtin) == 0 {
		return nil
	}
	for i, st := range job.Steps {
		if actionref.IsExternal(st.Uses) || slices.Contains(builtin, st.Uses) {
			continue
		}
		return NewConfigurationError("resolve_action", "failed to resolve an action", fmt.Errorf("job %q, step %d: %w", job.Name, i, unknownAction(st.Uses, builtin))).
			WithContext("uses", st.Uses).
			WithContext("job", job.Name).
			WithContext("step", i)
	}
	return nil
}

// unknownAction is the error of a uses that names no action: it is not one
// of builtin, the actions of Probe, not an external action, and not a name
// the workflow gives one under actions, which would have been replaced.
func unknownAction(uses string, builtin []string) error {
	msg := fmt.Sprintf("unknown action %q: it is not an action of Probe, an external action, or a name given under actions", uses)
	if s := suggest.Closest(uses, builtin); s != "" {
		msg += fmt.Sprintf("; did you mean %q?", s)
	}
	return errors.New(msg)
}

// checkEmbeddedJobs reads the job file of each step that embeds one, and
// those that job embeds in turn, and returns an error for a uses in them
// that names no action, so that it fails the run before any job starts
// rather than when the step is reached. A job is read by the names the
// workflow gives its external actions, as the embedded action reads it.
// builtin are the names of the actions of Probe; when it is empty, nothing
// is checked. vars are the vars of the run, which a path may read.
//
// What cannot be known yet is left to the embedded action, which checks the
// same as it starts a job: a path that reads more than the workflow's vars,
// and a file that cannot be read. An external action a job names is
// resolved then as well.
func (w *Workflow) checkEmbeddedJobs(builtin []string, vars map[string]any) error {
	if len(builtin) == 0 {
		return nil
	}
	ev := &expr.Expr{}
	env := map[string]any{"vars": vars}
	var found error
	for _, job := range w.Jobs {
		for _, st := range job.Steps {
			path, ok := embeddedJobFile(st)
			if !ok {
				continue
			}
			// A path the vars of the workflow give is known by now.
			if len(expr.TemplateExprs(path)) > 0 {
				expanded, err := ev.EvalTemplate(path, env)
				if err != nil {
					continue
				}
				st = &Step{Uses: embeddedAction, With: map[string]any{"path": expanded}}
			}
			eachEmbeddedStep(st, map[string]bool{}, func(file string, i int, es *Step) {
				uses := es.Uses
				if ref, named := w.named[uses]; named {
					uses = ref
				}
				if found != nil || actionref.IsExternal(uses) || slices.Contains(builtin, uses) {
					return
				}
				found = NewConfigurationError("resolve_action", "failed to resolve an action of an embedded job", fmt.Errorf("%s, step %d: %w", file, i, unknownAction(uses, builtin))).
					WithContext("uses", uses).
					WithContext("path", file).
					WithContext("step", i)
			})
			if found != nil {
				return found
			}
		}
	}
	return nil
}

// validateRepeatLimits validates repeat count and max_attempts against configured limits
//
// Start checks them as well, for a workflow made in code rather than loaded,
// since an async repeat without an interval cannot run.
func (w *Workflow) validateRepeatLimits() error {
	for jobIdx, job := range w.Jobs {
		// Validate repeat configuration
		if job.Repeat != nil {
			if err := job.Repeat.Validate(); err != nil {
				return NewConfigurationError("invalid_repeat",
					fmt.Sprintf("job %d (%s): %v", jobIdx, job.Name, err), nil)
			}
			if err := job.validateRepeatIDs(); err != nil {
				return NewConfigurationError("invalid_repeat",
					fmt.Sprintf("job %d (%s): %v", jobIdx, job.Name, err), nil)
			}
		}

		// Validate retry configuration for each step
		for stepIdx, step := range job.Steps {
			if step.Retry != nil {
				if err := step.Retry.Validate(); err != nil {
					return NewConfigurationError("invalid_retry",
						fmt.Sprintf("job %d (%s), step %d: %v", jobIdx, job.Name, stepIdx, err), nil)
				}
			}
		}
	}
	return nil
}
