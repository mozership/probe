package probe

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mozership/probe/internal/schedule"
	"github.com/mozership/probe/internal/vareval"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

// workflowRun is one run of a workflow: what Start decides for the run
// before any job starts, and what the run finds out as its jobs run. It is
// the run's own, so a workflow started again starts with no job failed, no
// outputs and every job of it, whatever the run before it was narrowed to
// or given.
type workflowRun struct {
	w *Workflow
	// config is what the run was started with, its Guard with the kinds of
	// guard the external actions keep to.
	config Config
	// jobs are the jobs that run: every job of the workflow, or those
	// selection narrowed them to. The workflow's own are left as they are.
	jobs []Job
	// selection is what the run was narrowed to with Config.Jobs; nil when
	// every job runs.
	selection *jobSelection
	scheduler *schedule.Scheduler
	// id names the run, for actions to tell where a request comes from.
	id string
	// env is the environment the vars read: the workflow's, with
	// Config.Env under it.
	env map[string]string
	// outputs are what the jobs publish, shared across them.
	outputs *Outputs
	// failed is set by any job that does not succeed. Jobs run
	// concurrently, so it is atomic; exitStatus is derived from it once
	// every job is done.
	failed atomic.Bool

	// What execute leaves for the report files.
	result     *Result
	startedAt  time.Time
	finishedAt time.Time
	exitStatus int
}

// prepare refuses the workflow for what keeps it from running with c before
// any job starts, and returns the run it would be: narrowed to the jobs
// c.Jobs selects, with the external actions its steps name resolved and the
// environment its vars read. Nothing is printed and no job starts.
func (w *Workflow) prepare(c Config) (*workflowRun, error) {
	// A loaded workflow was refused for these as it was loaded; one made in
	// code is refused here, before any job starts.
	found, err := w.validate()
	if err != nil {
		return nil, err
	}
	if len(found) > 0 {
		return nil, problemsError(found)
	}

	r := &workflowRun{w: w, config: c, jobs: w.Jobs, id: newRunID(), outputs: NewOutputs()}
	// A run narrowed to some jobs is the run of a workflow of those jobs
	// alone: the others are not run, reported or counted, and the actions
	// and files only they use are not fetched or read.
	// What a run of every job is refused for, a run of some is refused
	// for too, before the others are left out.
	if len(c.Jobs) > 0 {
		if err := w.checkAllJobs(c.Actions); err != nil {
			return nil, err
		}
		if r.jobs, r.selection, err = selectJobs(w.Jobs, c.Jobs); err != nil {
			return nil, err
		}
	}
	if r.scheduler, err = validatedScheduler(r.jobs); err != nil {
		return nil, err
	}

	// Fetch external actions before any job starts, so that a bad reference
	// fails the run up front and a download does not count against a step's
	// timeout. A uses that names no action fails the run here as well. The
	// guard the jobs run under learns from each action.yml the kinds of guard
	// the action keeps to.
	if r.config.Guard, err = resolveExternalActions(jobPointers(r.jobs), w.basePath, c.Guard, c.Actions); err != nil {
		return nil, err
	}

	// The variables read from files are those of the environment the vars
	// read, and a secret may be among them. What the environment has comes
	// first.
	r.env = w.Env()
	if len(c.Env) > 0 {
		r.env = maps.Clone(c.Env)
		maps.Copy(r.env, w.Env())
	}
	return r, nil
}

// jobPointers returns a pointer to each of jobs, in their order.
func jobPointers(jobs []Job) []*Job {
	ptrs := make([]*Job, len(jobs))
	for i := range jobs {
		ptrs[i] = &jobs[i]
	}
	return ptrs
}

// jobIDs returns the IDs of the jobs of the run, in the order they are
// reported in.
func (r *workflowRun) jobIDs() []string {
	ids := make([]string, len(r.jobs))
	for i, job := range r.jobs {
		ids[i] = job.ID
	}
	return ids
}

// execute runs the jobs of the run, reporting them as they run, and leaves
// the result and the exit status of the run in r. An error is what stopped
// the run, before any job started or once no job could start any more.
func (r *workflowRun) execute() error {
	reporter := r.startReport()

	vars, err := r.evalVars()
	if err != nil {
		// The caller prints this error itself, and a template's error can
		// quote the value of a declared secret.
		return &maskedError{err: err, masker: r.printer().Masker()}
	}

	// A job is named with the vars, as the report names it, where the run
	// says which job is which.
	namer := jobNamer(r.jobs, vars)
	if l, ok := reporter.(jobLabeler); ok {
		l.labelJobs(namer)
	}
	if r.selection != nil {
		// Said apart from the report, which is that of the jobs that run,
		// so that a run of some jobs is not taken for one of them all.
		p := r.printer()
		p.Fprintln(p.errWriter, colorDim().Sprint(r.selection.describe(namer)))
	}

	// The job files the steps embed are read with the vars the run has, so
	// that the file checked is the one a step runs. The error names the
	// file, whose path a var may have made from a declared secret.
	if err := checkEmbeddedJobs(r.jobs, r.w.named, r.config.Actions, vars); err != nil {
		return &maskedError{err: err, masker: r.printer().Masker()}
	}

	ctx := r.newJobContext(vars)
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

	if err := r.runJobs(ctx); err != nil {
		return err
	}

	reporter.Finish(ctx.Result)
	r.finishedAt = time.Now()
	r.result = ctx.Result
	r.exitStatus = ctx.Result.exitCode(r.failed.Load())
	return nil
}

// printer returns the printer of the workflow, which the run prints with.
func (r *workflowRun) printer() *Printer {
	return r.w.printer
}

