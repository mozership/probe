package probe

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/expr"
)

func TestStep_executeSingleAction_DefaultTimeout(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			// No Timeout specified - should use DefaultStepTimeout
		},
		expr: &expr.Expr{},
	}

	// Mock runner that completes quickly
	mock := newMockRunner()
	mock.SetResult("test-action", map[string]any{"status": 0})

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config: Config{Verbose: false},
		},
	}

	start := time.Now()
	_, err := step.executeSingleAction(mock, map[string]any{}, jCtx, false)
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	// Should complete quickly (well under default timeout)
	if duration > 1*time.Second {
		t.Errorf("Expected quick completion, took %v", duration)
	}
}

func TestStep_executeSingleAction_CustomTimeout(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Uses:    "test-action",
			Timeout: Interval{Duration: 2 * time.Second},
		},
		expr: &expr.Expr{},
	}

	// Mock runner that takes 3 seconds (longer than timeout)
	slowMock := &slowRunner{delay: 3 * time.Second}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config: Config{Verbose: false},
		},
	}

	start := time.Now()
	result, err := step.executeSingleAction(slowMock, map[string]any{}, jCtx, false)
	duration := time.Since(start)

	// Should timeout
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
	if !reflect.DeepEqual(result, actionResult{}) {
		t.Errorf("Expected no result on timeout, got %+v", result)
	}
	// Should timeout around 2 seconds
	if duration < 1900*time.Millisecond || duration > 2500*time.Millisecond {
		t.Errorf("Expected timeout around 2s, took %v", duration)
	}
}

// An action that times out is told to stop, rather than run on unseen. One
// that stops at once still fails for the timeout, and not for whatever it
// returns on being stopped.
func TestStep_executeSingleAction_TimeoutStopsTheAction(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, errors.New("rpc error: code = DeadlineExceeded")} {
		step := &stepRun{
			Step: &Step{
				Uses:    "test-action",
				Timeout: Interval{Duration: 100 * time.Millisecond},
			},
			expr: &expr.Expr{},
		}
		runner := &stoppableRunner{stopped: make(chan struct{}), err: err}

		_, got := step.executeSingleAction(runner, map[string]any{}, &JobContext{
			runEnv: &runEnv{},
		}, false)
		if got == nil || got.Error() != "action execution timed out after 100ms" {
			t.Errorf("executeSingleAction() error = %v, want the timeout", got)
		}
		select {
		case <-runner.stopped:
		case <-time.After(time.Second):
			t.Fatal("the action was not told to stop")
		}
	}
}

// An action that is done in time is not told to stop while it runs.
func TestStep_executeSingleAction_ContextOfAnActionDoneInTime(t *testing.T) {
	step := &stepRun{Step: &Step{Uses: "test-action", Timeout: Interval{Duration: time.Second}}, expr: &expr.Expr{}}
	runner := &contextRecordingRunner{}

	if _, err := step.executeSingleAction(runner, map[string]any{}, &JobContext{
		runEnv: &runEnv{},
	}, false); err != nil {
		t.Fatalf("executeSingleAction() error = %v", err)
	}
	if runner.ctx == nil {
		t.Fatal("the action was given no context")
	}
	if runner.during != nil {
		t.Errorf("the context was done while the action ran: %v", runner.during)
	}
}

func TestStep_executeSingleAction_CompletesBeforeTimeout(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Uses:    "test-action",
			Timeout: Interval{Duration: 3 * time.Second},
		},
		expr: &expr.Expr{},
	}

	// Mock runner that takes 1 second (less than timeout)
	slowMock := &slowRunner{delay: 1 * time.Second}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config: Config{Verbose: false},
		},
	}

	start := time.Now()
	result, err := step.executeSingleAction(slowMock, map[string]any{}, jCtx, false)
	duration := time.Since(start)

	// Should complete successfully
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if result.Status != 0 {
		t.Errorf("Expected status=0, got %v", result.Status)
	}
	// Should complete around 1 second
	if duration < 900*time.Millisecond || duration > 1500*time.Millisecond {
		t.Errorf("Expected completion around 1s, took %v", duration)
	}
}

func TestStep_DefaultStepTimeout_Value(t *testing.T) {
	expected := 5 * time.Minute
	if DefaultStepTimeout != expected {
		t.Errorf("DefaultStepTimeout = %v, want %v", DefaultStepTimeout, expected)
	}
}
