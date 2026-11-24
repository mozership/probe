package display

import (
	"github.com/fatih/color"

	"github.com/mozership/probe/internal/outcome"
	"github.com/mozership/probe/internal/style"
)

// The colors and the icons of what probe prints: the report, the progress
// of a run and the log. They are shared by the printer, the renderer of the
// report and the reporters, so that a status looks the same wherever it is
// shown. The colors and the icons of the statuses are those of the style
// package, which an action uses for what it reports under its step.

// colorSuccess returns a *color.Color for success (RGB 0,175,0)
func colorSuccess() *color.Color {
	return style.Success.Color()
}

// colorError returns a *color.Color for errors (red)
func colorError() *color.Color {
	return style.Failure.Color()
}

// colorInfo returns a *color.Color for info messages (blue)
func colorInfo() *color.Color {
	return style.Skipped.Color()
}

// ColorDim returns a *color.Color for subdued text
func ColorDim() *color.Color {
	return color.New(color.FgHiBlack)
}

// ColorWarning returns a *color.Color for warnings (yellow)
func ColorWarning() *color.Color {
	return style.Warning.Color()
}

// colorSkipped returns a *color.Color for skipped items (gray)
func colorSkipped() *color.Color {
	return color.New(color.FgHiBlack)
}

func colorNoTest() *color.Color {
	return style.NoTest.Color()
}

// Icon constants. Those of the statuses are the icons of the marks of the
// style package, followed by a space.
const (
	IconSuccess  = "✓ "
	IconError    = "✗ "
	IconTriangle = "△ "
	IconCircle   = "⏺"
	IconWait     = "🕐︎"
	IconSkip     = "⏭ "
	IconRetry    = "⟳"
	// IconStepStart marks a step that started, in the progress of a run.
	IconStepStart = "▸"
)

// statusIcon returns the icon of a step of status, as the report and the
// progress of a run show it, and its color. The icon ends with the space
// that sets it apart in the report; the progress trims it. A status the
// report does not know is shown by a question mark.
func statusIcon(status outcome.Status) (string, *color.Color) {
	var m style.Mark
	switch status {
	case outcome.StatusSuccess:
		m = style.Success
	case outcome.StatusError:
		m = style.Failure
	case outcome.StatusWarning:
		m = style.NoTest
	case outcome.StatusSkipped:
		m = style.Skipped
	default:
		return "?", ColorWarning()
	}
	return m.Icon() + " ", m.Color()
}
