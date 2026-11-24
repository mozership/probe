package probe

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/internal/outcome"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

func TestReportRenderer_headerText(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name        string
		title       string
		description string
		want        string
	}{
		{
			name:        "empty name",
			title:       "",
			description: "Some description",
			want:        "",
		},
		{
			name:        "name only",
			title:       "Test Workflow",
			description: "",
			want:        "Test Workflow\n\n",
		},
		{
			name:        "name and description",
			title:       "Test Workflow",
			description: "This is a test workflow",
			want:        "Test Workflow\nThis is a test workflow\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.headerText(tt.title, tt.description)
			if result != tt.want {
				t.Errorf("headerText() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestReportRenderer_writeJobStatus(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name     string
		jobName  string
		status   outcome.Status
		duration float64
		want     string
	}{
		{
			name:     "success status",
			jobName:  "Test Job",
			status:   outcome.StatusSuccess,
			duration: 1.5,
			want:     "⏺ Test Job (Completed in 1.50s)\n",
		},
		{
			name:     "error status",
			jobName:  "Failed Job",
			status:   outcome.StatusError,
			duration: 2.3,
			want:     "⏺ Failed Job (Failed in 2.30s)\n",
		},
		{
			name:     "skipped status",
			jobName:  "Skipped Job",
			status:   outcome.StatusSkipped,
			duration: 0.0,
			want:     "⏺ Skipped Job (SKIPPED)\n",
		},
		{
			name:     "warning status",
			jobName:  "Warning Job",
			status:   outcome.StatusWarning,
			duration: 0.1,
			want:     "⏺ Warning Job (Unknown status in 0.10s)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			r.writeJobStatus(&output, tt.jobName, tt.status, tt.duration)

			result := output.String()
			if result != tt.want {
				t.Errorf("writeJobStatus() = %q, want %q", result, tt.want)
			}
		})
	}
}

// Test that job status uses correct colors for each status type - with actual color detection
func TestReportRenderer_writeJobStatus_ColorMapping(t *testing.T) {
	// Enable colors to detect actual color differences
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = false

	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name              string
		status            outcome.Status
		expectedColorCode string // ANSI color code we expect
	}{
		{
			name:              "success uses green color",
			status:            outcome.StatusSuccess,
			expectedColorCode: "\x1b[38;2;0;175;0m", // RGB(0,175,0) from colorSuccess
		},
		{
			name:              "error uses red color",
			status:            outcome.StatusError,
			expectedColorCode: "\x1b[31m", // Red from colorError
		},
		{
			name:              "skipped uses gray color",
			status:            outcome.StatusSkipped,
			expectedColorCode: "\x1b[90m", // Bright black (gray) from colorSkipped
		},
		{
			name:              "warning uses yellow color",
			status:            outcome.StatusWarning,
			expectedColorCode: "\x1b[33m", // Yellow from colorWarning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			r.writeJobStatus(&output, "Test Job", tt.status, 1.0)

			result := output.String()

			// Verify basic content is present
			if !strings.Contains(result, "Test Job") {
				t.Errorf("writeJobStatus() should contain job name, got %q", result)
			}

			// Verify the expected color code is present in the output
			if !strings.Contains(result, tt.expectedColorCode) {
				t.Errorf("writeJobStatus() should contain color code %q for %s, got %q", tt.expectedColorCode, tt.name, result)
			}
		})
	}
}

// Test that SUCCESS status specifically does NOT use blue color (colorInfo)
func TestReportRenderer_writeJobStatus_SuccessNotBlue(t *testing.T) {
	// Enable colors to detect actual color differences
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = false

	r := newReportRenderer(nil, nil, nil)

	var output strings.Builder
	r.writeJobStatus(&output, "Success Job", outcome.StatusSuccess, 1.5)

	result := output.String()

	// Blue color code from colorInfo
	blueColorCode := "\x1b[34m"

	// SUCCESS jobs should NOT contain blue color
	if strings.Contains(result, blueColorCode) {
		t.Errorf("StatusSuccess should NOT use blue color (colorInfo), but found blue color code in output: %q", result)
	}

	// SUCCESS jobs SHOULD contain green color
	greenColorCode := "\x1b[38;2;0;175;0m" // RGB(0,175,0) from colorSuccess
	if !strings.Contains(result, greenColorCode) {
		t.Errorf("StatusSuccess should use green color (colorSuccess), but green color code not found in output: %q", result)
	}
}

