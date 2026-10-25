package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

func TestStep_parseWaitDuration(t *testing.T) {
	tests := []struct {
		name     string
		wait     string
		expected time.Duration
		hasError bool
	}{
		{
			name:     "seconds as integer string",
			wait:     "5",
			expected: 5 * time.Second,
			hasError: false,
		},
		{
			name:     "duration string with seconds",
			wait:     "3s",
			expected: 3 * time.Second,
			hasError: false,
		},
		{
			name:     "duration string with milliseconds",
			wait:     "500ms",
			expected: 500 * time.Millisecond,
			hasError: false,
		},
		{
			name:     "duration string with minutes",
			wait:     "2m",
			expected: 2 * time.Minute,
			hasError: false,
		},
		{
			name:     "invalid format",
			wait:     "invalid",
			expected: 0,
			hasError: true,
		},
		{
			name:     "empty string",
			wait:     "",
			expected: 0,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := parseWaitDuration(tt.wait)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for wait '%s', but got none", tt.wait)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for wait '%s': %v", tt.wait, err)
				}
				if duration != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, duration)
				}
			}
		})
	}
}

func TestStep_formatWaitTime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "exact seconds",
			duration: 5 * time.Second,
			expected: "5s",
		},
		{
			name:     "milliseconds",
			duration: 500 * time.Millisecond,
			expected: "500ms",
		},
		{
			name:     "mixed duration",
			duration: 2*time.Minute + 30*time.Second,
			expected: "150s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := newStepRun(&Step{}, nil)
			result := step.formatWaitTime(tt.duration)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestStep_getWaitTimeForDisplay(t *testing.T) {
	tests := []struct {
		name     string
		wait     string
		expected string
	}{
		{
			name:     "no wait",
			wait:     "",
			expected: "",
		},
		{
			name:     "seconds",
			wait:     "5",
			expected: "5s",
		},
		{
			name:     "duration string",
			wait:     "500ms",
			expected: "500ms",
		},
		{
			name:     "invalid format returns empty",
			wait:     "invalid",
			expected: "",
		},
		{
			name:     "template not evaluated returns empty",
			wait:     "{{ vars.wait }}",
			expected: "",
		},
		{
			name:     "unclosed opener returns empty",
			wait:     "{{ 5",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := newStepRun(&Step{Wait: tt.wait}, nil)
			result := step.getWaitTimeForDisplay()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestStep_handleWait(t *testing.T) {
	tests := []struct {
		name         string
		wait         string
		expectedTime string
		minDuration  time.Duration
		maxDuration  time.Duration
	}{
		{
			name:         "no wait",
			wait:         "",
			expectedTime: "",
			minDuration:  0,
			maxDuration:  10 * time.Millisecond,
		},
		{
			name:         "short wait",
			wait:         "10ms",
			expectedTime: "10ms",
			minDuration:  8 * time.Millisecond,
			maxDuration:  50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := newStepRun(&Step{Wait: tt.wait}, nil)
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: &Printer{},
				},
			}

			start := time.Now()
			step.handleWait(jCtx)
			duration := time.Since(start)

			// Skip expectedTime check for empty wait case
			if tt.wait != "" {
				expectedDuration, _ := time.ParseDuration(tt.expectedTime)
				if duration < expectedDuration {
					t.Errorf("expected at least %s, got %s", expectedDuration, duration)
				}
			}

			if duration < tt.minDuration {
				t.Errorf("Duration %v should be at least %v", duration, tt.minDuration)
			}

			if duration > tt.maxDuration {
				t.Errorf("Duration %v should not exceed %v", duration, tt.maxDuration)
			}
		})
	}
}

// A wait that cannot be evaluated, or is not a time, is told of, and the
// step runs at once rather than fail.
func TestStep_prepare_WaitThatIsNoTime(t *testing.T) {
	tests := []struct {
		name string
		wait string
		want string
	}{
		{name: "a template that cannot be evaluated", wait: "{{1 +}}s", want: "wait expression evaluation error: "},
		{name: "not a time", wait: "soon", want: "wait duration parsing error: invalid wait format: soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			st := newStepRun(&Step{Name: "step", Wait: tt.wait}, nil)
			jCtx := &JobContext{runEnv: &runEnv{Printer: printer}}

			start := time.Now()
			_, ok := st.prepare(jCtx)
			if !ok {
				t.Error("prepare() = false, want the step to run")
			}
			if d := time.Since(start); d > 500*time.Millisecond {
				t.Errorf("prepare() took %v, want no wait", d)
			}
			if out := printer.errWriter.(*bytes.Buffer).String(); !strings.Contains(out, tt.want) {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
			if st.templateErr != nil {
				t.Errorf("templateErr = %v, want the wait to fail nothing", st.templateErr)
			}
		})
	}
}

// A skipif that cannot be evaluated, or is not a boolean, is told of, and
// the step runs rather than be skipped.
func TestStep_shouldSkip_errorHandling(t *testing.T) {
	tests := []struct {
		name   string
		skipIf string
		want   string
	}{
		{name: "an expression that cannot be evaluated", skipIf: "invalid syntax ===", want: "skipif evaluation error: "},
		{name: "not a boolean", skipIf: `"yes"`, want: "skipif expression must return boolean, got: string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			st := newStepRun(&Step{Name: "step", SkipIf: tt.skipIf}, nil)
			jCtx := &JobContext{runEnv: &runEnv{Printer: printer, Result: NewResult()}}

			if st.shouldSkip(jCtx) {
				t.Error("shouldSkip() = true, want the step to run")
			}
			if out := printer.errWriter.(*bytes.Buffer).String(); !strings.Contains(out, tt.want) {
				t.Errorf("output = %q, want %q", out, tt.want)
			}
			if _, ok := st.prepare(jCtx); !ok {
				t.Error("prepare() = false, want the step to run")
			}
		})
	}
}

func TestStep_SkipIfWithWaitTiming(t *testing.T) {
	tests := []struct {
		name        string
		wait        string
		skipif      string
		expectSkip  bool
		maxDuration time.Duration
		minDuration time.Duration
	}{
		{
			name:        "skipped step with wait should not wait",
			wait:        "1s",
			skipif:      "true",
			expectSkip:  true,
			maxDuration: 50 * time.Millisecond, // Should be very fast since no wait
			minDuration: 0,
		},
		{
			name:        "non-skipped step with wait should wait",
			wait:        "100ms",
			skipif:      "false",
			expectSkip:  false,
			maxDuration: 200 * time.Millisecond, // Allow some overhead
			minDuration: 80 * time.Millisecond,  // Should wait at least this long
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{
					Name:   "Test Step",
					Uses:   "hello",
					Wait:   tt.wait,
					SkipIf: tt.skipif,
				},
				expr: &expr.Expr{},
			}

			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
					// Silent printer
					Config: Config{},
				},
			}

			start := time.Now()
			name, shouldContinue := step.prepare(jCtx)
			duration := time.Since(start)

			// Check skip behavior
			if tt.expectSkip && shouldContinue {
				t.Errorf("Expected step to be skipped (shouldContinue=false), got shouldContinue=%v", shouldContinue)
			}
			if !tt.expectSkip && !shouldContinue {
				t.Errorf("Expected step to continue (shouldContinue=true), got shouldContinue=%v", shouldContinue)
			}

			// Check timing
			if duration > tt.maxDuration {
				t.Errorf("Expected duration <= %v, got %v", tt.maxDuration, duration)
			}
			if duration < tt.minDuration {
				t.Errorf("Expected duration >= %v, got %v", tt.minDuration, duration)
			}

			if name != "Test Step" {
				t.Errorf("Expected step name 'Test Step', got %v", name)
			}
		})
	}
}

