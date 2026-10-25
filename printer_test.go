package probe

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

// Color function tests
func TestColorFunctions(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name     string
		colorFn  func() *color.Color
		text     string
		expected string
	}{
		{
			name:     "colorSuccess",
			colorFn:  colorSuccess,
			text:     "success",
			expected: "success",
		},
		{
			name:     "colorError",
			colorFn:  colorError,
			text:     "error",
			expected: "error",
		},
		{
			name:     "colorInfo",
			colorFn:  colorInfo,
			text:     "info",
			expected: "info",
		},
		{
			name:     "colorWarning",
			colorFn:  colorWarning,
			text:     "warning",
			expected: "warning",
		},
		{
			name:     "colorSkipped",
			colorFn:  colorSkipped,
			text:     "skipped",
			expected: "skipped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Sprintf
			result := tt.colorFn().Sprintf("%s", tt.text)
			if result != tt.expected {
				t.Errorf("Sprintf: expected %s, got %s", tt.expected, result)
			}

			// Test SprintFunc
			sprintFunc := tt.colorFn().SprintFunc()
			result2 := sprintFunc(tt.text)
			if result2 != tt.expected {
				t.Errorf("SprintFunc: expected %s, got %s", tt.expected, result2)
			}
		})
	}
}

func TestColorSuccess_RGB(t *testing.T) {
	// Test that colorSuccess returns the correct RGB color
	c := colorSuccess()

	// The color should contain RGB values for 0,175,0
	// We can't directly test RGB values, but we can test the function returns a valid color
	if c == nil {
		t.Error("colorSuccess() should return a non-nil *color.Color")
	}

	// Test that it can format text
	result := c.Sprintf("test")
	if !strings.Contains(result, "test") {
		t.Errorf("colorSuccess().Sprintf should contain 'test', got %s", result)
	}
}

func TestRepeatNoTestDisplay(t *testing.T) {
	// Test the "no test" display format
	totalCount := 1000

	actual := colorInfo().Sprintf("⏺") + " " +
		colorInfo().Sprintf("%d/%d completed (no test)", totalCount, totalCount)

	// Check that the format contains expected parts
	if !strings.Contains(actual, "1000/1000 completed (no test)") {
		t.Errorf("Expected format to contain '1000/1000 completed (no test)', got %s", actual)
	}
}

// Printer interface tests
func TestNewPrinter(t *testing.T) {
	printer := NewPrinter(false)
	if printer == nil {
		t.Error("NewPrinter() should return a non-nil Printer")
		return
	}

	if printer.verbose {
		t.Error("NewPrinter(false) should set verbose to false")
	}

	verbosePrinter := NewPrinter(true)
	if !verbosePrinter.verbose {
		t.Error("NewPrinter(true) should set verbose to true")
	}
}

// The spinner only draws on a terminal, so a test cannot run its render
// goroutine. The reader here does what that goroutine does: it reads Suffix
// under the spinner's own lock. Run with -race, a write to Suffix under any
// other lock is reported.
func TestAddSpinnerSuffix_SharesSpinnerLock(t *testing.T) {
	p := NewPrinter(false)

	done := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			p.spinner.Lock()
			_ = p.spinner.Suffix
			p.spinner.Unlock()
		}
	}()

	for i := range 100 {
		p.AddSpinnerSuffix(fmt.Sprintf("step %d", i))
	}
	close(done)
	<-readerDone

	if got, want := p.spinner.Suffix, " step 99..."; got != want {
		t.Errorf("Suffix = %q, want %q", got, want)
	}
}

func TestStatusType(t *testing.T) {
	tests := []struct {
		status   StatusType
		expected string
	}{
		{StatusSuccess, "success"},
		{StatusError, "error"},
		{StatusWarning, "warning"},
		{StatusSkipped, "skipped"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			// Test that the status type constants are properly defined
			if int(tt.status) < 0 {
				t.Errorf("StatusType %s should have a valid integer value", tt.expected)
			}
		})
	}
}

