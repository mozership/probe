package display

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"

	"github.com/mozership/probe/internal/outcome"
	"github.com/mozership/probe/style"
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

	r := newStreamReporter(p, NewRenderer(bufferIDs, nil, nil))

	return p, out, errOut, r
}

// newTestResult builds a Result with three jobs in a fixed order: one
// successful, one failed and one skipped.
func newTestResult(base time.Time) *outcome.Run {
	rs := outcome.New()

	rs.Jobs["first"] = &outcome.Job{
		JobID:     "first",
		JobName:   "First Job",
		StartTime: base,
		EndTime:   base.Add(1 * time.Second),
		Status:    outcome.StatusSuccess,
		StepResults: []outcome.Step{
			{Index: 0, Name: "First Step", Status: outcome.StatusSuccess, RT: "100ms"},
		},
	}

	rs.Jobs["second"] = &outcome.Job{
		JobID:     "second",
		JobName:   "Second Job",
		StartTime: base.Add(1 * time.Second),
		EndTime:   base.Add(3 * time.Second),
		Status:    outcome.StatusError,
		StepResults: []outcome.Step{
			{Index: 0, Name: "Second Step", Status: outcome.StatusError},
		},
	}

	rs.Jobs["third"] = &outcome.Job{
		JobID:     "third",
		JobName:   "Third Job",
		StartTime: base.Add(3 * time.Second),
		EndTime:   base.Add(3 * time.Second),
		Status:    outcome.StatusSkipped,
	}

	return rs
}

