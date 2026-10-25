package embedded

import (
	"reflect"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

// TestRunStepGivesTheJobItsOptions checks that the options the action is
// served with reach every job it runs, after those of the step that embeds
// it, so that a job is told which actions need no test, as WithNeedsNoCheck
// says. The job's one step is skipped, so that no action is run.
func TestRunStepGivesTheJobItsOptions(t *testing.T) {
	path := writeJob(t, "name: inner\nsteps:\n- name: Skipped\n  uses: hello\n  skipif: \"true\"\n")
	var got []Callback
	record := func(c *Callback) { got = append(got, *c) }
	a := &Action{log: hclog.NewNullLogger(), builtin: []string{"hello"}, opts: []Option{WithNeedsNoCheck([]string{"hello"}), record}}

	if _, _, err := a.RunStep(actionrpc.Call{With: map[string]any{"path": path}, Step: actionrpc.Step{RunID: "parent-run"}}); err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the options were applied %d times, want once", len(got))
	}
	if !reflect.DeepEqual(got[0].needsNoCheck, []string{"hello"}) {
		t.Errorf("needsNoCheck = %v, want what the action was served with", got[0].needsNoCheck)
	}
	// They come after those of the step, which are there as well.
	if got[0].runID != "parent-run" || !reflect.DeepEqual(got[0].builtin, []string{"hello"}) {
		t.Errorf("runID, builtin = %q, %v, want those of the step and the action", got[0].runID, got[0].builtin)
	}
}
