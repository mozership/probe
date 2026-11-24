package probe

import (
	"bytes"
	"testing"

	"github.com/fatih/color"
	"github.com/mozership/probe/internal/display"
	"github.com/mozership/probe/internal/outcome"
)

// failureTextOf is what the report shows under a step that failed with f.
func failureTextOf(f *outcome.Failure, test string) string {
	return display.NewRenderer(nil, nil, nil).FailureText(f, test)
}

// TestPrintTestResult_StepContext shows of the context of a step what its
// test reads, without its request and response.
func TestPrintTestResult_StepContext(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	errOut := new(bytes.Buffer)
	p := display.NewPrinterTo(true, new(bytes.Buffer), errOut)
	p.PrintTestResult(false, "res.code == 200", StepContext{
		Vars:   map[string]any{"host": "example.com"},
		Req:    map[string]any{"url": "http://example.com"},
		Res:    map[string]any{"code": 404},
		RT:     ResponseTime{Duration: "1.5ms", Sec: 0.0015},
		Status: 0,
	}.testEnv())

	want := "[DEBUG] Test: Failure (input: res.code == 200)\n[DEBUG] Env:\n" +
		"[DEBUG]   outputs: {}\n[DEBUG]   repeat_index: 0\n[DEBUG]   report: \"\"\n" +
		"[DEBUG]   rt:\n[DEBUG]     duration: 1.5ms\n[DEBUG]     sec: 0.0015\n" +
		"[DEBUG]   status: 0\n[DEBUG]   vars:\n[DEBUG]     host: example.com\n"
	if got := errOut.String(); got != want {
		t.Errorf("PrintTestResult()\ngot  %q\nwant %q", got, want)
	}
}
