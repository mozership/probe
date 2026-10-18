package probe

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestStreamPrinter builds a printer writing into buffers, together with a
// stream reporter over the given job order.
func newTestStreamPrinter(bufferIDs []string) (*Printer, *bytes.Buffer, *bytes.Buffer, *StreamReporter) {
	p := NewPrinter(false)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	p.outWriter = out
	p.errWriter = errOut
	p.spinner = nil

	r := newStreamReporter(p, newReportRenderer(bufferIDs, nil, nil))
	p.SetReporter(r)

	return p, out, errOut, r
}

// newTestResult builds a Result with three jobs in a fixed order: one
// successful, one failed and one skipped.
func newTestResult(base time.Time) *Result {
	rs := NewResult()

	rs.Jobs["first"] = &JobResult{
		JobID:     "first",
		JobName:   "First Job",
		StartTime: base,
		EndTime:   base.Add(1 * time.Second),
		Status:    StatusSuccess,
		StepResults: []StepResult{
			{Index: 0, Name: "First Step", Status: StatusSuccess, RT: "100ms"},
		},
	}

	rs.Jobs["second"] = &JobResult{
		JobID:     "second",
		JobName:   "Second Job",
		StartTime: base.Add(1 * time.Second),
		EndTime:   base.Add(3 * time.Second),
		Status:    StatusError,
		StepResults: []StepResult{
			{Index: 0, Name: "Second Step", Status: StatusError},
		},
	}

	rs.Jobs["third"] = &JobResult{
		JobID:     "third",
		JobName:   "Third Job",
		StartTime: base.Add(3 * time.Second),
		EndTime:   base.Add(3 * time.Second),
		Status:    StatusSkipped,
	}

	return rs
}