// Test that skipped jobs have gray formatting for entire line
func TestReportRenderer_writeJobStatus_SkippedFormatting(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)

	// Test skipped job formatting
	var output strings.Builder
	r.writeJobStatus(&output, "Skipped Job", outcome.StatusSkipped, 0.0)

	result := output.String()
	expected := "⏺ Skipped Job (SKIPPED)\n"

	if result != expected {
		t.Errorf("writeJobStatus() for skipped job = %q, want %q", result, expected)
	}

	// Test non-skipped job formatting for comparison
	var output2 strings.Builder
	r.writeJobStatus(&output2, "Success Job", outcome.StatusSuccess, 1.5)

	result2 := output2.String()
	expected2 := "⏺ Success Job (Completed in 1.50s)\n"

	if result2 != expected2 {
		t.Errorf("writeJobStatus() for success job = %q, want %q", result2, expected2)
	}
}

func TestReportRenderer_writeUnderJob(t *testing.T) {
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty input",
			input: "",
			want:  "\n",
		},
		{
			name:  "single line",
			input: "Test output",
			want:  "  ⎿ Test output\n\n",
		},
		{
			name:  "multiple lines",
			input: "Line 1\nLine 2\nLine 3",
			want:  "  ⎿ Line 1\n    Line 2\n    Line 3\n\n",
		},
		{
			name:  "with empty lines",
			input: "Line 1\n\nLine 3",
			want:  "  ⎿ Line 1\n    Line 3\n\n",
		},
		{
			name:  "whitespace only input",
			input: "   \n  \t  \n   ",
			want:  "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			r.writeUnderJob(&output, tt.input)

			result := output.String()
			if result != tt.want {
				t.Errorf("writeUnderJob() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestReportRenderer_writeFooter(t *testing.T) {
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name         string
		totalTime    float64
		successCount int
		totalJobs    int
		wantContains []string
	}{
		{
			name:         "all jobs succeeded",
			totalTime:    5.25,
			successCount: 3,
			totalJobs:    3,
			wantContains: []string{"Total workflow time: 5.25s", "All jobs succeeded"},
		},
		{
			name:         "some jobs failed",
			totalTime:    10.5,
			successCount: 2,
			totalJobs:    5,
			wantContains: []string{"Total workflow time: 10.50s", "3 job(s) failed"},
		},
		{
			name:         "all jobs failed",
			totalTime:    2.1,
			successCount: 0,
			totalJobs:    2,
			wantContains: []string{"Total workflow time: 2.10s", "2 job(s) failed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			start := time.Unix(1700000000, 0)
			totals := reportTotals{succeeded: tt.successCount, start: start, end: start.Add(time.Duration(tt.totalTime * float64(time.Second)))}
			r.writeFooter(&output, totals, tt.totalJobs)

			result := output.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("writeFooter() should contain %q, got %q", want, result)
				}
			}
		})
	}
}

func TestReportRenderer_reportText(t *testing.T) {
	r := newReportRenderer([]string{"job1", "job2"}, nil, nil)

	// Create test WorkflowBuffer
	rs := outcome.New()

	// Add job1 - successful
	startTime1 := time.Now()
	endTime1 := startTime1.Add(1 * time.Second)
	rs.Jobs["job1"] = &outcome.Job{
		JobID:     "job1",
		JobName:   "Successful Job",
		StartTime: startTime1,
		EndTime:   endTime1,
		Status:    outcome.StatusSuccess,
		StepResults: []outcome.Step{
			{
				Index:  1,
				Name:   "Test Step",
				Status: outcome.StatusSuccess,
				RT:     "100ms",
			},
		},
	}

	// Add job2 - failed
	startTime2 := time.Now()
	endTime2 := startTime2.Add(2 * time.Second)
	rs.Jobs["job2"] = &outcome.Job{
		JobID:     "job2",
		JobName:   "Failed Job",
		StartTime: startTime2,
		EndTime:   endTime2,
		Status:    outcome.StatusError,
		StepResults: []outcome.Step{
			{
				Index:  1,
				Name:   "Failed Step",
				Status: outcome.StatusError,
			},
		},
	}

	result := r.reportText(rs)

	// Verify the report contains expected elements
	expectedContains := []string{
		"Successful Job",
		"Failed Job",
		"Test Step",
		"Failed Step",
		"Total workflow time:",
		"1 job(s) failed",
	}

	for _, expected := range expectedContains {
		if !strings.Contains(result, expected) {
			t.Errorf("reportText() should contain %q, got:\n%s", expected, result)
		}
	}
}

