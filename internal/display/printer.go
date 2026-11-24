package display

import (
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
	"github.com/mozership/probe/style"
)

// Printer implements PrintWriter for console print
type Printer struct {
	verbose   bool
	spinner   *spinner.Spinner
	outWriter io.Writer
	errWriter io.Writer
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

// NewPrinter creates a new console print writer. Its output is colored
// whatever it is written to when FORCE_COLOR or PROBE_TTY is 1: the CLI sets
// PROBE_TTY when its standard output is a terminal, for the printer of an
// embedded job: that one runs in the process of an action, whose standard
// output is not the terminal, and what it writes is printed by the run.
func NewPrinter(verbose bool) *Printer {
	if style.Colored() {
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

// NewPrinterTo returns a printer that writes to out and err and shows no
// spinner, for a run whose output is captured, as a test captures it.
func NewPrinterTo(verbose bool, out, err io.Writer) *Printer {
	p := NewPrinter(verbose)
	p.spinner = nil
	p.outWriter = out
	p.errWriter = err
	return p
}

// Verbose reports whether the printer writes the verbose output of a run.
func (p *Printer) Verbose() bool {
	return p.verbose
}

// SetVerbose sets whether the printer writes the verbose output of a run.
func (p *Printer) SetVerbose(v bool) {
	p.verbose = v
}

// Out is where the printer writes the report of a run.
func (p *Printer) Out() io.Writer {
	return p.outWriter
}

// Err is where the printer writes errors, warnings and progress.
func (p *Printer) Err() io.Writer {
	return p.errWriter
}

// SetOut sets where the printer writes the report of a run.
func (p *Printer) SetOut(w io.Writer) {
	p.outWriter = w
}

// SetErr sets where the printer writes errors, warnings and progress.
func (p *Printer) SetErr(w io.Writer) {
	p.errWriter = w
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
	// The spinner's render goroutine reads Suffix under the spinner's own
	// lock, and async repeat sets it from several steps at once, so every
	// write takes that same lock.
	suffix := fmt.Sprintf(" %s...", p.masker.String(txt))
	p.spinner.Lock()
	p.spinner.Suffix = suffix
	p.spinner.Unlock()
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
		fmt.Fprintf(os.Stderr, "Fprint: %v\n", err)
	}
}

func (p *Printer) Fprintf(w io.Writer, f string, a ...any) {
	if err := p.write(w, fmt.Sprintf(f, a...)); err != nil {
		fmt.Fprintf(os.Stderr, "Fprintf: %v\n", err)
	}
}

func (p *Printer) Fprintln(w io.Writer, a ...any) {
	if err := p.write(w, fmt.Sprintln(a...)); err != nil {
		fmt.Fprintf(os.Stderr, "Fprintln: %v\n", err)
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
	return debugLine(format, args...)
}

// debugLine returns a line of verbose output, as LogDebug writes it.
func debugLine(format string, args ...any) string {
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

// LogError prints error messages to the error writer
func (p *Printer) LogError(format string, args ...any) {
	p.Fprint(p.errWriter, p.generateLogError(format, args...))
}

// LogWarn prints a warning to the error writer: something the user should
// know about that does not make the run fail.
func (p *Printer) LogWarn(format string, args ...any) {
	p.Fprint(p.errWriter, WarnLine(fmt.Sprintf(format, args...)))
}

// WarnLine returns msg as a line of warning, as LogWarn writes it, for what
// warns without a Printer, such as Load.
func WarnLine(msg string) string {
	return ColorWarning().Sprintf("[WARN] %s", msg) + "\n"
}

// LogTestError logs, in verbose mode, the error a test could not be
// evaluated for.
func (p *Printer) LogTestError(testExpr string, err error) {
	if p.verbose {
		p.Fprint(p.errWriter, p.generateLogError("Test Error: %s", err)+p.generateLogError("Input: %s", testExpr))
	}
}

// PrintTestResult prints, in verbose mode, what the test testExpr came to,
// with env, the names it was evaluated with, when it is not nil.
func (p *Printer) PrintTestResult(success bool, testExpr string, env map[string]any) {
	var resultStr string
	if success {
		resultStr = colorSuccess().Sprintf("Success")
	} else {
		resultStr = colorError().Sprintf("Failure")
	}
	var b DebugBlock
	b.Line("Test: %s (input: %s)", resultStr, testExpr)
	if env != nil {
		b.Line("Env:")
		b.mapData(p.masker.Map(env))
	}
	p.LogDebugBlock(&b)
}

// DebugBlock collects the lines of a block of verbose output, such as the
// request and the response of a step, so that the block is written at once.
// Jobs run concurrently, and a block written a line at a time would have
// the lines of another job land inside it.
type DebugBlock struct {
	strings.Builder
}

// line adds a line to the block, as LogDebug writes it.
func (b *DebugBlock) Line(format string, args ...any) {
	b.WriteString(debugLine(format, args...))
}

// mapData adds the lines PrintMapData writes of data to the block.
func (b *DebugBlock) mapData(data map[string]any) {
	for _, l := range dump.Lines(data, dump.Options{}) {
		b.Line("  %s", l)
	}
}

// LogDebugBlock writes a block of verbose output in one write, only in
// verbose mode.
func (p *Printer) LogDebugBlock(b *DebugBlock) {
	if p.verbose && b.Len() > 0 {
		p.Fprint(p.errWriter, b.String())
	}
}

// PrintEchoContent prints echo content with proper indentation in verbose mode
func (p *Printer) PrintEchoContent(content string) {
	// Add indent to all lines, including after user-specified newlines
	indent := "       "
	var b DebugBlock
	for line := range strings.SplitSeq(content, "\n") {
		b.Line("%s%s", indent, line)
	}
	p.LogDebugBlock(&b)
}

// PrintRequestResponse prints request and response data with proper formatting
func (p *Printer) PrintRequestResponse(stepIdx int, stepName string, req, res map[string]any, rt string) {
	var b DebugBlock
	b.Line("%s", ColorWarning().Sprintf("--- Step %d: %s", stepIdx, stepName))
	b.Line("Request:")
	b.mapData(p.masker.Map(req))

	b.Line("Response:")
	b.mapData(p.masker.Map(res))

	b.Line("RT: %s", colorInfo().Sprintf("%s", rt))
	p.LogDebugBlock(&b)
}

// PrintGivenRequest prints, for a step whose action failed without
// returning the request it made, the parameters the step gave the action,
// its templates evaluated, in place of the request and the response.
func (p *Printer) PrintGivenRequest(stepIdx int, stepName string, with map[string]any) {
	var b DebugBlock
	b.Line("%s", ColorWarning().Sprintf("--- Step %d: %s", stepIdx, stepName))
	b.Line("Request (as the step gave it; the action returned none):")
	b.mapData(p.masker.Map(with))
	p.LogDebugBlock(&b)
}

// PrintMapData prints map data in verbose mode as YAML-like lines, its keys
// sorted, and all of it, as it is: nothing is cut or left out.
func (p *Printer) PrintMapData(data map[string]any) {
	var b DebugBlock
	b.mapData(data)
	p.LogDebugBlock(&b)
}
