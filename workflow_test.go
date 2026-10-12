package probe

import (
	"bytes"
	"encoding/base64"
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

	"github.com/linyows/probe/report"

	"github.com/linyows/probe/expr"
	"github.com/linyows/probe/mask"
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
					Name:  "job1",
					Steps: []*Step{},
				},
				{
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
					Name:  "job1",
					Steps: []*Step{},
				},
				{
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
					Name:  "job1",
					Needs: []string{"job2"},
					Steps: []*Step{},
				},
				{
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
					Name:  "job1",
					Steps: []*Step{},
				},
				{
					Name:  "job2",
					Steps: []*Step{},
				},
				{
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
					Name:  "first",
					Steps: []*Step{},
				},
				{
					Name:  "second",
					Needs: []string{"first"},
					Steps: []*Step{},
				},
				{
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
					Name:  "job1",
					Steps: []*Step{},
				},
				{
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
					Name:  "setup",
					Steps: []*Step{},
				},
				{
					Name:  "worker1",
					Needs: []string{"setup"},
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 5 * time.Millisecond},
					},
				},
				{
					Name:  "worker2",
					Needs: []string{"setup"},
					Steps: []*Step{},
				},
				{
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
					Name:  "might-fail",
					Steps: []*Step{},
				},
				{
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
			Status:    "Completed",
			StartTime: time.Now().Add(-100 * time.Millisecond),
			EndTime:   time.Now(),
			Success:   true,
		}
		result.Jobs["test-job"] = jobResult

		// This should not panic and should execute successfully
		workflow.printer.PrintReport(result)

		// If we get here without panic, the test passes
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
					Name:  "producer1",
					Steps: []*Step{},
				},
				{
					Name:  "producer2",
					Steps: []*Step{},
				},
				{
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
				{Name: "root", Steps: []*Step{}},
				{Name: "level1-a", Needs: []string{"root"}, Steps: []*Step{}},
				{Name: "level1-b", Needs: []string{"root"}, Steps: []*Step{}},
				{Name: "level1-c", Needs: []string{"root"}, Steps: []*Step{}},
				{Name: "level2-a", Needs: []string{"level1-a", "level1-b"}, Steps: []*Step{}},
				{Name: "level2-b", Needs: []string{"level1-b", "level1-c"}, Steps: []*Step{}},
				{Name: "final", Needs: []string{"level2-a", "level2-b"}, Steps: []*Step{}},
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
					Name:  "parallel1",
					Steps: []*Step{},
				},
				{
					Name:  "repeat1",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 5 * time.Millisecond},
					},
				},
				{
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

func Test_evalVars(t *testing.T) {
	tests := []struct {
		name     string
		wf       *Workflow
		expected map[string]any
		err      error
	}{
		{
			name: "use expr",
			wf: &Workflow{
				Name: "Test",
				Vars: map[string]any{
					"host":  "{{HOST ?? 'http://localhost:3000'}}",
					"token": "{{TOKEN}}",
				},
				env: map[string]string{
					"TOKEN": "secrets",
				},
			},
			expected: map[string]any{
				"host":  "http://localhost:3000",
				"token": "secrets",
			},
			err: nil,
		},
		{
			name: "not exists environment",
			wf: &Workflow{
				Name: "Test",
				Vars: map[string]any{
					"host":  "{{HOST}}",
					"token": "{{TOKEN}}",
				},
				env: map[string]string{
					"TOKEN": "secrets",
				},
			},
			expected: map[string]any{
				"host":  "<nil>",
				"token": "secrets",
			},
			err: fmt.Errorf("environment(HOST) is nil"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := tt.wf.evalVars()
			if err != nil && err.Error() != tt.err.Error() {
				t.Errorf("expected error %+v, got %+v", tt.err, err)
			}
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Errorf("expected %#v, got %#v", tt.expected, actual)
			}
		})
	}
}

