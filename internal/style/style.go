// Package style is how Probe shows a status in a terminal: its icon and its
// color. Probe prints a run with it, and an action writes what it reports
// under its step with it through the sdk package, so that a status looks
// the same there as in the rest of the run.
//
// An action runs in a process of its own, whose standard output is not the
// terminal, so it would print no color by itself. Probe sets PROBE_TTY to 1
// in the environment of the action when it prints to a terminal, and a mark
// is colored then, as it is when FORCE_COLOR is 1.
package style

import (
	"os"

	"github.com/fatih/color"
)

// Mark is a status as Probe shows it.
type Mark int

const (
	// Success is what passed: a green ✓.
	Success Mark = iota
	// Failure is what failed: a red ✗.
	Failure
	// NoTest is what ran with nothing to check it: a blue △.
	NoTest
	// Skipped is what was not run: a blue ⏭.
	Skipped
	// Warning is what did not fail but should be looked into: a yellow ⚠︎.
	Warning
	// Info is what is told for the reader to know: a blue ⏺.
	Info
)

// marks are the icon and the color of each Mark.
var marks = map[Mark]struct {
	icon  string
	color func() *color.Color
}{
	Success: {"✓", func() *color.Color { return color.RGB(0, 175, 0) }},
	Failure: {"✗", func() *color.Color { return color.New(color.FgRed) }},
	NoTest:  {"△", func() *color.Color { return color.RGB(0, 102, 204) }},
	Skipped: {"⏭", func() *color.Color { return color.New(color.FgBlue) }},
	Warning: {"⚠︎", func() *color.Color { return color.New(color.FgYellow) }},
	Info:    {"⏺", func() *color.Color { return color.New(color.FgBlue) }},
}

// Icon is the icon of m, without color, or "?" for a Mark that is none of
// the above.
func (m Mark) Icon() string {
	if s, ok := marks[m]; ok {
		return s.icon
	}
	return "?"
}

// Color is the color of m, yellow for a Mark that is none of the above. It
// colors what it prints by color.NoColor, as Probe's own printer, which
// sets it, does; Paint and String color when FORCE_COLOR or PROBE_TTY is 1,
// or when color.NoColor is false.
func (m Mark) Color() *color.Color {
	if s, ok := marks[m]; ok {
		return s.color()
	}
	return color.New(color.FgYellow)
}

// Paint is s in the color of m when Colored, and s as it is otherwise.
func (m Mark) Paint(s string) string {
	c := m.Color()
	if Colored() {
		c.EnableColor()
	} else {
		c.DisableColor()
	}
	return c.Sprint(s)
}

// String is the icon of m in its color when Colored.
func (m Mark) String() string {
	return m.Paint(m.Icon())
}

// NoteLevel is the Mark of a note of level, as the sdk package names the
// levels: Failure for "critical", Warning for "warning" and Info for
// "info". A level that is none of these is Warning.
func NoteLevel(level string) Mark {
	switch level {
	case "critical":
		return Failure
	case "info":
		return Info
	default:
		return Warning
	}
}

// Colored tells whether what is printed is colored: when FORCE_COLOR or
// PROBE_TTY is 1, and otherwise when color.NoColor is false, as it is when
// the standard output of the process is a terminal and NO_COLOR is not set.
func Colored() bool {
	if os.Getenv("FORCE_COLOR") == "1" || os.Getenv("PROBE_TTY") == "1" {
		return true
	}
	return !color.NoColor
}
