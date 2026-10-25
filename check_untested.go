package probe

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mozership/probe/expr"
)

// checkTests warns of steps that nothing checks, and of tests that read
// nothing, which pass or fail whatever the step does.
func (c *checker) checkTests() {
	for i, job := range c.wf.Jobs {
		// A step whose runs the test of a repeated job reads is checked by
		// that test.
		var tested map[string]bool
		if job.Repeat != nil {
			tested = repeatReads(job.Repeat.Test, job.Repeat.BaselineTest)
		}
		for j, st := range job.Steps {
			path := fmt.Sprintf("jobs[%d].steps[%d]", i, j)
			id := st.idAt(j)
			switch {
			case strings.TrimSpace(st.Test) == "":
				// A step of an action that does nothing that could go
				// wrong, which is there for its echo or to publish what
				// it is given, has nothing to check.
				if !slices.Contains(c.opts.NeedsNoCheck, st.Uses) && !c.checkedByContract(st.Uses, st.With) && !tested[id] {
					c.add(SeverityWarning, c.line(path), c.stepWhere(i, j), "nothing checks this step: it has no test")
				}
			case expr.Parse(st.Test) == nil && expr.ReadsNothing(st.Test):
				c.add(SeverityWarning, c.line(path+".test"), c.stepWhere(i, j), "test reads nothing from the step, so it gives the same result whatever the step does")
			}
		}
	}
}

// checkedByContract reports whether with asks the action uses to check the
// step against a contract, as CheckOptions.Contracts says. The same key
// given to another action asks it nothing of the kind.
func (c *checker) checkedByContract(uses string, with map[string]any) bool {
	for _, key := range c.opts.Contracts[uses] {
		if _, ok := with[key].(map[string]any); ok {
			return true
		}
	}
	return false
}
