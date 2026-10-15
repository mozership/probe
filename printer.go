package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

// Color Functions
// colorSuccess returns a *color.Color for success (RGB 0,175,0)
func colorSuccess() *color.Color {
	return color.RGB(0, 175, 0)
}

// colorError returns a *color.Color for errors (red)
func colorError() *color.Color {
	return color.New(color.FgRed)
}

// colorInfo returns a *color.Color for info messages (blue)
func colorInfo() *color.Color {
	return color.New(color.FgBlue)
}

// colorDim returns a *color.Color for subdued text
func colorDim() *color.Color {
	return color.New(color.FgHiBlack)
}

// colorWarning returns a *color.Color for warnings (yellow)
func colorWarning() *color.Color {
	return color.New(color.FgYellow)
}

// colorSkipped returns a *color.Color for skipped items (gray)
func colorSkipped() *color.Color {
	return color.New(color.FgHiBlack)
}

func colorNoTest() *color.Color {
	return color.RGB(0, 102, 204)
}

// Icon constants
const (
	IconSuccess  = "✓ "
	IconError    = "✗ "
	IconTriangle = "△ "
	IconCircle   = "⏺"
	IconWait     = "🕐︎"
	IconSkip     = "⏭ "
	IconRetry    = "⟳"
)

// LogLevel defines different logging levels
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// Printer implements PrintWriter for console print
type Printer struct {
	verbose   bool
	Buffer    map[string]*strings.Builder
	BufferIDs []string // Order preservation
	spinner   *spinner.Spinner
	spinnerMu sync.Mutex // serializes writes to spinner.Suffix from concurrent steps (async repeat)
	outWriter io.Writer
	errWriter io.Writer
	reporter  Reporter
	// writeMu serializes writes to outWriter and errWriter. Jobs run
	// concurrently and report errors as they happen, so without it two
	// messages could interleave, and a buffer writer would race.
	writeMu sync.Mutex
	// masker hides the workflow's secrets in everything written through the
	// printer. Nil hides nothing.
	masker *mask.Masker
}

// SetMasker installs the masker applied to everything the printer writes.
func (p *Printer) SetMasker(m *mask.Masker) {
	p.masker = m
}

// Masker returns the masker the printer applies. It is nil when none was
// installed, and on a nil Printer, which some contexts run without.
func (p *Printer) Masker() *mask.Masker {
	if p == nil {
		return nil
	}
	return p.masker
}

// NewPrinter creates a new console print writer
func NewPrinter(verbose bool, bufferIDs []string) *Printer {
	if os.Getenv("FORCE_COLOR") == "1" || os.Getenv("PROBE_TTY") == "1" {
		color.NoColor = false
	}

	buffer := make(map[string]*strings.Builder)

	// Pre-initialize buffers for all provided job IDs
	for _, id := range bufferIDs {
		buffer[id] = &strings.Builder{}
	}

	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	return &Printer{
		verbose:   verbose,
		Buffer:    buffer,
		BufferIDs: bufferIDs, // Store order information
		spinner:   s,
		outWriter: os.Stdout,
		errWriter: os.Stderr,
	}
}

func newBufferPrinter() *Printer {
	pr := NewPrinter(false, []string{})
	pr.outWriter = new(bytes.Buffer)
	pr.errWriter = new(bytes.Buffer)
	// Tests do not render a live spinner; nil here makes the spinner
	// helpers no-op so that `go test -race` does not flag the
	// reader-in-library / writer-in-probe contention on spinner.Suffix.
	pr.spinner = nil
	return pr
}

// SetBufferIDs records the job order the report is rendered in. Workflow.Start
// sets it once job IDs are assigned, because a caller cannot know an ID that
// the scheduler generates for a job that omitted one.
func (p *Printer) SetBufferIDs(ids []string) {
	p.BufferIDs = ids
	for _, id := range ids {
		if _, exists := p.Buffer[id]; !exists {
			p.Buffer[id] = &strings.Builder{}
		}
	}
}

// SetReporter installs the output strategy used for this run.
func (p *Printer) SetReporter(r Reporter) {
	p.reporter = r
}

// Reporter returns the installed output strategy, or nil when none was set.
func (p *Printer) Reporter() Reporter {
	return p.reporter
}

