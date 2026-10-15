package probe

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Executor handles job execution with buffered output
type Executor struct {
	workflow *Workflow
	job      *Job
}

// NewExecutor creates a new job executor
func NewExecutor(w *Workflow, job *Job) *Executor {
	return &Executor{
		workflow: w,
		job:      job,
	}
}

// setJobID ensures the job has a valid ID, using Name if ID is empty
func (e *Executor) setJobID() {
	if e.job.ID == "" {
		e.job.ID = e.job.Name
	}
}

// Execute runs a job with buffered output
func (e *Executor) Execute(ctx JobContext) bool {
	e.setJobID()
	jobID := e.job.ID

	jr := ctx.Result.Jobs[jobID]

	jr.mutex.Lock()
	jr.StartTime = time.Now()
	jr.Status = "Running"
	jr.mutex.Unlock()

	// Use the existing buffered execution logic
	return e.executeWithBuffering(ctx)
}

// executeWithBuffering contains the buffered execution logic
func (e *Executor) executeWithBuffering(ctx JobContext) bool {
	ctx = e.setupContext(ctx)

	overallSuccess := e.executeJobRepeatLoop(ctx)

	if ctx.IsRepeating {
		e.appendRepeatStepResults(&ctx)
	}

	e.finalize(overallSuccess, ctx)

	return overallSuccess
}

// setupContext initializes the job context for buffered execution
func (e *Executor) setupContext(ctx JobContext) JobContext {
	jobID := e.job.ID
	_, total := ctx.JobScheduler.GetRepeatInfo(jobID)
	ctx.IsRepeating = total > 1
	if e.job.Repeat != nil {
		ctx.RepeatWarmup = e.job.Repeat.Warmup
	}
	ctx.RepeatTotal = total - ctx.RepeatWarmup
	ctx.StepCounters = make(map[int]StepRepeatCounter)
	return ctx
}

// executeJobRepeatLoop handles the main execution loop with repeat logic
func (e *Executor) executeJobRepeatLoop(ctx JobContext) bool {
	// Check if async execution is enabled
	if e.job.Repeat != nil && e.job.Repeat.Async {
		return e.executeJobRepeatLoopAsync(ctx)
	}

	// Synchronous execution (original behavior)
	jobID := e.job.ID
	overallSuccess := true

	for ctx.JobScheduler.ShouldRepeatJob(jobID) {
		current, _ := ctx.JobScheduler.GetRepeatInfo(jobID)
		ctx.RepeatCurrent = current

		// Execute single run
		err := e.job.Start(ctx)

		if err != nil && !ctx.warmingUp() {
			overallSuccess = false
			e.workflow.SetExitStatus(true)
		}

		ctx.JobScheduler.IncrementRepeatCounter(jobID)
		e.sleepBetweenRepeats(ctx)
	}

	return overallSuccess
}

// executeJobRepeatLoopAsync handles async execution loop with repeat logic
func (e *Executor) executeJobRepeatLoopAsync(ctx JobContext) bool {
	jobID := e.job.ID
	_, total := ctx.JobScheduler.GetRepeatInfo(jobID)

	var wg sync.WaitGroup
	var overallSuccess atomic.Bool
	overallSuccess.Store(true)

	interval := e.job.Repeat.Interval.Duration
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// The runs overlap, so each writes its outputs to a store of its own,
	// in front of the workflow's, and reads its own before anyone else's.
	// Once all are done, every run's outputs are published in the order of
	// the runs, so that a later run's value replaces an earlier one's and a
	// value only an earlier run published stays, as a job repeated in turn
	// leaves them.
	var runsMu sync.Mutex
	runs := make(map[int]*Outputs)

	for ctx.JobScheduler.ShouldRepeatJob(jobID) {
		current, _ := ctx.JobScheduler.GetRepeatInfo(jobID)

		// Each iteration gets its own Job/Step copy so the per-execution
		// fields mutated by Job.Start (Job.Name, Step.Expr, Step.ctx,
		// Step.Idx, Step.startedAt, Step.err, Step.retryAttempt) don't
		// race across goroutines.
		jobCopy := e.job.cloneForAsync()

		wg.Add(1)
		go func(repeatIndex int, j *Job) {
			defer wg.Done()

			// Create a copy of context for this goroutine
			execCtx := ctx
			execCtx.RepeatCurrent = repeatIndex
			if ctx.Outputs != nil {
				execCtx.Outputs = newRunOutputs(ctx.Outputs)
				runsMu.Lock()
				runs[repeatIndex] = execCtx.Outputs
				runsMu.Unlock()
			}

			// Execute single run
			err := j.Start(execCtx)

			if err != nil && !execCtx.warmingUp() {
				overallSuccess.Store(false)
				e.workflow.SetExitStatus(true)
			}
		}(current, jobCopy)

		ctx.JobScheduler.IncrementRepeatCounter(jobID)

		// Wait for interval before next execution (except for the last one)
		if current+1 < total {
			<-ticker.C
		}
	}

	// Wait for all goroutines to complete
	wg.Wait()

	order := make([]int, 0, len(runs))
	for i := range runs {
		order = append(order, i)
	}
	sort.Ints(order)
	for _, i := range order {
		// Any taken name was warned about when the run wrote it, so the same
		// warnings are not repeated here.
		_ = ctx.Outputs.publish(runs[i])
	}

	return overallSuccess.Load()
}

