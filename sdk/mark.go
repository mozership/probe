package sdk

import "github.com/mozership/probe/internal/style"

// Mark is a status as Probe shows it, its icon in its color, for an action
// to write what it reports under its step as the rest of the run shows a
// status. It is colored when Probe prints to a terminal, which Probe tells
// the action by setting PROBE_TTY to 1, or when FORCE_COLOR is 1.
//
//	fmt.Fprintf(&b, "%s %s\n", sdk.MarkSuccess, "the page shows the name")
type Mark = style.Mark

const (
	// MarkSuccess is what passed: a green ✓.
	MarkSuccess = style.Success
	// MarkFailure is what failed: a red ✗.
	MarkFailure = style.Failure
	// MarkNoTest is what ran with nothing to check it: a blue △.
	MarkNoTest = style.NoTest
	// MarkSkipped is what was not run: a blue ⏭.
	MarkSkipped = style.Skipped
	// MarkWarning is what did not fail but should be looked into: a yellow ⚠︎.
	MarkWarning = style.Warning
	// MarkInfo is what is told for the reader to know: a blue ⏺.
	MarkInfo = style.Info
)

// NoteLevelMark is the Mark of a note of level: MarkFailure for "critical",
// MarkWarning for "warning" and MarkInfo for "info". A level that is none of
// these is MarkWarning.
func NoteLevelMark(level string) Mark {
	return style.NoteLevel(level)
}
