package probe

import (
	"bytes"
	"errors"
	"maps"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/notes"
	"github.com/mozership/probe/mask"
	"github.com/mozership/probe/report"
)

// notesRunner answers every action with its own parameters as res, as
// hello does, so that a step leaves the notes its with gives, after
// sleeping for with.sleep when that is set. It records what each step was
// called with, by the name of the step, the last call of it last.
type notesRunner struct {
	mu   sync.Mutex
	with map[string][]map[string]any
}

func (r *notesRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	if s, ok := call.With["sleep"].(string); ok {
		if d, err := time.ParseDuration(s); err == nil {
			time.Sleep(d)
		}
	}
	r.mu.Lock()
	if r.with == nil {
		r.with = map[string][]map[string]any{}
	}
	r.with[call.Step.Name] = append(r.with[call.Step.Name], call.With)
	r.mu.Unlock()
	res := maps.Clone(call.With)
	return map[string]any{"req": call.With, "res": res, "status": 0}, nil, nil
}

// calls returns what the step called name was called with, each time.
func (r *notesRunner) calls(name string) []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.with[name]
}

func note(level, title, body string) map[string]any {
	return map[string]any{"level": level, "title": title, "body": body}
}

func TestNoteBook(t *testing.T) {
	m := mask.New([]string{"CARD"}, map[string]string{"CARD": "4111111111111111"})
	book := newNoteBook([]string{"a", "b"}, func() *mask.Masker { return m })

	if got := book.snapshot(); got == nil || len(got) != 0 {
		t.Fatalf("snapshot() of no notes = %#v, want an empty list", got)
	}

	// Job b ends first; its notes still come after those of a.
	book.record("b", "Job B", StepResult{Index: 0, Name: "b0", Status: StatusError,
		notes: []notes.Note{{Level: notes.Warning, Body: "paid with 4111111111111111"}}})
	book.record("a", "Job A", StepResult{Index: 1, Name: "a1", Status: StatusSuccess, HasTest: true,
		notes: []notes.Note{{Level: notes.Critical, Title: "t1", Body: "b1"}, {Level: notes.Info, Body: "b2"}}})
	// A step with no notes, and a skipped one, leave none.
	book.record("a", "Job A", StepResult{Index: 2, Name: "a2", Status: StatusSuccess})
	book.record("a", "Job A", StepResult{Index: 3, Name: "a3", Status: StatusSkipped})
	// A repeated step's notes tell the run they are of.
	book.record("a", "Job A", StepResult{Index: 4, Name: "a4", Status: StatusWarning,
		RepeatCounter: &StepRepeatCounter{SuccessCount: 1, FailureCount: 1, notesRun: 2},
		notes:         []notes.Note{{Level: notes.Info, Body: "b4"}}})

	want := []any{
		map[string]any{"job": "Job A", "job_id": "a", "step": "a1", "index": 1, "status": "passed", "level": "critical", "title": "t1", "body": "b1"},
		map[string]any{"job": "Job A", "job_id": "a", "step": "a1", "index": 1, "status": "passed", "level": "info", "title": "", "body": "b2"},
		map[string]any{"job": "Job A", "job_id": "a", "step": "a4", "index": 4, "status": "failed", "level": "info", "title": "", "body": "b4", "run": 2},
		map[string]any{"job": "Job B", "job_id": "b", "step": "b0", "index": 0, "status": "failed", "level": "warning", "title": "", "body": "paid with <secret:CARD>"},
	}
	got := book.snapshot()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot() =\n%#v\nwant\n%#v", got, want)
	}

	// A step that changes the entries it read does not change those of
	// another.
	got[0].(map[string]any)["body"] = "changed"
	if again := book.snapshot(); !reflect.DeepEqual(again, want) {
		t.Errorf("snapshot() after a change to an earlier one = %#v, want it as before", again)
	}
}

// A value the masker learns after the notes were kept is hidden in them
// from then on.
func TestNoteBookMasksWhatIsLearnedLater(t *testing.T) {
	m := mask.New(nil, nil)
	book := newNoteBook([]string{"a"}, func() *mask.Masker { return m })
	book.record("a", "A", StepResult{Name: "s", Status: StatusSuccess,
		notes: []notes.Note{{Level: notes.Info, Title: "sent Bearer a-token-learned-at-run-time", Body: "Bearer a-token-learned-at-run-time"}}})

	if body := book.snapshot()[0].(map[string]any)["body"]; body != "Bearer a-token-learned-at-run-time" {
		t.Fatalf("body before the token is learned = %v", body)
	}
	m.Learn(map[string]any{"headers": map[string]any{"Authorization": "Bearer a-token-learned-at-run-time"}})
	entry := book.snapshot()[0].(map[string]any)
	if body := entry["body"].(string); strings.Contains(body, "a-token") {
		t.Errorf("body after the token is learned = %q, want it hidden", body)
	}
	if title := entry["title"].(string); strings.Contains(title, "a-token") {
		t.Errorf("title after the token is learned = %q, want it hidden", title)
	}
}

