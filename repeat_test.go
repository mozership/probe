package probe

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Test Interval YAML unmarshaling
func TestIntervalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected time.Duration
		hasError bool
	}{
		{
			name:     "integer seconds",
			input:    10,
			expected: 10 * time.Second,
			hasError: false,
		},
		{
			name:     "uint64 seconds",
			input:    uint64(5),
			expected: 5 * time.Second,
			hasError: false,
		},
		{
			name:     "float64 seconds",
			input:    float64(3),
			expected: 3 * time.Second,
			hasError: false,
		},
		{
			name:     "duration string",
			input:    "2m30s",
			expected: 2*time.Minute + 30*time.Second,
			hasError: false,
		},
		{
			name:     "milliseconds string",
			input:    "500ms",
			expected: 500 * time.Millisecond,
			hasError: false,
		},
		{
			name:     "plain number string",
			input:    "15",
			expected: 15 * time.Second,
			hasError: false,
		},
		{
			name:     "invalid string",
			input:    "invalid",
			expected: 0,
			hasError: true,
		},
		{
			name:     "invalid type",
			input:    []string{"invalid"},
			expected: 0,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var interval Interval

			unmarshalFunc := func(v any) error {
				// Simulate the unmarshal behavior
				if vPtr, ok := v.(*any); ok {
					*vPtr = tt.input
					return nil
				}
				return fmt.Errorf("invalid unmarshal target")
			}

			err := interval.UnmarshalYAML(unmarshalFunc)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if interval.Duration != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, interval.Duration)
			}
		})
	}
}

