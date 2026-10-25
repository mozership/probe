package probe

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/mozership/probe/dag"
)

// JobStatus is where a job is in the run, for the scheduler to tell which
// jobs can start. How a job ended is its JobResult's Status.
type JobStatus int

const (
	JobPending JobStatus = iota
	JobRunning
	// JobCompleted is a job that ran, whether or not it succeeded.
	JobCompleted
	// JobSkipped is a job that is not run, as a job it needs did not
	// succeed. The jobs that need it are skipped in turn.
	JobSkipped
)

type JobScheduler struct {
	jobs map[string]*Job
	// order is the IDs of the jobs in the order they were added, which is
	// the order of the workflow, for what is told of them to be the same
	// each time.
	order  []string
	status map[string]JobStatus
	// results are whether each job that is over succeeded, which the jobs
	// that need it wait for; a skipped job did not.
	results        map[string]bool
	repeatCounters map[string]int // Track repeat execution count
	repeatTargets  map[string]int // Target repeat count
	mutex          sync.RWMutex
}

func NewJobScheduler() *JobScheduler {
	return &JobScheduler{
		jobs:           make(map[string]*Job),
		status:         make(map[string]JobStatus),
		results:        make(map[string]bool),
		repeatCounters: make(map[string]int),
		repeatTargets:  make(map[string]int),
	}
}

func (js *JobScheduler) AddJob(job *Job) error {
	js.mutex.Lock()
	defer js.mutex.Unlock()

	// The jobs of a workflow are given their IDs before they are added,
	// by assignJobIDs.
	if job.ID == "" {
		return fmt.Errorf("job %q has no ID", job.Name)
	}
	if _, exists := js.jobs[job.ID]; exists {
		return fmt.Errorf("duplicate job ID: %s", job.ID)
	}

	js.jobs[job.ID] = job
	js.order = append(js.order, job.ID)
	js.status[job.ID] = JobPending

	// Set repeat targets
	if job.Repeat != nil {
		js.repeatTargets[job.ID] = job.Repeat.Runs()
		js.repeatCounters[job.ID] = 0
	} else {
		js.repeatTargets[job.ID] = 1 // No repeat = run once
		js.repeatCounters[job.ID] = 0
	}

	return nil
}

func (js *JobScheduler) ValidateDependencies() error {
	js.mutex.RLock()
	defer js.mutex.RUnlock()

	// Check if all dependencies exist
	for jobID, job := range js.jobs {
		for _, dep := range job.Needs {
			if _, exists := js.jobs[dep]; !exists {
				return fmt.Errorf("job '%s' depends on non-existent job '%s'", jobID, dep)
			}
		}
	}

	// Check for circular dependencies
	return js.checkCircularDependencies()
}

// checkCircularDependencies refuses jobs whose needs are in a cycle. The
// jobs are looked at in the order they were added, so that the cycle is
// told from the first job of it in the workflow.
func (js *JobScheduler) checkCircularDependencies() error {
	// Define dependency getter function
	getDeps := func(jobID string) []string {
		if job, ok := js.jobs[jobID]; ok {
			return job.Needs
		}
		return nil
	}

	// Use dag package to detect cycles
	cycle := dag.DetectCycleFn(js.order, getDeps)
	if cycle != nil {
		return fmt.Errorf("circular dependency detected: %s", strings.Join(cycle, " -> "))
	}

	return nil
}

// canRunJobLocked reports whether the job jobID, one of the scheduler's,
// can start. Callers must already hold js.mutex (read or write): taking
// RLock again from GetRunnableJobs would deadlock when a writer is waiting
// on the same mutex.
func (js *JobScheduler) canRunJobLocked(jobID string) bool {
	job := js.jobs[jobID]
	if js.status[jobID] != JobPending {
		return false
	}

	// Check if all dependencies are fully completed (all repeats done). A
	// job that always runs waits for them to be over, however they ended.
	for _, dep := range job.Needs {
		if job.Always {
			if !js.isJobOverLocked(dep) {
				return false
			}
			continue
		}
		if !js.isJobFullyCompletedLocked(dep) {
			return false
		}
	}

	return true
}