func TestParseOutputMode(t *testing.T) {
	tests := []struct {
		input   string
		want    OutputMode
		wantErr bool
	}{
		{"", OutputModeAuto, false},
		{"auto", OutputModeAuto, false},
		{"AUTO", OutputModeAuto, false},
		{"  spinner  ", OutputModeSpinner, false},
		{"spinner", OutputModeSpinner, false},
		{"stream", OutputModeStream, false},
		{"Stream", OutputModeStream, false},
		{"json", OutputModeAuto, true},
		{"none", OutputModeAuto, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseOutputMode(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseOutputMode(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseOutputMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestOutputMode_String(t *testing.T) {
	tests := []struct {
		mode OutputMode
		want string
	}{
		{OutputModeAuto, "auto"},
		{OutputModeSpinner, "spinner"},
		{OutputModeStream, "stream"},
		{OutputMode(99), "auto"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("OutputMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestOutputMode_resolve(t *testing.T) {
	// Explicit modes are never rewritten by detection.
	if got := OutputModeSpinner.resolve(); got != OutputModeSpinner {
		t.Errorf("OutputModeSpinner.resolve() = %v, want spinner", got)
	}
	if got := OutputModeStream.resolve(); got != OutputModeStream {
		t.Errorf("OutputModeStream.resolve() = %v, want stream", got)
	}

	// CI never renders a spinner, so auto must stream there.
	t.Setenv("CI", "true")
	if got := OutputModeAuto.resolve(); got != OutputModeStream {
		t.Errorf("OutputModeAuto.resolve() under CI = %v, want stream", got)
	}
}

func TestNewReporter(t *testing.T) {
	p := newBufferPrinter()

	if _, ok := newReporter(OutputModeSpinner, p, newReportRenderer(nil, nil, nil)).(*BufferedReporter); !ok {
		t.Error("newReporter(spinner) should return a *BufferedReporter")
	}
	if _, ok := newReporter(OutputModeStream, p, newReportRenderer(nil, nil, nil)).(*StreamReporter); !ok {
		t.Error("newReporter(stream) should return a *StreamReporter")
	}
}

// TestStreamReporter_MatchesBufferedReport pins the core guarantee of the
// streaming report: the bytes on stdout are identical to the buffered report,
// no matter in which order the jobs finish.
func TestStreamReporter_MatchesBufferedReport(t *testing.T) {
	order := []string{"first", "second", "third"}
	base := time.Now()

	// Completion order is deliberately the reverse of the declared order.
	p, out, _, r := newTestStreamPrinter(order)
	rs := newTestResult(base)
	rs.SetReporter(r)

	r.Start("", "")
	for _, id := range []string{"third", "second", "first"} {
		rs.notifyJobDone(id)
	}
	r.Finish(rs)

	want := newReportRenderer(order, nil, nil).GenerateReport(newTestResult(base))

	if out.String() != want {
		t.Errorf("streamed report differs from buffered report\ngot:\n%s\nwant:\n%s", out.String(), want)
	}

	_ = p
}

// TestStreamReporter_FlushesInDeclaredOrder verifies that a job finishing
// early is held back until every job declared before it has been printed.
func TestStreamReporter_FlushesInDeclaredOrder(t *testing.T) {
	order := []string{"first", "second", "third"}
	_, out, _, r := newTestStreamPrinter(order)
	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	// "second" finishes first but must not appear yet.
	rs.notifyJobDone("second")
	if out.Len() != 0 {
		t.Fatalf("job out of order should be held back, got: %q", out.String())
	}

	// "first" unblocks both itself and the queued "second".
	rs.notifyJobDone("first")
	flushed := out.String()
	if !strings.Contains(flushed, "First Job") || !strings.Contains(flushed, "Second Job") {
		t.Fatalf("flushing the head job should emit both blocks, got: %q", flushed)
	}
	if strings.Index(flushed, "First Job") > strings.Index(flushed, "Second Job") {
		t.Error("First Job should be printed before Second Job")
	}
	if strings.Contains(flushed, "Third Job") {
		t.Error("Third Job should not be printed before it is done")
	}

	rs.notifyJobDone("third")
	if !strings.Contains(out.String(), "Third Job") {
		t.Error("Third Job should be printed once it is done")
	}
}

// TestStreamReporter_FinishFlushesMissingJobs covers a job that never gets a
// JobDone notification: the footer must not swallow its block.
func TestStreamReporter_FinishFlushesMissingJobs(t *testing.T) {
	order := []string{"first", "second", "third"}
	_, out, _, r := newTestStreamPrinter(order)
	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	rs.notifyJobDone("first")
	r.Finish(rs)

	report := out.String()
	for _, name := range []string{"First Job", "Second Job", "Third Job"} {
		if !strings.Contains(report, name) {
			t.Errorf("Finish() should flush %q, got:\n%s", name, report)
		}
	}
	if !strings.Contains(report, "1 job(s) failed") {
		t.Errorf("footer should count the failed job, got:\n%s", report)
	}
}

// TestStreamReporter_JobDoneIsIdempotent guards against a job being printed
// twice when it is announced more than once.
func TestStreamReporter_JobDoneIsIdempotent(t *testing.T) {
	order := []string{"first"}
	_, out, _, r := newTestStreamPrinter(order)
	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	rs.notifyJobDone("first")
	rs.notifyJobDone("first")

	if got := strings.Count(out.String(), "First Job"); got != 1 {
		t.Errorf("First Job printed %d times, want 1", got)
	}
}

// TestStreamReporter_UnknownJobIsPrinted covers a job missing from the
// declared order: it must still reach the report rather than be dropped.
func TestStreamReporter_UnknownJobIsPrinted(t *testing.T) {
	_, out, _, r := newTestStreamPrinter([]string{"first"})
	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	rs.notifyJobDone("second")

	if !strings.Contains(out.String(), "Second Job") {
		t.Errorf("a job outside the declared order should still be printed, got: %q", out.String())
	}
}

func TestStreamReporter_ProgressGoesToStderr(t *testing.T) {
	_, out, errOut, r := newTestStreamPrinter([]string{"first"})

	r.StepStart("first", "Calling API")
	r.StepDone("first", StepResult{Name: "Calling API", Status: StatusSuccess, RT: "120ms"})
	r.StepDone("first", StepResult{Name: "Checking", Status: StatusError})
	r.StepDone("first", StepResult{Name: "Skipped one", Status: StatusSkipped})
	r.StepDone("first", StepResult{
		Name:          "Polling",
		Status:        StatusWarning,
		RepeatCounter: &StepRepeatCounter{SuccessCount: 2, RepeatTotal: 3},
	})

	if out.Len() != 0 {
		t.Errorf("progress must not pollute stdout, got: %q", out.String())
	}

	progress := errOut.String()
	for _, want := range []string{"[first]", "Calling API", "120ms", "Checking", "Skipped one", "2/3 success"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress log should contain %q, got:\n%s", want, progress)
		}
	}
	if lines := strings.Count(progress, "\n"); lines != 5 {
		t.Errorf("progress log lines = %d, want 5:\n%s", lines, progress)
	}
}

func TestStreamReporter_StartPrintsHeader(t *testing.T) {
	_, out, _, r := newTestStreamPrinter([]string{"first"})

	r.Start("My Workflow", "does things")

	header := out.String()
	if !strings.Contains(header, "My Workflow") || !strings.Contains(header, "does things") {
		t.Errorf("Start() should print the header up front, got: %q", header)
	}
}

// TestStreamReporter_ConcurrentNotifications exercises the reporter the way
// concurrent jobs do, so that `go test -race` covers its locking.
func TestStreamReporter_ConcurrentNotifications(t *testing.T) {
	order := []string{"first", "second", "third"}
	_, out, _, r := newTestStreamPrinter(order)
	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	var wg sync.WaitGroup
	for _, id := range order {
		wg.Add(1)
		go func(jobID string) {
			defer wg.Done()
			for i := range 10 {
				rs.AddStepResult(jobID, StepResult{Index: i, Name: "step", Status: StatusSuccess})
			}
			rs.notifyJobDone(jobID)
		}(id)
	}
	wg.Wait()
	r.Finish(rs)

	report := out.String()
	firstAt := strings.Index(report, "First Job")
	secondAt := strings.Index(report, "Second Job")
	thirdAt := strings.Index(report, "Third Job")
	if firstAt < 0 || secondAt < 0 || thirdAt < 0 {
		t.Fatalf("all jobs should be reported, got:\n%s", report)
	}
	if firstAt >= secondAt || secondAt >= thirdAt {
		t.Errorf("jobs should keep the declared order, got:\n%s", report)
	}
}

func TestBufferedReporter_PrintsOnFinish(t *testing.T) {
	p := NewPrinter(false)
	out := new(bytes.Buffer)
	p.outWriter = out
	p.spinner = nil

	r := newBufferedReporter(p, newReportRenderer([]string{"first"}, nil, nil))
	p.SetReporter(r)

	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	r.Start("My Workflow", "does things")
	r.StepStart("first", "Calling API")
	rs.notifyJobDone("first")

	if out.Len() != 0 {
		t.Errorf("buffered reporter must print nothing before Finish, got: %q", out.String())
	}

	r.Finish(rs)

	report := out.String()
	if !strings.Contains(report, "My Workflow") {
		t.Errorf("Finish() should print the header, got:\n%s", report)
	}
	if !strings.Contains(report, "First Job") {
		t.Errorf("Finish() should print the report, got:\n%s", report)
	}
}

// TestPrinter_StepStartFallsBackToSpinner covers a printer with no reporter,
// as used by Job.RunIndependently.
func TestPrinter_StepStartFallsBackToSpinner(t *testing.T) {
	p := newBufferPrinter()
	if p.Reporter() != nil {
		t.Fatal("a fresh printer should have no reporter")
	}

	// Must not panic with a nil reporter and a nil spinner.
	p.StepStart("job", "step")
}
