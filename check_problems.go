package probe

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mozership/probe/internal/expr"
)

// reportAliasProblems tells what is wrong with the names the workflow's
// actions give, each on its line.
func (c *checker) reportAliasProblems(problems []aliasProblem) {
	for _, p := range problems {
		if p.name != "" {
			c.add(SeverityError, c.line("actions."+p.name), "", "%s", firstLine(p.msg))
			continue
		}
		if p.job == workflowDefaults {
			c.add(SeverityError, c.line("defaults."+p.action), "", "%s", firstLine(p.msg))
			continue
		}
		c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].defaults.%s", p.job, p.action)), c.jobWhere(p.job), "%s", firstLine(p.msg))
	}
}

// reportJobProblems tells what keeps the jobs from running as they are
// written, which a run is refused for as the workflow is loaded.
func (c *checker) reportJobProblems(problems []problem) {
	for _, p := range problems {
		c.reportProblem(p)
	}
}

// checkVarsTemplates tells the templates of the workflow's vars that do not
// parse. A load does not refuse them, as it does the jobs' problems: they
// are found as the vars are evaluated, when the run starts.
func (c *checker) checkVarsTemplates() {
	for _, p := range templateProblems("vars", "", "vars", c.wf.Vars) {
		c.reportProblem(p)
	}
}

// reportProblem tells p by the first line of its error, which says where in
// the expression it is, and how the expression is likely meant to be
// written where it is written as another language would write it.
func (c *checker) reportProblem(p problem) {
	msg := firstLine(p.err.Error())
	if h, ok := errors.AsType[*expr.HintError](p.err); ok {
		msg += "; " + strings.Join(h.Hints, "; ")
	}
	c.add(SeverityError, c.line(p.path), p.where, "%s", msg)
}
