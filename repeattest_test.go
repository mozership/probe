package probe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

func TestRepeatRuns(t *testing.T) {
	steps := repeatRuns([]StepRepeatCounter{
		{ID: "search", SuccessCount: 3, FailureCount: 1, RTs: []float64{0.010, 0.020, 0.030, 0.300}},
		// The entries of an iteration have a counter each, of one step.
		{ID: "each", SuccessCount: 2, RTs: []float64{0.001, 0.002}},
		{ID: "each", FailureCount: 1, SkippedCount: 1, RTs: []float64{0.003}},
		{ID: "failed", FailureCount: 2},
		{SuccessCount: 1},
	})

	want := map[string]any{
		"search": map[string]any{
			"runs": 4, "success": 3, "failure": 1, "skipped": 0, "success_rate": 0.75,
			"rt": map[string]any{"min": 0.01, "mean": 0.09, "p50": 0.02, "p90": 0.3, "p95": 0.3, "p99": 0.3, "max": 0.3},
		},
		"each": map[string]any{
			"runs": 4, "success": 2, "failure": 1, "skipped": 1, "success_rate": 0.5,
			"rt": map[string]any{"min": 0.001, "mean": 0.002, "p50": 0.002, "p90": 0.003, "p95": 0.003, "p99": 0.003, "max": 0.003},
		},
		"failed": map[string]any{
			"runs": 2, "success": 0, "failure": 2, "skipped": 0, "success_rate": 0.0,
			"rt": map[string]any(nil),
		},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Errorf("repeatRuns() = %#v, want %#v", steps, want)
	}
}

// repeatTestWorkflow is a job of count runs of one step, with test as the
// test over its runs.
func repeatTestWorkflow(count int, test string) *Workflow {
	return &Workflow{
		Name: "Repeat test",
		Vars: map[string]any{"rate": 0.75},
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: count, Test: test},
			Steps:  []*Step{{Name: "Query", ID: "query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}
}

// startWithReport starts the workflow with a JSON report, its actions run by
// runner, and returns it.
func startWithReport(t *testing.T, workflow *Workflow, runner ActionRunner) report.Report {
	t.Helper()
	return startComparedTo(t, workflow, runner, nil)
}

// startComparedTo is startWithReport with the report compared to baseline,
// the report of an earlier run.
func startComparedTo(t *testing.T, workflow *Workflow, runner ActionRunner, baseline *report.Report) report.Report {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	if err := workflow.Start(Config{Reports: []report.Target{{Format: report.JSON, Path: path}}, Baseline: baseline, ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return r
}

func TestEvalRepeatTest(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	env := repeatTestEnv{Steps: map[string]any{"search": map[string]any{"success": 3}}}
	tests := []struct {
		name        string
		test        string
		wantPassed  bool
		wantKind    string
		wantMessage string
		wantOutput  string
	}{
		{name: "a test that holds", test: "steps.search.success == 3", wantPassed: true},
		{name: "a test that cannot be evaluated", test: "nosuch(1)", wantKind: FailureTestError},
		{name: "a test that is not a boolean", test: "steps.search.success", wantKind: FailureTestType, wantMessage: "test evaluated to 3 (int), not a boolean", wantOutput: typeMismatchDetail("steps.search.success", 3)},
		{name: "a test that is false", test: "steps.search.success == 4", wantKind: FailureAssertion, wantMessage: "test evaluated to false: steps.search.success == 4 (steps.search.success = 3)", wantOutput: "       failed: steps.search.success == 4\n         steps.search.success = 3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.SetMasker(mask.New(nil, nil))
			ctx := JobContext{runEnv: &runEnv{Printer: printer}}

			res := evalRepeatTest(ctx, tt.test, env)
			if res.Test != tt.test || res.Passed != tt.wantPassed {
				t.Fatalf("result = %+v, want the test, passed: %v", res, tt.wantPassed)
			}
			if tt.wantPassed {
				if res.Failure != nil {
					t.Errorf("result = %+v, want no failure", res)
				}
				return
			}
			if res.Failure == nil || res.Failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want %s", res.Failure, tt.wantKind)
			}
			if tt.wantKind == FailureTestError {
				_, err := (&expr.Expr{}).Eval(tt.test, env)
				tt.wantMessage, tt.wantOutput = err.Error(), testErrorDetail(err.Error())
			}
			if res.Failure.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", res.Failure.Message, tt.wantMessage)
			}
			if out := failureTextOf(res.Failure, res.Test); out != tt.wantOutput {
				t.Errorf("output = %q, want %q", out, tt.wantOutput)
			}
		})
	}
}

// TestWorkflow_RepeatTestPassesOverFailedRuns passes a job one of whose runs
// failed its test, as the test over the runs allows: the run is counted, and
// neither the job nor its step fails for it.
func TestWorkflow_RepeatTestPassesOverFailedRuns(t *testing.T) {
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 500}, "rt": "20ms"},
		{"res": map[string]any{"code": 200}, "rt": "30ms"},
		{"res": map[string]any{"code": 200}, "rt": "40ms"},
	}}
	workflow := repeatTestWorkflow(4, "steps.query.success_rate >= vars.rate && steps.query.rt.max < 0.05")

	r := startWithReport(t, workflow, runner)
	if workflow.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitOK)
	}
	job := r.Jobs[0]
	if job.Status != report.Passed {
		t.Errorf("job status = %q, want %q", job.Status, report.Passed)
	}
	want := &report.RepeatTest{Test: "steps.query.success_rate >= vars.rate && steps.query.rt.max < 0.05", Status: report.Passed}
	if !reflect.DeepEqual(job.RepeatTest, want) {
		t.Errorf("repeat_test = %+v, want %+v", job.RepeatTest, want)
	}
	st := job.Steps[0]
	if st.Status != report.Passed || st.Failure != nil || st.Repeat.Failure != 1 {
		t.Errorf("step = %+v, want it passed with the failed run counted", st)
	}

	out := workflow.printer.outWriter.(*bytes.Buffer).String()
	if !strings.Contains(out, "✓  repeat test: steps.query.success_rate >= vars.rate") {
		t.Errorf("terminal should show the test passed, got:\n%s", out)
	}
}

