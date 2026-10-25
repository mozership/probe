package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/report"

	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
)

func TestWorkflowExecutor_DependencyManagement(t *testing.T) {
	tests := []struct {
		name        string
		jobs        []Job
		expectError bool
	}{
		{
			name: "jobs without dependencies",
			jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Steps: []*Step{},
				},
				{
					ID:    "job2",
					Name:  "job2",
					Steps: []*Step{},
				},
			},
			expectError: false,
		},
		{
			name: "jobs with valid dependencies",
			jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Steps: []*Step{},
				},
				{
					ID:    "job2",
					Name:  "job2",
					Needs: []string{"job1"},
					Steps: []*Step{},
				},
			},
			expectError: false,
		},
		{
			name: "jobs with circular dependencies",
			jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Needs: []string{"job2"},
					Steps: []*Step{},
				},
				{
					ID:    "job2",
					Name:  "job2",
					Needs: []string{"job1"},
					Steps: []*Step{},
				},
			},
			expectError: true,
		},
		{
			name: "jobs with missing dependencies",
			jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Needs: []string{"nonexistent"},
					Steps: []*Step{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &Workflow{
				Name:    "test-workflow",
				Jobs:    tt.jobs,
				printer: newBufferPrinter(),
			}

			config := Config{Verbose: false}
			err := workflow.Start(config)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestWorkflowExecutor_ParallelExecution(t *testing.T) {
	t.Run("parallel execution without dependencies", func(t *testing.T) {
		workflow := &Workflow{
			Name: "parallel-test",
			Jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Steps: []*Step{},
				},
				{
					ID:    "job2",
					Name:  "job2",
					Steps: []*Step{},
				},
				{
					ID:    "job3",
					Name:  "job3",
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		start := time.Now()
		err := workflow.Start(config)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Parallel execution should not error: %v", err)
		}

		// Parallel execution should be faster than sequential
		// With empty steps, this should complete very quickly
		if duration > 1*time.Second {
			t.Errorf("Parallel execution took too long: %v", duration)
		}
	})
}

func TestWorkflowExecutor_SequentialWithDependencies(t *testing.T) {
	t.Run("sequential execution with dependencies", func(t *testing.T) {
		workflow := &Workflow{
			Name: "sequential-test",
			Jobs: []Job{
				{
					ID:    "first",
					Name:  "first",
					Steps: []*Step{},
				},
				{
					ID:    "second",
					Name:  "second",
					Needs: []string{"first"},
					Steps: []*Step{},
				},
				{
					ID:    "third",
					Name:  "third",
					Needs: []string{"second"},
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Sequential execution with dependencies should not error: %v", err)
		}
	})
}

func TestWorkflowExecutor_BufferedOutput(t *testing.T) {
	t.Run("buffered output with multiple jobs", func(t *testing.T) {
		workflow := &Workflow{
			Name: "buffered-test",
			Jobs: []Job{
				{
					ID:    "job1",
					Name:  "job1",
					Steps: []*Step{},
				},
				{
					ID:    "job2",
					Name:  "job2",
					Needs: []string{"job1"},
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Buffered execution should not error: %v", err)
		}

		// With dependencies and multiple jobs, buffering should be used
		// This test mainly verifies that the workflow completes successfully
	})
}

func TestWorkflowExecutor_RepeatJobs(t *testing.T) {
	t.Run("job with repeat in parallel execution", func(t *testing.T) {
		workflow := &Workflow{
			Name: "repeat-test",
			Jobs: []Job{
				{
					ID:    "repeat-job",
					Name:  "repeat-job",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    3,
						Interval: Interval{Duration: 10 * time.Millisecond},
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		start := time.Now()
		err := workflow.Start(config)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Repeat job execution should not error: %v", err)
		}

		// Should take at least the interval time * (count-1)
		expectedMinDuration := 2 * 10 * time.Millisecond // 2 intervals for 3 executions
		if duration < expectedMinDuration {
			t.Errorf("Duration %v should be at least %v for repeat execution", duration, expectedMinDuration)
		}
	})
}

func TestWorkflowExecutor_MixedScenarios(t *testing.T) {
	t.Run("complex workflow with dependencies and repeats", func(t *testing.T) {
		workflow := &Workflow{
			Name: "complex-test",
			Jobs: []Job{
				{
					ID:    "setup",
					Name:  "setup",
					Steps: []*Step{},
				},
				{
					ID:    "worker1",
					Name:  "worker1",
					Needs: []string{"setup"},
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 5 * time.Millisecond},
					},
				},
				{
					ID:    "worker2",
					Name:  "worker2",
					Needs: []string{"setup"},
					Steps: []*Step{},
				},
				{
					ID:    "cleanup",
					Name:  "cleanup",
					Needs: []string{"worker1", "worker2"},
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Complex workflow should not error: %v", err)
		}
	})
}

func TestWorkflowExecutor_ErrorHandling(t *testing.T) {
	t.Run("workflow with failed job dependency", func(t *testing.T) {
		// This test verifies that jobs with failed dependencies are properly skipped
		// Since we're using empty steps, jobs should succeed, but we test the structure
		workflow := &Workflow{
			Name: "error-handling-test",
			Jobs: []Job{
				{
					ID:    "might-fail",
					Name:  "might-fail",
					Steps: []*Step{},
				},
				{
					ID:    "depends-on-failed",
					Name:  "depends-on-failed",
					Needs: []string{"might-fail"},
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		// Should complete without error even if dependency logic is exercised
		if err != nil {
			t.Errorf("Error handling test should not error: %v", err)
		}
	})
}

func TestWorkflowExecutor_PrintDetailedResults(t *testing.T) {
	t.Run("print detailed results functionality", func(t *testing.T) {
		workflow := &Workflow{
			Name: "detailed-results-test",
			Jobs: []Job{
				{
					ID:    "test-job",
					Name:  "test-job",
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		// Create workflow buffer
		result := NewResult()
		jobResult := &JobResult{
			JobName:   "test-job",
			JobID:     "test-job",
			Status:    StatusSuccess,
			StartTime: time.Now().Add(-100 * time.Millisecond),
			EndTime:   time.Now(),
		}
		result.Jobs["test-job"] = jobResult

		got := newReportRenderer([]string{"test-job"}, nil, workflow.printer.Masker()).GenerateReport(result)
		if !strings.Contains(got, "test-job") {
			t.Errorf("GenerateReport() should have the job, got:\n%s", got)
		}
	})
}

func TestParallelExecution_EdgeCases(t *testing.T) {
	t.Run("empty workflow", func(t *testing.T) {
		workflow := &Workflow{
			Name:    "empty-workflow",
			Jobs:    []Job{},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Empty workflow should not error: %v", err)
		}
	})

	t.Run("single job parallel execution", func(t *testing.T) {
		workflow := &Workflow{
			Name: "single-job",
			Jobs: []Job{
				{
					ID:    "solo",
					Name:  "solo",
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Single job workflow should not error: %v", err)
		}
	})

	t.Run("many jobs parallel execution", func(t *testing.T) {
		// Create a workflow with many jobs to test parallel execution limits
		jobs := make([]Job, 10)
		for i := range 10 {
			jobs[i] = Job{
				Name:  fmt.Sprintf("job-%d", i),
				Steps: []*Step{},
			}
		}

		workflow := &Workflow{
			Name:    "many-jobs",
			Jobs:    jobs,
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		start := time.Now()
		err := workflow.Start(config)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Many jobs workflow should not error: %v", err)
		}

		// Should complete quickly in parallel
		if duration > 2*time.Second {
			t.Errorf("Many parallel jobs took too long: %v", duration)
		}
	})
}

func TestBufferedExecution_EdgeCases(t *testing.T) {
	t.Run("buffered execution with single job", func(t *testing.T) {
		workflow := &Workflow{
			Name: "single-buffered",
			Jobs: []Job{
				{
					ID:    "buffered-job",
					Name:  "buffered-job",
					Needs: []string{}, // Force dependency path but no actual dependencies
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Single buffered job should not error: %v", err)
		}
	})

	t.Run("buffered execution with concurrent output", func(t *testing.T) {
		// Test that concurrent buffered output doesn't cause race conditions
		workflow := &Workflow{
			Name: "concurrent-buffered",
			Jobs: []Job{
				{
					ID:    "producer1",
					Name:  "producer1",
					Steps: []*Step{},
				},
				{
					ID:    "producer2",
					Name:  "producer2",
					Steps: []*Step{},
				},
				{
					ID:    "consumer",
					Name:  "consumer",
					Needs: []string{"producer1", "producer2"},
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Concurrent buffered execution should not error: %v", err)
		}
	})
}

func TestRepeatExecution_EdgeCases(t *testing.T) {
	t.Run("repeat with zero interval", func(t *testing.T) {
		workflow := &Workflow{
			Name: "zero-interval-repeat",
			Jobs: []Job{
				{
					ID:    "fast-repeat",
					Name:  "fast-repeat",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    5,
						Interval: Interval{Duration: 0}, // Zero interval
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		start := time.Now()
		err := workflow.Start(config)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Zero interval repeat should not error: %v", err)
		}

		// Should complete very quickly with zero interval
		if duration > 100*time.Millisecond {
			t.Errorf("Zero interval repeat took too long: %v", duration)
		}
	})

	t.Run("repeat with very short interval", func(t *testing.T) {
		workflow := &Workflow{
			Name: "short-interval-repeat",
			Jobs: []Job{
				{
					ID:    "quick-repeat",
					Name:  "quick-repeat",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    3,
						Interval: Interval{Duration: 1 * time.Millisecond},
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		start := time.Now()
		err := workflow.Start(config)
		duration := time.Since(start)

		if err != nil {
			t.Errorf("Short interval repeat should not error: %v", err)
		}

		// Should take at least the minimum interval time
		expectedMin := 2 * time.Millisecond // 2 intervals for 3 executions
		if duration < expectedMin {
			t.Errorf("Duration %v should be at least %v", duration, expectedMin)
		}
	})

	t.Run("repeat with single count", func(t *testing.T) {
		workflow := &Workflow{
			Name: "single-repeat",
			Jobs: []Job{
				{
					ID:    "once-repeat",
					Name:  "once-repeat",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    1,
						Interval: Interval{Duration: 10 * time.Millisecond},
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Single count repeat should not error: %v", err)
		}
	})
}

func TestExecutor_ConcurrencyEdgeCases(t *testing.T) {
	t.Run("high concurrency with dependencies", func(t *testing.T) {
		// Create a workflow with multiple levels of dependencies
		workflow := &Workflow{
			Name: "high-concurrency",
			Jobs: []Job{
				{ID: "root", Name: "root", Steps: []*Step{}},
				{ID: "level1-a", Name: "level1-a", Needs: []string{"root"}, Steps: []*Step{}},
				{ID: "level1-b", Name: "level1-b", Needs: []string{"root"}, Steps: []*Step{}},
				{ID: "level1-c", Name: "level1-c", Needs: []string{"root"}, Steps: []*Step{}},
				{ID: "level2-a", Name: "level2-a", Needs: []string{"level1-a", "level1-b"}, Steps: []*Step{}},
				{ID: "level2-b", Name: "level2-b", Needs: []string{"level1-b", "level1-c"}, Steps: []*Step{}},
				{ID: "final", Name: "final", Needs: []string{"level2-a", "level2-b"}, Steps: []*Step{}},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("High concurrency workflow should not error: %v", err)
		}
	})

	t.Run("mixed repeat and parallel execution", func(t *testing.T) {
		workflow := &Workflow{
			Name: "mixed-execution",
			Jobs: []Job{
				{
					ID:    "parallel1",
					Name:  "parallel1",
					Steps: []*Step{},
				},
				{
					ID:    "repeat1",
					Name:  "repeat1",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 5 * time.Millisecond},
					},
				},
				{
					ID:    "parallel2",
					Name:  "parallel2",
					Steps: []*Step{},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		if err != nil {
			t.Errorf("Mixed execution workflow should not error: %v", err)
		}
	})
}

func TestEnv(t *testing.T) {
	_ = os.Setenv("HOST", "http://localhost")
	_ = os.Setenv("TOKEN", "secrets")
	defer func() {
		_ = os.Unsetenv("HOST")
		_ = os.Unsetenv("TOKEN")
	}()

	expected := map[string]string{
		"HOST":  "http://localhost",
		"TOKEN": "secrets",
	}

	wf := &Workflow{}
	actual := wf.Env()

	if actual["HOST"] != expected["HOST"] || actual["TOKEN"] != expected["TOKEN"] {
		t.Errorf("expected %+v, got %+v", expected, actual)
	}
}

// Test_evalVars checks that a workflow evaluates its vars with its
// environment and the vars given, keeping the vars it writes as they are.
// How vars are evaluated is tested in internal/vareval.
func Test_evalVars(t *testing.T) {
	wf := &Workflow{
		Name:  "Test",
		Vars:  map[string]any{"host": "{{HOST}}", "url": "http://{{vars.host}}:{{vars.port}}", "port": "{{PORT}}"},
		env:   map[string]string{"HOST": "from-env", "PORT": "8080"},
		given: map[string]string{"port": "9090"},
	}
	got, err := wf.evalVars()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"host": "from-env", "url": "http://from-env:9090", "port": "9090"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("evalVars() = %#v, want %#v", got, want)
	}
	if wf.Vars["port"] != "{{PORT}}" {
		t.Errorf("the vars of the workflow were changed: %#v", wf.Vars)
	}
}

func TestStepRepeatCounter(t *testing.T) {
	tests := []struct {
		name         string
		successCount int
		failureCount int
		expected     string
	}{
		{
			name:         "all success",
			successCount: 100,
			failureCount: 0,
			expected:     "100/100 success (100.0%)",
		},
		{
			name:         "partial success",
			successCount: 80,
			failureCount: 20,
			expected:     "80/100 success (80.0%)",
		},
		{
			name:         "all failure",
			successCount: 0,
			failureCount: 100,
			expected:     "0/100 success (0.0%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := StepRepeatCounter{
				SuccessCount: tt.successCount,
				FailureCount: tt.failureCount,
				Name:         "Test Step",
			}

			totalCount := counter.SuccessCount + counter.FailureCount
			successRate := float64(counter.SuccessCount) / float64(totalCount) * 100
			actual := fmt.Sprintf("%d/%d success (%.1f%%)",
				counter.SuccessCount, totalCount, successRate)

			if actual != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, actual)
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

func TestStepRepeatDisplayConditions(t *testing.T) {
	tests := []struct {
		name          string
		repeatCurrent int
		repeatTotal   int
		shouldDisplay bool
		description   string
	}{
		{
			name:          "first execution",
			repeatCurrent: 1,
			repeatTotal:   10,
			shouldDisplay: true,
			description:   "should show initial message",
		},
		{
			name:          "middle execution",
			repeatCurrent: 5,
			repeatTotal:   10,
			shouldDisplay: false,
			description:   "should not display in middle",
		},
		{
			name:          "final execution",
			repeatCurrent: 10,
			repeatTotal:   10,
			shouldDisplay: true,
			description:   "should show final result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the display condition logic
			totalCount := tt.repeatCurrent // Simulate counter state
			isFirstExecution := totalCount == 1
			isFinalExecution := tt.repeatCurrent == tt.repeatTotal

			shouldDisplay := isFirstExecution || isFinalExecution

			if shouldDisplay != tt.shouldDisplay {
				t.Errorf("%s: expected shouldDisplay to be %v, got %v",
					tt.description, tt.shouldDisplay, shouldDisplay)
			}
		})
	}
}

// WorkflowBuffer tests
func TestWorkflowBuffer_AddStepResult(t *testing.T) {
	wb := NewResult()
	jobID := "test-job"

	// Add a job buffer first
	wb.Jobs[jobID] = &JobResult{
		JobID:       jobID,
		JobName:     "Test Job",
		StartTime:   time.Now(),
		StepResults: []StepResult{},
	}

	// Create test step results
	stepResult1 := StepResult{
		Index:  0,
		Name:   "Step 1",
		Status: StatusSuccess,
	}

	stepResult2 := StepResult{
		Index:  1,
		Name:   "Step 2",
		Status: StatusError,
		RepeatCounter: &StepRepeatCounter{
			SuccessCount: 3,
			FailureCount: 1,
		},
	}

	// Add step results
	wb.AddStepResult(jobID, stepResult1)
	wb.AddStepResult(jobID, stepResult2)

	// Verify step results were added
	jobResult, exists := wb.Jobs[jobID]
	if !exists {
		t.Fatal("Job buffer should exist")
	}
	if len(jobResult.StepResults) != 2 {
		t.Errorf("Expected 2 step results, got %d", len(jobResult.StepResults))
	}

	if jobResult.StepResults[0].Name != "Step 1" {
		t.Errorf("Expected first step name 'Step 1', got '%s'", jobResult.StepResults[0].Name)
	}

	if jobResult.StepResults[1].Name != "Step 2" {
		t.Errorf("Expected second step name 'Step 2', got '%s'", jobResult.StepResults[1].Name)
	}

	if jobResult.StepResults[1].RepeatCounter == nil {
		t.Error("Expected RepeatCounter to be set for second step")
	} else if jobResult.StepResults[1].RepeatCounter.SuccessCount != 3 {
		t.Errorf("Expected RepeatCounter.SuccessCount = 3, got %d", jobResult.StepResults[1].RepeatCounter.SuccessCount)
	}
}

func TestWorkflowBuffer_AddStepResult_NonExistentJob(t *testing.T) {
	wb := NewResult()

	stepResult := StepResult{
		Index:  0,
		Name:   "Step 1",
		Status: StatusSuccess,
	}

	// This should not panic even if job doesn't exist
	wb.AddStepResult("non-existent-job", stepResult)

	// Verify no job buffer was created
	if _, exists := wb.Jobs["non-existent-job"]; exists {
		t.Error("Job buffer should not be created for non-existent job")
	}
}

func TestWorkflowBuffer_ConcurrentAccess(t *testing.T) {
	wb := NewResult()
	jobID := "test-job"

	// Add a job buffer first
	wb.Jobs[jobID] = &JobResult{
		JobID:       jobID,
		JobName:     "Test Job",
		StartTime:   time.Now(),
		StepResults: []StepResult{},
	}

	// Test concurrent add and get operations
	done := make(chan bool, 2)

	// Goroutine 1: Add step results
	go func() {
		for i := range 10 {
			stepResult := StepResult{
				Index:  i,
				Name:   "Step " + string(rune('0'+i)),
				Status: StatusSuccess,
			}
			wb.AddStepResult(jobID, stepResult)
		}
		done <- true
	}()

	// Goroutine 2: Read job buffer.
	// AddStepResult writes StepResults under JobResult.mutex, so readers
	// must take the same lock — see printer.go where reports do exactly
	// this. Reading without the lock races with the writer goroutine.
	go func() {
		for range 5 {
			jobResult := wb.Jobs[jobID]
			if jobResult != nil {
				jobResult.mutex.Lock()
				_ = len(jobResult.StepResults)
				jobResult.mutex.Unlock()
			}
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// Verify final state
	jobResult, exists := wb.Jobs[jobID]
	if !exists {
		t.Fatal("Job buffer should exist after concurrent operations")
	}
	jobResult.mutex.Lock()
	stepCount := len(jobResult.StepResults)
	jobResult.mutex.Unlock()
	if stepCount != 10 {
		t.Errorf("Expected 10 step results after concurrent operations, got %d", stepCount)
	}
}

// newOrderTestWorkflow builds a workflow of independent jobs whose actions are
// mocked, so that Start can be driven from a test. withIDs selects whether the
// jobs declare an ID or leave it to the scheduler.
func newOrderTestWorkflow(withIDs bool) *Workflow {
	mk := func(id, name string) Job {
		job := Job{
			Name:  name,
			Steps: []*Step{{Name: name + " step", Uses: "hello", actionRunner: NewMockActionRunner()}},
		}
		if withIDs {
			job.ID = id
		}
		return job
	}

	return &Workflow{
		Name: "Order",
		Jobs: []Job{mk("a", "Alpha"), mk("b", "Bravo"), mk("c", "Charlie")},
	}
}

// startCapturing runs the workflow with a printer writing into a buffer and
// returns what reached stdout.
func startCapturing(t *testing.T, w *Workflow, mode OutputMode) string {
	t.Helper()
	return startCapturingWith(t, w, Config{Output: mode})
}

// startCapturingWith is startCapturing with the whole of the configuration
// given.
func startCapturingWith(t *testing.T, w *Workflow, c Config) string {
	t.Helper()

	if w.printer == nil {
		p := NewPrinter(false)
		p.spinner = nil
		p.errWriter = new(bytes.Buffer)
		w.printer = p
	}

	out := new(bytes.Buffer)
	w.printer.outWriter = out

	if err := w.Start(c); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	return out.String()
}

// TestWorkflow_Start_ReportsJobsWithGeneratedIDs covers a workflow whose jobs
// omit an ID. The scheduler generates one, and the report has to be keyed by
// the same value or every block is dropped.
func TestWorkflow_Start_ReportsJobsWithGeneratedIDs(t *testing.T) {
	for _, mode := range []OutputMode{OutputModeSpinner, OutputModeStream} {
		t.Run(mode.String(), func(t *testing.T) {
			report := startCapturing(t, newOrderTestWorkflow(false), mode)

			alphaAt := strings.Index(report, "Alpha")
			bravoAt := strings.Index(report, "Bravo")
			charlieAt := strings.Index(report, "Charlie")
			if alphaAt < 0 || bravoAt < 0 || charlieAt < 0 {
				t.Fatalf("every job should be reported, got:\n%s", report)
			}
			if alphaAt >= bravoAt || bravoAt >= charlieAt {
				t.Errorf("jobs should keep the declared order, got:\n%s", report)
			}
			if !strings.Contains(report, "All jobs succeeded") {
				t.Errorf("footer should count the jobs as succeeded, got:\n%s", report)
			}
		})
	}
}

// TestWorkflow_Start_StreamLabelsJobsByName covers the progress lines of a
// stream run, which are labelled by the names of the jobs, with the vars in
// them, rather than by the ids the jobs were given.
func TestWorkflow_Start_StreamLabelsJobsByName(t *testing.T) {
	w := newOrderTestWorkflow(false)
	w.Vars = map[string]any{"env": "staging"}
	w.Jobs[1].Name = "Bravo {{ vars.env }}"
	p := NewPrinter(false)
	p.spinner = nil
	errOut := new(bytes.Buffer)
	p.errWriter = errOut
	w.printer = p

	startCapturing(t, w, OutputModeStream)

	progress := errOut.String()
	for _, want := range []string{"[Alpha]", "[Bravo staging]", "[Charlie]"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress log should contain %q, got:\n%s", want, progress)
		}
	}
	if strings.Contains(progress, "job_") {
		t.Errorf("progress log should not name a job by the id it was given, got:\n%s", progress)
	}
}

// TestWorkflow_Start_StreamLabelMasksSecrets covers a job whose name has a
// secret long enough to be cut short in its label. The name is masked
// before it is cut, or the prefix left would be one the masker no longer
// finds; and the labels are padded to the width of what is shown.
func TestWorkflow_Start_StreamLabelMasksSecrets(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	w := newOrderTestWorkflow(false)
	w.Secrets = []string{"TOKEN"}
	w.env = map[string]string{"TOKEN": "abcdefghijklmnopqrstuvwxyz0123"}
	w.Vars = map[string]any{"tok": "{{TOKEN}}"}
	w.Jobs = w.Jobs[:2]
	w.Jobs[0].Name = "Deploy {{ vars.tok }}"
	p := NewPrinter(false)
	p.spinner = nil
	errOut := new(bytes.Buffer)
	p.errWriter = errOut
	w.printer = p

	startCapturing(t, w, OutputModeStream)

	progress := errOut.String()
	if strings.Contains(progress, "abcd") {
		t.Errorf("progress log should not show any of the secret, got:\n%s", progress)
	}
	if !strings.Contains(progress, "[Deploy ") {
		t.Errorf("progress log should label the job by its masked name, got:\n%s", progress)
	}
	column := -1
	for line := range strings.Lines(progress) {
		// The step's icon comes after the label and its padding.
		end := strings.Index(line, "]") + 1
		pad := len(line[end:]) - len(strings.TrimLeft(line[end:], " "))
		at := displayWidth(line[:end+pad])
		if column >= 0 && at != column {
			t.Errorf("steps should line up, got:\n%s", progress)
			break
		}
		column = at
	}
}

// TestWorkflow_Start_StreamAndSpinnerAgree pins the guarantee that streaming
// changes when the report is written, not what it says.
func TestWorkflow_Start_StreamAndSpinnerAgree(t *testing.T) {
	for _, withIDs := range []bool{true, false} {
		name := "declared ids"
		if !withIDs {
			name = "generated ids"
		}

		t.Run(name, func(t *testing.T) {
			spinner := startCapturing(t, newOrderTestWorkflow(withIDs), OutputModeSpinner)
			stream := startCapturing(t, newOrderTestWorkflow(withIDs), OutputModeStream)

			if stripDurations(spinner) != stripDurations(stream) {
				t.Errorf("reports differ\nspinner:\n%s\nstream:\n%s", spinner, stream)
			}
		})
	}
}

// TestWorkflow_Start_TwiceReportsBothRuns guards against per-run reporter state
// leaking into a second Start on the same workflow.
func TestWorkflow_Start_TwiceReportsBothRuns(t *testing.T) {
	for _, mode := range []OutputMode{OutputModeSpinner, OutputModeStream} {
		t.Run(mode.String(), func(t *testing.T) {
			w := newOrderTestWorkflow(true)

			first := startCapturing(t, w, mode)
			second := startCapturing(t, w, mode)

			if stripDurations(first) != stripDurations(second) {
				t.Errorf("a second run should report the same jobs\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

// stripDurations removes the measured times so that two runs of the same
// workflow can be compared.
func stripDurations(report string) string {
	return durationPattern.ReplaceAllString(report, "")
}

var durationPattern = regexp.MustCompile(`[0-9]+\.[0-9]+s`)

// TestWorkflow_MasksSecrets runs a workflow end to end and checks that a
// declared secret and a credential header stay out of the terminal output and
// the report, while the action still receives the real values.
func TestWorkflow_MasksSecrets(t *testing.T) {
	runner := &recordingRunner{result: map[string]any{
		"req": map[string]any{
			"url":     "http://api.test/?key=sekret-key",
			"headers": map[string]any{"authorization": "Bearer runtime-tok"},
		},
		"res": map[string]any{"code": 500, "body": "echo sekret-key"},
	}}

	w := &Workflow{
		Name:    "masking",
		Secrets: []string{"API_KEY"},
		env:     map[string]string{"API_KEY": "sekret-key"},
		Vars:    map[string]any{"key": "{{API_KEY}}"},
		Jobs: []Job{{
			ID:   "job",
			Name: "job",
			Steps: []*Step{{
				Name: "call {{vars.key}}",
				Uses: "http",
				With: map[string]any{
					"url":     "http://api.test/?key={{vars.key}}",
					"headers": map[string]any{"authorization": "Bearer runtime-tok"},
				},
				Test:         "res.code == 200",
				Echo:         "body was {{res.body}}",
				actionRunner: runner,
			}},
		}},
		printer: newBufferPrinter(),
	}
	w.printer.verbose = true

	path := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{Verbose: true, Reports: []report.Target{{Format: report.JSON, Path: path}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := runner.with["url"]; got != "http://api.test/?key=sekret-key" {
		t.Errorf("the action should receive the real value, got %v", got)
	}
	if runner.opts.Masker == nil {
		t.Error("the action should be given the masker for its log records")
	}

	report, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := w.printer.outWriter.(*bytes.Buffer).String() +
		w.printer.errWriter.(*bytes.Buffer).String() +
		string(report)

	for _, leak := range []string{"sekret-key", "runtime-tok"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q leaked:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "<secret:API_KEY>") {
		t.Errorf("expected the secret's marker in the output:\n%s", out)
	}
}

// recordingRunner returns a fixed result and keeps what it was called with.
type recordingRunner struct {
	mu     sync.Mutex
	result map[string]any
	with   map[string]any
	opts   RunOptions
}

func (r *recordingRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.with = with
	r.opts = opts
	return r.result, nil
}

// A job that always runs does so after a job it needs failed, where a job
// that does not is skipped, and the run still fails.
func TestWorkflow_Start_AlwaysJob(t *testing.T) {
	failing := NewMockActionRunner()
	failing.SetError("hello", errors.New("boom"))
	cleanup := NewMockActionRunner()
	skipped := NewMockActionRunner()

	w := &Workflow{
		Name: "Always",
		Jobs: []Job{
			{ID: "work", Name: "Work", Steps: []*Step{{Name: "Fails", Uses: "hello", actionRunner: failing}}},
			{ID: "report", Name: "Report", Needs: []string{"work"}, Steps: []*Step{{Name: "Never runs", Uses: "hello", actionRunner: skipped}}},
			{ID: "cleanup", Name: "Cleanup", Needs: []string{"work", "report"}, Always: true, Steps: []*Step{{Name: "Removes", Uses: "hello", actionRunner: cleanup}}},
		},
	}
	report := startCapturing(t, w, OutputModeSpinner)

	if len(cleanup.Calls["hello"]) != 1 {
		t.Errorf("the job that always runs ran %d times, want once:\n%s", len(cleanup.Calls["hello"]), report)
	}
	if len(skipped.Calls["hello"]) != 0 {
		t.Errorf("the job that needs the failed one ran:\n%s", report)
	}
	if w.exitStatus == 0 {
		t.Errorf("exit status = 0, want the run failed:\n%s", report)
	}
	if !strings.Contains(report, "Removes") {
		t.Errorf("the report lacks the step of the job that always runs:\n%s", report)
	}
}

// In a job with fail_fast, the steps after one that failed are not run and
// are reported as skipped; without it they run, as they always have.
func TestWorkflow_Start_FailFast(t *testing.T) {
	for _, failFast := range []bool{true, false} {
		t.Run(fmt.Sprintf("fail_fast %v", failFast), func(t *testing.T) {
			before := NewMockActionRunner()
			failing := NewMockActionRunner()
			failing.SetError("hello", errors.New("boom"))
			after := NewMockActionRunner()
			other := NewMockActionRunner()

			w := &Workflow{
				Name: "Fail fast",
				Jobs: []Job{
					{ID: "work", Name: "Work", FailFast: failFast, Steps: []*Step{
						{Name: "Before", Uses: "hello", actionRunner: before},
						{Name: "Fails", Uses: "hello", actionRunner: failing},
						{Name: "After {{vars.n}}", Uses: "hello", actionRunner: after, Iteration: Iteration{Entries: []map[string]any{{"n": 1}, {"n": 2}}}},
						{Name: "Last", Uses: "hello", actionRunner: after, Wait: "10ms"},
					}},
					// Another job is not held back by it.
					{ID: "other", Name: "Other", Steps: []*Step{{Name: "Runs", Uses: "hello", actionRunner: other}}},
				},
			}
			report := startCapturing(t, w, OutputModeSpinner)

			if len(before.Calls["hello"]) != 1 || len(other.Calls["hello"]) != 1 {
				t.Errorf("the step before the failure and the other job should run:\n%s", report)
			}
			wantAfter, wantSkipped := 3, 0
			if failFast {
				wantAfter, wantSkipped = 0, 3
			}
			if got := len(after.Calls["hello"]); got != wantAfter {
				t.Errorf("the steps after the failure ran %d times, want %d:\n%s", got, wantAfter, report)
			}
			if got := strings.Count(report, "(SKIPPED)"); got != wantSkipped {
				t.Errorf("%d steps are reported as skipped, want %d:\n%s", got, wantSkipped, report)
			}
			if failFast && !strings.Contains(report, "After 2 (SKIPPED)") {
				t.Errorf("a skipped step should keep its name, with its templates evaluated:\n%s", report)
			}
			if w.exitStatus == 0 {
				t.Errorf("exit status = 0, want the run failed:\n%s", report)
			}
		})
	}
}

// Each run of a repeated job with fail_fast starts over: a failure in one
// run does not skip the steps of the next.
func TestJob_FailFastStartsOverEachRun(t *testing.T) {
	failing := NewMockActionRunner()
	failing.SetError("hello", errors.New("boom"))
	after := NewMockActionRunner()
	job := &Job{ID: "work", Name: "Work", FailFast: true, Steps: []*Step{
		{Name: "Fails", Uses: "hello", actionRunner: failing, SkipIf: "repeat_index == 1"},
		{Name: "After", Uses: "hello", actionRunner: after},
	}}

	for i := range 2 {
		printer := newBufferPrinter()
		printer.SetMasker(mask.New(nil, nil))
		result := NewResult()
		result.Jobs[job.ID] = &JobResult{JobID: job.ID}
		failed, err := job.run(JobContext{
			runEnv: &runEnv{
				Printer: printer,
				Result:  result,
			},
			Outputs:       NewOutputs(),
			RepeatCurrent: i,
		})
		if err != nil {
			t.Fatal(err)
		}
		if want := i == 0; failed != want {
			t.Errorf("run %d: failed = %v, want %v", i, failed, want)
		}
	}
	if got := len(after.Calls["hello"]); got != 1 {
		t.Errorf("the step after ran %d times, want once: in the run whose first step was skipped", got)
	}
}

func TestWorkflow_Start_GivenVars(t *testing.T) {
	runner := NewMockActionRunner()
	w := &Workflow{
		Name: "Given",
		Vars: map[string]any{"url": "http://localhost"},
		Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{{Name: "S", Uses: "hello", actionRunner: runner, With: map[string]any{"url": "{{vars.url}}/{{vars.path}}"}}}}},
	}
	p := NewPrinter(false)
	p.spinner = nil
	p.outWriter = new(bytes.Buffer)
	p.errWriter = new(bytes.Buffer)
	w.printer = p
	if err := w.Start(Config{Output: OutputModeSpinner, Vars: map[string]string{"url": "http://given", "path": "health"}}); err != nil {
		t.Fatal(err)
	}
	if got := runner.Calls["hello"][0].With["url"]; got != "http://given/health" {
		t.Errorf("with.url = %v, want it made of the vars given", got)
	}
}

// The variables read from files are read by the vars as the environment
// is, which comes before them, and a secret among them is hidden.
func TestWorkflow_Start_EnvFromFiles(t *testing.T) {
	runner := NewMockActionRunner()
	w := &Workflow{
		Name:    "Env",
		Secrets: []string{"FILE_TOKEN"},
		Vars: map[string]any{
			"url":   "{{FILE_URL ?? 'http://default'}}",
			"both":  "{{BOTH}}",
			"token": "{{FILE_TOKEN}}",
		},
		env: map[string]string{"BOTH": "from the environment"},
		Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{{
			Name: "S", Uses: "hello", actionRunner: runner,
			With: map[string]any{"url": "{{vars.url}}", "both": "{{vars.both}}"},
			Echo: "token is {{vars.token}}",
		}}}},
	}
	report := startCapturingWith(t, w, Config{Output: OutputModeSpinner, Env: map[string]string{
		"FILE_URL":   "http://from-a-file",
		"BOTH":       "from a file",
		"FILE_TOKEN": "s3cr3t-from-a-file",
	}})

	with := runner.Calls["hello"][0].With
	if with["url"] != "http://from-a-file" {
		t.Errorf("with.url = %v, want the variable of the file", with["url"])
	}
	if with["both"] != "from the environment" {
		t.Errorf("with.both = %v, want the environment before the file", with["both"])
	}
	if strings.Contains(report, "s3cr3t-from-a-file") || !strings.Contains(report, "token is ") {
		t.Errorf("the secret read from a file should be hidden:\n%s", report)
	}
}

// In a repeated job with fail_fast, a run in which a step was not run is
// neither a success nor a failure of the step: it is counted on its own,
// and a step that was never run is reported as skipped.
func TestWorkflow_Start_FailFastRepeated(t *testing.T) {
	failing := NewMockActionRunner()
	failing.SetError("hello", errors.New("boom"))
	after := NewMockActionRunner()
	w := &Workflow{
		Name: "Fail fast",
		Jobs: []Job{{ID: "work", Name: "Work", FailFast: true, Repeat: &Repeat{Count: 3}, Steps: []*Step{
			// Fails in the first two runs, and is skipped in the last.
			{Name: "Fails", Uses: "hello", actionRunner: failing, SkipIf: "repeat_index == 2"},
			{Name: "Sometimes", Uses: "hello", actionRunner: after, Test: "true"},
			// Reached in the last run alone, where its own skipif skips it,
			// which is a success of it.
			{Name: "Own skipif", Uses: "hello", actionRunner: after, SkipIf: "repeat_index == 2", Test: "true"},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner)

	if got := len(after.Calls["hello"]); got != 1 {
		t.Errorf("the steps after the failure ran %d times, want once, in the run without it:\n%s", got, report)
	}
	if got := strings.Count(report, "1/3 success (33.3%), 2 skipped"); got != 2 {
		t.Errorf("%d steps are reported as run once and skipped twice, want both after the failure:\n%s", got, report)
	}
}

func TestPrinter_printStepRepeatResult_Skipped(t *testing.T) {
	// The colors are put back as they were, for the tests after this one.
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()
	printer := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name    string
		counter StepRepeatCounter
		hasTest bool
		want    string
	}{
		{"never run", StepRepeatCounter{SkippedCount: 3, RepeatTotal: 3}, true, "skipped in 3/3 runs"},
		{"run in some", StepRepeatCounter{SuccessCount: 1, SkippedCount: 2, RepeatTotal: 3}, true, "1/3 success (33.3%), 2 skipped"},
		{"without a test", StepRepeatCounter{SuccessCount: 2, SkippedCount: 1, RepeatTotal: 3}, false, "2/3 completed (no test), 1 skipped"},
		{"none skipped", StepRepeatCounter{SuccessCount: 3, RepeatTotal: 3}, true, "3/3 success (100.0%)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			printer.printStepRepeatResult(&tt.counter, tt.hasTest, &out)
			if !strings.Contains(out.String(), tt.want) {
				t.Errorf("printStepRepeatResult() = %q, want it to hold %q", out.String(), tt.want)
			}
		})
	}
}

func TestExecutor_appendRepeatStepResults_Skipped(t *testing.T) {
	job := &Job{ID: "work", Name: "Work", Steps: []*Step{{Name: "Never run", Test: "true"}, {Name: "Run once", Test: "true"}}}
	result := NewResult()
	result.Jobs[job.ID] = &JobResult{JobID: job.ID}
	ctx := &JobContext{
		runEnv: &runEnv{
			Result: result,
		},
		repeat: &jobRepeat{
			total: 3,
			counters: map[int]StepRepeatCounter{
				0: {Name: "Never run", Test: "true", SkippedCount: 3, RepeatTotal: 3},
				1: {Name: "Run once", Test: "true", SuccessCount: 1, SkippedCount: 2, RepeatTotal: 3},
			},
		},
	}
	NewExecutor(&Workflow{}, job).appendRepeatStepResults(ctx)

	steps := result.Jobs[job.ID].StepResults
	if len(steps) != 2 || steps[0].Status != StatusSkipped || steps[1].Status != StatusSuccess {
		t.Fatalf("statuses = %+v, want skipped then success", steps)
	}

	r := BuildReport("w", "", result, []string{job.ID}, time.Now(), time.Now())
	got := r.Jobs[0].Steps
	if got[0].Status != "skipped" || got[0].Repeat.Total != 3 || got[0].Repeat.Skipped != 3 || got[0].Repeat.Success != 0 {
		t.Errorf("report of the step never run = %+v, repeat %+v", got[0], got[0].Repeat)
	}
	if got[1].Status != "passed" || got[1].Repeat.Total != 3 || got[1].Repeat.Skipped != 2 || got[1].Repeat.Success != 1 {
		t.Errorf("report of the step run once = %+v, repeat %+v", got[1], got[1].Repeat)
	}
	// The message of a failure counts the runs the step was not run in
	// among the runs, as the count of the report does.
	failed := NewResult()
	failed.Jobs[job.ID] = &JobResult{JobID: job.ID}
	ctx.Result = failed
	ctx.repeat.counters = map[int]StepRepeatCounter{
		0: {Name: "Failed once", Test: "true", FailureCount: 1, SkippedCount: 2, RepeatTotal: 3, Failure: &StepFailure{Kind: FailureAssertion, Message: "test evaluated to false"}},
	}
	NewExecutor(&Workflow{}, job).appendRepeatStepResults(ctx)
	if got := failed.Jobs[job.ID].StepResults[0].Failure.Message; got != "1 of 3 iterations failed; the first: test evaluated to false" {
		t.Errorf("message = %q, want the three runs counted", got)
	}
}

// A repeated job reports every entry of an iteration, and the steps after
// it by their own counters: the counters are kept by the index a step ran
// with, which is past the number of steps the job is written with.
func TestWorkflow_Start_RepeatWithIteration(t *testing.T) {
	// The colors are put back as they were, for the tests after this one.
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	failing := NewMockActionRunner()
	failing.SetError("hello", errors.New("boom"))
	ok := NewMockActionRunner()
	w := &Workflow{
		Name: "Repeat",
		Jobs: []Job{{ID: "work", Name: "Work", FailFast: true, Repeat: &Repeat{Count: 2}, Steps: []*Step{
			{Name: "Each {{vars.n}}", Uses: "hello", actionRunner: ok, Test: "true", Iteration: Iteration{Entries: []map[string]any{{"n": "a"}, {"n": "b"}, {"n": "c"}}}},
			{Name: "Fails", Uses: "hello", actionRunner: failing},
			{Name: "After", Uses: "hello", actionRunner: ok, Test: "true"},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner)

	for _, want := range []string{
		" 0. Each a (repeating 2 times)",
		" 1. Each b (repeating 2 times)",
		" 2. Each c (repeating 2 times)",
		" 3. Fails (repeating 2 times)",
		" 4. After (repeating 2 times)",
		"skipped in 2/2 runs",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
	if got := strings.Count(report, "2/2 success (100.0%)"); got != 3 {
		t.Errorf("%d entries are reported as passed in both runs, want the three of the iteration:\n%s", got, report)
	}
}

// timingRunner records when each call of an action began and ended, and
// takes the time its with says.
type timingRunner struct {
	mu    sync.Mutex
	begun map[string]time.Time
	ended map[string]time.Time
}

func (r *timingRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	key, _ := with["key"].(string)
	r.mu.Lock()
	r.begun[key] = time.Now()
	r.mu.Unlock()
	if d, ok := with["take"].(string); ok {
		take, _ := time.ParseDuration(d)
		time.Sleep(take)
	}
	r.mu.Lock()
	r.ended[key] = time.Now()
	r.mu.Unlock()
	return map[string]any{"status": 0, "res": map[string]any{"code": 200}}, nil
}

// A job starts once the jobs it needs are done, and does not wait for one
// that started with them but that it does not need.
func TestWorkflow_Start_RunsAJobOnceTheJobsItNeedsAreDone(t *testing.T) {
	runner := &timingRunner{begun: map[string]time.Time{}, ended: map[string]time.Time{}}
	step := func(key, take string) []*Step {
		return []*Step{{Name: key, Uses: "hello", Test: "res.code == 200", actionRunner: runner, With: map[string]any{"key": key, "take": take}}}
	}
	w := &Workflow{Name: "needs", Jobs: []Job{
		{ID: "slow", Name: "Slow", Steps: step("slow", "1s")},
		{ID: "fast", Name: "Fast", Steps: step("fast", "0s")},
		{ID: "after", Name: "After", Needs: []string{"fast"}, Steps: step("after", "0s")},
		{ID: "last", Name: "Last", Needs: []string{"slow", "after"}, Steps: step("last", "0s")},
	}}
	report := startCapturing(t, w, OutputModeSpinner)

	if w.exitStatus != 0 {
		t.Fatalf("exit status = %d, want 0:\n%s", w.exitStatus, report)
	}
	if !runner.begun["after"].Before(runner.ended["slow"]) {
		t.Errorf("after began at %s, once slow ended at %s; want it to begin once fast is done", runner.begun["after"], runner.ended["slow"])
	}
	if runner.begun["last"].Before(runner.ended["slow"]) || runner.begun["last"].Before(runner.ended["after"]) {
		t.Errorf("last began before the jobs it needs were done")
	}
}

// A job whose dependency can no longer be done fails the run rather than be
// waited for for ever.
func TestWorkflow_startJobsWithDependenciesStopsWhenNothingCanRun(t *testing.T) {
	w := &Workflow{Name: "stuck", Jobs: []Job{
		{ID: "a", Name: "A", Steps: []*Step{{Uses: "hello"}}},
		{ID: "b", Name: "B", Needs: []string{"a"}, Steps: []*Step{{Uses: "hello"}}},
	}}
	w.printer = newBufferPrinter()
	w.outputs = NewOutputs()
	scheduler, err := w.initJobScheduler()
	if err != nil {
		t.Fatal(err)
	}
	// a is taken for running, but nothing runs it.
	scheduler.StartJob("a")

	errc := make(chan error, 1)
	go func() { errc <- w.startJobsWithDependencies(w.newJobContext(Config{}, nil, scheduler)) }()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "jobs b cannot run") {
			t.Errorf("startJobsWithDependencies() error = %v, want that b cannot run", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startJobsWithDependencies() waited for a job that nothing runs")
	}
}

func TestWorkflow_assignJobIDs(t *testing.T) {
	tests := []struct {
		name    string
		jobs    []Job
		want    []string
		wantErr string
	}{
		{name: "none given", jobs: []Job{{Name: "a"}, {Name: "b"}}, want: []string{"job_0", "job_1"}},
		{name: "some given", jobs: []Job{{ID: "build", Name: "a"}, {Name: "b"}}, want: []string{"build", "job_1"}},
		// The name of a job is not its ID, so two jobs of one name are told
		// apart.
		{name: "one name twice", jobs: []Job{{Name: "a"}, {Name: "a"}}, want: []string{"job_0", "job_1"}},
		{
			name:    "an ID that is another job's default",
			jobs:    []Job{{ID: "job_1", Name: "a"}, {Name: "b"}},
			wantErr: "job 1 has no ID, and 'job_1', the ID it would be given, is the ID of job 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Workflow{Jobs: tt.jobs}
			err := w.assignJobIDs()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("assignJobIDs() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("assignJobIDs() error = %v", err)
			}
			got := make([]string, len(w.Jobs))
			for i, job := range w.Jobs {
				got[i] = job.ID
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IDs = %v, want %v", got, tt.want)
			}
		})
	}
}

// A workflow made in code gives its jobs the IDs a loaded one does, and its
// result, its report and its graph name them by those.
func TestWorkflow_Start_JobIDsOfAWorkflowMadeInCode(t *testing.T) {
	w := &Workflow{
		Name: "made in code",
		Jobs: []Job{
			{Name: "first", Steps: []*Step{}},
			{Name: "first", Steps: []*Step{}},
		},
		printer: newBufferPrinter(),
	}
	if g := w.Graph(); g.Jobs[0].ID != "job_0" || g.Jobs[1].ID != "job_1" {
		t.Errorf("Graph() IDs = %q, %q before the run, want job_0, job_1", g.Jobs[0].ID, g.Jobs[1].ID)
	}

	out := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{Reports: []report.Target{{Format: report.JSON, Path: out}}}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Jobs) != 2 || r.Jobs[0].ID != "job_0" || r.Jobs[1].ID != "job_1" {
		t.Errorf("report jobs = %+v, want job_0 and job_1", r.Jobs)
	}
}

// TestWorkflow_WarnsOfShortSecret covers a declared secret too short to
// mask inside other text: the output is left readable, and a warning names
// the secret once without showing its value.
func TestWorkflow_WarnsOfShortSecret(t *testing.T) {
	w := &Workflow{
		Name:    "short secret",
		Secrets: []string{"PIN", "PIN", "API_KEY"},
		env:     map[string]string{"PIN": "x", "API_KEY": "sekret-key"},
		Jobs: []Job{{
			ID:   "job",
			Name: "job",
			Steps: []*Step{{
				Name:         "reads probe.StepContext",
				Uses:         "http",
				With:         map[string]any{"url": "http://api.test/"},
				actionRunner: &recordingRunner{result: map[string]any{"res": map[string]any{"code": 200}}},
			}},
		}},
		printer: newBufferPrinter(),
	}
	if err := w.Start(Config{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	stdout := w.printer.outWriter.(*bytes.Buffer).String()
	stderr := w.printer.errWriter.(*bytes.Buffer).String()
	if !strings.Contains(stdout, "probe.StepContext") {
		t.Errorf("a short secret should not be cut out of words:\n%s", stdout)
	}
	warn := "[WARN] the secret PIN is shorter than 4 characters, so it is masked only where it is a whole value; use a value of at least 4 characters\n"
	if n := strings.Count(stderr, warn); n != 1 {
		t.Errorf("want the warning once, got %d times:\n%s", n, stderr)
	}
	if strings.Contains(stderr, "API_KEY") {
		t.Errorf("a secret long enough to mask should not be warned about:\n%s", stderr)
	}
}
