package probe

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/mozership/probe/internal/dump"
	"github.com/mozership/probe/mask"
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
func NewPrinter(verbose bool) *Printer {
	if os.Getenv("FORCE_COLOR") == "1" || os.Getenv("PROBE_TTY") == "1" {
		color.NoColor = false
	}

	s := spinner.New(spinner.CharSets[9], 100*time.Millisecond)
	return &Printer{
		verbose:   verbose,
		spinner:   s,
		outWriter: os.Stdout,
		errWriter: os.Stderr,
	}
}

func newBufferPrinter() *Printer {
	pr := NewPrinter(false)
	pr.outWriter = new(bytes.Buffer)
	pr.errWriter = new(bytes.Buffer)
	// Tests do not render a live spinner; nil here makes the spinner
	// helpers no-op so that `go test -race` does not flag the
	// reader-in-library / writer-in-probe contention on spinner.Suffix.
	pr.spinner = nil
	return pr
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

// logTestError logs, in verbose mode, the error a test could not be
// evaluated for.
func (p *Printer) logTestError(testExpr string, err error) {
	if p.verbose {
		p.LogError("Test Error: %s", err)
		p.LogError("Input: %s", testExpr)
	}
}

// PrintTestResult prints test result in verbose mode
func (p *Printer) PrintTestResult(success bool, testExpr string, context any) {
	var resultStr string
	if success {
		resultStr = colorSuccess().Sprintf("Success")
	} else {
		resultStr = colorError().Sprintf("Failure")
	}
	p.LogDebug("Test: %s (input: %s)", resultStr, testExpr)
	if env := testEnv(context); env != nil {
		p.LogDebug("Env:")
		p.PrintMapData(p.masker.Map(env))
	}
}

// testEnv is what PrintTestResult shows of the context a test was evaluated
// in, by the names the test reads it by. Of a step's, the request and the
// response are left out, as the step has just shown them, under Request and
// Response.
func testEnv(context any) map[string]any {
	switch c := context.(type) {
	case StepContext:
		return map[string]any{
			"vars":         c.Vars,
			"outputs":      c.Outputs,
			"status":       c.Status,
			"rt":           map[string]any{"duration": c.RT.Duration, "sec": c.RT.Sec},
			"report":       c.Report,
			"repeat_index": c.RepeatIndex,
		}
	case map[string]any:
		return c
	default:
		return nil
	}
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

// PrintGivenRequest prints, for a step whose action failed without
// returning the request it made, the parameters the step gave the action,
// its templates evaluated, in place of the request and the response.
func (p *Printer) PrintGivenRequest(stepIdx int, stepName string, with map[string]any) {
	p.LogDebug("%s", colorWarning().Sprintf("--- Step %d: %s", stepIdx, stepName))
	p.LogDebug("Request (as the step gave it; the action returned none):")
	p.PrintMapData(p.masker.Map(with))
}

// PrintMapData prints map data in verbose mode as YAML-like lines, its keys
// sorted, and all of it, as it is: nothing is cut or left out.
func (p *Printer) PrintMapData(data map[string]any) {
	for _, l := range dump.Lines(data, dump.Options{}) {
		p.LogDebug("  %s", l)
	}
}