func TestStep_shouldSkip(t *testing.T) {
	tests := []struct {
		name     string
		skipif   string
		context  StepContext
		expected bool
		hasError bool
	}{
		{
			name:     "no skipif condition",
			skipif:   "",
			context:  StepContext{},
			expected: false,
			hasError: false,
		},
		{
			name:   "skipif returns true",
			skipif: "true",
			context: StepContext{
				Vars: map[string]any{},
			},
			expected: true,
			hasError: false,
		},
		{
			name:   "skipif returns false",
			skipif: "false",
			context: StepContext{
				Vars: map[string]any{},
			},
			expected: false,
			hasError: false,
		},
		{
			name:   "skipif with variable condition - skip",
			skipif: `vars.env == "production"`,
			context: StepContext{
				Vars: map[string]any{
					"env": "production",
				},
			},
			expected: true,
			hasError: false,
		},
		{
			name:   "skipif with variable condition - don't skip",
			skipif: `vars.env == "production"`,
			context: StepContext{
				Vars: map[string]any{
					"env": "development",
				},
			},
			expected: false,
			hasError: false,
		},
		{
			name:   "skipif with empty variable condition",
			skipif: `vars.url == ""`,
			context: StepContext{
				Vars: map[string]any{
					"url": "",
				},
			},
			expected: true,
			hasError: false,
		},
		{
			name:   "skipif with contains condition",
			skipif: `vars.url contains "production"`,
			context: StepContext{
				Vars: map[string]any{
					"url": "https://production.example.com",
				},
			},
			expected: true,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{
					SkipIf: tt.skipif,
				},
				ctx:  tt.context,
				expr: &expr.Expr{},
			}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: &Printer{},
				},
			}

			result := step.shouldSkip(jCtx)
			if result != tt.expected {
				t.Errorf("shouldSkip() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStep_createSkippedStepResult(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Wait: "2s",
		},
		idx: 1,
	}
	jCtx := &JobContext{
		runEnv: &runEnv{},
	}
	name := "Test Step"

	result := step.createSkippedStepResult(name, jCtx)

	if result.Index != 1 {
		t.Errorf("Index = %v, want %v", result.Index, 1)
	}
	if result.Name != "Test Step (SKIPPED)" {
		t.Errorf("Name = %v, want %v", result.Name, "Test Step (SKIPPED)")
	}
	if result.Status != StatusSkipped {
		t.Errorf("Status = %v, want %v", result.Status, StatusSkipped)
	}
	if result.WaitTime != "2s" {
		t.Errorf("WaitTime = %v, want %v", result.WaitTime, "2s")
	}
	if result.HasTest != false {
		t.Errorf("HasTest = %v, want %v", result.HasTest, false)
	}
	if result.RepeatCounter != nil {
		t.Errorf("RepeatCounter should be nil for non-repeat step, got %v", result.RepeatCounter)
	}
}

func TestStep_createStepResult_Timing(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Test: "res.status == 200",
			Echo: "Hello World",
			Wait: "1s",
		},
		idx:  1,
		expr: &expr.Expr{},
		ctx: StepContext{
			Res: map[string]any{"status": 200},
			RT: ResponseTime{
				Duration: "250ms",
				Sec:      0.25,
			},
		},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Timing: true},
			Printer: newBufferPrinter(),
		},
	}

	status, testOutput := step.verdict(jCtx)
	result := step.createStepResult("Test Step", jCtx, status, testOutput)

	if result.RT != "250ms" {
		t.Errorf("RT = %v, want %v", result.RT, "250ms")
	}
	if result.RTSec != 0.25 {
		t.Errorf("RTSec = %v, want %v", result.RTSec, 0.25)
	}
	if !strings.Contains(result.EchoOutput, "Hello World") {
		t.Errorf("EchoOutput = %q, want the echo", result.EchoOutput)
	}
	if result.RepeatCounter != nil {
		t.Errorf("RepeatCounter = %v, want nil", result.RepeatCounter)
	}
}
func TestStep_createFailedStepResult(t *testing.T) {
	testErr := fmt.Errorf("test error message")
	step := &stepRun{
		Step: &Step{
			Test: "res.code == 200",
			Wait: "3s",
		},
		idx: 2,
		err: testErr,
		ctx: StepContext{
			RT: ResponseTime{
				Duration: "500ms",
				Sec:      0.5,
			},
			Res: map[string]any{"report": "HTTP error occurred"},
		},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config: Config{Timing: true},
		},
	}
	name := "Failed Step"

	result := step.createFailedStepResult(name, jCtx)

	if result.Index != 2 {
		t.Errorf("Index = %v, want %v", result.Index, 2)
	}
	if result.Name != "Failed Step" {
		t.Errorf("Name = %v, want %v", result.Name, "Failed Step")
	}
	if result.Status != StatusError {
		t.Errorf("Status = %v, want %v", result.Status, StatusError)
	}
	if result.WaitTime != "3s" {
		t.Errorf("WaitTime = %v, want %v", result.WaitTime, "3s")
	}
	if result.HasTest != true {
		t.Errorf("HasTest = %v, want %v", result.HasTest, true)
	}
	if result.RT != "500ms" {
		t.Errorf("RT = %v, want %v", result.RT, "500ms")
	}
	if result.RTSec != 0.5 {
		t.Errorf("RTSec = %v, want %v", result.RTSec, 0.5)
	}
	if result.Report != "HTTP error occurred" {
		t.Errorf("Report = %v, want %v", result.Report, "HTTP error occurred")
	}
	if result.TestOutput != "       test error message\n" {
		t.Errorf("TestOutput = %q, want %q", result.TestOutput, "       test error message\n")
	}
	if result.RepeatCounter != nil {
		t.Errorf("RepeatCounter should be nil for non-repeat step, got %v", result.RepeatCounter)
	}
}

func TestStep_verdict(t *testing.T) {
	tests := []struct {
		name         string
		test         string
		uses         string
		needsNoCheck []string
		wantStatus   StatusType
		wantFailed   bool
		wantOutput   bool
	}{
		{name: "a test that holds passes", test: "res.code == 0", wantStatus: StatusSuccess},
		{name: "a test that does not hold fails the job", test: "res.code == 1", wantStatus: StatusError, wantFailed: true, wantOutput: true},
		{name: "an action that needs no check passes", uses: "hello", needsNoCheck: []string{"hello"}, wantStatus: StatusSuccess},
		{name: "a step that nothing checks is a warning", uses: "hello", wantStatus: StatusWarning},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{Test: tt.test, Uses: tt.uses}, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 0}}}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Config:  Config{NeedsNoCheck: tt.needsNoCheck},
					Printer: newBufferPrinter(),
					Result:  NewResult(),
				},
			}

			status, out := st.verdict(jCtx)
			if status != tt.wantStatus {
				t.Errorf("status = %v, want %v", status, tt.wantStatus)
			}
			if (out != "") != tt.wantOutput {
				t.Errorf("output = %q, want one: %v", out, tt.wantOutput)
			}
			if jCtx.Failed != tt.wantFailed {
				t.Errorf("Failed = %v, want %v", jCtx.Failed, tt.wantFailed)
			}
			if got := jCtx.Result.failures[FailureAssertion]; got != tt.wantFailed {
				t.Errorf("assertion failure recorded = %v, want %v", got, tt.wantFailed)
			}
			if (st.failure != nil) != tt.wantFailed {
				t.Errorf("failure = %+v, want one: %v", st.failure, tt.wantFailed)
			}
		})
	}
}

// Creating the result of a failed step only reads what the check left: it
// neither fails the job nor records the failure, which verdict has done.
func TestStep_createStepResult_ChangesNothing(t *testing.T) {
	failure := &StepFailure{Kind: FailureAssertion, Message: "test evaluated to false"}
	st := &stepRun{Step: &Step{Test: "res.code == 1"}, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 0}}, failure: failure}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
	}

	result := st.createStepResult("Check", jCtx, StatusError, "the output")
	if result.Status != StatusError || result.TestOutput != "the output" || result.Failure != failure {
		t.Errorf("result = %+v, want the status, output and failure it was given", result)
	}
	if jCtx.Failed {
		t.Error("Failed = true, want the job left as it was")
	}
	if len(jCtx.Result.failures) != 0 {
		t.Errorf("failures = %v, want none recorded", jCtx.Result.failures)
	}

	passed := st.createStepResult("Check", jCtx, StatusSuccess, "")
	if passed.Failure != nil || passed.TestOutput != "" {
		t.Errorf("result = %+v, want no failure on a step that passed", passed)
	}
}

// A step that failed before its test could run is told to have a test as
// one that ran is: by what checks it, an action that needs no check among
// them.
func TestStep_createFailedStepResult_HasTestAsARunStepHas(t *testing.T) {
	tests := []struct {
		name string
		st   *Step
		want bool
	}{
		{name: "with a test", st: &Step{Uses: "http", Test: "true"}, want: true},
		{name: "of an action that needs no check", st: &Step{Uses: "hello"}, want: true},
		{name: "unchecked", st: &Step{Uses: "http"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: tt.st, expr: &expr.Expr{}, err: errors.New("boom")}
			jCtx := &JobContext{runEnv: &runEnv{Config: Config{NeedsNoCheck: []string{"hello"}}}}

			failed := st.createFailedStepResult("Step", jCtx)
			ran := st.createStepResult("Step", jCtx, StatusSuccess, "")
			if failed.HasTest != tt.want || ran.HasTest != tt.want {
				t.Errorf("HasTest = %v failed and %v run, want %v", failed.HasTest, ran.HasTest, tt.want)
			}
		})
	}
}

