package embedded

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
	// builtin are the names of the actions of Probe, or none when the
	// action is not told them.
	builtin []string
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep runs the job as part of the run of the step that embeds it. It
// keeps no state of its own: the job keeps its own state, in its own run.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	with := call.With
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, nil, errors.New("embedded action requires parameters in 'with' section. Please specify embedded job details like path")
	}

	actionrpc.LogParams(a.log, "received embedded request parameters", with)

	before := WithBefore(func(path string, vars map[string]any) {
		a.log.Debug("embedded job prepared", "path", path, "vars", vars)
	})
	after := WithAfter(func(result *Result) {
		a.log.Debug("embedded job completed", "result", result)
	})
	ret, err := Execute(with, before, after, WithRunID(call.Step.RunID), WithGuard(call.Guard), WithActions(call.Actions), WithBuiltinActions(a.builtin), WithCleanups(call.Cleanups))

	actionrpc.LogOutcome(a.log, "embedded job", ret, err)

	return ret, nil, err
}

func Serve() {
	ServeWithActions(nil)
}

// ServeWithActions is Serve for an action that knows builtin, the names of
// the actions of Probe: a job that uses any other name is stopped before
// its first step, and told which name.
func ServeWithActions(builtin []string) {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log, builtin: builtin}
	})
}

// Keeps returns the kinds of guard the embedded action keeps to: it runs
// its job under the guard of the run, whose steps keep to it or are refused.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// Params returns the keys the embedded action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}
