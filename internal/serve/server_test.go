package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/internal/cron"
)

// logBuffer is what a server logs, safe for the goroutines of its runs.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// lines returns the lines logged, each as the JSON object it is.
func (b *logBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("a line of the log is not JSON: %s", sc.Text())
		}
		out = append(out, line)
	}
	return out
}

// runs returns the lines logged of runs, by msg run.
func (b *logBuffer) runs(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range b.lines(t) {
		if l["msg"] == "run" {
			out = append(out, l)
		}
	}
	return out
}

func newTestServer(t *testing.T, exe string, workflows map[string]*Workflow, grace time.Duration) (*Server, *logBuffer) {
	t.Helper()
	c := &Config{Workers: 2, Results: Results{Dir: t.TempDir()}, Grace: Duration{Duration: grace}, Workflows: workflows}
	log := new(logBuffer)
	return New(c, exe, slog.New(slog.NewJSONHandler(log, nil))), log
}

// eventually waits for cond to hold, for up to ten seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A loop runs its workflow again once the run before it is over, and each
// run is a line of the log, with where it left its report and output.
func TestServer_Loop(t *testing.T) {
	exe := fakeProbeExe(t, 0, 0)
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"intake": {Files: []string{"/w.yml"}, Loop: true, Interval: Duration{Duration: 50 * time.Millisecond}},
	}, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Serve(ctx)
		close(done)
	}()
	eventually(t, "three runs", func() bool { return len(log.runs(t)) >= 3 })
	cancel()
	<-done

	for _, r := range log.runs(t) {
		if r["workflow"] != "intake" || r["status"] != StatusPassed || r["exit_status"] != float64(0) || r["level"] != "INFO" {
			t.Errorf("run = %v", r)
		}
		for _, key := range []string{"report", "output"} {
			if _, err := os.Stat(r[key].(string)); err != nil {
				t.Errorf("%s: %v", key, err)
			}
		}
	}
	lines := log.lines(t)
	if lines[0]["msg"] != "serving" || lines[len(lines)-1]["msg"] != "stopped" {
		t.Errorf("log = %v", lines)
	}
}

// A workflow is not started while a run of it is going, which the log says;
// a failed run is logged as an error.
func TestServer_TriggerSkipsWhileRunning(t *testing.T) {
	exe := fakeProbeExe(t, 500*time.Millisecond, 1)
	w := &Workflow{Files: []string{"/w.yml"}}
	s, log := newTestServer(t, exe, map[string]*Workflow{"w": w}, time.Second)

	done, ok := s.trigger("w", w)
	if !ok {
		t.Fatal("the first trigger started no run")
	}
	if _, ok := s.trigger("w", w); ok {
		t.Error("a second run started while the first was going")
	}
	<-done
	if _, ok := s.trigger("w", w); !ok {
		t.Error("no run started once the first was over")
	}
	s.wg.Wait()

	var skipped int
	for _, l := range log.lines(t) {
		if l["msg"] == "run skipped: the run before it is still going" {
			skipped++
		}
	}
	runs := log.runs(t)
	if skipped != 1 || len(runs) != 2 || runs[0]["status"] != StatusFailed || runs[0]["level"] != "ERROR" {
		t.Errorf("skipped = %d, runs = %v", skipped, runs)
	}
}

// Stopped, serve waits for the runs going, up to the grace, and then stops
// them.
func TestServer_StopWaitsForRunsWithinGrace(t *testing.T) {
	tests := []struct {
		name   string
		sleep  time.Duration
		grace  time.Duration
		status string
	}{
		{"a run over within the grace", 500 * time.Millisecond, 10 * time.Second, StatusPassed},
		{"a run longer than the grace", time.Minute, 300 * time.Millisecond, StatusStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe := fakeProbeExe(t, tt.sleep, 0)
			s, log := newTestServer(t, exe, map[string]*Workflow{
				"w": {Files: []string{"/w.yml"}, Loop: true, Interval: Duration{Duration: time.Hour}},
			}, tt.grace)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				s.Serve(ctx)
				close(done)
			}()
			eventually(t, "the run to start", func() bool { return s.busy["w"].Load() })
			time.Sleep(100 * time.Millisecond)
			cancel()
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("serve did not stop")
			}
			runs := log.runs(t)
			if len(runs) != 1 || runs[0]["status"] != tt.status {
				t.Errorf("runs = %v, want one %s", runs, tt.status)
			}
		})
	}
}

