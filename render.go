package probe

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

// reportRenderer renders the report of a run as text: a block for each job,
// in the order the workflow writes them, and a footer. What it renders is
// shown as it is, so the secrets the masker knows are hidden in it.
//
// Its methods named write* write their part of the report to the builder
// they are given, and those named *Text return theirs.
type reportRenderer struct {
	// order are the IDs of the jobs, in the order their blocks come in.
	order []string
	// baseline is the report of an earlier run, which the times of the
	// repeated steps are shown against. Nil shows none.
	baseline *report.Report
	masker   *mask.Masker
}

// newReportRenderer returns a renderer of the jobs of order, which shows
// the times of the repeated steps against baseline, when it is not nil.
func newReportRenderer(order []string, baseline *report.Report, masker *mask.Masker) *reportRenderer {
	return &reportRenderer{order: order, baseline: baseline, masker: masker}
}

// fprintf writes to w, with the secrets hidden.
func (r *reportRenderer) fprintf(w io.Writer, format string, a ...any) {
	_, _ = io.WriteString(w, r.masker.String(fmt.Sprintf(format, a...)))
}

// writeStepRepeatStart writes the line that starts a repeated step: its
// number, its name and how many times it was repeated.
func (r *reportRenderer) writeStepRepeatStart(b *strings.Builder, stepIdx int, stepName string, repeatCount, warmup int) {
	num := colorDim().Sprintf("%2d.", stepIdx)
	if warmup > 0 {
		r.fprintf(b, "%s %s (repeating %d times, after %d to warm up)\n", num, stepName, repeatCount, warmup)
		return
	}
	r.fprintf(b, "%s %s (repeating %d times)\n", num, stepName, repeatCount)
}

// writeStepRepeatResult writes how the runs of a repeated step went, and
// the summary of their times when it took any.
func (r *reportRenderer) writeStepRepeatResult(b *strings.Builder, counter *StepRepeatCounter, hasTest bool) {
	totalCount := counter.RepeatTotal

	// The runs in which the step was not run, after one that failed, are
	// told beside the count, of which they are no part.
	skipped := ""
	if counter.SkippedCount > 0 {
		skipped = fmt.Sprintf(", %d skipped", counter.SkippedCount)
	}
	if counter.SuccessCount == 0 && counter.FailureCount == 0 && counter.SkippedCount > 0 {
		r.fprintf(b, "    %s skipped in %d/%d runs\n",
			colorInfo().Sprintf(IconSkip),
			counter.SkippedCount,
			totalCount)
		return
	}

	if hasTest {
		successRate := float64(counter.SuccessCount) / float64(totalCount) * 100
		statusIcon := colorSuccess().Sprintf(IconSuccess)
		if counter.FailureCount > 0 {
			if counter.SuccessCount == 0 {
				statusIcon = colorError().Sprintf(IconError)
			} else {
				statusIcon = colorNoTest().Sprintf(IconTriangle)
			}
		}

		r.fprintf(b, "    %s %d/%d success (%.1f%%)%s\n",
			statusIcon,
			counter.SuccessCount,
			totalCount,
			successRate,
			skipped)
	} else {
		// For no test cases, show success count (completed without errors)
		statusIcon := colorNoTest().Sprintf(IconTriangle)
		if counter.FailureCount > 0 {
			statusIcon = colorError().Sprintf(IconError)
		}
		r.fprintf(b, "    %s %d/%d completed (no test)%s\n",
			statusIcon,
			counter.SuccessCount,
			totalCount,
			skipped)
	}

	if l := report.NewLatency(counter.RTs); l != nil {
		r.fprintf(b, "      %s\n", colorDim().Sprint(l.Summary()))
	}
}

// writeStepRepeatError writes under the result of a repeated step why the
// first of its runs that failed before its test could be evaluated did: the
// error of its action, a template that could not be evaluated, or the
// guard's refusal. The count alone would not tell it, and the error is told
// nowhere else unless the run is verbose.
func (r *reportRenderer) writeStepRepeatError(b *strings.Builder, counter *StepRepeatCounter) {
	f := counter.Failure
	if f == nil || f.Message == "" {
		return
	}
	switch f.Kind {
	case FailureAction, FailureTemplate, FailureRefused:
		b.WriteString(r.failureText(f, ""))
	}
}