// Test StatusType constants have expected values
func TestStatusTypeConstants(t *testing.T) {
	// Test that status constants are assigned in expected order
	if StatusSuccess != 0 {
		t.Errorf("StatusSuccess should be 0, got %d", StatusSuccess)
	}
	if StatusError != 1 {
		t.Errorf("StatusError should be 1, got %d", StatusError)
	}
	if StatusWarning != 2 {
		t.Errorf("StatusWarning should be 2, got %d", StatusWarning)
	}
	if StatusSkipped != 3 {
		t.Errorf("StatusSkipped should be 3, got %d", StatusSkipped)
	}

	// Test that all constants are different
	statuses := []StatusType{StatusSuccess, StatusError, StatusWarning, StatusSkipped}
	for i, status1 := range statuses {
		for j, status2 := range statuses {
			if i != j && status1 == status2 {
				t.Errorf("StatusType constants should be unique, but %d and %d are both %d", i, j, status1)
			}
		}
	}
}

func TestStepResult(t *testing.T) {
	result := StepResult{
		Index:      1,
		Name:       "Test Step",
		Status:     StatusSuccess,
		RT:         "100ms",
		TestOutput: "",
		EchoOutput: "",
		HasTest:    true,
	}

	if result.Index != 1 {
		t.Errorf("Expected Index to be 1, got %d", result.Index)
	}

	if result.Name != "Test Step" {
		t.Errorf("Expected Name to be 'Test Step', got %s", result.Name)
	}

	if result.Status != StatusSuccess {
		t.Errorf("Expected Status to be StatusSuccess, got %d", result.Status)
	}

	if !result.HasTest {
		t.Error("Expected HasTest to be true")
	}
}

// Generate method tests
func TestPrinter_generateHeader(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := newReportRenderer(nil, nil, nil)

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
			result := printer.generateHeader(tt.title, tt.description)
			if result != tt.want {
				t.Errorf("generateHeader() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestPrinter_generateError(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := NewPrinter(false)

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple error",
			format: "Something went wrong",
			args:   []any{},
			want:   "Error: Something went wrong\n",
		},
		{
			name:   "formatted error",
			format: "Failed to process %s with code %d",
			args:   []any{"file.txt", 404},
			want:   "Error: Failed to process file.txt with code 404\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := printer.generateError(tt.format, tt.args...)
			if result != tt.want {
				t.Errorf("generateError() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestPrinter_generateLogDebug(t *testing.T) {
	printer := NewPrinter(false)

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple debug",
			format: "Debug message",
			args:   []any{},
			want:   "[DEBUG] Debug message\n",
		},
		{
			name:   "formatted debug",
			format: "Processing item %d of %d",
			args:   []any{5, 10},
			want:   "[DEBUG] Processing item 5 of 10\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := printer.generateLogDebug(tt.format, tt.args...)
			if result != tt.want {
				t.Errorf("generateLogDebug() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestPrinter_generateLogError(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := NewPrinter(false)

	tests := []struct {
		name   string
		format string
		args   []any
		want   string
	}{
		{
			name:   "simple log error",
			format: "Critical error occurred",
			args:   []any{},
			want:   "[ERROR] Critical error occurred\n",
		},
		{
			name:   "formatted log error",
			format: "Database connection failed: %s",
			args:   []any{"timeout"},
			want:   "[ERROR] Database connection failed: timeout\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := printer.generateLogError(tt.format, tt.args...)
			if result != tt.want {
				t.Errorf("generateLogError() = %q, want %q", result, tt.want)
			}
		})
	}
}
func TestPrinter_generateJobStatus(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name     string
		jobID    string
		jobName  string
		status   StatusType
		duration float64
		want     string
	}{
		{
			name:     "success status",
			jobID:    "job1",
			jobName:  "Test Job",
			status:   StatusSuccess,
			duration: 1.5,
			want:     "⏺ Test Job (Completed in 1.50s)\n",
		},
		{
			name:     "error status",
			jobID:    "job2",
			jobName:  "Failed Job",
			status:   StatusError,
			duration: 2.3,
			want:     "⏺ Failed Job (Failed in 2.30s)\n",
		},
		{
			name:     "skipped status",
			jobID:    "job3",
			jobName:  "Skipped Job",
			status:   StatusSkipped,
			duration: 0.0,
			want:     "⏺ Skipped Job (SKIPPED)\n",
		},
		{
			name:     "warning status",
			jobID:    "job4",
			jobName:  "Warning Job",
			status:   StatusWarning,
			duration: 0.1,
			want:     "⏺ Warning Job (Unknown status in 0.10s)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			printer.generateJobStatus(tt.jobID, tt.jobName, tt.status, tt.duration, &output)

			result := output.String()
			if result != tt.want {
				t.Errorf("generateJobStatus() = %q, want %q", result, tt.want)
			}
		})
	}
}

// Test that job status uses correct colors for each status type - with actual color detection
func TestPrinter_generateJobStatus_ColorMapping(t *testing.T) {
	// Enable colors to detect actual color differences
	color.NoColor = false
	defer func() { color.NoColor = true }()

	printer := newReportRenderer(nil, nil, nil)

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
			printer.generateJobStatus("test-job", "Test Job", tt.status, 1.0, &output)

			result := output.String()

			// Verify basic content is present
			if !strings.Contains(result, "Test Job") {
				t.Errorf("generateJobStatus() should contain job name, got %q", result)
			}

			// Verify the expected color code is present in the output
			if !strings.Contains(result, tt.expectedColorCode) {
				t.Errorf("generateJobStatus() should contain color code %q for %s, got %q", tt.expectedColorCode, tt.name, result)
			}
		})
	}
}