// isJobOverLocked is the lock-free check for whether a job is over: it ran
// all of its repeat executions, whether or not they succeeded, or it was
// skipped because a job it needs did not succeed. Callers must
// already hold js.mutex.
func (js *JobScheduler) isJobOverLocked(jobID string) bool {
	switch js.status[jobID] {
	case JobSkipped:
		return true
	case JobCompleted:
		return js.repeatCounters[jobID] >= js.repeatTargets[jobID]
	}
	return false
}

// isJobFullyCompletedLocked is the lock-free check for whether a job has
// finished all of its repeat executions. Callers must already hold js.mutex.
func (js *JobScheduler) isJobFullyCompletedLocked(jobID string) bool {
	if js.status[jobID] != JobCompleted {
		return false
	}

	// Check if all repeat executions are done
	target := js.repeatTargets[jobID]
	counter := js.repeatCounters[jobID]

	return counter >= target && js.results[jobID]
}

// StartJob marks the job of jobID as running.
func (js *JobScheduler) StartJob(jobID string) {
	js.mutex.Lock()
	defer js.mutex.Unlock()

	js.status[jobID] = JobRunning
}

// FinishJob marks the job of jobID as over, and whether it succeeded.
func (js *JobScheduler) FinishJob(jobID string, success bool) {
	js.mutex.Lock()
	defer js.mutex.Unlock()

	js.status[jobID] = JobCompleted
	js.results[jobID] = success
}

// IncrementRepeatCounter increments the repeat counter for a job
func (js *JobScheduler) IncrementRepeatCounter(jobID string) {
	js.mutex.Lock()
	defer js.mutex.Unlock()

	js.repeatCounters[jobID]++
}

// ShouldRepeatJob checks if a job should be repeated
func (js *JobScheduler) ShouldRepeatJob(jobID string) bool {
	js.mutex.RLock()
	defer js.mutex.RUnlock()

	counter := js.repeatCounters[jobID]
	target := js.repeatTargets[jobID]

	return counter < target
}

// GetRepeatInfo returns current repeat counter and target for a job
func (js *JobScheduler) GetRepeatInfo(jobID string) (current, target int) {
	js.mutex.RLock()
	defer js.mutex.RUnlock()

	return js.repeatCounters[jobID], js.repeatTargets[jobID]
}

func (js *JobScheduler) GetRunnableJobs() []string {
	js.mutex.RLock()
	defer js.mutex.RUnlock()

	var runnable []string
	for jobID := range js.jobs {
		if js.canRunJobLocked(jobID) {
			runnable = append(runnable, jobID)
		}
	}

	return runnable
}

// PendingJobs returns the jobs that have not started, sorted.
func (js *JobScheduler) PendingJobs() []string {
	js.mutex.RLock()
	defer js.mutex.RUnlock()

	var pending []string
	for jobID, status := range js.status {
		if status == JobPending {
			pending = append(pending, jobID)
		}
	}
	slices.Sort(pending)
	return pending
}

// MarkJobsWithFailedDependencies skips the jobs a job they need did not
// succeed in, except those that always run, and returns them.
// It iterates until no more jobs are newly marked, propagating failures transitively
// through multi-level dependency chains.
func (js *JobScheduler) MarkJobsWithFailedDependencies() []string {
	js.mutex.Lock()
	defer js.mutex.Unlock()

	var skippedJobs []string

	for {
		changed := false
		for jobID, job := range js.jobs {
			if js.status[jobID] != JobPending {
				continue
			}

			// A job that always runs is not held back by a dependency that
			// failed: it runs once they are all over.
			if job.Always {
				continue
			}

			// Check if any dependency has failed
			hasFailedDependency := false
			for _, dep := range job.Needs {
				if js.status[dep] == JobSkipped || (js.status[dep] == JobCompleted && !js.results[dep]) {
					hasFailedDependency = true
					break
				}
			}

			if hasFailedDependency {
				js.status[jobID] = JobSkipped
				js.results[jobID] = false
				skippedJobs = append(skippedJobs, jobID)
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	return skippedJobs
}
