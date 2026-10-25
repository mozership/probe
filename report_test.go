package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/report"

	"github.com/mozership/probe/internal/expr"
)

// newReportResult builds a Result covering every status a report has to map.
func newReportResult() (*Result, []string) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	rs := NewResult()

	rs.Jobs["api"] = &JobResult{
		JobID:     "api",
		JobName:   "API",
		Status:    StatusError,
		StartTime: start,
		EndTime:   start.Add(1500 * time.Millisecond),
		StepResults: []StepResult{
			{Index: 0, Name: "Health", Status: StatusSuccess, Test: "res.code == 200", Elapsed: 120 * time.Millisecond, Echo: "line 1\nline 2\n", HasEcho: true},
			{Index: 1, Name: "Create", Status: StatusError, Test: "res.code == 201", Elapsed: 80 * time.Millisecond,
				RetryAttempt: 3, RetryMax: 3,
				Failure: &StepFailure{
					Kind:     FailureAssertion,
					Message:  "test evaluated to false",
					Request:  map[string]any{"url": "http://example.test/items"},
					Response: map[string]any{"code": 500, "body": "boom ``` ]]>"},
				}},
			{Index: 2, Name: "Connect", Status: StatusError, Failure: &StepFailure{Kind: FailureAction, Message: "connection refused"}},
			{Index: 3, Name: "Cleanup", Status: StatusSkipped},
			{Index: 4, Name: "Note", Status: StatusWarning},
		},
	}
	rs.Jobs["after"] = &JobResult{
		JobID:     "after",
		JobName:   "After | API",
		Status:    StatusSkipped,
		StartTime: start,
		EndTime:   start,
	}
	rs.Jobs["poll"] = &JobResult{
		JobID:     "poll",
		JobName:   "Poll",
		Status:    StatusError,
		StartTime: start,
		EndTime:   start.Add(3 * time.Second),
		StepResults: []StepResult{
			{Index: 0, Name: "Tick", Status: StatusWarning, HasTest: true, Test: "res.code == 0",
				RepeatCounter: &StepRepeatCounter{Name: "Tick", SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}},
		},
	}
	rs.Jobs["setup"] = &JobResult{
		JobID:     "setup",
		JobName:   "Setup",
		Status:    StatusError,
		StartTime: start,
		EndTime:   start.Add(10 * time.Millisecond),
	}

	return rs, []string{"api", "after", "poll", "setup"}
}

func TestBuildReport(t *testing.T) {
	rs, order := newReportResult()
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	r := BuildReport("Demo", "A demo run", rs, order, start, start.Add(3*time.Second))

	if r.Status != report.Failed {
		t.Errorf("Status = %q, want %q", r.Status, report.Failed)
	}
	if r.DurationMs != 3000 {
		t.Errorf("DurationMs = %d, want 3000", r.DurationMs)
	}

	wantJobs := report.Count{Total: 4, Failed: 3, Skipped: 1}
	if r.Summary.Jobs != wantJobs {
		t.Errorf("Summary.Jobs = %+v, want %+v", r.Summary.Jobs, wantJobs)
	}
	wantSteps := report.Count{Total: 6, Passed: 1, Failed: 3, Skipped: 1, Untested: 1}
	if r.Summary.Steps != wantSteps {
		t.Errorf("Summary.Steps = %+v, want %+v", r.Summary.Steps, wantSteps)
	}

	var ids []string
	for _, j := range r.Jobs {
		ids = append(ids, j.ID)
	}
	if want := []string{"api", "after", "poll", "setup"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("job order = %v, want %v", ids, want)
	}

	steps := r.Jobs[0].Steps
	if steps[0].Echo != "line 1\nline 2" {
		t.Errorf("echo = %q, want it without the white space around it", steps[0].Echo)
	}
	if steps[0].DurationMs != 120 {
		t.Errorf("DurationMs = %d, want 120", steps[0].DurationMs)
	}
	if steps[1].Retry == nil || *steps[1].Retry != (report.Retry{Attempts: 3, Max: 3}) {
		t.Errorf("Retry = %+v, want 3/3", steps[1].Retry)
	}
	if steps[1].Failure == nil || steps[1].Failure.Response["code"] != 500 {
		t.Errorf("Failure = %+v, want the response carried over", steps[1].Failure)
	}
	if steps[3].Name != "Cleanup" || steps[3].Status != report.Skipped {
		t.Errorf("skipped step = %q/%q, want the name as it is, the status skipped", steps[3].Name, steps[3].Status)
	}
	if steps[4].Status != report.Untested {
		t.Errorf("step without a test = %q, want %q", steps[4].Status, report.Untested)
	}

	repeated := r.Jobs[2].Steps[0]
	if repeated.Status != report.Failed {
		t.Errorf("repeat with a failing iteration = %q, want %q", repeated.Status, report.Failed)
	}
	if repeated.Repeat == nil || *repeated.Repeat != (report.Repeat{Total: 3, Success: 2, Failure: 1}) {
		t.Errorf("Repeat = %+v, want 2 of 3", repeated.Repeat)
	}
}

