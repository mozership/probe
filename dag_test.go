package probe

import (
	"reflect"
	"testing"

	"github.com/mozership/probe/dag"
)

func TestWorkflow_Graph(t *testing.T) {
	w := &Workflow{
		Jobs: []Job{
			{Name: "Build", ID: "build", Steps: []*Step{
				{Name: "Compile", Uses: "shell"},
				{Uses: "http"},
			}},
			{Name: "Test", Needs: []string{"build"}, Steps: []*Step{
				{Name: "Without path", Uses: "embedded"},
			}},
		},
	}

	want := dag.Graph{Jobs: []dag.Job{
		{ID: "build", Name: "Build", Steps: []dag.Step{
			{Name: "Compile"},
			{Name: "http"},
		}},
		{ID: "Test", Name: "Test", Needs: []string{"build"}, Steps: []dag.Step{
			{Name: "Without path", Embedded: true},
		}},
	}}
	if got := w.Graph(); !reflect.DeepEqual(got, want) {
		t.Errorf("Graph() = %#v\nwant %#v", got, want)
	}
}

func TestWorkflow_Graph_Embedded(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]any
		path string
		want dag.Step
	}{
		{
			name: "path relative to the working directory",
			path: "./testdata/embedded-success-job.yml",
			want: dag.Step{
				Name:          "Run job",
				Embedded:      true,
				EmbeddedFile:  "embedded-success-job.yml",
				EmbeddedSteps: []string{"Simple success step", "Another success step"},
			},
		},
		{
			name: "path from the workflow's vars",
			vars: map[string]any{"job": "embedded-success-job.yml"},
			path: "./testdata/{{vars.job}}",
			want: dag.Step{
				Name:          "Run job",
				Embedded:      true,
				EmbeddedFile:  "embedded-success-job.yml",
				EmbeddedSteps: []string{"Simple success step", "Another success step"},
			},
		},
		{
			name: "file that does not exist",
			path: "./missing.yml",
			want: dag.Step{Name: "Run job", Embedded: true},
		},
		// The embedded action does not look next to the workflow file, so the
		// graph does not either: it would show steps that never run.
		{
			name: "file next to the workflow only",
			path: "./embedded-success-job.yml",
			want: dag.Step{Name: "Run job", Embedded: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Workflow{
				basePath: "testdata",
				Vars:     tt.vars,
				Jobs: []Job{{Name: "Deploy", Steps: []*Step{
					{Name: "Run job", Uses: "embedded", With: map[string]any{"path": tt.path}},
				}}},
			}
			if got := w.Graph().Jobs[0].Steps[0]; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("step = %#v\nwant %#v", got, tt.want)
			}
		})
	}
}
