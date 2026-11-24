package engine

import (
	"bytes"

	"github.com/mozership/probe/internal/display"
)

// Printer writes what a run prints: its report, its errors and warnings,
// and, in verbose mode, what its steps did.
type Printer = display.Printer

// NewPrinter returns a printer that writes to the standard output and
// error. Its output is colored whatever it is written to when FORCE_COLOR or
// PROBE_TTY is 1: the CLI sets PROBE_TTY when its standard output is a
// terminal, for the printer of an embedded job, which runs in the process
// of an action, whose standard output is not the terminal.
func NewPrinter(verbose bool) *Printer {
	return display.NewPrinter(verbose)
}

// OutputMode selects how a workflow run delivers its report.
type OutputMode = display.Mode

// The modes a report can be written in.
const (
	// OutputModeAuto picks spinner on an interactive terminal and stream
	// otherwise.
	OutputModeAuto = display.ModeAuto
	// OutputModeSpinner shows a spinner while running and prints the whole
	// report once every job has finished.
	OutputModeSpinner = display.ModeSpinner
	// OutputModeStream prints each job block as soon as it is final and logs
	// step progress to stderr while jobs are still running.
	OutputModeStream = display.ModeStream
)

// ParseOutputMode converts a user supplied name into an OutputMode.
func ParseOutputMode(s string) (OutputMode, error) {
	return display.ParseMode(s)
}

// newBufferPrinter returns a printer that writes to buffers and shows no
// spinner, for tests.
func newBufferPrinter() *Printer {
	return display.NewPrinterTo(false, new(bytes.Buffer), new(bytes.Buffer))
}
