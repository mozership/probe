package probe

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/mozership/probe/actionrpc"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/schedule"
	"github.com/mozership/probe/internal/suggest"
	"github.com/mozership/probe/internal/vareval"
	"github.com/mozership/probe/mask"
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
	Secrets []string `yaml:"secrets,omitempty"`
	// exitStatus is that of the last run Start finished, for ExitStatus.
	exitStatus int
	// env is the workflow's own environment, which the vars of every run
	// read; the process's when it is not set. What a run is given in
	// Config.Env is the run's own, and is not kept here.
	env map[string]string
	// basePath is the directory containing the workflow file (used for resolving relative paths)
	basePath string
	// named are the names the workflow gives its external actions, as a job
	// it embeds is told them
	named   map[string]string
	printer *Printer
	// runID names the last run Start began, for RunID.
	runID string
}

// RunID returns the ID of the last run Start began, which every action of
// the run is told about, or an empty string before Start.
func (w *Workflow) RunID() string {
	return w.runID
}

// newRunID returns a random ID for a run.
func newRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start runs the workflow with the given configuration. It prepares the
// run, which refuses the workflow for what keeps it from running before any
// job starts, executes the jobs of the run, and writes the report files the
// run was asked for. What a run decides and finds out is its own, so the
// workflow is left as it was for the next Start, but for the exit status and
// the ID of the last run.
func (w *Workflow) Start(c Config) error {
	r, err := w.prepare(c)
	if err != nil {
		return err
	}
	w.runID = r.id
	if err := r.execute(); err != nil {
		return err
	}
	w.exitStatus = r.exitStatus
	return r.writeReports()
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

// jobNamer returns the name of the job of each id among jobs, with the
// templates in it evaluated with vars where they can be: a name that reads
// more than the vars, such as repeat_index, is as written.
func jobNamer(jobs []Job, vars map[string]any) func(id string) string {
	ev := &expr.Expr{}
	env := map[string]any{"vars": vars}
	return func(id string) string {
		for _, j := range jobs {
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

// newScheduler returns a scheduler of jobs, added in their order.
func newScheduler(jobs []Job) (*schedule.Scheduler, error) {
	scheduler := schedule.New()
	for i := range jobs {
		if err := addToSchedule(scheduler, &jobs[i]); err != nil {
			return nil, err
		}
	}
	return scheduler, nil
}

// addToSchedule adds job to s by what s waits on: its ID, its needs and
// whether it always runs. The jobs of a workflow are given their IDs before
// they are added, by assignJobIDs.
func addToSchedule(s *schedule.Scheduler, job *Job) error {
	if job.ID == "" {
		return fmt.Errorf("job %q has no ID", job.Name)
	}
	return s.Add(schedule.Job{ID: job.ID, Needs: job.Needs, Always: job.Always})
}

// checkCycle refuses jobs whose needs are in a cycle, as a run does. A need
// that is no job is not refused.
func (w *Workflow) checkCycle() error {
	scheduler, err := newScheduler(w.Jobs)
	if err != nil {
		return err
	}
	return scheduler.CheckCycle()
}

// validatedScheduler returns a scheduler of jobs, refusing them for a need
// that is no job and for needs in a cycle.
func validatedScheduler(jobs []Job) (*schedule.Scheduler, error) {
	scheduler, err := newScheduler(jobs)
	if err != nil {
		return nil, err
	}
	if err := scheduler.Validate(); err != nil {
		return nil, err
	}
	return scheduler, nil
}

// Env returns the workflow's own environment, which the vars of every run
// read: the process's, as it was when Env was first called, unless the
// workflow was given one. What a run is given in Config.Env is that run's
// alone, and is not in it.
func (w *Workflow) Env() map[string]string {
	if len(w.env) == 0 {
		w.env = envMap()
	}
	return w.env
}

// evalVars evaluates the vars of the workflow with its own environment
// alone, as vareval.Eval does, for what is drawn of it before a run. A run
// evaluates them with what it is given, in workflowRun.evalVars.
func (w *Workflow) evalVars() (map[string]any, error) {
	return vareval.Eval(w.Vars, nil, w.Env())
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
				return guard, NewConfigurationError("resolve_action", "failed to resolve an external action", err)
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
				return guard, NewConfigurationError("resolve_action", "failed to resolve an external action", err)
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
		return NewConfigurationError("resolve_action", "failed to resolve an action", fmt.Errorf("job %q, step %d: %w", job.Name, i, unknownAction(st.Uses, builtin)))
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
