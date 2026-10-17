package probe

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
)

func TestJob_validateSteps(t *testing.T) {
	tests := []struct {
		name      string
		steps     []*Step
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid steps without outputs",
			steps: []*Step{
				{Name: "Step 1", Uses: "echo"},
				{Name: "Step 2", Uses: "echo"},
			},
			expectErr: false,
		},
		{
			name: "valid steps with results and id",
			steps: []*Step{
				{ID: "auth_step", Name: "Auth", Uses: "http", Outputs: map[string]string{"token": "{{ res.body.token }}"}},
				{Name: "Simple step", Uses: "echo"},
			},
			expectErr: false,
		},
		{
			name: "invalid: outputs without id",
			steps: []*Step{
				{Name: "Step with outputs but no id", Uses: "http", Outputs: map[string]string{"data": "{{ res.body }}"}},
			},
			expectErr: true,
			errMsg:    "step with outputs must have an 'id' field",
		},
		{
			name: "invalid step id format - uppercase",
			steps: []*Step{
				{ID: "Auth_Step", Name: "Auth", Uses: "http", Outputs: map[string]string{"token": "{{ res.body.token }}"}},
			},
			expectErr: true,
			errMsg:    "invalid step ID 'Auth_Step' - only [a-z0-9_-] characters are allowed",
		},
		{
			name: "invalid step id format - special chars",
			steps: []*Step{
				{ID: "auth@step", Name: "Auth", Uses: "http", Outputs: map[string]string{"token": "{{ res.body.token }}"}},
			},
			expectErr: true,
			errMsg:    "invalid step ID 'auth@step' - only [a-z0-9_-] characters are allowed",
		},
		{
			name: "duplicate step ids",
			steps: []*Step{
				{ID: "auth_step", Name: "Auth 1", Uses: "http", Outputs: map[string]string{"token": "{{ res.body.token }}"}},
				{ID: "auth_step", Name: "Auth 2", Uses: "http", Outputs: map[string]string{"data": "{{ res.body.data }}"}},
			},
			expectErr: true,
			errMsg:    "duplicate step ID 'auth_step'",
		},
		{
			name: "valid step ids with allowed characters",
			steps: []*Step{
				{ID: "step_1", Name: "Step 1", Uses: "http", Outputs: map[string]string{"data": "{{ res.body }}"}},
				{ID: "step-2", Name: "Step 2", Uses: "http", Outputs: map[string]string{"result": "{{ res.status }}"}},
				{ID: "step3", Name: "Step 3", Uses: "http", Outputs: map[string]string{"count": "{{ res.body.count }}"}},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{
				Name:  "Test Job",
				Steps: tt.steps,
			}

			err := job.validateSteps()

			if tt.expectErr {
				if err == nil {
					t.Errorf("validateSteps() expected error but got none")
				} else if tt.errMsg != "" && err.Error() != "" {
					// Check if error message contains expected substring
					if len(tt.errMsg) > 0 {
						errorStr := err.Error()
						found := false
						// Simple substring check
						for i := 0; i <= len(errorStr)-len(tt.errMsg); i++ {
							if errorStr[i:i+len(tt.errMsg)] == tt.errMsg {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("validateSteps() error = %v, want error containing %v", err, tt.errMsg)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("validateSteps() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestIsValidStepID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		{
			name:     "valid lowercase letters",
			id:       "auth",
			expected: true,
		},
		{
			name:     "valid with numbers",
			id:       "step1",
			expected: true,
		},
		{
			name:     "valid with underscores",
			id:       "auth_step",
			expected: true,
		},
		{
			name:     "valid with hyphens",
			id:       "auth-step",
			expected: true,
		},
		{
			name:     "valid mixed",
			id:       "auth_step-1",
			expected: true,
		},
		{
			name:     "invalid uppercase",
			id:       "Auth",
			expected: false,
		},
		{
			name:     "invalid special chars",
			id:       "auth@step",
			expected: false,
		},
		{
			name:     "invalid spaces",
			id:       "auth step",
			expected: false,
		},
		{
			name:     "invalid dots",
			id:       "auth.step",
			expected: false,
		},
		{
			name:     "empty string",
			id:       "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidStepID(tt.id)
			if result != tt.expected {
				t.Errorf("isValidStepID(%q) = %v, want %v", tt.id, result, tt.expected)
			}
		})
	}
}

func TestJob_shouldSkip(t *testing.T) {
	tests := []struct {
		name     string
		skipIf   string
		vars     map[string]any
		expected bool
	}{
		{
			name:     "empty skipif",
			skipIf:   "",
			expected: false,
		},
		{
			name:     "skipif true",
			skipIf:   "true",
			expected: true,
		},
		{
			name:     "skipif false",
			skipIf:   "false",
			expected: false,
		},
		{
			name:     "skipif with variable true",
			skipIf:   "vars.skip_job",
			vars:     map[string]any{"skip_job": true},
			expected: true,
		},
		{
			name:     "skipif with variable false",
			skipIf:   "vars.skip_job",
			vars:     map[string]any{"skip_job": false},
			expected: false,
		},
		{
			name:     "skipif with expression",
			skipIf:   `vars.env == "test"`,
			vars:     map[string]any{"env": "test"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{
				Name:   "Test Job",
				SkipIf: tt.skipIf,
			}

			ctx := JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
				Vars:    tt.vars,
				Outputs: NewOutputs(),
			}

			ev := &expr.Expr{}
			result := job.shouldSkip(ev, ctx)

			if result != tt.expected {
				t.Errorf("shouldSkip() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestJob_shouldSkip_errorHandling(t *testing.T) {
	tests := []struct {
		name     string
		skipIf   string
		expected bool
	}{
		{
			name:     "invalid expression",
			skipIf:   "invalid syntax ===",
			expected: false, // Should not skip on error
		},
		{
			name:     "non-boolean result",
			skipIf:   `"string_value"`,
			expected: false, // Should not skip on type error
		},
		{
			name:     "undefined variable",
			skipIf:   "vars.undefined_var",
			expected: false, // Should not skip on evaluation error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{
				Name:   "Test Job",
				SkipIf: tt.skipIf,
			}

			ctx := JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
				Vars:    map[string]any{},
				Outputs: NewOutputs(),
			}

			ev := &expr.Expr{}
			result := job.shouldSkip(ev, ctx)

			if result != tt.expected {
				t.Errorf("shouldSkip() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestJob_handleSkip(t *testing.T) {
	job := &Job{
		ID:   "test-job",
		Name: "Test Job",
	}

	// Create a result with the job entry
	result := NewResult()
	jobResult := &JobResult{
		JobID:   "test-job",
		JobName: "Test Job",
		Status:  "running",
		Success: false,
	}
	result.Jobs["test-job"] = jobResult

	ctx := JobContext{
		runEnv: &runEnv{
			Result:  result,
			Printer: newBufferPrinter(),
			Config:  Config{Verbose: false},
		},
	}

	// Call handleSkip
	job.handleSkip(ctx)

	// Verify the job was marked as skipped
	if jobResult.Status != "skipped" {
		t.Errorf("Expected job status to be 'skipped', got '%s'", jobResult.Status)
	}

	if !jobResult.Success {
		t.Errorf("Expected skipped job to be marked as successful")
	}
}

func TestJob_RunIndependently_Success(t *testing.T) {
	// Test with empty steps - should succeed but with no actual work
	job := &Job{
		Name:  "Empty Test Job",
		Steps: []*Step{},
	}

	vars := map[string]any{"test_var": "test_value"}
	printer := newBufferPrinter()
	success, outputs, report, errorMsg, duration := job.RunIndependently(vars, printer, "test-job")

	// Empty job should succeed
	if !success {
		t.Errorf("Expected empty job to succeed, but it failed with error: %s", errorMsg)
	}

	if outputs == nil {
		t.Errorf("Expected outputs to be non-nil")
	}

	// Report may be empty for jobs with no steps
	if report == "" && len(job.Steps) > 0 {
		t.Errorf("Expected non-empty report for job with steps")
	}

	if duration <= 0 {
		t.Errorf("Expected positive duration, got: %v", duration)
	}
}

func TestJob_RunIndependently_Failure(t *testing.T) {
	// Test with step that returns error via MockActionRunner
	mock := NewMockActionRunner()
	testErr := fmt.Errorf("mock action execution failed")
	mock.SetError("failing-action", testErr)

	job := &Job{
		Name: "Failed Test Job",
		Steps: []*Step{
			{
				Name:         "Failing Step",
				Uses:         "failing-action",
				With:         map[string]any{},
				actionRunner: mock,
			},
		},
	}

	vars := map[string]any{"test_var": "test_value"}
	printer := newBufferPrinter()
	success, outputs, _, errorMsg, duration := job.RunIndependently(vars, printer, "test-job")

	// Job with nonexistent action should fail
	if success {
		t.Errorf("Expected job with invalid action to fail, but it succeeded")
	}

	if errorMsg == "" {
		t.Errorf("Expected error message for failed job")
	}

	if outputs == nil {
		t.Errorf("Expected outputs to be non-nil even for failed job")
	}

	if duration <= 0 {
		t.Errorf("Expected positive duration, got: %v", duration)
	}

	// Verify error message was written to printer's error buffer
	if errBuffer, ok := printer.errWriter.(*bytes.Buffer); ok {
		errorOutput := errBuffer.String()
		if errorOutput == "" {
			t.Errorf("Expected error output in printer buffer, but got empty string")
		}

		// Verify the error message contains expected content
		expectedContent := "mock action execution failed"
		if !strings.Contains(errorOutput, expectedContent) {
			t.Errorf("Expected error output to contain '%s', but got: %s", expectedContent, errorOutput)
		}

		// Verify it contains error formatting
		if !strings.Contains(errorOutput, "Error:") {
			t.Errorf("Expected error output to contain 'Error:' prefix, but got: %s", errorOutput)
		}
	} else {
		t.Errorf("Expected printer.errWriter to be *bytes.Buffer, but got %T", printer.errWriter)
	}
}

func TestJob_RunIndependently_Parameters(t *testing.T) {
	// Test that the function accepts and handles parameters correctly
	job := &Job{
		Name:  "Parameter Test Job",
		Steps: []*Step{}, // Empty steps to avoid plugin issues
	}

	tests := []struct {
		name    string
		vars    map[string]any
		verbose bool
		jobID   string
	}{
		{
			name:    "with variables",
			vars:    map[string]any{"key1": "value1", "key2": 123},
			verbose: true,
			jobID:   "test-job-1",
		},
		{
			name:    "empty variables",
			vars:    map[string]any{},
			verbose: false,
			jobID:   "test-job-2",
		},
		{
			name:    "nil variables",
			vars:    nil,
			verbose: true,
			jobID:   "test-job-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.verbose = tt.verbose
			success, outputs, report, errorMsg, duration := job.RunIndependently(tt.vars, printer, tt.jobID)

			// Empty job should succeed regardless of parameters
			if !success {
				t.Errorf("Expected job to succeed with parameters %v, but failed: %s", tt.vars, errorMsg)
			}

			if outputs == nil {
				t.Errorf("Expected non-nil outputs")
			}

			if duration <= 0 {
				t.Errorf("Expected positive duration")
			}

			// Basic type checks - Report may be empty for jobs with no steps
			if report == "" && len(job.Steps) > 0 {
				t.Errorf("Expected non-empty report for job with steps")
			}

			if errorMsg != "" {
				t.Errorf("Expected no error message for successful job, got: %s", errorMsg)
			}
		})
	}
}

// A job run outside a workflow, as an embedded one is, hides the credentials
// its steps pass to actions, as a workflow does.
func TestJob_RunIndependently_HidesCredentials(t *testing.T) {
	step := &Step{
		Name: "login",
		Uses: "hello",
		With: map[string]any{"password": "inner-only-pass"},
		Test: "true",
	}
	step.actionRunner = NewMockActionRunner()
	job := &Job{Name: "embedded", Steps: []*Step{step}}

	printer := newBufferPrinter()
	if printer.Masker() != nil {
		t.Fatal("the printer already has a masker; the test needs one without")
	}
	job.RunIndependently(map[string]any{}, printer, "embedded")

	if got := printer.Masker().String("sent inner-only-pass"); got != "sent <redacted>" {
		t.Errorf("Masker().String() = %q, want the step's password hidden", got)
	}
}

func TestJob_ApplyDefaults(t *testing.T) {
	withHeaders := &Step{Uses: "http", With: map[string]any{
		"url":     "http://step",
		"headers": map[string]any{"accept": "text/plain"},
	}}
	// A step that sets no with of its own still gets the defaults.
	bare := &Step{Uses: "http"}
	other := &Step{Uses: "shell", With: map[string]any{"cmd": "true"}}

	job := &Job{
		Steps: []*Step{withHeaders, bare, other},
		Defaults: map[string]any{
			"http": map[string]any{
				"url":     "http://default",
				"timeout": "5s",
				"headers": map[string]any{"accept": "application/json", "user-agent": "probe"},
			},
		},
	}
	job.ApplyDefaults()

	want := map[string]any{
		"url":     "http://step",
		"timeout": "5s",
		"headers": map[string]any{"accept": "text/plain", "user-agent": "probe"},
	}
	if !reflect.DeepEqual(withHeaders.With, want) {
		t.Errorf("step with its own values: With = %v, want %v", withHeaders.With, want)
	}
	if bare.With["url"] != "http://default" || bare.With["timeout"] != "5s" {
		t.Errorf("step without with: With = %v, want the defaults", bare.With)
	}
	if !reflect.DeepEqual(other.With, map[string]any{"cmd": "true"}) {
		t.Errorf("step of another action: With = %v, want it untouched", other.With)
	}

	// Defaults that are not a map of objects are ignored.
	(&Job{Steps: []*Step{{Uses: "http"}}, Defaults: "nope"}).ApplyDefaults()
}

func TestJob_RunStandalone(t *testing.T) {
	passing := func() *Step {
		st := &Step{Name: "ok", Uses: "hello", Test: "true"}
		st.actionRunner = NewMockActionRunner()
		return st
	}
	failing := func() *Step {
		st := &Step{Name: "ng", Uses: "hello", Test: "false"}
		st.actionRunner = NewMockActionRunner()
		return st
	}

	tests := []struct {
		name        string
		steps       []*Step
		wantSuccess bool
		wantErr     string
	}{
		{name: "steps pass", steps: []*Step{passing()}, wantSuccess: true},
		{name: "a step fails", steps: []*Step{passing(), failing()}, wantSuccess: false},
		{name: "an invalid step", steps: []*Step{{ID: "Bad ID", Uses: "hello"}}, wantErr: "step validation failed"},
		{name: "an action that cannot be resolved", steps: []*Step{{Uses: "./no-such-action"}}, wantErr: "failed to resolve an external action"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "standalone", Steps: tt.steps}
			run := job.RunStandalone(map[string]any{}, newBufferPrinter(), "standalone", t.TempDir())

			if tt.wantErr != "" {
				if run.Err == nil || !strings.Contains(run.Err.Error(), tt.wantErr) {
					t.Fatalf("Err = %v, want one containing %q", run.Err, tt.wantErr)
				}
				if run.Success {
					t.Error("Success = true for a job that could not run")
				}
				return
			}
			if run.Err != nil {
				t.Fatalf("Err = %v, want none: a failing step is not an error", run.Err)
			}
			if run.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", run.Success, tt.wantSuccess)
			}
		})
	}
}

// RunIndependently keeps the messages it returned before RunStandalone
// existed, since code outside probe may compare them.
func TestJob_RunIndependently_ErrorMessages(t *testing.T) {
	st := &Step{Name: "ng", Uses: "hello", Test: "false"}
	st.actionRunner = NewMockActionRunner()
	_, _, _, msg, _ := (&Job{Name: "j", Steps: []*Step{st}}).RunIndependently(map[string]any{}, newBufferPrinter(), "j")
	if msg != "execution error in job_start: job execution failed" {
		t.Errorf("failed step: errorMsg = %q", msg)
	}

	_, _, _, msg, _ = (&Job{Name: "j", Steps: []*Step{{ID: "Bad ID", Uses: "hello"}}}).RunIndependently(map[string]any{}, newBufferPrinter(), "j")
	if !strings.HasPrefix(msg, "execution error in job_start: step validation failed") {
		t.Errorf("invalid step: errorMsg = %q", msg)
	}
}

// RunIndependently resolves an external action only when a step runs it, as
// it did before RunStandalone resolved them up front.
func TestJob_RunIndependently_ResolvesActionsWhenRun(t *testing.T) {
	missing := func() *Step { return &Step{Name: "missing", Uses: "./no-such-action", Test: "true"} }

	tests := []struct {
		name        string
		job         *Job
		wantSuccess bool
		wantMsg     string
	}{
		{
			name:        "a skipped job",
			job:         &Job{Name: "j", SkipIf: "true", Steps: []*Step{missing()}},
			wantSuccess: true,
		},
		{
			name: "a skipped step",
			job: &Job{Name: "j", Steps: []*Step{func() *Step {
				st := missing()
				st.SkipIf = "true"
				return st
			}()}},
			wantSuccess: true,
		},
		{
			name:    "a step that runs the action",
			job:     &Job{Name: "j", Steps: []*Step{missing()}},
			wantMsg: "execution error in job_start: job execution failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			success, _, _, msg, _ := tt.job.RunIndependently(map[string]any{}, newBufferPrinter(), "j")
			if success != tt.wantSuccess || msg != tt.wantMsg {
				t.Errorf("RunIndependently() = %v, %q; want %v, %q", success, msg, tt.wantSuccess, tt.wantMsg)
			}
		})
	}
}

// TestRunStandaloneTellsTheRun checks that a job run on its own is told the
// run WithRunID names, such as the run of the step that embeds it, or else a
// run of its own.
func TestRunStandaloneTellsTheRun(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts []StandaloneOption
	}{
		{name: "the run given", opts: []StandaloneOption{WithRunID("parent-run")}},
		{name: "a run of its own"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewMockActionRunner()
			runner.SetResult("hello", map[string]any{"status": 0})
			job := &Job{Name: "embedded", Steps: []*Step{{Name: "s", Uses: "hello", actionRunner: runner}}}
			job.RunStandalone(map[string]any{}, newBufferPrinter(), "embedded", "", tt.opts...)

			calls := runner.Calls["hello"]
			if len(calls) != 1 {
				t.Fatalf("calls = %v", calls)
			}
			got := calls[0].Step.RunID
			if len(tt.opts) > 0 && got != "parent-run" {
				t.Errorf("RunID = %q, want the run given", got)
			}
			if len(tt.opts) == 0 && len(got) != 16 {
				t.Errorf("RunID = %q, want a run of its own", got)
			}
		})
	}
}

// TestRunStandaloneUnderAGuard checks that a job run on its own, as one the
// embedded action runs, keeps to the guard WithGuard gives it, so that a
// workflow cannot get round its guard by embedding a job.
func TestRunStandaloneUnderAGuard(t *testing.T) {
	guard := actionrpc.Guard{ReadOnly: true, Keeps: map[string][]string{"hello": {actionrpc.KindReadOnly, actionrpc.KindAllowHost}}}
	dir := t.TempDir()
	// Two local actions: one whose action.yml says it keeps to read-only,
	// and one that says nothing. Neither executable exists, so fetching one
	// would fail the job.
	for name, guardLine := range map[string]string{"declared": "guard: [read-only]\n", "silent": ""} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := "name: " + name + "\n" + guardLine + "runs:\n  using: binary\n  path: missing\n"
		if err := os.WriteFile(filepath.Join(dir, name, "action.yml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := NewMockActionRunner()
	job := &Job{Name: "embedded", Steps: []*Step{
		{Name: "keeps", Uses: "hello", Test: "true", actionRunner: runner},
		{Name: "does not", Uses: "ssh", Test: "true", actionRunner: runner},
		// An external action that declares no guard is refused without its
		// executable being fetched.
		{Name: "silent", Uses: "./silent", Test: "true", actionRunner: runner},
	}}
	run := job.RunStandalone(map[string]any{}, newBufferPrinter(), "embedded", dir, WithGuard(guard))

	if run.Err != nil {
		t.Fatalf("Err = %v, want none", run.Err)
	}
	if run.Success {
		t.Error("Success = true, want the refused steps to fail the job")
	}
	// The guard the steps run under holds what each external action of the
	// job declares, nothing for ./silent.
	calls := runner.Calls["hello"]
	if want := guard.WithKeeps("./silent", nil); len(calls) != 1 || !reflect.DeepEqual(calls[0].Guard, want) {
		t.Errorf("hello was called %v, want once under %+v", calls, want)
	}
	if n := len(runner.Calls["ssh"]) + len(runner.Calls["./silent"]); n != 0 {
		t.Errorf("the refused actions were run %d times, want none", n)
	}
	// The first refusal is told, so that the step that embeds the job is
	// refused as well.
	if run.Refused == nil || !strings.Contains(run.Refused.Reason, `step 1 "does not" of the embedded job: the action ssh does not keep to --read-only`) {
		t.Errorf("Refused = %+v, want the refusal of the ssh step", run.Refused)
	}
}

// TestRunStandaloneTakesTheActionAtItsWord checks that an external action
// whose action.yml declares the kinds of guard the run is under runs under
// it, and is told the guard with what it declared.
func TestRunStandaloneTakesTheActionAtItsWord(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "declared"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "declared", "probe-declared")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: declared\nguard: [read-only]\nruns:\n  using: binary\n  path: probe-declared\n"
	if err := os.WriteFile(filepath.Join(dir, "declared", "action.yml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	guard := actionrpc.Guard{ReadOnly: true}
	runner := NewMockActionRunner()
	job := &Job{Name: "embedded", Steps: []*Step{{Name: "declared", Uses: "./declared", Test: "true", actionRunner: runner}}}
	run := job.RunStandalone(map[string]any{}, newBufferPrinter(), "embedded", dir, WithGuard(guard))

	if run.Err != nil || !run.Success {
		t.Fatalf("Err = %v, Success = %v; want the declared action to run", run.Err, run.Success)
	}
	calls := runner.Calls["./declared"]
	want := guard.WithKeeps("./declared", []string{actionrpc.KindReadOnly})
	if len(calls) != 1 || !reflect.DeepEqual(calls[0].Guard, want) {
		t.Errorf("./declared was called %v, want once under %+v", calls, want)
	}
}

// TestRunStandaloneReplacesAnInheritedDeclaration checks that a job run on
// its own does not take a declaration the guard it was given holds for the
// same uses: its ./silent is read from its own directory, and declares
// nothing, whatever ./silent declared where the guard came from.
func TestRunStandaloneReplacesAnInheritedDeclaration(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "silent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "silent", "action.yml"), []byte("name: silent\nruns:\n  using: binary\n  path: missing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	guard := actionrpc.Guard{ReadOnly: true, Keeps: map[string][]string{"./silent": {actionrpc.KindReadOnly}}}
	runner := NewMockActionRunner()
	job := &Job{Name: "embedded", Steps: []*Step{{Name: "silent", Uses: "./silent", Test: "true", actionRunner: runner}}}
	run := job.RunStandalone(map[string]any{}, newBufferPrinter(), "embedded", dir, WithGuard(guard))

	if run.Err != nil {
		t.Fatalf("Err = %v, want none", run.Err)
	}
	if n := len(runner.Calls["./silent"]); n != 0 {
		t.Errorf("./silent was run %d times, want it refused", n)
	}
	if run.Refused == nil || !strings.Contains(run.Refused.Reason, "the action ./silent does not keep to --read-only") {
		t.Errorf("Refused = %+v, want the refusal of ./silent", run.Refused)
	}
}

func TestRunStandaloneWithoutARefusal(t *testing.T) {
	runner := NewMockActionRunner()
	job := &Job{Name: "embedded", Steps: []*Step{{Name: "fails", Uses: "hello", Test: "false", actionRunner: runner}}}
	run := job.RunStandalone(map[string]any{}, newBufferPrinter(), "embedded", t.TempDir())
	if run.Success || run.Refused != nil {
		t.Errorf("Success = %v, Refused = %+v; want a failure that is not a refusal", run.Success, run.Refused)
	}
}

// Running a job leaves it as written: each run evaluates the job's name and
// its steps anew, and what a run finds out is the run's alone.
func TestJob_RunLeavesTheJobAsWritten(t *testing.T) {
	runner := NewMockActionRunner()
	runner.SetResult("hello", map[string]any{"status": 0, "res": map[string]any{"code": 200}, "rt": "10ms"})
	job := &Job{ID: "work", Name: "Work {{vars.n}}", Steps: []*Step{
		{Name: "Greet {{vars.n}}", Uses: "hello", Test: "res.code == 200", actionRunner: runner},
		{Uses: "hello", actionRunner: runner, Iteration: Iteration{Entries: []map[string]any{{"x": 1}, {"x": 2}}}},
	}}
	written := &Job{Name: job.Name, Steps: []*Step{{}, {}}}
	for i, st := range job.Steps {
		*written.Steps[i] = *st
	}

	for _, n := range []int{1, 2} {
		printer := newBufferPrinter()
		printer.SetMasker(mask.New(nil, nil))
		result := NewResult()
		result.Jobs[job.ID] = &JobResult{JobID: job.ID}
		failed, err := job.run(JobContext{
			runEnv: &runEnv{
				Printer: printer,
				Result:  result,
			},
			Vars:    map[string]any{"n": n},
			Outputs: NewOutputs(),
		})
		if err != nil || failed {
			t.Fatalf("run %d: failed = %v, err = %v", n, failed, err)
		}
		jr := result.Jobs[job.ID]
		if want := fmt.Sprintf("Work %d", n); jr.JobName != want {
			t.Errorf("run %d: job name = %q, want %q", n, jr.JobName, want)
		}
		if want := fmt.Sprintf("Greet %d", n); jr.StepResults[0].Name != want {
			t.Errorf("run %d: step name = %q, want %q", n, jr.StepResults[0].Name, want)
		}
		if jr.StepResults[1].Name != "Unknown Step" {
			t.Errorf("run %d: the name of a step without one = %q, want Unknown Step", n, jr.StepResults[1].Name)
		}
	}

	if job.Name != written.Name {
		t.Errorf("job name = %q, want it as written, %q", job.Name, written.Name)
	}
	for i, st := range job.Steps {
		if !reflect.DeepEqual(*st, *written.Steps[i]) {
			t.Errorf("step %d = %+v, want it as written, %+v", i, *st, *written.Steps[i])
		}
	}
}