func TestStep_createFailedStepResult_NoTest(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			// No Test field
		},
		idx: 1,
		ctx: StepContext{},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{},
	}
	name := "No Test Step"

	result := step.createFailedStepResult(name, jCtx)

	if result.HasTest != false {
		t.Errorf("HasTest = %v, want %v", result.HasTest, false)
	}
	if result.TestOutput != "" {
		t.Errorf("TestOutput = %v, want empty string", result.TestOutput)
	}
}

// Tests for refactored methods

// The wait shows the name of the step as prepare evaluated it: with its
// templates evaluated, and the stand-in for a step without one.
func TestStep_waitMessage(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"Poll {{vars.n}}", "Poll 3"},
		{"", "Unknown Step"},
	} {
		step := newStepRun(&Step{Name: tt.name}, nil)
		step.ctx = StepContext{Vars: map[string]any{"n": 3}}
		jCtx := &JobContext{
			runEnv: &runEnv{
				Printer: newBufferPrinter(),
			},
		}
		if _, ok := step.prepare(jCtx); !ok {
			t.Fatalf("prepare() of %q did not continue", tt.name)
		}
		if got := step.waitMessage(2 * time.Second); !strings.HasSuffix(got, " "+tt.want) {
			t.Errorf("waitMessage() of %q = %q, want it to end with %q", tt.name, got, tt.want)
		}
	}
}

func TestStep_prepare(t *testing.T) {
	tests := []struct {
		name           string
		stepName       string
		wait           string
		skipif         string
		skipCondition  map[string]any
		expectName     string
		expectContinue bool
		expectError    bool
	}{
		{
			name:           "normal step preparation",
			stepName:       "Test Step",
			wait:           "",
			skipif:         "",
			expectName:     "Test Step",
			expectContinue: true,
			expectError:    false,
		},
		{
			name:           "empty name gets default",
			stepName:       "",
			wait:           "",
			skipif:         "",
			expectName:     "Unknown Step",
			expectContinue: true,
			expectError:    false,
		},
		{
			name:           "step with wait",
			stepName:       "Wait Step",
			wait:           "10ms",
			skipif:         "",
			expectName:     "Wait Step",
			expectContinue: true,
			expectError:    false,
		},
		{
			name:           "skipped step",
			stepName:       "Skipped Step",
			wait:           "",
			skipif:         "true",
			expectName:     "Skipped Step",
			expectContinue: false,
			expectError:    false,
		},
		{
			name:           "step with conditional skip - skip",
			stepName:       "Conditional Step",
			wait:           "",
			skipif:         `vars.env == "test"`,
			skipCondition:  map[string]any{"env": "test"},
			expectName:     "Conditional Step",
			expectContinue: false,
			expectError:    false,
		},
		{
			name:           "step with conditional skip - continue",
			stepName:       "Conditional Step",
			wait:           "",
			skipif:         `vars.env == "test"`,
			skipCondition:  map[string]any{"env": "prod"},
			expectName:     "Conditional Step",
			expectContinue: true,
			expectError:    false,
		},
		{
			name:           "skipped step with wait should not wait",
			stepName:       "Skipped Wait Step",
			wait:           "2s",
			skipif:         "true",
			expectName:     "Skipped Wait Step",
			expectContinue: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{
					Name:   tt.stepName,
					Wait:   tt.wait,
					SkipIf: tt.skipif,
				},
				expr: &expr.Expr{},
			}

			// Set up context
			vars := tt.skipCondition
			if vars == nil {
				vars = map[string]any{}
			}
			step.ctx = StepContext{
				Vars: vars,
			}

			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
				// Use silent printer to avoid output during tests
			}

			name, shouldContinue := step.prepare(jCtx)

			if name != tt.expectName {
				t.Errorf("prepare() name = %v, want %v", name, tt.expectName)
			}
			if shouldContinue != tt.expectContinue {
				t.Errorf("prepare() shouldContinue = %v, want %v", shouldContinue, tt.expectContinue)
			}
		})
	}
}

func TestStep_executeAction(t *testing.T) {
	t.Run("with mock runner", func(t *testing.T) {
		step := &stepRun{
			Step: &Step{
				Uses: "http",
				With: map[string]any{"url": "http://example.com"},
			},
			expr: &expr.Expr{},
		}

		// Set up mock runner
		mock := newMockRunner()
		mock.SetResult("http", map[string]any{
			"status": 0,
			"res": map[string]any{
				"code": 200,
				"body": "OK",
			},
		})

		// Create minimal job context
		jCtx := &JobContext{
			runEnv: &runEnv{
				Config: Config{Verbose: false, ActionRunner: mock},
			},
		}

		// Initialize step context
		step.ctx = StepContext{}

		// Execute action
		result, err := step.executeAction("Test Step", jCtx)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if result.Status != 0 {
			t.Errorf("Expected status=0, got %v", result.Status)
		}
		if result.Res["code"] != 200 {
			t.Errorf("Expected res.code=200, got %v", result.Res["code"])
		}
	})

	t.Run("with mock runner error", func(t *testing.T) {
		step := &stepRun{
			Step: &Step{
				Uses: "failing-action",
				With: map[string]any{},
			},
			expr: &expr.Expr{},
		}

		// Set up mock runner with error
		mock := newMockRunner()
		testErr := fmt.Errorf("connection failed")
		mock.SetError("failing-action", testErr)

		// Create minimal job context
		jCtx := &JobContext{
			runEnv: &runEnv{
				Config: Config{Verbose: false, ActionRunner: mock},
			},
		}

		// Initialize step context
		step.ctx = StepContext{}

		// Execute action - should return error
		result, err := step.executeAction("Test Step", jCtx)
		if err != testErr {
			t.Errorf("Expected test error, got %v", err)
		}
		if !reflect.DeepEqual(result, actionResult{}) {
			t.Errorf("Expected no result on error, got %+v", result)
		}
	})

	t.Run("uses default plugin runner when none set", func(t *testing.T) {
		if _, ok := (&runEnv{}).runner().(*PluginActionRunner); !ok {
			t.Errorf("runner() = %T, want a *PluginActionRunner", (&runEnv{}).runner())
		}
		mock := newMockRunner()
		if got := (&runEnv{Config: Config{ActionRunner: mock}}).runner(); got != mock {
			t.Errorf("runner() = %T, want the runner the run was given", got)
		}
	})
}

func TestStep_processActionResult(t *testing.T) {
	tests := []struct {
		name         string
		actionResult map[string]any
		expectLogs   int
	}{
		{
			name: "normal action result",
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{"status": 200, "body": "response"},
				"rt":  "100ms",
			},
			expectLogs: 1,
		},
		{
			name: "action result with JSON body",
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{
					"status": 200,
					"body":   `{"message": "success"}`,
				},
				"rt": "150ms",
			},
			expectLogs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{},
				expr: &expr.Expr{},
			}

			jCtx := &JobContext{
				runEnv: &runEnv{},
			}

			step.processActionResult(readActionResult(tt.actionResult), jCtx)
		})
	}
}

func TestStep_handleActionError(t *testing.T) {
	tests := []struct {
		name    string
		verbose bool
		err     error
		wantOut string
	}{
		{name: "an action error", err: fmt.Errorf("test error")},
		{name: "an action error in a verbose run", verbose: true, err: fmt.Errorf("test error"), wantOut: "Error: Action execution failed: test error\n"},
		{name: "a refusal", err: actionrpc.Refuse("POST may write")},
		{name: "a refusal in a verbose run", verbose: true, err: actionrpc.Refuse("POST may write"), wantOut: "Error: refused: POST may write\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{
					Uses: "mock-action-for-error-test",
				},
				expr: &expr.Expr{},
			}
			printer := newBufferPrinter()
			printer.verbose = tt.verbose
			jCtx := &JobContext{
				runEnv: &runEnv{
					Config:  Config{Verbose: tt.verbose},
					Printer: printer,
				},
			}

			step.handleActionError(tt.err, "test-step", jCtx)

			// The error is kept as it is, for the step's result to tell
			// what the action said and nothing about where it was caught.
			if step.err != tt.err {
				t.Errorf("step.err = %v, want %v", step.err, tt.err)
			}
			if !jCtx.Failed {
				t.Errorf("handleActionError() should mark job as failed")
			}
			// The step's result tells the error, so it is told as it
			// happens only in a verbose run, before the exchange.
			got := printer.errWriter.(*bytes.Buffer).String()
			if tt.wantOut == "" && got != "" || !strings.HasPrefix(got, tt.wantOut) {
				t.Errorf("printed %q, want it to start with %q", got, tt.wantOut)
			}
		})
	}
}

