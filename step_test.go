package probe

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/mask"
)

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

// Tests for refactored methods

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

func TestStep_evalEcho(t *testing.T) {
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
			expected:    "Hello World",
			expectError: false,
		},
		{
			name:        "multi-line echo with explicit newlines",
			echo:        "Line 1\nLine 2\nLine 3",
			context:     StepContext{},
			expected:    "Line 1\nLine 2\nLine 3",
			expectError: false,
		},
		{
			name:        "complex multiline with indentation",
			echo:        "Header\n  Indented\n    More indented\nBack to left",
			context:     StepContext{},
			expected:    "Header\n  Indented\n    More indented\nBack to left",
			expectError: false,
		},
		{
			name:        "empty line handling",
			echo:        "Line 1\n\nLine 3",
			context:     StepContext{},
			expected:    "Line 1\n\nLine 3",
			expectError: false,
		},
		{
			name:        "template expression",
			echo:        "Status: {{vars.status}}",
			context:     StepContext{Vars: map[string]any{"status": "OK"}},
			expected:    "Status: OK",
			expectError: false,
		},
		{
			name:        "multiline template expression",
			echo:        "Status: {{vars.status}}\nCode: {{vars.code}}",
			context:     StepContext{Vars: map[string]any{"status": "OK", "code": "200"}},
			expected:    "Status: OK\nCode: 200",
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

			result := step.evalEcho()

			if result != tt.expected {
				t.Errorf("evalEcho() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestStep_evalEcho_Error(t *testing.T) {
	step := &stepRun{
		Step: &Step{
			Echo: "{{invalid_expression + }}", // Invalid syntax that will cause template error
		},
		ctx:  StepContext{},
		expr: &expr.Expr{},
	}

	result := step.evalEcho()

	// An echo that cannot be evaluated comes to its error.
	if !strings.HasPrefix(result, "error: {{invalid_expression + }}: ") {
		t.Errorf("evalEcho() with invalid expression should return error message, got %q", result)
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
		if len(steps) != 1 || steps[0].Status != StatusSkipped || steps[0].Name != "Skipped" {
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
