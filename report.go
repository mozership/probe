package probe

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mozership/probe/report"
)

// BuildReport assembles a Report from the results of a run. Jobs are listed in
// the given order, which is the order they are declared in the workflow.
func BuildReport(name, description string, rs *Result, order []string, startedAt, finishedAt time.Time) *report.Report {
	r := report.New(name, description, startedAt, finishedAt)
	if rs == nil {
		return r
	}

	for _, id := range order {
		jr, ok := rs.Jobs[id]
		if !ok {
			continue
		}
		r.AddJob(buildJobReport(jr))
	}

	return r
}

func buildJobReport(jr *JobResult) report.Job {
	jr.mutex.Lock()
	defer jr.mutex.Unlock()

	status := report.Passed
	switch jr.Status {
	case StatusSkipped:
		status = report.Skipped
	case StatusError:
		status = report.Failed
	}

	job := report.Job{
		ID:         jr.JobID,
		Name:       jr.JobName,
		Status:     status,
		StartedAt:  jr.StartTime,
		DurationMs: jr.EndTime.Sub(jr.StartTime).Milliseconds(),
		Steps:      make([]report.Step, 0, len(jr.StepResults)),
	}
	for _, sr := range jr.StepResults {
		job.Steps = append(job.Steps, buildStepReport(sr))
	}
	job.RepeatTest = buildRepeatTestReport(jr.RepeatTest)
	job.BaselineTest = buildRepeatTestReport(jr.BaselineTest)

	return job
}

func buildRepeatTestReport(rt *RepeatTestResult) *report.RepeatTest {
	if rt == nil {
		return nil
	}
	out := &report.RepeatTest{Test: rt.Test, Status: report.Passed}
	switch {
	case rt.Failure != nil:
		out.Status = report.Failed
		out.Failure = &report.Failure{Kind: rt.Failure.Kind, Message: rt.Failure.Message}
	case rt.Skipped != "":
		out.Status = report.Skipped
		out.Reason = rt.Skipped
	}
	return out
}

func buildStepReport(sr StepResult) report.Step {
	step := report.Step{
		Index:      sr.Index,
		ID:         sr.ID,
		Name:       sr.Name,
		Test:       sr.Test,
		DurationMs: sr.Elapsed.Milliseconds(),
	}

	switch sr.Status {
	case StatusSuccess:
		step.Status = report.Passed
	case StatusError:
		step.Status = report.Failed
	case StatusSkipped:
		step.Status = report.Skipped
	default:
		step.Status = report.Untested
	}

	if sr.RetryAttempt > 0 {
		step.Retry = &report.Retry{Attempts: sr.RetryAttempt, Max: sr.RetryMax}
	}

	if c := sr.RepeatCounter; c != nil {
		step.Name = c.Name
		step.Repeat = &report.Repeat{
			Total:   c.SuccessCount + c.FailureCount + c.SkippedCount,
			Success: c.SuccessCount,
			Failure: c.FailureCount,
			Skipped: c.SkippedCount,
			Warmup:  c.Warmup,
			Latency: report.NewLatency(c.RTs),
		}
		// The terminal report marks a mix of passing and failing iterations as
		// a warning. Any failing iteration fails the job, so it fails the step,
		// unless the job has a test over its runs, which counts them instead.
		switch {
		case c.FailureCount > 0 && !c.Tested:
			step.Status = report.Failed
		case c.SuccessCount == 0 && c.FailureCount == 0 && c.SkippedCount > 0:
			step.Status = report.Skipped
		case sr.HasTest:
			step.Status = report.Passed
		default:
			step.Status = report.Untested
		}
	} else {
		// The report files hold the echo without the white space around it.
		step.Echo = strings.TrimSpace(sr.Echo)
	}

	step.Contract = sr.Contract

	if f := sr.Failure; f != nil && step.Status == report.Failed {
		step.Failure = &report.Failure{
			Kind:       f.Kind,
			Message:    f.Message,
			Request:    jsonSafeMap(f.Request),
			Response:   jsonSafeMap(f.Response),
			Violations: f.Violations,
		}
	}

	return step
}

// jsonSafeMap returns m unchanged when it encodes as JSON, and otherwise a
// copy in which every value that does not encode is replaced by its %v form.
// Action results are plain data, so the fallback only guards against a
// report that cannot be written at all.
func jsonSafeMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	if _, err := json.Marshal(m); err == nil {
		return m
	}

	safe := make(map[string]any, len(m))
	for k, v := range m {
		if _, err := json.Marshal(v); err != nil {
			if nested, ok := v.(map[string]any); ok {
				safe[k] = jsonSafeMap(nested)
				continue
			}
			safe[k] = fmt.Sprintf("%v", v)
			continue
		}
		safe[k] = v
	}
	return safe
}
