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

// outputError is what kept an output of a workflow from being given.
type outputError struct {
	name string
	err  error
}

func (e outputError) Error() string {
	return fmt.Sprintf("output '%s' of the workflow is not given: %v", e.name, e.err)
}

// evalWorkflowOutputs evaluates outputs, the outputs of a workflow, against
// env, and returns their values by name, with what kept any of them from
// being given, in the order of their names. An output that comes to nil,
// as one read from a job that did not publish it does, is not given, and
// is not an error.
func evalWorkflowOutputs(outputs map[string]string, env WorkflowOutputEnv) (map[string]any, []outputError) {
	if len(outputs) == 0 {
		return nil, nil
	}
	values := make(map[string]any, len(outputs))
	var errs []outputError
	ev := &expr.Expr{}
	for _, name := range slices.Sorted(maps.Keys(outputs)) {
		v, err := ev.Eval(outputs[name], env)
		if err != nil {
			errs = append(errs, outputError{name: name, err: err})
			continue
		}
		if v != nil {
			values[name] = v
		}
	}
	return values, errs
}

// readsUnpublished reports whether input reads from outputs a name that
// published does not have.
func readsUnpublished(input string, published map[string]any) bool {
	for _, chain := range expr.Reads(input, "outputs") {
		if len(chain) == 0 {
			continue
		}
		if _, ok := published[chain[0]]; !ok {
			return true
		}
	}
	return false
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