// Test that SUCCESS status specifically does NOT use blue color (colorInfo)
func TestPrinter_generateJobStatus_SuccessNotBlue(t *testing.T) {
	// Enable colors to detect actual color differences
	color.NoColor = false
	defer func() { color.NoColor = true }()

	printer := newReportRenderer(nil, nil, nil)

	var output strings.Builder
	printer.generateJobStatus("success-job", "Success Job", StatusSuccess, 1.5, &output)

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

// Test PrintMapData with nested structures
func TestPrinter_PrintMapData(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := NewPrinter(true) // verbose mode to see LogDebug output

	tests := []struct {
		name     string
		data     map[string]any
		expected []string // strings that should appear in the output
	}{
		{
			name: "simple flat map",
			data: map[string]any{
				"key1": "value1",
				"key2": 42,
			},
			expected: []string{"key1: value1", "key2: 42"},
		},
		{
			name: "nested map",
			data: map[string]any{
				"outer": map[string]any{
					"inner":  "nested_value",
					"number": 123,
				},
			},
			expected: []string{"outer:", "inner: nested_value", "number: 123"},
		},
		{
			name: "deeply nested map",
			data: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "deep_value",
					},
				},
			},
			expected: []string{"level1:", "level2:", "level3: deep_value"},
		},
		{
			name: "map with slice",
			data: map[string]any{
				"items": []any{"item1", "item2", "item3"},
			},
			expected: []string{"items:", "- item1", "- item2", "- item3"},
		},
		{
			name: "map with nested slice and map",
			data: map[string]any{
				"complex": []any{
					map[string]any{
						"nested_key": "nested_value",
					},
					"simple_item",
				},
			},
			expected: []string{"complex:", "-", "nested_key: nested_value", "- simple_item"},
		},
		{
			name: "empty containers",
			data: map[string]any{
				"empty_map":   map[string]any{},
				"empty_slice": []any{},
			},
			expected: []string{"empty_map: {}", "empty_slice: []"},
		},
		{
			name: "string map and slice",
			data: map[string]any{
				"string_map": map[string]string{
					"str_key": "str_value",
				},
				"string_slice": []string{"str1", "str2"},
			},
			expected: []string{"string_map:", "str_key: str_value", "string_slice:", "- str1", "- str2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture output by redirecting to a buffer
			var captured strings.Builder
			originalOut := printer.errWriter
			printer.errWriter = &captured

			printer.PrintMapData(tt.data)

			// Restore original writer
			printer.errWriter = originalOut

			output := captured.String()

			// Check that all expected strings are present in the output
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("PrintMapData() output should contain %q, got:\n%s", expected, output)
				}
			}
		})
	}
}