func TestNoteBookNil(t *testing.T) {
	var book *noteBook
	book.record("a", "A", StepResult{notes: []notes.Note{{Level: notes.Info, Body: "b"}}})
	if got := book.snapshot(); got == nil || len(got) != 0 {
		t.Errorf("nil snapshot() = %#v, want an empty list", got)
	}
}

// notesWorkflow has a job whose step passes and leaves a critical and an
// info note, after a while, a job whose step fails and leaves a warning
// note at once, and a last job that needs both, with always, whose steps
// read the notes.
func notesWorkflow() *Workflow {
	return &Workflow{
		Name:    "Notes",
		Secrets: []string{"PROBE_TEST_CARD"},
		Jobs: []Job{
			{
				ID: "checkout", Name: "Checkout",
				Steps: []*Step{{
					Name: "Buy", Uses: "hello", Test: "status == 0",
					With: map[string]any{
						"sleep": "100ms",
						"notes": []any{
							note("critical", "Shipping form", "Rejects a valid ZIP."),
							map[string]any{"level": "info", "body": "Done."},
						},
					},
				}},
			},
			{
				ID: "persona", Name: "Persona",
				Steps: []*Step{{
					Name: "Shopper", Uses: "hello", Test: "false",
					With: map[string]any{"notes": []any{
						note("warning", "Paid", "Paid with 4111111111111111."),
						note("high", "Not a level", "Left out."),
					}},
				}},
			},
			{
				ID: "report", Name: "Report", Needs: []string{"checkout", "persona"}, Always: true,
				Steps: []*Step{
					// The notes are given as got, not as notes, or the step
					// would leave them as its own.
					{Name: "All", Uses: "hello", With: map[string]any{"got": "{{ notes }}"}},
					{
						Name: "Critical", Uses: "hello",
						Iteration: Iteration{Expr: "{{ filter(notes, .level == 'critical') }}"},
						With:      map[string]any{"title": "{{ vars.title }}", "step": "{{ vars.step }}"},
					},
					{Name: "Count", Uses: "hello", Test: `len(notes) == 3 && notes[2].status == "failed"`},
				},
			},
		},
		printer: newBufferPrinter(),
	}
}

func TestNotesReachLaterSteps(t *testing.T) {
	t.Setenv("PROBE_TEST_CARD", "4111111111111111")
	w := notesWorkflow()
	runner := &notesRunner{}
	r := startWithReport(t, w, runner)

	all := runner.calls("All")
	if len(all) != 1 {
		t.Fatalf("All was called %d times, want once", len(all))
	}
	want := []any{
		map[string]any{"job": "Checkout", "job_id": "checkout", "step": "Buy", "index": 0, "status": "passed", "level": "critical", "title": "Shipping form", "body": "Rejects a valid ZIP."},
		map[string]any{"job": "Checkout", "job_id": "checkout", "step": "Buy", "index": 0, "status": "passed", "level": "info", "title": "", "body": "Done."},
		map[string]any{"job": "Persona", "job_id": "persona", "step": "Shopper", "index": 0, "status": "failed", "level": "warning", "title": "Paid", "body": "Paid with <secret:PROBE_TEST_CARD>."},
	}
	if got := all[0]["got"]; !reflect.DeepEqual(got, want) {
		t.Errorf("with.got =\n%#v\nwant\n%#v", got, want)
	}

	critical := runner.calls("Critical")
	if len(critical) != 1 || critical[0]["title"] != "Shipping form" || critical[0]["step"] != "Buy" {
		t.Errorf("Critical was called with %v, want once with the critical note's fields", critical)
	}

	if st := reportStep(t, r, "report", "Count"); st.Status != "passed" {
		t.Errorf("Count = %s, want it to pass: its test reads the notes", st.Status)
	}
	if st := reportStep(t, r, "checkout", "Buy"); st.Status != "passed" {
		t.Errorf("Buy = %s, want it to pass", st.Status)
	}

	stderr := w.printer.errWriter.(*bytes.Buffer).String()
	if !strings.Contains(stderr, `step 0 "Shopper": notes[1]: level "high" is not one of info, warning or critical; it is left out of the notes`) {
		t.Errorf("stderr = %q, want a warning of the note left out", stderr)
	}
}