// TestWorkflow_RepeatTestFails fails a job whose test over its runs is
// false, with the exit status of a failed test, though a run's action failed.
func TestWorkflow_RepeatTestFails(t *testing.T) {
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		nil,
		{"res": map[string]any{"code": 200}, "rt": "400ms"},
	}}
	workflow := repeatTestWorkflow(3, "steps.query.rt.p95 < 0.3")

	r := startWithReport(t, workflow, runner)
	if workflow.exitStatus != ExitTestFailed {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitTestFailed)
	}
	job := r.Jobs[0]
	if r.Status != report.Failed || job.Status != report.Failed {
		t.Errorf("status = %q, job status = %q, want both failed", r.Status, job.Status)
	}
	rt := job.RepeatTest
	if rt == nil || rt.Status != report.Failed || rt.Failure == nil {
		t.Fatalf("repeat_test = %+v, want it failed", rt)
	}
	if rt.Failure.Kind != report.FailureAssertion || rt.Failure.Message != "test evaluated to false: steps.query.rt.p95 < 0.3 (steps.query.rt.p95 = 0.4)" {
		t.Errorf("failure = %+v, want the false condition and what it compared", rt.Failure)
	}

	out := workflow.printer.outWriter.(*bytes.Buffer).String()
	// The test lines up with the steps above it: its icon with theirs, and
	// why it failed with its text.
	for _, s := range []string{
		"\n        ✗  repeat test: steps.query.rt.p95 < 0.3\n",
		"\n           failed: steps.query.rt.p95 < 0.3\n",
		"\n             steps.query.rt.p95 = 0.4\n",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("terminal should contain %q, got:\n%s", s, out)
		}
	}
}

// TestWorkflow_RepeatTestError fails a job whose test over its runs cannot
// be evaluated, as a step whose test cannot be is.
func TestWorkflow_RepeatTestError(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{nil, nil}}
	workflow := repeatTestWorkflow(2, "steps.query.rt.p95 < 0.3")

	r := startWithReport(t, workflow, runner)
	if workflow.exitStatus != ExitTestFailed {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitTestFailed)
	}
	if rt := r.Jobs[0].RepeatTest; rt == nil || rt.Failure == nil || rt.Failure.Kind != report.FailureTestError {
		t.Errorf("repeat_test = %+v, want a test error: no run returned a time", rt)
	}
}

