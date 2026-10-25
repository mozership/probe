package probe

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/mozership/probe/expr"
)

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
	// The result holds the name alone; the report marks it as skipped.
	if result.Name != "Test Step" {
		t.Errorf("Name = %v, want %v", result.Name, "Test Step")
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

	result := step.createStepResult("Test Step", jCtx, step.verdict(jCtx))

	if result.RT != "250ms" {
		t.Errorf("RT = %v, want %v", result.RT, "250ms")
	}
	if result.RTSec != 0.25 {
		t.Errorf("RTSec = %v, want %v", result.RTSec, 0.25)
	}
	if result.Echo != "Hello World" || !result.HasEcho {
		t.Errorf("Echo = %q (HasEcho %v), want the echo as it is", result.Echo, result.HasEcho)
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
	if result.Failure == nil || result.Failure.Message != "test error message" {
		t.Errorf("Failure = %+v, want the error", result.Failure)
	}
	if out := failureTextOf(result.Failure, result.Test); out != "       test error message\n" {
		t.Errorf("failure text = %q, want %q", out, "       test error message\n")
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

			status := st.verdict(jCtx)
			if status != tt.wantStatus {
				t.Errorf("status = %v, want %v", status, tt.wantStatus)
			}
			if out := failureTextOf(st.failure, st.Test); (out != "") != tt.wantOutput {
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

	result := st.createStepResult("Check", jCtx, StatusError)
	if result.Status != StatusError || result.Failure != failure {
		t.Errorf("result = %+v, want the status it was given and the failure the check left", result)
	}
	if jCtx.Failed {
		t.Error("Failed = true, want the job left as it was")
	}
	if len(jCtx.Result.failures) != 0 {
		t.Errorf("failures = %v, want none recorded", jCtx.Result.failures)
	}

	passed := st.createStepResult("Check", jCtx, StatusSuccess)
	if passed.Failure != nil {
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
			ran := st.createStepResult("Step", jCtx, StatusSuccess)
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
	if result.Failure != nil {
		t.Errorf("Failure = %+v, want none without an error", result.Failure)
	}
}

func TestStep_handleRepeatExecution_AccumulatesEchoes(t *testing.T) {
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
	if got := len(counter.Echoes); got != 3 {
		t.Fatalf("Echoes length = %d, want 3", got)
	}
	for i, out := range counter.Echoes {
		if want := fmt.Sprintf("iteration %d", i+1); out != want {
			t.Errorf("Echoes[%d] = %q, want %q", i, out, want)
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
	if stepResults[0].Echo != "" || stepResults[0].HasEcho {
		t.Errorf("StepResult.Echo should be empty for repeat steps, got %q", stepResults[0].Echo)
	}
	if stepResults[0].RepeatCounter == nil {
		t.Fatal("StepResult.RepeatCounter is nil")
	}
	if got := len(stepResults[0].RepeatCounter.Echoes); got != 3 {
		t.Errorf("StepResult.RepeatCounter.Echoes length = %d, want 3", got)
	}
}

func TestStepRepeatCounterUpdate(t *testing.T) {
	// Test counter update logic
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
		},
		repeat: &jobRepeat{
			total:    10,
			counters: make(map[int]StepRepeatCounter),
		},
		RepeatCurrent: 3,
	}

	step := &stepRun{
		Step: &Step{
			Name: "Test Step",
			Test: "true", // Always success
		},
		idx:  0,
		expr: &expr.Expr{},
	}

	// Capture stdout to avoid test output noise
	oldStdout := os.Stdout
	os.Stdout, _ = os.Open(os.DevNull)
	defer func() { os.Stdout = oldStdout }()

	// Execute multiple times
	for i := 1; i <= 3; i++ {
		jCtx.RepeatCurrent = i
		step.handleRepeatExecution(jCtx, "Test Step", false) // false = no error
	}

	// Check final counter state
	counter := jCtx.repeat.counters[0]
	if counter.SuccessCount != 3 {
		t.Errorf("Expected SuccessCount to be 3, got %d", counter.SuccessCount)
	}
	if counter.FailureCount != 0 {
		t.Errorf("Expected FailureCount to be 0, got %d", counter.FailureCount)
	}
	if counter.Name != "Test Step" {
		t.Errorf("Expected Name to be 'Test Step', got %s", counter.Name)
	}
}
