package style_test

import (
	"testing"

	"github.com/fatih/color"

	"github.com/mozership/probe/internal/style"
	"github.com/mozership/probe/sdk"
)

// noColor runs the test with color.NoColor set to v and FORCE_COLOR and
// PROBE_TTY unset.
func noColor(t *testing.T, v bool) {
	t.Helper()
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("PROBE_TTY", "")
	was := color.NoColor
	color.NoColor = v
	t.Cleanup(func() { color.NoColor = was })
}

func TestIcon(t *testing.T) {
	tests := map[style.Mark]string{
		style.Success:  "✓",
		style.Failure:  "✗",
		style.NoTest:   "△",
		style.Skipped:  "⏭",
		style.Warning:  "⚠︎",
		style.Info:     "⏺",
		style.Mark(99): "?",
	}
	for m, want := range tests {
		if got := m.Icon(); got != want {
			t.Errorf("Mark(%d).Icon() = %q, want %q", m, got, want)
		}
	}
}

func TestColor(t *testing.T) {
	tests := map[style.Mark]*color.Color{
		style.Success:  color.RGB(0, 175, 0),
		style.Failure:  color.New(color.FgRed),
		style.NoTest:   color.RGB(0, 102, 204),
		style.Skipped:  color.New(color.FgBlue),
		style.Warning:  color.New(color.FgYellow),
		style.Info:     color.New(color.FgBlue),
		style.Mark(99): color.New(color.FgYellow),
	}
	for m, want := range tests {
		if got := m.Color(); !got.Equals(want) {
			t.Errorf("Mark(%d).Color() = %v, want %v", m, got, want)
		}
	}
}

// Without a terminal, a mark is its icon alone, which a test of an action
// can compare.
func TestStringPlain(t *testing.T) {
	noColor(t, true)
	if got := style.Success.String(); got != "✓" {
		t.Errorf("Success.String() = %q, want %q", got, "✓")
	}
	if got := style.Failure.Paint("fail"); got != "fail" {
		t.Errorf("Failure.Paint(%q) = %q, want it as it is", "fail", got)
	}
}

func TestStringColored(t *testing.T) {
	noColor(t, false)
	if got, want := style.Failure.String(), "\x1b[31m✗\x1b[0m"; got != want {
		t.Errorf("Failure.String() = %q, want %q", got, want)
	}
}

// An action runs with no terminal of its own, so color.NoColor is true in
// its process, and colors by PROBE_TTY, which Probe sets when it prints to
// a terminal, or by FORCE_COLOR.
func TestColoredByEnvironment(t *testing.T) {
	for _, env := range []string{"PROBE_TTY", "FORCE_COLOR"} {
		t.Run(env, func(t *testing.T) {
			noColor(t, true)
			t.Setenv(env, "1")
			if !style.Colored() {
				t.Fatalf("Colored() = false with %s=1", env)
			}
			if got, want := style.Failure.String(), "\x1b[31m✗\x1b[0m"; got != want {
				t.Errorf("Failure.String() = %q, want %q", got, want)
			}
			// The mark does not change how other colors print.
			if !color.NoColor {
				t.Error("color.NoColor was changed")
			}
		})
	}
}

func TestColored(t *testing.T) {
	noColor(t, true)
	if style.Colored() {
		t.Error("Colored() = true with color.NoColor and neither PROBE_TTY nor FORCE_COLOR")
	}
	noColor(t, false)
	if !style.Colored() {
		t.Error("Colored() = false without color.NoColor")
	}
}

func TestNoteLevel(t *testing.T) {
	tests := map[string]style.Mark{
		sdk.NoteCritical: style.Failure,
		sdk.NoteWarning:  style.Warning,
		sdk.NoteInfo:     style.Info,
		"unknown":        style.Warning,
	}
	for level, want := range tests {
		if got := style.NoteLevel(level); got != want {
			t.Errorf("NoteLevel(%q) = %d, want %d", level, got, want)
		}
	}
}
