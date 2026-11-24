package check

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mozership/probe/internal/engine"
	"github.com/mozership/probe/internal/expr"
)

// reportAliasProblems tells what is wrong with the names the workflow's
// actions give, each on its line.
func (c *checker) reportAliasProblems(problems []engine.AliasProblem) {
	for _, p := range problems {
		if p.Name != "" {
			c.add(SeverityError, c.line("actions."+p.Name), "", "%s", engine.FirstLine(p.Msg))
			continue
		}
		if p.Job == engine.WorkflowDefaults {
			c.add(SeverityError, c.line("defaults."+p.Action), "", "%s", engine.FirstLine(p.Msg))
			continue
		}
		c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].defaults.%s", p.Job, p.Action)), c.jobWhere(p.Job), "%s", engine.FirstLine(p.Msg))
	}
}

// reportJobProblems tells what keeps the jobs from running as they are
// written, which a run is refused for as the workflow is loaded.
func (c *checker) reportJobProblems(problems []engine.Problem) {
	for _, p := range problems {
		c.reportProblem(p)
	}
}

// checkVarsTemplates tells the templates of the workflow's vars that do not
// parse. A load does not refuse them, as it does the jobs' problems: they
// are found as the vars are evaluated, when the run starts.
func (c *checker) checkVarsTemplates() {
	for _, p := range engine.TemplateProblems("vars", "", "vars", c.wf.Vars) {
		c.reportProblem(p)
	}
}

// reportProblem tells p by the first line of its error, which says where in
// the expression it is, and how the expression is likely meant to be
// written where it is written as another language would write it.
func (c *checker) reportProblem(p engine.Problem) {
	msg := engine.FirstLine(p.Err.Error())
	if h, ok := errors.AsType[*expr.HintError](p.Err); ok {
		msg += "; " + strings.Join(h.Hints, "; ")
	}
	c.add(SeverityError, c.line(p.Path), p.Where, "%s", msg)
}
