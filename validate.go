package probe

import "fmt"

// The workflow is refused for what keeps it from running at these points,
// each in the order written here, and the first error stops it:
//
//   - Load: validateIDs, then validate. What validate returns as problems,
//     probe check tells one by one and Load refuses the workflow for.
//   - Start, in Workflow.prepare: validate, which a workflow made in code
//     has not been through; checkAllJobs and selectJobs when the run is
//     narrowed to some jobs; validatedScheduler; then resolveExternalActions.
//   - Start, in workflowRun.execute, once the vars are evaluated:
//     checkEmbeddedJobs, whose paths may read the vars.
//
// What each of them checks is its own, and the error texts and their order
// are what probe check and the tests pin.

// validate refuses the workflow for its repeat and retry settings and for a
// job whose ID it cannot be given, giving each job without an ID its own,
// and returns what keeps a job from running as it is written, as problems.
// Load and Start both go through it, in this order.
func (w *Workflow) validate() ([]problem, error) {
	if err := w.validateRepeatLimits(); err != nil {
		return nil, err
	}
	// The result, the report order and the scheduler all index jobs by ID,
	// so every job has one before anything keys off them. A loaded workflow
	// has them already; one made in code is given them here. A step
	// without an ID is left without one, so that what needs one, such as
	// its outputs, is refused: it is read as defaultStepID where it is read
	// by where it is written.
	if err := w.assignJobIDs(); err != nil {
		return nil, err
	}
	return w.problems(), nil
}

// validateIDs refuses the workflow for a job ID that two jobs have, and for
// a step ID that two steps have, in the same job or not. Load checks it
// before validate gives the jobs without an ID theirs.
func (w *Workflow) validateIDs() error {
	jobIDs := make(map[string]int)           // jobID -> jobIndex
	globalStepIDs := make(map[string]string) // stepID -> "job[index].step[index]"

	for jobIdx, job := range w.Jobs {
		// Check for duplicate job IDs
		if job.ID != "" {
			if existingIdx, exists := jobIDs[job.ID]; exists {
				return NewConfigurationError("duplicate_job_id",
					fmt.Sprintf("duplicate job ID '%s' found in job %d (already used in job %d)",
						job.ID, jobIdx, existingIdx), nil)
			}
			jobIDs[job.ID] = jobIdx
		}

		// Check for duplicate step IDs across all jobs
		for stepIdx, step := range job.Steps {
			if step != nil && step.ID != "" {
				stepLocation := fmt.Sprintf("job[%d].step[%d]", jobIdx, stepIdx)
				if existingLocation, exists := globalStepIDs[step.ID]; exists {
					return NewConfigurationError("duplicate_step_id",
						fmt.Sprintf("duplicate step ID '%s' found in %s (already used in %s)",
							step.ID, stepLocation, existingLocation), nil)
				}
				globalStepIDs[step.ID] = stepLocation
			}
		}
	}

	return nil
}

// validateRepeatLimits validates repeat count and max_attempts against configured limits
//
// Start checks them as well, for a workflow made in code rather than loaded,
// since an async repeat without an interval cannot run.
func (w *Workflow) validateRepeatLimits() error {
	for jobIdx, job := range w.Jobs {
		// Validate repeat configuration
		if job.Repeat != nil {
			if err := job.Repeat.Validate(); err != nil {
				return NewConfigurationError("invalid_repeat",
					fmt.Sprintf("job %d (%s): %v", jobIdx, job.Name, err), nil)
			}
			if err := job.validateRepeatIDs(); err != nil {
				return NewConfigurationError("invalid_repeat",
					fmt.Sprintf("job %d (%s): %v", jobIdx, job.Name, err), nil)
			}
		}

		// Validate retry configuration for each step
		for stepIdx, step := range job.Steps {
			if step != nil && step.Retry != nil {
				if err := step.Retry.Validate(); err != nil {
					return NewConfigurationError("invalid_retry",
						fmt.Sprintf("job %d (%s), step %d: %v", jobIdx, job.Name, stepIdx, err), nil)
				}
			}
		}
	}
	return nil
}

// checkAllJobs refuses the jobs for what a run of them all would refuse
// them for before any runs, and a run of some of them would not see in the
// jobs it leaves out: a need that is no job, needs in a cycle, and a uses
// that names no action of Probe or external one. The external actions are
// not fetched, which a run does for the jobs that run alone.
func (w *Workflow) checkAllJobs(builtin []string) error {
	if _, err := validatedScheduler(w.Jobs); err != nil {
		return err
	}
	for i := range w.Jobs {
		if err := checkBuiltinUses(&w.Jobs[i], builtin); err != nil {
			return err
		}
	}
	return nil
}