// TestWorkflow_RepeatTestOfOneRun checks the test of a job of one run, which
// is not otherwise counted as a repeated job is.
func TestWorkflow_RepeatTestOfOneRun(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 500}, "rt": "10ms"},
	}}
	workflow := repeatTestWorkflow(1, "steps.query.runs == 1 && steps.query.failure == 1")

	r := startWithReport(t, workflow, runner)
	if workflow.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitOK)
	}
	if rt := r.Jobs[0].RepeatTest; rt == nil || rt.Status != report.Passed {
		t.Errorf("repeat_test = %+v, want it passed", rt)
	}
}

// TestWorkflow_RepeatTestCannotStart fails a job that cannot start, though
// its test over its runs holds: the runs are only counted when they ran.
func TestWorkflow_RepeatTestCannotStart(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{nil, nil}}
	workflow := repeatTestWorkflow(2, "true")
	workflow.Jobs[0].Name = "{{ nofunc() }}"

	if err := workflow.Start(Config{ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if workflow.exitStatus != ExitConfigError {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitConfigError)
	}
}

// TestWorkflow_RepeatTestReadsStepWithoutID reads the runs of a step that
// has no id, as a workflow made in code has, by where it is written.
func TestWorkflow_RepeatTestReadsStepWithoutID(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
	}}
	workflow := repeatTestWorkflow(2, "steps.step_0.runs == 2")
	workflow.Jobs[0].Steps[0].ID = ""

	r := startWithReport(t, workflow, runner)
	if rt := r.Jobs[0].RepeatTest; rt == nil || rt.Status != report.Passed {
		t.Errorf("repeat_test = %+v, want it passed", rt)
	}
}

// TestWorkflow_RepeatTestAfterSkippedRuns checks the test of a job whose
// skipif skipped the runs after the first, which leave the job skipped only
// when no run was counted.
func TestWorkflow_RepeatTestAfterSkippedRuns(t *testing.T) {
	tests := []struct {
		test   string
		status string
		exit   int
	}{
		{"false", report.Failed, ExitTestFailed},
		{"steps.query.runs == 1", report.Passed, ExitOK},
	}
	for _, tt := range tests {
		t.Run(tt.test, func(t *testing.T) {
			runner := &timedRunner{answers: []map[string]any{
				{"res": map[string]any{"code": 200}, "rt": "10ms"},
			}}
			workflow := repeatTestWorkflow(3, tt.test)
			workflow.Jobs[0].SkipIf = "outputs.query?.done == true"
			workflow.Jobs[0].Steps[0].Outputs = map[string]string{"done": "true"}

			r := startWithReport(t, workflow, runner)
			if workflow.exitStatus != tt.exit {
				t.Errorf("exit status = %d, want %d", workflow.exitStatus, tt.exit)
			}
			if job := r.Jobs[0]; job.Status != tt.status || job.RepeatTest == nil || job.RepeatTest.Status != tt.status {
				t.Errorf("job = %+v, want it and its test %s", job, tt.status)
			}
		})
	}
}

// TestWorkflow_RepeatTestOfSkippedJob leaves the test of a job whose every
// run was skipped unchecked, in turn and async.
func TestWorkflow_RepeatTestOfSkippedJob(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(fmt.Sprintf("async %v", async), func(t *testing.T) {
			workflow := repeatTestWorkflow(2, "false")
			workflow.Jobs[0].SkipIf = "true"
			workflow.Jobs[0].Repeat.Async = async
			workflow.Jobs[0].Repeat.Interval = Interval{Duration: time.Millisecond}

			r := startWithReport(t, workflow, &timedRunner{})
			if workflow.exitStatus != ExitOK {
				t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitOK)
			}
			if job := r.Jobs[0]; job.Status != report.Skipped || job.RepeatTest != nil {
				t.Errorf("job = %+v, want it skipped with no test checked", job)
			}
		})
	}
}