// writeStepBaseline writes under the times of a repeated step those of the
// step in the baseline, bl, and how much they changed; nothing when there
// is no baseline to show them against, or the step took no times.
func (r *reportRenderer) writeStepBaseline(b *strings.Builder, counter *StepRepeatCounter, bl *report.RepeatBaseline) {
	if r.baseline == nil {
		return
	}
	if l := report.NewLatency(counter.RTs); l != nil {
		r.fprintf(b, "      %s\n", colorDim().Sprint(bl.Summary(l)))
	}
}

// repeatTestText returns the outcome of a test of a repeated job, the
// test over its runs or the one against the run compared to, named by label,
// under its steps, and why it failed or was skipped under it. It is laid out
// as a step is without its number, so that its icon and text line up with
// theirs, and why it failed with the text.
func (r *reportRenderer) repeatTestText(label string, rt *RepeatTestResult) string {
	switch {
	case rt == nil:
		return ""
	case rt.Skipped != "":
		return fmt.Sprintf("    %s %s: %s\n       %s %s\n", colorInfo().Sprintf(IconSkip), label, rt.Test, colorInfo().Sprintf("skipped:"), rt.Skipped)
	case rt.Passed:
		return fmt.Sprintf("    %s %s: %s\n", colorSuccess().Sprintf(IconSuccess), label, rt.Test)
	}
	return fmt.Sprintf("    %s %s: %s\n%s", colorError().Sprintf(IconError), label, rt.Test, r.failureText(rt.Failure, rt.Test))
}

// failureText returns why a step, or a test of a repeated job, failed, as
// it is shown under it: f is its failure and test its test. A test that is
// false is told by the condition of it that is false and what that
// compared, then the request and the response; one that could not be
// evaluated, or did not come to a boolean, by that; a contract that is
// broken by where, then the request and the response; and an error by its
// message. It is empty when f is nil.
func (r *reportRenderer) failureText(f *StepFailure, test string) string {
	if f == nil {
		return ""
	}
	switch f.Kind {
	case FailureAssertion:
		var b strings.Builder
		if f.clause != nil {
			b.WriteString(falseClauseDetail(maskClause(r.masker, *f.clause)))
		}
		b.WriteString(exchangeDetail(r.masker, f.Request, f.Response))
		return b.String()
	case FailureTestError:
		return testErrorDetail(f.Message)
	case FailureTestType:
		return typeMismatchDetail(test, f.got)
	case FailureContractResponse, FailureContractRequest:
		return contractDetail(r.masker, f)
	default:
		return indentDetail(f.Message)
	}
}

// writeUnderJob writes text as the body of the block of a job: its first
// line after ⎿, the others indented to it and the blank ones left out,
// then a blank line that ends the block. In the report the body comes under
// the status line of the job; what a job run on its own reports is the body
// alone.
func (r *reportRenderer) writeUnderJob(b *strings.Builder, text string) {
	text = strings.TrimSpace(text)
	if text != "" {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				if i == 0 {
					r.fprintf(b, "  ⎿ %s\n", line)
				} else {
					r.fprintf(b, "    %s\n", line)
				}
			}
		}
	}

	b.WriteString("\n")
}

// reportTotals are what the footer of a report tells of the jobs whose
// blocks it ends: how many of them succeeded, and the time from the
// earliest start of one to the latest end, which is the time of the
// workflow, as jobs run at once. The buffered and the streamed report
// both add their jobs to one, so that their footers are the same.
type reportTotals struct {
	succeeded  int
	start, end time.Time
}

// add counts jr in the totals. Callers hold jr.mutex.
func (t *reportTotals) add(jr *JobResult) {
	if jr.Succeeded() {
		t.succeeded++
	}
	if t.start.IsZero() || jr.StartTime.Before(t.start) {
		t.start = jr.StartTime
	}
	if t.end.IsZero() || jr.EndTime.After(t.end) {
		t.end = jr.EndTime
	}
}

// seconds is the time of the workflow in seconds, 0 before a job is added.
func (t reportTotals) seconds() float64 {
	if t.start.IsZero() || t.end.IsZero() {
		return 0
	}
	return t.end.Sub(t.start).Seconds()
}

