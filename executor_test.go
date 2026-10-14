package probe

import (
	"maps"
	"strings"
	"sync"
	"testing"
	"time"
)

// Test basic executor creation and interface compliance
func TestJobExecutor_Creation(t *testing.T) {
	workflow := &Workflow{Name: "test-workflow"}
	job := &Job{Name: "test-job", Steps: []*Step{}}

	t.Run("Executor creation", func(t *testing.T) {
		executor := NewExecutor(workflow, job)
		if executor == nil {
			t.Fatal("NewExecutor should not return nil")
		}
	})
}

// TestExecutionResult_Structure is no longer needed as ExecutionResult has been removed

// TestExecutionConfig_Structure is no longer needed as ExecutionConfig has been removed

func TestExecutor_AppendRepeatStepResults(t *testing.T) {
	workflow := &Workflow{Name: "test-workflow"}
	job := &Job{
		Name: "test-job",
		ID:   "test-job",
		Steps: []*Step{
			{
				Name: "test-step",
				Test: "status == 200",
			},
		},
	}
	executor := NewExecutor(workflow, job)

	// Create WorkflowBuffer with JobResult
	result := NewResult()
	jobResult := &JobResult{
		JobName: job.Name,
		JobID:   "test-job",
	}
	result.Jobs["test-job"] = jobResult

	// Create test context with step counters
	ctx := JobContext{
		StepCounters: map[int]StepRepeatCounter{
			0: {
				SuccessCount: 3,
				FailureCount: 1,
				Name:         "test-step",
				LastResult:   true,
			},
		},
		Config:     Config{Verbose: false},
		Printer:    NewPrinter(false, []string{}),
		Result:     result,
		countersMu: &sync.Mutex{},
	}

	// Call the method
	executor.appendRepeatStepResults(&ctx)

	// Check if step results were added to WorkflowBuffer
	jobResult, exists := result.Jobs["test-job"]
	if !exists {
		t.Fatal("Job buffer should exist after appendRepeatStepResults")
	}
	if len(jobResult.StepResults) == 0 {
		t.Error("appendRepeatStepResults should add StepResults to WorkflowBuffer")
	}

	// Should have created a StepResult with RepeatCounter
	if len(jobResult.StepResults) != 1 {
		t.Errorf("Expected 1 step result, got %d", len(jobResult.StepResults))
	}

	stepResult := jobResult.StepResults[0]
	if stepResult.RepeatCounter == nil {
		t.Error("StepResult should have RepeatCounter")
	}

	if stepResult.RepeatCounter.SuccessCount != 3 {
		t.Errorf("Expected SuccessCount=3, got %d", stepResult.RepeatCounter.SuccessCount)
	}

	if stepResult.RepeatCounter.FailureCount != 1 {
		t.Errorf("Expected FailureCount=1, got %d", stepResult.RepeatCounter.FailureCount)
	}
}

func TestJobExecutor_Integration_WithMockJob(t *testing.T) {
	// Test that the executor can handle basic job execution scenarios
	// without relying on the actual job.Start() method which has plugin dependencies

	t.Run("executor creation and interface compliance", func(t *testing.T) {
		workflow := &Workflow{Name: "test-workflow"}
		job := &Job{Name: "test-job", Steps: []*Step{}}

		// Test that the executor can be created
		executor := NewExecutor(workflow, job)

		if executor == nil {
			t.Error("Executor creation failed")
		}
	})
}

func TestExecutor_AsyncRepeat(t *testing.T) {
	t.Run("async flag should be recognized", func(t *testing.T) {
		// Test that async flag is properly set and recognized
		asyncRepeat := &Repeat{
			Count:    10,
			Interval: Interval{Duration: 10 * time.Millisecond},
			Async:    true,
		}

		if !asyncRepeat.Async {
			t.Error("Async flag should be true")
		}

		syncRepeat := &Repeat{
			Count:    10,
			Interval: Interval{Duration: 10 * time.Millisecond},
			Async:    false,
		}

		if syncRepeat.Async {
			t.Error("Async flag should be false")
		}
	})

	t.Run("async repeat structure is valid", func(t *testing.T) {
		workflow := &Workflow{Name: "test-workflow"}
		job := &Job{
			Name: "test-job",
			ID:   "test-job",
			Repeat: &Repeat{
				Count:    5,
				Interval: Interval{Duration: 10 * time.Millisecond},
				Async:    true,
			},
			Steps: []*Step{},
		}

		executor := NewExecutor(workflow, job)
		if executor == nil {
			t.Fatal("Executor should not be nil")
		}

		if job.Repeat == nil {
			t.Fatal("Job repeat should not be nil")
		}

		if !job.Repeat.Async {
			t.Error("Job repeat async flag should be true")
		}
	})
}

// TestExecutor_AsyncRepeat_HandleSkipNoDataRace covers the handleSkip
// path: when an async-repeat job evaluates skipif==true on every iteration,
// each goroutine reaches Job.handleSkip and writes JobResult.Status /
// JobResult.Success on the shared *JobResult. Without locking those
// writes, `go test -race` flags a data race between sibling iterations.
func TestExecutor_AsyncRepeat_HandleSkipNoDataRace(t *testing.T) {
	workflow := &Workflow{
		Name: "async-skip-race-test",
		Jobs: []Job{
			{
				Name:   "always-skip",
				ID:     "always-skip",
				SkipIf: "true",
				Steps:  []*Step{},
				Repeat: &Repeat{
					Count:    20,
					Interval: Interval{Duration: 1 * time.Millisecond},
					Async:    true,
				},
			},
		},
		printer: newBufferPrinter(),
	}

	config := Config{Verbose: false}
	if err := workflow.Start(config); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
}

