package probe

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozership/probe/report"
)

// TestEndToEndNotes runs a workflow whose steps use an action served by an
// executable of its own, which leaves the notes it is given, as probe-agent
// does, and a last job whose steps read them: what crosses the process of
// the action is lists and maps as they are decoded on the way back.
func TestEndToEndNotes(t *testing.T) {
	dir := t.TempDir()
	probeBin := filepath.Join(dir, "probe")
	actionDir := filepath.Join(dir, "agent")
	for _, b := range [][]string{{probeBin, "./cmd/probe"}, {filepath.Join(actionDir, "agent"), "./testdata/external-action"}} {
		if out, err := exec.Command("go", "build", "-o", b[0], b[1]).CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v\n%s", b[1], err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte("runs:\n  using: binary\n  path: agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workflow := filepath.Join(dir, "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`name: notes
secrets: [PROBE_E2E_CARD]
jobs:
- id: agent
  name: Agent
  steps:
  - name: Scenario
    uses: ./agent
    with:
      notes:
      - level: critical
        title: Shipping form
        body: |
          Paid with 4111111111111111, and the form rejected **94105**.
      - level: info
        body: Done in 22 steps.
    test: res.greeting != ""
  - name: Persona
    uses: ./agent
    with:
      notes:
      - level: warning
        body: The gift wrap toggle is hidden.
      - level: high
        body: Left out.
    test: "false"
- id: report
  name: Report
  needs: [agent]
  always: true
  steps:
  - name: Read
    uses: hello
    test: >-
      len(notes) == 3
      && notes[0].job == "Agent" && notes[0].job_id == "agent"
      && notes[0].step == "Scenario" && notes[0].index == 0
      && notes[0].status == "passed" && notes[0].level == "critical"
      && notes[0].title == "Shipping form"
      && notes[0].body == "Paid with <secret:PROBE_E2E_CARD>, and the form rejected **94105**.\n"
      && notes[1].title == "" && notes[1].level == "info"
      && notes[2].step == "Persona" && notes[2].status == "failed" && notes[2].level == "warning"
      && notes[0].run == nil
  - name: Critical
    uses: hello
    iteration: "{{ filter(notes, .level == 'critical') }}"
    test: vars.title == "Shipping form" && vars.step == "Scenario"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	reportPath := filepath.Join(dir, "report.json")
	cmd := exec.Command(probeBin, "--report", "json="+reportPath, workflow)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PROBE_E2E_CARD=4111111111111111")
	out, err := cmd.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != ExitTestFailed {
		t.Fatalf("exit = %v, want %d: the Persona step fails\n%s", err, ExitTestFailed, out)
	}
	if !strings.Contains(string(out), `notes[1]: level "high" is not one of info, warning or critical; it is left out of the notes`) {
		t.Errorf("output does not warn of the note left out\n%s", out)
	}
	if strings.Contains(string(out), "4111111111111111") {
		t.Errorf("output shows the secret\n%s", out)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ job, step, status string }{
		{"agent", "Scenario", report.Passed},
		{"agent", "Persona", report.Failed},
		{"report", "Read", report.Passed},
		{"report", "Critical", report.Passed},
	} {
		if st := reportStep(t, r, want.job, want.step); st.Status != want.status {
			t.Errorf("%s / %s = %s, want %s\n%s", want.job, want.step, st.Status, want.status, out)
		}
	}
}
