package probe

import (
	"fmt"
	"slices"

	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/report"
)

// repeatTestEnv is what the tests of a repeated job read: the vars of the
// workflow, and under steps the runs of each step by its id. The test
// against the run compared to reads that run's steps under baseline too.
type repeatTestEnv struct {
	Vars     map[string]any `expr:"vars"`
	Steps    map[string]any `expr:"steps"`
	Baseline map[string]any `expr:"baseline"`
}

// repeatRuns gathers the counters of a repeated job by the id of their step,
// as the test of the job reads them. A step run once per entry of its
// iteration has a counter per entry, and its runs are those of them all.
//
// Each step has runs, success, failure and skipped, the counts of its runs;
// success_rate, the share of the runs it passed in; and rt, the distribution
// of the times its action took in seconds, with min, mean, p50, p90, p95,
// p99 and max, or nil when it returned none.
func repeatRuns(counters []StepRepeatCounter) map[string]any {
	type runs struct {
		success, failure, skipped int
		rts                       []float64
	}
	byID := map[string]*runs{}
	for _, c := range counters {
		if c.ID == "" {
			continue
		}
		r := byID[c.ID]
		if r == nil {
			r = &runs{}
			byID[c.ID] = r
		}
		r.success += c.SuccessCount
		r.failure += c.FailureCount
		r.skipped += c.SkippedCount
		r.rts = append(r.rts, c.RTs...)
	}

	steps := make(map[string]any, len(byID))
	for id, r := range byID {
		total := r.success + r.failure + r.skipped
		rate := 0.0
		if total > 0 {
			rate = float64(r.success) / float64(total)
		}
		steps[id] = map[string]any{
			"runs":         total,
			"success":      r.success,
			"failure":      r.failure,
			"skipped":      r.skipped,
			"success_rate": rate,
			"rt":           repeatTimes(r.rts),
		}
	}
	return steps
}

// repeatReads returns the ids of the steps that tests, the tests of a
// repeated job, read under steps; nil when they read none, or do not parse.
func repeatReads(tests ...string) map[string]bool {
	var ids map[string]bool
	for _, test := range tests {
		for _, chain := range expr.Reads(test, "steps") {
			if ids == nil {
				ids = map[string]bool{}
			}
			ids[chain[0]] = true
		}
	}
	return ids
}

// baselineRuns gathers the repeated steps of the job of jobID in base, the
// report of the run compared to, by their ids, as the test against that run
// reads them under baseline.steps. Each step has runs, success and
// success_rate, as the runs of the step have; and rt, with the same fields
// in seconds, or nil when the step returned no times. A job has several
// steps of an id when the step was run once per entry of an iteration: their
// counts are added up, but their times cannot be, so rt is nil.
func baselineRuns(base *report.Report, jobID string) map[string]any {
	all := base.BaselineSteps(jobID).All()
	steps := make(map[string]any, len(all))
	for id, runs := range all {
		var total, success int
		for _, b := range runs {
			total += b.Total
			success += b.Success
		}
		rate := 0.0
		if total > 0 {
			rate = float64(success) / float64(total)
		}
		var rt map[string]any
		if l := runs[0].Latency; len(runs) == 1 && l != nil {
			rt = map[string]any{
				"min":  l.MinMs / 1000,
				"mean": l.MeanMs / 1000,
				"p50":  l.P50Ms / 1000,
				"p90":  l.P90Ms / 1000,
				"p95":  l.P95Ms / 1000,
				"p99":  l.P99Ms / 1000,
				"max":  l.MaxMs / 1000,
			}
		}
		steps[id] = map[string]any{
			"runs":         total,
			"success":      success,
			"success_rate": rate,
			"rt":           rt,
		}
	}
	return steps
}

// baselineSkip returns why test, the test of the job of jobID against base,
// the report of the run compared to, is skipped: when no run was compared
// to, or as baselineMissing tells. It returns empty when the test is checked.
func baselineSkip(test string, base *report.Report, jobID string) string {
	if base == nil {
		return "no baseline was given"
	}
	return baselineMissing(test, baselineRuns(base, jobID))
}

// baselineMissing returns why test, the test of a repeated job against the
// run compared to, cannot be checked against steps, that run's steps as
// baselineRuns gathers them: the first step it reads under baseline.steps
// that the run has none of, or whose times it reads and the run has none
// of. It returns empty when the test can be checked.
func baselineMissing(test string, steps map[string]any) string {
	for _, chain := range expr.Reads(test, "baseline") {
		if len(chain) < 2 || chain[0] != "steps" {
			continue
		}
		id := chain[1]
		st, ok := steps[id].(map[string]any)
		if !ok {
			return fmt.Sprintf("the baseline has no step %s", id)
		}
		if rt, _ := st["rt"].(map[string]any); len(chain) > 2 && chain[2] == "rt" && rt == nil {
			return fmt.Sprintf("the baseline has no times of step %s", id)
		}
	}
	return ""
}

