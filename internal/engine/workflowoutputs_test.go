package engine

import (
	"reflect"
	"strings"
	"testing"
)

func TestEvalWorkflowOutputs(t *testing.T) {
	env := WorkflowOutputEnv{
		Vars: map[string]any{"queue": "orders"},
		Outputs: map[string]any{
			"take":     map[string]any{"messages": []any{map[string]any{"id": 1}}},
			"messages": []any{map[string]any{"id": 1}},
		},
	}
	got, errs := evalWorkflowOutputs(map[string]string{
		"messages": "outputs.take.messages",
		"queue":    "vars.queue",
		"count":    "len(outputs.messages)",
		"absent":   "outputs.later?.cursor",
		"broken":   "outputs.later.cursor",
		"wrong":    "1 + 'a'",
	}, env)

	want := map[string]any{
		"messages": []any{map[string]any{"id": 1}},
		"queue":    "orders",
		"count":    1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("values = %#v, want %#v", got, want)
	}
	if len(errs) != 2 {
		t.Fatalf("errors = %v, want 2", errs)
	}
	for i, name := range []string{"broken", "wrong"} {
		if !strings.HasPrefix(errs[i].Error(), "output '"+name+"' of the workflow is not given: ") {
			t.Errorf("errors[%d] = %v, want one of %s", i, errs[i], name)
		}
	}
}

func TestEvalWorkflowOutputs_None(t *testing.T) {
	got, errs := evalWorkflowOutputs(nil, WorkflowOutputEnv{})
	if got != nil || errs != nil {
		t.Errorf("got %v, %v; want nothing", got, errs)
	}
}

func TestWorkflow_OutputProblems(t *testing.T) {
	w := &Workflow{Outputs: map[string]string{
		"ok":     "outputs.a",
		"empty":  "",
		"broken": "outputs.a +",
	}}
	problems := w.outputProblems()
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want 2", problems)
	}
	if problems[0].Path != "outputs.broken" || !strings.HasPrefix(problems[0].Error(), "outputs.broken: ") {
		t.Errorf("problems[0] = %s at %s", problems[0], problems[0].Path)
	}
	if problems[1].Path != "outputs.empty" || problems[1].Error() != "outputs.empty: the expression is empty" {
		t.Errorf("problems[1] = %s at %s", problems[1], problems[1].Path)
	}
}