// A step of a repeated job leaves the notes of its first failing run, or
// of its last when none failed, telling the run without the warmup runs.
// The steps of the job do not see them; the jobs after it do.
func TestNotesOfARepeatedJob(t *testing.T) {
	w := &Workflow{
		Name: "Repeated notes",
		Jobs: []Job{
			{
				ID: "smoke", Name: "Smoke",
				Repeat: &Repeat{Count: 5, Warmup: 1},
				Steps: []*Step{
					{
						Name: "Fails in 3 and 4", Uses: "hello", Test: "repeat_index != 3 && repeat_index != 4",
						With: map[string]any{
							"notes": []any{note("info", "", "run {{ repeat_index }}"), map[string]any{"level": "bad", "body": "b"}},
						},
					},
					{
						Name: "Passes", Uses: "hello", Test: "true",
						With: map[string]any{"notes": []any{note("info", "", "run {{ repeat_index }}")}, "seen": "{{ len(notes) }}"},
					},
				},
			},
			{
				ID: "after", Name: "After", Needs: []string{"smoke"}, Always: true,
				Steps: []*Step{{Name: "Read", Uses: "hello", With: map[string]any{"got": "{{ notes }}"}}},
			},
		},
		printer: newBufferPrinter(),
	}
	runner := &notesRunner{}
	startWithReport(t, w, runner)

	for _, c := range runner.calls("Passes") {
		if c["seen"] != 0 {
			t.Errorf("a step of the repeated job saw %v notes, want 0", c["seen"])
		}
	}

	read := runner.calls("Read")
	if len(read) != 1 {
		t.Fatalf("Read was called %d times, want once", len(read))
	}
	got, _ := read[0]["got"].([]any)
	if len(got) != 2 {
		t.Fatalf("notes = %#v, want 2", got)
	}
	first := got[0].(map[string]any)
	if first["body"] != "run 3" || first["run"] != 3 || first["status"] != "failed" {
		t.Errorf("notes of the failing step = %v, want those of run 3", first)
	}
	second := got[1].(map[string]any)
	if second["body"] != "run 5" || second["run"] != 5 || second["status"] != "passed" {
		t.Errorf("notes of the passing step = %v, want those of run 5", second)
	}

	stderr := w.printer.errWriter.(*bytes.Buffer).String()
	if n := strings.Count(stderr, `level "bad"`); n != 1 {
		t.Errorf("stderr warns of the bad note %d times, want once:\n%s", n, stderr)
	}
}

// A workflow whose actions leave no notes reads an empty list.
func TestNotesNone(t *testing.T) {
	w := &Workflow{
		Name: "No notes",
		Jobs: []Job{{ID: "a", Name: "A", Steps: []*Step{
			{Name: "One", Uses: "hello", Test: "true"},
			{Name: "Two", Uses: "hello", Test: "notes != nil && len(notes) == 0"},
		}}},
		printer: newBufferPrinter(),
	}
	r := startWithReport(t, w, &notesRunner{})
	if st := reportStep(t, r, "a", "Two"); st.Status != "passed" {
		t.Errorf("Two = %s, want it to pass", st.Status)
	}
}

// reportStep returns the step called name of the job of jobID in r.
func reportStep(t *testing.T, r report.Report, jobID, name string) report.Step {
	t.Helper()
	for _, job := range r.Jobs {
		if job.ID != jobID {
			continue
		}
		for _, st := range job.Steps {
			if st.Name == name {
				return st
			}
		}
	}
	t.Fatalf("no step %q in the job %q of the report", name, jobID)
	return report.Step{}
}

// A step keeps the notes of its first failing run, and until one fails,
// those of the last run by number, whatever order the runs end in.
func TestStepRepeatCounterKeepNotes(t *testing.T) {
	n := func(body string) []notes.Note { return []notes.Note{{Level: notes.Info, Body: body}} }
	var c StepRepeatCounter
	for _, r := range []struct {
		run    int
		failed bool
	}{{3, false}, {1, false}, {2, false}} {
		c.keepNotes(n("run"), r.run, r.failed)
	}
	if c.notesRun != 3 {
		t.Errorf("notesRun after runs 3, 1, 2 passed = %d, want 3", c.notesRun)
	}

	// The first failure is kept whatever its number: the step's failure is
	// of the run that failed first.
	c.keepNotes(n("failed"), 2, true)
	if c.notesRun != 2 || c.notes[0].Body != "failed" {
		t.Errorf("notes after run 2 failed = run %d %v, want those of run 2", c.notesRun, c.notes)
	}
}

