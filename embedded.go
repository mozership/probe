package probe

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
)

// embeddedAction is the action that runs a job file as a step, through
// Job.RunStandalone. Its job files are those of Probe, so they are read
// ahead of the run, for the graph and for what the run would refuse, as the
// action reads them with ReadJobFile.
const embeddedAction = "embedded"

// embeddedJobFile returns the path of the job file st runs, as its with
// gives it, when st is a step of the embedded action that gives one.
func embeddedJobFile(st *Step) (string, bool) {
	if st == nil || st.Uses != embeddedAction {
		return "", false
	}
	path, ok := st.With["path"].(string)
	return path, ok && path != ""
}

// ReadJobFile reads the job file at path, as the embedded action runs it: by
// names, the names the workflow that embeds it gives its external actions,
// and with the job's defaults applied to its steps. A nil names reads each
// uses as it is written.
func ReadJobFile(path string, names map[string]string) (*Job, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("embedded steps file does not exist: %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded steps file: %w", err)
	}

	job := &Job{}
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Validator(validator.New()), yaml.AllowDuplicateMapKey())
	if err := dec.Decode(job); err != nil {
		return nil, fmt.Errorf("failed to decode YAML job: %w", err)
	}
	if len(job.Steps) == 0 {
		return nil, fmt.Errorf("no steps found in embedded file: %s", path)
	}

	// Before the defaults, which are keyed by what a step's uses says.
	if err := job.UseActions(names); err != nil {
		return nil, fmt.Errorf("embedded steps file %s: %w", path, err)
	}
	job.ApplyDefaults()
	return job, nil
}

// eachEmbeddedStep calls visit for each step of the job file st embeds, when
// st is a step of the embedded action, and of the job files those steps
// embed in turn, with the path of the file as it is written and the position
// of the step in it. seen holds the files read, each of which is read once,
// so that job files that embed one another end.
//
// Only what can be told before a run is read: a path that is a template is
// not followed, and nor is a file that cannot be read.
func eachEmbeddedStep(st *Step, seen map[string]bool, visit func(file string, index int, st *Step)) {
	file, ok := embeddedJobFile(st)
	if !ok || expr.HasTemplate(file) {
		return
	}
	abs, err := filepath.Abs(file)
	if err != nil || seen[abs] {
		return
	}
	seen[abs] = true
	job, err := ReadJobFile(file, nil)
	if err != nil {
		return
	}
	for i, es := range job.Steps {
		if es == nil {
			continue
		}
		visit(file, i, es)
		eachEmbeddedStep(es, seen, visit)
	}
}

// checkEmbeddedJobs reads the job file of each step of jobs that embeds
// one, and those that job embeds in turn, and returns an error for a uses in
// them that names no action, so that it fails the run before any job starts
// rather than when the step is reached. A job is read by named, the names
// the workflow gives its external actions, as the embedded action reads it.
// builtin are the names of the actions of Probe; when it is empty, nothing
// is checked. vars are the vars of the run, which a path may read.
//
// What cannot be known yet is left to the embedded action, which checks the
// same as it starts a job: a path that reads more than the workflow's vars,
// and a file that cannot be read. An external action a job names is
// resolved then as well.
func checkEmbeddedJobs(jobs []Job, named map[string]string, builtin []string, vars map[string]any) error {
	if len(builtin) == 0 {
		return nil
	}
	ev := &expr.Expr{}
	env := map[string]any{"vars": vars}
	var found error
	for _, job := range jobs {
		for _, st := range job.Steps {
			path, ok := embeddedJobFile(st)
			if !ok {
				continue
			}
			// A path the vars of the workflow give is known by now.
			if expr.HasTemplate(path) {
				expanded, err := ev.EvalTemplate(path, env)
				if err != nil {
					continue
				}
				st = &Step{Uses: embeddedAction, With: map[string]any{"path": expanded}}
			}
			eachEmbeddedStep(st, map[string]bool{}, func(file string, i int, es *Step) {
				uses := es.Uses
				if ref, ok := named[uses]; ok {
					uses = ref
				}
				if found != nil || actionref.IsExternal(uses) || slices.Contains(builtin, uses) {
					return
				}
				found = NewConfigurationError("resolve_action", "failed to resolve an action of an embedded job", fmt.Errorf("%s, step %d: %w", file, i, unknownAction(uses, builtin)))
			})
			if found != nil {
				return found
			}
		}
	}
	return nil
}
