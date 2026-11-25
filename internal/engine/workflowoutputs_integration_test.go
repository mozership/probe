package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/display"
	"github.com/mozership/probe/internal/report"
)

// The json report holds the outputs of the workflow, evaluated once every
// job is over, with the secrets in them masked. An output read from a job
// that was skipped is not given, and one that cannot be evaluated is warned
// of and not given either; neither changes how the run ends.
func TestWorkflow_StartGivesOutputs(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("take", map[string]any{"res": map[string]any{
		"code":     200,
		"messages": []any{map[string]any{"id": "m1", "body": "for abcdefghijklmnopqrstuvwxyz0123"}},
		"next":     7,
	}})
	runner.SetResult("fail", map[string]any{"res": map[string]any{"code": 500}})

	var errOut strings.Builder
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
			{ID: "check", Name: "Check", Steps: []*Step{{Name: "Fails", Uses: "fail", Test: "res.code == 200"}}},
			{ID: "after", Name: "After", Needs: []string{"check"}, Steps: []*Step{{
				ID: "late", Name: "Late", Uses: "take", Outputs: map[string]string{"rows": "res.messages"},
			}}},
		},
		Outputs: map[string]string{
			"messages": "outputs.take.messages",
			"cursor":   "outputs.cursor",
			"queue":    "vars.queue",
			"rows":     "outputs.late?.rows",
			"broken":   "outputs.late.rows",
		},
		printer: display.NewPrinterTo(false, new(strings.Builder), &errOut),
	}

	path := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: path}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if w.exitStatus != 1 {
		t.Errorf("exit status = %d, want 1 for the failed step alone", w.exitStatus)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"messages": []any{map[string]any{"id": "m1", "body": "for <secret:TOKEN>"}},
		"cursor":   float64(7),
		"queue":    "orders",
	}
	if !reflect.DeepEqual(r.Outputs, want) {
		t.Errorf("outputs = %#v, want %#v", r.Outputs, want)
	}
	if !strings.Contains(errOut.String(), "output 'broken' of the workflow is not given") {
		t.Errorf("no warning of the output that could not be evaluated in %q", errOut.String())
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