// Test that skipped jobs have gray formatting for entire line
func TestPrinter_generateJobStatus_SkippedFormatting(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := newReportRenderer(nil, nil, nil)

	// Test skipped job formatting
	var output strings.Builder
	printer.generateJobStatus("skip-job", "Skipped Job", StatusSkipped, 0.0, &output)

	result := output.String()
	expected := "⏺ Skipped Job (SKIPPED)\n"

	if result != expected {
		t.Errorf("generateJobStatus() for skipped job = %q, want %q", result, expected)
	}

	// Test non-skipped job formatting for comparison
	var output2 strings.Builder
	printer.generateJobStatus("success-job", "Success Job", StatusSuccess, 1.5, &output2)

	result2 := output2.String()
	expected2 := "⏺ Success Job (Completed in 1.50s)\n"

	if result2 != expected2 {
		t.Errorf("generateJobStatus() for success job = %q, want %q", result2, expected2)
	}
}

func TestPrinter_generateJobResults(t *testing.T) {
	printer := newReportRenderer(nil, nil, nil)

	tests := []struct {
		name  string
		jobID string
		input string
		want  string
	}{
		{
			name:  "empty input",
			jobID: "job1",
			input: "",
			want:  "\n",
		},
		{
			name:  "single line",
			jobID: "job1",
			input: "Test output",
			want:  "  ⎿ Test output\n\n",
		},
		{
			name:  "multiple lines",
			jobID: "job1",
			input: "Line 1\nLine 2\nLine 3",
			want:  "  ⎿ Line 1\n    Line 2\n    Line 3\n\n",
		},
		{
			name:  "with empty lines",
			jobID: "job1",
			input: "Line 1\n\nLine 3",
			want:  "  ⎿ Line 1\n    Line 3\n\n",
		},
		{
			name:  "whitespace only input",
			jobID: "job1",
			input: "   \n  \t  \n   ",
			want:  "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			printer.generateJobResults(tt.jobID, tt.input, &output)

			result := output.String()
			if result != tt.want {
				t.Errorf("generateJobResults() = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestPrinter_generateFooter(t *testing.T) {
	printer := newReportRenderer(nil, nil, nil)

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
			printer.generateFooter(tt.totalTime, tt.successCount, tt.totalJobs, &output)

			result := output.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("generateFooter() should contain %q, got %q", want, result)
				}
			}
		})
	}
}

func TestPrinter_generateReport(t *testing.T) {
	printer := newReportRenderer([]string{"job1", "job2"}, nil, nil)

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

	result := printer.GenerateReport(rs)

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
			t.Errorf("generateReport() should contain %q, got:\n%s", expected, result)
		}
	}
}

func TestPrinter_generateReport_EmptyBuffer(t *testing.T) {
	printer := newReportRenderer(nil, nil, nil)

	rs := NewResult()
	result := printer.GenerateReport(rs)

	// Should contain at least the footer
	if !strings.Contains(result, "Total workflow time: 0.00s") {
		t.Errorf("generateReport() with empty buffer should contain footer, got %q", result)
	}
}

func TestPrinter_generateReport_WithRepeatStep(t *testing.T) {
	printer := newReportRenderer([]string{"job1"}, nil, nil)

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

	result := printer.GenerateReport(rs)

	expectedContains := []string{
		"Job with Repeat",
		"Repeat Step (repeating 10 times)",
		"8/10 success (80.0%)",
		"Total workflow time:",
		"All jobs succeeded",
	}

	for _, expected := range expectedContains {
		if !strings.Contains(result, expected) {
			t.Errorf("generateReport() should contain %q, got:\n%s", expected, result)
		}
	}
}

// Tests for new Step Output Formatting Functions

