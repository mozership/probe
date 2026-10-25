package probe

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/expr"
)

func TestStep_executeActionWithRetry_Timeout(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Uses:    "test-action",
			Test:    "res.code == 200",
			Timeout: Interval{Duration: 500 * time.Millisecond},
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 100 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	// Mock runner that takes 1 second per attempt (longer than timeout)
	slowMock := &slowRunner{delay: 1 * time.Second}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	start := time.Now()
	result, err := step.executeActionWithRetry(slowMock, map[string]any{}, jCtx, "test")
	duration := time.Since(start)

	// First attempt should timeout at 500ms
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Expected timeout error, got: %v", err)
	}

	// Should timeout on first attempt around 500ms, then retry with interval
	// Total: ~500ms (timeout) + 100ms (interval) + 500ms (timeout) + 100ms (interval) + 500ms (timeout) = ~1700ms
	expectedMin := 1500 * time.Millisecond
	expectedMax := 2000 * time.Millisecond
	if duration < expectedMin || duration > expectedMax {
		t.Errorf("Expected retry with timeout around 1700ms, took %v", duration)
	}

	// On timeout, result should be nil
	if !reflect.DeepEqual(result, actionResult{}) {
		t.Errorf("Expected no result on timeout, got %+v", result)
	}
}

func TestStep_executeActionWithRetry_NoTest(t *testing.T) {
	callCount := 0
	mock := &callCountingRunner{
		result:    map[string]any{"status": 0},
		callCount: &callCount,
	}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			// No Test field - should execute once without retry
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 10 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	_, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected action to be called once (no test = no retry), got %d calls", callCount)
	}
}

func TestStep_executeActionWithRetry_TestFailThenPass(t *testing.T) {
	callCount := 0
	mock := &callCountingRunner{
		callCount: &callCount,
		resultFunc: func(count int) map[string]any {
			if count <= 2 {
				// First two attempts return non-matching response
				return map[string]any{
					"status": 0,
					"res":    map[string]any{"code": 200, "body": `{"messages":[{"id":"msg1"}]}`},
				}
			}
			// Third attempt returns matching response
			return map[string]any{
				"status": 0,
				"res":    map[string]any{"code": 200, "body": `{"messages":[]}`},
			}
		},
	}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			Test: "res.code == 200 && res.body.messages == []",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 10 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	_, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test")
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if callCount != 3 {
		t.Errorf("Expected 3 attempts (2 test failures + 1 success), got %d", callCount)
	}
}

func TestStep_executeActionWithRetry_TestFailAllAttempts(t *testing.T) {
	callCount := 0
	mock := &callCountingRunner{
		callCount: &callCount,
		result: map[string]any{
			"status": 0,
			"res":    map[string]any{"code": 200, "body": `{"result":"not_empty"}`},
		},
	}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			Test: "res.code == 500",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 10 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	result, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test")
	if err != nil {
		t.Errorf("Expected no error (test failure is not an action error), got: %v", err)
	}
	if result.Res == nil {
		t.Error("Expected the result of the last attempt, got none")
	}
	if callCount != 3 {
		t.Errorf("Expected all 3 attempts to be tried, got %d", callCount)
	}
}

// TestStep_executeActionWithRetry_QuietsNonFinalAttempts pins the behaviour
// that keeps a readiness wait from logging its retries as errors: an action
// cannot tell that it is being retried, so the runner silences every attempt
// that still has another one behind it.
func TestStep_executeActionWithRetry_QuietsNonFinalAttempts(t *testing.T) {
	mock := &optionsRecordingRunner{failUntil: 2}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			Test: "res.code == 200",
			Retry: &StepRetry{
				MaxAttempts: 4,
				Interval:    Interval{Duration: time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
		t.Fatalf("expected the step to succeed once the action recovers, got: %v", err)
	}

	got := mock.recorded()
	if len(got) != 3 {
		t.Fatalf("attempts = %d, want 3 (2 failures + 1 success)", len(got))
	}
	for i, opts := range got {
		// Attempts 1..3 all have attempt 4 behind them, so all are quiet.
		if !opts.Quiet {
			t.Errorf("attempt %d: Quiet = false, want true", i+1)
		}
		if opts.Verbose {
			t.Errorf("attempt %d: Verbose = true, want false", i+1)
		}
	}
}

// TestStep_executeActionWithRetry_FinalAttemptIsNotQuiet makes sure a step
// that really fails still reports the action's own error.
func TestStep_executeActionWithRetry_FinalAttemptIsNotQuiet(t *testing.T) {
	mock := &optionsRecordingRunner{failUntil: 3}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			Test: "res.code == 200",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err == nil {
		t.Fatal("expected an error once every attempt failed")
	}

	got := mock.recorded()
	if len(got) != 3 {
		t.Fatalf("attempts = %d, want 3", len(got))
	}
	for i, opts := range got[:2] {
		if !opts.Quiet {
			t.Errorf("attempt %d: Quiet = false, want true", i+1)
		}
	}
	if got[2].Quiet {
		t.Error("final attempt: Quiet = true, want false")
	}
}

// TestStep_executeActionWithRetry_VerboseOverridesQuiet keeps --verbose able
// to show what each retry attempt did.
func TestStep_executeActionWithRetry_VerboseOverridesQuiet(t *testing.T) {
	mock := &optionsRecordingRunner{failUntil: 1}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
			Test: "res.code == 200",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: true},
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
		t.Fatalf("expected the step to succeed, got: %v", err)
	}

	for i, opts := range mock.recorded() {
		if !opts.Verbose {
			t.Errorf("attempt %d: Verbose = false, want true", i+1)
		}
		if opts.logLevel() != hclog.Debug {
			t.Errorf("attempt %d: logLevel = %v, want debug", i+1, opts.logLevel())
		}
	}
}