// TestExecutor_AsyncRepeat_NoDataRace exercises the executeJobRepeatLoopAsync
// path end-to-end with a mocked ActionRunner so that, under `go test -race`,
// any concurrent mutation of shared *Step / *Job state is reported as a race.
//
// Before the fix, every goroutine spawned by the async repeat loop called
// e.job.Start on the same *Job, which in turn mutated j.Name (expandJobName)
// and st.Expr / st.ctx / st.startedAt / st.err / st.retryAttempt on the same
// *Step instances.
func TestExecutor_AsyncRepeat_NoDataRace(t *testing.T) {
	runner := NewMockActionRunner()
	runner.SetResult("hello", map[string]any{"status": 0})

	// Each Step instance has actionRunner pre-wired to the mock so that
	// st.executeAction takes the mock path instead of spawning the
	// real plugin process.
	step := &Step{
		Name:         "tick",
		Uses:         "hello",
		actionRunner: runner,
	}

	workflow := &Workflow{
		Name: "async-race-test",
		Jobs: []Job{
			{
				Name:  "racer",
				ID:    "racer",
				Steps: []*Step{step},
				Repeat: &Repeat{
					Count:    20,
					Interval: Interval{Duration: 1 * time.Millisecond},
					Async:    true,
				},
			},
		},
		printer: newBufferPrinter(),
	}

	config := Config{Verbose: false}
	if err := workflow.Start(config); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
}

// echoRunner answers every action with its own parameters as res, as hello
// does, after sleeping for with.sleep when that is set. It records the
// values of with.got, so that a test can see what each run read.
type echoRunner struct {
	mu  sync.Mutex
	got []string
}

func (r *echoRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	if s, ok := with["sleep"].(string); ok {
		if d, err := time.ParseDuration(s); err == nil {
			time.Sleep(d)
		}
	}
	if got, ok := with["got"].(string); ok {
		r.mu.Lock()
		r.got = append(r.got, got)
		r.mu.Unlock()
	}
	res := make(map[string]any, len(with))
	maps.Copy(res, with)
	return map[string]any{"req": with, "res": res, "status": 0}, nil
}

// repeatOutputsWorkflow repeats a job that publishes a value naming its run,
// waits, and reads it back, followed by a job that reads it after. One step
// is skipped by the last run.
func repeatOutputsWorkflow(runner ActionRunner, async bool) *Workflow {
	repeated := Job{
		Name: "repeated",
		ID:   "repeated",
		Repeat: &Repeat{
			Count:    4,
			Interval: Interval{Duration: 10 * time.Millisecond},
			Async:    async,
		},
		Steps: []*Step{
			{
				Name: "publish", ID: "pub", Uses: "hello", actionRunner: runner,
				With:    map[string]any{"v": "run-{{repeat_index}}"},
				Outputs: map[string]string{"v": "res.v"},
			},
			{
				// The last run skips this one, so what the jobs after read is
				// what the run before left.
				Name: "early", ID: "early", Uses: "hello", actionRunner: runner,
				SkipIf:  "repeat_index == 3",
				With:    map[string]any{"e": "early-{{repeat_index}}"},
				Outputs: map[string]string{"e": "res.e"},
			},
			{
				// Long enough for the other runs to publish meanwhile.
				Name: "pause", ID: "pause", Uses: "hello", actionRunner: runner,
				With: map[string]any{"sleep": "100ms"},
			},
			{
				Name: "read own", ID: "own", Uses: "hello", actionRunner: runner,
				With: map[string]any{"got": "{{outputs.pub.v}}", "want": "run-{{repeat_index}}"},
				Test: "res.got == res.want",
			},
		},
	}
	after := Job{
		Name:  "after",
		ID:    "after",
		Needs: []string{"repeated"},
		Steps: []*Step{
			{
				Name: "read last", ID: "last", Uses: "hello", actionRunner: runner,
				With: map[string]any{"got": "{{outputs.pub.v}}", "early": "{{outputs.early.e}}"},
				Test: `res.got == "run-3" && res.early == "early-2"`,
			},
		},
	}
	return &Workflow{
		Name:    "repeat outputs",
		Jobs:    []Job{repeated, after},
		printer: newBufferPrinter(),
	}
}

func TestRepeatedJobOutputs(t *testing.T) {
	// A repeated job used to save no outputs at all, so its later steps
	// could not read its earlier ones, nor could the jobs after it. Each run
	// reads its own, also when the runs overlap, and the jobs after read
	// those of the last run.
	for _, async := range []bool{false, true} {
		name := "in turn"
		if async {
			name = "async"
		}
		t.Run(name, func(t *testing.T) {
			runner := &echoRunner{}
			w := repeatOutputsWorkflow(runner, async)
			if err := w.Start(Config{}); err != nil {
				t.Fatalf("Start() error: %v", err)
			}
			if w.exitStatus != 0 {
				runner.mu.Lock()
				got := strings.Join(runner.got, ", ")
				runner.mu.Unlock()
				t.Errorf("exit status = %d, want 0; values read: %s", w.exitStatus, got)
			}
		})
	}
}
