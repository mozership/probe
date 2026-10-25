package probe

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mozership/probe/expr"
)

// problem is what keeps a job from running as it is written. It is found as
// the workflow is loaded, so that the run is refused before any job starts,
// rather than when the job or the step it is in is reached.
type problem struct {
	// path is the key the problem is at, such as jobs[0].steps[1].test, by
	// which probe check finds its line.
	path string
	// where names the job and the step it is in, such as job 0 "Login",
	// step 1; empty when it is in neither.
	where string
	err   error
}

func (p problem) Error() string {
	if p.where == "" {
		return p.err.Error()
	}
	return p.where + ": " + p.err.Error()
}

// problemsError joins problems into the error a run is refused with.
func problemsError(problems []problem) error {
	errs := make([]error, len(problems))
	for i, p := range problems {
		errs[i] = p
	}
	return NewConfigurationError("invalid_workflow", "the workflow cannot run as it is written", errors.Join(errs...))
}

// problems returns what keeps the jobs of w from running as they are
// written, in the order they are written. The vars of the workflow are left
// out: a var the person running it gives replaces what it writes.
func (w *Workflow) problems() []problem {
	var out []problem
	for i := range w.Jobs {
		out = append(out, w.Jobs[i].problems(fmt.Sprintf("jobs[%d]", i), jobWhere(w.Jobs, i))...)
	}
	return out
}

// problems returns what keeps j from running as it is written, with each
// path under jp, the path of the job, and named after where, which names it.
func (j *Job) problems(jp, where string) []problem {
	var out []problem
	if err := j.validateSteps(); err != nil {
		out = append(out, problem{path: jp + ".steps", where: where, err: err})
	}
	out = append(out, templateProblems(jp+".name", where, "name", j.Name)...)
	out = append(out, expressionProblems(jp+".skipif", where, "skipif", j.SkipIf)...)
	if j.Repeat != nil {
		out = append(out, expressionProblems(jp+".repeat.test", where, "repeat.test", j.Repeat.Test)...)
		out = append(out, expressionProblems(jp+".repeat.baseline_test", where, "repeat.baseline_test", j.Repeat.BaselineTest)...)
	}
	for k, st := range j.Steps {
		if st == nil {
			continue
		}
		sp := fmt.Sprintf("%s.steps[%d]", jp, k)
		sw := stepWhere(where, k, st)
		out = append(out, templateProblems(sp+".name", sw, "name", st.Name)...)
		out = append(out, templateProblems(sp+".with", sw, "with", st.With)...)
		out = append(out, templateProblems(sp+".vars", sw, "vars", st.Vars)...)
		out = append(out, templateProblems(sp+".echo", sw, "echo", st.Echo)...)
		out = append(out, templateProblems(sp+".iteration", sw, "iteration", st.Iteration.Expr)...)
		out = append(out, waitProblems(sp+".wait", sw, st.Wait)...)
		out = append(out, expressionProblems(sp+".test", sw, "test", st.Test)...)
		out = append(out, expressionProblems(sp+".skipif", sw, "skipif", st.SkipIf)...)
		for _, key := range slices.Sorted(maps.Keys(st.Outputs)) {
			out = append(out, expressionProblems(sp+".outputs."+key, sw, "outputs."+key, st.Outputs[key])...)
		}
	}
	return out
}

// expressionProblems returns the problem of input, the expression field
// is, when it does not parse.
func expressionProblems(path, where, field, input string) []problem {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	if err := expr.Parse(input); err != nil {
		return []problem{{path: path, where: where, err: fmt.Errorf("%s: %w", field, err)}}
	}
	return nil
}

// templateProblems returns the problems of the templates in v, a string or
// a map or list of them at any depth, keys too, that do not parse.
func templateProblems(path, where, field string, v any) []problem {
	var out []problem
	switch v := v.(type) {
	case string:
		for _, e := range expr.TemplateExprs(v) {
			if err := expr.Parse(e); err != nil {
				out = append(out, problem{path: path, where: where, err: fmt.Errorf("%s: template {{%s}}: %w", field, e, err)})
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			out = append(out, templateProblems(path+"."+k, where, field+"."+k, k)...)
			out = append(out, templateProblems(path+"."+k, where, field+"."+k, v[k])...)
		}
	case []any:
		for i, e := range v {
			out = append(out, templateProblems(fmt.Sprintf("%s[%d]", path, i), where, fmt.Sprintf("%s[%d]", field, i), e)...)
		}
	}
	return out
}

// waitProblems returns the problem of wait when it is not a time to wait,
// or when its templates do not parse. A wait a template gives is known only
// when the step runs.
func waitProblems(path, where, wait string) []problem {
	if len(expr.TemplateExprs(wait)) > 0 {
		return templateProblems(path, where, "wait", wait)
	}
	if wait == "" {
		return nil
	}
	if _, err := parseWaitDuration(wait); err != nil {
		return []problem{{path: path, where: where, err: fmt.Errorf("wait: %w", err)}}
	}
	return nil
}
