package probe

import (
	"reflect"
	"strings"
	"testing"
)

// TestProbe_load returns what is wrong with the names and the jobs, each of
// them, rather than failing on the first, and Load fails on the first.
func TestProbe_load(t *testing.T) {
	const names = "actions:\n  http: ./greet\n"
	tests := []struct {
		name         string
		workflow     string
		wantAliases  int
		wantProblems []string
		wantErr      string
		wantLoadErr  string
	}{
		{
			name:         "a wrong name and tests that do not parse",
			workflow:     "name: w\n" + names + "jobs:\n- name: J\n  steps:\n  - uses: http\n    test: res.code ==\n  - uses: http\n    test: res.code ==\n",
			wantAliases:  1,
			wantProblems: []string{"jobs[0].steps[0].test", "jobs[0].steps[1].test"},
			wantLoadErr:  `the name "http" is that of an action of Probe`,
		},
		{
			name:         "a test that does not parse",
			workflow:     "name: w\njobs:\n- name: J\n  steps:\n  - uses: http\n    test: res.code ==\n",
			wantProblems: []string{"jobs[0].steps[0].test"},
			wantLoadErr:  "the workflow cannot run as it is written",
		},
		{
			name:        "a wrong name before an ID written twice",
			workflow:    "name: w\n" + names + "jobs:\n- name: A\n  id: a\n  steps:\n  - uses: http\n- name: B\n  id: a\n  steps:\n  - uses: http\n",
			wantAliases: 1,
			wantErr:     "duplicate job ID 'a'",
			wantLoadErr: `the name "http" is that of an action of Probe`,
		},
		{
			name:        "a text that does not decode",
			workflow:    "name: [\n",
			wantErr:     "failed to decode YAML workflow",
			wantLoadErr: "failed to decode YAML workflow",
		},
		{
			name:     "nothing wrong",
			workflow: "name: w\njobs:\n- name: J\n  steps:\n  - uses: http\n    test: res.code == 200\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", tt.workflow)
			p := &Probe{Config: Config{Actions: builtinNames}}
			aliases, problems, err := p.load([]string{path}, tt.workflow)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("load() error = %v, want %q", err, tt.wantErr)
			}
			if len(aliases) != tt.wantAliases {
				t.Errorf("load() aliases = %v, want %d", aliases, tt.wantAliases)
			}
			var paths []string
			for _, pr := range problems {
				paths = append(paths, pr.path)
			}
			if !reflect.DeepEqual(paths, tt.wantProblems) {
				t.Errorf("load() problems at %v, want %v", paths, tt.wantProblems)
			}

			err = (&Probe{FilePath: path, Config: Config{Actions: builtinNames}}).Load()
			if tt.wantLoadErr == "" && err != nil || tt.wantLoadErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantLoadErr)) {
				t.Errorf("Load() error = %v, want %q", err, tt.wantLoadErr)
			}
		})
	}
}
