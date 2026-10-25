package probe

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/report"

	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/mask"
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
			if got := buildStepReport(tt.sr, nil).Status; got != tt.want {
				t.Errorf("status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildReport_FailureOnlyOnFailedSteps(t *testing.T) {
	sr := StepResult{Status: StatusSuccess, Failure: &StepFailure{Kind: FailureAssertion}}
	if got := buildStepReport(sr, nil).Failure; got != nil {
		t.Errorf("Failure = %+v, want nil for a passing step", got)
	}
}

func TestBuildReport_FailureCarriesViolations(t *testing.T) {
	vs := []report.Violation{{In: "response", Field: "$.id", Message: "body failed"}}
	sr := StepResult{Status: StatusError, Failure: &StepFailure{Kind: FailureContractResponse, Violations: vs}}
	got := buildStepReport(sr, nil).Failure
	if got == nil || !reflect.DeepEqual(got.Violations, vs) {
		t.Errorf("Failure = %+v, want the violations carried over", got)
	}
}

// The message of a failure in the report files is told anew as they are
// written, but it is the message the failure was made with when the masker
// has learned nothing since: of a test that is false, with its operands
// shown or hidden and a secret the masker knew in one cut short, of one that
// could not be evaluated or did not come to a boolean, of an action's error
// and of a broken contract, alone or as the first of the runs of a repeated
// step that failed.
func TestBuildReport_KeepsTheMessageOfAFailure(t *testing.T) {
	const token = "tk-0123456789abcdefghijklmnopqrstuvwxyz"
	m := mask.New(nil, nil)
	m.Learn(map[string]any{"headers": map[string]any{"authorization": token}})
	printer := newBufferPrinter()
	printer.SetMasker(m)
	env := StepContext{Res: map[string]any{"code": 500, "body": strings.Repeat("x", 180) + token}}

	failed := func(test string, hideOperands bool) *StepFailure {
		t.Helper()
		passed, f := evaluateTest(&expr.Expr{}, test, env, printer, hideOperands)
		if passed {
			t.Fatalf("%s held, want it to fail", test)
		}
		return f
	}
	tests := []struct {
		name    string
		failure *StepFailure
		want    string
	}{
		{name: "a test that is false", failure: failed("res.code == 200", false), want: "test evaluated to false: res.code == 200 (res.code = 500)"},
		{name: "a test that is false, its operands hidden", failure: failed("res.code == 200", true), want: "test evaluated to false: res.code == 200"},
		{name: "a test that is false, a known secret cut short", failure: failed(`res.body == ""`, false), want: `test evaluated to false: res.body == "" (res.body = "` + strings.Repeat("x", 180) + `<redacted>")`},
		{name: "a test that is false of two values", failure: failed("res.code == 200 && res.body != ''", false), want: "test evaluated to false: res.code == 200 (res.code = 500)"},
		{name: "a test that cannot be evaluated", failure: failed("nosuch(1)", false)},
		{name: "a test that is not a boolean", failure: failed("res.code", false), want: "test evaluated to 500 (int), not a boolean"},
		{name: "an action's error", failure: &StepFailure{Kind: FailureAction, Message: "dial tcp 127.0.0.1:1: connect: connection refused"}, want: "dial tcp 127.0.0.1:1: connect: connection refused"},
		{name: "a broken contract", failure: &StepFailure{Kind: FailureContractResponse, Message: "the response breaks its contract in 1 place"}, want: "the response breaks its contract in 1 place"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want != "" && tt.failure.Message != tt.want {
				t.Fatalf("Message = %q, want %q", tt.failure.Message, tt.want)
			}
			if got := buildStepReport(StepResult{Status: StatusError, Failure: tt.failure}, m).Failure.Message; got != tt.failure.Message {
				t.Errorf("step: message = %q, want %q", got, tt.failure.Message)
			}
			if got := buildRepeatTestReport(&RepeatTestResult{Failure: tt.failure}, m).Failure.Message; got != tt.failure.Message {
				t.Errorf("test of a repeated job: message = %q, want %q", got, tt.failure.Message)
			}
			repeated := repeatStepResult(0, StepRepeatCounter{FailureCount: 2, SuccessCount: 1, Failure: tt.failure}, false)
			if got := buildStepReport(repeated, m).Failure.Message; got != repeated.Failure.Message {
				t.Errorf("repeated step: message = %q, want %q", got, repeated.Failure.Message)
			}
		})
	}
}

// What a false condition compared is masked in the message of the report
// files with what the masker knows as they are written, before it is cut, so
// a secret it learned after the test was checked is hidden whole.
func TestBuildReport_MasksTheMessageWithWhatTheMaskerLearnedSince(t *testing.T) {
	const token = "tk-0123456789abcdefghijklmnopqrstuvwxyz"
	m := mask.New(nil, nil)
	printer := newBufferPrinter()
	printer.SetMasker(m)
	env := StepContext{Res: map[string]any{"body": strings.Repeat("x", 180) + token}}
	_, f := evaluateTest(&expr.Expr{}, `res.body == ""`, env, printer, false)
	if f == nil || !strings.Contains(f.Message, token[:12]) {
		t.Fatalf("failure = %+v, want the message to have the start of the token cut short", f)
	}

	m.Learn(map[string]any{"headers": map[string]any{"authorization": token}})
	want := `test evaluated to false: res.body == "" (res.body = "` + strings.Repeat("x", 180) + `<redacted>")`
	if got := buildStepReport(StepResult{Status: StatusError, Failure: f}, m).Failure.Message; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	// A repeated step keeps the count of the runs it failed in before the
	// message of the first of them.
	repeated := repeatStepResult(0, StepRepeatCounter{FailureCount: 2, SuccessCount: 1, Failure: f}, false)
	if got, want := buildStepReport(repeated, m).Failure.Message, "2 of 3 iterations failed; the first: "+want; got != want {
		t.Errorf("repeated step: message = %q, want %q", got, want)
	}
}

func TestBuildReport_StepCarriesContract(t *testing.T) {
	c := &report.Contract{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"}
	for _, sr := range []StepResult{
		{Status: StatusSuccess, Contract: c},
		{Status: StatusError, Contract: c},
		{Status: StatusSuccess, Contract: c, RepeatCounter: &StepRepeatCounter{SuccessCount: 1, Checked: true}},
	} {
		if got := buildStepReport(sr, nil).Contract; got != c {
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