func TestStep_finalize(t *testing.T) {
	tests := []struct {
		name         string
		verbose      bool
		isRepeating  bool
		actionResult map[string]any
		expectCalled string // "verbose", "repeat", or "standard"
	}{
		{
			name:        "verbose mode",
			verbose:     true,
			isRepeating: false,
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{"status": 200},
				"rt":  "100ms",
			},
			expectCalled: "verbose",
		},
		{
			name:        "repeat mode",
			verbose:     false,
			isRepeating: true,
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{"status": 200},
				"rt":  "100ms",
			},
			expectCalled: "repeat",
		},
		{
			name:        "standard mode",
			verbose:     false,
			isRepeating: false,
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{"status": 200},
				"rt":  "100ms",
			},
			expectCalled: "standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{},
				expr: &expr.Expr{},
			}

			jCtx := &JobContext{
				runEnv: &runEnv{
					Config: Config{
						Verbose: tt.verbose,
					},
					Printer: newBufferPrinter(),
				},
			}
			if tt.isRepeating {
				jCtx.repeat = newJobRepeat(1, 0)
			}

			// Note: This test mainly verifies that the correct code path is taken
			// More detailed testing of each path should be done in their specific test functions
			step.finalize("test-step", jCtx)

			// For now, we just verify the method doesn't panic
			// In a more complete test, we might track which methods were called
		})
	}
}

// Integration test for the run method
func TestStep_run_Integration(t *testing.T) {
	t.Run("step with skip condition", func(t *testing.T) {
		step := newStepRun(&Step{
			Name:   "Skipped Step",
			Uses:   "", // Empty action since this step should be skipped anyway
			SkipIf: "true",
		}, nil)

		step.ctx = StepContext{
			Vars: map[string]any{},
		}

		jobContext := JobContext{
			runEnv: &runEnv{
				Printer: newBufferPrinter(),
			},
		}

		// Execute the step (should be skipped)
		step.run(&jobContext)

		// Since step is skipped, job should not fail
		if jobContext.Failed {
			t.Errorf("run() with skip condition should not fail the job")
		}
	})

	t.Run("successful step execution with mock", func(t *testing.T) {
		step := newStepRun(&Step{
			Name: "Test Step",
			Uses: "http",
			With: map[string]any{"url": "http://example.com"},
		}, nil)

		// Set up mock runner for successful execution
		mock := newMockRunner()
		mock.SetResult("http", map[string]any{
			"code": 0,
			"results": map[string]any{
				"status": 200,
				"body":   "Success",
			},
			"rt": "100ms",
		})

		step.ctx = StepContext{
			Vars: map[string]any{},
		}

		jobContext := JobContext{
			runEnv: &runEnv{
				Config:  Config{Verbose: false, ActionRunner: mock},
				Printer: newBufferPrinter(),
			},
		}

		// Execute the step
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("run() method panicked: %v", r)
			}
		}()

		step.run(&jobContext)

		// Verify successful execution
		if jobContext.Failed {
			t.Errorf("run() with successful mock should not fail the job")
		}

		// Verify context was updated
		if step.ctx.RT.Duration != "100ms" {
			t.Errorf("Expected RT.Duration to be '100ms', got %v", step.ctx.RT.Duration)
		}
	})

	t.Run("failed step execution with mock", func(t *testing.T) {
		step := newStepRun(&Step{
			Name: "Failed Step",
			Uses: "failing-action",
		}, nil)

		// Set up mock runner for failed execution
		mock := newMockRunner()
		testErr := fmt.Errorf("network error")
		mock.SetError("failing-action", testErr)

		step.ctx = StepContext{
			Vars: map[string]any{},
		}

		jobContext := JobContext{
			runEnv: &runEnv{
				Config:  Config{Verbose: false, ActionRunner: mock},
				Printer: newBufferPrinter(),
			},
		}

		// Execute the step
		step.run(&jobContext)

		// Verify failed execution
		if !jobContext.Failed {
			t.Errorf("run() with failing mock should fail the job")
		}

		// Verify error was set
		if step.err == nil {
			t.Errorf("Expected step error to be set")
		}
	})
}

func TestSleepWithMessage(t *testing.T) {
	tests := []struct {
		name           string
		duration       time.Duration
		message        string
		expectFnCalled bool
		minCalls       int
		maxCalls       int
	}{
		{
			name:           "Short duration under 1s - no call",
			duration:       50 * time.Millisecond,
			message:        "ignored",
			expectFnCalled: false,
		},
		{
			name:           "1.25s duration - expect 1 to 2 calls",
			duration:       1250 * time.Millisecond,
			message:        "hello",
			expectFnCalled: true,
			minCalls:       1,
			maxCalls:       2,
		},
		{
			name:           "Exact 1.05s duration - expect 1 call",
			duration:       1050 * time.Millisecond,
			message:        "one",
			expectFnCalled: true,
			minCalls:       1,
			maxCalls:       1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string

			sleepWithMessage(tt.duration, tt.message, func(m string) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, m)
			})

			if tt.expectFnCalled {
				count := len(calls)
				if count < tt.minCalls || count > tt.maxCalls {
					t.Errorf("expected between %d and %d calls, got %d", tt.minCalls, tt.maxCalls, count)
				}
				for _, msg := range calls {
					if msg != tt.message {
						t.Errorf("unexpected message: got %s, want %s", msg, tt.message)
					}
				}
			} else {
				if len(calls) > 0 {
					t.Errorf("expected no calls, but got %d", len(calls))
				}
			}
		})
	}
}

func TestStep_getEchoOutput(t *testing.T) {
	// Enable color output for testing
	originalNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = originalNoColor }()

	tests := []struct {
		name        string
		echo        string
		context     StepContext
		expected    string
		expectError bool
	}{
		{
			name:        "single line echo",
			echo:        "Hello World",
			context:     StepContext{},
			expected:    "       Hello World\n",
			expectError: false,
		},
		{
			name:        "multi-line echo with explicit newlines",
			echo:        "Line 1\nLine 2\nLine 3",
			context:     StepContext{},
			expected:    "       Line 1\n       Line 2\n       Line 3\n",
			expectError: false,
		},
		{
			name:        "complex multiline with indentation",
			echo:        "Header\n  Indented\n    More indented\nBack to left",
			context:     StepContext{},
			expected:    "       Header\n         Indented\n           More indented\n       Back to left\n",
			expectError: false,
		},
		{
			name:        "empty line handling",
			echo:        "Line 1\n\nLine 3",
			context:     StepContext{},
			expected:    "       Line 1\n       \n       Line 3\n",
			expectError: false,
		},
		{
			name:        "template expression",
			echo:        "Status: {{vars.status}}",
			context:     StepContext{Vars: map[string]any{"status": "OK"}},
			expected:    "       Status: OK\n",
			expectError: false,
		},
		{
			name:        "multiline template expression",
			echo:        "Status: {{vars.status}}\nCode: {{vars.code}}",
			context:     StepContext{Vars: map[string]any{"status": "OK", "code": "200"}},
			expected:    "       Status: OK\n       Code: 200\n",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{
					Echo: tt.echo,
				},
				ctx:  tt.context,
				expr: &expr.Expr{},
			}

			result := step.getEchoOutput()

			if result != tt.expected {
				t.Errorf("getEchoOutput() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestStep_getEchoOutput_Error(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Echo: "{{invalid_expression + }}", // Invalid syntax that will cause template error
		},
		ctx:  StepContext{},
		expr: &expr.Expr{},
	}

	result := step.getEchoOutput()

	// Should contain error indication when template evaluation fails
	if !strings.Contains(result, "error: {{invalid_expression + }}: ") {
		t.Errorf("getEchoOutput() with invalid expression should return error message, got %q", result)
	}

	// Verify indentation is applied even to error messages
	lines := strings.SplitSeq(strings.TrimSuffix(result, "\n"), "\n")
	for line := range lines {
		if !strings.HasPrefix(line, "       ") {
			t.Errorf("getEchoOutput() should indent all lines including errors, line without indent: %q", line)
		}
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

func TestStep_DefaultStepTimeout_Value(t *testing.T) {
	expected := 5 * time.Minute
	if DefaultStepTimeout != expected {
		t.Errorf("DefaultStepTimeout = %v, want %v", DefaultStepTimeout, expected)
	}
}

func TestStep_handleRepeatExecution_AccumulatesEchoOutputs(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Echo: "iteration {{vars.i}}",
		},
		idx:  1,
		expr: &expr.Expr{},
	}

	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
		repeat: &jobRepeat{
			counters: make(map[int]StepRepeatCounter),
			total:    3,
		},
		CurrentJobID: "job-1",
	}
	jCtx.Result.Jobs["job-1"] = &JobResult{JobID: "job-1"}

	// Runs are counted from 0, as the executor counts them.
	for i := range 3 {
		jCtx.RepeatCurrent = i
		step.ctx = StepContext{
			Vars: map[string]any{"i": i + 1},
		}
		step.handleRepeatExecution(jCtx, "Repeat Step", false)
	}

	counter, ok := jCtx.repeat.counters[step.idx]
	if !ok {
		t.Fatal("StepCounters[1] missing")
	}
	if got := len(counter.EchoOutputs); got != 3 {
		t.Fatalf("EchoOutputs length = %d, want 3", got)
	}
	for i, out := range counter.EchoOutputs {
		want := fmt.Sprintf("iteration %d", i+1)
		if !strings.Contains(out, want) {
			t.Errorf("EchoOutputs[%d] = %q, want to contain %q", i, out, want)
		}
	}

	// The runs themselves add no result; the executor makes one from the
	// counter once they are done.
	if got := len(jCtx.Result.Jobs["job-1"].StepResults); got != 0 {
		t.Fatalf("StepResults length = %d after the runs, want 0", got)
	}
	e := &Executor{job: &Job{ID: "job-1", Steps: []*Step{{Name: "first"}, step.Step}}}
	e.appendRepeatStepResults(jCtx)

	stepResults := jCtx.Result.Jobs["job-1"].StepResults
	if len(stepResults) != 1 {
		t.Fatalf("StepResults length = %d, want 1", len(stepResults))
	}
	if stepResults[0].EchoOutput != "" {
		t.Errorf("StepResult.EchoOutput should be empty for repeat steps, got %q", stepResults[0].EchoOutput)
	}
	if stepResults[0].RepeatCounter == nil {
		t.Fatal("StepResult.RepeatCounter is nil")
	}
	if got := len(stepResults[0].RepeatCounter.EchoOutputs); got != 3 {
		t.Errorf("StepResult.RepeatCounter.EchoOutputs length = %d, want 3", got)
	}
}

func TestParseExitStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"nil", nil, int(ExitStatusFailure)},
		{"int 0", int(0), int(ExitStatusSuccess)},
		{"int 1", int(1), int(ExitStatusFailure)},
		{"int64 0", int64(0), int(ExitStatusSuccess)},
		{"int64 1", int64(1), int(ExitStatusFailure)},
		{"float64 0", float64(0), int(ExitStatusSuccess)},
		{"float64 1", float64(1), int(ExitStatusFailure)},
		{"string 0", "0", int(ExitStatusSuccess)},
		{"string 1", "1", int(ExitStatusFailure)},
		{"ExitStatus success", ExitStatusSuccess, int(ExitStatusSuccess)},
		{"ExitStatus failure", ExitStatusFailure, int(ExitStatusFailure)},
		{"unknown type", []int{0}, int(ExitStatusFailure)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseExitStatus(tt.input)
			if result != tt.expected {
				t.Errorf("parseExitStatus(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
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

// TestStep_executeSingleAction_Trace passes -vv on to the action, whose log
// then shows how its process is run as well.
func TestStep_executeSingleAction_Trace(t *testing.T) {
	mock := &optionsRecordingRunner{}
	step := &stepRun{Step: &Step{Uses: "test-action"}, expr: &expr.Expr{}}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Verbose: true, Trace: true},
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
	if !recorded[0].Trace || recorded[0].logLevel() != hclog.Trace {
		t.Errorf("Trace = %v, logLevel = %v, want true, trace", recorded[0].Trace, recorded[0].logLevel())
	}
}

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

// startGroup starts cmd in a process group of its own, as the shell action
// does for a background step, and returns its pid.
func startGroup(t *testing.T, cmd string) int {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", cmd)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		t.Fatalf("start %q: %v", cmd, err)
	}
	// Reap the process once it is stopped, so that the group is really gone.
	go func() { _ = c.Wait() }()
	pid := c.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return pid
}

// waitGroupGone reports whether the group led by pid is gone within a second.
// A killed process stays visible until it is reaped, which happens on its
// own schedule.
func waitGroupGone(pid int) bool {
	deadline := time.Now().Add(time.Second)
	for syscall.Kill(-pid, 0) == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

// A process any action says it left running is stopped once the workflow is
// over, also when the action failed after it started it: the engine does not
// know which actions start them.
func TestStepTracksTheProcessesAnActionLeaves(t *testing.T) {
	for name, err := range map[string]error{"succeeded": nil, "failed": errors.New("boom")} {
		t.Run(name, func(t *testing.T) {
			step := &stepRun{Step: &Step{Uses: "anything"}, expr: &expr.Expr{}}
			runner := &lateBackgroundRunner{t: t, err: err, pid: make(chan int, 1)}
			jCtx := &JobContext{
				runEnv: &runEnv{
					background: newBackground(),
				},
			}

			_, got := step.executeSingleAction(runner, map[string]any{}, jCtx, false)
			if (got != nil) != (err != nil) {
				t.Fatalf("executeSingleAction() error = %v, want %v", got, err)
			}
			pid := <-runner.pid

			jCtx.background.Stop()
			if !waitGroupGone(pid) {
				t.Errorf("process group %d the action left is still running", pid)
			}
		})
	}
}

func TestStepTimeoutStillTracksBackground(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Uses:    "shell",
			Timeout: Interval{Duration: 100 * time.Millisecond},
		},
		expr: &expr.Expr{},
	}
	runner := &lateBackgroundRunner{t: t, delay: 300 * time.Millisecond, pid: make(chan int, 1)}
	jCtx := &JobContext{
		runEnv: &runEnv{
			background: newBackground(),
		},
	}

	if _, err := step.executeSingleAction(runner, map[string]any{}, jCtx, false); err == nil {
		t.Fatal("expected the step to time out")
	}

	// The workflow ends while the action is still running.
	jCtx.background.Stop()

	pid := <-runner.pid
	if !waitGroupGone(pid) {
		t.Errorf("process group %d started after the step timed out is still running", pid)
	}
}

func TestStep_processActionResultStdoutJSON(t *testing.T) {
	tests := []struct {
		name   string
		res    map[string]any
		want   any
		wantOK bool
	}{
		{
			name:   "an object with a trailing newline",
			res:    map[string]any{"stdout": "{\"id\":\"m1\",\"count\":2}\n"},
			want:   map[string]any{"id": "m1", "count": float64(2)},
			wantOK: true,
		},
		{
			name:   "an array",
			res:    map[string]any{"stdout": ` ["a", {"b": true}] `},
			want:   []any{"a", map[string]any{"b": true}},
			wantOK: true,
		},
		{name: "plain text", res: map[string]any{"stdout": "hello\n"}},
		{name: "empty", res: map[string]any{"stdout": ""}},
		{name: "JSON Lines", res: map[string]any{"stdout": "{\"a\":1}\n{\"a\":2}\n"}},
		{name: "braces that are not JSON", res: map[string]any{"stdout": "{key: value}"}},
		{name: "a scalar", res: map[string]any{"stdout": "42\n"}},
		{name: "no stdout", res: map[string]any{"code": 0}},
		{name: "stdout that is not a string", res: map[string]any{"stdout": 1}},
		{
			name:   "a json field the action set itself",
			res:    map[string]any{"stdout": `{"a":1}`, "json": "the action's own"},
			want:   "the action's own",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, hadStdout := tt.res["stdout"]
			step := &stepRun{Step: &Step{}, expr: &expr.Expr{}}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
			}
			step.processActionResult(readActionResult(map[string]any{"res": tt.res}), jCtx)

			got, ok := step.ctx.Res["json"]
			if ok != tt.wantOK {
				t.Fatalf("res.json present = %v, want %v (%#v)", ok, tt.wantOK, got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("res.json = %#v, want %#v", got, tt.want)
			}
			if hadStdout && !reflect.DeepEqual(step.ctx.Res["stdout"], stdout) {
				t.Errorf("res.stdout = %#v, want it kept as %#v", step.ctx.Res["stdout"], stdout)
			}
		})
	}
}

// TestStepTellsTheActionAboutTheStep checks that every call of an action is
// told the run, the job and the step it is made for.
func TestStepTellsTheActionAboutTheStep(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("hello", map[string]any{"status": 0})
	step := func(id, name, test string, retry *StepRetry) *Step {
		return &Step{ID: id, Name: name, Uses: "hello", Test: test, Retry: retry}
	}
	workflow := &Workflow{
		Name: "steps",
		Vars: map[string]any{"who": "alice"},
		Jobs: []Job{
			{
				Name: "Log in {{ vars.who }}",
				ID:   "login",
				Steps: []*Step{
					step("auth", "Post as {{ vars.who }}", "", nil),
					step("", "Retried", "false", &StepRetry{MaxAttempts: 2, Interval: Interval{Duration: time.Millisecond}}),
				},
				Repeat: &Repeat{Count: 2},
			},
		},
		printer: newBufferPrinter(),
	}
	_ = workflow.Start(Config{ActionRunner: runner})

	run := workflow.RunID()
	if len(run) != 16 {
		t.Fatalf("RunID() = %q, want 16 hex digits", run)
	}
	var got []actionrpc.Step
	for _, c := range runner.Calls["hello"] {
		got = append(got, c.Step)
	}
	first := func(repeat int) []actionrpc.Step {
		return []actionrpc.Step{
			{RunID: run, JobID: "login", JobName: "Log in alice", Index: 0, ID: "auth", Name: "Post as alice", Repeat: repeat, Attempt: 1},
			{RunID: run, JobID: "login", JobName: "Log in alice", Index: 1, ID: "step_1", Name: "Retried", Repeat: repeat, Attempt: 1},
			{RunID: run, JobID: "login", JobName: "Log in alice", Index: 1, ID: "step_1", Name: "Retried", Repeat: repeat, Attempt: 2},
		}
	}
	want := append(first(0), first(1)...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps told =\n%+v\nwant\n%+v", got, want)
	}
}

