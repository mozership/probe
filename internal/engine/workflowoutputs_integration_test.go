package engine

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/display"
	"github.com/mozership/probe/internal/report"
)

// runForOutputs runs w with runner and returns the outputs its json report
// holds, its exit status and what it wrote to the standard error.
func runForOutputs(t *testing.T, w *Workflow, runner *mockRunner, jobs ...string) (map[string]any, int, string) {
	t.Helper()
	var errOut strings.Builder
	w.printer = display.NewPrinterTo(false, new(strings.Builder), &errOut)
	path := filepath.Join(t.TempDir(), "report.json")
	err := w.Start(Config{ActionRunner: runner, Jobs: jobs, Reports: []report.Target{{Format: report.JSON, Path: path}}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	return r.Outputs, w.exitStatus, errOut.String()
}

// The json report holds the outputs of the workflow, evaluated once every
// job is over, however its steps ended. The secrets in them are masked, but
// nothing is hidden by its name alone, and a value JSON cannot hold is
// written as its text once masked.
func TestWorkflow_StartGivesOutputs(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("take", map[string]any{"res": map[string]any{
		"code":     200,
		"messages": []any{map[string]any{"id": "m1", "body": "for abcdefghijklmnopqrstuvwxyz0123"}},
		"next":     7,
	}})
	runner.SetResult("fail", map[string]any{"res": map[string]any{"code": 500, "stdout": "why it failed"}})
	runner.SetResult("odd", map[string]any{"res": map[string]any{"values": []any{"abcdefghijklmnopqrstuvwxyz0123", math.NaN()}}})

	w := &Workflow{
		Name:    "Outputs",
		Secrets: []string{"TOKEN"},
		env:     map[string]string{"TOKEN": "abcdefghijklmnopqrstuvwxyz0123"},
		Vars:    map[string]any{"queue": "orders"},
		Jobs: []Job{
			{ID: "receive", Name: "Receive", Steps: []*Step{{
				ID: "take", Name: "Take", Uses: "take", Test: "res.code == 200",
				Outputs: map[string]string{"messages": "res.messages", "cursor": "res.next"},
			}}},
			{ID: "check", Name: "Check", Steps: []*Step{
				{ID: "fails", Name: "Fails", Uses: "fail", Test: "res.code == 200", Outputs: map[string]string{"stdout": "res.stdout"}},
				{ID: "odd", Name: "Odd", Uses: "odd", Outputs: map[string]string{"values": "res.values"}},
			}},
		},
		Outputs: map[string]string{
			"messages": "outputs.take.messages",
			"cursor":   "outputs.cursor",
			"queue":    "vars.queue",
			"password": "vars.queue",
			"why":      "outputs.fails.stdout",
			"values":   "outputs.odd.values",
		},
	}

	got, status, _ := runForOutputs(t, w, runner)
	if status != 1 {
		t.Errorf("exit status = %d, want 1 for the failed step alone", status)
	}
	want := map[string]any{
		"messages": []any{map[string]any{"id": "m1", "body": "for <secret:TOKEN>"}},
		"cursor":   float64(7),
		"queue":    "orders",
		"password": "orders",
		"why":      "why it failed",
		"values":   []any{"<secret:TOKEN>", "NaN"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outputs = %#v, want %#v", got, want)
	}
}

// An output that cannot be evaluated once every job ran to its end is
// warned of, and not given; how the run ends does not change.
func TestWorkflow_StartWarnsOfOutputNotGiven(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"res": map[string]any{"code": 200}})
	w := &Workflow{
		Name: "Warns",
		Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{{ID: "s", Name: "S", Uses: "ok", Test: "res.code == 200"}}}},
		Outputs: map[string]string{
			"code":   "outputs.s?.code ?? 0",
			"broken": "outputs.late.rows",
		},
	}
	got, status, errOut := runForOutputs(t, w, runner)
	if status != 0 {
		t.Errorf("exit status = %d, want 0", status)
	}
	if !reflect.DeepEqual(got, map[string]any{"code": float64(0)}) {
		t.Errorf("outputs = %#v", got)
	}
	if !strings.Contains(errOut, "output 'broken' of the workflow is not given") {
		t.Errorf("no warning of the output not given in %q", errOut)
	}
}

