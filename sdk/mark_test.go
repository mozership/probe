package sdk

import "testing"

func TestMarks(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("PROBE_TTY", "")
	for _, tt := range []struct {
		mark Mark
		icon string
	}{
		{MarkSuccess, "✓"},
		{MarkFailure, "✗"},
		{MarkNoTest, "△"},
		{MarkSkipped, "⏭"},
		{MarkWarning, "⚠︎"},
		{MarkInfo, "⏺"},
	} {
		if got := tt.mark.Icon(); got != tt.icon {
			t.Errorf("Icon() = %q, want %q", got, tt.icon)
		}
	}
}

func TestNoteLevelMark(t *testing.T) {
	for level, want := range map[string]Mark{
		"critical": MarkFailure,
		"warning":  MarkWarning,
		"info":     MarkInfo,
		"other":    MarkWarning,
	} {
		if got := NoteLevelMark(level); got != want {
			t.Errorf("NoteLevelMark(%q) = %v, want %v", level, got.Icon(), want.Icon())
		}
	}
}