// StepStart announces that a step began. The buffered reporter turns this into
// the spinner suffix, the stream reporter into a stderr progress line.
func (p *Printer) StepStart(jobID, stepName string) {
	if p.reporter != nil {
		p.reporter.StepStart(jobID, stepName)
		return
	}
	p.AddSpinnerSuffix(stepName)
}

func (p *Printer) StartSpinner() {
	if !p.verbose && p.spinner != nil {
		p.spinner.Start()
	}
}

func (p *Printer) StopSpinner() {
	if !p.verbose && p.spinner != nil {
		p.spinner.Stop()
	}
}

func (p *Printer) AddSpinnerSuffix(txt string) {
	if p.verbose || p.spinner == nil {
		return
	}
	// Async repeat fans out steps across goroutines; serialize writes to
	// spinner.Suffix so that probe's own writers don't race each other.
	p.spinnerMu.Lock()
	p.spinner.Suffix = fmt.Sprintf(" %s...", p.masker.String(txt))
	p.spinnerMu.Unlock()
}

// Every write goes through write, so that secrets are hidden and concurrent
// jobs do not interleave or race on the writer.
func (p *Printer) write(w io.Writer, s string) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err := io.WriteString(w, p.masker.String(s))
	return err
}

func (p *Printer) Fprint(w io.Writer, a ...any) {
	if err := p.write(w, fmt.Sprint(a...)); err != nil {
		fmt.Printf("Fprint: %v\n", err)
	}
}

func (p *Printer) Fprintf(w io.Writer, f string, a ...any) {
	if err := p.write(w, fmt.Sprintf(f, a...)); err != nil {
		fmt.Printf("Fprintf: %v\n", err)
	}
}

func (p *Printer) Fprintln(w io.Writer, a ...any) {
	if err := p.write(w, fmt.Sprintln(a...)); err != nil {
		fmt.Printf("Fprintln: %v\n", err)
	}
}

// printStepRepeatStart prints the start of a repeated step execution
func (p *Printer) printStepRepeatStart(stepIdx int, stepName string, repeatCount, warmup int, output *strings.Builder) {
	num := colorDim().Sprintf("%2d.", stepIdx)
	if warmup > 0 {
		p.Fprintf(output, "%s %s (repeating %d times, after %d to warm up)\n", num, stepName, repeatCount, warmup)
		return
	}
	p.Fprintf(output, "%s %s (repeating %d times)\n", num, stepName, repeatCount)
}

// printStepRepeatResult prints the final result of a repeated step execution
func (p *Printer) printStepRepeatResult(counter *StepRepeatCounter, hasTest bool, output *strings.Builder) {
	totalCount := counter.RepeatTotal

	// The runs in which the step was not run, after one that failed, are
	// told beside the count, of which they are no part.
	skipped := ""
	if counter.SkippedCount > 0 {
		skipped = fmt.Sprintf(", %d skipped", counter.SkippedCount)
	}
	if counter.SuccessCount == 0 && counter.FailureCount == 0 && counter.SkippedCount > 0 {
		p.Fprintf(output, "    %s skipped in %d/%d runs\n",
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

		p.Fprintf(output, "    %s %d/%d success (%.1f%%)%s\n",
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
		p.Fprintf(output, "    %s %d/%d completed (no test)%s\n",
			statusIcon,
			counter.SuccessCount,
			totalCount,
			skipped)
	}

	if l := report.NewLatency(counter.RTs); l != nil {
		p.Fprintf(output, "      %s\n", colorDim().Sprint(l.Summary()))
	}
}

// generateRepeatTest generates the outcome of the test of a repeated job
// over its runs, under its steps, and why it failed under it. It is laid out
// as a step is without its number, so that its icon and text line up with
// theirs, and why it failed with the text.
func (p *Printer) generateRepeatTest(rt *RepeatTestResult) string {
	if rt == nil {
		return ""
	}
	if rt.Passed {
		return fmt.Sprintf("    %s repeat test: %s\n", colorSuccess().Sprintf(IconSuccess), rt.Test)
	}
	return fmt.Sprintf("    %s repeat test: %s\n%s", colorError().Sprintf(IconError), rt.Test, rt.Output)
}

// generateJobResults generates buffered job results as string
func (p *Printer) generateJobResults(jobID string, input string, output *strings.Builder) {
	input = strings.TrimSpace(input)
	if input != "" {
		lines := strings.Split(input, "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) != "" {
				if i == 0 {
					p.Fprintf(output, "  ⎿ %s\n", line)
				} else {
					p.Fprintf(output, "    %s\n", line)
				}
			}
		}
	}

	output.WriteString("\n")
}

// generateFooter generates the workflow execution summary as string
func (p *Printer) generateFooter(totalTime float64, successCount, totalJobs int, output *strings.Builder) {
	if successCount == totalJobs {
		p.Fprintf(output, "Total workflow time: %.2fs %s\n",
			totalTime,
			colorSuccess().Sprintf(IconSuccess+"All jobs succeeded"))
	} else {
		failedCount := totalJobs - successCount
		p.Fprintf(output, "Total workflow time: %.2fs %s\n",
			totalTime,
			colorError().Sprintf(IconError+"%d job(s) failed", failedCount))
	}
}

// generateJobStatus generates job status line with appropriate icon and color
func (p *Printer) generateJobStatus(jobID, jobName string, status StatusType, duration float64, output *strings.Builder) {
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
		p.Fprintf(output, "%s %s %s\n",
			colorFunc().Sprint(icon),
			colorFunc().Sprint(jobName),
			colorFunc().Sprint(statusText))
	} else {
		p.Fprintf(output, "%s %s (%s)\n",
			colorFunc().Sprint(icon),
			jobName,
			statusText)
	}
}