// The runs of an async job end in any order; the notes are still those of
// the last run by number.
func TestNotesOfAnAsyncRepeatedJob(t *testing.T) {
	w := &Workflow{
		Name: "Async notes",
		Jobs: []Job{
			{
				ID: "smoke", Name: "Smoke",
				Repeat: &Repeat{Count: 3, Async: true, Interval: Interval{Duration: time.Millisecond}},
				Steps: []*Step{{
					Name: "Search", Uses: "hello", Test: "true",
					With: map[string]any{
						// The first run ends last.
						"sleep": "{{ string((3 - repeat_index) * 60) + 'ms' }}",
						"notes": []any{note("info", "", "run {{ repeat_index + 1 }}")},
					},
				}},
			},
			{
				ID: "after", Name: "After", Needs: []string{"smoke"},
				Steps: []*Step{{Name: "Read", Uses: "hello", With: map[string]any{"got": "{{ notes }}"}}},
			},
		},
		printer: newBufferPrinter(),
	}
	runner := &notesRunner{}
	startWithReport(t, w, runner)

	read := runner.calls("Read")
	if len(read) != 1 {
		t.Fatalf("Read was called %d times, want once", len(read))
	}
	got, _ := read[0]["got"].([]any)
	if len(got) != 1 {
		t.Fatalf("notes = %#v, want 1", got)
	}
	if e := got[0].(map[string]any); e["body"] != "run 3" || e["run"] != 3 {
		t.Errorf("notes = %v, want those of run 3", e)
	}
}

// flakyRunner answers the first call of each step as notesRunner does, so
// that the step leaves the notes its with gives, and fails every later
// call of it with an error, as an action that stops answering does.
type flakyRunner struct {
	notesRunner
	mu       sync.Mutex
	attempts map[string]int
}

func (r *flakyRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	if _, ok := call.With["flaky"]; ok {
		r.mu.Lock()
		if r.attempts == nil {
			r.attempts = map[string]int{}
		}
		r.attempts[call.Step.Name]++
		n := r.attempts[call.Step.Name]
		r.mu.Unlock()
		if n > 1 {
			return nil, nil, errors.New("connection refused")
		}
	}
	return r.notesRunner.Run(name, call, opts)
}

// A step whose last attempt ended in an error of its action leaves no
// notes, though an earlier attempt returned some: the action of the last
// attempt returned none. So does such a run of a repeated job.
func TestNotesOfAStepWhoseActionFailed(t *testing.T) {
	flaky := map[string]any{"flaky": true, "notes": []any{note("critical", "", "from the first attempt")}}
	w := &Workflow{
		Name: "Failed action",
		Jobs: []Job{
			{
				ID: "retried", Name: "Retried",
				Steps: []*Step{{
					Name: "Retried", Uses: "hello", Test: "false", With: flaky,
					Retry: &StepRetry{MaxAttempts: 2, Interval: Interval{Duration: time.Millisecond}},
				}},
			},
			{
				ID: "repeated", Name: "Repeated",
				Repeat: &Repeat{Count: 2},
				Steps: []*Step{{
					Name: "Repeated", Uses: "hello", Test: "true", With: flaky,
				}},
			},
			{
				ID: "after", Name: "After", Needs: []string{"retried", "repeated"}, Always: true,
				Steps: []*Step{{Name: "Read", Uses: "hello", With: map[string]any{"got": "{{ notes }}"}}},
			},
		},
		printer: newBufferPrinter(),
	}
	runner := &flakyRunner{}
	r := startWithReport(t, w, runner)

	if st := reportStep(t, r, "retried", "Retried"); st.Failure == nil || st.Failure.Kind != FailureAction {
		t.Fatalf("Retried = %+v, want it to fail with an action error", st)
	}
	read := runner.calls("Read")
	if len(read) != 1 {
		t.Fatalf("Read was called %d times, want once", len(read))
	}
	// The repeated step's notes are those of its first failing run, its
	// second, an action error, which has none; its first run's are not
	// kept in their place.
	if got, _ := read[0]["got"].([]any); got == nil || len(got) != 0 {
		t.Errorf("notes = %#v, want none", got)
	}
}