func TestPrinter_PrintTestResult(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name     string
		success  bool
		testExpr string
		context  any
		verbose  bool
		expected string
	}{
		{
			name:     "successful test in verbose mode",
			success:  true,
			testExpr: "res.status == 200",
			context:  map[string]any{"status": 200},
			verbose:  true,
			expected: "[DEBUG] Test: Success (input: res.status == 200)\n[DEBUG] Env:\n[DEBUG]   status: 200\n",
		},
		{
			name:     "failed test in verbose mode",
			success:  false,
			testExpr: "res.status == 200",
			context:  map[string]any{"status": 404},
			verbose:  true,
			expected: "[DEBUG] Test: Failure (input: res.status == 200)\n[DEBUG] Env:\n[DEBUG]   status: 404\n",
		},
		{
			name:     "the context of a step, without its request and response",
			success:  false,
			testExpr: "res.code == 200",
			context: StepContext{
				Vars:   map[string]any{"host": "example.com"},
				Req:    map[string]any{"url": "http://example.com"},
				Res:    map[string]any{"code": 404},
				RT:     ResponseTime{Duration: "1.5ms", Sec: 0.0015},
				Status: 0,
			},
			verbose: true,
			expected: "[DEBUG] Test: Failure (input: res.code == 200)\n[DEBUG] Env:\n" +
				"[DEBUG]   outputs: {}\n[DEBUG]   repeat_index: 0\n[DEBUG]   report: \"\"\n" +
				"[DEBUG]   rt:\n[DEBUG]     duration: 1.5ms\n[DEBUG]     sec: 0.0015\n" +
				"[DEBUG]   status: 0\n[DEBUG]   vars:\n[DEBUG]     host: example.com\n",
		},
		{
			name:     "test in non-verbose mode",
			success:  true,
			testExpr: "res.status == 200",
			context:  map[string]any{"status": 200},
			verbose:  false,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := newBufferPrinter()
			pr.verbose = tt.verbose
			pr.PrintTestResult(tt.success, tt.testExpr, tt.context)
			result := fmt.Sprintf("%s", pr.errWriter)
			if result != tt.expected {
				t.Errorf("PrintTestResult()\ngot  %q\nwant %q", result, tt.expected)
			}
		})
	}
}

func TestPrinter_PrintEchoContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		verbose bool
	}{
		{
			name:    "single line in verbose mode",
			content: "Hello World",
			verbose: true,
		},
		{
			name:    "multi-line in verbose mode",
			content: "Line 1\nLine 2\nLine 3",
			verbose: true,
		},
		{
			name:    "content in non-verbose mode",
			content: "Hello World",
			verbose: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.verbose = tt.verbose

			// This method prints debug output to buffer, we mainly test it doesn't panic
			printer.PrintEchoContent(tt.content)

			// Test passes if no panic occurs
		})
	}
}

func TestPrinter_PrintRequestResponse(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	tests := []struct {
		name     string
		stepIdx  int
		stepName string
		req      map[string]any
		res      map[string]any
		rt       string
		verbose  bool
	}{
		{
			name:     "simple request response in verbose mode",
			stepIdx:  1,
			stepName: "Test Step",
			req:      map[string]any{"method": "GET", "url": "http://example.com"},
			res:      map[string]any{"status": 200, "body": "OK"},
			rt:       "123ms",
			verbose:  true,
		},
		{
			name:     "nested data in verbose mode",
			stepIdx:  2,
			stepName: "Complex Step",
			req:      map[string]any{"headers": map[string]any{"Accept": "application/json"}},
			res:      map[string]any{"data": map[string]any{"id": 123, "name": "test"}},
			rt:       "456ms",
			verbose:  true,
		},
		{
			name:     "request response in non-verbose mode",
			stepIdx:  1,
			stepName: "Test Step",
			req:      map[string]any{"method": "GET"},
			res:      map[string]any{"status": 200},
			rt:       "100ms",
			verbose:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := newBufferPrinter()
			printer.verbose = tt.verbose

			// This method prints debug output to buffer, we mainly test it doesn't panic
			printer.PrintRequestResponse(tt.stepIdx, tt.stepName, tt.req, tt.res, tt.rt)

			// Test passes if no panic occurs
		})
	}
}