// sleepBetweenRepeats handles the interval sleep between job repetitions
func (e *Executor) sleepBetweenRepeats(ctx JobContext) {
	jobID := e.job.ID
	current, target := ctx.JobScheduler.GetRepeatInfo(jobID)
	if current < target && e.job.Repeat != nil {
		time.Sleep(e.job.Repeat.Interval.Duration)
	}
}

// finalize updates the final job status and marks it as completed
func (e *Executor) finalize(overallSuccess bool, ctx JobContext) {
	jobID := e.job.ID
	jr := ctx.Result.Jobs[jobID]
	jr.mutex.Lock()
	duration := time.Since(jr.StartTime)
	jr.EndTime = jr.StartTime.Add(duration)
	jr.Success = overallSuccess
	if overallSuccess {
		// Don't overwrite "skipped" status
		if jr.Status != "skipped" {
			jr.Status = "Completed"
		}
	} else {
		jr.Status = "Failed"
	}
	jr.mutex.Unlock()

	// Mark job as completed
	ctx.JobScheduler.SetJobStatus(jobID, JobCompleted, overallSuccess)

	// The job's report block is final now, so it can be streamed out.
	ctx.Result.notifyJobDone(jobID)
}

// appendRepeatStepResults appends the final results of repeat step executions to buffer.
// The counters are kept by where each step is written and which entry of
// its iteration it ran with (Step.counterKey), so that a run does not count
// for another step when an iteration comes to another number of entries in
// each run. They are read in the order of those keys, which is the order the
// steps ran in, and numbered from there as the steps of any job are.
func (e *Executor) appendRepeatStepResults(ctx *JobContext) {
	jobID := e.job.ID

	ctx.countersMu.Lock()
	indexes := make([]int, 0, len(ctx.StepCounters))
	for i := range ctx.StepCounters {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	counters := make([]StepRepeatCounter, len(indexes))
	for n, i := range indexes {
		counter := ctx.StepCounters[i]
		// Ensure RepeatTotal is set (in case it wasn't set earlier)
		if counter.RepeatTotal == 0 {
			counter.RepeatTotal = ctx.RepeatTotal
			ctx.StepCounters[i] = counter
		}
		counter.Warmup = ctx.RepeatWarmup
		counters[n] = counter
	}
	ctx.countersMu.Unlock()

	// Create StepResults for repeat steps and add them to Result
	for n := range indexes {
		counter := counters[n]
		hasTest := counter.Test != "" || counter.Checked

		// Determine status based on repeat counter results
		var status StatusType
		if counter.FailureCount == 0 && counter.SuccessCount == 0 && counter.SkippedCount > 0 {
			// Never run - skipped in every run, after a step that failed
			status = StatusSkipped
		} else if counter.FailureCount == 0 {
			// No failures - success
			status = StatusSuccess
		} else if counter.SuccessCount == 0 {
			// All failures - error
			status = StatusError
		} else {
			// Mixed results - warning
			status = StatusWarning
		}

		// Create StepResult with repeat counter
		stepResult := StepResult{
			Index:         n,
			Name:          counter.Name,
			Status:        status,
			HasTest:       hasTest,
			RepeatCounter: &counter,
			Test:          counter.Test,
			Contract:      counter.Contract,
		}
		if counter.Failure != nil {
			f := *counter.Failure
			// The runs the step was not run in are runs of the job too, as
			// the count in the report files has them.
			f.Message = fmt.Sprintf("%d of %d iterations failed; the first: %s",
				counter.FailureCount, counter.SuccessCount+counter.FailureCount+counter.SkippedCount, f.Message)
			stepResult.Failure = &f
		}

		// Add step result to workflow buffer
		if ctx.Result != nil {
			ctx.Result.AddStepResult(jobID, stepResult)
		}
	}
}
