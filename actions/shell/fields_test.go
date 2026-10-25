package shell

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, ReqTypes(), ResTypes())
	a := &Action{log: hclog.NewNullLogger()}
	t.Cleanup(StopStarted)
	for _, with := range []map[string]any{
		{"cmd": "echo hi; echo err >&2", "shell": "/bin/sh", "workdir": t.TempDir(), "env": map[string]any{"A": "1"}},
		{"cmd": "exit 3"},
		{"cmd": "sleep 5", "timeout": "100ms"},
		{"cmd": "sleep 60", "background": true},
		{"cmd": "echo ready; sleep 60", "background": true, "ready": map[string]any{"log": "ready"}},
	} {
		ret, _, err := a.RunStep(actionrpc.Call{With: with, Processes: &actionrpc.Processes{}})
		if err != nil {
			t.Fatalf("%v: %v", with, err)
		}
		actiontest.CheckFields(t, ReqFields(), ResFields(), nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	actiontest.CheckRequired(t, Required(), map[string]any{"cmd": "true", "timeout": "5s"}, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