// TestWorkflowStartRefusesRepeatTestIDCollision refuses a job made in code
// in which a step without an id would be read by the id another step has.
func TestWorkflowStartRefusesRepeatTestIDCollision(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{nil, nil, nil, nil}}
	workflow := repeatTestWorkflow(2, "steps.step_0.success_rate == 1")
	workflow.Jobs[0].Steps = []*Step{
		{Name: "Unnamed", Uses: "http"},
		{Name: "Named", ID: "step_0", Uses: "http"},
	}

	err := workflow.Start(Config{ActionRunner: runner})
	if err == nil || !strings.Contains(err.Error(), "steps 0 and 1 are both read as steps.step_0") {
		t.Errorf("Start() = %v, want the steps read by one id refused", err)
	}
	if runner.calls != 0 {
		t.Errorf("action called %d times, want none", runner.calls)
	}
}

// TestBuildReport_RepeatTestedStepFailedAndSkipped reports a step of a job
// with a test over its runs that failed in one run and was not run in
// another as passed: it ran, and its runs are counted.
func TestBuildReport_RepeatTestedStepFailedAndSkipped(t *testing.T) {
	st := buildStepReport(StepResult{
		Name:          "Query",
		HasTest:       true,
		Status:        StatusError,
		RepeatCounter: &StepRepeatCounter{Name: "Query", FailureCount: 1, SkippedCount: 1, Tested: true},
	}, nil)
	if st.Status != report.Passed {
		t.Errorf("status = %q, want %q", st.Status, report.Passed)
	}
}

// TestRepeatTimesAtTheLimit reads times just under and just over a limit as
// they were taken: rounded to the microsecond, as the report has them, the
// first would fail a test that it is under 0.3 and the second pass one that
// it is at most 0.3.
func TestRepeatTimesAtTheLimit(t *testing.T) {
	under := repeatTimes([]float64{0.2999999})
	if p95 := under["p95"].(float64); !(p95 < 0.3) {
		t.Errorf("p95 = %v, want under 0.3", p95)
	}
	over := repeatTimes([]float64{0.3000004})
	if p95 := over["p95"].(float64); p95 <= 0.3 {
		t.Errorf("p95 = %v, want over 0.3", p95)
	}
	if repeatTimes(nil) != nil {
		t.Error("no times should be nil")
	}
}

// TestWorkflow_RepeatTestChecksStepWithoutTest reports a step that has no
// test of its own, but that the test over the runs reads, as passed rather
// than untested.
func TestWorkflow_RepeatTestChecksStepWithoutTest(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
	}}
	workflow := repeatTestWorkflow(2, "steps.query.rt.p95 < 0.3")
	workflow.Jobs[0].Steps[0].Test = ""

	r := startWithReport(t, workflow, runner)
	if st := r.Jobs[0].Steps[0]; st.Status != report.Passed {
		t.Errorf("step status = %q, want %q", st.Status, report.Passed)
	}
	if r.Summary.Steps.Passed != 1 || r.Summary.Steps.Untested != 0 {
		t.Errorf("summary = %+v, want the step counted as passed", r.Summary.Steps)
	}
}

func TestBaselineRuns(t *testing.T) {
	base := &report.Report{Jobs: []report.Job{
		{ID: "other", Steps: []report.Step{{ID: "search", Repeat: &report.Repeat{Total: 1, Success: 1}}}},
		{ID: "api", Steps: []report.Step{
			{ID: "search", Repeat: &report.Repeat{Total: 10, Success: 9, Latency: &report.Latency{
				Count: 9, MinMs: 1, MeanMs: 2, P50Ms: 3, P90Ms: 4, P95Ms: 5, P99Ms: 6, MaxMs: 7,
			}}},
			// The entries of an iteration are steps of one id.
			{ID: "each", Repeat: &report.Repeat{Total: 2, Success: 2, Latency: &report.Latency{Count: 2, P50Ms: 1}}},
			{ID: "each", Repeat: &report.Repeat{Total: 2, Success: 1, Latency: &report.Latency{Count: 1, P50Ms: 2}}},
			{ID: "failed", Repeat: &report.Repeat{Total: 3}},
			{ID: "once"},
		}},
	}}

	want := map[string]any{
		"search": map[string]any{
			"runs": 10, "success": 9, "success_rate": 0.9,
			"rt": map[string]any{"min": 0.001, "mean": 0.002, "p50": 0.003, "p90": 0.004, "p95": 0.005, "p99": 0.006, "max": 0.007},
		},
		"each":   map[string]any{"runs": 4, "success": 3, "success_rate": 0.75, "rt": map[string]any(nil)},
		"failed": map[string]any{"runs": 3, "success": 0, "success_rate": 0.0, "rt": map[string]any(nil)},
	}
	if got := baselineRuns(base, "api"); !reflect.DeepEqual(got, want) {
		t.Errorf("baselineRuns() = %#v, want %#v", got, want)
	}
	if got := baselineRuns(base, "none"); len(got) != 0 {
		t.Errorf("baselineRuns() of a job the baseline has none of = %#v, want none", got)
	}
}

