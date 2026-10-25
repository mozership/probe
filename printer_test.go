package probe

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/mozership/probe/mask"
)

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