// repeatTimes is the distribution of secs, the times of the runs in seconds,
// as the test of a repeated job reads it, or nil when there are none. The
// percentiles are by nearest rank, as the report's latency is, but of the
// times as they were taken: the report rounds them to the microsecond, which
// would put a time just under a limit at it.
func repeatTimes(secs []float64) map[string]any {
	if len(secs) == 0 {
		return nil
	}
	sorted := slices.Clone(secs)
	slices.Sort(sorted)
	var sum float64
	for _, s := range sorted {
		sum += s
	}
	return map[string]any{
		"min":  sorted[0],
		"mean": sum / float64(len(sorted)),
		"p50":  report.NearestRank(sorted, 50),
		"p90":  report.NearestRank(sorted, 90),
		"p95":  report.NearestRank(sorted, 95),
		"p99":  report.NearestRank(sorted, 99),
		"max":  sorted[len(sorted)-1],
	}
}

// skipped reports whether the job was skipped by its skipif, which leaves
// its test over its runs unchecked.
func (e *Executor) skipped(ctx JobContext) bool {
	jr := ctx.Result.Jobs[e.job.ID]
	jr.mutex.Lock()
	defer jr.mutex.Unlock()
	return jr.Status == "skipped"
}

// unskip takes back the skipped status that a run skipped by the job's
// skipif left, when other runs ran and the test over them decides.
func (e *Executor) unskip(ctx JobContext) {
	jr := ctx.Result.Jobs[e.job.ID]
	jr.mutex.Lock()
	defer jr.mutex.Unlock()
	if jr.Status == "skipped" {
		jr.Status = "Running"
	}
}

// checkRepeatTest checks the test of a repeated job against the runs of its
// steps once every run is over, records the outcome on the job's result, and
// reports whether the test held, which is whether the job passed.
func (e *Executor) checkRepeatTest(ctx JobContext, counters []StepRepeatCounter) bool {
	env := repeatTestEnv{Vars: ctx.Vars, Steps: repeatRuns(counters)}
	res := evalRepeatTest(ctx, e.job.Repeat.Test, env)
	e.recordRepeatTest(ctx, res, func(jr *JobResult) { jr.RepeatTest = res })
	return res.Passed
}

// checkBaselineTest checks the test of a repeated job against the run
// compared to once every run is over, and records the outcome on the job's
// result. It reports whether the job passes by it: when the test held, or
// was skipped, as it is when no run was compared to, or that run has none of
// a step the test reads.
func (e *Executor) checkBaselineTest(ctx JobContext, counters []StepRepeatCounter) bool {
	test := e.job.Repeat.BaselineTest
	res := &RepeatTestResult{Test: test, Passed: true, Skipped: ctx.baselineSkipped}
	if res.Skipped == "" {
		env := repeatTestEnv{Vars: ctx.Vars, Steps: repeatRuns(counters), Baseline: map[string]any{"steps": baselineRuns(ctx.Printer.baseline, e.job.ID)}}
		res = evalRepeatTest(ctx, test, env)
	}
	e.recordRepeatTest(ctx, res, func(jr *JobResult) { jr.BaselineTest = res })
	return res.Passed
}

// evalRepeatTest evaluates test, a test of a repeated job, against env.
func evalRepeatTest(ctx JobContext, test string, env repeatTestEnv) *RepeatTestResult {
	ev := &expr.Expr{}
	res := &RepeatTestResult{Test: test}

	out, err := ev.Eval(test, env)
	if err != nil {
		res.Failure = &StepFailure{Kind: FailureTestError, Message: err.Error()}
		res.Output = ctx.Printer.generateTestError(test, err)
	} else if passed, ok := out.(bool); !ok {
		res.Failure = &StepFailure{
			Kind:    FailureTestType,
			Message: fmt.Sprintf("test evaluated to %v (%T), not a boolean", out, out),
		}
		res.Output = ctx.Printer.generateTestTypeMismatch(test, out)
	} else if !passed {
		message := "test evaluated to false"
		if clause, ok := ev.FalseClause(test, env); ok {
			clause = ctx.Printer.maskClause(clause)
			message += ": " + clauseSummary(clause)
			res.Output = ctx.Printer.generateFalseClause(clause)
		}
		res.Failure = &StepFailure{Kind: FailureAssertion, Message: message}
	} else {
		res.Passed = true
	}
	return res
}

// recordRepeatTest records res, the outcome of a test of a repeated job, by
// set on the job's result, and its failure for the exit code of the run.
func (e *Executor) recordRepeatTest(ctx JobContext, res *RepeatTestResult, set func(*JobResult)) {
	if res.Failure != nil {
		ctx.Result.recordFailure(res.Failure.Kind)
	}
	if jr, ok := ctx.Result.Jobs[e.job.ID]; ok {
		jr.mutex.Lock()
		set(jr)
		jr.mutex.Unlock()
	}
}