func TestReportRenderer_reportText_EmptyBuffer(t *testing.T) {
	r := newReportRenderer(nil, nil, nil)

	rs := outcome.New()
	result := r.reportText(rs)

	// Should contain at least the footer
	if !strings.Contains(result, "Total workflow time: 0.00s") {
		t.Errorf("reportText() with empty buffer should contain footer, got %q", result)
	}
}

func TestReportRenderer_reportText_WithRepeatStep(t *testing.T) {
	r := newReportRenderer([]string{"job1"}, nil, nil)

	rs := outcome.New()

	startTime := time.Now()
	endTime := startTime.Add(1 * time.Second)
	rs.Jobs["job1"] = &outcome.Job{
		JobID:     "job1",
		JobName:   "Job with Repeat",
		StartTime: startTime,
		EndTime:   endTime,
		Status:    outcome.StatusSuccess,
		StepResults: []outcome.Step{
			{
				Index:   1,
				Name:    "Repeat Step",
				Status:  outcome.StatusSuccess,
				HasTest: true,
				RepeatCounter: &outcome.RepeatCounter{
					SuccessCount: 8,
					FailureCount: 2,
					Name:         "Repeat Step",
					LastResult:   true,
					RepeatTotal:  10,
				},
			},
		},
	}

	result := r.reportText(rs)

	expectedContains := []string{
		"Job with Repeat",
		"Repeat Step (repeating 10 times)",
		"8/10 success (80.0%)",
		"Total workflow time:",
		"All jobs succeeded",
	}

	for _, expected := range expectedContains {
		if !strings.Contains(result, expected) {
			t.Errorf("reportText() should contain %q, got:\n%s", expected, result)
		}
	}
}

// Test embedded job report formatting with newlines
func TestReportRenderer_stepResultsText_ReportNewlineFormatting(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name         string
		stepResults  []outcome.Step
		wantContains string
		wantEndsWith string
	}{
		{
			name: "single step with report",
			stepResults: []outcome.Step{
				{
					Index:  1,
					Name:   "Test Step",
					Status: outcome.StatusSuccess,
					Report: "This is a test report",
				},
			},
			wantContains: "     This is a test report\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with multiline report",
			stepResults: []outcome.Step{
				{
					Index:  1,
					Name:   "Multiline Step",
					Status: outcome.StatusSuccess,
					Report: "Line 1\nLine 2\nLine 3",
				},
			},
			wantContains: "     Line 1\n     Line 2\n     Line 3\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with report containing trailing whitespace",
			stepResults: []outcome.Step{
				{
					Index:  1,
					Name:   "Whitespace Step",
					Status: outcome.StatusSuccess,
					Report: "Report with trailing spaces   \n\t\n",
				},
			},
			wantContains: "     Report with trailing spaces\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with empty report",
			stepResults: []outcome.Step{
				{
					Index:  1,
					Name:   "Empty Report Step",
					Status: outcome.StatusSuccess,
					Report: "",
				},
			},
			wantContains: "Empty Report Step",
			wantEndsWith: "\n",
		},
		{
			name: "multiple steps with reports",
			stepResults: []outcome.Step{
				{
					Index:  1,
					Name:   "First Step",
					Status: outcome.StatusSuccess,
					Report: "First report",
				},
				{
					Index:  2,
					Name:   "Second Step",
					Status: outcome.StatusSuccess,
					Report: "Second report\nWith multiple lines",
				},
			},
			wantContains: "     First report\n",
			wantEndsWith: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.stepResultsText("", tt.stepResults)

			// Check that the result contains the expected formatted report
			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("stepResultsText() should contain %q, got:\n%s", tt.wantContains, result)
			}

			// Check that the result ends with a newline to prevent display corruption
			if !strings.HasSuffix(result, tt.wantEndsWith) {
				t.Errorf("stepResultsText() should end with %q, got result ending with: %q", tt.wantEndsWith, result[len(result)-5:])
			}

			// Verify that embedded job reports are properly indented with 5 spaces
			lines := strings.SplitSeq(result, "\n")
			for line := range lines {
				if strings.Contains(line, "report") && len(line) > 0 {
					if !strings.HasPrefix(line, "     ") {
						t.Errorf("Report lines should be indented with 5 spaces, got line: %q", line)
					}
				}
			}
		})
	}
}