func TestBuildReport_StatusMapping(t *testing.T) {
	tests := []struct {
		name string
		sr   StepResult
		want string
	}{
		{"success", StepResult{Status: StatusSuccess}, report.Passed},
		{"error", StepResult{Status: StatusError}, report.Failed},
		{"skipped", StepResult{Status: StatusSkipped}, report.Skipped},
		{"no test", StepResult{Status: StatusWarning}, report.Untested},
		{"repeat all passed", StepResult{Status: StatusSuccess, HasTest: true, RepeatCounter: &StepRepeatCounter{SuccessCount: 3}}, report.Passed},
		{"repeat without a test", StepResult{Status: StatusSuccess, RepeatCounter: &StepRepeatCounter{SuccessCount: 3}}, report.Untested},
		{"repeat all failed", StepResult{Status: StatusError, HasTest: true, RepeatCounter: &StepRepeatCounter{FailureCount: 3}}, report.Failed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildStepReport(tt.sr).Status; got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildReport_FailureOnlyOnFailedSteps(t *testing.T) {
	sr := StepResult{Status: StatusSuccess, Failure: &StepFailure{Kind: FailureAssertion}}
	if got := buildStepReport(sr).Failure; got != nil {
		t.Errorf("Failure = %+v, want nil for a passing step", got)
	}
}

func TestBuildReport_FailureCarriesViolations(t *testing.T) {
	vs := []report.Violation{{In: "response", Field: "$.id", Message: "body failed"}}
	sr := StepResult{Status: StatusError, Failure: &StepFailure{Kind: FailureContractResponse, Violations: vs}}
	got := buildStepReport(sr).Failure
	if got == nil || !reflect.DeepEqual(got.Violations, vs) {
		t.Errorf("Failure = %+v, want the violations carried over", got)
	}
}

func TestBuildReport_StepCarriesContract(t *testing.T) {
	c := &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}
	for _, sr := range []StepResult{
		{Status: StatusSuccess, Contract: c},
		{Status: StatusError, Contract: c},
		{Status: StatusSuccess, Contract: c, RepeatCounter: &StepRepeatCounter{SuccessCount: 1, Checked: true}},
	} {
		if got := buildStepReport(sr).Contract; got != c {
			t.Errorf("Contract = %+v, want %+v", got, c)
		}
	}
}

func TestBuildReport_NilResult(t *testing.T) {
	r := BuildReport("Empty", "", nil, []string{"a"}, time.Time{}, time.Time{})
	if r.Status != report.Passed || len(r.Jobs) != 0 {
		t.Errorf("got status %q with %d jobs, want an empty passing report", r.Status, len(r.Jobs))
	}

	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"jobs": []`) {
		t.Errorf("jobs should encode as an empty array, got:\n%s", buf.String())
	}
}

func TestJSONSafeMap(t *testing.T) {
	m := map[string]any{
		"ok":     "text",
		"bad":    make(chan int),
		"nested": map[string]any{"fn": func() {}, "n": 1},
	}

	safe := jsonSafeMap(m)
	if _, err := json.Marshal(safe); err != nil {
		t.Fatalf("result should encode, got %v", err)
	}
	if safe["ok"] != "text" {
		t.Errorf("encodable values should be kept, got %v", safe["ok"])
	}
	if nested, _ := safe["nested"].(map[string]any); nested["n"] != 1 {
		t.Errorf("encodable nested values should be kept, got %v", safe["nested"])
	}

	plain := map[string]any{"a": 1}
	if got := jsonSafeMap(plain); reflect.ValueOf(got).Pointer() != reflect.ValueOf(plain).Pointer() {
		t.Error("an encodable map should be returned as is")
	}
	if jsonSafeMap(nil) != nil {
		t.Error("nil should stay nil")
	}
}

// TestWorkflow_StartWritesReports runs a workflow end to end and checks the
// report files reflect what each step did.
func TestWorkflow_StartWritesReports(t *testing.T) {
	runner := newMockRunner()
	runner.SetResult("ok", map[string]any{
		"req": map[string]any{"path": "/ok"},
		"res": map[string]any{"code": 200},
	})
	runner.SetResult("secret", map[string]any{
		"req": map[string]any{"token": "hunter2"},
		"res": map[string]any{"code": 500, "dump": false},
	})
	runner.SetError("down", errors.New("connection refused"))

	workflow := &Workflow{
		Name: "Report Run",
		Jobs: []Job{
			{
				ID:   "checks",
				Name: "Checks",
				Steps: []*Step{
					{Name: "Passes", Uses: "ok", Test: "res.code == 200"},
					{Name: "Fails", Uses: "ok", Test: "res.code == 201"},
					{Name: "Hidden", Uses: "secret", Test: "res.code == 200"},
					{Name: "Errors", Uses: "down"},
				},
			},
			{
				ID:    "later",
				Name:  "Later",
				Needs: []string{"checks"},
				Steps: []*Step{{Name: "Never", Uses: "ok"}},
			},
		},
		printer: newBufferPrinter(),
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "report.json")
	junitPath := filepath.Join(dir, "junit.xml")
	mdPath := filepath.Join(dir, "report.md")

	err := workflow.Start(Config{ActionRunner: runner, Reports: []report.Target{
		{Format: report.JSON, Path: jsonPath},
		{Format: report.JUnit, Path: junitPath},
		{Format: report.Markdown, Path: mdPath},
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if r.Name != "Report Run" || r.Status != report.Failed {
		t.Errorf("report = %q/%q, want Report Run/failed", r.Name, r.Status)
	}
	if r.RunID == "" || r.RunID != workflow.RunID() {
		t.Errorf("run_id = %q, want the run, %q", r.RunID, workflow.RunID())
	}
	if len(r.Jobs) != 2 || r.Jobs[0].ID != "checks" || r.Jobs[1].Status != report.Skipped {
		t.Fatalf("jobs = %+v, want checks then a skipped later", r.Jobs)
	}

	steps := r.Jobs[0].Steps
	if len(steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(steps))
	}
	if steps[0].Status != report.Passed || steps[0].Test != "res.code == 200" {
		t.Errorf("step 0 = %+v, want a passed step with its test", steps[0])
	}

	fails := steps[1].Failure
	if fails == nil || fails.Kind != FailureAssertion || fails.Request["path"] != "/ok" {
		t.Errorf("step 1 failure = %+v, want an assertion with the request", fails)
	}

	hidden := steps[2].Failure
	if hidden == nil || hidden.Request != nil || hidden.Response != nil {
		t.Errorf("step 2 failure = %+v, want no request or response when dump is false", hidden)
	}
	if strings.Contains(string(data), "hunter2") {
		t.Error("a response marked dump: false must not reach the report")
	}

	errored := steps[3].Failure
	if errored == nil || errored.Kind != FailureAction || !strings.Contains(errored.Message, "connection refused") {
		t.Errorf("step 3 failure = %+v, want the action error", errored)
	}

	for _, p := range []string{junitPath, mdPath} {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Errorf("%s was not written: %v", p, err)
		}
	}
}

func TestWorkflow_StartReportsWriteError(t *testing.T) {
	workflow := &Workflow{
		Name:    "Unwritable",
		Jobs:    []Job{{Name: "job", Steps: []*Step{}}},
		printer: newBufferPrinter(),
	}

	dir := t.TempDir()
	good := filepath.Join(dir, "report.md")

	err := workflow.Start(Config{Reports: []report.Target{
		{Format: report.JSON, Path: dir}, // a directory, so the file cannot be created
		{Format: report.Markdown, Path: good},
	}})
	if err == nil || !strings.Contains(err.Error(), "json report") {
		t.Errorf("error = %v, want the json report failure", err)
	}
	if _, statErr := os.Stat(good); statErr != nil {
		t.Errorf("a failing report should not stop the others: %v", statErr)
	}
}

func TestStep_runTestRecordsFailure(t *testing.T) {
	tests := []struct {
		name     string
		test     string
		wantKind string
		wantMsg  string
	}{
		{"passes", "res.code == 200", "", ""},
		{"false", "res.code == 201", FailureAssertion, "test evaluated to false: res.code == 201 (res.code = 200)"},
		{"not a boolean", "res.code", FailureTestType, "test evaluated to 200 (int), not a boolean"},
		{"cannot evaluate", "res.code ==", FailureTestError, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &stepRun{
				Step: &Step{
					Test: tt.test,
				},
				expr: &expr.Expr{},
				ctx: StepContext{
					Req: map[string]any{"path": "/"},
					Res: map[string]any{"code": 200},
				},
				// A failure left over from an earlier evaluation must not leak.
				failure: &StepFailure{Kind: "stale"},
			}

			st.runTest(newBufferPrinter())

			if tt.wantKind == "" {
				if st.failure != nil {
					t.Errorf("failure = %+v, want nil", st.failure)
				}
				return
			}
			if st.failure == nil || st.failure.Kind != tt.wantKind {
				t.Fatalf("failure = %+v, want kind %q", st.failure, tt.wantKind)
			}
			if tt.wantMsg != "" && st.failure.Message != tt.wantMsg {
				t.Errorf("message = %q, want %q", st.failure.Message, tt.wantMsg)
			}
			if tt.wantMsg == "" && st.failure.Message == "" {
				t.Error("message should explain the failure")
			}
		})
	}
}

// TestWorkflow_GitHubSummaryOutsideActions checks that asking for a job
// summary where there is none warns but neither fails the run nor stops the
// other reports.
func TestWorkflow_GitHubSummaryOutsideActions(t *testing.T) {
	t.Setenv("GITHUB_STEP_SUMMARY", "")

	w := &Workflow{
		Name:    "summary",
		Jobs:    []Job{{Name: "job", Steps: []*Step{}}},
		printer: newBufferPrinter(),
	}
	md := filepath.Join(t.TempDir(), "report.md")

	err := w.Start(Config{Reports: []report.Target{
		{Format: report.GitHubSummary},
		{Format: report.Markdown, Path: md},
	}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if w.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d", w.exitStatus, ExitOK)
	}
	if _, err := os.Stat(md); err != nil {
		t.Errorf("the markdown report should still be written: %v", err)
	}
	stderr := w.printer.errWriter.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "[WARN] GITHUB_STEP_SUMMARY is not set") {
		t.Errorf("expected a warning, got:\n%s", stderr)
	}
}

func TestWorkflow_GitHubSummaryFull(t *testing.T) {
	// Fill the summary up to GitHub's 1 MiB limit.
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	w := &Workflow{
		Name:    "full",
		Jobs:    []Job{{Name: "job", Steps: []*Step{}}},
		printer: newBufferPrinter(),
	}
	if err := w.Start(Config{Reports: []report.Target{{Format: report.GitHubSummary, Path: path}}}); err != nil {
		t.Fatalf("a full summary should not fail the run: %v", err)
	}
	stderr := w.printer.errWriter.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "[WARN] the job summary has no room left") {
		t.Errorf("expected a warning, got:\n%s", stderr)
	}
}

// timedRunner answers the n-th call with the n-th of its answers, an error
// when the answer has none to give.
type timedRunner struct {
	mu      sync.Mutex
	calls   int
	answers []map[string]any
}

func (r *timedRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.answers[r.calls]
	r.calls++
	if a == nil {
		return nil, nil, errors.New("connection refused")
	}
	return a, nil, nil
}

// TestWorkflow_RepeatReportsLatency repeats a step whose action takes another
// time in each run: the run whose test fails is part of the distribution, as
// a slow run is no less a sample for failing a limit, and the run whose
// action failed returns no time and is not.
func TestWorkflow_RepeatReportsLatency(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 200}, "rt": "20ms"},
		{"res": map[string]any{"code": 500}, "rt": "300ms"},
		nil,
	}}
	workflow := &Workflow{
		Name: "Latency",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 4},
			Steps:  []*Step{{Name: "Query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}

	jsonPath := filepath.Join(t.TempDir(), "report.json")
	if err := workflow.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: jsonPath}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	st := r.Jobs[0].Steps[0]
	if st.Repeat == nil || st.Repeat.Latency == nil {
		t.Fatalf("step = %+v, want the latency of its runs", st)
	}
	want := report.Latency{Count: 3, MinMs: 10, MeanMs: 110, P50Ms: 20, P90Ms: 300, P95Ms: 300, P99Ms: 300, MaxMs: 300}
	if *st.Repeat.Latency != want {
		t.Errorf("latency = %+v, want %+v", *st.Repeat.Latency, want)
	}
	if st.Repeat.Success != 2 || st.Repeat.Failure != 2 {
		t.Errorf("repeat = %+v, want 2 successes and 2 failures", st.Repeat)
	}

	out := workflow.printer.outWriter.(*bytes.Buffer).String()
	if !strings.Contains(out, "min 10.0ms, p50 20.0ms, p90 300ms, p95 300ms, p99 300ms, max 300ms") {
		t.Errorf("terminal should show the latency, got:\n%s", out)
	}
}

// TestWorkflow_RepeatComparesToBaseline runs a repeated job with the report
// of an earlier run as the baseline: each step is reported with its id, or
// step_ and where it is written when it has none, and found by it in the
// baseline.
func TestWorkflow_RepeatComparesToBaseline(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 200}, "rt": "20ms"},
		{"res": map[string]any{"code": 200}, "rt": "30ms"},
		{"res": map[string]any{"code": 200}, "rt": "40ms"},
	}}
	workflow := &Workflow{
		Name: "Latency",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 2},
			Steps: []*Step{
				{Name: "Query", ID: "query", Uses: "http", Test: "res.code == 200"},
				{Name: "Again", Uses: "http", Test: "res.code == 200"},
			},
		}},
		printer: newBufferPrinter(),
	}
	started := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	baseline := &report.Report{
		RunID: "run-1", StartedAt: started,
		Jobs: []report.Job{{ID: "search", Steps: []report.Step{
			{Index: 0, ID: "query", Repeat: &report.Repeat{Total: 5, Success: 5, Latency: &report.Latency{Count: 5, P50Ms: 7}}},
			{Index: 1, ID: "step_1", Repeat: &report.Repeat{Total: 5, Success: 4, Latency: &report.Latency{Count: 5, P50Ms: 9}}},
		}}},
	}

	jsonPath := filepath.Join(t.TempDir(), "report.json")
	cfg := Config{Reports: []report.Target{{Format: report.JSON, Path: jsonPath}}, Baseline: baseline, ActionRunner: runner}
	if err := workflow.Start(cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}

	r, err := report.Read(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if r.Baseline == nil || r.Baseline.RunID != "run-1" || !r.Baseline.StartedAt.Equal(started) {
		t.Errorf("Baseline = %+v, want the run compared to", r.Baseline)
	}
	for i, want := range []struct {
		id    string
		p50Ms float64
	}{{"query", 7}, {"step_1", 9}} {
		st := r.Jobs[0].Steps[i]
		if st.ID != want.id {
			t.Errorf("step %d id = %q, want %q", i, st.ID, want.id)
		}
		if st.Repeat == nil || st.Repeat.Baseline == nil || st.Repeat.Baseline.Latency.P50Ms != want.p50Ms {
			t.Errorf("step %d repeat = %+v, want a baseline with p50 %vms", i, st.Repeat, want.p50Ms)
		}
	}
}

// TestWorkflow_RepeatLatencyLeavesOutRetriedError retries a step whose first
// attempt returns a time and fails its test, and whose last attempt fails to
// run: the run returned no time, so it adds none, rather than that of the
// attempt before.
func TestWorkflow_RepeatLatencyLeavesOutRetriedError(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 500}, "rt": "50ms"},
		nil,
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
	}}
	workflow := &Workflow{
		Name: "Latency",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 2},
			Steps: []*Step{{
				Name: "Query", Uses: "http", Test: "res.code == 200",
				Retry: &StepRetry{MaxAttempts: 2},
			}},
		}},
		printer: newBufferPrinter(),
	}

	jsonPath := filepath.Join(t.TempDir(), "report.json")
	if err := workflow.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: jsonPath}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	l := r.Jobs[0].Steps[0].Repeat.Latency
	if l == nil || l.Count != 1 || l.MaxMs != 10 {
		t.Errorf("latency = %+v, want the one time of the second run", l)
	}
}

// TestWorkflow_RepeatLeavesOutWarmup repeats a step after two warmup runs,
// one whose action failed and one whose test did: neither fails the job or
// sets the exit status, and the counts and the times are of the runs after
// them.
func TestWorkflow_RepeatLeavesOutWarmup(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		nil,
		{"res": map[string]any{"code": 500}, "rt": "900ms"},
		{"res": map[string]any{"code": 200}, "rt": "10ms"},
		{"res": map[string]any{"code": 200}, "rt": "20ms"},
		{"res": map[string]any{"code": 200}, "rt": "30ms"},
	}}
	workflow := &Workflow{
		Name: "Warmup",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 3, Warmup: 2},
			Steps:  []*Step{{Name: "Query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}

	jsonPath := filepath.Join(t.TempDir(), "report.json")
	if err := workflow.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: jsonPath}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if workflow.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitOK)
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if r.Status != report.Passed {
		t.Errorf("status = %q, want %q", r.Status, report.Passed)
	}
	st := r.Jobs[0].Steps[0]
	if st.Repeat == nil {
		t.Fatalf("step = %+v, want its repeat", st)
	}
	if st.Repeat.Total != 3 || st.Repeat.Success != 3 || st.Repeat.Failure != 0 || st.Repeat.Warmup != 2 {
		t.Errorf("repeat = %+v, want 3 successes after 2 warmup runs", st.Repeat)
	}
	if l := st.Repeat.Latency; l == nil || l.Count != 3 || l.MinMs != 10 || l.MaxMs != 30 {
		t.Errorf("latency = %+v, want the times of the 3 runs after the warmup", l)
	}
}

// TestWorkflow_RepeatWarmupLeavesExitStatus fails a run after an action error
// in a warmup run: the exit status is that of the failed test, as the action
// error is counted in nothing.
func TestWorkflow_RepeatWarmupLeavesExitStatus(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		nil,
		{"res": map[string]any{"code": 500}, "rt": "10ms"},
	}}
	workflow := &Workflow{
		Name: "Warmup",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 1, Warmup: 1},
			Steps:  []*Step{{Name: "Query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}

	if err := workflow.Start(Config{ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if workflow.exitStatus != ExitTestFailed {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitTestFailed)
	}
}

// TestWorkflow_RepeatOnlyWarmup runs a job whose one run is a warmup run: it
// fails its test, and the job passes with no run counted.
func TestWorkflow_RepeatOnlyWarmup(t *testing.T) {
	runner := &timedRunner{answers: []map[string]any{
		{"res": map[string]any{"code": 500}, "rt": "10ms"},
	}}
	workflow := &Workflow{
		Name: "Warmup",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 0, Warmup: 1},
			Steps:  []*Step{{Name: "Query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}

	jsonPath := filepath.Join(t.TempDir(), "report.json")
	if err := workflow.Start(Config{ActionRunner: runner, Reports: []report.Target{{Format: report.JSON, Path: jsonPath}}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if runner.calls != 1 {
		t.Errorf("action called %d times, want 1", runner.calls)
	}
	if workflow.exitStatus != ExitOK {
		t.Errorf("exit status = %d, want %d", workflow.exitStatus, ExitOK)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r report.Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if r.Status != report.Passed || len(r.Jobs[0].Steps) != 0 {
		t.Errorf("report = %+v, want a passed job with no step counted", r)
	}
}

// TestWorkflow_RepeatTellsWhyItsActionFailed repeats a step whose action
// fails: the error is told once, under the step's count, rather than once
// for each run as it happens.
func TestWorkflow_RepeatTellsWhyItsActionFailed(t *testing.T) {
	runner := newMockRunner()
	runner.SetError("http", errors.New(`Get "http://127.0.0.1:1/x": connection refused`))
	workflow := &Workflow{
		Name: "Repeat",
		Jobs: []Job{{
			ID:     "search",
			Name:   "Search",
			Repeat: &Repeat{Count: 3},
			Steps:  []*Step{{Name: "Query", Uses: "http", Test: "res.code == 200"}},
		}},
		printer: newBufferPrinter(),
	}
	if err := workflow.Start(Config{ActionRunner: runner}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	out := workflow.printer.outWriter.(*bytes.Buffer).String()
	lines := strings.Split(out, "\n")
	told := false
	for i, line := range lines[:len(lines)-1] {
		if strings.HasSuffix(line, "0/3 success (0.0%)") {
			told = strings.TrimSpace(lines[i+1]) == `Get "http://127.0.0.1:1/x": connection refused`
		}
	}
	if !told {
		t.Errorf("terminal should tell the error under the count, got:\n%s", out)
	}
	if n := strings.Count(out, "connection refused"); n != 1 {
		t.Errorf("terminal tells the error %d times, want once:\n%s", n, out)
	}
	if errOut := workflow.printer.errWriter.(*bytes.Buffer).String(); strings.Contains(errOut, "connection refused") {
		t.Errorf("the error should not be told as it happens outside a verbose run, got:\n%s", errOut)
	}
}

// TestWorkflow_NeedsNoCheckPassesWithoutTest passes a step without a test of
// an action Config.NeedsNoCheck names, as hello, once and repeated, while a
// step of another action without a test is untested. Without the action
// named there, its step is untested too: the engine knows of no action by
// itself.
func TestWorkflow_NeedsNoCheckPassesWithoutTest(t *testing.T) {
	answer := map[string]any{"res": map[string]any{"status": 0}, "rt": "1ms"}
	for _, tt := range []struct {
		count        int
		needsNoCheck []string
		want         string
	}{
		{count: 0, needsNoCheck: []string{"hello"}, want: report.Passed},
		{count: 2, needsNoCheck: []string{"hello"}, want: report.Passed},
		{count: 0, want: report.Untested},
		{count: 2, want: report.Untested},
	} {
		count := tt.count
		runner := &timedRunner{answers: []map[string]any{answer, answer, answer, answer}}
		job := Job{
			ID:   "report",
			Name: "Report",
			Steps: []*Step{
				{Name: "Echo", Uses: "hello", Echo: "done"},
				{Name: "Unchecked", Uses: "http"},
			},
		}
		if count > 0 {
			job.Repeat = &Repeat{Count: count}
		}
		workflow := &Workflow{Name: "Hello", Jobs: []Job{job}, printer: newBufferPrinter()}

		path := filepath.Join(t.TempDir(), "report.json")
		if err := workflow.Start(Config{Reports: []report.Target{{Format: report.JSON, Path: path}}, ActionRunner: runner, NeedsNoCheck: tt.needsNoCheck}); err != nil {
			t.Fatalf("Start: %v", err)
		}
		r, err := report.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		steps := r.Jobs[0].Steps
		if steps[0].Status != tt.want {
			t.Errorf("count %d, needs no check %v: hello step = %q, want %q", count, tt.needsNoCheck, steps[0].Status, tt.want)
		}
		if steps[1].Status != report.Untested {
			t.Errorf("count %d: http step = %q, want %q", count, steps[1].Status, report.Untested)
		}
	}
}
