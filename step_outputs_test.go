package probe

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/expr"
)

func TestSaveOutputsWarnsAboutTakenName(t *testing.T) {
	// A later step without an id publishing a name already taken gets a
	// warning, not an error-labelled message, as its value cannot be read.
	errOut := new(bytes.Buffer)
	printer := NewPrinter(false)
	printer.errWriter = errOut
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: printer,
		},
		Outputs: NewOutputs(),
	}
	_ = jCtx.Outputs.Set("first", map[string]any{"token": "one"})

	st := &stepRun{Step: &Step{Outputs: map[string]string{"token": "res.v"}}, expr: &expr.Expr{}}
	st.ctx = StepContext{Res: map[string]any{"v": "two"}}
	st.saveOutputs(jCtx)

	out := errOut.String()
	if !strings.Contains(out, "[WARN]") || strings.Contains(out, "Error:") {
		t.Errorf("output = %q, want a warning and no error", out)
	}

	// One with an id is read through it, and is not warned of.
	errOut.Reset()
	st = &stepRun{Step: &Step{ID: "third", Outputs: map[string]string{"token": "res.v"}}, expr: &expr.Expr{}}
	st.ctx = StepContext{Res: map[string]any{"v": "three"}}
	st.saveOutputs(jCtx)
	if out := errOut.String(); out != "" {
		t.Errorf("output = %q, want none for a step read through its id", out)
	}
}

// An output that cannot be evaluated is told of and left out, and the
// others of the step are saved.
func TestSaveOutputs_SkipsOneThatCannotBeEvaluated(t *testing.T) {
	printer := newBufferPrinter()
	jCtx := &JobContext{runEnv: &runEnv{Printer: printer}, Outputs: NewOutputs()}
	st := &stepRun{Step: &Step{ID: "login", Outputs: map[string]string{"bad": "nosuch(1)", "token": "res.v"}}, expr: &expr.Expr{}}
	st.ctx = StepContext{Res: map[string]any{"v": "abc"}}

	st.saveOutputs(jCtx)

	if out := printer.errWriter.(*bytes.Buffer).String(); !strings.Contains(out, "output 'bad' evaluation error: ") {
		t.Errorf("output = %q, want the error of the output", out)
	}
	got, ok := jCtx.Outputs.GetAll()["login"].(map[string]any)
	if !ok {
		t.Fatalf("outputs = %v, want the step's", jCtx.Outputs.GetAll())
	}
	if got["token"] != "abc" {
		t.Errorf("token = %v, want abc", got["token"])
	}
	if _, ok := got["bad"]; ok {
		t.Errorf("outputs = %v, want bad left out", got)
	}
}
