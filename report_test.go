package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/report"

	"github.com/mozership/probe/expr"
)

// newReportResult builds a Result covering every status a report has to map.
func newReportResult() (*Result, []string) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	rs := NewResult()

	rs.Jobs["api"] = &JobResult{
		JobID:     "api",
		JobName:   "API",
		Status:    "Failed",
		StartTime: start,
		EndTime:   start.Add(1500 * time.Millisecond),
		Success:   false,
		StepResults: []StepResult{
			{Index: 0, Name: "Health", Status: StatusSuccess, Test: "res.code == 200", Elapsed: 120 * time.Millisecond, EchoOutput: "       line 1\n       line 2\n"},
			{Index: 1, Name: "Create", Status: StatusError, Test: "res.code == 201", Elapsed: 80 * time.Millisecond,
				RetryAttempt: 3, RetryMax: 3,
				Failure: &StepFailure{
					Kind:     FailureAssertion,
					Message:  "test evaluated to false",
					Request:  map[string]any{"url": "http://example.test/items"},
					Response: map[string]any{"code": 500, "body": "boom ``` ]]>"},
				}},
			{Index: 2, Name: "Connect", Status: StatusError, Failure: &StepFailure{Kind: FailureAction, Message: "connection refused"}},
			{Index: 3, Name: "Cleanup (SKIPPED)", Status: StatusSkipped},
			{Index: 4, Name: "Note", Status: StatusWarning},
		},
	}
	rs.Jobs["after"] = &JobResult{
		JobID:     "after",
		JobName:   "After | API",
		Status:    "skipped",
		StartTime: start,
		EndTime:   start,
		Success:   true,
	}
	rs.Jobs["poll"] = &JobResult{
		JobID:     "poll",
		JobName:   "Poll",
		Status:    "Failed",
		StartTime: start,
		EndTime:   start.Add(3 * time.Second),
		Success:   false,
		StepResults: []StepResult{
			{Index: 0, Name: "Tick", Status: StatusWarning, HasTest: true, Test: "res.code == 0",
				RepeatCounter: &StepRepeatCounter{Name: "Tick", SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}},
		},
	}
	rs.Jobs["setup"] = &JobResult{
		JobID:     "setup",
		JobName:   "Setup",
		Status:    "Failed",
		StartTime: start,
		EndTime:   start.Add(10 * time.Millisecond),
		Success:   false,
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
		t.Errorf("echo = %q, want the terminal indentation removed", steps[0].Echo)
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
		t.Errorf("skipped step = %q/%q, want the SKIPPED marker removed from the name", steps[3].Name, steps[3].Status)
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
	runner := NewMockActionRunner()
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
					{Name: "Passes", Uses: "ok", Test: "res.code == 200", actionRunner: runner},
					{Name: "Fails", Uses: "ok", Test: "res.code == 201", actionRunner: runner},
					{Name: "Hidden", Uses: "secret", Test: "res.code == 200", actionRunner: runner},
					{Name: "Errors", Uses: "down", actionRunner: runner},
				},
			},
			{
				ID:    "later",
				Name:  "Later",
				Needs: []string{"checks"},
				Steps: []*Step{{Name: "Never", Uses: "ok", actionRunner: runner}},
			},
		},
		printer: newBufferPrinter(),
	}

	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "report.json")
	junitPath := filepath.Join(dir, "junit.xml")
	mdPath := filepath.Join(dir, "report.md")

	err := workflow.Start(Config{Reports: []report.Target{
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

func TestStep_DoTestRecordsFailure(t *testing.T) {
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
			st := &Step{
				Test: tt.test,
				Expr: &expr.Expr{},
				ctx: StepContext{
					Req: map[string]any{"path": "/"},
					Res: map[string]any{"code": 200},
				},
				// A failure left over from an earlier evaluation must not leak.
				failure: &StepFailure{Kind: "stale"},
			}

			st.DoTest(newBufferPrinter())

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
