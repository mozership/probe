package probe

import (
	"fmt"
	"slices"

	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/report"
)

// repeatTestEnv is what the test of a repeated job reads: the vars of the
// workflow, and under steps the runs of each step by its id.
type repeatTestEnv struct {
	Vars  map[string]any `expr:"vars"`
	Steps map[string]any `expr:"steps"`
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

// repeatReads returns the ids of the steps that test, the test of a repeated
// job, reads under steps; nil when it reads none, or does not parse.
func repeatReads(test string) map[string]bool {
	chains := expr.Reads(test, "steps")
	if len(chains) == 0 {
		return nil
	}
	ids := make(map[string]bool, len(chains))
	for _, chain := range chains {
		ids[chain[0]] = true
	}
	return ids
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
	test := e.job.Repeat.Test
	env := repeatTestEnv{Vars: ctx.Vars, Steps: repeatRuns(counters)}
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

	if res.Failure != nil {
		ctx.Result.recordFailure(res.Failure.Kind)
	}
	if jr, ok := ctx.Result.Jobs[e.job.ID]; ok {
		jr.mutex.Lock()
		jr.RepeatTest = res
		jr.mutex.Unlock()
	}
	return res.Passed
}
