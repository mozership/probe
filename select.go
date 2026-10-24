package probe

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mozership/probe/internal/suggest"
	"github.com/mozership/probe/report"
)

// Why a job runs in a run narrowed to some of the jobs of a workflow.
const (
	selectedJob = iota + 1
	// neededJob is a job a job that runs needs, whose outputs it may read.
	neededJob
	// alwaysJob is a job that always runs after a job that runs, as one
	// that removes what that job made does.
	alwaysJob
)

// jobSelection is what selectJobs narrowed a workflow's jobs to.
type jobSelection struct {
	// total is how many jobs the workflow has.
	total int
	// selected, needed and always are the jobs that run, each by its id,
	// by why they run.
	selected, needed, always []string
}

// describe says which jobs a run narrowed to some of them runs, each by the
// name name gives its id, and why each that was not selected runs too.
func (s *jobSelection) describe(name func(id string) string) string {
	var names []string
	for _, id := range s.selected {
		names = append(names, name(id))
	}
	for _, id := range s.needed {
		names = append(names, name(id)+" (needed)")
	}
	for _, id := range s.always {
		names = append(names, name(id)+" (always)")
	}
	return fmt.Sprintf("Running %d of %d jobs: %s", len(names), s.total, strings.Join(names, ", "))
}

// report returns the selection as the report of the run records it.
func (s *jobSelection) report() *report.Selection {
	return &report.Selection{Jobs: slices.Clone(s.selected), Total: s.total}
}

// selectJobs returns the jobs of jobs a run selecting those names names
// runs, in the order they are written, and why each runs. A name is the id
// of a job, or, when no job has that id, the name of a job; a name that is
// no job's, or that several jobs have, is an error.
//
// A selected job runs with the jobs it needs, and those they need, since it
// may read what they publish. A job that always runs runs as well when a
// job it needs runs, for it may be what removes what that job made; it
// waits only for those of the jobs it needs that run. No other job runs.
// The jobs must have their IDs.
func selectJobs(jobs []Job, names []string) ([]Job, *jobSelection, error) {
	byID := make(map[string]int, len(jobs))
	for i, j := range jobs {
		byID[j.ID] = i
	}

	why := make([]int, len(jobs))
	var queue []int
	for _, name := range names {
		i, err := findJob(jobs, byID, name)
		if err != nil {
			return nil, nil, err
		}
		if why[i] == 0 {
			queue = append(queue, i)
		}
		why[i] = selectedJob
	}

	// The jobs the selected ones need, however far down.
	for len(queue) > 0 {
		i := queue[0]
		queue = queue[1:]
		for _, dep := range jobs[i].Needs {
			if k, ok := byID[dep]; ok && why[k] == 0 {
				why[k] = neededJob
				queue = append(queue, k)
			}
		}
	}

	// The jobs that always run after one that runs. One may follow another
	// such job, so they are looked for until no more are found. They do not
	// bring the other jobs they need, which would run what was not asked
	// for.
	for found := true; found; {
		found = false
		for i, j := range jobs {
			if why[i] != 0 || !j.Always {
				continue
			}
			if slices.ContainsFunc(j.Needs, func(dep string) bool {
				k, ok := byID[dep]
				return ok && why[k] != 0
			}) {
				why[i] = alwaysJob
				found = true
			}
		}
	}

	sel := &jobSelection{total: len(jobs)}
	var out []Job
	for i, j := range jobs {
		switch why[i] {
		case 0:
			continue
		case selectedJob:
			sel.selected = append(sel.selected, j.ID)
		case neededJob:
			sel.needed = append(sel.needed, j.ID)
		case alwaysJob:
			sel.always = append(sel.always, j.ID)
			// It waits for the jobs it needs that run. One that is not in
			// the workflow is kept, for the run to be refused for it.
			j.Needs = slices.DeleteFunc(slices.Clone(j.Needs), func(dep string) bool {
				k, ok := byID[dep]
				return ok && why[k] == 0
			})
		}
		out = append(out, j)
	}
	return out, sel, nil
}

// findJob returns the index of the job of jobs whose id is name, or, when
// there is none, of the one job whose name is name.
func findJob(jobs []Job, byID map[string]int, name string) (int, error) {
	if i, ok := byID[name]; ok {
		return i, nil
	}
	var found []int
	for i, j := range jobs {
		if j.Name == name {
			found = append(found, i)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		known := make([]string, 0, 2*len(jobs))
		for _, j := range jobs {
			known = append(known, j.ID, j.Name)
		}
		msg := fmt.Sprintf("no job has the id or name %q", name)
		if s := suggest.Closest(name, known); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		} else if slices.ContainsFunc(jobs, func(j Job) bool { return strings.Contains(j.Name, "{{") }) {
			// The vars are not evaluated yet, and a name may read more.
			msg += "; a job whose name has a template is selected by its id"
		}
		return 0, errors.New(msg)
	default:
		ids := make([]string, len(found))
		for k, i := range found {
			ids[k] = jobs[i].ID
		}
		return 0, fmt.Errorf("%d jobs are named %q; select one by its id: %s", len(found), name, strings.Join(ids, ", "))
	}
}