// Abort stops the runs going without waiting for the grace.
func TestServer_Abort(t *testing.T) {
	exe := fakeProbeExe(t, time.Minute, 0)
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"w": {Files: []string{"/w.yml"}, Loop: true},
	}, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Serve(ctx)
		close(done)
	}()
	eventually(t, "the run to start", func() bool { return s.busy["w"].Load() })
	time.Sleep(100 * time.Millisecond)
	cancel()
	s.Abort()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("serve did not stop")
	}
	if runs := log.runs(t); len(runs) != 1 || runs[0]["status"] != StatusStopped {
		t.Errorf("runs = %v", runs)
	}
}

// A workflow with a schedule is run at the times it matches.
func TestServer_Schedule(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the next minute")
	}
	exe := fakeProbeExe(t, 0, 0)
	schedule, err := cron.Parse("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"w": {Files: []string{"/w.yml"}, Schedule: "* * * * *", schedule: schedule},
	}, time.Second)
	ctx := t.Context()
	go s.Serve(ctx)
	deadline := time.Now().Add(70 * time.Second)
	for len(log.runs(t)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no run at the start of the minute")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if started := log.runs(t)[0]["started_at"].(string); started[17:19] != "00" {
		t.Errorf("started at %s, want the start of a minute", started)
	}
}

// Prune removes the runs last written Keep ago, and leaves the others and
// what is not a run.
func TestServer_Prune(t *testing.T) {
	s, _ := newTestServer(t, "", map[string]*Workflow{"w": {Files: []string{"/w.yml"}}}, time.Second)
	s.config.Results.Keep = Duration{Duration: time.Hour}
	dir := filepath.Join(s.config.Results.Dir, "w")
	old := filepath.Join(dir, "20261101T000000.000000Z-1")
	recent := filepath.Join(dir, "20261126T000000.000000Z-2")
	other := filepath.Join(dir, "notes")
	for _, d := range []string{old, recent, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "output.log"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for _, d := range []string{old, other} {
		for _, p := range []string{d, filepath.Join(d, "output.log")} {
			if err := os.Chtimes(p, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
	}
	going := filepath.Join(dir, "20261102T000000.000000Z-3")
	if err := os.MkdirAll(going, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(going, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.runner.active = map[string]int{going: 0}
	s.prune(now)
	for d, kept := range map[string]bool{old: false, recent: true, other: true, going: true} {
		if _, err := os.Stat(d); (err == nil) != kept {
			t.Errorf("%s kept = %v, want %v", filepath.Base(d), err == nil, kept)
		}
	}
}

// A run waiting for a worker when serve is stopped is not run.
func TestServer_StopStartsNoWaitingRun(t *testing.T) {
	exe := fakeProbeExe(t, 500*time.Millisecond, 0)
	a, b := &Workflow{Files: []string{"/a.yml"}}, &Workflow{Files: []string{"/b.yml"}}
	s, log := newTestServer(t, exe, map[string]*Workflow{"a": a, "b": b}, 10*time.Second)
	s.config.Workers = 1
	s.workers = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Serve(ctx)
		close(done)
	}()
	if _, ok := s.trigger("a", a); !ok {
		t.Fatal("a did not start")
	}
	eventually(t, "a to take the worker", func() bool { return len(s.workers) == 1 })
	if _, ok := s.trigger("b", b); !ok {
		t.Fatal("b was not triggered")
	}
	cancel()
	<-done
	runs := log.runs(t)
	if len(runs) != 1 || runs[0]["workflow"] != "a" {
		t.Errorf("runs = %v, want a alone", runs)
	}
}

// A loop whose runs do not pass waits longer after each: one that cannot
// run its workflow, and one whose target is down.
func TestServer_LoopBacksOffRunsInError(t *testing.T) {
	s, log := newTestServer(t, "/nonexistent/probe", map[string]*Workflow{
		"w": {Files: []string{"/w.yml"}, Loop: true},
	}, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	s.Serve(ctx)
	runs := log.runs(t)
	if len(runs) < 2 || len(runs) > 3 {
		t.Errorf("runs = %d in 2.5s, want 2 or 3 with a wait of 1s and then 2s", len(runs))
	}
	for _, r := range runs {
		if r["status"] != StatusError || r["error"] == nil {
			t.Errorf("run = %v", r)
		}
		if _, ok := r["report"]; ok {
			t.Errorf("a run without a report is told with one: %v", r)
		}
	}
}

func TestServer_LoopBacksOffFailedRuns(t *testing.T) {
	exe := fakeProbeExe(t, 0, 3)
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"w": {Files: []string{"/w.yml"}, Loop: true},
	}, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	s.Serve(ctx)
	runs := log.runs(t)
	if len(runs) < 2 || len(runs) > 3 {
		t.Errorf("runs = %d in 2.5s, want 2 or 3 with a wait of 1s and then 2s", len(runs))
	}
	for _, r := range runs {
		if r["status"] != StatusFailed {
			t.Errorf("run = %v", r)
		}
	}
}

// forEachServer returns a server whose workflow source gives items, each
// handled by verify and then by ack or dlq, and starts it.
func forEachServer(t *testing.T, outputs string, concurrency int) (*logBuffer, func()) {
	t.Helper()
	return forEachServerWith(t, outputs, concurrency, 100*time.Millisecond, time.Hour)
}

// forEachServerWith is forEachServer with each run taking sleep, and the
// loop of the source waiting interval between its runs.
func forEachServerWith(t *testing.T, outputs string, concurrency int, sleep, interval time.Duration) (*logBuffer, func()) {
	t.Helper()
	exe := fakeProbeExe(t, sleep, 0)
	t.Setenv(fakeOutputsEnv, outputs)
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"source": {Files: []string{"/source.yml"}, Loop: true, Interval: Duration{Duration: interval},
			ForEach: &ForEach{Items: "messages", Workflow: "verify", Concurrency: concurrency, OnSuccess: "ack", OnFailure: "dlq"}},
		"verify": {Files: []string{"/verify.yml"}, Vars: map[string]any{"queue": "orders"}},
		"ack":    {Files: []string{"/ack.yml"}},
		"dlq":    {Files: []string{"/dlq.yml"}},
	}, 10*time.Second)
	s.config.Workers = 1
	s.workers = make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Serve(ctx)
		close(done)
	}()
	return log, func() {
		cancel()
		<-done
	}
}

func byWorkflow(runs []map[string]any) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, r := range runs {
		w := r["workflow"].(string)
		out[w] = append(out[w], r)
	}
	return out
}

