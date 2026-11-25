package engine

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mozership/probe/internal/expr"
)

// WorkflowOutputEnv is what the outputs of a workflow read once every job
// is over: the vars of the run, and the outputs its jobs published.
type WorkflowOutputEnv struct {
	Vars    map[string]any `expr:"vars"`
	Outputs map[string]any `expr:"outputs"`
}

// evalWorkflowOutputs evaluates outputs, the outputs of a workflow, against
// env, and returns their values by name, with what kept any of them from
// being given, in the order of their names. An output that comes to nil,
// as one read from a job that did not publish it does, is not given, and
// is not an error.
func evalWorkflowOutputs(outputs map[string]string, env WorkflowOutputEnv) (map[string]any, []error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	values := make(map[string]any, len(outputs))
	var errs []error
	ev := &expr.Expr{}
	for _, name := range slices.Sorted(maps.Keys(outputs)) {
		v, err := ev.Eval(outputs[name], env)
		if err != nil {
			errs = append(errs, fmt.Errorf("output '%s' of the workflow is not given: %w", name, err))
			continue
		}
		if v != nil {
			values[name] = v
		}
	}
	return values, errs
}

// outputProblems returns what keeps the outputs of the workflow from being
// evaluated as they are written, in the order of their names.
func (w *Workflow) outputProblems() []Problem {
	var out []Problem
	for _, name := range slices.Sorted(maps.Keys(w.Outputs)) {
		path := "outputs." + name
		if w.Outputs[name] == "" {
			out = append(out, Problem{Path: path, Err: fmt.Errorf("%s: the expression is empty", path)})
			continue
		}
		out = append(out, expressionProblems(path, "", path, w.Outputs[name])...)
	}
	return out
}
