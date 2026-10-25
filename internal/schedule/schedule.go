// Package schedule tells which jobs of a run can start, from the jobs each
// one needs and how those ended. It knows a job by its ID, its needs and
// whether it always runs, and nothing of what running one is.
package schedule

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mozership/probe/dag"
)

// Job is what the scheduler knows of a job.
type Job struct {
	ID string
	// Needs are the IDs of the jobs this one waits for.
	Needs []string
	// Always is whether the job runs once the jobs it needs are over,
	// however they ended, rather than only when they all succeeded.
	Always bool
}

// status is where a job is in the run.
type status int

const (
	pending status = iota
	running
	// finished is a job that ran, whether or not it succeeded.
	finished
	// skipped is a job that is not run, as a job it needs did not
	// succeed. The jobs that need it are skipped in turn.
	skipped
)

// Scheduler tells which jobs can start. It is safe for use by goroutines
// at once.
type Scheduler struct {
	jobs map[string]Job
	// order is the IDs of the jobs in the order they were added, which is
	// the order of the workflow, for what is told of them to be the same
	// each time.
	order  []string
	status map[string]status
	// results are whether each job that is over succeeded, which the jobs
	// that need it wait for; a skipped job did not.
	results map[string]bool
	mutex   sync.RWMutex
}

// New returns a scheduler of no jobs.
func New() *Scheduler {
	return &Scheduler{
		jobs:    make(map[string]Job),
		status:  make(map[string]status),
		results: make(map[string]bool),
	}
}

// Add adds job, pending. A job without an ID, or with the ID of one added
// already, is refused.
func (s *Scheduler) Add(job Job) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if job.ID == "" {
		return errors.New("a job has no ID")
	}
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("duplicate job ID: %s", job.ID)
	}

	job.Needs = slices.Clone(job.Needs)
	s.jobs[job.ID] = job
	s.order = append(s.order, job.ID)
	s.status[job.ID] = pending
	return nil
}

// Validate refuses a need that is no job, and needs in a cycle. The jobs
// are looked at in the order they were added.
func (s *Scheduler) Validate() error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for _, id := range s.order {
		for _, dep := range s.jobs[id].Needs {
			if _, exists := s.jobs[dep]; !exists {
				return fmt.Errorf("job '%s' depends on non-existent job '%s'", id, dep)
			}
		}
	}
	return s.checkCycleLocked()
}

// CheckCycle refuses jobs whose needs are in a cycle. A need that is no job
// is not refused.
func (s *Scheduler) CheckCycle() error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.checkCycleLocked()
}

// checkCycleLocked is CheckCycle for a caller that holds s.mutex. The jobs
// are looked at in the order they were added, so that the cycle is told
// from the first job of it in the workflow.
func (s *Scheduler) checkCycleLocked() error {
	needs := func(id string) []string {
		return s.jobs[id].Needs
	}
	if cycle := dag.DetectCycleFn(s.order, needs); cycle != nil {
		return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

// Start marks the job of id as running.
func (s *Scheduler) Start(id string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.status[id] = running
}

// Finish marks the job of id as over, and whether it succeeded. The jobs
// that need it can start from then on, so it is called once, when nothing
// more of the job is to be done.
func (s *Scheduler) Finish(id string, success bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.status[id] = finished
	s.results[id] = success
}

// Runnable returns the pending jobs that can start, in the order they were
// added: those whose needs all succeeded, and those that always run whose
// needs are all over.
func (s *Scheduler) Runnable() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var runnable []string
	for _, id := range s.order {
		if s.canStartLocked(id) {
			runnable = append(runnable, id)
		}
	}
	return runnable
}

// canStartLocked reports whether the job of id can start. Callers must
// already hold s.mutex (read or write): taking RLock again from Runnable
// would deadlock when a writer is waiting on the same mutex.
func (s *Scheduler) canStartLocked(id string) bool {
	if s.status[id] != pending {
		return false
	}
	job := s.jobs[id]
	for _, dep := range job.Needs {
		if job.Always {
			if !s.isOverLocked(dep) {
				return false
			}
			continue
		}
		if !s.succeededLocked(dep) {
			return false
		}
	}
	return true
}

// isOverLocked reports whether the job of id is over: it finished, whether
// or not it succeeded, or it was skipped. Callers must already hold s.mutex.
func (s *Scheduler) isOverLocked(id string) bool {
	st := s.status[id]
	return st == finished || st == skipped
}

// succeededLocked reports whether the job of id finished and succeeded.
// Callers must already hold s.mutex.
func (s *Scheduler) succeededLocked(id string) bool {
	return s.status[id] == finished && s.results[id]
}

// Pending returns the jobs that have not started, in the order they were
// added.
func (s *Scheduler) Pending() []string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var ids []string
	for _, id := range s.order {
		if s.status[id] == pending {
			ids = append(ids, id)
		}
	}
	return ids
}

// SkipBlocked skips the pending jobs that a job they need did not succeed
// in, except those that always run, and returns them in the order they were
// added. A skipped job skips the jobs that need it in turn, however deep the
// chain of needs is.
func (s *Scheduler) SkipBlocked() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var skippedIDs []string
	for {
		changed := false
		for _, id := range s.order {
			job := s.jobs[id]
			// A job that always runs is not held back by a need that failed:
			// it runs once they are all over.
			if s.status[id] != pending || job.Always {
				continue
			}
			if slices.ContainsFunc(job.Needs, s.failedLocked) {
				s.status[id] = skipped
				s.results[id] = false
				skippedIDs = append(skippedIDs, id)
				changed = true
			}
		}
		if !changed {
			return skippedIDs
		}
	}
}

// failedLocked reports whether the job of id is over and did not succeed.
// Callers must already hold s.mutex.
func (s *Scheduler) failedLocked(id string) bool {
	return s.status[id] == skipped || (s.status[id] == finished && !s.results[id])
}
