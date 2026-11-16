// Command caller-action serves an action that calls another with
// probe.ActionCaller, the way an action that drives a target through the
// action of the target does. It calls with.uses with with.with, as many
// times as with.times says, and returns the res of the last call.
package main

import (
	"context"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe"
	"github.com/mozership/probe/actionrpc"
)

type action struct{ log hclog.Logger }

func (a action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

func (a action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	uses, _ := call.With["uses"].(string)
	with, _ := call.With["with"].(map[string]any)
	times := 1
	if n, ok := call.With["times"].(int); ok {
		times = n
	}
	caller := probe.NewActionCaller(call, "", probe.WithCallerLog(a.log))
	var last *probe.CallResult
	for range times {
		res, err := caller.Call(context.Background(), uses, with)
		if err != nil {
			return nil, nil, err
		}
		last = res
	}
	return map[string]any{
		"req":    call.With,
		"res":    last.Result["res"],
		"status": 0,
	}, nil, nil
}

func main() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action { return action{log: log} })
}
