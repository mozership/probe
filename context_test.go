package probe

import (
	"reflect"
	"slices"
	"testing"

	"github.com/mozership/probe/expr"
)

func TestResponseTimeExpressions(t *testing.T) {
	// Create a step context with the new RT structure
	ctx := StepContext{
		RT: ResponseTime{
			Duration: "250ms",
			Sec:      0.25,
		},
		Vars: map[string]any{
			"threshold": 0.3,
		},
	}

	ev := &expr.Expr{}

	t.Run("access rt.duration", func(t *testing.T) {
		result, err := ev.Eval(`rt.duration`, ctx)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if result != "250ms" {
			t.Errorf("Expected rt.duration to be '250ms', got %v", result)
		}
	})

	t.Run("access rt.sec", func(t *testing.T) {
		result, err := ev.Eval(`rt.sec`, ctx)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if result != 0.25 {
			t.Errorf("Expected rt.sec to be 0.25, got %v", result)
		}
	})

	t.Run("compare rt.sec with threshold", func(t *testing.T) {
		result, err := ev.Eval(`rt.sec < vars.threshold`, ctx)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if result != true {
			t.Errorf("Expected rt.sec < threshold to be true, got %v", result)
		}
	})

	t.Run("use rt in template", func(t *testing.T) {
		template := "Response took {{ rt.duration }} ({{ rt.sec }}s)"
		result, err := ev.EvalTemplate(template, ctx)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		expected := "Response took 250ms (0.25s)"
		if result != expected {
			t.Errorf("Expected template result to be '%s', got '%s'", expected, result)
		}
	})
}

// What a verbose run shows of the context of a test is the context by the
// names the test reads it by, but for the request and the response, which
// the step has just shown.
func TestStepContext_testEnv(t *testing.T) {
	ctx := StepContext{
		Vars:        map[string]any{"v": 1},
		Res:         map[string]any{"code": 200},
		Req:         map[string]any{"url": "/"},
		RT:          ResponseTime{Duration: "250ms", Sec: 0.25},
		Report:      "r",
		Outputs:     map[string]any{"o": 2},
		Status:      3,
		RepeatIndex: 4,
	}
	env := ctx.testEnv()

	var names []string
	for f := range reflect.TypeFor[StepContext]().Fields() {
		if name := f.Tag.Get("expr"); name != "req" && name != "res" {
			names = append(names, name)
		}
	}
	var keys []string
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(names)
	slices.Sort(keys)
	if !slices.Equal(keys, names) {
		t.Errorf("keys = %v, want %v", keys, names)
	}
	want := map[string]any{
		"vars":         ctx.Vars,
		"outputs":      ctx.Outputs,
		"status":       3,
		"rt":           map[string]any{"duration": "250ms", "sec": 0.25},
		"report":       "r",
		"repeat_index": 4,
	}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("testEnv() = %v, want %v", env, want)
	}
}
