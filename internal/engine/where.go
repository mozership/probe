package engine

import "fmt"

// JobWhere names the job at index i of jobs.
func JobWhere(jobs []Job, i int) string {
	return fmt.Sprintf("job %d %q", i, jobs[i].Name)
}

// StepWhere names st, the step at index i of the job job names.
func StepWhere(job string, i int, st *Step) string {
	if st == nil || st.Name == "" {
		return fmt.Sprintf("%s, step %d", job, i)
	}
	return fmt.Sprintf("%s, step %d %q", job, i, st.Name)
}
