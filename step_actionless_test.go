package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mask"
)

// runActionless runs job with a runner that fails the test when an action
// is run, and returns the results of its steps.
func runActionless(t *testing.T, job *Job, vars map[string]any, guard actionrpc.Guard) []StepResult {
	t.Helper()
	printer := newBufferPrinter()
	printer.SetMasker(mask.New(nil, nil))
	result := NewResult()
	result.Jobs[job.ID] = &JobResult{JobID: job.ID}
	runner := runnerFunc(func(name string, _ actionrpc.Call, _ RunOptions) (map[string]any, map[string]any, error) {
		t.Errorf("the action %q ran", name)
		return nil, nil, nil
	})
	if _, err := job.run(JobContext{
		runEnv: &runEnv{
			Config:  Config{ActionRunner: runner, Guard: guard},
			Printer: printer,
			Result:  result,
		},
		Vars:    vars,
		Outputs: NewOutputs(),
	}); err != nil {
		t.Fatal(err)
	}
	return result.Jobs[job.ID].StepResults
}

// runnerFunc is an ActionRunner of a function.
type runnerFunc func(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error)

func (f runnerFunc) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	return f(name, call, opts)
}

// A step without uses runs no action: its test is checked against what it
// reads by functions, with status 0, and a step that only waits passes.
// The guard of the run has no action of it to refuse.
func TestStep_WithoutUses(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "app.log")
	if err := os.WriteFile(log, []byte("level=INFO msg=started\nCreate symlink a\nCreate symlink b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	job := &Job{ID: "verify", Name: "Verify", Steps: []*Step{
		{Name: "Started", Test: `status == 0 && file(vars.log) contains "msg=started"`},
		{Name: "Two symlinks", Test: `count(lines(file(vars.log)), # contains "Create symlink") == 2`, Outputs: map[string]string{"ok": "true"}},
		{Name: "No error", Test: `file(vars.log) contains "level=ERROR"`},
		{Name: "Settle", Wait: "10ms"},
		{Name: "Gone", Test: `!exists(vars.log)`},
	}}
	srs := runActionless(t, job, map[string]any{"log": log}, actionrpc.Guard{ReadOnly: true})

	want := []StatusType{StatusSuccess, StatusSuccess, StatusError, StatusSuccess, StatusError}
	if len(srs) != len(want) {
		t.Fatalf("step results = %+v, want %d", srs, len(want))
	}
	for i, w := range want {
		if srs[i].Status != w {
			t.Errorf("step %d %q = %v, want %v (failure %+v)", i, srs[i].Name, srs[i].Status, w, srs[i].Failure)
		}
	}
	if !srs[3].HasTest {
		t.Error("the step that only waits is counted as untested")
	}
}

// A step without uses retries its test until it holds, as one that waits
// for a file to appear needs.
func TestStep_WithoutUsesRetriesItsTest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready")
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(path, nil, 0o600)
	}()
	job := &Job{ID: "wait", Name: "Wait", Steps: []*Step{
		{Name: "Ready", Test: "exists(vars.path)", Retry: &StepRetry{MaxAttempts: 50, Interval: Interval{Duration: 20 * time.Millisecond}}},
	}}
	srs := runActionless(t, job, map[string]any{"path": path}, actionrpc.Guard{})
	if len(srs) != 1 || srs[0].Status != StatusSuccess {
		t.Fatalf("step results = %+v, want one that passed", srs)
	}
	if srs[0].RetryAttempt < 2 {
		t.Errorf("RetryAttempt = %d, want the test retried", srs[0].RetryAttempt)
	}
}

// A step without uses that is given what only an action takes, or nothing
// to do, is refused as the workflow is loaded.
func TestProbe_Load_RefusesAStepWithoutUses(t *testing.T) {
	tests := []struct {
		name    string
		step    string
		refused string
	}{
		{name: "with", step: "    test: 'true'\n    with:\n      cmd: ls\n", refused: `step 0 "S": with: the step has no uses to give it to`},
		{name: "nothing to do", step: "    echo: hi\n", refused: `step 0 "S": the step has no uses, and nothing to do without one`},
		{name: "retry without a test", step: "    wait: 1s\n    retry:\n      max_attempts: 3\n      interval: 1s\n", refused: `step 0 "S": retry: a step without uses runs nothing to retry`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeWorkflow(t, t.TempDir(), "workflow.yml", "name: w\njobs:\n- name: J\n  steps:\n  - name: S\n"+tt.step)
			err := New(path, false).Do()
			if err == nil || !strings.Contains(err.Error(), tt.refused) {
				t.Errorf("Do() error = %v, want it to say %q", err, tt.refused)
			}
		})
	}
}

// probe check takes a step without uses as running no action: no unknown
// action, and no warning of a test that reads no response, as there is
// none. Reading res or req, which are empty, is an error.
func TestCheck_StepWithoutUses(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: w
vars:
  log: /var/log/app.log
jobs:
- name: J
  steps:
  - name: Wait
    wait: 1s
  - name: Started
    test: file(vars.log) contains "started"
    retry:
      max_attempts: 3
      interval: 1s
  - name: Gone
    test: '!exists("/run/app.sock")'
  - name: Reads res
    test: res.code == 200
    echo: "{{ req.url }}"
`)
	findings, err := Check(path, checkActions)
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 17, "test: res is empty here, as the step has no uses to run"},
		{SeverityError, 18, "echo: req is empty here, as the step has no uses to run"},
	})
}
