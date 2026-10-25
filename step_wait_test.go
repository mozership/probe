package probe

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/internal/expr"
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
		{name: "zero", wait: "0", expected: 0},
		{name: "zero duration", wait: "0s", expected: 0},
		{name: "fractional seconds", wait: "1.5", expected: 1500 * time.Millisecond},
		{name: "spaces around", wait: " 2 ", expected: 2 * time.Second},
		{name: "a nanosecond", wait: "1ns", expected: time.Nanosecond},
		{name: "negative seconds", wait: "-1", hasError: true},
		{name: "negative duration", wait: "-1s", hasError: true},
		{name: "below a nanosecond", wait: "0.0000000001", hasError: true},
		{name: "the most seconds a duration holds", wait: "9223372036", expected: 9223372036 * time.Second},
		{name: "too many seconds for a duration", wait: "9223372037", hasError: true},
		{name: "too long a duration", wait: "2562048h", hasError: true},
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
		{name: "zero", wait: "0", expected: "0s"},
		{name: "minutes in seconds", wait: "1m30s", expected: "90s"},
		{name: "fractional seconds", wait: "1.5", expected: "1.5s"},
		{name: "negative returns empty", wait: "-1s", expected: ""},
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
		{name: "not a time", wait: "soon", want: "wait duration parsing error: invalid wait soon: use a duration"},
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