// Test embedded job report formatting with newlines
func TestPrinter_generateJobResultsFromStepResults_ReportNewlineFormatting(t *testing.T) {
	// Disable color output for consistent testing
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := newReportRenderer(nil, nil, nil)

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
			result := printer.generateJobResultsFromStepResults("", tt.stepResults)

			// Check that the result contains the expected formatted report
			if !strings.Contains(result, tt.wantContains) {
				t.Errorf("generateJobResultsFromStepResults() should contain %q, got:\n%s", tt.wantContains, result)
			}

			// Check that the result ends with a newline to prevent display corruption
			if !strings.HasSuffix(result, tt.wantEndsWith) {
				t.Errorf("generateJobResultsFromStepResults() should end with %q, got result ending with: %q", tt.wantEndsWith, result[len(result)-5:])
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

// TestPrinter_generateJobResultsFromStepResults_Baseline shows under the times
// of each repeated step those of the step of its id in the baseline, in the
// job of the same id, and says so of a step the baseline has none of.
func TestPrinter_generateJobResultsFromStepResults_Baseline(t *testing.T) {
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

	printer := newReportRenderer(nil, nil, nil)
	if got := printer.generateJobResultsFromStepResults("api", steps); strings.Contains(got, "baseline") {
		t.Errorf("without a baseline nothing should be shown against one, got:\n%s", got)
	}

	printer.baseline = baseline
	got := printer.generateJobResultsFromStepResults("api", steps)
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

func TestPrinter_generateJobResultsFromStepResults_RepeatEchoOutputs(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	printer := newReportRenderer(nil, nil, nil)

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

	got := printer.generateJobResultsFromStepResults("", []StepResult{stepResult})

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

func TestPrinter_MasksOutput(t *testing.T) {
	p := newBufferPrinter()
	p.SetMasker(mask.New([]string{"S"}, map[string]string{"S": `pa"ss`}))

	p.Fprint(p.outWriter, "a pa\"ss b")
	p.Fprintf(p.outWriter, " %s", `pa"ss`)
	p.Fprintln(p.outWriter, " pa\"ss")
	p.PrintError("failed with %q", `pa"ss`)

	failure := exchangeDetail(p.Masker(),
		map[string]any{"headers": map[string]any{"authorization": "Bearer x"}, "body": `pa"ss`},
		map[string]any{"body": `pa"ss`})
	p.Fprint(p.outWriter, failure)
	// A secret where a long body is cut is hidden before it is cut, not
	// shown in part.
	p.Fprint(p.outWriter, exchangeDetail(p.Masker(), nil,
		map[string]any{"body": strings.Repeat("x", 510) + `pa"ss`}))

	p.verbose = true
	p.PrintTestResult(false, "t", StepContext{
		Req: map[string]any{"cookie": "c=1"},
		Res: map[string]any{"body": `pa"ss`},
	})

	out := p.outWriter.(*bytes.Buffer).String() + p.errWriter.(*bytes.Buffer).String()
	if strings.Contains(out, "pa") {
		t.Errorf("output still shows the secret:\n%s", out)
	}
	if strings.Contains(out, "Bearer x") || strings.Contains(out, "c=1") {
		t.Errorf("output still shows a credential header:\n%s", out)
	}
	if !strings.Contains(out, "<secret:S>") || !strings.Contains(out, "<redacted>") {
		t.Errorf("expected masked markers in:\n%s", out)
	}
}

func TestIndentDetail(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"one line", "connection refused", "       connection refused\n"},
		{"several lines", "with.cmd: {{x.y}}: cannot fetch y\n | x.y\n | ..^", "       with.cmd: {{x.y}}: cannot fetch y\n        | x.y\n        | ..^\n"},
		{"surrounding newlines", "\nmessage\n", "       message\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := indentDetail(tt.in); got != tt.want {
				t.Errorf("indentDetail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOperandText(t *testing.T) {
	tests := []struct {
		name    string
		operand expr.Operand
		want    string
	}{
		{"nil", expr.Operand{Expr: "res.body.id", Value: nil}, "res.body.id = nil"},
		{"a string is quoted", expr.Operand{Expr: "res.stdout", Value: "ok\n"}, `res.stdout = "ok\n"`},
		{"an empty string", expr.Operand{Expr: "vars.name", Value: ""}, `vars.name = ""`},
		{"a number", expr.Operand{Expr: "rt.sec", Value: 0.25}, "rt.sec = 0.25"},
		{"a bool", expr.Operand{Expr: "res.complete", Value: false}, "res.complete = false"},
		{"a list", expr.Operand{Expr: "res.values", Value: []any{"a", 1}}, `res.values = ["a",1]`},
		{"a map", expr.Operand{Expr: "res.body", Value: map[string]any{"ok": true}}, `res.body = {"ok":true}`},
		{"a long value is cut", expr.Operand{Expr: "res.body", Value: strings.Repeat("x", 300)}, `res.body = "` + strings.Repeat("x", maxOperandLength-1) + "..."},
		{"a value JSON cannot hold", expr.Operand{Expr: "f", Value: func() {}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := operandText(tt.operand)
			if tt.want == "" {
				if !strings.HasPrefix(got, "f = 0x") {
					t.Errorf("operandText() = %q, want the value as %%v writes it", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("operandText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClauseSummary(t *testing.T) {
	got := clauseSummary(expr.Clause{Expr: `res.body.name == vars.name`, Operands: []expr.Operand{
		{Expr: "res.body.name", Value: "alice"},
		{Expr: "vars.name", Value: "bob"},
	}})
	if want := `res.body.name == vars.name (res.body.name = "alice", vars.name = "bob")`; got != want {
		t.Errorf("clauseSummary() = %q, want %q", got, want)
	}
	if got := clauseSummary(expr.Clause{Expr: "1 == 2"}); got != "1 == 2" {
		t.Errorf("clauseSummary() = %q, want the condition alone", got)
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

	for name, got := range map[string]string{"GenerateReport": r.GenerateReport(rs), "GenerateReportOnlySteps": r.GenerateReportOnlySteps(rs)} {
		if strings.Contains(got, "hunter2") {
			t.Errorf("%s() shows the secret:\n%s", name, got)
		}
		if !strings.Contains(got, "Log in as") {
			t.Errorf("%s() should have the step, got:\n%s", name, got)
		}
	}
}

// Jobs run concurrently, so each block of verbose output must be written at
// once: a block written a line at a time has the lines of another job land
// inside it.
func TestPrinter_WritesEachBlockAtOnce(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	const jobs, iterations = 16, 50
	blocks := []struct {
		name  string
		print func(p *Printer, job int)
	}{
		{"PrintRequestResponse", func(p *Printer, job int) {
			p.PrintRequestResponse(1, fmt.Sprintf("step of job %d", job),
				map[string]any{"job": job, "method": "GET", "url": "/x"},
				map[string]any{"job": job, "status": 200, "body": "ok"}, "1ms")
		}},
		{"PrintGivenRequest", func(p *Printer, job int) {
			p.PrintGivenRequest(1, fmt.Sprintf("step of job %d", job), map[string]any{"job": job, "get": "/x"})
		}},
		{"PrintEchoContent", func(p *Printer, job int) {
			p.PrintEchoContent(fmt.Sprintf("echo of job %d\nsecond line\nthird line", job))
		}},
		{"PrintTestResult", func(p *Printer, job int) {
			p.PrintTestResult(true, "res.status == 200", map[string]any{"job": job, "a": 1, "b": 2})
		}},
		{"PrintMapData", func(p *Printer, job int) {
			p.PrintMapData(map[string]any{"job": job, "a": 1, "b": 2})
		}},
	}

	for _, bl := range blocks {
		t.Run(bl.name, func(t *testing.T) {
			// The block each job writes, written alone.
			want := make([]string, jobs)
			for job := range jobs {
				p := newBufferPrinter()
				p.verbose = true
				bl.print(p, job)
				want[job] = p.errWriter.(*bytes.Buffer).String()
				if strings.Count(want[job], "\n") < 3 {
					t.Fatalf("job %d wrote %q, not a block of lines", job, want[job])
				}
			}

			p := newBufferPrinter()
			p.verbose = true
			var wg sync.WaitGroup
			for job := range jobs {
				wg.Go(func() {
					for range iterations {
						bl.print(p, job)
					}
				})
			}
			wg.Wait()

			got := p.errWriter.(*bytes.Buffer).String()
			for job := range jobs {
				if n := strings.Count(got, want[job]); n != iterations {
					t.Errorf("the block of job %d is whole %d times of %d; another job's lines landed inside it", job, n, iterations)
				}
			}
		})
	}
}
