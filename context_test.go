package probe

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/internal/expr"
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

func TestStepContext_RepeatIndex(t *testing.T) {
	tests := []struct {
		name        string
		repeatIndex int
		expression  string
		expected    string
	}{
		{
			name:        "repeat_index in echo",
			repeatIndex: 3,
			expression:  "Repeat: {{repeat_index}}",
			expected:    "Repeat: 3",
		},
		{
			name:        "repeat_index in test",
			repeatIndex: 5,
			expression:  "repeat_index == 5",
			expected:    "true",
		},
		{
			name:        "repeat_index arithmetic",
			repeatIndex: 2,
			expression:  "{{repeat_index + 1}}",
			expected:    "3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := StepContext{
				RepeatIndex: tt.repeatIndex,
			}

			ev := &expr.Expr{}
			var result string
			var err error

			if strings.Contains(tt.expression, "{{") {
				// Template expression
				result, err = ev.EvalTemplate(tt.expression, ctx)
			} else {
				// Boolean/arithmetic expression
				output, evalErr := ev.Eval(tt.expression, ctx)
				err = evalErr
				if err == nil {
					result = fmt.Sprintf("%v", output)
				}
			}

			if err != nil {
				t.Errorf("Expression evaluation failed: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestJobContextRepeatTracking(t *testing.T) {
	ctx := JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
		},
		repeat: &jobRepeat{
			total:    10,
			counters: make(map[int]StepRepeatCounter),
		},
		RepeatCurrent: 5,
	}

	// Test initial state
	if !ctx.repeating() {
		t.Error("expected the runs to be counted")
	}

	if ctx.RepeatCurrent != 5 {
		t.Errorf("expected RepeatCurrent to be 5, got %d", ctx.RepeatCurrent)
	}

	if ctx.repeat.total != 10 {
		t.Errorf("expected the total to be 10, got %d", ctx.repeat.total)
	}

	// Test step counter initialization
	counter := StepRepeatCounter{
		SuccessCount: 3,
		FailureCount: 2,
		Name:         "Test Step",
		LastResult:   true,
	}

	ctx.repeat.counters[0] = counter

	if len(ctx.repeat.counters) != 1 {
		t.Errorf("expected 1 step counter, got %d", len(ctx.repeat.counters))
	}

	if ctx.repeat.counters[0].SuccessCount != 3 {
		t.Errorf("expected SuccessCount to be 3, got %d", ctx.repeat.counters[0].SuccessCount)
	}
}