// The fields probe check knows a step of a repeated job to have are those
// the tests read when the job runs, under steps and under baseline.steps.
func TestRepeatFieldsAreThoseTheTestsRead(t *testing.T) {
	keys := func(v any) []string {
		m, _ := v.(map[string]any)
		return slices.Sorted(maps.Keys(m))
	}
	sorted := func(fields []string) []string {
		return slices.Sorted(slices.Values(fields))
	}

	run := repeatRuns([]StepRepeatCounter{{ID: "s", SuccessCount: 1, RTs: []float64{0.1}}})["s"]
	if got, want := keys(run), sorted(repeatRunFields); !slices.Equal(got, want) {
		t.Errorf("fields of a step under steps = %v, want repeatRunFields %v", got, want)
	}
	if got, want := keys(run.(map[string]any)[runFieldRT]), sorted(repeatRTFields); !slices.Equal(got, want) {
		t.Errorf("fields of rt under steps = %v, want repeatRTFields %v", got, want)
	}

	base := &report.Report{Jobs: []report.Job{{ID: "j", Steps: []report.Step{
		{ID: "s", Repeat: &report.Repeat{Total: 1, Success: 1, Latency: &report.Latency{Count: 1}}},
	}}}}
	baseline := baselineRuns(base, "j")["s"]
	if got, want := keys(baseline), sorted(baselineRunFields); !slices.Equal(got, want) {
		t.Errorf("fields of a step under baseline.steps = %v, want baselineRunFields %v", got, want)
	}
	if got, want := keys(baseline.(map[string]any)[runFieldRT]), sorted(repeatRTFields); !slices.Equal(got, want) {
		t.Errorf("fields of rt under baseline.steps = %v, want repeatRTFields %v", got, want)
	}
}

func TestBaselineMissing(t *testing.T) {
	steps := map[string]any{
		"search": map[string]any{"runs": 10, "rt": map[string]any{"p95": 0.1}},
		"each":   map[string]any{"runs": 4, "rt": map[string]any(nil)},
	}
	tests := []struct {
		test, want string
	}{
		{"steps.search.rt.p95 < baseline.steps.search.rt.p95 * 1.2", ""},
		{"steps.each.runs >= baseline.steps.each.runs", ""},
		{"steps.added.rt.p95 < baseline.steps.added.rt.p95", "the baseline has no step added"},
		{"steps.each.rt.p95 < baseline.steps.each.rt.p95", "the baseline has no times of step each"},
		{"steps.search.rt.p95 < 0.3", ""},
	}
	for _, tt := range tests {
		if got := baselineMissing(tt.test, steps); got != tt.want {
			t.Errorf("baselineMissing(%q) = %q, want %q", tt.test, got, tt.want)
		}
	}
}

