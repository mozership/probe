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
