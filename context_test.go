package probe

import (
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
