package probe

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mozership/probe/internal/suggest"
)

func (c *checker) checkNeeds() {
	ids := map[string]bool{}
	for _, job := range c.wf.Jobs {
		ids[job.ID] = true
	}
	unknown := false
	for i, job := range c.wf.Jobs {
		for _, need := range job.Needs {
			if ids[need] {
				continue
			}
			unknown = true
			msg := fmt.Sprintf("needs: no job has the id %q", need)
			if s := suggest.Closest(need, slices.Sorted(maps.Keys(ids))); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].needs", i)), c.jobWhere(i), "%s", msg)
		}
	}
	if unknown {
		return
	}

	s := NewJobScheduler()
	for i := range c.wf.Jobs {
		if err := s.AddJob(&c.wf.Jobs[i]); err != nil {
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d]", i)), c.jobWhere(i), "%s", firstLine(err.Error()))
			return
		}
	}
	if err := s.ValidateDependencies(); err != nil {
		c.add(SeverityError, 0, "", "%s", firstLine(err.Error()))
	}
}
