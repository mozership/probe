package probe

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/suggest"
)

// The names an expression of a step, or the skipif of a job, reads from,
// and those the tests of a repeated job read from, from the fields of what
// they are evaluated against.
var (
	stepNames   = exprKeys(reflect.TypeFor[StepContext]())
	repeatNames = exprKeys(reflect.TypeFor[repeatTestEnv]())
)

func exprKeys(t reflect.Type) []string {
	var keys []string
	for f := range t.Fields() {
		if name := f.Tag.Get("expr"); name != "" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

// checkNames checks the names that the expressions and templates read. A
// name that what they are evaluated against does not have, such as steps in
// the test of a step, is an error: there is nothing to read it from, so the
// expression fails: the step fails, or its skipif, wait or output is left
// out. A var not defined where it is read, by the workflow or by the step,
// whose own vars read only those of the workflow, is a warning, as it reads
// nil unless --var gives it, which check cannot know. A call of a method
// expressions do not have, such as res.body.contains("x"), is an error too,
// with how it is written in expressions, as it fails whatever it is called
// on.
//
// Of the templates of a job's name, which are evaluated against the job and
// so can read more than they are meant to, and of the workflow's vars, which
// read the environment, only the vars they read and the methods they call
// are checked.
func (c *checker) checkNames() {
	workflowVars := varNames(c.wf.Vars)
	// later are the vars the step defines that are not defined yet where
	// input is read.
	read := func(path, where, field, input string, names, vars, later []string) {
		add := func(severity, format string, args ...any) {
			c.addOnce(severity, path, where, "%s: "+format, append([]any{field}, args...)...)
		}
		for _, name := range expr.Names(input) {
			if names == nil || slices.Contains(names, name) {
				continue
			}
			add(SeverityError, "%s is not defined; it can read %s%s", name, listNames(names), nameHint(name, names))
		}
		for _, h := range expr.MethodCallHints(input) {
			add(SeverityError, "%s", h)
		}
		if vars == nil {
			return
		}
		for _, chain := range expr.Reads(input, "vars") {
			if slices.Contains(vars, chain[0]) {
				continue
			}
			msg := fmt.Sprintf("vars.%s is not defined by the workflow, so it is nil unless --var gives it", chain[0])
			if slices.Contains(later, chain[0]) {
				msg = fmt.Sprintf("vars.%s is not defined yet: a step's vars read only those of the workflow, so it is nil unless --var gives it", chain[0])
			} else {
				msg += suggest.Hint(chain[0], vars)
			}
			add(SeverityWarning, "%s", msg)
		}
	}

	forStrings("vars", c.wf.Vars, func(path, s string) {
		for _, input := range expr.TemplateExprs(s) {
			read(path, "", "vars", input, nil, workflowVars, nil)
		}
	})
	c.eachExpr(func(e exprAt) {
		names, vars := stepNames, workflowVars
		var later []string
		if e.step < 0 {
			if e.field == "name" {
				names = nil
			}
		} else {
			vars, later = stepVars(workflowVars, c.wf.Jobs[e.job].Steps[e.step], e.field)
		}
		read(e.path, e.where, e.field, e.input, names, vars, later)
	})
	for i, job := range c.wf.Jobs {
		if job.Repeat == nil {
			continue
		}
		for _, t := range job.Repeat.tests() {
			read(fmt.Sprintf("jobs[%d].repeat.%s", i, t.key), c.jobWhere(i), "repeat."+t.key, t.test, repeatNames, workflowVars, nil)
		}
	}
}

// checkResults checks what the expressions and templates of each step read
// under res and req. Those evaluated before the step runs, such as its
// skipif, read them empty, which is an error. Those evaluated after, its
// test, echo and outputs, may read only the fields the step's action
// returns, as it declares them: a field it never returns reads nil, which
// is an error for a built-in action, and a warning for an external one,
// whose action.yml says what it returns without Probe knowing it. Only the
// top level is checked, as what is under a field, such as a body, is data.
func (c *checker) checkResults() {
	add := func(severity string, e exprAt, format string, args ...any) {
		c.addOnce(severity, e.path, e.where, "%s: "+format, append([]any{e.field}, args...)...)
	}
	c.eachExpr(func(e exprAt) {
		if e.step < 0 {
			return
		}
		st := c.wf.Jobs[e.job].Steps[e.step]
		if !readsResult(e.field) {
			for _, name := range expr.Names(e.input) {
				if name == "res" || name == "req" {
					add(SeverityError, e, "%s is empty here, as the step has not run yet when its %s is evaluated; %s is read by the step's test, echo and outputs", name, e.field, name)
				}
			}
			return
		}
		fields, ok := c.resultFields(st)
		if !ok {
			return
		}
		severity := SeverityError
		if actionref.IsExternal(st.Uses) {
			severity = SeverityWarning
		}
		for _, name := range []string{"req", "res"} {
			declared, checked := fields[name]
			if !checked {
				continue
			}
			for _, chain := range expr.Reads(e.input, name) {
				if slices.Contains(declared, chain[0]) {
					continue
				}
				msg := fmt.Sprintf("%s.%s is not a field the %s action returns", name, chain[0], st.Uses)
				// Among the few fields of a res, one two edits away, as
				// code is from body, is as likely another as the one meant.
				if s := suggest.Within(chain[0], declared, 1); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				}
				msg += fmt.Sprintf("; its %s has %s", name, listNames(declared))
				if name == "res" && chain[0] == "status" {
					msg += "; the step's own status is read as status"
				}
				add(severity, e, "%s", msg)
			}
		}
	})
}

// resultFields returns the fields of req and of res that the action of st
// returns, by req and res, leaving out either one its action does not
// declare, and false when what st reads of neither is checked: for an
// action that declares nothing, and for one that returns the keys of with,
// for a step whose with has a key that holds a template, which names its
// key only when it runs.
func (c *checker) resultFields(st *Step) (map[string][]string, bool) {
	keys := slices.Collect(maps.Keys(st.With))
	if slices.Contains(c.opts.WithFields, st.Uses) {
		if slices.ContainsFunc(keys, expr.HasTemplate) {
			return nil, false
		}
	} else {
		keys = nil
	}
	out := map[string][]string{}
	for name, declared := range map[string]map[string][]string{"req": c.opts.ReqFields, "res": c.resFields} {
		fields, ok := declared[st.Uses]
		if !ok {
			continue
		}
		fields = append(slices.Clone(fields), keys...)
		if name == "res" {
			fields = addedResFields(fields)
		}
		slices.Sort(fields)
		out[name] = slices.Compact(fields)
	}
	return out, len(out) > 0
}

// nameHint returns what name, which is not among names, was likely meant
// as, to follow what names it, or an empty string.
func nameHint(name string, names []string) string {
	step := slices.Equal(names, stepNames)
	hint := suggest.Hint(name, names)
	switch {
	case expr.NameFor(name) != "":
		return fmt.Sprintf("; did you mean %s?", expr.NameFor(name))
	case hint != "":
		return hint
	case step && name == "steps":
		return "; what an earlier step publishes is read under outputs"
	case step && name == strings.ToUpper(name) && strings.Trim(name, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "":
		lower := strings.ToLower(name)
		return fmt.Sprintf("; the environment is read only by the workflow's vars, as %s: \"{{%s}}\", which a step reads as vars.%s", lower, name, lower)
	}
	return ""
}

// varNames returns the names of vars, sorted, or nil when a name holds a
// template, so that the names are known only as the workflow runs.
func varNames(vars map[string]any) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		if expr.HasTemplate(name) {
			return nil
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// stepVars returns the names of the vars that field of a step reads, as a
// run gives them: the step's vars are evaluated against those of the
// workflow alone; its iteration, when a template gives it, against those
// and the step's own; and the rest against those, and the keys of the entry
// of the iteration it runs with. vars is nil when the names are not known
// before the step runs, as the keys of the entries a template gives are
// not. later are the names the step defines that field does not read.
func stepVars(workflow []string, st *Step, field string) (vars, later []string) {
	own := varNames(st.Vars)
	if workflow == nil || (own == nil && len(st.Vars) > 0) {
		return nil, nil
	}
	for _, entry := range st.Iteration.Entries {
		for name := range entry {
			own = append(own, name)
		}
	}
	switch {
	case field == "vars":
		return workflow, own
	case field != "iteration" && st.Iteration.Expr != "":
		return nil, nil
	}
	names := append(slices.Clone(workflow), own...)
	slices.Sort(names)
	return slices.Compact(names), nil
}