func TestParseOutputMode(t *testing.T) {
	tests := []struct {
		input   string
		want    Mode
		wantErr bool
	}{
		{"", ModeAuto, false},
		{"auto", ModeAuto, false},
		{"AUTO", ModeAuto, false},
		{"  spinner  ", ModeSpinner, false},
		{"spinner", ModeSpinner, false},
		{"stream", ModeStream, false},
		{"Stream", ModeStream, false},
		{"json", ModeAuto, true},
		{"none", ModeAuto, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMode(tt.input)
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
		mode Mode
		want string
	}{
		{ModeAuto, "auto"},
		{ModeSpinner, "spinner"},
		{ModeStream, "stream"},
		{Mode(99), "auto"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("OutputMode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestOutputMode_resolve(t *testing.T) {
	// Explicit modes are never rewritten by detection.
	if got := ModeSpinner.resolve(); got != ModeSpinner {
		t.Errorf("OutputModeSpinner.resolve() = %v, want spinner", got)
	}
	if got := ModeStream.resolve(); got != ModeStream {
		t.Errorf("OutputModeStream.resolve() = %v, want stream", got)
	}

	// CI never renders a spinner, so auto must stream there.
	t.Setenv("CI", "true")
	if got := ModeAuto.resolve(); got != ModeStream {
		t.Errorf("OutputModeAuto.resolve() under CI = %v, want stream", got)
	}
}

func TestNewReporter(t *testing.T) {
	p := newBufferPrinter()

	if _, ok := NewReporter(ModeSpinner, p, NewRenderer(nil, nil, nil)).(*BufferedReporter); !ok {
		t.Error("newReporter(spinner) should return a *BufferedReporter")
	}
	if _, ok := NewReporter(ModeStream, p, NewRenderer(nil, nil, nil)).(*StreamReporter); !ok {
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
		rs.NotifyJobDone(id)
	}
	r.Finish(rs)

	want := NewRenderer(order, nil, nil).reportText(newTestResult(base))

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
	rs.NotifyJobDone("second")
	if out.Len() != 0 {
		t.Fatalf("job out of order should be held back, got: %q", out.String())
	}

	// "first" unblocks both itself and the queued "second".
	rs.NotifyJobDone("first")
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

	rs.NotifyJobDone("third")
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

	rs.NotifyJobDone("first")
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

	rs.NotifyJobDone("first")
	rs.NotifyJobDone("first")

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

	rs.NotifyJobDone("second")

	if !strings.Contains(out.String(), "Second Job") {
		t.Errorf("a job outside the declared order should still be printed, got: %q", out.String())
	}
}

func TestStreamReporter_ProgressGoesToStderr(t *testing.T) {
	_, out, errOut, r := newTestStreamPrinter([]string{"first"})

	r.StepStart("first", "Calling API")
	r.StepDone("first", outcome.Step{Name: "Calling API", Status: outcome.StatusSuccess, RT: "120ms"})
	r.StepDone("first", outcome.Step{Name: "Checking", Status: outcome.StatusError})
	r.StepDone("first", outcome.Step{Name: "Skipped one", Status: outcome.StatusSkipped})
	r.StepDone("first", outcome.Step{
		Name:          "Polling",
		Status:        outcome.StatusWarning,
		RepeatCounter: &outcome.RepeatCounter{SuccessCount: 2, RepeatTotal: 3},
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

// The line of a step that is done tells its status by its icon, and a
// skipped step is marked in its name as the report marks it.
func TestStreamReporter_StepDoneLines(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	_, _, errOut, r := newTestStreamPrinter([]string{"first"})
	r.StepDone("first", outcome.Step{Name: "get", Status: outcome.StatusSuccess, RT: "12ms"})
	r.StepDone("first", outcome.Step{Name: "post", Status: outcome.StatusError})
	r.StepDone("first", outcome.Step{Name: "sleep", Status: outcome.StatusWarning})
	r.StepDone("first", outcome.Step{Name: "later", Status: outcome.StatusSkipped})
	r.StepDone("first", outcome.Step{Name: "poll", Status: outcome.StatusSkipped, RepeatCounter: &outcome.RepeatCounter{Name: "poll", SkippedCount: 3, RepeatTotal: 3}})
	r.StepDone("first", outcome.Step{Name: "odd", Status: outcome.Status(99)})

	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	want := []string{
		"[first] ✓ get (12ms)",
		"[first] ✗ post",
		"[first] △ sleep",
		"[first] ⏭ later (SKIPPED)",
		"[first] ⏭ poll (0/3 success)",
		"[first] ? odd",
	}
	if len(lines) != len(want) {
		t.Fatalf("progress log lines = %d, want %d:\n%s", len(lines), len(want), errOut.String())
	}
	for i, line := range lines {
		// The line starts with the time, which is left out.
		_, rest, _ := strings.Cut(line, " ")
		if rest != want[i] {
			t.Errorf("line %d = %q, want %q", i, rest, want[i])
		}
	}
}

// TestStreamReporter_ProgressIsLabelledByJobName covers jobs running at
// once, whose lines are told apart by the names of their jobs and line up.
func TestStreamReporter_ProgressIsLabelledByJobName(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	_, _, errOut, r := newTestStreamPrinter([]string{"job_0", "job_1", "e2e"})
	names := map[string]string{"job_0": "Unit", "job_1": "Unit", "e2e": "結合テスト"}
	r.LabelJobs(func(id string) string { return names[id] })

	r.StepStart("job_0", "go test")
	r.StepStart("e2e", "probe")
	r.StepStart("job_1", "go vet")

	lines := strings.Split(strings.TrimSuffix(errOut.String(), "\n"), "\n")
	want := []string{
		"[Unit (job_0)] ▸ go test",
		"[結合テスト]   ▸ probe",
		"[Unit (job_1)] ▸ go vet",
	}
	if len(lines) != len(want) {
		t.Fatalf("progress log lines = %d, want %d:\n%s", len(lines), len(want), errOut.String())
	}
	for i, line := range lines {
		// The line starts with the time, which is left out.
		_, rest, _ := strings.Cut(line, " ")
		if rest != want[i] {
			t.Errorf("line %d = %q, want %q", i, rest, want[i])
		}
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
				rs.AddStepResult(jobID, outcome.Step{Index: i, Name: "step", Status: outcome.StatusSuccess})
			}
			rs.NotifyJobDone(jobID)
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

	r := newBufferedReporter(p, NewRenderer([]string{"first"}, nil, nil))

	rs := newTestResult(time.Now())
	rs.SetReporter(r)

	r.Start("My Workflow", "does things")
	r.StepStart("first", "Calling API")
	rs.NotifyJobDone("first")

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

// The start of a step reaches the reporter through the result, which
// tells nothing when it has no reporter, or when there is no result.
func TestResult_notifyStepStart(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	color.NoColor = true

	_, _, errOut, r := newTestStreamPrinter([]string{"first"})
	rs := outcome.New()
	rs.SetReporter(r)
	rs.NotifyStepStart("first", "Calling API")
	if _, rest, _ := strings.Cut(errOut.String(), " "); rest != "[first] ▸ Calling API\n" {
		t.Errorf("progress = %q, want the step that started", errOut.String())
	}

	// Must not panic without a reporter, or without a result.
	outcome.New().NotifyStepStart("first", "step")
	var none *outcome.Run
	none.NotifyStepStart("first", "step")
}

// newRichTestResult builds a Result whose jobs show what the report puts
// under a step: an echo, a failure, a skipped step and a repeated one, with
// jobs that ran at once and so overlap in time.
func newRichTestResult(base time.Time) *outcome.Run {
	rs := newTestResult(base)
	rs.Jobs["first"].StepResults = append(rs.Jobs["first"].StepResults,
		outcome.Step{Index: 1, Name: "Echoes", Status: outcome.StatusSuccess, Echo: "line 1\n  line 2", HasEcho: true},
		outcome.Step{Index: 2, Name: "Later", Status: outcome.StatusSkipped},
		outcome.Step{Index: 3, Name: "Untested", Status: outcome.StatusWarning},
	)
	rs.Jobs["second"].StartTime = base.Add(-time.Second)
	rs.Jobs["second"].StepResults = []outcome.Step{
		{Index: 0, Name: "Second Step", Status: outcome.StatusError, Test: "res.code == 200", Failure: &outcome.Failure{
			Kind:     outcome.FailureAssertion,
			Message:  "test evaluated to false",
			Request:  map[string]any{"url": "/"},
			Response: map[string]any{"code": 500},
		}},
		{Index: 1, Name: "Polled", Status: outcome.StatusWarning, RepeatCounter: &outcome.RepeatCounter{Name: "Polled", SuccessCount: 2, FailureCount: 1, RepeatTotal: 3, Echoes: []string{"a", "", "c"}}},
	}
	return rs
}

// The buffered and the streamed report are the same bytes on stdout, the
// header and the footer included, whatever order the jobs finish in: both
// render the blocks and add up the totals of the footer the same way.
func TestBufferedAndStreamedReportsAreIdentical(t *testing.T) {
	defer func(was bool) { color.NoColor = was }(color.NoColor)
	order := []string{"first", "second", "third"}
	base := time.Unix(1700000000, 0)

	for _, noColor := range []bool{true, false} {
		color.NoColor = noColor

		buffered := newBufferPrinter()
		br := newBufferedReporter(buffered, NewRenderer(order, nil, nil))
		brs := newRichTestResult(base)
		brs.SetReporter(br)
		br.Start("Workflow", "does things")
		for _, id := range order {
			brs.NotifyJobDone(id)
		}
		br.Finish(brs)

		streamed := newBufferPrinter()
		sr := newStreamReporter(streamed, NewRenderer(order, nil, nil))
		srs := newRichTestResult(base)
		srs.SetReporter(sr)
		sr.Start("Workflow", "does things")
		for _, id := range []string{"second", "third", "first"} {
			srs.NotifyJobDone(id)
		}
		sr.Finish(srs)

		got := streamed.outWriter.(*bytes.Buffer).String()
		want := buffered.outWriter.(*bytes.Buffer).String()
		if got != want {
			t.Errorf("NoColor %v: streamed report differs from the buffered one\ngot:\n%s\nwant:\n%s", noColor, got, want)
		}
		// The second job started a second before the first, and the third
		// ended three seconds after the first started.
		if !strings.Contains(want, "Total workflow time: 4.00s") {
			t.Errorf("NoColor %v: footer should tell the time from the earliest start to the latest end, got:\n%s", noColor, want)
		}
	}
}

// A step's status has one icon and one color, which the report and the
// progress of a run both show it by.
func TestStatusIcon(t *testing.T) {
	tests := []struct {
		status outcome.Status
		icon   string
		color  *color.Color
	}{
		{outcome.StatusSuccess, IconSuccess, colorSuccess()},
		{outcome.StatusError, IconError, colorError()},
		{outcome.StatusWarning, IconTriangle, colorNoTest()},
		{outcome.StatusSkipped, IconSkip, colorInfo()},
		{outcome.Status(99), "?", ColorWarning()},
	}
	for _, tt := range tests {
		icon, c := statusIcon(tt.status)
		if icon != tt.icon || !c.Equals(tt.color) {
			t.Errorf("statusIcon(%d) = %q, %v; want %q, %v", tt.status, icon, c, tt.icon, tt.color)
		}
	}
}

// The icons of the statuses are those of the marks an action prints with,
// so that what it reports under its step looks like the step.
func TestStatusIconsAreMarks(t *testing.T) {
	tests := map[string]style.Mark{
		IconSuccess:  style.Success,
		IconError:    style.Failure,
		IconTriangle: style.NoTest,
		IconSkip:     style.Skipped,
	}
	for icon, m := range tests {
		if want := m.Icon() + " "; icon != want {
			t.Errorf("icon %q, want %q", icon, want)
		}
	}
}

// The stream reporter labels the jobs by their names, which the run tells
// it through JobLabeler.
var _ JobLabeler = (*StreamReporter)(nil)