// Each item a run gives is handled by a run of the workflow of for_each,
// with the item as its vars over those of the serve file, and then by a run
// of on_success or on_failure as it passed or not, each logged with the run
// and the item it came from. One worker does: a run holds it only while it
// goes.
func TestServer_ForEach(t *testing.T) {
	log, stop := forEachServer(t, `{"messages": [{"id": "a"}, {"id": "b", "exit": 1}, {"id": "c", "body": {"n": 1}}]}`, 2)
	eventually(t, "the items to be handled", func() bool {
		w := byWorkflow(log.runs(t))
		return len(w["ack"]) == 2 && len(w["dlq"]) == 1
	})
	stop()

	w := byWorkflow(log.runs(t))
	if len(w["source"]) != 1 || len(w["verify"]) != 3 {
		t.Fatalf("runs = %v", w)
	}
	source := w["source"][0]["run"]
	verified := map[float64]map[string]any{}
	for _, r := range w["verify"] {
		if r["from"] != source {
			t.Errorf("verify = %v, want it from %s", r, source)
		}
		verified[r["item"].(float64)] = r
	}
	if verified[1]["status"] != StatusFailed || verified[0]["status"] != StatusPassed {
		t.Errorf("verify = %v", verified)
	}
	for _, r := range append(w["ack"], w["dlq"]...) {
		item := r["item"].(float64)
		if r["from"] != source || r["for"] != verified[item]["run"] {
			t.Errorf("%s = %v, want it for the run of item %v", r["workflow"], r, item)
		}
	}
	if w["dlq"][0]["item"] != float64(1) {
		t.Errorf("dlq = %v, want item 1", w["dlq"][0])
	}

	fr := fakeReportAt(t, verified[2]["report"].(string))
	if fr.Vars["id"] != "c" || fr.Vars["queue"] != "orders" || fr.Vars["body"] != "map[n:1]" {
		t.Errorf("vars of item 2 = %v", fr.Vars)
	}
}

// A run is over once the items it gave are, so a loop takes no more than it
// handles.
func TestServer_ForEachHoldsTheSource(t *testing.T) {
	log, stop := forEachServer(t, `{"messages": [{"id": "a"}, {"id": "b"}]}`, 1)
	eventually(t, "the items to be handled", func() bool { return len(byWorkflow(log.runs(t))["ack"]) == 2 })
	stop()
	w := byWorkflow(log.runs(t))
	if len(w["source"]) != 1 {
		t.Errorf("source ran %d times", len(w["source"]))
	}
	// With a concurrency of 1, an item starts once the one before it is
	// handled.
	if at(t, w["verify"][1], "started_at").Before(at(t, w["ack"][0], "finished_at")) {
		t.Errorf("item 1 started before item 0 was acknowledged: %v", w)
	}
}

