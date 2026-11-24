package probe

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/internal/outcome"
)

func TestRunExitCode(t *testing.T) {
	tests := []struct {
		name   string
		failed bool
		kinds  []string
		want   int
	}{
		{"success", false, nil, ExitOK},
		{"success ignores stray kinds", false, []string{outcome.FailureAction}, ExitOK},
		{"failed with nothing recorded", true, nil, ExitTestFailed},
		{"assertion", true, []string{outcome.FailureAssertion}, ExitTestFailed},
		{"test error", true, []string{outcome.FailureTestError}, ExitTestFailed},
		{"test type", true, []string{outcome.FailureTestType}, ExitTestFailed},
		{"contract response", true, []string{outcome.FailureContractResponse}, ExitTestFailed},
		{"contract request", true, []string{outcome.FailureContractRequest}, ExitTestFailed},
		{"action", true, []string{outcome.FailureAction}, ExitActionError},
		{"action wins over assertion", true, []string{outcome.FailureAssertion, outcome.FailureAction}, ExitActionError},
		{"config", true, []string{failureConfig}, ExitConfigError},
		{"config wins over everything", true, []string{outcome.FailureAction, failureConfig, outcome.FailureAssertion}, ExitConfigError},
		{"refused", true, []string{outcome.FailureRefused}, ExitConfigError},
		{"refused wins over an action error", true, []string{outcome.FailureAction, outcome.FailureRefused}, ExitConfigError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := outcome.New()
			for _, k := range tt.kinds {
				rs.RecordFailure(k)
			}
			if got := runExitCode(rs, tt.failed); got != tt.want {
				t.Errorf("runExitCode(%v) = %d, want %d", tt.failed, got, tt.want)
			}
		})
	}
}

func TestRun_RecordFailure(t *testing.T) {
	// A nil Run belongs to contexts built without one; recording must not panic.
	var nilResult *outcome.Run
	nilResult.RecordFailure(outcome.FailureAction)

	rs := outcome.New()
	rs.RecordFailure("")
	if got := runExitCode(rs, true); got != ExitTestFailed {
		t.Errorf("an empty kind should be ignored, runExitCode = %d", got)
	}

	// Jobs record concurrently; run under -race.
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				rs.RecordFailure(outcome.FailureAssertion)
			} else {
				rs.RecordFailure(outcome.FailureAction)
			}
		}(i)
	}
	wg.Wait()
	if got := runExitCode(rs, true); got != ExitActionError {
		t.Errorf("runExitCode = %d, want %d", got, ExitActionError)
	}
}

// TestWorkflow_ExitCode runs workflows end to end and checks the exit status
// each kind of failure leaves behind.
func TestWorkflow_ExitCode(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{
		"req": map[string]any{},
		"res": map[string]any{"code": 200},
	})
	runner.SetError("down", errors.New("connection refused"))

	step := func(name, uses, test string) *Step {
		return &Step{Name: name, Uses: uses, Test: test}
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
			jobs: []Job{{Name: "{{ nofunc() }}", Steps: []*Step{{Name: "s", Uses: "ok"}}}},
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
			if err := w.Start(Config{ActionRunner: runner}); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if w.exitStatus != tt.want {
				t.Errorf("exit status = %d, want %d", w.exitStatus, tt.want)
			}
		})
	}
}
