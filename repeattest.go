package probe

import (
	"fmt"
	"math"

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
		var rt map[string]any
		if l := report.NewLatency(r.rts); l != nil {
			rt = map[string]any{
				"min":  seconds(l.MinMs),
				"mean": seconds(l.MeanMs),
				"p50":  seconds(l.P50Ms),
				"p90":  seconds(l.P90Ms),
				"p95":  seconds(l.P95Ms),
				"p99":  seconds(l.P99Ms),
				"max":  seconds(l.MaxMs),
			}
		}
		steps[id] = map[string]any{
			"runs":         total,
			"success":      r.success,
			"failure":      r.failure,
			"skipped":      r.skipped,
			"success_rate": rate,
			"rt":           rt,
		}
	}
	return steps
}

// seconds converts a time in milliseconds, which a Latency gives to the
// microsecond, to seconds, as rt.sec is, without the error of dividing.
func seconds(ms float64) float64 {
	return math.Round(ms*1e3) / 1e6
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