// A job that did not run to its end may be why an output cannot be
// evaluated, as it reads what no step published, so it is left out without
// a word: in a run narrowed with --job, and in one with a job that was
// skipped or failed.
func TestWorkflow_StartQuietOfOutputsJobsDidNotPublish(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"res": map[string]any{"code": 200, "rows": []any{1}}})
	runner.SetResult("fail", map[string]any{"res": map[string]any{"code": 500}})
	tests := []struct {
		name string
		jobs []Job
		only []string
	}{
		{
			name: "narrowed with --job",
			jobs: []Job{
				{ID: "receive", Name: "Receive", Steps: []*Step{{ID: "take", Name: "Take", Uses: "ok", Outputs: map[string]string{"rows": "res.rows"}}}},
				{ID: "ack", Name: "Ack", Steps: []*Step{{Name: "Ack", Uses: "ok", Test: "res.code == 200"}}},
			},
			only: []string{"ack"},
		},
		{
			name: "a job skipped",
			jobs: []Job{
				{ID: "receive", Name: "Receive", SkipIf: "true", Steps: []*Step{{ID: "take", Name: "Take", Uses: "ok", Outputs: map[string]string{"rows": "res.rows"}}}},
				{ID: "ack", Name: "Ack", Steps: []*Step{{Name: "Ack", Uses: "ok", Test: "res.code == 200"}}},
			},
		},
		{
			name: "a job failed",
			jobs: []Job{
				{ID: "receive", Name: "Receive", FailFast: true, Steps: []*Step{
					{Name: "Fails", Uses: "fail", Test: "res.code == 200"},
					{ID: "take", Name: "Take", Uses: "ok", Outputs: map[string]string{"rows": "res.rows"}},
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Workflow{Name: "Quiet", Jobs: tt.jobs, Outputs: map[string]string{"rows": "outputs.take.rows"}}
			got, _, errOut := runForOutputs(t, w, runner, tt.only...)
			if got != nil {
				t.Errorf("outputs = %#v, want none", got)
			}
			if strings.Contains(errOut, "of the workflow is not given") {
				t.Errorf("warned of the output: %q", errOut)
			}
		})
	}
}

// An output that cannot be evaluated although what it reads was published
// is a mistake in it, which is warned of in a run whose job failed too.
func TestWorkflow_StartWarnsOfBrokenOutputWhenAJobFailed(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("fail", map[string]any{"res": map[string]any{"code": 500, "body": "down"}})
	w := &Workflow{
		Name: "Warns",
		Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{{
			ID: "s", Name: "S", Uses: "fail", Test: "res.code == 200", Outputs: map[string]string{"body": "res.body"},
		}}}},
		Outputs: map[string]string{"typo": "len(outputs.s.body) + 'a'"},
	}
	_, status, errOut := runForOutputs(t, w, runner)
	if status != 1 {
		t.Errorf("exit status = %d, want 1", status)
	}
	if !strings.Contains(errOut, "output 'typo' of the workflow is not given") {
		t.Errorf("no warning of the broken output in %q", errOut)
	}
}

// A workflow without outputs writes none to the report.
func TestWorkflow_StartWithoutOutputs(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{"res": map[string]any{"code": 200}})
	w := &Workflow{
		Name:    "None",
		Jobs:    []Job{{ID: "j", Name: "J", Steps: []*Step{{Name: "S", Uses: "ok", Test: "res.code == 200"}}}},
		printer: newBufferPrinter(),
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: path}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"outputs"`) {
		t.Errorf("the report has outputs:\n%s", data)
	}
}

// A workflow whose output does not parse is refused as it is loaded.
func TestLoad_RefusesWorkflowOutputThatDoesNotParse(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: outputs
jobs:
- name: J
  steps:
  - uses: hello
outputs:
  broken: outputs.a +
`)
	err := (&Probe{FilePath: path, Config: Config{Actions: builtinNames}}).Load()
	if err == nil || !strings.Contains(err.Error(), "outputs.broken: ") {
		t.Errorf("Load = %v, want the output refused", err)
	}
}