// TestReportRenderer_stepResultsText_Baseline shows under the times
// of each repeated step those of the step of its id in the baseline, in the
// job of the same id, and says so of a step the baseline has none of.
func TestReportRenderer_stepResultsText_Baseline(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	repeated := func(index int, id string, sec float64) outcome.Step {
		return outcome.Step{
			Index: index, ID: id, Name: id, Status: outcome.StatusSuccess, HasTest: true,
			RepeatCounter: &outcome.RepeatCounter{ID: id, Name: id, SuccessCount: 1, RepeatTotal: 1, RTs: []float64{sec}},
		}
	}
	steps := []outcome.Step{
		repeated(0, "search", 0.12),
		repeated(1, "added", 0.005),
		{Index: 2, ID: "once", Name: "once", Status: outcome.StatusSuccess},
	}
	baseline := &report.Report{Jobs: []report.Job{
		{ID: "other", Steps: []report.Step{
			{ID: "search", Repeat: &report.Repeat{Latency: &report.Latency{Count: 1, P50Ms: 1, P95Ms: 1, P99Ms: 1}}},
		}},
		{ID: "api", Steps: []report.Step{
			{ID: "search", Repeat: &report.Repeat{Latency: &report.Latency{Count: 1, P50Ms: 100, P95Ms: 200, P99Ms: 120}}},
		}},
	}}

	r := newReportRenderer(nil, nil, nil)
	if got := r.stepResultsText("api", steps); strings.Contains(got, "baseline") {
		t.Errorf("without a baseline nothing should be shown against one, got:\n%s", got)
	}

	r.baseline = baseline
	got := r.stepResultsText("api", steps)
	for _, want := range []string{
		"max 120ms\n      baseline p50 100ms (+20.0%), p95 200ms (-40.0%), p99 120ms\n",
		"max 5.0ms\n      no baseline\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output should contain %q, got:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "baseline"); n != 2 {
		t.Errorf("output should show the baseline of the 2 repeated steps alone, got:\n%s", got)
	}
}

func TestReportRenderer_stepResultsText_RepeatEchoOutputs(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)

	stepResult := outcome.Step{
		Index:   1,
		Name:    "Repeat Step",
		Status:  outcome.StatusSuccess,
		HasTest: false,
		RepeatCounter: &outcome.RepeatCounter{
			Name:         "Repeat Step",
			SuccessCount: 3,
			FailureCount: 0,
			RepeatTotal:  3,
			Echoes: []string{
				"iteration 1\n",
				"iteration 2\n",
				"iteration 3\n",
			},
		},
	}

	got := r.stepResultsText("", []outcome.Step{stepResult})

	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("iteration %d", i)
		if !strings.Contains(got, want) {
			t.Errorf("output should contain %q, got:\n%s", want, got)
		}
	}

	idx1 := strings.Index(got, "iteration 1")
	idx2 := strings.Index(got, "iteration 2")
	idx3 := strings.Index(got, "iteration 3")
	if idx1 >= idx2 || idx2 >= idx3 {
		t.Errorf("echo outputs should appear in order, got positions %d, %d, %d", idx1, idx2, idx3)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output should end with newline, got %q", got)
	}
}

