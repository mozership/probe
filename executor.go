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

// Execute runs a job with buffered output
func (e *Executor) Execute(ctx JobContext) bool {
	jr := ctx.Result.Jobs[e.job.ID]

	jr.mutex.Lock()
	jr.StartTime = time.Now()
	jr.mutex.Unlock()

	// Use the existing buffered execution logic
	return e.executeWithBuffering(ctx)
}

// executeWithBuffering contains the buffered execution logic
func (e *Executor) executeWithBuffering(ctx JobContext) bool {
	ctx = e.setupContext(ctx)

	overallSuccess := e.executeJobRepeatLoop(ctx)

	if rep := ctx.repeat; rep != nil {
		counters := e.appendRepeatStepResults(&ctx)
		// A job is left skipped when its skipif skipped every run counted.
		// A run that ran a step leaves a counter, and then the test is
		// checked, whichever runs were skipped. A run that could not start
		// failed the job already.
		if rep.tested && (len(counters) > 0 || !e.skipped(ctx)) {
			e.unskip(ctx)
			overallSuccess = e.checkRepeatTest(ctx, counters) && overallSuccess
		}
		if rep.baselineTested && (len(counters) > 0 || !e.skipped(ctx)) {
			e.unskip(ctx)
			overallSuccess = e.checkBaselineTest(ctx, counters) && overallSuccess
		}
	}

	e.finalize(overallSuccess, ctx)

	return overallSuccess
}

// setupContext gives the job's context what its runs share: when the runs
// are counted, the counts they are counted in.
func (e *Executor) setupContext(ctx JobContext) JobContext {
	jobID := e.job.ID
	_, total := ctx.JobScheduler.GetRepeatInfo(jobID)
	var r Repeat
	if e.job.Repeat != nil {
		r = *e.job.Repeat
	}
	tested, baselineTested := r.Test != "", r.BaselineTest != ""
	// A warmup run is left out by the counters, and the tests read them, so a
	// job run once keeps them too when the run is a warmup run or the job has
	// a test.
	if total <= 1 && r.Warmup == 0 && !tested && !baselineTested {
		return ctx
	}
	rep := newJobRepeat(total, r.Warmup)
	rep.tested = tested
	rep.baselineTested = baselineTested
	tests := []string{r.Test}
	if baselineTested {
		// Whether the test is skipped depends on the run compared to alone,
		// and not on the runs, so it is known before they start.
		rep.baselineSkipped = baselineSkip(r.BaselineTest, ctx.Baseline, e.job.ID)
		if rep.baselineSkipped == "" {
			tests = append(tests, r.BaselineTest)
		}
	}
	rep.reads = repeatReads(tests...)
	ctx.repeat = rep
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

		// Execute single run. A job that cannot start fails in any run; a
		// step that failed fails the job unless the run is only counted.
		failed, err := e.job.run(ctx)

		if err != nil || (failed && !ctx.countedOnly()) {
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

		// The runs share the job: what a run of it finds out is kept in the
		// run's context and in a stepRun for each step, and the job is not
		// changed by running it.
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

			// Execute single run, failed as a run in turn is
			failed, err := j.run(execCtx)

			if err != nil || (failed && !execCtx.countedOnly()) {
				overallSuccess.Store(false)
				e.workflow.SetExitStatus(true)
			}
		}(current, e.job)

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
	// A job that passed keeps the status it has, which is skipped when its
	// skipif skipped it.
	if !overallSuccess {
		jr.Status = StatusError
	}
	jr.mutex.Unlock()

	ctx.JobScheduler.FinishJob(jobID, overallSuccess)

	// The job's report block is final now, so it can be streamed out.
	ctx.Result.notifyJobDone(jobID)
}

// appendRepeatStepResults appends the final results of repeat step executions to buffer.
// The counters are kept by where each step is written and which entry of
// its iteration it ran with (Step.counterKey), so that a run does not count
// for another step when an iteration comes to another number of entries in
// each run. They are read in the order of those keys, which is the order the
// steps ran in, and numbered from there as the steps of any job are.
//
// It returns the counters, for the test of the job over its runs.
func (e *Executor) appendRepeatStepResults(ctx *JobContext) []StepRepeatCounter {
	jobID := e.job.ID

	rep := ctx.repeat
	rep.mu.Lock()
	indexes := make([]int, 0, len(rep.counters))
	for i := range rep.counters {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	counters := make([]StepRepeatCounter, len(indexes))
	for n, i := range indexes {
		counter := rep.counters[i]
		// Ensure RepeatTotal is set (in case it wasn't set earlier)
		if counter.RepeatTotal == 0 {
			counter.RepeatTotal = rep.total
			rep.counters[i] = counter
		}
		counter.Warmup = rep.warmup
		counter.Tested = rep.tested
		counters[n] = counter
	}
	rep.mu.Unlock()

	// Create StepResults for repeat steps and add them to Result
	for n := range indexes {
		counter := counters[n]
		hasTest := counter.Test != "" || counter.Checked || rep.reads[counter.ID]

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
			ID:            counter.ID,
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

	return counters
}
