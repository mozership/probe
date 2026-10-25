package probe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// A job is given its ID before it is scheduled, so the scheduler does not
// make one up.
func TestAddToSchedule_WithoutID(t *testing.T) {
	w := &Workflow{Jobs: []Job{{Name: "a"}}}
	const want = `job "a" has no ID`
	if _, err := w.newScheduler(); err == nil || err.Error() != want {
		t.Errorf("newScheduler() error = %v, want %q", err, want)
	}
}

// A cycle is told from its first job in the workflow, not in the order of
// the IDs, by a run and by probe dag alike.
func TestCycleIsToldInWorkflowOrder(t *testing.T) {
	const want = "circular dependency detected: zeta -> alpha"

	t.Run("run", func(t *testing.T) {
		w := &Workflow{Jobs: []Job{
			{ID: "zeta", Name: "Zeta", Needs: []string{"alpha"}},
			{ID: "alpha", Name: "Alpha", Needs: []string{"zeta"}},
		}}
		if _, err := w.initJobScheduler(); err == nil || err.Error() != want {
			t.Errorf("initJobScheduler() error = %v, want %q", err, want)
		}
	})

	t.Run("dag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cycle.yml")
		content := "name: Cycle\njobs:\n- id: zeta\n  name: Zeta\n  needs: [alpha]\n  steps:\n  - uses: hello\n- id: alpha\n  name: Alpha\n  needs: [zeta]\n  steps:\n  - uses: hello\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		p := &Probe{FilePath: path}
		if _, err := p.DagMermaid(); err == nil || err.Error() != want {
			t.Errorf("DagMermaid() error = %v, want %q", err, want)
		}
	})
}

// orderRunner records the job each call of an action is from, by the who it
// is given, and fails the calls from the job of failing.
type orderRunner struct {
	failing string

	mu    sync.Mutex
	calls []string
}

func (r *orderRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	who, _ := with["who"].(string)
	r.mu.Lock()
	r.calls = append(r.calls, who)
	r.mu.Unlock()
	if who == r.failing {
		return nil, errors.New("boom")
	}
	return map[string]any{"status": 0}, nil
}

// A job that needs a repeated one waits for every run of it, as the
// scheduler is told a job is over once all of its runs are, in turn and with
// the runs overlapping alike. When the runs failed, a job that always runs
// still runs after them, and a job that does not is skipped. Another job
// ends between the runs, for the jobs that can start to be looked at again
// while the repeated one is not over.
func TestWorkflow_Start_NeedsRepeatedJob(t *testing.T) {
	for _, async := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("async %v, failing %v", async, fail), func(t *testing.T) {
				runner := &orderRunner{}
				if fail {
					runner.failing = "work"
				}
				step := func(who string) []*Step {
					return []*Step{{Name: who, Uses: "hello", With: map[string]any{"who": who}, actionRunner: runner}}
				}
				w := &Workflow{
					Name: "Repeated",
					Jobs: []Job{
						{
							ID: "work", Name: "Work", Steps: step("work"),
							Repeat: &Repeat{Count: 3, Interval: Interval{Duration: 20 * time.Millisecond}, Async: async},
						},
						{ID: "side", Name: "Side", Steps: []*Step{{Name: "side", Uses: "hello", Wait: "10ms", With: map[string]any{"who": "side"}, actionRunner: runner}}},
						{ID: "after", Name: "After", Needs: []string{"work"}, Steps: step("after")},
						{ID: "cleanup", Name: "Cleanup", Needs: []string{"work"}, Always: true, Steps: step("cleanup")},
					},
				}
				report := startCapturing(t, w, OutputModeSpinner)

				runner.mu.Lock()
				calls := slices.DeleteFunc(slices.Clone(runner.calls), func(who string) bool { return who == "side" })
				runner.mu.Unlock()
				first := calls[:min(3, len(calls))]
				if !slices.Equal(first, []string{"work", "work", "work"}) {
					t.Errorf("calls = %v, want the three runs of work first:\n%s", calls, report)
				}
				rest := slices.Sorted(slices.Values(calls[len(first):]))
				wantRest := []string{"cleanup"}
				if !fail {
					wantRest = []string{"after", "cleanup"}
				}
				if !slices.Equal(rest, wantRest) {
					t.Errorf("calls after work = %v, want %v:\n%s", rest, wantRest, report)
				}
				if failed := w.exitStatus != 0; failed != fail {
					t.Errorf("run failed = %v, want %v:\n%s", failed, fail, report)
				}
			})
		}
	}
}
