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

// printStepRepeatStart prints the start of a repeated step execution
func (r *reportRenderer) printStepRepeatStart(stepIdx int, stepName string, repeatCount, warmup int, output *strings.Builder) {
	num := colorDim().Sprintf("%2d.", stepIdx)
	if warmup > 0 {
		r.fprintf(output, "%s %s (repeating %d times, after %d to warm up)\n", num, stepName, repeatCount, warmup)
		return
	}
	r.fprintf(output, "%s %s (repeating %d times)\n", num, stepName, repeatCount)
}

// printStepRepeatResult prints the final result of a repeated step execution
func (r *reportRenderer) printStepRepeatResult(counter *StepRepeatCounter, hasTest bool, output *strings.Builder) {
	totalCount := counter.RepeatTotal

	// The runs in which the step was not run, after one that failed, are
	// told beside the count, of which they are no part.
	skipped := ""
	if counter.SkippedCount > 0 {
		skipped = fmt.Sprintf(", %d skipped", counter.SkippedCount)
	}
	if counter.SuccessCount == 0 && counter.FailureCount == 0 && counter.SkippedCount > 0 {
		r.fprintf(output, "    %s skipped in %d/%d runs\n",
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

		r.fprintf(output, "    %s %d/%d success (%.1f%%)%s\n",
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
		r.fprintf(output, "    %s %d/%d completed (no test)%s\n",
			statusIcon,
			counter.SuccessCount,
			totalCount,
			skipped)
	}

	if l := report.NewLatency(counter.RTs); l != nil {
		r.fprintf(output, "      %s\n", colorDim().Sprint(l.Summary()))
	}
}

// printStepBaseline prints under the times of a repeated step those of the
// step in the baseline, b, and how much they changed; nothing when there is
// no baseline to show them against, or the step took no times.
func (r *reportRenderer) printStepBaseline(counter *StepRepeatCounter, b *report.RepeatBaseline, output *strings.Builder) {
	if r.baseline == nil {
		return
	}
	if l := report.NewLatency(counter.RTs); l != nil {
		r.fprintf(output, "      %s\n", colorDim().Sprint(b.Summary(l)))
	}
}

// generateRepeatTest generates the outcome of a test of a repeated job, the
// test over its runs or the one against the run compared to, named by label,
// under its steps, and why it failed or was skipped under it. It is laid out
// as a step is without its number, so that its icon and text line up with
// theirs, and why it failed with the text.
func (r *reportRenderer) generateRepeatTest(label string, rt *RepeatTestResult) string {
	switch {
	case rt == nil:
		return ""
	case rt.Skipped != "":
		return fmt.Sprintf("    %s %s: %s\n       %s %s\n", colorInfo().Sprintf(IconSkip), label, rt.Test, colorInfo().Sprintf("skipped:"), rt.Skipped)
	case rt.Passed:
		return fmt.Sprintf("    %s %s: %s\n", colorSuccess().Sprintf(IconSuccess), label, rt.Test)
	}
	return fmt.Sprintf("    %s %s: %s\n%s", colorError().Sprintf(IconError), label, rt.Test, rt.Output)
}

// generateJobResults generates buffered job results as string
func (r *reportRenderer) generateJobResults(jobID string, input string, output *strings.Builder) {
	input = strings.TrimSpace(input)
	if input != "" {
		lines := strings.Split(input, "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				if i == 0 {
					r.fprintf(output, "  ⎿ %s\n", line)
				} else {
					r.fprintf(output, "    %s\n", line)
				}
			}
		}
	}

	output.WriteString("\n")
}

// generateFooter generates the workflow execution summary as string
func (r *reportRenderer) generateFooter(totalTime float64, successCount, totalJobs int, output *strings.Builder) {
	if successCount == totalJobs {
		r.fprintf(output, "Total workflow time: %.2fs %s\n",
			totalTime,
			colorSuccess().Sprintf(IconSuccess+"All jobs succeeded"))
	} else {
		failedCount := totalJobs - successCount
		r.fprintf(output, "Total workflow time: %.2fs %s\n",
			totalTime,
			colorError().Sprintf(IconError+"%d job(s) failed", failedCount))
	}
}

// generateJobStatus generates job status line with appropriate icon and color
func (r *reportRenderer) generateJobStatus(jobID, jobName string, status StatusType, duration float64, output *strings.Builder) {
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
		r.fprintf(output, "%s %s %s\n",
			colorFunc().Sprint(icon),
			colorFunc().Sprint(jobName),
			colorFunc().Sprint(statusText))
	} else {
		r.fprintf(output, "%s %s (%s)\n",
			colorFunc().Sprint(icon),
			jobName,
			statusText)
	}
}

// generateJobReport renders a single job block: the status line followed by
// its step results. It also reports whether the job counts as successful and
// the time range it covered, so that both the buffered and the streaming
// report can build the same footer.
func (r *reportRenderer) generateJobReport(jr *JobResult) (string, bool, time.Time, time.Time) {
	jr.mutex.Lock()
	defer jr.mutex.Unlock()

	var output strings.Builder

	duration := jr.EndTime.Sub(jr.StartTime)

	status := StatusSuccess
	succeeded := true
	if jr.Status == "skipped" {
		// Skipped jobs are considered successful.
		status = StatusSkipped
	} else if !jr.Success {
		status = StatusError
		succeeded = false
	}

	r.generateJobStatus(jr.JobID, jr.JobName, status, duration.Seconds(), &output)
	r.generateJobResults(jr.JobID, r.generateJobResultsFromStepResults(jr.JobID, jr.StepResults)+r.generateRepeatTest("repeat test", jr.RepeatTest)+r.generateRepeatTest("baseline test", jr.BaselineTest), &output)

	return output.String(), succeeded, jr.StartTime, jr.EndTime
}

