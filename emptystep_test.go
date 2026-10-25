package probe

import (
	"strings"
	"testing"
)

// emptySteps are workflows with an item under steps that holds nothing,
// which is decoded as no step, by the line it is on.
var emptySteps = []struct {
	name     string
	workflow string
	lines    []int
	// refused is what Load is refused for first.
	refused string
}{
	{
		name:     "the only step",
		workflow: "name: e\njobs:\n- name: J\n  steps:\n  -\n",
		lines:    []int{5},
		refused:  `job 0 "J", step 0: the step is empty`,
	},
	{
		name: "among other steps",
		workflow: `name: e
vars:
  url: http://localhost
jobs:
- name: J
  id: j
  defaults:
    http:
      url: "{{ vars.url }}"
  repeat:
    count: 2
    interval: 1s
    test: steps.first.success_rate == 100
  steps:
  - name: first
    id: first
    uses: http
    with:
      get: /
    retry:
      max_attempts: 2
    outputs:
      code: res.code
    test: res.code == 200
  -
  - name: last
    uses: http
    with:
      get: /{{ outputs.first.code }}
    echo: "{{ res.body }}"
    test: res.code == 200
  -
- name: K
  needs: [j]
  steps:
  - uses: shell
    with:
      cmd: echo {{ outputs.first.code }}
    test: res.code == 0
`,
		lines:   []int{25, 32},
		refused: `job 0 "J", step 1: the step is empty`,
	},
}

// TestCheck_EmptyStep reports an item under steps that holds nothing on
// its line, and checks the other steps as it does without it.
func TestCheck_EmptyStep(t *testing.T) {
	for _, tt := range emptySteps {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", tt.workflow)
			findings, err := Check(path, checkActions)
			if err != nil {
				t.Fatal(err)
			}
			var wants []want
			for _, l := range tt.lines {
				wants = append(wants, want{SeverityError, l, "the step is empty"})
			}
			checkFindings(t, findings, wants)
		})
	}
}

// TestLoad_EmptyStep refuses a workflow with an item under steps that
// holds nothing, which a run cannot run, rather than failing on it.
func TestLoad_EmptyStep(t *testing.T) {
	for _, tt := range emptySteps {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWorkflow(t, tt.workflow)
			if err == nil || !strings.Contains(err.Error(), tt.refused) {
				t.Errorf("Load() error = %v, want one saying %q", err, tt.refused)
			}
		})
	}
}