// Test Interval YAML marshaling
func TestIntervalMarshal(t *testing.T) {
	tests := []struct {
		name     string
		interval Interval
		expected any
	}{
		{
			name:     "whole seconds",
			interval: Interval{Duration: 10 * time.Second},
			expected: 10,
		},
		{
			name:     "milliseconds",
			interval: Interval{Duration: 500 * time.Millisecond},
			expected: "500ms",
		},
		{
			name:     "complex duration",
			interval: Interval{Duration: 2*time.Minute + 30*time.Second},
			expected: 150, // 2m30s = 150 seconds, will be marshaled as integer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.interval.MarshalYAML()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRepeatFunctionality_Intervals(t *testing.T) {
	tests := []struct {
		name            string
		count           int
		interval        time.Duration
		expectedMinTime time.Duration
		expectedMaxTime time.Duration
	}{
		{
			name:            "short interval repeat",
			count:           3,
			interval:        5 * time.Millisecond,
			expectedMinTime: 10 * time.Millisecond, // 2 intervals
			expectedMaxTime: 50 * time.Millisecond, // generous upper bound
		},
		{
			name:            "medium interval repeat",
			count:           4,
			interval:        20 * time.Millisecond,
			expectedMinTime: 60 * time.Millisecond,  // 3 intervals
			expectedMaxTime: 150 * time.Millisecond, // generous upper bound
		},
		{
			name:            "zero interval repeat",
			count:           5,
			interval:        0,
			expectedMinTime: 0,
			expectedMaxTime: 50 * time.Millisecond, // should be very fast
		},
		{
			name:            "single repeat",
			count:           1,
			interval:        10 * time.Millisecond,
			expectedMinTime: 0, // no intervals needed
			expectedMaxTime: 20 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &Workflow{
				Name: "repeat-interval-test",
				Jobs: []Job{
					{
						Name:  "repeat-job",
						Steps: []*Step{}, // Empty steps to avoid external dependencies
						Repeat: &Repeat{
							Count:    tt.count,
							Interval: Interval{Duration: tt.interval},
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
				t.Errorf("Repeat workflow should not error: %v", err)
			}

			if duration < tt.expectedMinTime {
				t.Errorf("Duration %v should be at least %v", duration, tt.expectedMinTime)
			}

			if duration > tt.expectedMaxTime {
				t.Errorf("Duration %v should not exceed %v", duration, tt.expectedMaxTime)
			}
		})
	}
}

func TestRepeatFunctionality_DifferentTimeUnits(t *testing.T) {
	tests := []struct {
		name     string
		interval string
		expected time.Duration
	}{
		{
			name:     "milliseconds",
			interval: "100ms",
			expected: 100 * time.Millisecond,
		},
		{
			name:     "seconds",
			interval: "1s",
			expected: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test parsing of different time units
			duration, err := time.ParseDuration(tt.interval)
			if err != nil {
				t.Errorf("Failed to parse duration %s: %v", tt.interval, err)
				return
			}

			if duration != tt.expected {
				t.Errorf("Parsed duration %v, expected %v", duration, tt.expected)
			}

			// Test in actual workflow
			workflow := &Workflow{
				Name: "time-unit-test",
				Jobs: []Job{
					{
						Name:  "time-unit-job",
						Steps: []*Step{},
						Repeat: &Repeat{
							Count:    2,
							Interval: Interval{Duration: duration},
						},
					},
				},
				printer: newBufferPrinter(),
			}

			config := Config{Verbose: false}
			start := time.Now()
			err = workflow.Start(config)
			elapsed := time.Since(start)

			if err != nil {
				t.Errorf("Time unit workflow should not error: %v", err)
			}

			// For very small intervals, just check it completes
			if duration > 10*time.Millisecond {
				expectedMin := duration // at least 1 interval
				if elapsed < expectedMin {
					t.Errorf("Elapsed time %v should be at least %v", elapsed, expectedMin)
				}
			}
		})
	}
}

func TestRepeatFunctionality_ParallelVsSequential(t *testing.T) {
	t.Run("repeat in parallel mode", func(t *testing.T) {
		workflow := &Workflow{
			Name: "parallel-repeat-test",
			Jobs: []Job{
				{
					Name:  "parallel-repeat-job1",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    3,
						Interval: Interval{Duration: 10 * time.Millisecond},
					},
				},
				{
					Name:  "parallel-repeat-job2",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 15 * time.Millisecond},
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
			t.Errorf("Parallel repeat workflow should not error: %v", err)
		}

		// In parallel mode with repeat, individual goroutines are created for each repeat iteration
		// This should complete relatively quickly as iterations run concurrently
		maxExpected := 100 * time.Millisecond // generous bound for parallel execution
		if duration > maxExpected {
			t.Errorf("Parallel repeat took too long: %v (max expected: %v)", duration, maxExpected)
		}
	})

	t.Run("repeat in sequential mode with dependencies", func(t *testing.T) {
		workflow := &Workflow{
			Name: "sequential-repeat-test",
			Jobs: []Job{
				{
					Name:  "first-repeat-job",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: 10 * time.Millisecond},
					},
				},
				{
					Name:  "second-repeat-job",
					Needs: []string{"first-repeat-job"},
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
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
			t.Errorf("Sequential repeat workflow should not error: %v", err)
		}

		// Sequential execution should take longer as jobs run one after another
		// Each job has 2 repeats with 10ms intervals, so minimum ~20ms per job
		minExpected := 20 * time.Millisecond // conservative minimum
		if duration < minExpected {
			t.Errorf("Sequential repeat completed too quickly: %v (min expected: %v)", duration, minExpected)
		}
	})
}

func TestRepeatFunctionality_EdgeCases(t *testing.T) {
	t.Run("zero count repeat", func(t *testing.T) {
		workflow := &Workflow{
			Name: "zero-count-test",
			Jobs: []Job{
				{
					Name:  "zero-count-job",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    0, // Edge case: zero count
						Interval: Interval{Duration: 10 * time.Millisecond},
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		// Should handle zero count gracefully (might not execute or execute once)
		if err != nil {
			t.Errorf("Zero count repeat should not error: %v", err)
		}
	})

	t.Run("high count repeat", func(t *testing.T) {
		workflow := &Workflow{
			Name: "high-count-test",
			Jobs: []Job{
				{
					Name:  "high-count-job",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    100,                   // High count
						Interval: Interval{Duration: 0}, // Zero interval to keep test fast
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
			t.Errorf("High count repeat should not error: %v", err)
		}

		// Even with high count, zero interval should keep it reasonably fast
		maxExpected := 1 * time.Second
		if duration > maxExpected {
			t.Errorf("High count repeat took too long: %v", duration)
		}
	})

	t.Run("negative interval handling", func(t *testing.T) {
		// Test that negative intervals are handled properly
		workflow := &Workflow{
			Name: "negative-interval-test",
			Jobs: []Job{
				{
					Name:  "negative-interval-job",
					Steps: []*Step{},
					Repeat: &Repeat{
						Count:    2,
						Interval: Interval{Duration: -10 * time.Millisecond}, // Negative interval
					},
				},
			},
			printer: newBufferPrinter(),
		}

		config := Config{Verbose: false}
		err := workflow.Start(config)

		// Should handle negative intervals gracefully (likely treat as zero)
		if err != nil {
			t.Errorf("Negative interval repeat should not error: %v", err)
		}
	})
}

func TestEnvironmentVariableOverride(t *testing.T) {
	t.Run("default max repeat count", func(t *testing.T) {
		// Ensure environment variable is not set
		_ = os.Unsetenv(EnvMaxRepeatCount)

		max := getMaxRepeatCount()
		if max != DefaultMaxRepeatCount {
			t.Errorf("Expected default max repeat count %d, got %d", DefaultMaxRepeatCount, max)
		}
	})

	t.Run("custom max repeat count via env", func(t *testing.T) {
		// Set custom max via environment variable
		customMax := 50000
		if err := os.Setenv(EnvMaxRepeatCount, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxRepeatCount) }()

		max := getMaxRepeatCount()
		if max != customMax {
			t.Errorf("Expected custom max repeat count %d, got %d", customMax, max)
		}
	})

	t.Run("default max attempts", func(t *testing.T) {
		// Ensure environment variable is not set
		_ = os.Unsetenv(EnvMaxAttempts)

		max := getMaxAttempts()
		if max != DefaultMaxAttempts {
			t.Errorf("Expected default max attempts %d, got %d", DefaultMaxAttempts, max)
		}
	})

	t.Run("custom max attempts via env", func(t *testing.T) {
		// Set custom max via environment variable
		customMax := 25000
		if err := os.Setenv(EnvMaxAttempts, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxAttempts) }()

		max := getMaxAttempts()
		if max != customMax {
			t.Errorf("Expected custom max attempts %d, got %d", customMax, max)
		}
	})

	t.Run("invalid env value falls back to default", func(t *testing.T) {
		// Set invalid value
		if err := os.Setenv(EnvMaxRepeatCount, "invalid"); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxRepeatCount) }()

		max := getMaxRepeatCount()
		if max != DefaultMaxRepeatCount {
			t.Errorf("Expected default max repeat count %d for invalid env value, got %d", DefaultMaxRepeatCount, max)
		}
	})

	t.Run("negative env value falls back to default", func(t *testing.T) {
		// Set negative value
		if err := os.Setenv(EnvMaxRepeatCount, "-100"); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxRepeatCount) }()

		max := getMaxRepeatCount()
		if max != DefaultMaxRepeatCount {
			t.Errorf("Expected default max repeat count %d for negative env value, got %d", DefaultMaxRepeatCount, max)
		}
	})
}

func TestRepeatValidation(t *testing.T) {
	t.Run("valid repeat count", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxRepeatCount)

		r := &Repeat{Count: 100}
		err := r.Validate()
		if err != nil {
			t.Errorf("Expected no error for valid repeat count, got: %v", err)
		}
	})

	t.Run("repeat count at default limit", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxRepeatCount)

		r := &Repeat{Count: DefaultMaxRepeatCount}
		err := r.Validate()
		if err == nil {
			t.Errorf("Expected error for repeat count at limit")
		}
	})

	t.Run("repeat count exceeds default limit", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxRepeatCount)

		r := &Repeat{Count: DefaultMaxRepeatCount + 1}
		err := r.Validate()
		if err == nil {
			t.Errorf("Expected error for repeat count exceeding limit")
		}
	})

	t.Run("repeat count valid with custom limit", func(t *testing.T) {
		customMax := 50000
		if err := os.Setenv(EnvMaxRepeatCount, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxRepeatCount) }()

		r := &Repeat{Count: 20000}
		err := r.Validate()
		if err != nil {
			t.Errorf("Expected no error for repeat count under custom limit, got: %v", err)
		}
	})

	t.Run("repeat count exceeds custom limit", func(t *testing.T) {
		customMax := 50000
		if err := os.Setenv(EnvMaxRepeatCount, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxRepeatCount) }()

		r := &Repeat{Count: customMax}
		err := r.Validate()
		if err == nil {
			t.Errorf("Expected error for repeat count at custom limit")
		}
	})
}