// GenerateReport generates a complete workflow report string using Result data
func (r *reportRenderer) GenerateReport(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder
	successCount := 0
	var earliestStart, latestEnd time.Time

	// Generate step results and job summaries for each job in the order
	for _, jobID := range r.order {
		if jr, exists := rs.Jobs[jobID]; exists {
			block, succeeded, start, end := r.generateJobReport(jr)

			// Track earliest start and latest end time for wall clock calculation
			if earliestStart.IsZero() || start.Before(earliestStart) {
				earliestStart = start
			}
			if latestEnd.IsZero() || end.After(latestEnd) {
				latestEnd = end
			}

			if succeeded {
				successCount++
			}

			output.WriteString(block)
		}
	}

	// Calculate actual wall clock time (for parallel jobs)
	var totalTime float64
	if !earliestStart.IsZero() && !latestEnd.IsZero() {
		totalTime = latestEnd.Sub(earliestStart).Seconds()
	}

	// Generate workflow footer
	r.generateFooter(totalTime, successCount, len(rs.Jobs), &output)

	return output.String()
}

func (r *reportRenderer) GenerateReportOnlySteps(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder

	for _, jobID := range r.order {
		if jr, exists := rs.Jobs[jobID]; exists {
			jr.mutex.Lock()
			// Generate job results from StepResults
			stepOutput := r.generateJobResultsFromStepResults(jr.JobID, jr.StepResults)
			r.generateJobResults(jr.JobID, stepOutput, &output)
			jr.mutex.Unlock()
		}
	}

	return output.String()
}

// generateJobResultsFromStepResults creates job output string from
// StepResults of the job of jobID, by which its steps are found in the
// baseline.
func (r *reportRenderer) generateJobResultsFromStepResults(jobID string, stepResults []StepResult) string {
	if len(stepResults) == 0 {
		return ""
	}

	var output strings.Builder
	baseline := r.baseline.BaselineSteps(jobID)

	for _, stepResult := range stepResults {
		// Handle repeat steps
		if stepResult.RepeatCounter != nil {
			r.printStepRepeatStart(stepResult.Index, stepResult.RepeatCounter.Name, stepResult.RepeatCounter.SuccessCount+stepResult.RepeatCounter.FailureCount+stepResult.RepeatCounter.SkippedCount, stepResult.RepeatCounter.Warmup, &output)
			r.printStepRepeatResult(stepResult.RepeatCounter, stepResult.HasTest, &output)
			r.printStepBaseline(stepResult.RepeatCounter, baseline.Next(stepResult.ID), &output)
			for _, echo := range stepResult.RepeatCounter.EchoOutputs {
				echo = strings.TrimRight(echo, " \n\t\r")
				if echo == "" {
					continue
				}
				output.WriteString(colorInfo().Sprint(echo) + "\n")
			}
		} else {
			// Generate regular step output
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

			switch stepResult.Status {
			case StatusSuccess:
				r.fprintf(&output, "%s %s %s%s%s\n", num, colorSuccess().Sprintf(IconSuccess), waitPrefix, stepResult.Name, ps)
			case StatusError:
				if stepResult.TestOutput != "" {
					r.fprintf(&output, "%s %s %s%s%s\n%s\n", num, colorError().Sprintf(IconError), waitPrefix, stepResult.Name, ps, stepResult.TestOutput)
				} else {
					r.fprintf(&output, "%s %s %s%s%s\n", num, colorError().Sprintf(IconError), waitPrefix, stepResult.Name, ps)
				}
			case StatusWarning:
				r.fprintf(&output, "%s %s %s%s%s\n", num, colorNoTest().Sprintf(IconTriangle), waitPrefix, stepResult.Name, ps)
			case StatusSkipped:
				r.fprintf(&output, "%s %s %s%s%s\n", num, colorInfo().Sprintf(IconSkip), waitPrefix, colorDim().Sprintf("%s", stepResult.Name), ps)
			}

			if stepResult.Report != "" {
				a5space := "     "
				re := strings.ReplaceAll(stepResult.Report, "\n", "\n"+a5space)
				re = strings.TrimRight(re, " \n\t\r")
				output.WriteString(a5space + re + "\n")
			}

			if stepResult.EchoOutput != "" {
				// NOTE:
				// If you apply an ANSI reset code to a string that contains line breaks, the Trim methods will no longer work as expected.
				// Therefore, remove the trailing line breaks first, then apply the color settings and finally add the line break.
				echo := strings.TrimRight(stepResult.EchoOutput, " \n\t\r")
				output.WriteString(colorInfo().Sprint(echo) + "\n")
			}
		}
	}

	return output.String()
}

// generateHeader generates the workflow header string
func (r *reportRenderer) generateHeader(name, description string) string {
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
