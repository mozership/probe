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
			id := st.ID
			if id == "" {
				id = defaultStepID(j)
			}
			switch {
			case strings.TrimSpace(st.Test) == "":
				// The hello action does nothing that could go wrong, so
				// a step of it, which is there for its echo or to publish
				// what it is given, has nothing to check.
				if st.Uses != "hello" && !c.checkedByContract(st.Uses, st.With) && !tested[id] {
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

// The fields of a step that the tests of a repeated job read under steps,
// those that the test against the run compared to reads under
// baseline.steps, and those of their rt.
var (
	repeatRunFields   = []string{"runs", "success", "failure", "skipped", "success_rate", "rt"}
	baselineRunFields = []string{"runs", "success", "success_rate", "rt"}
	repeatRTFields    = []string{"min", "mean", "p50", "p90", "p95", "p99", "max"}
)

// checkRepeatTests checks that the tests of a repeated job read steps of the
// job by their ids, and fields that they have; that only the test against
// the run compared to reads it, under baseline.steps; and that this test
// reads it.
func (c *checker) checkRepeatTests() {
	for i, job := range c.wf.Jobs {
		if job.Repeat == nil {
			continue
		}
		ids := make([]string, 0, len(job.Steps))
		for j, st := range job.Steps {
			id := st.ID
			if id == "" {
				id = defaultStepID(j)
			}
			ids = append(ids, id)
		}
		for _, t := range []struct{ key, test string }{
			{"test", job.Repeat.Test},
			{"baseline_test", job.Repeat.BaselineTest},
		} {
			if strings.TrimSpace(t.test) == "" || expr.Parse(t.test) != nil {
				continue
			}
			key := "repeat." + t.key
			line := c.line(fmt.Sprintf("jobs[%d].%s", i, key))
			// A chain is read with each of its prefixes, so the same finding
			// comes from several of them.
			seen := map[string]bool{}
			add := func(severity, msg string) {
				if msg != "" && !seen[msg] {
					seen[msg] = true
					c.add(severity, line, c.jobWhere(i), "%s: %s", key, msg)
				}
			}
			for _, chain := range expr.Reads(t.test, "steps") {
				add(SeverityError, repeatChainProblem("steps", chain, ids, repeatRunFields))
			}
			baseline := expr.Reads(t.test, "baseline")
			byKey := expr.ReadsByVariableKey(t.test, "baseline")
			// A test against the run compared to that reads it by a key
			// known only when it runs is refused as the workflow is loaded;
			// see Repeat.Validate.
			switch {
			case t.key == "test" && len(baseline)+len(byKey) > 0:
				add(SeverityError, "baseline is read only by repeat.baseline_test")
			case t.key == "test":
			case len(baseline) == 0 && len(byKey) == 0:
				add(SeverityWarning, "it reads nothing under baseline, so it can go in repeat.test")
			}
			for _, chain := range baseline {
				if t.key != "baseline_test" {
					break
				}
				if chain[0] != "steps" {
					add(SeverityError, fmt.Sprintf("baseline has no %s; it has steps", chain[0]))
					continue
				}
				if len(chain) > 1 {
					add(SeverityError, repeatChainProblem("baseline.steps", chain[1:], ids, baselineRunFields))
				}
			}
		}
	}
}

// repeatChainProblem returns what is wrong with chain, read by a test of a
// repeated job under prefix, given the ids of the steps of the job and the
// fields that a step has there; empty when nothing is.
func repeatChainProblem(prefix string, chain, ids, fields []string) string {
	switch {
	case !slices.Contains(ids, chain[0]):
		return fmt.Sprintf("%s.%s is not a step of the job; its steps are %s",
			prefix, chain[0], strings.Join(ids, ", "))
	case len(chain) > 1 && !slices.Contains(fields, chain[1]):
		return fmt.Sprintf("%s.%s has no %s; it has %s",
			prefix, chain[0], chain[1], strings.Join(fields, ", "))
	case len(chain) > 2 && chain[1] != "rt":
		return fmt.Sprintf("%s.%s.%s is a number, which has no %s",
			prefix, chain[0], chain[1], chain[2])
	case len(chain) > 2 && !slices.Contains(repeatRTFields, chain[2]):
		return fmt.Sprintf("%s.%s.rt has no %s; it has %s",
			prefix, chain[0], chain[2], strings.Join(repeatRTFields, ", "))
	case len(chain) > 3:
		return fmt.Sprintf("%s.%s.rt.%s is a number, which has no %s",
			prefix, chain[0], chain[2], chain[3])
	}
	return ""
}