func Test_evalVarsReadsOtherVars(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]any
		env      map[string]string
		expected map[string]any
	}{
		{
			name: "a var defined after the one that reads it",
			vars: map[string]any{
				"base": "http://localhost:{{vars.port}}",
				"port": "{{PORT ?? '18080'}}",
			},
			env: map[string]string{"PORT": "9000"},
			expected: map[string]any{
				"base": "http://localhost:9000",
				"port": "9000",
			},
		},
		{
			name: "a chain of vars",
			vars: map[string]any{
				"a": "{{vars.b}}-a",
				"b": "{{vars.c}}-b",
				"c": "c",
			},
			expected: map[string]any{
				"a": "c-b-a",
				"b": "c-b",
				"c": "c",
			},
		},
		{
			name: "a map var reading a string var",
			vars: map[string]any{
				"domain": "example.test",
				"alice": map[string]any{
					"user": "alice@{{vars.domain}}",
					"tags": []any{"{{vars.domain}}"},
				},
			},
			expected: map[string]any{
				"domain": "example.test",
				"alice": map[string]any{
					"user": "alice@example.test",
					"tags": []any{"example.test"},
				},
			},
		},
		{
			name: "a string var reading a field of a map var",
			vars: map[string]any{
				"auth": map[string]any{"user": "admin", "password": "{{PASSWORD}}"},
				"pair": "{{vars.auth.user + ':' + vars.auth.password}}",
			},
			env: map[string]string{"PASSWORD": "secret"},
			expected: map[string]any{
				"auth": map[string]any{"user": "admin", "password": "secret"},
				"pair": "admin:secret",
			},
		},
		{
			name: "a var that is not defined falls back",
			vars: map[string]any{
				"port": "{{vars.missing ?? '8080'}}",
			},
			expected: map[string]any{
				"port": "8080",
			},
		},
		{
			name: "a var read by a key known only at run time",
			vars: map[string]any{
				"picked": "{{vars[WHICH]}}",
				"x":      "{{vars.y}}!",
				"y":      "why",
			},
			env: map[string]string{"WHICH": "x"},
			expected: map[string]any{
				"picked": "why!",
				"x":      "why!",
				"y":      "why",
			},
		},
		{
			name: "an array at the top level",
			vars: map[string]any{
				"port":  "8080",
				"list":  []any{"{{vars.port}}", map[string]any{"p": "{{vars.port}}"}},
				"label": "{{vars.list[0]}}",
			},
			expected: map[string]any{
				"port":  "8080",
				"list":  []any{"8080", map[string]any{"p": "8080"}},
				"label": "8080",
			},
		},
		{
			name: "a value that is not a string",
			vars: map[string]any{
				"retries": 3,
				"label":   "{{vars.retries * 2}}",
			},
			expected: map[string]any{
				"retries": 3,
				"label":   "6",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.env
			if env == nil {
				// A non-empty env keeps Env from reading the process's own.
				env = map[string]string{"UNUSED": ""}
			}
			wf := &Workflow{Name: "Test", Vars: tt.vars, env: env}
			actual, err := wf.evalVars()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(tt.expected, actual) {
				t.Errorf("expected %#v, got %#v", tt.expected, actual)
			}
		})
	}
}

func Test_evalVarsReadsOneRandomValue(t *testing.T) {
	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"password": "{{random_str(24)}}",
			"auth":     "alice:{{vars.password}}",
			"header":   "Basic {{encode_base64(vars.auth)}}",
		},
		env: map[string]string{"UNUSED": ""},
	}
	for range 20 {
		actual, err := wf.evalVars()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		password := actual["password"].(string)
		if actual["auth"] != "alice:"+password {
			t.Fatalf("auth = %q, want it built from password %q", actual["auth"], password)
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+password))
		if actual["header"] != want {
			t.Fatalf("header = %q, want %q", actual["header"], want)
		}
	}
}

func Test_evalVarsReadsVarsAsAWhole(t *testing.T) {
	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"name":     "probe",
			"snapshot": map[string]any{"all": "{{vars}}"},
		},
		env: map[string]string{"UNUSED": ""},
	}
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]any{
		"name":     "probe",
		"snapshot": map[string]any{"all": map[string]any{"name": "probe"}},
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected %#v, got %#v", expected, actual)
	}
	// A var holding the map being filled in would make it cyclic, which JSON
	// cannot encode.
	if _, err := json.Marshal(actual); err != nil {
		t.Errorf("vars cannot be encoded as JSON: %v", err)
	}
}

// Test_evalVarsWithTemplatesFromFiles checks that a var that expands a
// template comes after the vars that do not, since what the template reads
// is known only once it runs, and that two such vars do not make a cycle.
func Test_evalVarsWithTemplatesFromFiles(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "body.tmpl")
	if err := os.WriteFile(body, []byte("{{ vars.zone }}-{{ vars.yes }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.tmpl")
	if err := os.WriteFile(other, []byte("other {{ vars.zone }}"), 0o600); err != nil {
		t.Fatal(err)
	}

	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"a":    "{{ template(file('" + body + "')) }}",
			"b":    "{{ template(file('" + other + "')) }} and {{ vars.a }}",
			"yes":  true,
			"zone": "{{ ZONE }}",
		},
		env: map[string]string{"ZONE": "tokyo"},
	}
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]any{
		"a":    "tokyo-true",
		"b":    "other tokyo and tokyo-true",
		"yes":  true,
		"zone": "tokyo",
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("expected %#v, got %#v", expected, actual)
	}
}

