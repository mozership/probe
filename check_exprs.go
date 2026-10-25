package probe

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mozership/probe/expr"
)

// exprAt is an expression of a job or a step, or one of the templates of
// it, and where it is.
type exprAt struct {
	path, where, field string
	// job and step are the indexes of the job and the step it is in; step
	// is -1 for a field of the job read before its steps.
	job, step int
	// late is whether it is read once the step has published its outputs,
	// as test and echo are.
	late  bool
	input string
}

// eachExpr calls f with each expression of the jobs and their steps, and
// each template, in the order they are read.
func (c *checker) eachExpr(f func(exprAt)) {
	templates := func(e exprAt, v any) {
		forStrings(e.path, v, func(path, s string) {
			for _, input := range expr.TemplateExprs(s) {
				e.path, e.input = path, input
				f(e)
			}
		})
	}
	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		jobField := func(key string) exprAt {
			return exprAt{path: jp + "." + key, where: c.jobWhere(i), field: key, job: i, step: -1}
		}
		templates(jobField("name"), job.Name)
		e := jobField("skipif")
		e.input = job.SkipIf
		f(e)
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d]", jp, j)
			field := func(key string, late bool) exprAt {
				return exprAt{path: sp + "." + key, where: c.stepWhere(i, j), field: key, job: i, step: j, late: late}
			}
			expression := func(key string, late bool, input string) {
				e := field(key, late)
				e.input = input
				f(e)
			}
			templates(field("name", false), st.Name)
			templates(field("with", false), st.With)
			templates(field("vars", false), st.Vars)
			templates(field("iteration", false), st.Iteration.Expr)
			templates(field("wait", false), st.Wait)
			expression("skipif", false, st.SkipIf)
			for _, k := range slices.Sorted(maps.Keys(st.Outputs)) {
				expression("outputs."+k, false, st.Outputs[k])
			}
			expression("test", true, st.Test)
			templates(field("echo", true), st.Echo)
		}
	}
}

// The fields of a step that are evaluated once its action has returned, and
// can read what it returned under res and req: test, echo and outputs. The
// rest are evaluated before it runs.
func readsResult(field string) bool {
	return field == "test" || field == "echo" || strings.HasPrefix(field, "outputs.")
}

// forStrings calls f with each string in v, a string or a map or list of
// them at any depth, keys too, and the path it is at under path.
func forStrings(path string, v any, f func(path, s string)) {
	switch v := v.(type) {
	case string:
		f(path, v)
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			f(path+"."+k, k)
			forStrings(path+"."+k, v[k], f)
		}
	case []any:
		for i, e := range v {
			forStrings(fmt.Sprintf("%s[%d]", path, i), e, f)
		}
	}
}

func isTemplate(s string) bool {
	return len(expr.TemplateExprs(s)) > 0
}