// writeFooter writes the line that ends the report: the time of the
// workflow and how many of its totalJobs jobs failed, of the totals t.
func (r *reportRenderer) writeFooter(b *strings.Builder, t reportTotals, totalJobs int) {
	if t.succeeded == totalJobs {
		r.fprintf(b, "Total workflow time: %.2fs %s\n",
			t.seconds(),
			colorSuccess().Sprintf(IconSuccess+"All jobs succeeded"))
	} else {
		failedCount := totalJobs - t.succeeded
		r.fprintf(b, "Total workflow time: %.2fs %s\n",
			t.seconds(),
			colorError().Sprintf(IconError+"%d job(s) failed", failedCount))
	}
}

// writeJobStatus writes the status line of a job, in the color of its
// status.
func (r *reportRenderer) writeJobStatus(b *strings.Builder, jobName string, status StatusType, duration float64) {
	var icon string
	var statusText string
	var colorFunc func() *color.Color

	switch status {
	case StatusSuccess:
		icon = IconCircle
		statusText = fmt.Sprintf("Completed in %.2fs", duration)
		colorFunc = colorSuccess
	case StatusError:
		icon = IconCircle
		statusText = fmt.Sprintf("Failed in %.2fs", duration)
		colorFunc = colorError
	case StatusSkipped:
		icon = IconCircle
		statusText = "(SKIPPED)"
		colorFunc = colorSkipped
	default:
		icon = IconCircle
		statusText = fmt.Sprintf("Unknown status in %.2fs", duration)
		colorFunc = colorWarning
	}

	// For skipped jobs, make the entire line gray
	if status == StatusSkipped {
		r.fprintf(b, "%s %s %s\n",
			colorFunc().Sprint(icon),
			colorFunc().Sprint(jobName),
			colorFunc().Sprint(statusText))
	} else {
		r.fprintf(b, "%s %s (%s)\n",
			colorFunc().Sprint(icon),
			jobName,
			statusText)
	}
}

// jobText returns the block of a job: the status line followed by its step
// results. It adds the job to totals, as it was when its block was
// rendered, for the footer.
func (r *reportRenderer) jobText(jr *JobResult, totals *reportTotals) string {
	jr.mutex.Lock()
	defer jr.mutex.Unlock()

	var output strings.Builder

	duration := jr.EndTime.Sub(jr.StartTime)
	r.writeJobStatus(&output, jr.JobName, jr.Status, duration.Seconds())
	r.writeUnderJob(&output, r.stepResultsText(jr.JobID, jr.StepResults)+r.repeatTestText("repeat test", jr.RepeatTest)+r.repeatTestText("baseline test", jr.BaselineTest))
	totals.add(jr)

	return output.String()
}

// reportText returns the report of rs: the block of each job, in order,
// and the footer.
func (r *reportRenderer) reportText(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder
	var totals reportTotals
	for _, jobID := range r.order {
		if jr, exists := rs.Jobs[jobID]; exists {
			output.WriteString(r.jobText(jr, &totals))
		}
	}
	r.writeFooter(&output, totals, len(rs.Jobs))

	return output.String()
}

// stepsText returns the steps of the jobs of rs, in order, without their
// status lines or the footer: what a job run on its own reports.
func (r *reportRenderer) stepsText(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder

	for _, jobID := range r.order {
		if jr, exists := rs.Jobs[jobID]; exists {
			jr.mutex.Lock()
			stepOutput := r.stepResultsText(jr.JobID, jr.StepResults)
			r.writeUnderJob(&output, stepOutput)
			jr.mutex.Unlock()
		}
	}

	return output.String()
}

