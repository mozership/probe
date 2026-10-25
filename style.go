package probe

import "github.com/fatih/color"

// The colors and the icons of what probe prints: the report, the progress
// of a run and the log. They are shared by the printer, the renderer of the
// report and the reporters, so that a status looks the same wherever it is
// shown.

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

// statusIcon returns the icon of a step of status, as the report and the
// progress of a run show it, and its color. The icon ends with the space
// that sets it apart in the report; the progress trims it. A status the
// report does not know is shown by a question mark.
func statusIcon(status StatusType) (string, *color.Color) {
	switch status {
	case StatusSuccess:
		return IconSuccess, colorSuccess()
	case StatusError:
		return IconError, colorError()
	case StatusWarning:
		return IconTriangle, colorNoTest()
	case StatusSkipped:
		return IconSkip, colorInfo()
	default:
		return "?", colorWarning()
	}
}
