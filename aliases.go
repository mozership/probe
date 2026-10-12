package probe

import (
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
)

// aliasName is a name a workflow can give an external action: one that
// cannot be taken for a path or a repository, or hold a template.
var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// aliasProblem is what is wrong with a name the workflow's actions give,
// or with a use of one.
type aliasProblem struct {
	// name is the name under actions, or empty when job is set.
	name string
	// job is the index of the job whose defaults are wrong, with action the
	// key of them that is, or workflowDefaults for the defaults of the
	// workflow.
	job    int
	action string
	msg    string
}

// workflowDefaults is the job of an aliasProblem in the defaults of the
// workflow, which belong to no job.
const workflowDefaults = -1

func (a aliasProblem) Error() string {
	return a.msg
}

// checkAliases returns what is wrong with the names the workflow's actions
// give, in the order of the names. builtin are the names of the actions of
// Probe, which a name must not be; when it is empty, that is not checked.
func (w *Workflow) checkAliases(builtin []string) []aliasProblem {
	var problems []aliasProblem
	for _, name := range slices.Sorted(maps.Keys(w.Actions)) {
		ref := w.Actions[name]
		var msg string
		switch {
		case !aliasName.MatchString(name):
			msg = fmt.Sprintf("actions: the name %q must be letters, digits, '_' and '-', starting with a letter or a digit", name)
		case slices.Contains(builtin, name):
			msg = fmt.Sprintf("actions: the name %q is that of an action of Probe, which a workflow cannot give to another", name)
		case !actionref.IsExternal(ref):
			msg = fmt.Sprintf("actions: %s must name an external action, as github.com/<owner>/<repo>@<commit SHA> or a local path starting with ./, not %q", name, ref)
		// A local path is taken as it is written, so a template in one would
		// name a directory with braces in its name.
		case len(expr.TemplateExprs(ref)) > 0:
			msg = fmt.Sprintf("actions: %s must name an external action as it is, not by a template: %q", name, ref)
		default:
			if _, err := actionref.Parse(ref); err != nil {
				msg = fmt.Sprintf("actions: %s: %s", name, err)
			}
		}
		if msg != "" {
			problems = append(problems, aliasProblem{name: name, msg: msg})
		}
	}
	return problems
}

// resolveAliases writes, in place of each name the workflow's actions give,
// the external action it names: in the uses of the steps and in the keys of
// the defaults of the jobs and of the workflow. What runs the workflow, the guard included, then
// sees the action as if the workflow had named it in full, so that a name
// lets nothing run that the action it stands for would not.
//
// A name that is wrong, as checkAliases tells, is left as it is written. It
// returns the defaults that are written for an action twice, once by its
// name and once in full.
func (w *Workflow) resolveAliases(wrong []aliasProblem) []aliasProblem {
	names := maps.Clone(w.Actions)
	for _, p := range wrong {
		delete(names, p.name)
	}
	if len(names) == 0 {
		return nil
	}

	var problems []aliasProblem
	twice := func(job int, actions []string) {
		for _, action := range actions {
			problems = append(problems, aliasProblem{job: job, action: action, msg: fmt.Sprintf("defaults: %s and %s are the same action, whose defaults can be written once", action, names[action])})
		}
	}
	twice(workflowDefaults, renameDefaults(w.Defaults, names))
	for i := range w.Jobs {
		twice(i, w.Jobs[i].useNames(names))
	}
	return problems
}

// useNames writes, in place of each of names, the action it stands for: in
// the uses of the job's steps and in the keys of its defaults. It returns
// the names the defaults are written by when they are written for the
// action in full as well, and leaves those as they are.
func (j *Job) useNames(names map[string]string) []string {
	for _, st := range j.Steps {
		if st == nil {
			continue
		}
		if ref, ok := names[st.Uses]; ok {
			st.Uses = ref
		}
	}
	return renameDefaults(j.Defaults, names)
}

// renameDefaults writes, in place of each key of defaults that is one of
// names, the action it stands for. It returns the names the defaults are
// written by when they are written for the action in full as well, and
// leaves those as they are.
func renameDefaults(v any, names map[string]string) []string {
	defaults, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	var twice []string
	for _, action := range slices.Sorted(maps.Keys(defaults)) {
		ref, ok := names[action]
		if !ok {
			continue
		}
		if _, dup := defaults[ref]; dup {
			twice = append(twice, action)
			continue
		}
		defaults[ref] = defaults[action]
		delete(defaults, action)
	}
	return twice
}

// UseActions reads the job by names, the names a workflow gives its external
// actions: a uses or a key of the defaults that is one of them becomes the
// action it stands for. The embedded action does so for the job it runs,
// with the names of the workflow that embeds it, before ApplyDefaults.
func (j *Job) UseActions(names map[string]string) error {
	if twice := j.useNames(names); len(twice) > 0 {
		return fmt.Errorf("defaults: %s and %s are the same action, whose defaults can be written once", twice[0], names[twice[0]])
	}
	return nil
}

// namedActions returns the names the workflow gives its external actions,
// without those in wrong, for a job it embeds to be read by. A local action
// is given as an absolute path: the workflow's ./action is relative to the
// workflow file, where the job's own would be relative to the job file.
func (w *Workflow) namedActions(wrong []aliasProblem) map[string]string {
	names := maps.Clone(w.Actions)
	for _, p := range wrong {
		delete(names, p.name)
	}
	for name, action := range names {
		ref, err := actionref.Parse(action)
		if err != nil || ref.Local == "" {
			continue
		}
		dir := ref.Local
		if !filepath.IsAbs(dir) && w.basePath != "" {
			dir = filepath.Join(w.basePath, dir)
		}
		if abs, err := filepath.Abs(dir); err == nil {
			names[name] = abs
		}
	}
	return names
}