// A run that passed but gave no list of objects is logged as an error, and
// gives no items.
func TestServer_ForEachWithoutItems(t *testing.T) {
	tests := []struct{ outputs, want string }{
		{`{"other": []}`, "the run gives no output messages"},
		{`{"messages": "a"}`, "output messages is to be a list of objects, the items, not string"},
		{`{"messages": ["a"]}`, "item 0 of output messages is to be an object"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			log, stop := forEachServer(t, tt.outputs, 1)
			eventually(t, "the source to run", func() bool { return len(log.runs(t)) > 0 })
			stop()
			runs := log.runs(t)
			if len(runs) != 1 || runs[0]["status"] != StatusError || !strings.Contains(runs[0]["error"].(string), tt.want) {
				t.Errorf("runs = %v", runs)
			}
		})
	}
}

// An empty list is no items, and a passed run.
func TestServer_ForEachOfNoItems(t *testing.T) {
	log, stop := forEachServer(t, `{"messages": []}`, 1)
	eventually(t, "the source to run", func() bool { return len(log.runs(t)) > 0 })
	stop()
	if runs := log.runs(t); len(runs) != 1 || runs[0]["status"] != StatusPassed {
		t.Errorf("runs = %v", runs)
	}
}

func fakeReportAt(t *testing.T, path string) fakeReport {
	t.Helper()
	return readFakeReport(t, Run{Dir: filepath.Dir(path)})
}

// Once serve is stopped, no item starts and nothing more is run for one
// handled: those left are given again by the queue.
func TestServer_ForEachStopped(t *testing.T) {
	log, stop := forEachServerWith(t, `{"messages": [{"id": "a"}, {"id": "b"}, {"id": "c"}]}`, 1, time.Second, time.Hour)
	eventually(t, "the first item to start", func() bool {
		for _, l := range log.lines(t) {
			if l["msg"] == "run" && l["workflow"] == "source" {
				return true
			}
		}
		return false
	})
	stop()
	w := byWorkflow(log.runs(t))
	if len(w["verify"]) > 1 || len(w["ack"]) != 0 {
		t.Errorf("runs after the stop = %v", w)
	}
}

