package probe

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mozership/probe/internal/expr"
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
			if st == nil {
				continue
			}
			ids = append(ids, st.idAt(j))
		}
		for _, t := range job.Repeat.tests() {
			if strings.TrimSpace(t.test) == "" || expr.Parse(t.test) != nil {
				continue
			}
			key := "repeat." + t.key
			path := fmt.Sprintf("jobs[%d].%s", i, key)
			add := func(severity, msg string) {
				if msg != "" {
					c.addOnce(severity, path, c.jobWhere(i), "%s: %s", key, msg)
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
	case len(chain) > 2 && chain[1] != runFieldRT:
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