// contractOf is the check of an action that checked the step against a
// contract and found the violations given.
func contractOf(vs ...map[string]any) *actionrpc.ContractCheck {
	list := []any{}
	for _, v := range vs {
		list = append(list, v)
	}
	check := actionrpc.ContractCheckOf(list, nil)
	return &check
}

// The check the action reports of an attempt against a contract is the
// step's, and an attempt that fails leaves none of an earlier one.
func TestStep_executeSingleAction_TakesTheContractTheActionReports(t *testing.T) {
	callCount := 0
	broken := contractOf(map[string]any{"in": "response", "message": "body failed"})
	runner := &callCountingRunner{
		callCount: &callCount,
		result:    map[string]any{"res": map[string]any{"code": 200}},
		contractFunc: func(int) *actionrpc.ContractCheck {
			return broken
		},
	}
	st := &stepRun{Step: &Step{Uses: "http"}, expr: &expr.Expr{}}
	jCtx := &JobContext{runEnv: &runEnv{Printer: newBufferPrinter()}}

	if _, err := st.executeSingleAction(runner, map[string]any{}, jCtx, false); err != nil {
		t.Fatalf("executeSingleAction() error = %v", err)
	}
	if !reflect.DeepEqual(st.contract, broken) {
		t.Errorf("contract = %+v, want %+v", st.contract, broken)
	}

	runner.err = errors.New("connection refused")
	if _, err := st.executeSingleAction(runner, map[string]any{}, jCtx, false); err == nil {
		t.Fatal("executeSingleAction() error = nil, want the action's")
	}
	if st.contract != nil {
		t.Errorf("contract = %+v after a failed attempt, want none", st.contract)
	}
}

func TestStep_check_Contract(t *testing.T) {
	broken := map[string]any{"in": "response", "field": "$.id", "reason": "want integer", "message": "body failed"}

	tests := []struct {
		name        string
		test        string
		contract    *actionrpc.ContractCheck
		wantChecked bool
		wantOK      bool
		wantKind    string
	}{
		{name: "no contract and no test"},
		{name: "a contract kept, without a test", contract: contractOf(), wantChecked: true, wantOK: true},
		{name: "a contract kept, with a test that holds", test: "res.code == 200", contract: contractOf(), wantChecked: true, wantOK: true},
		{name: "a contract kept, with a test that fails", test: "res.code == 201", contract: contractOf(), wantChecked: true, wantKind: FailureAssertion},
		{name: "a contract broken, without a test", contract: contractOf(broken), wantChecked: true, wantKind: FailureContractResponse},
		{name: "a contract broken, with a test that holds", test: "res.code == 200", contract: contractOf(broken), wantChecked: true, wantKind: FailureContractResponse},
		{name: "a request that breaks the contract", test: "res.code == 200", contract: contractOf(map[string]any{"in": "request", "message": "body failed"}), wantChecked: true, wantKind: FailureContractRequest},
		// What res holds is the step's to read; the runner hears of a
		// contract only from the action.
		{name: "violations in res alone", test: "res.code == 200", wantChecked: true, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := map[string]any{"code": 200}
			if tt.contract == nil && tt.test != "" {
				res["violations"] = []any{broken}
			}
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}, ctx: StepContext{Res: res}, contract: tt.contract}
			if got := st.checked(); got != tt.wantChecked {
				t.Fatalf("checked() = %v, want %v", got, tt.wantChecked)
			}
			if !tt.wantChecked {
				return
			}
			_, ok := st.check(newBufferPrinter())
			if ok != tt.wantOK {
				t.Fatalf("check() = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK {
				if st.failure != nil {
					t.Errorf("failure = %+v, want nil", st.failure)
				}
				return
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want kind %q", st.failure, tt.wantKind)
			}
		})
	}
}

func TestStep_contractFailure(t *testing.T) {
	st := &stepRun{
		Step: &Step{},
		ctx: StepContext{
			Req: map[string]any{"url": "http://example.com/v1/users/2"},
			Res: map[string]any{"code": 200},
		},
		contract: contractOf(
			map[string]any{"in": "response", "reason": "missing property 'name'", "message": "body failed"},
			map[string]any{"in": "response", "field": "$.id", "reason": "got string, want integer", "message": "body failed"},
		),
	}

	f := st.contractFailure()
	if f == nil {
		t.Fatal("contractFailure() = nil, want a failure")
	}
	if f.Kind != FailureContractResponse {
		t.Errorf("Kind = %q, want %q", f.Kind, FailureContractResponse)
	}
	if f.Message != "the response breaks its contract in 2 places" {
		t.Errorf("Message = %q", f.Message)
	}
	want := []report.Violation{
		{In: "response", Reason: "missing property 'name'", Message: "body failed"},
		{In: "response", Field: "$.id", Reason: "got string, want integer", Message: "body failed"},
	}
	if !reflect.DeepEqual(f.Violations, want) {
		t.Errorf("Violations = %+v, want %+v", f.Violations, want)
	}
	if f.Request == nil || f.Response == nil {
		t.Error("the failure should carry the request and response, as a failed test does")
	}
}

func TestStep_createStepResult_Contract(t *testing.T) {
	broken := map[string]any{"in": "response", "message": "GET Path '/v1/teams' not found"}

	tests := []struct {
		name       string
		contract   *actionrpc.ContractCheck
		wantStatus StatusType
	}{
		{name: "a response that keeps to its contract passes without a test", contract: contractOf(), wantStatus: StatusSuccess},
		{name: "a response that breaks its contract fails", contract: contractOf(broken), wantStatus: StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 200}}, contract: tt.contract}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
					Result:  NewResult(),
				},
			}

			status, testOutput := st.verdict(jCtx)
			result := st.createStepResult("Contract", jCtx, status, testOutput)
			if result.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", result.Status, tt.wantStatus)
			}
			if !result.HasTest {
				t.Error("HasTest = false, want true: the contract checks the step")
			}
			if tt.wantStatus != StatusError {
				return
			}
			if result.Failure == nil || len(result.Failure.Violations) != 1 {
				t.Fatalf("Failure = %+v, want the violation", result.Failure)
			}
			if !strings.Contains(result.TestOutput, "GET Path '/v1/teams' not found") {
				t.Errorf("TestOutput = %q, want the violation", result.TestOutput)
			}
			if got := jCtx.Result.exitCode(true); got != ExitTestFailed {
				t.Errorf("exit code = %d, want %d", got, ExitTestFailed)
			}
		})
	}
}

func TestStep_executeActionWithRetry_ContractBroken(t *testing.T) {
	callCount := 0
	mock := &callCountingRunner{
		callCount: &callCount,
		resultFunc: func(count int) map[string]any {
			return map[string]any{"status": 0, "res": map[string]any{"code": 200}}
		},
		contractFunc: func(count int) *actionrpc.ContractCheck {
			if count <= 2 {
				return contractOf(map[string]any{"in": "response", "message": "body failed"})
			}
			return contractOf()
		},
	}
	step := &stepRun{
		Step: &Step{
			Uses: "http",
			Test: "res.code == 200",
			Retry: &StepRetry{
				MaxAttempts: 3,
				Interval:    Interval{Duration: 10 * time.Millisecond},
			},
		},
		expr: &expr.Expr{},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
		},
	}

	if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The test holds every time; the attempts that break the contract are
	// retried as failed tests are.
	if callCount != 3 {
		t.Errorf("attempts = %d, want 3", callCount)
	}
}