func TestRepeatValidationAsyncInterval(t *testing.T) {
	_ = os.Unsetenv(EnvMaxRepeatCount)

	tests := []struct {
		name    string
		repeat  Repeat
		wantErr bool
	}{
		{"async without interval", Repeat{Count: 5, Async: true}, true},
		{"async with interval", Repeat{Count: 5, Async: true, Interval: Interval{Duration: time.Millisecond}}, false},
		{"in turn without interval", Repeat{Count: 5}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.repeat.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, want error %v", err, tt.wantErr)
			}
		})
	}
}

func TestStepRetryValidation(t *testing.T) {
	t.Run("valid max attempts", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxAttempts)

		s := &StepRetry{MaxAttempts: 100}
		err := s.Validate()
		if err != nil {
			t.Errorf("Expected no error for valid max attempts, got: %v", err)
		}
	})

	t.Run("max attempts at default limit", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxAttempts)

		s := &StepRetry{MaxAttempts: DefaultMaxAttempts}
		err := s.Validate()
		if err != nil {
			t.Errorf("Expected no error for max attempts at limit, got: %v", err)
		}
	})

	t.Run("max attempts exceeds default limit", func(t *testing.T) {
		_ = os.Unsetenv(EnvMaxAttempts)

		s := &StepRetry{MaxAttempts: DefaultMaxAttempts + 1}
		err := s.Validate()
		if err == nil {
			t.Errorf("Expected error for max attempts exceeding limit")
		}
	})

	t.Run("max attempts valid with custom limit", func(t *testing.T) {
		customMax := 25000
		if err := os.Setenv(EnvMaxAttempts, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxAttempts) }()

		s := &StepRetry{MaxAttempts: 15000}
		err := s.Validate()
		if err != nil {
			t.Errorf("Expected no error for max attempts under custom limit, got: %v", err)
		}
	})

	t.Run("max attempts exceeds custom limit", func(t *testing.T) {
		customMax := 25000
		if err := os.Setenv(EnvMaxAttempts, fmt.Sprintf("%d", customMax)); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
		defer func() { _ = os.Unsetenv(EnvMaxAttempts) }()

		s := &StepRetry{MaxAttempts: customMax + 1}
		err := s.Validate()
		if err == nil {
			t.Errorf("Expected error for max attempts exceeding custom limit")
		}
	})
}