// TestStep_executeSingleAction_WithoutRetryIsNotQuiet covers a step with no
// retry: its only attempt must report failures.
func TestStep_executeSingleAction_WithoutRetryIsNotQuiet(t *testing.T) {
	mock := &optionsRecordingRunner{}

	step := &stepRun{
		Step: &Step{
			Uses: "test-action",
		},
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: false},
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeSingleAction(mock, map[string]any{}, jCtx, false); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	recorded := mock.recorded()
	if len(recorded) != 1 {
		t.Fatalf("attempts = %d, want 1", len(recorded))
	}
	if recorded[0].Quiet {
		t.Error("a step without retry should not be quiet")
	}
	if recorded[0].logLevel() != hclog.Warn {
		t.Errorf("logLevel = %v, want warn", recorded[0].logLevel())
	}
}

// The first attempt of a retry waits for its initial delay.
func TestStep_executeActionWithRetry_InitialDelay(t *testing.T) {
	callCount := 0
	runner := &callCountingRunner{result: map[string]any{"res": map[string]any{"code": 0}}, callCount: &callCount}
	delay := 50 * time.Millisecond
	st := &stepRun{
		Step: &Step{
			Uses:  "test-action",
			Test:  "true",
			Retry: &StepRetry{MaxAttempts: 2, InitialDelay: Interval{Duration: delay}},
		},
		expr: &expr.Expr{},
	}
	printer := newBufferPrinter()
	printer.verbose = true
	jCtx := &JobContext{runEnv: &runEnv{Config: Config{Verbose: true}, Printer: printer}}

	start := time.Now()
	if _, err := st.executeActionWithRetry(runner, map[string]any{}, jCtx, "test"); err != nil {
		t.Fatalf("executeActionWithRetry() error = %v", err)
	}
	if d := time.Since(start); d < delay {
		t.Errorf("took %v, want at least the initial delay of %v", d, delay)
	}
	if callCount != 1 {
		t.Errorf("calls = %d, want 1", callCount)
	}
	if out := printer.errWriter.(*bytes.Buffer).String(); !strings.Contains(out, "Initial delay before first attempt: 50ms") {
		t.Errorf("output = %q, want the initial delay told of", out)
	}
}

func TestStep_executeActionWithRetry_UncheckedRunsOnce(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
		err    error
	}{
		{name: "a result nothing checks", result: map[string]any{"status": 1, "res": map[string]any{"code": 500}}},
		{name: "an action error", err: fmt.Errorf("connection refused")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			mock := &callCountingRunner{callCount: &callCount, result: tt.result, err: tt.err}
			step := &stepRun{
				Step: &Step{
					Uses:  "http",
					Retry: &StepRetry{MaxAttempts: 3, Interval: Interval{Duration: time.Millisecond}},
				},
				expr: &expr.Expr{},
			}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
			}

			_, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test")
			if (err != nil) != (tt.err != nil) {
				t.Errorf("err = %v, want %v", err, tt.err)
			}
			if callCount != 1 {
				t.Errorf("attempts = %d, want 1", callCount)
			}
			if step.retryAttempt != 0 {
				t.Errorf("retryAttempt = %d, want 0: a step nothing checks is not retried", step.retryAttempt)
			}
		})
	}
}
