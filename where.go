package probe

import "fmt"

// jobWhere names the job at index i of jobs.
func jobWhere(jobs []Job, i int) string {
	return fmt.Sprintf("job %d %q", i, jobs[i].Name)
}

// stepWhere names st, the step at index i of the job job names.
func stepWhere(job string, i int, st *Step) string {
	if st == nil || st.Name == "" {
		return fmt.Sprintf("%s, step %d", job, i)
	}
	return fmt.Sprintf("%s, step %d %q", job, i, st.Name)
}

// jobWhere names the job at index i of the workflow checked.
func (c *checker) jobWhere(i int) string {
	return jobWhere(c.wf.Jobs, i)
}

// stepWhere names the step at index j of the job at index i of the
// workflow checked.
func (c *checker) stepWhere(i, j int) string {
	return stepWhere(c.jobWhere(i), j, c.wf.Jobs[i].Steps[j])
}
