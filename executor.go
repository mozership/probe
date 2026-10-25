package probe

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Executor runs a job: once, or as many times as its repeat says, and then
// records what the runs came to on the job's result.
type Executor struct {
	job *Job
}

// NewExecutor returns an Executor that runs job.
func NewExecutor(job *Job) *Executor {
	return &Executor{job: job}
}

// Run runs the job and records its result, and reports whether it passed.
// A job that did not pass is the caller's to fail the workflow for.
func (e *Executor) Run(ctx JobContext) bool {
	jr := ctx.Result.Jobs[e.job.ID]
	jr.mutex.Lock()
	jr.StartTime = time.Now()
	jr.mutex.Unlock()

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
	total := e.runs()
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
	total := e.runs()
	overallSuccess := true

	for current := range total {
		ctx.RepeatCurrent = current

		// Execute single run. A job that cannot start fails in any run; a
		// step that failed fails the job unless the run is only counted.
		failed, err := e.job.run(ctx)

		if err != nil || (failed && !ctx.countedOnly()) {
			overallSuccess = false
		}

		e.sleepBetweenRepeats(current+1, total)
	}

	return overallSuccess
}

// executeJobRepeatLoopAsync handles async execution loop with repeat logic
func (e *Executor) executeJobRepeatLoopAsync(ctx JobContext) bool {
	total := e.runs()

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

	for current := range total {
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
			}
		}(current, e.job)

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

// runs returns the number of times the job runs: once, or as many times
// as its repeat says, warmup runs included.
func (e *Executor) runs() int {
	if e.job.Repeat == nil {
		return 1
	}
	return e.job.Repeat.Runs()
}

// sleepBetweenRepeats waits for the interval of the job's repeat once done
// of its total runs are done, unless that was the last of them.
func (e *Executor) sleepBetweenRepeats(done, total int) {
	if done < total && e.job.Repeat != nil {
		time.Sleep(e.job.Repeat.Interval.Duration)
	}
}

// finalize updates the final job status and tells the scheduler the job is
// over, once all of its runs are done.
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

	ctx.scheduler.Finish(jobID, overallSuccess)

	// The job's report block is final now, so it can be streamed out.
	ctx.Result.notifyJobDone(jobID)
}

// appendRepeatStepResults adds a result for each step of the repeated job,
// made from the step's counter, to the job's result. The counters are kept
// by where each step is written and which entry of its iteration it ran with
// (Step.counterKey), so that a run does not count for another step when an
// iteration comes to another number of entries in each run. They are read in
// the order of those keys, which is the order the steps ran in, and numbered
// from there as the steps of any job are.
//
// It returns the counters, for the test of the job over its runs.
func (e *Executor) appendRepeatStepResults(ctx *JobContext) []StepRepeatCounter {
	rep := ctx.repeat
	counters := rep.snapshotCounters()
	if ctx.Result != nil {
		for n, counter := range counters {
			ctx.Result.AddStepResult(e.job.ID, repeatStepResult(n, counter, rep.reads[counter.ID]))
		}
	}
	return counters
}

// snapshotCounters returns a copy of the counter of each step, in the order
// of their keys, with what the runs share filled in: the total of the runs
// counted, the warmup runs and whether the job is tested over its runs.
//
// The copy is shallow: its fields can be set apart from the stored counter,
// but its Failure, Contract, RTs and Echoes are the stored counter's,
// so what they point to is not to be changed through it.
func (r *jobRepeat) snapshotCounters() []StepRepeatCounter {
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make([]int, 0, len(r.counters))
	for k := range r.counters {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	counters := make([]StepRepeatCounter, len(keys))
	for n, k := range keys {
		counter := r.counters[k]
		if counter.RepeatTotal == 0 {
			counter.RepeatTotal = r.total
		}
		counter.Warmup = r.warmup
		counter.Tested = r.tested
		counters[n] = counter
	}
	return counters
}

// repeatStepResult returns the result of the step at index, made from its
// counter over the runs of the job. readByTest is whether a test of the job
// over its runs reads the step, which gives the step a test as its own does.
//
// The step is skipped when it ran in no run, passed when it failed in none,
// failed when it passed in none, and warned of otherwise. Its failure is the
// first run's, with the number of runs it failed in.
func repeatStepResult(index int, counter StepRepeatCounter, readByTest bool) StepResult {
	var status StatusType
	switch {
	case counter.FailureCount == 0 && counter.SuccessCount == 0 && counter.SkippedCount > 0:
		status = StatusSkipped
	case counter.FailureCount == 0:
		status = StatusSuccess
	case counter.SuccessCount == 0:
		status = StatusError
	default:
		status = StatusWarning
	}

	result := StepResult{
		Index:         index,
		ID:            counter.ID,
		Name:          counter.Name,
		Status:        status,
		HasTest:       counter.Test != "" || counter.Checked || readByTest,
		RepeatCounter: &counter,
		Test:          counter.Test,
		Contract:      counter.Contract,
	}
	if counter.Failure != nil {
		f := *counter.Failure
		// The runs the step was not run in are runs of the job too, as the
		// count in the report files has them.
		f.Message = fmt.Sprintf("%d of %d iterations failed; the first: %s",
			counter.FailureCount, counter.SuccessCount+counter.FailureCount+counter.SkippedCount, f.Message)
		result.Failure = &f
	}
	return result
}
