package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mozership/probe/report"
)

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

// A secret that a later step teaches the masker is hidden in the report
// files as it is in the terminal report: the message of a failure, which
// tells what the false condition compared, is made as the files are
// written, masked before the values are cut, so no file has the part of the
// secret that the cut left. A repeated step is told by the first of its
// runs that failed, which the later step had not run after either.
func TestWorkflow_Start_MasksWhatALaterStepTeachesInReportFiles(t *testing.T) {
	const token = "tk-0123456789abcdefghijklmnopqrstuvwxyz"
	tests := []struct {
		name   string
		body   string
		test   string
		repeat *Repeat
	}{
		// The response is cut after 512 bytes, inside the token.
		{name: "in the response", body: strings.Repeat("x", 500) + token, test: "res.code == 200"},
		// A value the condition compared is cut after 200 characters,
		// inside the token.
		{name: "in the false condition", body: strings.Repeat("x", 180) + token, test: `res.body == ""`},
		{name: "in the false condition of a repeated step", body: strings.Repeat("x", 180) + token, test: `res.body == ""`, repeat: &Repeat{Count: 2}},
	}
	formats := map[report.Format]string{report.JSON: "report.json", report.JUnit: "report.xml", report.Markdown: "report.md"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaks := newMockRunner()
			leaks.SetResult("http", map[string]any{"res": map[string]any{"code": 500, "body": tt.body}, "status": 0})
			w := &Workflow{
				Name: "Leak",
				Jobs: []Job{{ID: "j", Name: "J", Repeat: tt.repeat, Steps: []*Step{
					{Name: "Leaks", Uses: "http", Test: tt.test},
					// The token is a credential of this step, which teaches
					// the masker it.
					{Name: "Uses", Uses: "http", With: map[string]any{"headers": map[string]any{"authorization": token}}},
				}}},
				printer: newBufferPrinter(),
			}
			dir := t.TempDir()
			var targets []report.Target
			for format, file := range formats {
				targets = append(targets, report.Target{Format: format, Path: filepath.Join(dir, file)})
			}
			runner := stepRunners{byName: map[string]ActionRunner{"Leaks": leaks}, fallback: newMockRunner()}
			if err := w.Start(Config{ActionRunner: runner, Reports: targets}); err != nil {
				t.Fatalf("Start() error: %v", err)
			}

			for format, file := range formats {
				data, err := os.ReadFile(filepath.Join(dir, file))
				if err != nil {
					t.Fatal(err)
				}
				got := string(data)
				if !strings.Contains(got, "Leaks") || !strings.Contains(got, "<redacted>") {
					t.Fatalf("the %s report should have the failed step with the token hidden:\n%s", format, got)
				}
				for _, part := range []string{token, token[:9], token[:12]} {
					if strings.Contains(got, part) {
						t.Errorf("the %s report has %q of the token:\n%s", format, part, got)
					}
				}
			}
		})
	}
}