func TestStep_handleRepeatExecution_Contract(t *testing.T) {
	step := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
		repeat: &jobRepeat{
			counters: make(map[int]StepRepeatCounter),
			total:    2,
		},
	}

	step.contract = contractOf()
	step.handleRepeatExecution(jCtx, "Contract", false)
	step.contract = contractOf(map[string]any{"in": "response", "message": "body failed"})
	step.handleRepeatExecution(jCtx, "Contract", false)

	counter := jCtx.repeat.counters[0]
	if counter.SuccessCount != 1 || counter.FailureCount != 1 {
		t.Errorf("counts = %d/%d, want 1 success and 1 failure", counter.SuccessCount, counter.FailureCount)
	}
	if !counter.Checked {
		t.Error("Checked = false, want true")
	}
	if counter.Failure == nil || counter.Failure.Kind != FailureContractResponse {
		t.Errorf("Failure = %+v, want a contract failure", counter.Failure)
	}
}

func TestStep_contractFailure_Kind(t *testing.T) {
	request := map[string]any{"in": "request", "message": "request body failed"}
	response := map[string]any{"in": "response", "message": "response body failed"}

	tests := []struct {
		name        string
		violations  []map[string]any
		wantKind    string
		wantMessage string
	}{
		{
			name:        "in the response",
			violations:  []map[string]any{response},
			wantKind:    FailureContractResponse,
			wantMessage: "the response breaks its contract in 1 place",
		},
		{
			name:        "in the request",
			violations:  []map[string]any{request, request},
			wantKind:    FailureContractRequest,
			wantMessage: "the request breaks its contract in 2 places",
		},
		{
			name:        "in both, where the request decides the kind",
			violations:  []map[string]any{request, response, response},
			wantKind:    FailureContractRequest,
			wantMessage: "the request breaks its contract in 1 place, and the response in 2 places",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, contract: contractOf(tt.violations...)}
			f := st.contractFailure()
			if f == nil {
				t.Fatal("contractFailure() = nil, want a failure")
			}
			if f.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", f.Kind, tt.wantKind)
			}
			if f.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", f.Message, tt.wantMessage)
			}
			if len(f.Violations) != len(tt.violations) {
				t.Errorf("Violations = %+v, want %d", f.Violations, len(tt.violations))
			}
		})
	}
}

func TestStep_contractMatched(t *testing.T) {
	tests := []struct {
		name     string
		contract *actionrpc.ContractCheck
		want     *report.Contract
	}{
		{name: "no contract"},
		{name: "a contract with nothing that matches", contract: contractOf()},
		{
			name:     "an operation and a response",
			contract: &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}},
			want:     &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"},
		},
		{
			name:     "an operation that declares no response for the status",
			contract: &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}"}},
			want:     &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{Step: &Step{}, contract: tt.contract}
			if got := st.contractMatched(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("contractMatched() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStep_createStepResult_ContractMatched(t *testing.T) {
	contract := &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}}
	st := &stepRun{Step: &Step{}, expr: &expr.Expr{}, ctx: StepContext{Res: map[string]any{"code": 200}}, contract: contract}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
	}

	result := st.createStepResult("Get a user", jCtx, StatusSuccess, "")
	want := &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}
	if !reflect.DeepEqual(result.Contract, want) {
		t.Errorf("Contract = %+v, want %+v", result.Contract, want)
	}
}

func TestStep_handleRepeatExecution_ContractMatched(t *testing.T) {
	step := &stepRun{Step: &Step{}, idx: 0, expr: &expr.Expr{}}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
			Result:  NewResult(),
		},
		repeat: &jobRepeat{
			counters: make(map[int]StepRepeatCounter),
			total:    2,
		},
		CurrentJobID: "job-1",
	}
	jCtx.Result.Jobs["job-1"] = &JobResult{JobID: "job-1"}

	for _, response := range []string{"200", "2XX"} {
		step.contract = &actionrpc.ContractCheck{Matched: &actionrpc.ContractMatch{Spec: "openapi.yml", Operation: "GET /items", Response: response}}
		step.handleRepeatExecution(jCtx, "Items", false)
	}

	// The first iteration's is kept, as its failure is.
	want := &report.Contract{Spec: "openapi.yml", Operation: "GET /items", Response: "200"}
	if got := jCtx.repeat.counters[0].Contract; !reflect.DeepEqual(got, want) {
		t.Errorf("Contract = %+v, want %+v", got, want)
	}

	// The step's result, made from the counter, carries it.
	e := &Executor{job: &Job{ID: "job-1", Steps: []*Step{step.Step}}}
	e.appendRepeatStepResults(jCtx)
	results := jCtx.Result.Jobs["job-1"].StepResults
	if len(results) != 1 || !reflect.DeepEqual(results[0].Contract, want) {
		t.Errorf("StepResults = %+v, want one carrying %+v", results, want)
	}
}

func TestStep_executeActionWithRetry_ContractOnly(t *testing.T) {
	broken := map[string]any{"in": "response", "message": "body failed"}

	tests := []struct {
		name         string
		brokenUntil  int // the attempts up to this one break the contract
		wantAttempts int
	}{
		{name: "retried until the contract holds", brokenUntil: 2, wantAttempts: 3},
		{name: "retried as many times as allowed", brokenUntil: 5, wantAttempts: 3},
		{name: "not retried when the contract holds", brokenUntil: 0, wantAttempts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			mock := &callCountingRunner{
				callCount: &callCount,
				resultFunc: func(count int) map[string]any {
					return map[string]any{"status": 0, "res": map[string]any{"code": 200}}
				},
				contractFunc: func(count int) *actionrpc.ContractCheck {
					if count <= tt.brokenUntil {
						return contractOf(broken)
					}
					return contractOf()
				},
			}
			// No test: the contract alone checks the step.
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

			if _, err := step.executeActionWithRetry(mock, map[string]any{}, jCtx, "test"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if callCount != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", callCount, tt.wantAttempts)
			}
		})
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

func TestStep_executeAction_Guard(t *testing.T) {
	guard := actionrpc.Guard{ReadOnly: true, Keeps: map[string][]string{"http": {actionrpc.KindReadOnly, actionrpc.KindAllowHost}}, AllowActions: []string{"shell"}}

	tests := []struct {
		name    string
		uses    string
		guard   actionrpc.Guard
		refused bool
	}{
		{name: "an action that keeps to the guard", uses: "http", guard: guard},
		{name: "an action allowed by the person running probe", uses: "shell", guard: guard},
		{name: "an action that does not keep to the guard", uses: "ssh", guard: guard, refused: true},
		{name: "an external action", uses: "./actions/mine", guard: guard, refused: true},
		{name: "any action without a guard", uses: "ssh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockRunner()
			st := &stepRun{Step: &Step{Uses: tt.uses, With: map[string]any{}}, expr: &expr.Expr{}}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Config:  Config{Guard: tt.guard, ActionRunner: mock},
					Printer: newBufferPrinter(),
				},
			}

			_, err := st.executeAction("Step", jCtx)
			if tt.refused {
				if !actionrpc.IsRefused(err) || !strings.Contains(err.Error(), "--allow-action "+tt.uses) {
					t.Fatalf("err = %v, want a refusal naming --allow-action %s", err, tt.uses)
				}
				if n := len(mock.Calls[tt.uses]); n != 0 {
					t.Errorf("the action was run %d times, want none", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			calls := mock.Calls[tt.uses]
			if len(calls) != 1 {
				t.Fatalf("the action was run %d times, want once", len(calls))
			}
			if !reflect.DeepEqual(calls[0].Guard, tt.guard) {
				t.Errorf("the action was told %+v, want %+v", calls[0].Guard, tt.guard)
			}
		})
	}
}

func TestStep_executeActionWithRetry_Refused(t *testing.T) {
	mock := newMockRunner()
	mock.SetError("http", actionrpc.Refuse("POST may write"))
	st := &stepRun{
		Step: &Step{
			Uses:  "http",
			With:  map[string]any{},
			Test:  "res.code == 201",
			Retry: &StepRetry{MaxAttempts: 3, Interval: Interval{Duration: time.Millisecond}},
		},
		expr: &expr.Expr{},
	}
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config:  Config{Guard: actionrpc.Guard{ReadOnly: true, Keeps: map[string][]string{"http": {actionrpc.KindReadOnly}}}, ActionRunner: mock},
			Printer: newBufferPrinter(),
		},
	}

	_, err := st.executeAction("Step", jCtx)
	if !actionrpc.IsRefused(err) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	// What the guard refused is refused again, so it is not retried.
	if n := len(mock.Calls["http"]); n != 1 {
		t.Errorf("attempts = %d, want 1", n)
	}
}

func TestFailureKindOf_Refused(t *testing.T) {
	if got := failureKindOf(fmt.Errorf("step: %w", actionrpc.Refuse("no"))); got != FailureRefused {
		t.Errorf("failureKindOf = %q, want %q", got, FailureRefused)
	}
}