// The report is shown as it is rendered, so it hides the secrets its masker
// knows, as the printer does in what it writes.
func TestReportRenderer_MasksTheReport(t *testing.T) {
	r := newReportRenderer([]string{"job"}, nil, mask.New([]string{"S"}, map[string]string{"S": "hunter2"}))
	rs := outcome.New()
	rs.Jobs["job"] = &outcome.Job{
		JobID:   "job",
		JobName: "Job hunter2",
		StepResults: []outcome.Step{
			{Index: 0, Name: "Log in as hunter2", Status: outcome.StatusSuccess, RT: "1ms"},
		},
	}

	for name, got := range map[string]string{"reportText": r.reportText(rs), "stepsText": r.stepsText(rs)} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("%s() shows the secret:\n%s", name, got)
		}
		if !strings.Contains(got, "Log in as") {
			t.Errorf("%s() should have the step, got:\n%s", name, got)
		}
	}
}

func TestReportRenderer_writeStepRepeatResult_Skipped(t *testing.T) {
	// The colors are put back as they were, for the tests after this one.
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name    string
		counter outcome.RepeatCounter
		hasTest bool
		want    string
	}{
		{"never run", outcome.RepeatCounter{SkippedCount: 3, RepeatTotal: 3}, true, "skipped in 3/3 runs"},
		{"run in some", outcome.RepeatCounter{SuccessCount: 1, SkippedCount: 2, RepeatTotal: 3}, true, "1/3 success (33.3%), 2 skipped"},
		{"without a test", outcome.RepeatCounter{SuccessCount: 2, SkippedCount: 1, RepeatTotal: 3}, false, "2/3 completed (no test), 1 skipped"},
		{"none skipped", outcome.RepeatCounter{SuccessCount: 3, RepeatTotal: 3}, true, "3/3 success (100.0%)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r.writeStepRepeatResult(&out, &tt.counter, tt.hasTest)
			if !strings.Contains(out.String(), tt.want) {
				t.Errorf("writeStepRepeatResult() = %q, want it to hold %q", out.String(), tt.want)
			}
		})
	}
}