// Test_evalVarsTemplateReadsALaterTemplateVar checks that a var whose
// template reads another var that expands a template, and would come first
// by name, is evaluated after it, and that a cycle between them is reported.
func Test_evalVarsTemplateReadsALaterTemplateVar(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first := write("a.tmpl", "a sees {{ vars.z }}")
	last := write("z.tmpl", "z is {{ vars.n }}-{{ random_str(8) }}")

	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"a": "{{ template(file('" + first + "')) }}",
			"n": 1,
			"z": "{{ template(file('" + last + "')) }}",
		},
		env: map[string]string{"UNUSED": ""},
	}
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	z, _ := actual["z"].(string)
	if !strings.HasPrefix(z, "z is 1-") || actual["a"] != "a sees "+z {
		t.Errorf("a should see the value z holds, got a = %q, z = %q", actual["a"], z)
	}

	cyclic := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"a": "{{ template(file('" + write("ca.tmpl", "{{ vars.b }}") + "')) }}",
			"b": "{{ template(file('" + write("cb.tmpl", "{{ vars.a }}") + "')) }}",
		},
		env: map[string]string{"UNUSED": ""},
	}
	if _, err := cyclic.evalVars(); err == nil || !strings.Contains(err.Error(), "vars: circular reference: a -> b -> a") {
		t.Errorf("error = %v, want the cycle", err)
	}
}

// Test_evalVarsTemplateReadsVarsByAKeyKnownWhenItRuns checks that a template
// reading vars by a key known only when it runs waits for every other var.
func Test_evalVarsTemplateReadsVarsByAKeyKnownWhenItRuns(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"a": "{{ template(file('" + write("a.tmpl", "a sees {{ vars[WHICH] }}") + "')) }}",
			"z": "{{ template(file('" + write("z.tmpl", "z") + "')) }}",
		},
		env: map[string]string{"WHICH": "z"},
	}
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actual["a"] != "a sees z" {
		t.Errorf("a = %q, want it to see z", actual["a"])
	}
}

// Test_evalVarsKeyReadsAVar checks that a var whose key reads another var is
// evaluated after it, as one whose value reads it is.
func Test_evalVarsKeyReadsAVar(t *testing.T) {
	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"body":   map[string]any{"{{ vars.tenant }}": map[string]any{"active": true}},
			"tenant": "{{ TENANT }}",
		},
		env: map[string]string{"TENANT": "acme"},
	}
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"acme": map[string]any{"active": true}}
	if !reflect.DeepEqual(actual["body"], want) {
		t.Errorf("body = %#v, want %#v", actual["body"], want)
	}
}

// Test_evalVarsListNamedLikeATemplate checks that a list var is evaluated
// whatever its name holds, now that the keys of a map are templates too.
func Test_evalVarsListNamedLikeATemplate(t *testing.T) {
	wf := &Workflow{
		Name: "Test",
		Vars: map[string]any{
			"odd{{name}}": []any{"{{ 1 + 1 }}"},
			"bad":         []any{"ok", map[string]any{"x": "{{ nosuch() }}"}},
		},
		env: map[string]string{"UNUSED": ""},
	}
	_, err := wf.evalVars()
	if err == nil || !strings.Contains(err.Error(), "vars.bad[1].x: ") {
		t.Errorf("error = %v, want one naming vars.bad[1].x", err)
	}

	delete(wf.Vars, "bad")
	actual, err := wf.evalVars()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(actual["odd{{name}}"], []any{2}) {
		t.Errorf("odd{{name}} = %#v, want the list evaluated", actual["odd{{name}}"])
	}
}

func Test_evalVarsCycleIsReproducible(t *testing.T) {
	// a reads b and c through the values of a map, and both read a back, so
	// the cycle reported depends on which of them is visited first.
	vars := map[string]any{
		"a": map[string]any{"x": "{{vars.c}}", "y": "{{vars.b}}", "z": "{{vars.c}}"},
		"b": "{{vars.a.y}}",
		"c": "{{vars.a.x}}",
	}
	for i := range 50 {
		wf := &Workflow{Name: "Test", Vars: vars, env: map[string]string{"UNUSED": ""}}
		_, err := wf.evalVars()
		if err == nil || err.Error() != "vars: circular reference: a -> c -> a" {
			t.Fatalf("run %d: error = %v, want the cycle through c", i, err)
		}
	}
}

