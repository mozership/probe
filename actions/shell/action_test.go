package shell

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

// A command run in the background is told to the runner, which stops it once
// the run is over; one run in the foreground is not.
func TestActionRunStepReportsABackgroundCommand(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}

	procs := &actionrpc.Processes{}
	ret, _, err := a.RunStep(actionrpc.Call{With: map[string]any{"cmd": "sleep 60", "background": true}, Processes: procs})
	if err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	t.Cleanup(StopStarted)
	res, _ := ret["res"].(map[string]any)
	got := procs.List()
	if len(got) != 1 || got[0].PID != res["pid"] || got[0].Log != res["log"] || got[0].Log == "" {
		t.Errorf("reported %v, want the pid and log of %v", got, res)
	}

	procs = &actionrpc.Processes{}
	if _, _, err := a.RunStep(actionrpc.Call{With: map[string]any{"cmd": "true"}, Processes: procs}); err != nil {
		t.Fatalf("RunStep() error = %v", err)
	}
	if got := procs.List(); len(got) != 0 {
		t.Errorf("a command in the foreground reported %v", got)
	}
}
