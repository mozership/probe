package probe

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestResult_exitCode(t *testing.T) {
	tests := []struct {
		name   string
		failed bool
		kinds  []string
		want   int
	}{
		{"success", false, nil, ExitOK},
		{"success ignores stray kinds", false, []string{FailureAction}, ExitOK},
		{"failed with nothing recorded", true, nil, ExitTestFailed},
		{"assertion", true, []string{FailureAssertion}, ExitTestFailed},
		{"test error", true, []string{FailureTestError}, ExitTestFailed},
		{"test type", true, []string{FailureTestType}, ExitTestFailed},
		{"contract response", true, []string{FailureContractResponse}, ExitTestFailed},
		{"contract request", true, []string{FailureContractRequest}, ExitTestFailed},
		{"action", true, []string{FailureAction}, ExitActionError},
		{"action wins over assertion", true, []string{FailureAssertion, FailureAction}, ExitActionError},
		{"config", true, []string{failureConfig}, ExitConfigError},
		{"config wins over everything", true, []string{FailureAction, failureConfig, FailureAssertion}, ExitConfigError},
		{"refused", true, []string{FailureRefused}, ExitConfigError},
		{"refused wins over an action error", true, []string{FailureAction, FailureRefused}, ExitConfigError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := NewResult()
			for _, k := range tt.kinds {
				rs.recordFailure(k)
			}
			if got := rs.exitCode(tt.failed); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.failed, got, tt.want)
			}
		})
	}
}

func TestResult_recordFailure(t *testing.T) {
	// A nil Result belongs to contexts built without one; recording must not panic.
	var nilResult *Result
	nilResult.recordFailure(FailureAction)

	rs := NewResult()
	rs.recordFailure("")
	if got := rs.exitCode(true); got != ExitTestFailed {
		t.Errorf("an empty kind should be ignored, exitCode = %d", got)
	}

	// Jobs record concurrently; run under -race.
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				rs.recordFailure(FailureAssertion)
			} else {
				rs.recordFailure(FailureAction)
			}
		}(i)
	}
	wg.Wait()
	if got := rs.exitCode(true); got != ExitActionError {
		t.Errorf("exitCode = %d, want %d", got, ExitActionError)
	}
}

// TestWorkflow_ExitCode runs workflows end to end and checks the exit status
// each kind of failure leaves behind.
func TestWorkflow_ExitCode(t *testing.T) {
	runner := NewMockActionRunner()
	runner.SetResult("ok", map[string]any{
		"req": map[string]any{},
		"res": map[string]any{"code": 200},
	})
	runner.SetError("down", errors.New("connection refused"))

	step := func(name, uses, test string) *Step {
		return &Step{Name: name, Uses: uses, Test: test, actionRunner: runner}
	}

	tests := []struct {
		name string
		jobs []Job
		want int
	}{
		{
			name: "every test holds",
			jobs: []Job{{Name: "a", Steps: []*Step{step("s", "ok", "res.code == 200")}}},
			want: ExitOK,
		},
		{
			name: "a test is false",
			jobs: []Job{{Name: "a", Steps: []*Step{step("s", "ok", "res.code == 500")}}},
			want: ExitTestFailed,
		},
		{
			name: "a test cannot be evaluated",
			jobs: []Job{{Name: "a", Steps: []*Step{step("s", "ok", `parse_int("x") == 1`)}}},
			want: ExitTestFailed,
		},
		{
			name: "an action errors",
			jobs: []Job{{Name: "a", Steps: []*Step{step("s", "down", "res.code == 200")}}},
			want: ExitActionError,
		},
		{
			name: "an action error in one job and a false test in another",
			jobs: []Job{
				{Name: "a", Steps: []*Step{step("s", "ok", "res.code == 500")}},
				{Name: "b", Steps: []*Step{step("s", "down", "res.code == 200")}},
			},
			want: ExitActionError,
		},
		{
			name: "a job that cannot start",
			jobs: []Job{{Name: "{{ nofunc() }}", Steps: []*Step{{Name: "s", Uses: "ok", actionRunner: runner}}}},
			want: ExitConfigError,
		},
		{
			name: "an action errors inside a repeated job",
			jobs: []Job{{
				Name:   "a",
				Repeat: &Repeat{Count: 2, Interval: Interval{Duration: time.Millisecond}},
				Steps:  []*Step{step("s", "down", "res.code == 200")},
			}},
			want: ExitActionError,
		},
		{
			name: "a test is false inside a repeated job",
			jobs: []Job{{
				Name:   "a",
				Repeat: &Repeat{Count: 2, Interval: Interval{Duration: time.Millisecond}},
				Steps:  []*Step{step("s", "ok", "res.code == 500")},
			}},
			want: ExitTestFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Workflow{Name: "exit", Jobs: tt.jobs, printer: newBufferPrinter()}
			if err := w.Start(Config{}); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if w.exitStatus != tt.want {
				t.Errorf("exit status = %d, want %d", w.exitStatus, tt.want)
			}
		})
	}
}