func Test_evalVarsCircularReference(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]any
		want string
	}{
		{
			name: "itself",
			vars: map[string]any{"a": "{{vars.a}}"},
			want: "vars: circular reference: a -> a",
		},
		{
			name: "two vars",
			vars: map[string]any{"a": "{{vars.b}}", "b": "{{vars.a}}"},
			want: "vars: circular reference: a -> b -> a",
		},
		{
			name: "through a map var",
			vars: map[string]any{
				"a": map[string]any{"x": "{{vars.c}}"},
				"b": "{{vars.a.x}}",
				"c": "{{vars.b}}",
			},
			want: "vars: circular reference: a -> c -> b -> a",
		},
		{
			name: "itself by name, along with a read of vars as a whole",
			vars: map[string]any{
				"a": "{{vars.a ?? 'fallback'}} {{toJSON(vars)}}",
				"b": "b",
			},
			want: "vars: circular reference: a -> a",
		},
		{
			name: "through an array var",
			vars: map[string]any{"a": []any{"{{vars.b}}"}, "b": "{{vars.a[0]}}"},
			want: "vars: circular reference: a -> b -> a",
		},
		{
			name: "two vars read by keys known only at run time",
			vars: map[string]any{"a": "{{vars[K]}}", "b": "{{toJSON(vars)}}"},
			want: "vars: circular reference: a -> b -> a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{Name: "Test", Vars: tt.vars, env: map[string]string{"UNUSED": ""}}
			_, err := wf.evalVars()
			if err == nil || err.Error() != tt.want {
				t.Errorf("error = %v, want %q", err, tt.want)
			}
		})
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
		IsRepeating:   true,
		RepeatCurrent: 5,
		RepeatTotal:   10,
		StepCounters:  make(map[int]StepRepeatCounter),
		Printer:       newBufferPrinter(),
		countersMu:    &sync.Mutex{},
	}

	// Test initial state
	if !ctx.IsRepeating {
		t.Error("expected IsRepeating to be true")
	}

	if ctx.RepeatCurrent != 5 {
		t.Errorf("expected RepeatCurrent to be 5, got %d", ctx.RepeatCurrent)
	}

	if ctx.RepeatTotal != 10 {
		t.Errorf("expected RepeatTotal to be 10, got %d", ctx.RepeatTotal)
	}

	// Test step counter initialization
	counter := StepRepeatCounter{
		SuccessCount: 3,
		FailureCount: 2,
		Name:         "Test Step",
		LastResult:   true,
	}

	ctx.StepCounters[0] = counter

	if len(ctx.StepCounters) != 1 {
		t.Errorf("expected 1 step counter, got %d", len(ctx.StepCounters))
	}

	if ctx.StepCounters[0].SuccessCount != 3 {
		t.Errorf("expected SuccessCount to be 3, got %d", ctx.StepCounters[0].SuccessCount)
	}
}

func TestStepRepeatCounterUpdate(t *testing.T) {
	// Test counter update logic
	jCtx := &JobContext{
		IsRepeating:   true,
		RepeatCurrent: 3,
		RepeatTotal:   10,
		StepCounters:  make(map[int]StepRepeatCounter),
		Printer:       newBufferPrinter(),
		countersMu:    &sync.Mutex{},
	}

	step := &Step{
		Name: "Test Step",
		Test: "true", // Always success
		Idx:  0,
		Expr: &expr.Expr{},
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
	counter := jCtx.StepCounters[0]
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

	if w.printer == nil {
		p := NewPrinter(false, nil)
		p.spinner = nil
		p.errWriter = new(bytes.Buffer)
		w.printer = p
	}

	out := new(bytes.Buffer)
	w.printer.outWriter = out

	if err := w.Start(Config{Output: mode}); err != nil {
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
						{Name: "After {{vars.n}}", Uses: "hello", actionRunner: after, Iteration: []map[string]any{{"n": 1}, {"n": 2}}},
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
			Printer:       printer,
			Result:        result,
			Outputs:       NewOutputs(),
			RepeatCurrent: i,
			countersMu:    &sync.Mutex{},
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
