package probe

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/mozership/probe/expr"
)

const (
	// DefaultMaxRepeatCount is the default upper limit for repeat count
	DefaultMaxRepeatCount = 10000
	// DefaultMaxAttempts is the default upper limit for max attempts
	DefaultMaxAttempts = 10000
	// EnvMaxRepeatCount is the environment variable name for overriding max repeat count
	EnvMaxRepeatCount = "PROBE_MAX_REPEAT_COUNT"
	// EnvMaxAttempts is the environment variable name for overriding max attempts
	EnvMaxAttempts = "PROBE_MAX_ATTEMPTS"
)

// getMaxRepeatCount returns the max repeat count from environment variable or default
func getMaxRepeatCount() int {
	if v := os.Getenv(EnvMaxRepeatCount); v != "" {
		if max, err := strconv.Atoi(v); err == nil && max > 0 {
			return max
		}
	}
	return DefaultMaxRepeatCount
}

// getMaxAttempts returns the max attempts from environment variable or default
func getMaxAttempts() int {
	if v := os.Getenv(EnvMaxAttempts); v != "" {
		if max, err := strconv.Atoi(v); err == nil && max > 0 {
			return max
		}
	}
	return DefaultMaxAttempts
}

// Interval represents a time interval that can be specified as a number (seconds) or duration string
type Interval struct {
	time.Duration
}

// UnmarshalYAML implements custom YAML unmarshaling for Interval
func (i *Interval) UnmarshalYAML(unmarshal func(any) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}

	switch v := raw.(type) {
	case int:
		// If it's an integer, treat as seconds
		i.Duration = time.Duration(v) * time.Second
	case float64:
		// YAML might parse numbers as float64
		i.Duration = time.Duration(int(v)) * time.Second
	case uint64:
		// YAML might parse numbers as uint64
		i.Duration = time.Duration(v) * time.Second
	case string:
		// If it's a string, try to parse as duration
		// First check if it's a plain number (backward compatibility)
		if matched, _ := regexp.MatchString(`^\d+$`, v); matched {
			// Plain number, treat as seconds
			if seconds, err := strconv.Atoi(v); err == nil {
				i.Duration = time.Duration(seconds) * time.Second
			} else {
				return fmt.Errorf("invalid interval: %s", v)
			}
		} else {
			// Parse as duration string
			if dur, err := time.ParseDuration(v); err == nil {
				i.Duration = dur
			} else {
				return fmt.Errorf("invalid interval format: %s", v)
			}
		}
	default:
		return fmt.Errorf("interval must be a number or duration string, got %T: %v", v, v)
	}

	return nil
}

// MarshalYAML implements custom YAML marshaling for Interval
func (i Interval) MarshalYAML() (any, error) {
	// If it's a whole number of seconds, return as integer
	if i.Duration%time.Second == 0 {
		return int(i.Duration / time.Second), nil
	}
	// Otherwise, return as duration string
	return i.String(), nil
}

// Repeat defines the repeat configuration for jobs
type Repeat struct {
	Count    int      `yaml:"count" validate:"required,gte=0"`
	Interval Interval `yaml:"interval"`
	Async    bool     `yaml:"async,omitempty"`
	// Warmup is the number of runs before the count, which are run as any
	// other but counted in neither the results nor the times, and do not
	// fail the job.
	Warmup int `yaml:"warmup,omitempty" validate:"gte=0"`
	// Test is checked once every run is over, against the runs of the
	// steps, and decides alone whether the job passed: a run in which a
	// step failed is then counted, and fails nothing by itself.
	Test string `yaml:"test,omitempty"`
	// BaselineTest is checked once every run is over too, against the runs
	// of the steps and those of the run compared to, under baseline. It is
	// checked only when that run has the steps it reads, and then the job
	// passes only when it holds. It leaves how a failed run is taken to Test.
	BaselineTest string `yaml:"baseline_test,omitempty"`
}

// repeatTest is a test of a repeated job, by the key it is written under.
type repeatTest struct {
	key, test string
}

// tests returns the tests of the job, by their keys under repeat, given or
// not.
func (r *Repeat) tests() []repeatTest {
	return []repeatTest{
		{"test", r.Test},
		{"baseline_test", r.BaselineTest},
	}
}

// Runs returns the number of runs of the job, the warmup runs included.
func (r *Repeat) Runs() int {
	return r.Warmup + r.Count
}

// Validate performs custom validation for Repeat
func (r *Repeat) Validate() error {
	maxCount := getMaxRepeatCount()
	if r.Count >= maxCount {
		return fmt.Errorf("repeat count %d exceeds maximum allowed value of %d. To allow higher values, set %s environment variable (e.g., %s=%d)",
			r.Count, maxCount, EnvMaxRepeatCount, EnvMaxRepeatCount, r.Count+1)
	}
	if r.Warmup < 0 {
		return fmt.Errorf("repeat warmup %d must be zero or greater", r.Warmup)
	}
	// The warmup runs are runs too, each with a process for its action. The
	// count is below the limit here, so the limit is compared without
	// adding the two, which a large warmup would overflow.
	if r.Warmup > 0 && r.Warmup >= maxCount-r.Count {
		return fmt.Errorf("repeat count %d with warmup %d reaches maximum allowed value of %d runs. To allow more runs, set %s environment variable",
			r.Count, r.Warmup, maxCount, EnvMaxRepeatCount)
	}
	// The interval is what spaces the runs of an async repeat. Without one,
	// every run would start at once, each with a process for its action.
	if r.Async && r.Interval.Duration <= 0 {
		return fmt.Errorf("repeat with async needs an interval, the time from the start of one run to the start of the next")
	}
	if baselineReadByKey(r.BaselineTest) {
		return fmt.Errorf("repeat.baseline_test: %s", errBaselineByKey)
	}
	return nil
}

// errBaselineByKey tells why a test against the run compared to cannot read
// a step of it, or the field of a step, by a key known only when it runs.
const errBaselineByKey = "baseline.steps is read by a key known only when the test runs; write the step and the field, as baseline.steps.search.rt, so that the test is skipped when the baseline has none of them"

// baselineReadByKey reports whether test reads a step under baseline.steps,
// or a field of one, by a key known only when it runs. Whether the run
// compared to has what the test reads is known before the test runs only
// when the step and its field are written in it, and the test is skipped
// when it does not.
func baselineReadByKey(test string) bool {
	for _, chain := range expr.ReadsByVariableKey(test, "baseline") {
		if len(chain) < 3 {
			return true
		}
	}
	return false
}

// StepRetry defines the retry configuration for steps until success (status 0)
type StepRetry struct {
	MaxAttempts  int      `yaml:"max_attempts" validate:"required,gte=1"`
	Interval     Interval `yaml:"interval"`
	InitialDelay Interval `yaml:"initial_delay,omitempty"`
}

// Validate performs custom validation for StepRetry
func (s *StepRetry) Validate() error {
	maxAttempts := getMaxAttempts()
	if s.MaxAttempts > maxAttempts {
		return fmt.Errorf("max_attempts %d exceeds maximum allowed value of %d. To allow higher values, set %s environment variable (e.g., %s=%d)",
			s.MaxAttempts, maxAttempts, EnvMaxAttempts, EnvMaxAttempts, s.MaxAttempts)
	}
	return nil
}
