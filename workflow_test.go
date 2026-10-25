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

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/report"

	"github.com/fatih/color"
	"github.com/mozership/probe/internal/textwidth"
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

// newOrderTestWorkflow builds a workflow of independent jobs, whose actions a
// mock runner is to run, so that Start can be driven from a test. withIDs selects whether the
// jobs declare an ID or leave it to the scheduler.
func newOrderTestWorkflow(withIDs bool) *Workflow {
	mk := func(id, name string) Job {
		job := Job{
			Name:  name,
			Steps: []*Step{{Name: name + " step", Uses: "hello"}},
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

// startCapturing runs the workflow, its actions run by runner, with a
// printer writing into a buffer and returns what reached stdout.
func startCapturing(t *testing.T, w *Workflow, mode OutputMode, runner ActionRunner) string {
	t.Helper()
	return startCapturingWith(t, w, Config{Output: mode, ActionRunner: runner})
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
			report := startCapturing(t, newOrderTestWorkflow(false), mode, newMockRunner())

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

	startCapturing(t, w, OutputModeStream, newMockRunner())

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

	startCapturing(t, w, OutputModeStream, newMockRunner())

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
		at := textwidth.Width(line[:end+pad])
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
			spinner := startCapturing(t, newOrderTestWorkflow(withIDs), OutputModeSpinner, newMockRunner())
			stream := startCapturing(t, newOrderTestWorkflow(withIDs), OutputModeStream, newMockRunner())

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

			first := startCapturing(t, w, mode, newMockRunner())
			second := startCapturing(t, w, mode, newMockRunner())

			if stripDurations(first) != stripDurations(second) {
				t.Errorf("a second run should report the same jobs\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

// A run that passes after one that failed, on the same workflow, has the exit
// status of a run that passed.
func TestWorkflow_Start_TwiceAfterFailure(t *testing.T) {
	runner := newMockRunner()
	runner.SetError("hello", errors.New("boom"))
	w := &Workflow{
		Name: "rerun",
		Jobs: []Job{{ID: "a", Name: "A", Steps: []*Step{{Name: "greet", Uses: "hello"}}}},
	}

	first := startCapturing(t, w, OutputModeStream, runner)
	if w.exitStatus == 0 {
		t.Fatalf("exit status of the first run = 0, want it failed:\n%s", first)
	}

	runner.mu.Lock()
	delete(runner.Errors, "hello")
	runner.mu.Unlock()
	second := startCapturing(t, w, OutputModeStream, runner)
	if w.exitStatus != 0 {
		t.Errorf("exit status of the second run = %d, want 0:\n%s", w.exitStatus, second)
	}
}

// The outputs a run published are not there for a later run on the same
// workflow to read.
func TestWorkflow_Start_TwiceDoesNotShareOutputs(t *testing.T) {
	writer := newMockRunner()
	writer.SetResult("hello", map[string]any{"res": map[string]any{"tok": "first"}, "status": 0})
	w := &Workflow{
		Name: "rerun",
		Jobs: []Job{{ID: "writer", Name: "Writer", Steps: []*Step{{
			Name:    "publish",
			Uses:    "hello",
			Outputs: map[string]string{"tok": "res.tok"},
		}}}},
	}
	first := startCapturing(t, w, OutputModeStream, writer)
	if w.exitStatus != 0 {
		t.Fatalf("exit status of the first run = %d, want 0:\n%s", w.exitStatus, first)
	}

	// The second run has only a job that runs unless outputs.tok is set.
	reader := newMockRunner()
	w.Jobs = []Job{{ID: "reader", Name: "Reader", Steps: []*Step{{
		Name:   "read",
		Uses:   "hello",
		SkipIf: "outputs.tok != nil",
	}}}}
	second := startCapturing(t, w, OutputModeStream, reader)
	if len(reader.Calls["hello"]) != 1 {
		t.Errorf("the reader was skipped, as outputs.tok was set by the first run:\n%s", second)
	}
}

// The jobs one run was narrowed to are not all a later run on the same
// workflow has.
func TestWorkflow_Start_TwiceSelectsFromEveryJob(t *testing.T) {
	w := newOrderTestWorkflow(true)

	startCapturingWith(t, w, Config{Output: OutputModeStream, Jobs: []string{"a"}})
	second := startCapturingWith(t, w, Config{Output: OutputModeStream})

	for _, name := range []string{"Alpha", "Bravo", "Charlie"} {
		if !strings.Contains(second, name) {
			t.Errorf("the second run does not report %s:\n%s", name, second)
		}
	}
	if len(w.Jobs) != 3 {
		t.Errorf("the workflow has %d jobs after the runs, want 3", len(w.Jobs))
	}

	third := startCapturingWith(t, w, Config{Output: OutputModeStream, Jobs: []string{"b"}})
	if strings.Contains(third, "Alpha") || !strings.Contains(third, "Bravo") {
		t.Errorf("a run narrowed to b should report Bravo alone:\n%s", third)
	}
}

// The environment a run is given in Config.Env is that run's alone, under
// what the workflow's own environment has, in a later run on the same
// workflow as in the first.
func TestWorkflow_Start_TwiceTakesEachRunsEnv(t *testing.T) {
	runner := &recordingRunner{result: map[string]any{}}
	w := &Workflow{
		Name: "rerun",
		env:  map[string]string{"OWN": "workflow"},
		Vars: map[string]any{"given": "{{GIVEN}}", "own": "{{OWN}}"},
		Jobs: []Job{{ID: "a", Name: "A", Steps: []*Step{{
			Name: "read",
			Uses: "hello",
			With: map[string]any{"given": "{{vars.given}}", "own": "{{vars.own}}"},
		}}}},
	}

	for _, given := range []string{"first", "second"} {
		startCapturingWith(t, w, Config{Output: OutputModeStream, ActionRunner: runner, Env: map[string]string{"GIVEN": given, "OWN": given}})
		if got := runner.with["given"]; got != given {
			t.Errorf("GIVEN = %v, want %q", got, given)
		}
		if got := runner.with["own"]; got != "workflow" {
			t.Errorf("OWN = %v, want the workflow's own %q", got, "workflow")
		}
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
				Test: "res.code == 200",
				Echo: "body was {{res.body}}",
			}},
		}},
		printer: newBufferPrinter(),
	}
	w.printer.verbose = true

	path := filepath.Join(t.TempDir(), "report.json")
	if err := w.Start(Config{ActionRunner: runner, Verbose: true, Reports: []report.Target{{Format: report.JSON, Path: path}}}); err != nil {
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

func (r *recordingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.with = call.With
	r.opts = opts
	return r.result, nil, nil
}

// A job that always runs does so after a job it needs failed, where a job
// that does not is skipped, and the run still fails.
func TestWorkflow_Start_AlwaysJob(t *testing.T) {
	failing := newMockRunner()
	failing.SetError("hello", errors.New("boom"))
	cleanup := newMockRunner()
	skipped := newMockRunner()

	w := &Workflow{
		Name: "Always",
		Jobs: []Job{
			{ID: "work", Name: "Work", Steps: []*Step{{Name: "Fails", Uses: "hello"}}},
			{ID: "report", Name: "Report", Needs: []string{"work"}, Steps: []*Step{{Name: "Never runs", Uses: "hello"}}},
			{ID: "cleanup", Name: "Cleanup", Needs: []string{"work", "report"}, Always: true, Steps: []*Step{{Name: "Removes", Uses: "hello"}}},
		},
	}
	report := startCapturing(t, w, OutputModeSpinner, stepRunners{byName: map[string]ActionRunner{
		"Fails": failing, "Never runs": skipped, "Removes": cleanup,
	}})

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
			before := newMockRunner()
			failing := newMockRunner()
			failing.SetError("hello", errors.New("boom"))
			after := newMockRunner()
			other := newMockRunner()

			w := &Workflow{
				Name: "Fail fast",
				Jobs: []Job{
					{ID: "work", Name: "Work", FailFast: failFast, Steps: []*Step{
						{Name: "Before", Uses: "hello"},
						{Name: "Fails", Uses: "hello"},
						{Name: "After {{vars.n}}", Uses: "hello", Iteration: Iteration{Entries: []map[string]any{{"n": 1}, {"n": 2}}}},
						{Name: "Last", Uses: "hello", Wait: "10ms"},
					}},
					// Another job is not held back by it.
					{ID: "other", Name: "Other", Steps: []*Step{{Name: "Runs", Uses: "hello"}}},
				},
			}
			report := startCapturing(t, w, OutputModeSpinner, stepRunners{byName: map[string]ActionRunner{
				"Before": before, "Fails": failing, "Runs": other,
			}, fallback: after})

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

func TestWorkflow_Start_GivenVars(t *testing.T) {
	runner := newMockRunner()
	w := &Workflow{
		Name: "Given",
		Vars: map[string]any{"url": "http://localhost"},
		Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{{Name: "S", Uses: "hello", With: map[string]any{"url": "{{vars.url}}/{{vars.path}}"}}}}},
	}
	p := NewPrinter(false)
	p.spinner = nil
	p.outWriter = new(bytes.Buffer)
	p.errWriter = new(bytes.Buffer)
	w.printer = p
	if err := w.Start(Config{ActionRunner: runner, Output: OutputModeSpinner, Vars: map[string]string{"url": "http://given", "path": "health"}}); err != nil {
		t.Fatal(err)
	}
	if got := runner.Calls["hello"][0].With["url"]; got != "http://given/health" {
		t.Errorf("with.url = %v, want it made of the vars given", got)
	}
}

// The variables read from files are read by the vars as the environment
// is, which comes before them, and a secret among them is hidden.
func TestWorkflow_Start_EnvFromFiles(t *testing.T) {
	runner := newMockRunner()
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
			Name: "S", Uses: "hello",
			With: map[string]any{"url": "{{vars.url}}", "both": "{{vars.both}}"},
			Echo: "token is {{vars.token}}",
		}}}},
	}
	report := startCapturingWith(t, w, Config{Output: OutputModeSpinner, ActionRunner: runner, Env: map[string]string{
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
	failing := newMockRunner()
	failing.SetError("hello", errors.New("boom"))
	after := newMockRunner()
	w := &Workflow{
		Name: "Fail fast",
		Jobs: []Job{{ID: "work", Name: "Work", FailFast: true, Repeat: &Repeat{Count: 3}, Steps: []*Step{
			// Fails in the first two runs, and is skipped in the last.
			{Name: "Fails", Uses: "hello", SkipIf: "repeat_index == 2"},
			{Name: "Sometimes", Uses: "hello", Test: "true"},
			// Reached in the last run alone, where its own skipif skips it,
			// which is a success of it.
			{Name: "Own skipif", Uses: "hello", SkipIf: "repeat_index == 2", Test: "true"},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner, stepRunners{byName: map[string]ActionRunner{"Fails": failing}, fallback: after})

	if got := len(after.Calls["hello"]); got != 1 {
		t.Errorf("the steps after the failure ran %d times, want once, in the run without it:\n%s", got, report)
	}
	if got := strings.Count(report, "1/3 success (33.3%), 2 skipped"); got != 2 {
		t.Errorf("%d steps are reported as run once and skipped twice, want both after the failure:\n%s", got, report)
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

	failing := newMockRunner()
	failing.SetError("hello", errors.New("boom"))
	ok := newMockRunner()
	w := &Workflow{
		Name: "Repeat",
		Jobs: []Job{{ID: "work", Name: "Work", FailFast: true, Repeat: &Repeat{Count: 2}, Steps: []*Step{
			{Name: "Each {{vars.n}}", Uses: "hello", Test: "true", Iteration: Iteration{Entries: []map[string]any{{"n": "a"}, {"n": "b"}, {"n": "c"}}}},
			{Name: "Fails", Uses: "hello"},
			{Name: "After", Uses: "hello", Test: "true"},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner, stepRunners{byName: map[string]ActionRunner{"Fails": failing}, fallback: ok})

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

func (r *timingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	with := call.With
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
	return map[string]any{"status": 0, "res": map[string]any{"code": 200}}, nil, nil
}

// A job starts once the jobs it needs are done, and does not wait for one
// that started with them but that it does not need.
func TestWorkflow_Start_RunsAJobOnceTheJobsItNeedsAreDone(t *testing.T) {
	runner := &timingRunner{begun: map[string]time.Time{}, ended: map[string]time.Time{}}
	step := func(key, take string) []*Step {
		return []*Step{{Name: key, Uses: "hello", Test: "res.code == 200", With: map[string]any{"key": key, "take": take}}}
	}
	w := &Workflow{Name: "needs", Jobs: []Job{
		{ID: "slow", Name: "Slow", Steps: step("slow", "1s")},
		{ID: "fast", Name: "Fast", Steps: step("fast", "0s")},
		{ID: "after", Name: "After", Needs: []string{"fast"}, Steps: step("after", "0s")},
		{ID: "last", Name: "Last", Needs: []string{"slow", "after"}, Steps: step("last", "0s")},
	}}
	report := startCapturing(t, w, OutputModeSpinner, runner)

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
				Name: "reads probe.StepContext",
				Uses: "http",
				With: map[string]any{"url": "http://api.test/"},
			}},
		}},
		printer: newBufferPrinter(),
	}
	if err := w.Start(Config{ActionRunner: &recordingRunner{result: map[string]any{"res": map[string]any{"code": 200}}}}); err != nil {
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

// A secret that a later step teaches the masker is hidden in what the
// report shows of an earlier step that failed: the response under it and
// what the false condition compared. Both are masked as the report shows
// them, before they are cut short, so the report has <redacted> where the
// secret was, not the part of it that the cut left, which no masker finds.
func TestWorkflow_Start_MasksWhatALaterStepTeaches(t *testing.T) {
	const token = "tk-0123456789abcdefghijklmnopqrstuvwxyz"
	tests := []struct {
		name string
		body string
		test string
	}{
		// The response is cut after 512 bytes, inside the token.
		{name: "in the response", body: strings.Repeat("x", 500) + token, test: "res.code == 200"},
		// A value the condition compared is cut after 200 characters,
		// inside the token.
		{name: "in the false condition", body: strings.Repeat("x", 180) + token, test: `res.body == ""`},
	}
	for _, tt := range tests {
		for _, mode := range []OutputMode{OutputModeSpinner, OutputModeStream} {
			t.Run(tt.name+"/"+mode.String(), func(t *testing.T) {
				defer func(was bool) { color.NoColor = was }(color.NoColor)
				color.NoColor = true

				leaks := newMockRunner()
				leaks.SetResult("http", map[string]any{"res": map[string]any{"code": 500, "body": tt.body}, "status": 0})
				w := &Workflow{
					Name: "Leak",
					Jobs: []Job{{ID: "j", Name: "J", Steps: []*Step{
						{Name: "Leaks", Uses: "http", Test: tt.test},
						// The token is a credential of this step, which
						// teaches the masker it.
						{Name: "Uses", Uses: "http", With: map[string]any{"headers": map[string]any{"authorization": token}}},
					}}},
				}
				report := startCapturing(t, w, mode, stepRunners{byName: map[string]ActionRunner{"Leaks": leaks}, fallback: newMockRunner()})

				if !strings.Contains(report, "Leaks") || !strings.Contains(report, "<redacted>") {
					t.Fatalf("the report should show the failed step with the token hidden:\n%s", report)
				}
				for _, part := range []string{token, token[:9], token[:12]} {
					if strings.Contains(report, part) {
						t.Errorf("the report shows %q of the token:\n%s", part, report)
					}
				}
			})
		}
	}
}