// generateJobReport renders a single job block: the status line followed by
// its step results. It also reports whether the job counts as successful and
// the time range it covered, so that both the buffered and the streaming
// report can build the same footer.
func (p *Printer) generateJobReport(jr *JobResult) (string, bool, time.Time, time.Time) {
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

	p.generateJobStatus(jr.JobID, jr.JobName, status, duration.Seconds(), &output)
	p.generateJobResults(jr.JobID, p.generateJobResultsFromStepResults(jr.StepResults)+p.generateRepeatTest(jr.RepeatTest), &output)

	return output.String(), succeeded, jr.StartTime, jr.EndTime
}

// GenerateReport generates a complete workflow report string using Result data
func (p *Printer) GenerateReport(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder
	successCount := 0
	var earliestStart, latestEnd time.Time

	// Generate step results and job summaries for each job in BufferIDs order
	for _, jobID := range p.BufferIDs {
		if jr, exists := rs.Jobs[jobID]; exists {
			block, succeeded, start, end := p.generateJobReport(jr)

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
	p.generateFooter(totalTime, successCount, len(rs.Jobs), &output)

	return output.String()
}

func (p *Printer) GenerateReportOnlySteps(rs *Result) string {
	if rs == nil {
		return ""
	}

	var output strings.Builder

	for _, jobID := range p.BufferIDs {
		if jr, exists := rs.Jobs[jobID]; exists {
			jr.mutex.Lock()
			// Generate job results from StepResults
			stepOutput := p.generateJobResultsFromStepResults(jr.StepResults)
			p.generateJobResults(jr.JobID, stepOutput, &output)
			jr.mutex.Unlock()
		}
	}

	return output.String()
}

// PrintReport prints a complete workflow report using Result data
func (p *Printer) PrintReport(rs *Result) {
	reportOutput := p.GenerateReport(rs)
	if reportOutput != "" {
		p.Fprint(p.outWriter, reportOutput)
	}
}

// generateJobResultsFromStepResults creates job output string from StepResults
func (p *Printer) generateJobResultsFromStepResults(stepResults []StepResult) string {
	if len(stepResults) == 0 {
		return ""
	}

	var output strings.Builder

	for _, stepResult := range stepResults {
		// Handle repeat steps
		if stepResult.RepeatCounter != nil {
			p.printStepRepeatStart(stepResult.Index, stepResult.RepeatCounter.Name, stepResult.RepeatCounter.SuccessCount+stepResult.RepeatCounter.FailureCount+stepResult.RepeatCounter.SkippedCount, stepResult.RepeatCounter.Warmup, &output)
			p.printStepRepeatResult(stepResult.RepeatCounter, stepResult.HasTest, &output)
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
				p.Fprintf(&output, "%s %s %s%s%s\n", num, colorSuccess().Sprintf(IconSuccess), waitPrefix, stepResult.Name, ps)
			case StatusError:
				if stepResult.TestOutput != "" {
					p.Fprintf(&output, "%s %s %s%s%s\n%s\n", num, colorError().Sprintf(IconError), waitPrefix, stepResult.Name, ps, stepResult.TestOutput)
				} else {
					p.Fprintf(&output, "%s %s %s%s%s\n", num, colorError().Sprintf(IconError), waitPrefix, stepResult.Name, ps)
				}
			case StatusWarning:
				p.Fprintf(&output, "%s %s %s%s%s\n", num, colorNoTest().Sprintf(IconTriangle), waitPrefix, stepResult.Name, ps)
			case StatusSkipped:
				p.Fprintf(&output, "%s %s %s%s%s\n", num, colorInfo().Sprintf(IconSkip), waitPrefix, colorDim().Sprintf("%s", stepResult.Name), ps)
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
func (p *Printer) generateHeader(name, description string) string {
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

// PrintHeader prints the workflow name and description
func (p *Printer) PrintHeader(name, description string) {
	header := p.generateHeader(name, description)
	if header != "" {
		p.Fprint(p.outWriter, header)
	}
}

// generateError generates an error message string
func (p *Printer) generateError(format string, args ...any) string {
	return fmt.Sprintf("%s: %s\n", colorError().Sprintf("Error"), fmt.Sprintf(format, args...))
}

// PrintError prints an error message
func (p *Printer) PrintError(format string, args ...any) {
	p.Fprint(p.errWriter, p.generateError(format, args...))
}

// PrintVerbose prints verbose output (only if verbose mode is enabled)
func (p *Printer) PrintVerbose(format string, args ...any) {
	if p.verbose {
		p.Fprintf(p.errWriter, format, args...)
	}
}

// PrintSeparator prints a separator line for verbose output
func (p *Printer) PrintSeparator() {
	if p.verbose {
		p.Fprintln(p.errWriter, "- - -")
	}
}

// generateLogDebug generates debug message string
func (p *Printer) generateLogDebug(format string, args ...any) string {
	return fmt.Sprintf("[DEBUG] %s\n", fmt.Sprintf(format, args...))
}

// LogDebug prints debug messages (only in verbose mode)
func (p *Printer) LogDebug(format string, args ...any) {
	if p.verbose {
		p.Fprint(p.errWriter, p.generateLogDebug(format, args...))
	}
}

// generateLogError generates error log message string
func (p *Printer) generateLogError(format string, args ...any) string {
	return fmt.Sprintf("%s\n", colorError().Sprintf("[ERROR] %s", fmt.Sprintf(format, args...)))
}

// LogError prints error messages to stderr
func (p *Printer) LogError(format string, args ...any) {
	p.Fprint(os.Stderr, p.generateLogError(format, args...))
}

// LogWarn prints a warning to the error writer: something the user should
// know about that does not make the run fail.
func (p *Printer) LogWarn(format string, args ...any) {
	p.Fprint(p.errWriter, colorWarning().Sprintf("[WARN] %s", fmt.Sprintf(format, args...))+"\n")
}

// Step Output Formatting Functions
// These functions handle output formatting with proper separation of concerns

// generateEchoOutput formats echo output with proper indentation
func (p *Printer) generateEchoOutput(content string, err error) string {
	if err != nil {
		content = fmt.Sprintf("error: %v", err)
	}
	return indentDetail(content)
}

// indentDetail indents every line of s, including those after a newline in
// it, to the column of a step's name, where the details printed under a step
// start. The result ends with a newline.
func indentDetail(s string) string {
	indent := "       "
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n") + "\n"
}

// generateTestFailure formats test failure output with request/response info
func (p *Printer) generateTestFailure(testExpr string, result any, req, res map[string]any) string {
	// Check if response has dump field set to false
	if res != nil {
		if dump, exists := res["dump"]; exists {
			if dumpBool, ok := dump.(bool); ok && !dumpBool {
				return "" // Don't dump request/response if dump is false
			}
		}
	}

	// Mask the data, not the formatted text: %#v escapes quotes and
	// backslashes, so a secret containing one would no longer match.
	output := fmt.Sprintf("       %s %#v\n", colorInfo().Sprintf("request:"), p.masker.Map(req))
	output += fmt.Sprintf("       %s %#v\n", colorInfo().Sprintf("response:"), p.masker.Map(res))
	return output
}

// generateFalseClause formats the condition of a test that is false, and
// under it what each thing it compares evaluated to:
//
//	failed: res.code == 200
//	  res.code = 500
//
// c is a condition maskClause has hidden the secrets of.
func (p *Printer) generateFalseClause(c expr.Clause) string {
	var b strings.Builder
	fmt.Fprintf(&b, "       %s %s\n", colorInfo().Sprintf("failed:"), c.Expr)
	for _, o := range c.Operands {
		fmt.Fprintf(&b, "         %s\n", operandText(o))
	}
	return b.String()
}

// maskClause returns c with the secrets hidden in the condition and in what
// its operands evaluated to. The values are masked as data, before they are
// written out and cut: a secret that is cut, or whose quotes are escaped, is
// no longer the text the masker knows.
func (p *Printer) maskClause(c expr.Clause) expr.Clause {
	out := expr.Clause{Expr: p.masker.String(c.Expr)}
	for _, o := range c.Operands {
		out.Operands = append(out.Operands, expr.Operand{Expr: p.masker.String(o.Expr), Value: p.masker.Value(o.Value)})
	}
	return out
}

// maxOperandLength is how much of a value a false condition compared is
// shown, as a whole response body may be one.
const maxOperandLength = 200

// operandText is an operand and its value on one line, as res.code = 500.
// A string is quoted, so that an empty one and one that ends with a newline
// can be seen, and anything else is written as JSON.
func operandText(o expr.Operand) string {
	var value string
	switch v := o.Value.(type) {
	case nil:
		value = "nil"
	case string:
		value = strconv.Quote(v)
	default:
		if data, err := json.Marshal(v); err == nil {
			value = string(data)
		} else {
			value = fmt.Sprintf("%v", v)
		}
	}
	if r := []rune(value); len(r) > maxOperandLength {
		value = string(r[:maxOperandLength]) + "..."
	}
	return o.Expr + " = " + value
}

// clauseSummary is a false condition on one line, for the message of a
// failure: res.code == 200 (res.code = 500).
func clauseSummary(c expr.Clause) string {
	if len(c.Operands) == 0 {
		return c.Expr
	}
	parts := make([]string, len(c.Operands))
	for i, o := range c.Operands {
		parts[i] = operandText(o)
	}
	return fmt.Sprintf("%s (%s)", c.Expr, strings.Join(parts, ", "))
}

// generateContractFailure formats a response that breaks its contract: each
// violation, then the request and response as a failed test shows them.
func (p *Printer) generateContractFailure(f *StepFailure) string {
	var b strings.Builder
	for _, v := range f.Violations {
		b.WriteString(indentDetail(p.masker.String(v.String())))
	}
	b.WriteString(p.generateTestFailure("", nil, f.Request, f.Response))
	return b.String()
}

// generateTestError formats test evaluation error output
func (p *Printer) generateTestError(testExpr string, err error) string {
	if p.verbose {
		p.LogError("Test Error: %s", err)
		p.LogError("Input: %s", testExpr)
	}
	return indentDetail(fmt.Sprintf("test error: %v", err))
}

// generateTestTypeMismatch formats test type mismatch error output
func (p *Printer) generateTestTypeMismatch(testExpr string, result any) string {
	txt := fmt.Sprintf("Test: `%s` = %v\n", testExpr, result)
	if p.verbose {
		p.LogDebug("%s", txt)
	}
	return indentDetail(txt)
}

// PrintTestResult prints test result in verbose mode
func (p *Printer) PrintTestResult(success bool, testExpr string, context any) {
	var resultStr string
	if success {
		resultStr = colorSuccess().Sprintf("Success")
	} else {
		resultStr = colorError().Sprintf("Failure")
	}
	// The context holds the request and response; show them masked.
	if sc, ok := context.(StepContext); ok {
		sc.Req = p.masker.Map(sc.Req)
		sc.Res = p.masker.Map(sc.Res)
		context = sc
	}
	p.LogDebug("Test: %s (input: %s, env: %s)", resultStr, testExpr, colorDim().Sprintf("%#v", context))
}

// PrintEchoContent prints echo content with proper indentation in verbose mode
func (p *Printer) PrintEchoContent(content string) {
	// Add indent to all lines, including after user-specified newlines
	indent := "       "
	lines := strings.SplitSeq(content, "\n")

	for line := range lines {
		p.LogDebug("%s%s", indent, line)
	}
}

// PrintRequestResponse prints request and response data with proper formatting
func (p *Printer) PrintRequestResponse(stepIdx int, stepName string, req, res map[string]any, rt string) {
	p.LogDebug("%s", colorWarning().Sprintf("--- Step %d: %s", stepIdx, stepName))
	p.LogDebug("Request:")
	p.PrintMapData(p.masker.Map(req))

	p.LogDebug("Response:")
	p.PrintMapData(p.masker.Map(res))

	p.LogDebug("RT: %s", colorInfo().Sprintf("%s", rt))
}

// PrintMapData prints map data with proper formatting for nested structures
func (p *Printer) PrintMapData(data map[string]any) {
	p.printMapDataRecursive(data, 1)
}

// printMapDataRecursive recursively prints map data with YAML-like indentation
func (p *Printer) printMapDataRecursive(data map[string]any, indentLevel int) {
	indent := strings.Repeat("  ", indentLevel)

	for k, v := range data {
		switch val := v.(type) {
		case map[string]any:
			if len(val) == 0 {
				p.LogDebug("%s%s: {}", indent, k)
			} else {
				p.LogDebug("%s%s:", indent, k)
				p.printMapDataRecursive(val, indentLevel+1)
			}
		case []any:
			if len(val) == 0 {
				p.LogDebug("%s%s: []", indent, k)
			} else {
				p.LogDebug("%s%s:", indent, k)
				p.printSliceRecursive(val, indentLevel+1)
			}
		case map[string]string:
			if len(val) == 0 {
				p.LogDebug("%s%s: {}", indent, k)
			} else {
				p.LogDebug("%s%s:", indent, k)
				for kk, vv := range val {
					p.LogDebug("%s  %s: %v", indent, kk, vv)
				}
			}
		case []string:
			if len(val) == 0 {
				p.LogDebug("%s%s: []", indent, k)
			} else {
				p.LogDebug("%s%s:", indent, k)
				for _, item := range val {
					p.LogDebug("%s- %v", indent+"  ", item)
				}
			}
		default:
			p.LogDebug("%s%s: %v", indent, k, v)
		}
	}
}

// printSliceRecursive recursively prints slice data with YAML-like indentation
func (p *Printer) printSliceRecursive(data []any, indentLevel int) {
	indent := strings.Repeat("  ", indentLevel)

	for _, v := range data {
		switch val := v.(type) {
		case map[string]any:
			if len(val) == 0 {
				p.LogDebug("%s- {}", indent)
			} else {
				p.LogDebug("%s-", indent)
				p.printMapDataRecursive(val, indentLevel+1)
			}
		case []any:
			if len(val) == 0 {
				p.LogDebug("%s- []", indent)
			} else {
				p.LogDebug("%s-", indent)
				p.printSliceRecursive(val, indentLevel+1)
			}
		case map[string]string:
			if len(val) == 0 {
				p.LogDebug("%s- {}", indent)
			} else {
				p.LogDebug("%s-", indent)
				for kk, vv := range val {
					p.LogDebug("%s  %s: %v", indent, kk, vv)
				}
			}
		case []string:
			if len(val) == 0 {
				p.LogDebug("%s- []", indent)
			} else {
				p.LogDebug("%s-", indent)
				for _, item := range val {
					p.LogDebug("%s  - %v", indent, item)
				}
			}
		default:
			p.LogDebug("%s- %v", indent, v)
		}
	}
}
