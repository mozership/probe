package serve

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
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
	c := &Config{Workers: 2, Results: Results{Dir: t.TempDir()}, Grace: Duration{grace}, Workflows: workflows}
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
		"intake": {Files: []string{"/w.yml"}, Loop: true, Interval: Duration{50 * time.Millisecond}},
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
				"w": {Files: []string{"/w.yml"}, Loop: true, Interval: Duration{time.Hour}},
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
	s.config.Results.Keep = Duration{time.Hour}
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
	s.prune(now)
	for d, kept := range map[string]bool{old: false, recent: true, other: true} {
		if _, err := os.Stat(d); (err == nil) != kept {
			t.Errorf("%s kept = %v, want %v", filepath.Base(d), err == nil, kept)
		}
	}
}
