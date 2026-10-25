package probe

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
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
		status   StatusType
		duration float64
		want     string
	}{
		{
			name:     "success status",
			jobName:  "Test Job",
			status:   StatusSuccess,
			duration: 1.5,
			want:     "⏺ Test Job (Completed in 1.50s)\n",
		},
		{
			name:     "error status",
			jobName:  "Failed Job",
			status:   StatusError,
			duration: 2.3,
			want:     "⏺ Failed Job (Failed in 2.30s)\n",
		},
		{
			name:     "skipped status",
			jobName:  "Skipped Job",
			status:   StatusSkipped,
			duration: 0.0,
			want:     "⏺ Skipped Job (SKIPPED)\n",
		},
		{
			name:     "warning status",
			jobName:  "Warning Job",
			status:   StatusWarning,
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
		status            StatusType
		expectedColorCode string // ANSI color code we expect
	}{
		{
			name:              "success uses green color",
			status:            StatusSuccess,
			expectedColorCode: "\x1b[38;2;0;175;0m", // RGB(0,175,0) from colorSuccess
		},
		{
			name:              "error uses red color",
			status:            StatusError,
			expectedColorCode: "\x1b[31m", // Red from colorError
		},
		{
			name:              "skipped uses gray color",
			status:            StatusSkipped,
			expectedColorCode: "\x1b[90m", // Bright black (gray) from colorSkipped
		},
		{
			name:              "warning uses yellow color",
			status:            StatusWarning,
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
	r.writeJobStatus(&output, "Success Job", StatusSuccess, 1.5)

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
	r.writeJobStatus(&output, "Skipped Job", StatusSkipped, 0.0)

	result := output.String()
	expected := "⏺ Skipped Job (SKIPPED)\n"

	if result != expected {
		t.Errorf("writeJobStatus() for skipped job = %q, want %q", result, expected)
	}

	// Test non-skipped job formatting for comparison
	var output2 strings.Builder
	r.writeJobStatus(&output2, "Success Job", StatusSuccess, 1.5)

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
			r.writeFooter(&output, tt.totalTime, tt.successCount, tt.totalJobs)

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
	rs := NewResult()

	// Add job1 - successful
	startTime1 := time.Now()
	endTime1 := startTime1.Add(1 * time.Second)
	rs.Jobs["job1"] = &JobResult{
		JobID:     "job1",
		JobName:   "Successful Job",
		StartTime: startTime1,
		EndTime:   endTime1,
		Status:    StatusSuccess,
		StepResults: []StepResult{
			{
				Index:  1,
				Name:   "Test Step",
				Status: StatusSuccess,
				RT:     "100ms",
			},
		},
	}

	// Add job2 - failed
	startTime2 := time.Now()
	endTime2 := startTime2.Add(2 * time.Second)
	rs.Jobs["job2"] = &JobResult{
		JobID:     "job2",
		JobName:   "Failed Job",
		StartTime: startTime2,
		EndTime:   endTime2,
		Status:    StatusError,
		StepResults: []StepResult{
			{
				Index:  1,
				Name:   "Failed Step",
				Status: StatusError,
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

	rs := NewResult()
	result := r.reportText(rs)

	// Should contain at least the footer
	if !strings.Contains(result, "Total workflow time: 0.00s") {
		t.Errorf("reportText() with empty buffer should contain footer, got %q", result)
	}
}

func TestReportRenderer_reportText_WithRepeatStep(t *testing.T) {
	r := newReportRenderer([]string{"job1"}, nil, nil)

	rs := NewResult()

	startTime := time.Now()
	endTime := startTime.Add(1 * time.Second)
	rs.Jobs["job1"] = &JobResult{
		JobID:     "job1",
		JobName:   "Job with Repeat",
		StartTime: startTime,
		EndTime:   endTime,
		Status:    StatusSuccess,
		StepResults: []StepResult{
			{
				Index:   1,
				Name:    "Repeat Step",
				Status:  StatusSuccess,
				HasTest: true,
				RepeatCounter: &StepRepeatCounter{
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
		stepResults  []StepResult
		wantContains string
		wantEndsWith string
	}{
		{
			name: "single step with report",
			stepResults: []StepResult{
				{
					Index:  1,
					Name:   "Test Step",
					Status: StatusSuccess,
					Report: "This is a test report",
				},
			},
			wantContains: "     This is a test report\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with multiline report",
			stepResults: []StepResult{
				{
					Index:  1,
					Name:   "Multiline Step",
					Status: StatusSuccess,
					Report: "Line 1\nLine 2\nLine 3",
				},
			},
			wantContains: "     Line 1\n     Line 2\n     Line 3\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with report containing trailing whitespace",
			stepResults: []StepResult{
				{
					Index:  1,
					Name:   "Whitespace Step",
					Status: StatusSuccess,
					Report: "Report with trailing spaces   \n\t\n",
				},
			},
			wantContains: "     Report with trailing spaces\n",
			wantEndsWith: "\n",
		},
		{
			name: "step with empty report",
			stepResults: []StepResult{
				{
					Index:  1,
					Name:   "Empty Report Step",
					Status: StatusSuccess,
					Report: "",
				},
			},
			wantContains: "Empty Report Step",
			wantEndsWith: "\n",
		},
		{
			name: "multiple steps with reports",
			stepResults: []StepResult{
				{
					Index:  1,
					Name:   "First Step",
					Status: StatusSuccess,
					Report: "First report",
				},
				{
					Index:  2,
					Name:   "Second Step",
					Status: StatusSuccess,
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

	repeated := func(index int, id string, sec float64) StepResult {
		return StepResult{
			Index: index, ID: id, Name: id, Status: StatusSuccess, HasTest: true,
			RepeatCounter: &StepRepeatCounter{ID: id, Name: id, SuccessCount: 1, RepeatTotal: 1, RTs: []float64{sec}},
		}
	}
	steps := []StepResult{
		repeated(0, "search", 0.12),
		repeated(1, "added", 0.005),
		{Index: 2, ID: "once", Name: "once", Status: StatusSuccess},
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

	stepResult := StepResult{
		Index:   1,
		Name:    "Repeat Step",
		Status:  StatusSuccess,
		HasTest: false,
		RepeatCounter: &StepRepeatCounter{
			Name:         "Repeat Step",
			SuccessCount: 3,
			FailureCount: 0,
			RepeatTotal:  3,
			EchoOutputs: []string{
				"       iteration 1\n",
				"       iteration 2\n",
				"       iteration 3\n",
			},
		},
	}

	got := r.stepResultsText("", []StepResult{stepResult})

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
	rs := NewResult()
	rs.Jobs["job"] = &JobResult{
		JobID:   "job",
		JobName: "Job hunter2",
		StepResults: []StepResult{
			{Index: 0, Name: "Log in as hunter2", Status: StatusSuccess, RT: "1ms"},
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
		counter StepRepeatCounter
		hasTest bool
		want    string
	}{
		{"never run", StepRepeatCounter{SkippedCount: 3, RepeatTotal: 3}, true, "skipped in 3/3 runs"},
		{"run in some", StepRepeatCounter{SuccessCount: 1, SkippedCount: 2, RepeatTotal: 3}, true, "1/3 success (33.3%), 2 skipped"},
		{"without a test", StepRepeatCounter{SuccessCount: 2, SkippedCount: 1, RepeatTotal: 3}, false, "2/3 completed (no test), 1 skipped"},
		{"none skipped", StepRepeatCounter{SuccessCount: 3, RepeatTotal: 3}, true, "3/3 success (100.0%)\n"},
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
		counter StepRepeatCounter
		hasTest bool
		want    string
	}{
		{"all passed", StepRepeatCounter{SuccessCount: 3, RepeatTotal: 3}, true, "    ✓  3/3 success (100.0%)\n"},
		{"some failed", StepRepeatCounter{SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}, true, "    △  2/3 success (66.7%)\n"},
		{"all failed", StepRepeatCounter{FailureCount: 3, RepeatTotal: 3}, true, "    ✗  0/3 success (0.0%)\n"},
		{"no test", StepRepeatCounter{SuccessCount: 1000, RepeatTotal: 1000}, false, "    △  1000/1000 completed (no test)\n"},
		{"no test, some failed", StepRepeatCounter{SuccessCount: 2, FailureCount: 1, RepeatTotal: 3}, false, "    ✗  2/3 completed (no test)\n"},
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
		rt   *RepeatTestResult
		want string
	}{
		{"none", nil, ""},
		{"skipped", &RepeatTestResult{Test: "p95 < 100", Skipped: "no run took a time"}, "    ⏭  repeat test: p95 < 100\n       skipped: no run took a time\n"},
		{"passed", &RepeatTestResult{Test: "p95 < 100", Passed: true}, "    ✓  repeat test: p95 < 100\n"},
		{"failed", &RepeatTestResult{Test: "p95 < 100", Output: "       p95 = 120\n"}, "    ✗  repeat test: p95 < 100\n       p95 = 120\n"},
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
		step StepResult
		want string
	}{
		{"no time", StepResult{Index: 1, Name: "get", Status: StatusSuccess}, " 1. ✓  get\n"},
		{"start alone", StepResult{Index: 1, Name: "get", Status: StatusSuccess, StartedAt: started}, " 1. ✓  get (@1700000000)\n"},
		{"time alone", StepResult{Index: 1, Name: "get", Status: StatusSuccess, RT: "12ms"}, " 1. ✓  get (12ms)\n"},
		{"start and time", StepResult{Index: 1, Name: "get", Status: StatusSuccess, StartedAt: started, RT: "12ms"}, " 1. ✓  get (@1700000000 12ms)\n"},
		{"retried, then timed", StepResult{Index: 1, Name: "get", Status: StatusSuccess, RetryAttempt: 2, RetryMax: 3, RT: "12ms"}, " 1. ✓  get ⟳ 2/3 (12ms)\n"},
		{"waited", StepResult{Index: 1, Name: "get", Status: StatusSuccess, WaitTime: "1s"}, " 1. ✓  " + IconWait + "1s → get\n"},
		{"failed", StepResult{Index: 2, Name: "post", Status: StatusError}, " 2. ✗  post\n"},
		{"failed, with why", StepResult{Index: 2, Name: "post", Status: StatusError, TestOutput: "       res.code = 500"}, " 2. ✗  post\n       res.code = 500\n"},
		{"untested", StepResult{Index: 3, Name: "sleep", Status: StatusWarning}, " 3. △  sleep\n"},
		{"skipped", StepResult{Index: 4, Name: "later", Status: StatusSkipped}, " 4. ⏭  later (SKIPPED)\n"},
		{"echoed", StepResult{Index: 5, Name: "echo", Status: StatusWarning, EchoOutput: "hello\n\n"}, " 5. △  echo\nhello\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.stepResultsText("", []StepResult{tt.step}); got != tt.want {
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
		got := r.stepResultsText("", []StepResult{{Index: 1, Name: "get", Status: StatusSuccess, RetryAttempt: attempt, RetryMax: 3}})
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

	got := r.stepResultsText("", []StepResult{{
		Index: 1, Name: "echo", Status: StatusWarning,
		RepeatCounter: &StepRepeatCounter{Name: "echo", SuccessCount: 3, RepeatTotal: 3, EchoOutputs: []string{"first\n", " \n\t", "third"}},
	}})
	if want := "    △  3/3 completed (no test)\nfirst\nthird\n"; !strings.HasSuffix(got, want) {
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
		failure *StepFailure
		want    string
	}{
		{"no failure", nil, ""},
		{"no message", &StepFailure{Kind: FailureAction}, ""},
		{"the action", &StepFailure{Kind: FailureAction, Message: "connection refused"}, "       connection refused\n"},
		{"a template", &StepFailure{Kind: FailureTemplate, Message: "cannot fetch y"}, "       cannot fetch y\n"},
		{"the guard", &StepFailure{Kind: FailureRefused, Message: "refused"}, "       refused\n"},
		{"the test", &StepFailure{Kind: FailureAssertion, Message: "res.code == 200"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			r.writeStepRepeatError(&out, &StepRepeatCounter{Failure: tt.failure})
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