func at(t *testing.T, line map[string]any, key string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, line[key].(string))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A loop backs off when none of the items of its runs is handled, as their
// on_failure fails too, though the runs that gave them passed; an item that
// fails alone among others that are handled does not slow the loop.
func TestServer_ForEachBacksOffItemsNotHandled(t *testing.T) {
	tests := []struct {
		name, outputs string
		backoff       bool
	}{
		{"no item handled", `{"messages": [{"id": "a", "exit": 1}]}`, true},
		{"an item handled", `{"messages": [{"id": "a", "exit": 1}, {"id": "b"}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log, stop := forEachServerWith(t, tt.outputs, 1, 0, 0)
			eventually(t, "the source to run again", func() bool { return len(byWorkflow(log.runs(t))["source"]) >= 2 })
			stop()
			w := byWorkflow(log.runs(t))
			second := at(t, w["source"][1], "started_at")
			var last time.Time
			for _, r := range append(w["ack"], w["dlq"]...) {
				if end := at(t, r, "finished_at"); end.Before(second) && end.After(last) {
					last = end
				}
			}
			if gap := second.Sub(last); (gap >= firstBackoff-50*time.Millisecond) != tt.backoff {
				t.Errorf("the source waited %s after its items, want a backoff: %v", gap, tt.backoff)
			}
		})
	}
}

// An item whose vars cannot be given as it holds them is skipped, and
// logged as an error; the other items are handled.
func TestServer_ForEachSkipsItems(t *testing.T) {
	log, stop := forEachServer(t, `{"messages": [{"id": "a"}, {" id": "b", "id": "c"}, {"": "d"}, {"receipt": "x-<secret:PIN>"}, {"n": {"k": ["<secret:PIN>"]}}, {"token": "<redacted>"}, {"<secret:K>": "v"}]}`, 1)
	eventually(t, "the items to be handled", func() bool {
		return len(byWorkflow(log.runs(t))["ack"]) == 1 && len(logged(t, log, "item skipped")) == 6
	})
	stop()
	var skipped []string
	for _, l := range log.lines(t) {
		if l["msg"] == "item skipped" {
			skipped = append(skipped, fmt.Sprint(l["item"], ": ", l["error"]))
		}
	}
	slices.Sort(skipped)
	want := []string{
		"1: the item has the key id twice, once the white space around them is left out",
		"2: a key of the item is empty, which no var can be named",
		"3: receipt holds a secret or a credential, which the report masks, so the run would be given what hides it",
		"4: n holds a secret or a credential, which the report masks, so the run would be given what hides it",
		"5: token holds a secret or a credential, which the report masks, so the run would be given what hides it",
		"6: <secret:K> holds a secret or a credential, which the report masks, so the run would be given what hides it",
	}
	if !reflect.DeepEqual(skipped, want) {
		t.Errorf("skipped =\n%q\nwant\n%q", skipped, want)
	}
	if w := byWorkflow(log.runs(t)); len(w["verify"]) != 1 {
		t.Errorf("verify ran %d times, want once", len(w["verify"]))
	}
}

// stateServer returns a server whose loop source, with the state cursor,
// gives the outputs of the fake, and items handled by verify and ack, each
// run of which exits with its var exit; and the path of its state.
func stateServer(t *testing.T, outputs string) (*Server, *logBuffer, string) {
	t.Helper()
	exe := fakeProbeExe(t, 0, 0)
	t.Setenv(fakeOutputsEnv, outputs)
	s, log := newTestServer(t, exe, map[string]*Workflow{
		"source": {Files: []string{"/source.yml"}, Loop: true, Interval: Duration{Duration: 50 * time.Millisecond}, State: []string{"cursor", "seen"},
			ForEach: &ForEach{Items: "messages", Workflow: "verify", Concurrency: 1, OnSuccess: "ack"}},
		"verify": {Files: []string{"/verify.yml"}},
		"ack":    {Files: []string{"/ack.yml"}},
	}, 10*time.Second)
	return s, log, s.statePath("source")
}

// serveUntil serves s until cond holds of its log.
func serveUntil(t *testing.T, s *Server, what string, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Serve(ctx)
		close(done)
	}()
	eventually(t, what, cond)
	cancel()
	<-done
}

func sourceRuns(t *testing.T, log *logBuffer) []map[string]any {
	t.Helper()
	return byWorkflow(log.runs(t))["source"]
}

func logged(t *testing.T, log *logBuffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range log.lines(t) {
		if l["msg"] == msg {
			out = append(out, l)
		}
	}
	return out
}

// The state a run gives is saved once its items are handled, and the next
// run is given it as vars; the first run, with none yet, is given nothing.
func TestServer_State(t *testing.T) {
	s, log, path := stateServer(t, `{"cursor": 7, "messages": [{"id": "a"}]}`)
	serveUntil(t, s, "two runs of the source", func() bool { return len(sourceRuns(t, log)) >= 2 })

	runs := sourceRuns(t, log)
	first := fakeReportAt(t, runs[0]["report"].(string))
	if _, ok := first.Vars["cursor"]; ok {
		t.Errorf("the first run was given a cursor: %v", first.Vars)
	}
	second := fakeReportAt(t, runs[1]["report"].(string))
	if second.Vars["cursor"] != "7" {
		t.Errorf("the second run was given %v, want cursor 7", second.Vars)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil || !reflect.DeepEqual(state, map[string]any{"cursor": float64(7)}) {
		t.Errorf("state = %s, %v", data, err)
	}

	if saved := logged(t, log, "state saved"); len(saved) == 0 || !reflect.DeepEqual(saved[0]["state"], []any{"cursor"}) {
		t.Errorf("state saved = %v", saved)
	}
}

// The state is left as it was when a run does not pass, when an item of it
// is not handled, when what it gives is masked, and when it gives none of
// it; an output the run does not give keeps its value.
func TestServer_StateKept(t *testing.T) {
	tests := []struct {
		name, outputs, msg, reason string
		exit                       int
		want                       map[string]any
	}{
		{"a run that fails", `{"cursor": 8, "messages": []}`, "state kept", "the run did not pass", 1, map[string]any{"cursor": json.Number("1"), "seen": "x"}},
		{"an item not handled", `{"cursor": 8, "messages": [{"id": "a", "exit": 1}]}`, "state kept", "not every item of the run was handled", 0, map[string]any{"cursor": json.Number("1"), "seen": "x"}},
		{"a masked output", `{"cursor": "<secret:K>", "messages": []}`, "state kept", "an output of the state holds a secret or a credential, which the report masks", 0, map[string]any{"cursor": json.Number("1"), "seen": "x"}},
		{"none of the state", `{"messages": []}`, "state kept", "the run gave none of the outputs of the state", 0, map[string]any{"cursor": json.Number("1"), "seen": "x"}},
		{"an output not given", `{"cursor": 8, "messages": []}`, "state saved", "", 0, map[string]any{"cursor": json.Number("8"), "seen": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, log, path := stateServer(t, tt.outputs)
			t.Setenv(fakeExitEnv, strconv.Itoa(tt.exit))
			if err := writeState(path, map[string]any{"cursor": 1, "seen": "x", "dropped": "y"}); err != nil {
				t.Fatal(err)
			}
			serveUntil(t, s, tt.msg, func() bool {
				for _, l := range logged(t, log, tt.msg) {
					if tt.reason == "" || l["reason"] == tt.reason {
						return true
					}
				}
				return false
			})
			state, err := readState(path, []string{"cursor", "seen"})
			if err != nil || !reflect.DeepEqual(state, tt.want) {
				t.Errorf("state = %v, %v; want %v", state, err, tt.want)
			}
			fr := fakeReportAt(t, sourceRuns(t, log)[0]["report"].(string))
			if fr.Vars["cursor"] != "1" || fr.Vars["seen"] != "x" {
				t.Errorf("the run was given %v, want the state", fr.Vars)
			}
			if _, ok := fr.Vars["dropped"]; ok {
				t.Errorf("the run was given what the state no longer names: %v", fr.Vars)
			}
		})
	}
}

// A large integer, as the ID a cursor holds, is saved and given as it is
// written, not rounded.
func TestServer_StateKeepsLargeIntegers(t *testing.T) {
	s, log, path := stateServer(t, `{"cursor": 9007199254740995, "messages": [{"id": 9007199254740993}]}`)
	serveUntil(t, s, "two runs of the source", func() bool { return len(sourceRuns(t, log)) >= 2 })
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "9007199254740995") {
		t.Errorf("state = %s, %v", data, err)
	}
	if second := fakeReportAt(t, sourceRuns(t, log)[1]["report"].(string)); !strings.Contains(second.Stdin, `"cursor":9007199254740995`) {
		t.Errorf("the second run was given %s", second.Stdin)
	}
	verify := fakeReportAt(t, byWorkflow(log.runs(t))["verify"][0]["report"].(string))
	if !strings.Contains(verify.Stdin, `"id":9007199254740993`) {
		t.Errorf("the item was given as %s", verify.Stdin)
	}
}

// The state of a run is saved from the outputs read as the run was over,
// though its report is gone by the time its items are handled.
func TestServer_KeepStateFromOutputsRead(t *testing.T) {
	s, _, path := stateServer(t, `{}`)
	w := s.config.Workflows["source"]
	r := Run{ID: "20261126T000000.000000Z-1", Status: StatusPassed, Dir: filepath.Join(t.TempDir(), "gone")}
	s.keepState("source", w, outcome{run: r, outputs: map[string]any{"cursor": json.Number("12345678901234567890")}})
	state, err := readState(path, w.State)
	if err != nil || !reflect.DeepEqual(state, map[string]any{"cursor": json.Number("12345678901234567890")}) {
		t.Errorf("state = %v, %v", state, err)
	}
}

// A source with a state backs off while not every item of its runs is
// handled, as each run gives the same items again until they are.
func TestServer_StateBacksOffItemsNotHandled(t *testing.T) {
	s, log, _ := stateServer(t, `{"cursor": 8, "messages": [{"id": "a"}, {"": "skipped"}]}`)
	serveUntil(t, s, "two runs of the source", func() bool { return len(sourceRuns(t, log)) >= 2 })
	w := byWorkflow(log.runs(t))
	second := at(t, w["source"][1], "started_at")
	last := at(t, w["ack"][0], "finished_at")
	if gap := second.Sub(last); gap < firstBackoff-50*time.Millisecond {
		t.Errorf("the source waited %s after its items, want a backoff", gap)
	}
}

// A state that cannot be read keeps the workflow from running, as the log
// says, rather than start it from none.
func TestServer_StateUnreadable(t *testing.T) {
	s, log, path := stateServer(t, `{"messages": []}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	serveUntil(t, s, "the run to be skipped", func() bool { return len(logged(t, log, "run skipped: the state cannot be read")) > 0 })
	if runs := sourceRuns(t, log); len(runs) != 0 {
		t.Errorf("the source ran: %v", runs)
	}
}
