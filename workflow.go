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
	"github.com/mozership/probe/expr"
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

	// Build the scheduler first: it assigns an ID to every job that omits one,
	// and the result, the report order and the scheduler all index jobs by ID,
	// so they have to agree before anything keys off them.
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

	// A reporter tracks how far the report has been emitted, which is state
	// for this run alone, so every run gets a fresh one. A caller cannot know
	// a generated ID in advance, so the report is rendered in the order of
	// the IDs the jobs have now.
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
		jobID := job.ID
		if jobID == "" {
			jobID = job.Name
		}
		jr := &JobResult{
			JobName:   job.Name,
			JobID:     jobID,
			StartTime: time.Now(),
		}
		rs.Jobs[jobID] = jr
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
			jr.Status = "skipped"
			jr.Success = true // Skipped jobs are considered successful (same as skipif)
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
		ctx.JobScheduler.SetJobStatus(jobID, JobRunning, false)

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

// evalVars evaluates template variables in workflow vars using environment
// variables. A var can read another as vars.<name>; each one is evaluated
// after the vars it reads, so a value such as random_str(8) is computed once
// and every var that reads it sees the same value. A var whose template
// cannot be evaluated is an error, which stops the workflow before any job
// starts.
//
// A var the person running the workflow gave is not evaluated: it is the
// text given, whatever the workflow writes for it, and the vars that read it
// see that text.
func (w *Workflow) evalVars() (map[string]any, error) {
	written := w.Vars
	if len(w.given) > 0 {
		written = maps.Clone(w.Vars)
		if written == nil {
			written = make(map[string]any, len(w.given))
		}
		for name, value := range w.given {
			written[name] = givenVar(value)
		}
	}

	order, err := varsOrder(written)
	if err != nil {
		return nil, err
	}

	vars := make(map[string]any)
	env := strmapToAnymap(w.Env())

	var errs []error
	failed := make(map[string]bool)
	done := make(map[string]bool)
	var cycle error

	// evaluate evaluates the var k, which path, the vars being evaluated,
	// waits for. A template a var expands is read only when it runs, so a
	// var it reads that has not been evaluated yet, which varsOrder could not
	// see, is evaluated first and k evaluated again after it. k has kept no
	// value then, so a value such as random_str(8) is still computed once.
	var evaluate func(k string, path []string)
	evaluate = func(k string, path []string) {
		if done[k] || failed[k] || cycle != nil {
			return
		}
		v := written[k]
		if given, ok := v.(givenVar); ok {
			vars[k] = string(given)
			done[k] = true
			return
		}

		// A var that reads one that failed is not evaluated: its error
		// would only repeat that one.
		keys, dynamic := expr.Refs(v, "vars")
		if len(failed) > 0 && (dynamic || expr.CallsTemplate(v) || slices.ContainsFunc(keys, func(d string) bool { return failed[d] })) {
			failed[k] = true
			return
		}

		ev := &expr.Expr{BeforeTemplate: func(text string) error {
			keys, dynamic := expr.Refs(text, "vars")
			for _, d := range keys {
				if _, ok := written[d]; ok && !done[d] {
					return &pendingVarError{name: d}
				}
			}
			// A template that reads vars by a key known only when it runs
			// waits for every other var, as varsOrder orders such a var.
			if dynamic {
				for _, d := range order {
					if d != k && !done[d] {
						return &pendingVarError{name: d}
					}
				}
			}
			return nil
		}}
		for {
			// Each var reads a copy of the vars evaluated so far. A
			// template such as {{vars}} keeps the map it returns, and the
			// map being filled in would then hold itself.
			env["vars"] = maps.Clone(vars)

			out, err := evalVar(ev, k, v, env)
			var pending *pendingVarError
			if errors.As(err, &pending) {
				d := pending.name
				waiting := append(slices.Clone(path), k)
				if i := slices.Index(waiting, d); i >= 0 {
					chain := append(waiting[i:], d)
					cycle = fmt.Errorf("vars: circular reference: %s", strings.Join(chain, " -> "))
					return
				}
				evaluate(d, waiting)
				if cycle != nil {
					return
				}
				if failed[d] {
					failed[k] = true
					return
				}
				continue
			}
			if err != nil {
				errs = append(errs, err)
				failed[k] = true
				return
			}
			vars[k] = out
			done[k] = true
			return
		}
	}
	for _, k := range order {
		evaluate(k, nil)
		if cycle != nil {
			return nil, cycle
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return vars, nil
}

// givenVar is the value of a var the person running the workflow gave. It
// is a type of its own so that it is told from a string the workflow
// writes, whose templates are evaluated.
type givenVar string

// pendingVarError stops the evaluation of a var whose template reads the var
// name, which has not been evaluated yet.
type pendingVarError struct {
	name string
}

func (e *pendingVarError) Error() string {
	return fmt.Sprintf("vars.%s has not been evaluated yet", e.name)
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

// evalVar evaluates the templates in the var name, a workflow var or a step
// var: a string becomes a string, and a map or a list keeps the type of each
// value that is a single template. Other values are kept as they are. An
// error names the value that could not be evaluated, as vars.auth.user.
func evalVar(ev *expr.Expr, name string, v any, env any) (any, error) {
	switch v := v.(type) {
	case string:
		out, err := ev.EvalTemplate(v, env)
		if err != nil {
			return nil, &expr.FieldError{Path: "vars." + name, Err: err}
		}
		return out, nil
	case map[string]any:
		out, err := ev.EvalTemplateMap(v, env)
		if err != nil {
			return nil, prefixFieldErrors(err, "vars."+name+".")
		}
		return out, nil
	case []any:
		// Evaluated as a map's value is, so that a list renders the
		// templates in it as one nested in a map does. The map is keyed by a
		// name of its own rather than the var's, which, holding a template,
		// would be evaluated as a key is; the errors are named after the var.
		const key = "list"
		out, err := ev.EvalTemplateMap(map[string]any{key: v}, env)
		if err != nil {
			return nil, renameFieldErrors(err, key, "vars."+name)
		}
		return out[key], nil
	default:
		return v, nil
	}
}

// renameFieldErrors names the value at the top of the path of each
// *expr.FieldError joined into err, from, as to.
func renameFieldErrors(err error, from, to string) error {
	rename := func(path string) string {
		if rest, ok := strings.CutPrefix(path, from); ok {
			return to + rest
		}
		return path
	}
	var errs []error
	for _, e := range unwrapJoined(err) {
		var fe *expr.FieldError
		if errors.As(e, &fe) {
			evaluated := ""
			if fe.EvaluatedPath != "" {
				evaluated = rename(fe.EvaluatedPath)
			}
			e = &expr.FieldError{Path: rename(fe.Path), EvaluatedPath: evaluated, Err: fe.Err}
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// prefixFieldErrors puts prefix before the path of each *expr.FieldError
// joined into err, so that the path names the value from further up.
func prefixFieldErrors(err error, prefix string) error {
	var errs []error
	for _, e := range unwrapJoined(err) {
		var fe *expr.FieldError
		if errors.As(e, &fe) {
			evaluated := ""
			if fe.EvaluatedPath != "" {
				evaluated = prefix + fe.EvaluatedPath
			}
			e = &expr.FieldError{Path: prefix + fe.Path, EvaluatedPath: evaluated, Err: fe.Err}
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// varsOrder returns the names of vars in an order in which each comes after
// the vars it reads. A var that reads vars by a key known only when it runs
// comes after all the others. A var that expands a template, such as one read
// from a file, comes after all the vars that do not, since what the template
// reads is known only when it runs; among themselves they are ordered by what
// they read outside it. Names are otherwise sorted, and Refs returns the keys
// of a map in the order of its sorted keys, so neither the order nor a
// reported cycle depends on map iteration.
func varsOrder(vars map[string]any) ([]string, error) {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	slices.Sort(names)

	expands := make(map[string]bool, len(vars))
	for _, k := range names {
		expands[k] = expr.CallsTemplate(vars[k])
	}

	deps := make(map[string][]string, len(vars))
	for _, k := range names {
		keys, dynamic := expr.Refs(vars[k], "vars")
		for _, d := range keys {
			// A name that is not a var reads as nil, as it always has.
			if _, ok := vars[d]; ok {
				deps[k] = append(deps[k], d)
			}
		}
		if dynamic {
			// A var that reads vars as a whole is taken to read every other
			// var, but not itself: only a key it names reads itself.
			for _, d := range names {
				if d != k && !slices.Contains(deps[k], d) {
					deps[k] = append(deps[k], d)
				}
			}
		}
		if expands[k] {
			// Two vars that expand templates are not taken to read each
			// other, which would make every pair of them a cycle.
			for _, d := range names {
				if d != k && !expands[d] && !slices.Contains(deps[k], d) {
					deps[k] = append(deps[k], d)
				}
			}
		}
	}

	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(vars))
	order := make([]string, 0, len(vars))
	var path []string
	var visit func(k string) error
	visit = func(k string) error {
		switch state[k] {
		case done:
			return nil
		case visiting:
			i := slices.Index(path, k)
			cycle := append(slices.Clone(path[i:]), k)
			return fmt.Errorf("vars: circular reference: %s", strings.Join(cycle, " -> "))
		}
		state[k] = visiting
		path = append(path, k)
		for _, d := range deps[k] {
			if err := visit(d); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[k] = done
		order = append(order, k)
		return nil
	}
	for _, k := range names {
		if err := visit(k); err != nil {
			return nil, err
		}
	}

	return order, nil
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
			cleanups:     newCleanups(w.runID, w.printer),
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
		for i, st := range job.Steps {
			if !actionref.IsExternal(st.Uses) {
				if len(builtin) > 0 && !slices.Contains(builtin, st.Uses) {
					return guard, NewConfigurationError("resolve_action", "failed to resolve an action", fmt.Errorf("job %q, step %d: %w", job.Name, i, unknownAction(st.Uses, builtin))).
						WithContext("uses", st.Uses).
						WithContext("job", job.Name).
						WithContext("step", i)
				}
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

// unknownAction is the error of a uses that names no action: it is not one
// of builtin, the actions of Probe, not an external action, and not a name
// the workflow gives one under actions, which would have been replaced.
func unknownAction(uses string, builtin []string) error {
	msg := fmt.Sprintf("unknown action %q: it is not an action of Probe, an external action, or a name given under actions", uses)
	if s := suggest(uses, builtin); s != "" {
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