// stepResultsText returns the lines of stepResults, the steps of the job
// of jobID, by which they are found in the baseline.
func (r *reportRenderer) stepResultsText(jobID string, stepResults []StepResult) string {
	if len(stepResults) == 0 {
		return ""
	}

	var output strings.Builder
	baseline := r.baseline.BaselineSteps(jobID)

	for _, stepResult := range stepResults {
		// Handle repeat steps
		if stepResult.RepeatCounter != nil {
			r.writeStepRepeatStart(&output, stepResult.Index, stepResult.RepeatCounter.Name, stepResult.RepeatCounter.SuccessCount+stepResult.RepeatCounter.FailureCount+stepResult.RepeatCounter.SkippedCount, stepResult.RepeatCounter.Warmup)
			r.writeStepRepeatResult(&output, stepResult.RepeatCounter, stepResult.HasTest)
			r.writeStepRepeatError(&output, stepResult.RepeatCounter)
			r.writeStepBaseline(&output, stepResult.RepeatCounter, baseline.Next(stepResult.ID))
			for _, echo := range stepResult.RepeatCounter.Echoes {
				// A run that echoed nothing but white space is left out.
				if strings.TrimSpace(echo) == "" {
					continue
				}
				writeEcho(&output, echo)
			}
		} else {
			num := colorDim().Sprintf("%2d.", stepResult.Index)

			// Add wait time indicator if present
			waitPrefix := ""
			if stepResult.WaitTime != "" {
				waitPrefix = colorDim().Sprintf("%s%s → ", IconWait, stepResult.WaitTime)
			}

			// Add suffix: retry info then timing (start time + response time)
			ps := ""
			if stepResult.RetryAttempt > 0 {
				retryColor := colorDim()
				if stepResult.RetryAttempt > 1 {
					retryColor = colorWarning()
				}
				ps += retryColor.Sprintf(" %s %d/%d", IconRetry, stepResult.RetryAttempt, stepResult.RetryMax)
			}
			if !stepResult.StartedAt.IsZero() || stepResult.RT != "" {
				timing := ""
				if !stepResult.StartedAt.IsZero() {
					timing = fmt.Sprintf("@%d", stepResult.StartedAt.Unix())
				}
				if stepResult.RT != "" {
					if timing != "" {
						timing += " " + stepResult.RT
					} else {
						timing = stepResult.RT
					}
				}
				ps += colorDim().Sprintf(" (%s)", timing)
			}

			icon, c := statusIcon(stepResult.Status)
			mark := c.Sprint(icon)
			switch stepResult.Status {
			case StatusSuccess, StatusWarning:
				r.fprintf(&output, "%s %s %s%s%s\n", num, mark, waitPrefix, stepResult.Name, ps)
			case StatusError:
				if why := r.failureText(stepResult.Failure, stepResult.Test); why != "" {
					r.fprintf(&output, "%s %s %s%s%s\n%s\n", num, mark, waitPrefix, stepResult.Name, ps, why)
				} else {
					r.fprintf(&output, "%s %s %s%s%s\n", num, mark, waitPrefix, stepResult.Name, ps)
				}
			case StatusSkipped:
				r.fprintf(&output, "%s %s %s%s%s\n", num, mark, waitPrefix, colorDim().Sprintf("%s", stepTitle(stepResult)), ps)
			}

			if stepResult.Report != "" {
				a5space := "     "
				re := strings.ReplaceAll(stepResult.Report, "\n", "\n"+a5space)
				re = strings.TrimRight(re, " \n\t\r")
				output.WriteString(a5space + re + "\n")
			}

			if stepResult.HasEcho {
				writeEcho(&output, stepResult.Echo)
			}
		}
	}

	return output.String()
}

// writeEcho writes what a step echoed under it, each line indented to the
// column of its name, in the color of info.
func writeEcho(b *strings.Builder, echo string) {
	// An ANSI reset code after a line break would keep the Trim methods
	// from seeing it, so the trailing white space is removed before the
	// color is put on, and the line break is added after.
	echo = strings.TrimRight(indentDetail(echo), " \n\t\r")
	b.WriteString(colorInfo().Sprint(echo) + "\n")
}

// stepTitle returns how a step is named in the report and in the progress
// of a run: by its name, marked as skipped when it was. A repeated step is
// not marked, as its counts tell how many of its runs were skipped.
func stepTitle(sr StepResult) string {
	if sr.Status == StatusSkipped && sr.RepeatCounter == nil {
		return sr.Name + " (SKIPPED)"
	}
	return sr.Name
}

// headerText returns the name and description of the workflow that head
// the report, or nothing when it has no name.
func (r *reportRenderer) headerText(name, description string) string {
	if name == "" {
		return ""
	}

	var output strings.Builder
	bold := color.New(color.Bold)
	output.WriteString(bold.Sprintf("%s\n", name))
	if description != "" {
		output.WriteString(colorDim().Sprintf("%s\n", description))
	}
	output.WriteString("\n")
	return output.String()
}
