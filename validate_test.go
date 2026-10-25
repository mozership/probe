package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A workflow a job of which cannot run as it is written is refused as it is
// loaded, with all that is wrong, and before any job runs.
func TestProbe_Load_RefusesWhatCannotRun(t *testing.T) {
	dir := t.TempDir()
	ran := filepath.Join(dir, "ran")
	path := writeWorkflow(t, dir, "workflow.yml", `name: refused
jobs:
- name: First
  steps:
  - name: Touch
    uses: shell
    with:
      cmd: touch `+ran+`
    test: res.code == 0
- name: Second
  needs: [job_0]
  steps:
  - name: Bad id
    id: Bad.Id
    uses: hello
    test: res.code == 0
  - name: Bad test
    uses: hello
    test: res.code = 200
  - name: Bad wait
    uses: hello
    wait: soon
    test: res.code == 0
`)

	p := New(path, false)
	err := p.Do()
	if err == nil {
		t.Fatal("Do() succeeded")
	}
	for _, msg := range []string{
		`job 1 "Second": step 0: invalid step ID 'Bad.Id'`,
		`job 1 "Second", step 1 "Bad test": test:`,
		`job 1 "Second", step 2 "Bad wait": wait: invalid wait format: soon`,
	} {
		if !strings.Contains(err.Error(), msg) {
			t.Errorf("Do() error = %v, want it to say %q", err, msg)
		}
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("the first job ran")
	}
}

// An item under steps that holds nothing is refused as the workflow is
// loaded, on its own and among other steps, rather than failed on.
func TestProbe_Load_RefusesAnEmptyStep(t *testing.T) {
	tests := []struct {
		name     string
		workflow string
		refused  string
	}{
		{
			name:     "the only step",
			workflow: "name: e\njobs:\n- name: J\n  steps:\n  -\n",
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
			refused: `job 0 "J", step 1: the step is empty`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadWorkflow(t, tt.workflow)
			if err == nil || !strings.Contains(err.Error(), tt.refused) {
				t.Errorf("Load() error = %v, want one saying %q", err, tt.refused)
			}
		})
	}
}

// What a template gives is known only when the step runs, so a wait that is
// a template is not checked as a time when the workflow is loaded.
func TestProbe_Load_WaitOfATemplate(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: wait
vars:
  wait: 0s
jobs:
- name: Wait
  steps:
  - uses: hello
    wait: "{{ vars.wait }}"
    test: res.code == 0
`)
	if err := New(path, false).Load(); err != nil {
		t.Errorf("Load() error = %v", err)
	}
}

// A workflow made in code is refused when it starts, before any job runs.
func TestWorkflow_Start_RefusesWhatCannotRun(t *testing.T) {
	runner := NewMockActionRunner()
	w := &Workflow{
		Name: "made in code",
		Jobs: []Job{
			{Name: "first", Steps: []*Step{{Name: "ok", Uses: "hello", actionRunner: runner}}},
			{Name: "second", Steps: []*Step{{Name: "broken", Uses: "hello", SkipIf: "(", actionRunner: runner}}},
		},
		printer: newBufferPrinter(),
	}
	err := w.Start(Config{})
	if err == nil || !strings.Contains(err.Error(), `job 1 "second", step 0 "broken": skipif:`) {
		t.Fatalf("Start() error = %v, want the skipif refused", err)
	}
	if len(runner.Calls) > 0 {
		t.Errorf("actions ran for %v, want none", runner.Calls)
	}
}

func TestWaitProblems(t *testing.T) {
	tests := []struct {
		wait string
		want string
	}{
		{wait: ""},
		{wait: "500ms"},
		{wait: "3"},
		{wait: "{{ vars.wait }}"},
		{wait: "soon", want: "wait: invalid wait format: soon"},
		{wait: "{{ vars. }}", want: "wait: template {{ vars. }}"},
	}
	for _, tt := range tests {
		t.Run(tt.wait, func(t *testing.T) {
			got := waitProblems("wait", "", tt.wait)
			if tt.want == "" {
				if len(got) > 0 {
					t.Errorf("waitProblems() = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0].Error(), tt.want) {
				t.Errorf("waitProblems() = %v, want one saying %q", got, tt.want)
			}
		})
	}
}

// A loaded step without an id keeps none, and an action is told it by where
// it is written, as a step made in code is.
func TestProbe_Load_StepWithoutAnID(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: ids
jobs:
- name: Steps
  steps:
  - uses: hello
    test: res.code == 0
`)
	p := New(path, false)
	if err := p.Load(); err != nil {
		t.Fatal(err)
	}
	st := p.workflow.Jobs[0].Steps[0]
	if st.ID != "" {
		t.Errorf("ID = %q, want none", st.ID)
	}
	if got := newStepRun(st, nil).repeatID(); got != "step_0" {
		t.Errorf("repeatID() = %q, want step_0", got)
	}
}

// The outputs of a step without an id are read by their names alone, by the
// steps of later jobs too, and not by where the step is written.
func TestWorkflow_OutputsOfAStepWithoutAnID(t *testing.T) {
	runner := NewMockActionRunner()
	runner.SetResult("publish", map[string]any{"res": map[string]any{"pid": 42}, "status": 0})
	w := &Workflow{
		Name: "outputs",
		Jobs: []Job{
			{ID: "start", Name: "Start", Steps: []*Step{
				{Name: "Publish", Uses: "publish", Test: "res.pid == 42", Outputs: map[string]string{"pid": "res.pid"}, actionRunner: runner},
			}},
			{ID: "use", Name: "Use", Needs: []string{"start"}, Steps: []*Step{
				{Name: "Read", Uses: "read", Test: "outputs.pid == 42 && outputs.step_0 == nil", actionRunner: runner},
			}},
		},
		printer: newBufferPrinter(),
	}
	if err := w.Start(Config{}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if w.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d: the outputs were not read by name", w.exitStatus, ExitOK)
	}
}