// TestWorkflow_BaselineTest checks the test of a repeated job against the
// run compared to: the job passes when it holds, fails when it does not, and
// passes with the test skipped when there is nothing to compare to. A step
// that only the test reads is then shown untested, as nothing checked it.
func TestWorkflow_BaselineTest(t *testing.T) {
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	const test = "steps.query.rt.p95 < baseline.steps.query.rt.p95 * 1.2"
	baseline := func(id string, p95Ms float64) *report.Report {
		return &report.Report{RunID: "run-1", StartedAt: time.Now(), Jobs: []report.Job{{ID: "search", Steps: []report.Step{{
			ID: id, Repeat: &report.Repeat{Total: 2, Success: 2, Latency: &report.Latency{Count: 2, P50Ms: p95Ms, P95Ms: p95Ms, P99Ms: p95Ms}},
		}}}}}
	}
	tests := []struct {
		name     string
		baseline *report.Report
		exit     int
		want     *report.RepeatTest
		terminal []string
	}{
		{
			name:     "faster than the baseline",
			baseline: baseline("query", 100),
			exit:     ExitOK,
			want:     &report.RepeatTest{Test: test, Status: report.Passed},
			terminal: []string{"\n        ✓  baseline test: " + test + "\n"},
		},
		{
			name:     "slower than the baseline",
			baseline: baseline("query", 10),
			exit:     ExitTestFailed,
			want: &report.RepeatTest{Test: test, Status: report.Failed, Failure: &report.Failure{
				Kind:    report.FailureAssertion,
				Message: "test evaluated to false: " + test + " (steps.query.rt.p95 = 0.02, baseline.steps.query.rt.p95 * 1.2 = 0.012)",
			}},
			terminal: []string{
				"\n        ✗  baseline test: " + test + "\n",
				"\n             baseline.steps.query.rt.p95 * 1.2 = 0.012\n",
			},
		},
		{
			name:     "no baseline",
			exit:     ExitOK,
			want:     &report.RepeatTest{Test: test, Status: report.Skipped, Reason: "no baseline was given"},
			terminal: []string{"\n        ⏭  baseline test: " + test + "\n           skipped: no baseline was given\n"},
		},
		{
			name:     "a baseline without the step",
			baseline: baseline("other", 100),
			exit:     ExitOK,
			want:     &report.RepeatTest{Test: test, Status: report.Skipped, Reason: "the baseline has no step query"},
			terminal: []string{"skipped: the baseline has no step query\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &timedRunner{answers: []map[string]any{
				{"res": map[string]any{"code": 200}, "rt": "10ms"},
				{"res": map[string]any{"code": 200}, "rt": "20ms"},
			}}
			workflow := repeatTestWorkflow(2, "")
			workflow.Jobs[0].Repeat.BaselineTest = test
			workflow.Jobs[0].Steps[0].Test = ""

			r := startComparedTo(t, workflow, runner, tt.baseline)
			if workflow.exitStatus != tt.exit {
				t.Errorf("exit status = %d, want %d", workflow.exitStatus, tt.exit)
			}
			job := r.Jobs[0]
			wantStatus := report.Passed
			if tt.exit != ExitOK {
				wantStatus = report.Failed
			}
			if job.Status != wantStatus {
				t.Errorf("job status = %q, want %q", job.Status, wantStatus)
			}
			if job.RepeatTest != nil {
				t.Errorf("repeat_test = %+v, want none", job.RepeatTest)
			}
			if !reflect.DeepEqual(job.BaselineTest, tt.want) {
				t.Errorf("baseline_test = %+v, want %+v", job.BaselineTest, tt.want)
			}
			// The step has no test of its own, and is shown tested only when
			// the test that reads it is checked.
			wantStep := report.Passed
			if tt.want.Status == report.Skipped {
				wantStep = report.Untested
			}
			if st := job.Steps[0]; st.Status != wantStep {
				t.Errorf("step status = %q, want %q", st.Status, wantStep)
			}

			out := workflow.printer.outWriter.(*bytes.Buffer).String()
			for _, s := range tt.terminal {
				if !strings.Contains(out, s) {
					t.Errorf("terminal should contain %q, got:\n%s", s, out)
				}
			}
		})
	}
}

// TestWorkflow_BaselineTestLeavesFailedRunsToTest fails a job a run of which
// failed, though its test against the run compared to holds: that test does
// not decide how a failed run is taken, as the test over the runs does.
func TestWorkflow_BaselineTestLeavesFailedRunsToTest(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 500}, "rt": "10ms"},
	}}
	workflow := repeatTestWorkflow(2, "")
	workflow.Jobs[0].Repeat.BaselineTest = "steps.query.runs == baseline.steps.query.runs"
	base := &report.Report{RunID: "run-1", StartedAt: time.Now(), Jobs: []report.Job{{ID: "search", Steps: []report.Step{{
		ID: "query", Repeat: &report.Repeat{Total: 2, Success: 2},
	}}}}}

	r := startComparedTo(t, workflow, runner, base)
	if workflow.exitStatus != ExitTestFailed {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitTestFailed)
	}
	job := r.Jobs[0]
	if job.Status != report.Failed || job.BaselineTest == nil || job.BaselineTest.Status != report.Passed {
		t.Errorf("job = %+v, want it failed with the baseline test passed", job)
	}
}