func TestStep_runTest_FalseClause(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name        string
		test        string
		res         map[string]any
		wantMessage string
		wantOutput  string
		notInOutput string
	}{
		{
			name:        "the first false condition and what it read",
			test:        "res.code == 200 &&\nres.body.name == \"alice\"",
			res:         map[string]any{"code": 200, "body": map[string]any{"name": "bob"}},
			wantMessage: `test evaluated to false: res.body.name == "alice" (res.body.name = "bob")`,
			wantOutput:  "       failed: res.body.name == \"alice\"\n         res.body.name = \"bob\"\n       request: ",
		},
		{
			name:        "an action that keeps its response out of the report",
			test:        "res.code == 200",
			res:         map[string]any{"code": 500, "dump": false},
			wantMessage: "test evaluated to false: res.code == 200",
			wantOutput:  "       failed: res.code == 200\n",
			notInOutput: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := NewPrinter(false)
			printer.SetMasker(mask.New(nil, nil))
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}}
			st.ctx = StepContext{Req: map[string]any{}, Res: tt.res}

			out, ok := st.runTest(printer)
			if ok {
				t.Fatal("runTest() passed, want it to fail")
			}
			if st.failure == nil || st.failure.Message != tt.wantMessage {
				t.Errorf("failure = %+v, want the message %q", st.failure, tt.wantMessage)
			}
			if !strings.HasPrefix(out, tt.wantOutput) {
				t.Errorf("output = %q, want it to start with %q", out, tt.wantOutput)
			}
			if tt.notInOutput != "" && strings.Contains(out, tt.notInOutput) {
				t.Errorf("output = %q, want it without %q", out, tt.notInOutput)
			}
		})
	}
}

func TestEvaluateTest(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	env := StepContext{Res: map[string]any{"code": 500}}
	_, evalErr := (&expr.Expr{}).Eval("nosuch(1)", env)
	if evalErr == nil {
		t.Fatal("nosuch(1) evaluated, want an error to test with")
	}

	tests := []struct {
		name         string
		test         string
		hideOperands bool
		wantPassed   bool
		wantKind     string
		wantMessage  string
		wantDetail   string
	}{
		{name: "a test that holds", test: "res.code == 500", wantPassed: true},
		{name: "a test that cannot be evaluated", test: "nosuch(1)", wantKind: FailureTestError, wantMessage: evalErr.Error(), wantDetail: testErrorDetail(evalErr)},
		{name: "a test that is not a boolean", test: "res.code", wantKind: FailureTestType, wantMessage: "test evaluated to 500 (int), not a boolean", wantDetail: typeMismatchDetail("res.code", 500)},
		{name: "a test that is false", test: "res.code == 200", wantKind: FailureAssertion, wantMessage: "test evaluated to false: res.code == 200 (res.code = 500)", wantDetail: "       failed: res.code == 200\n         res.code = 500\n"},
		{name: "a test that is false, its operands hidden", test: "res.code == 200", hideOperands: true, wantKind: FailureAssertion, wantMessage: "test evaluated to false: res.code == 200", wantDetail: "       failed: res.code == 200\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.SetMasker(mask.New(nil, nil))

			passed, failure, detail := evaluateTest(&expr.Expr{}, tt.test, env, printer, tt.hideOperands)
			if passed != tt.wantPassed {
				t.Errorf("passed = %v, want %v", passed, tt.wantPassed)
			}
			if tt.wantPassed {
				if failure != nil || detail != "" {
					t.Errorf("failure = %+v, detail = %q, want neither", failure, detail)
				}
				return
			}
			if failure == nil || failure.Kind != tt.wantKind || failure.Message != tt.wantMessage {
				t.Errorf("failure = %+v, want %s %q", failure, tt.wantKind, tt.wantMessage)
			}
			if failure != nil && (failure.Request != nil || failure.Response != nil) {
				t.Errorf("failure = %+v, want no exchange: the caller adds one", failure)
			}
			if detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", detail, tt.wantDetail)
			}
		})
	}
}

// A test that cannot be evaluated, or is not a boolean, fails the step
// without the exchange, which only a false test is told with.
func TestStep_runTest_ErrorAndType(t *testing.T) {
	tests := []struct {
		name     string
		test     string
		wantKind string
	}{
		{name: "an error", test: "nosuch(1)", wantKind: FailureTestError},
		{name: "not a boolean", test: "res.code", wantKind: FailureTestType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.SetMasker(mask.New(nil, nil))
			st := &stepRun{Step: &Step{Test: tt.test}, expr: &expr.Expr{}, ctx: StepContext{Req: map[string]any{"url": "/"}, Res: map[string]any{"code": 500}}}

			out, ok := st.runTest(printer)
			if ok {
				t.Fatal("runTest() passed, want it to fail")
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want %s", st.failure, tt.wantKind)
			}
			if st.failure.Request != nil || strings.Contains(out, "request:") {
				t.Errorf("failure = %+v, output = %q, want no exchange", st.failure, out)
			}
		})
	}
}

// A verbose run records the steps it skips as any other run does, for the
// report: by their own skipif, and after a failure in a job with fail_fast.
func TestStep_handleSkip_VerboseRecordsTheStep(t *testing.T) {
	for _, afterFailure := range []bool{false, true} {
		printer := newBufferPrinter()
		printer.verbose = true
		printer.SetMasker(mask.New(nil, nil))
		result := NewResult()
		result.Jobs["job"] = &JobResult{JobID: "job"}
		jCtx := &JobContext{
			runEnv: &runEnv{
				Printer: printer,
				Result:  result,
				Config:  Config{Verbose: true},
			},
			CurrentJobID: "job",
		}

		st := &stepRun{Step: &Step{Name: "Skipped", SkipIf: "true"}, expr: &expr.Expr{}}
		st.handleSkip("Skipped", "the reason", afterFailure, jCtx)

		steps := result.Jobs["job"].StepResults
		if len(steps) != 1 || steps[0].Status != StatusSkipped || steps[0].Name != "Skipped (SKIPPED)" {
			t.Errorf("afterFailure %v: results = %+v, want the step recorded as skipped", afterFailure, steps)
		}
	}
}

// A verbose run shows, for a step whose action failed without returning the
// request it made, what the step gave the action, its templates evaluated
// and its credentials hidden, so that it still tells what was asked for.
func TestStep_handleActionError_ShowsTheGivenRequest(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	mock := newMockRunner()
	mock.SetError("http", errors.New("connection refused"))
	st := &stepRun{
		Step: &Step{
			Uses: "http",
			With: map[string]any{"url": "http://{{vars.host}}", "password": "hunter2"},
		},
		expr: &expr.Expr{},
		ctx:  StepContext{Vars: map[string]any{"host": "127.0.0.1:1"}},
	}
	printer := newBufferPrinter()
	printer.verbose = true
	printer.SetMasker(mask.New(nil, nil))
	jCtx := &JobContext{runEnv: &runEnv{Config: Config{Verbose: true, ActionRunner: mock}, Printer: printer}}

	_, err := st.executeAction("h", jCtx)
	if err == nil {
		t.Fatal("expected the action to fail")
	}
	st.handleActionError(err, "h", jCtx)

	out := printer.errWriter.(*bytes.Buffer).String()
	want := "[DEBUG] Request (as the step gave it; the action returned none):\n[DEBUG]   password: <redacted>\n[DEBUG]   url: http://127.0.0.1:1\n"
	if !strings.Contains(out, want) {
		t.Errorf("output does not have\n%s\ngot:\n%s", want, out)
	}
	if strings.Contains(out, "hunter2") || strings.Contains(out, "Response:") {
		t.Errorf("output should hide the password and show no empty response:\n%s", out)
	}
}

// A step whose action returned the request it made shows that, not what the
// step gave it.
func TestStep_handleActionError_ShowsTheRequestTheActionMade(t *testing.T) {
	printer := newBufferPrinter()
	printer.verbose = true
	jCtx := &JobContext{runEnv: &runEnv{Config: Config{Verbose: true}, Printer: printer}}
	st := &stepRun{Step: &Step{Uses: "http"}, expr: &expr.Expr{}, given: map[string]any{"url": "given"}}
	st.ctx.Req = map[string]any{"url": "made"}

	st.handleActionError(errors.New("failed"), "h", jCtx)

	out := printer.errWriter.(*bytes.Buffer).String()
	if !strings.Contains(out, "[DEBUG] Request:\n[DEBUG]   url: made\n") || strings.Contains(out, "given") {
		t.Errorf("output should show the request the action made:\n%s", out)
	}
}

// TestStep_IDAt reads a step by its id, or by where it is written when it
// has none.
func TestStep_IDAt(t *testing.T) {
	if got := (&Step{ID: "login"}).idAt(2); got != "login" {
		t.Errorf("idAt of a step with an id = %q, want login", got)
	}
	if got := (&Step{}).idAt(2); got != "step_2" {
		t.Errorf("idAt of a step without one = %q, want step_2", got)
	}
}
