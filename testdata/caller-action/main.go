// Command caller-action serves an action that calls another with
// probe.ActionCaller, the way an action that drives a target through the
// action of the target does. It calls with.uses with with.with, as many
// times as with.times says, and returns the res of the last call as
// res.called, and how many calls it made as res.calls.
package main

import (
	"context"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe"
	"github.com/mozership/probe/action"
)

type testAction struct{}

func (a testAction) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(action.Call{With: with})
	return ret, err
}

func (a testAction) RunStep(call action.Call) (map[string]any, map[string]any, error) {
	uses, _ := call.With["uses"].(string)
	with, _ := call.With["with"].(map[string]any)
	// A number of with comes as an int64 over RPC.
	times := 1
	switch n := call.With["times"].(type) {
	case int64:
		times = int(n)
	case int:
		times = n
	}
	caller := probe.NewActionCaller(call, "")
	var last *probe.CallResult
	calls := 0
	for range times {
		res, err := caller.Call(context.Background(), uses, with)
		if err != nil {
			return nil, nil, err
		}
		last = res
		calls++
	}
	return map[string]any{
		"req":    call.With,
		"res":    map[string]any{"called": last.Result["res"], "calls": calls},
		"status": 0,
	}, nil, nil
}

func main() {
	action.Serve(func(log hclog.Logger) action.Action { return testAction{} })
}
