package probe

import (
	"strings"
	"testing"

	"github.com/mozership/probe/internal/textwidth"
)

func TestJobLabels(t *testing.T) {
	long := "Integration tests against the staging cluster"
	tests := []struct {
		name  string
		order []string
		names map[string]string
		want  map[string]string
	}{
		{
			name:  "a job is labelled by its name",
			order: []string{"job_0", "build"},
			names: map[string]string{"job_0": "Tests", "build": "Build app"},
			want:  map[string]string{"job_0": "Tests", "build": "Build app"},
		},
		{
			name:  "a job without a name is labelled by its id",
			order: []string{"job_0"},
			names: map[string]string{"job_0": ""},
			want:  map[string]string{"job_0": "job_0"},
		},
		{
			name:  "jobs named the same have their ids added",
			order: []string{"job_0", "job_1", "lint"},
			names: map[string]string{"job_0": "Tests", "job_1": "Tests", "lint": "Lint"},
			want:  map[string]string{"job_0": "Tests (job_0)", "job_1": "Tests (job_1)", "lint": "Lint"},
		},
		{
			name:  "a long name is cut short",
			order: []string{"job_0"},
			names: map[string]string{"job_0": long},
			want:  map[string]string{"job_0": "Integration tests again…"},
		},
		{
			name:  "long names cut to the same have their ids added",
			order: []string{"job_0", "job_1"},
			names: map[string]string{"job_0": long + " A", "job_1": long + " B"},
			want:  map[string]string{"job_0": "Integration tes… (job_0)", "job_1": "Integration tes… (job_1)"},
		},
		{
			name:  "a name keeps to one line",
			order: []string{"job_0"},
			names: map[string]string{"job_0": " Check\n  [api]\t "},
			want:  map[string]string{"job_0": "Check [api]"},
		},
		{
			name:  "a name keeps no format characters",
			order: []string{"job_0"},
			names: map[string]string{"job_0": "Check\u202e ipa\u200b"},
			want:  map[string]string{"job_0": "Check ipa"},
		},
		{
			name:  "a wide name is cut by the columns it takes",
			order: []string{"job_0"},
			names: map[string]string{"job_0": strings.Repeat("結合", 10)},
			want:  map[string]string{"job_0": strings.Repeat("結合", 5) + "結…"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobLabels(tt.order, func(id string) string { return tt.names[id] })
			for id, want := range tt.want {
				if got[id] != want {
					t.Errorf("label of %s = %q, want %q", id, got[id], want)
				}
				if w := textwidth.Width(got[id]); w > maxJobLabelWidth {
					t.Errorf("label of %s is %d columns wide, more than %d", id, w, maxJobLabelWidth)
				}
			}
		})
	}
}