// startReport sets up what the run prints with, the masker of its secrets
// and a reporter of its own, and prints the header of the report. It
// returns the reporter.
func (r *workflowRun) startReport() Reporter {
	if r.w.printer == nil {
		r.w.printer = NewPrinter(r.config.Verbose)
	}
	p := r.printer()

	// Install the masker before anything is printed, the header included.
	// It masks the secrets in the environment the run's vars read, which
	// the variables read from files are part of.
	p.SetMasker(mask.New(r.w.Secrets, r.env))
	// A secret too short to mask inside other text is shown there as it is.
	// Say so once, by name, so that the user can lengthen it rather than
	// trust it to be hidden.
	for _, name := range p.Masker().TooShort() {
		p.LogWarn("the secret %s is shorter than %d characters, so it is masked only where it is a whole value; use a value of at least %d characters", name, mask.MinLength, mask.MinLength)
	}

	// A reporter tracks how far the report has been emitted, which is state
	// for this run alone, so every run gets a fresh one.
	render := newReportRenderer(r.jobIDs(), r.config.Baseline, p.Masker())
	reporter := newReporter(r.config.Output, p, render)
	p.SetReporter(reporter)

	r.startedAt = time.Now()
	reporter.Start(r.w.Name, r.w.Description)
	return reporter
}

// evalVars evaluates the vars of the workflow, with the vars the person
// running it gave and the environment of the run, as vareval.Eval does. A
// var whose template cannot be evaluated is an error, which stops the run
// before any job starts.
func (r *workflowRun) evalVars() (map[string]any, error) {
	return vareval.Eval(r.w.Vars, r.config.Vars, r.env)
}

// newJobContext returns the context the jobs of the run run with, with a
// result of their own and vars.
func (r *workflowRun) newJobContext(vars map[string]any) JobContext {
	p := r.printer()
	rs := r.newResult()
	rs.SetReporter(p.Reporter())

	return JobContext{
		runEnv: &runEnv{
			Config:     r.config,
			Printer:    p,
			Result:     rs,
			scheduler:  r.scheduler,
			background: newBackground(),
			cleanups:   newCleanups(r.id, p, r.config.Trace),
			baseDir:    r.w.basePath,
			runID:      r.id,
			actions:    r.w.named,
		},
		Vars:    vars,
		Outputs: r.outputs,
	}
}

// newResult returns the result of the run, with a result for each of its
// jobs.
func (r *workflowRun) newResult() *Result {
	rs := NewResult()
	for _, job := range r.jobs {
		rs.Jobs[job.ID] = &JobResult{
			JobName:   job.Name,
			JobID:     job.ID,
			StartTime: time.Now(),
		}
	}
	return rs
}

// runJobs runs the jobs, each as soon as the jobs it needs are done, rather
// than once every job started with them is: a job does not wait for one it
// does not need. A job whose dependency failed is skipped as soon as that is
// known.
func (r *workflowRun) runJobs(ctx JobContext) error {
	done := make(chan struct{})
	running := 0

	for {
		skippedJobs := ctx.scheduler.SkipBlocked()
		if ctx.Result != nil {
			skipJobs(skippedJobs, ctx.Result)
		}

		running += r.startJobs(ctx.scheduler.Runnable(), ctx, done)
		if running == 0 {
			break
		}

		// Whichever job ends, the jobs that need it may be able to run now.
		<-done
		running--
	}

	// With no job running, a job not over yet would never run: what it
	// waits for will not change any more.
	if pending := ctx.scheduler.Pending(); len(pending) > 0 {
		return fmt.Errorf("jobs %s cannot run: the jobs they need will not be done", strings.Join(pending, ", "))
	}
	return nil
}

// skipJobs records the jobs of jobIDs as skipped in rs, as jobs whose
// dependencies failed.
func skipJobs(jobIDs []string, rs *Result) {
	for _, jobID := range jobIDs {
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

// startJobs starts each of jobIDs, and returns how many it started. Each job
// sends to done once it is over and its status says so, and marks the run
// failed when it does not succeed.
func (r *workflowRun) startJobs(jobIDs []string, ctx JobContext, done chan<- struct{}) int {
	for _, jobID := range jobIDs {
		job := r.job(jobID)
		ctx.scheduler.Start(jobID)

		go func(j *Job) {
			defer func() { done <- struct{}{} }()

			if !NewExecutor(j).Run(ctx) {
				r.failed.Store(true)
			}
		}(job)
	}
	return len(jobIDs)
}

// job returns the job of id. The scheduler has the jobs of the run alone, so
// a job it starts is one of them.
func (r *workflowRun) job(id string) *Job {
	for i := range r.jobs {
		if r.jobs[i].ID == id {
			return &r.jobs[i]
		}
	}
	panic(fmt.Sprintf("probe: no job has the ID %q", id))
}

// writeReports writes every report file the run was asked for, compared to
// the baseline when there is one. A file that cannot be written does not
// stop the others; the errors are returned together.
func (r *workflowRun) writeReports() error {
	if len(r.config.Reports) == 0 {
		return nil
	}

	rep := BuildReport(r.w.Name, r.w.Description, r.result, r.jobIDs(), r.startedAt, r.finishedAt)
	rep.RunID = r.id
	if r.selection != nil {
		rep.Selection = r.selection.report()
	}
	rep.Compare(r.config.Baseline)
	rep.Mask(r.printer().Masker())

	var errs []error
	for _, t := range r.config.Reports {
		err := rep.Write(t)
		if errors.Is(err, report.ErrNoStepSummary) || errors.Is(err, report.ErrStepSummaryFull) {
			// Outside GitHub Actions there is no summary to write to, and a
			// full one has no room; say so and carry on, so one command line
			// serves CI and a laptop and a full summary fails no run.
			r.printer().LogWarn("%v", err)
			continue
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