// A repeated step tells by its icon how its runs went: all passed, some
// failed, or all failed; without a test, whether any of them failed.
func TestReportRenderer_writeStepRepeatResult_Icons(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name    string
		counter outcome.RepeatCounter
		hasTest bool
		want    string
	}{
		{"all passed", outcome.RepeatCounter{SuccessCount: 3, RepeatTotal: 3}, true, "    ✓  3/3 success (100.0%)\n"},
		{"some failed", outcome.RepeatCounter{SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}, true, "    △  2/3 success (66.7%)\n"},
		{"all failed", outcome.RepeatCounter{FailureCount: 3, RepeatTotal: 3}, true, "    ✗  0/3 success (0.0%)\n"},
		{"no test", outcome.RepeatCounter{SuccessCount: 1000, RepeatTotal: 1000}, false, "    △  1000/1000 completed (no test)\n"},
		{"no test, some failed", outcome.RepeatCounter{SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}, false, "    ✗  2/3 completed (no test)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r.writeStepRepeatResult(&out, &tt.counter, tt.hasTest)
			if got := out.String(); got != tt.want {
				t.Errorf("writeStepRepeatResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReportRenderer_repeatTestText(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name string
		rt   *outcome.RepeatTest
		want string
	}{
		{"none", nil, ""},
		{"skipped", &outcome.RepeatTest{Test: "p95 < 100", Skipped: "no run took a time"}, "    ⏭  repeat test: p95 < 100\n       skipped: no run took a time\n"},
		{"passed", &outcome.RepeatTest{Test: "p95 < 100", Passed: true}, "    ✓  repeat test: p95 < 100\n"},
		{"failed", &outcome.RepeatTest{Test: "p95 < 100", Failure: &outcome.Failure{Kind: outcome.FailureAssertion, Clause: &expr.Clause{Expr: "p95 < 100", Operands: []expr.Operand{{Expr: "p95", Value: 120}}}}}, "    ✗  repeat test: p95 < 100\n       failed: p95 < 100\n         p95 = 120\n"},
		{"not a boolean", &outcome.RepeatTest{Test: "p95", Failure: &outcome.Failure{Kind: outcome.FailureTestType, Got: 120}}, "    ✗  repeat test: p95\n       Test: `p95` = 120\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.repeatTestText("repeat test", tt.rt); got != tt.want {
				t.Errorf("repeatTestText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A step's line ends with the attempt it passed in, when it was retried,
// and with when it started and how long it took, as far as they are known.
func TestReportRenderer_stepResultsText_StepLine(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true
	r := newReportRenderer(nil, nil, nil)
	started := time.Unix(1700000000, 0)

	tests := []struct {
		name string
		step outcome.Step
		want string
	}{
		{"no time", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess}, " 1. ✓  get\n"},
		{"start alone", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess, StartedAt: started}, " 1. ✓  get (@1700000000)\n"},
		{"time alone", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess, RT: "12ms"}, " 1. ✓  get (12ms)\n"},
		{"start and time", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess, StartedAt: started, RT: "12ms"}, " 1. ✓  get (@1700000000 12ms)\n"},
		{"retried, then timed", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess, RetryAttempt: 2, RetryMax: 3, RT: "12ms"}, " 1. ✓  get ⟳ 2/3 (12ms)\n"},
		{"waited", outcome.Step{Index: 1, Name: "get", Status: outcome.StatusSuccess, WaitTime: "1s"}, " 1. ✓  " + IconWait + "1s → get\n"},
		{"failed", outcome.Step{Index: 2, Name: "post", Status: outcome.StatusError}, " 2. ✗  post\n"},
		{"failed, with why", outcome.Step{Index: 2, Name: "post", Status: outcome.StatusError, Failure: &outcome.Failure{Kind: outcome.FailureAction, Message: "connection refused"}}, " 2. ✗  post\n       connection refused\n\n"},
		{"failed, with nothing to tell", outcome.Step{Index: 2, Name: "post", Status: outcome.StatusError, Failure: &outcome.Failure{Kind: outcome.FailureAssertion}}, " 2. ✗  post\n"},
		{"untested", outcome.Step{Index: 3, Name: "sleep", Status: outcome.StatusWarning}, " 3. △  sleep\n"},
		{"skipped", outcome.Step{Index: 4, Name: "later", Status: outcome.StatusSkipped}, " 4. ⏭  later (SKIPPED)\n"},
		{"echoed", outcome.Step{Index: 5, Name: "echo", Status: outcome.StatusWarning, Echo: "hello\n\n", HasEcho: true}, " 5. △  echo\n       hello\n"},
		{"echoed nothing", outcome.Step{Index: 5, Name: "echo", Status: outcome.StatusWarning, HasEcho: true}, " 5. △  echo\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.stepResultsText("", []outcome.Step{tt.step}); got != tt.want {
				t.Errorf("stepResultsText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The attempt a step passed in is dim when it passed in the first, and in
// the color of a warning when it took more.
func TestReportRenderer_stepResultsText_RetryColor(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = false
	r := newReportRenderer(nil, nil, nil)

	if colorDim().Sprint("x") == colorWarning().Sprint("x") {
		t.Fatal("the dim and the warning colors should differ")
	}
	for attempt, want := range map[int]string{
		1: colorDim().Sprintf(" %s %d/%d", IconRetry, 1, 3),
		2: colorWarning().Sprintf(" %s %d/%d", IconRetry, 2, 3),
	} {
		got := r.stepResultsText("", []outcome.Step{{Index: 1, Name: "get", Status: outcome.StatusSuccess, RetryAttempt: attempt, RetryMax: 3}})
		if !strings.Contains(got, want) {
			t.Errorf("attempt %d: stepResultsText() = %q, want it to hold %q", attempt, got, want)
		}
	}
}

// What a repeated step echoed is shown under it, run by run, without the
// runs that echoed nothing but white space.
func TestReportRenderer_stepResultsText_RepeatEchoSkipsBlank(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true
	r := newReportRenderer(nil, nil, nil)

	got := r.stepResultsText("", []outcome.Step{{
		Index: 1, Name: "echo", Status: outcome.StatusWarning,
		RepeatCounter: &outcome.RepeatCounter{Name: "echo", SuccessCount: 3, RepeatTotal: 3, Echoes: []string{"first\n", " \n\t", "third"}},
	}})
	if want := "    △  3/3 completed (no test)\n       first\n       third\n"; !strings.HasSuffix(got, want) {
		t.Errorf("stepResultsText() = %q, want it to end with %q", got, want)
	}
}

// A repeated step shows why its first run failed when its test could not
// tell: the error of its action, a template or the guard. A failed test is
// told by the count.
func TestReportRenderer_writeStepRepeatError(t *testing.T) {
	r := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name    string
		failure *outcome.Failure
		want    string
	}{
		{"no failure", nil, ""},
		{"no message", &outcome.Failure{Kind: outcome.FailureAction}, ""},
		{"the action", &outcome.Failure{Kind: outcome.FailureAction, Message: "connection refused"}, "       connection refused\n"},
		{"a template", &outcome.Failure{Kind: outcome.FailureTemplate, Message: "cannot fetch y"}, "       cannot fetch y\n"},
		{"the guard", &outcome.Failure{Kind: outcome.FailureRefused, Message: "refused"}, "       refused\n"},
		{"the test", &outcome.Failure{Kind: outcome.FailureAssertion, Message: "res.code == 200"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r.writeStepRepeatError(&out, &outcome.RepeatCounter{Failure: tt.failure})
			if got := out.String(); got != tt.want {
				t.Errorf("writeStepRepeatError() = %q, want %q", got, tt.want)
			}
		})
	}
}

// There is nothing to render of no result.
func TestReportRenderer_NilResult(t *testing.T) {
	r := newReportRenderer([]string{"job"}, nil, nil)
	if got := r.reportText(nil); got != "" {
		t.Errorf("reportText(nil) = %q, want nothing", got)
	}
	if got := r.stepsText(nil); got != "" {
		t.Errorf("stepsText(nil) = %q, want nothing", got)
	}
}

// What a step echoed is written under it, each line indented to the column
// of its name; an echo that could not be evaluated, by its error.
func TestWriteEcho(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name     string
		content  string
		err      error
		expected string
	}{
		{
			name:     "single line content",
			content:  "Hello World",
			err:      nil,
			expected: "       Hello World\n",
		},
		{
			name:     "multi-line content with explicit newlines",
			content:  "Line 1\nLine 2\nLine 3",
			err:      nil,
			expected: "       Line 1\n       Line 2\n       Line 3\n",
		},
		{
			name:     "complex multiline with indentation",
			content:  "Header\n  Indented\n    More indented\nBack to left",
			err:      nil,
			expected: "       Header\n         Indented\n           More indented\n       Back to left\n",
		},
		{
			name:     "empty line handling",
			content:  "Line 1\n\nLine 3",
			err:      nil,
			expected: "       Line 1\n       \n       Line 3\n",
		},
		{
			name:     "error case",
			content:  "",
			err:      fmt.Errorf("template error"),
			expected: "       error: template error\n",
		},
		{
			name:     "empty content",
			content:  "",
			err:      nil,
			expected: "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeEcho(&b, echoText(tt.content, tt.err))
			if got := b.String(); got != tt.expected {
				t.Errorf("writeEcho() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// failureTextOf returns why f failed, as the report shows it under its step,
// for a renderer without a masker.
func failureTextOf(f *outcome.Failure, test string) string {
	return newReportRenderer(nil, nil, nil).failureText(f, test)
}

// Why a step failed is shown under it by the kind of its failure, from
// what the failure holds.
func TestReportRenderer_failureText(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	clause := &expr.Clause{Expr: "res.code == 200", Operands: []expr.Operand{{Expr: "res.code", Value: 500}}}
	tests := []struct {
		name    string
		failure *outcome.Failure
		test    string
		want    string
	}{
		{"none", nil, "", ""},
		{"false, with the exchange", &outcome.Failure{Kind: outcome.FailureAssertion, Clause: clause, Request: map[string]any{"url": "/"}, Response: map[string]any{"code": 500}},
			"res.code == 200",
			"       failed: res.code == 200\n         res.code = 500\n       request:\n         url: /\n       response:\n         code: 500\n"},
		{"false, without the exchange", &outcome.Failure{Kind: outcome.FailureAssertion, Clause: &expr.Clause{Expr: "res.code == 200"}}, "res.code == 200", "       failed: res.code == 200\n"},
		{"false, its condition unknown", &outcome.Failure{Kind: outcome.FailureAssertion}, "res.code == 200", ""},
		{"not evaluated", &outcome.Failure{Kind: outcome.FailureTestError, Message: "unknown name nosuch\n | nosuch"}, "nosuch", "       test error: unknown name nosuch\n        | nosuch\n"},
		{"not a boolean", &outcome.Failure{Kind: outcome.FailureTestType, Got: 500}, "res.code", "       Test: `res.code` = 500\n"},
		{"broken contract", &outcome.Failure{Kind: outcome.FailureContractResponse, Violations: []report.Violation{{In: "response", Field: "body.id", Reason: "required"}}},
			"",
			"       " + (report.Violation{In: "response", Field: "body.id", Reason: "required"}).String() + "\n"},
		{"an action error", &outcome.Failure{Kind: outcome.FailureAction, Message: "connection refused\n"}, "", "       connection refused\n"},
		{"a template", &outcome.Failure{Kind: outcome.FailureTemplate, Message: "with.url: unknown"}, "", "       with.url: unknown\n"},
		{"refused", &outcome.Failure{Kind: outcome.FailureRefused, Message: "refused: not here"}, "", "       refused: not here\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failureTextOf(tt.failure, tt.test); got != tt.want {
				t.Errorf("failureText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The condition that is false is kept as it is, and masked as the report
// shows it, by the masker of the renderer: what that knows by then is
// hidden, also where the value is cut short.
func TestReportRenderer_failureText_MasksTheClause(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	const secret = "s3cr3t-value-of-the-run"
	m := mask.New([]string{"S"}, map[string]string{"S": secret})
	r := newReportRenderer(nil, nil, m)
	value := strings.Repeat("x", 190) + secret
	f := &outcome.Failure{Kind: outcome.FailureAssertion, Clause: &expr.Clause{
		Expr:     `res.body == ""`,
		Operands: []expr.Operand{{Expr: "res.body", Value: value}},
	}}

	got := r.failureText(f, `res.body == ""`)
	if strings.Contains(got, secret[:6]) || !strings.Contains(got, "<") {
		t.Errorf("failureText() = %q, want the secret hidden before the value is cut", got)
	}
	if f.Clause.Operands[0].Value != value {
		t.Error("failureText() changed the failure's clause, want it kept as it is")
	}
}

// A line of the progress of a run starts with the time it is written, and
// the label of its job is padded to the width of the labels, by the width
// the label takes on a terminal, so that the steps line up.
func TestReportRenderer_progressText(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	r := newReportRenderer(nil, nil, nil)
	at := time.Date(2026, 11, 16, 9, 5, 3, 7_000_000, time.UTC)
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"start", r.stepStartText(progressLine{at: at, label: "unit", width: 6}, "go test"), "09:05:03.007 [unit]   ▸ go test\n"},
		{"wide label", r.stepStartText(progressLine{at: at, label: "結合", width: 6}, "probe"), "09:05:03.007 [結合]   ▸ probe\n"},
		{"widest label", r.stepStartText(progressLine{at: at, label: "e2e", width: 3}, "probe"), "09:05:03.007 [e2e] ▸ probe\n"},
		{"done", r.stepDoneText(progressLine{at: at, label: "unit", width: 4}, outcome.Step{Name: "go vet", Status: outcome.StatusSuccess, RT: "1.2s"}), "09:05:03.007 [unit] ✓ go vet (1.2s)\n"},
		{"repeated", r.stepDoneText(progressLine{at: at, label: "unit", width: 4}, outcome.Step{Name: "poll", Status: outcome.StatusError, RT: "1s", RepeatCounter: &outcome.RepeatCounter{SuccessCount: 2, RepeatTotal: 3}}), "09:05:03.007 [unit] ✗ poll (2/3 success)\n"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
