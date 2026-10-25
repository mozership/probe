package embedded

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/internal/mapping"
)

type Action struct {
	log hclog.Logger
	// builtin are the names of the actions of Probe, or none when the
	// action is not told them.
	builtin []string
	// opts are given to every job the action runs, after those of the step
	// that embeds it.
	opts []Option
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
		a.log.Trace("embedded job prepared", "path", path, "vars", vars)
	})
	after := WithAfter(func(result *Result) {
		a.log.Trace("embedded job completed", "result", result)
	})
	opts := []Option{before, after, WithRunID(call.Step.RunID), WithGuard(call.Guard), WithActions(call.Actions), WithBuiltinActions(a.builtin), WithCleanups(call.Cleanups)}
	ret, err := Execute(with, append(opts, a.opts...)...)

	actionrpc.LogOutcome(a.log, "embedded job", ret, err)

	return ret, nil, err
}

// ServeWithActions serves the embedded action, which knows builtin, the
// names of the actions of Probe: a job that uses any other name is stopped
// before its first step, and told which name. opts are given to every job
// it runs, such as WithNeedsNoCheck.
func ServeWithActions(builtin []string, opts ...Option) {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log, builtin: builtin, opts: opts}
	})
}

// Spec declares the embedded action. It keeps to every kind of guard: it
// runs its job under the guard of the run, whose steps keep to it or are
// refused. It cannot run without path.
//
// Spec leaves Serve to the registry, which serves the action with
// ServeWithActions to tell it the names of the actions of Probe.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Keeps:     []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: mapping.FieldTags(Res{}),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  kind.Fields(Res{}),
		Required:  [][]string{{"path"}},
	}
}
