package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// pruneEvery is how often the runs older than Results.Keep are removed.
const pruneEvery = time.Hour

// Server runs the workflows of a Config.
type Server struct {
	config *Config
	runner *runner
	log    *slog.Logger
	// workers holds a token for each run going, up to Config.Workers.
	workers chan struct{}
	// busy is whether each workflow has a run going, or waiting for a
	// worker, by its name.
	busy map[string]*atomic.Bool
	// stopping is closed once serve is stopped, as no run starts from then.
	stopping chan struct{}
	stopOnce sync.Once
	// runs ends the runs going, once serve has waited the grace for them.
	runs    context.Context
	endRuns context.CancelFunc
	wg      sync.WaitGroup
}

// New returns a Server of c, whose runs are processes of exe, the probe
// command, and which logs each run to log.
func New(c *Config, exe string, log *slog.Logger) *Server {
	runs, endRuns := context.WithCancel(context.Background())
	s := &Server{
		config:   c,
		runner:   &runner{exe: exe, results: c.Results.Dir, dir: c.dir},
		log:      log,
		workers:  make(chan struct{}, c.Workers),
		busy:     make(map[string]*atomic.Bool, len(c.Workflows)),
		stopping: make(chan struct{}),
		runs:     runs,
		endRuns:  endRuns,
	}
	for name := range c.Workflows {
		s.busy[name] = new(atomic.Bool)
	}
	return s
}

// Serve starts the workflows that have a schedule or loop, and runs them
// until ctx is done. It then starts no run, waits Config.Grace for those
// going, ends those still going with SIGTERM, and returns once each is
// over.
func (s *Server) Serve(ctx context.Context) {
	s.log.Info("serving", "workflows", slices.Sorted(maps.Keys(s.config.Workflows)), "workers", s.config.Workers, "results", s.config.Results.Dir)
	var triggers sync.WaitGroup
	for _, name := range slices.Sorted(maps.Keys(s.config.Workflows)) {
		w := s.config.Workflows[name]
		switch {
		case w.schedule != nil:
			triggers.Go(func() { s.onSchedule(ctx, name, w) })
		case w.Loop:
			triggers.Go(func() { s.loop(ctx, name, w) })
		}
	}
	if s.config.Results.Keep.Duration > 0 {
		triggers.Go(func() { s.pruneLoop(ctx) })
	}

	<-ctx.Done()
	s.stopOnce.Do(func() { close(s.stopping) })
	triggers.Wait()
	s.log.Info("stopping", "grace", s.config.Grace.String())
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(s.config.Grace.Duration):
		s.endRuns()
		<-done
	}
	s.endRuns()
	s.log.Info("stopped")
}

// Abort kills the runs going at once, and any that would start, as a second
// signal to serve does.
func (s *Server) Abort() {
	s.runner.killAll()
	s.endRuns()
}

// onSchedule starts a run of w at each time its schedule matches, until ctx
// is done.
func (s *Server) onSchedule(ctx context.Context, name string, w *Workflow) {
	for {
		next, ok := w.schedule.Next(time.Now())
		if !ok {
			s.log.Error("the schedule never comes", "workflow", name, "schedule", w.Schedule)
			return
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.trigger(name, w)
		}
	}
}

// Waits of a loop whose runs did not pass, doubled from the first up to the
// last for each such run in a row, so that a workflow whose target is down,
// or that cannot run, is not started again and again at once.
const (
	firstBackoff = time.Second
	lastBackoff  = time.Minute
)

// loop runs w, and again once the run is over and Interval has passed,
// until ctx is done.
func (s *Server) loop(ctx context.Context, name string, w *Workflow) {
	var backoff time.Duration
	for ctx.Err() == nil {
		wait := w.Interval.Duration
		if done, ok := s.trigger(name, w); ok {
			select {
			case o := <-done:
				if o.run.Status != StatusPassed || !o.handled {
					backoff = min(max(2*backoff, firstBackoff), lastBackoff)
					wait = max(wait, backoff)
				} else {
					backoff = 0
				}
			case <-ctx.Done():
				return
			}
		} else {
			wait = max(wait, firstBackoff)
		}
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// outcome is how a run of a workflow started by its schedule or loop went:
// the run, and whether every item it gave was handled.
type outcome struct {
	run     Run
	handled bool
}

// trigger starts a run of w unless one is going or waiting for a worker,
// as the log then says. It returns what has the outcome once the run is
// over, with the items it gave handled, and whether it started one. A run
// waiting for a worker when serve is stopped is not run, and nothing is
// sent.
func (s *Server) trigger(name string, w *Workflow) (<-chan outcome, bool) {
	if s.isStopping() {
		return nil, false
	}
	if !s.busy[name].CompareAndSwap(false, true) {
		s.log.Warn("run skipped: the run before it is still going", "workflow", name)
		return nil, false
	}
	done := make(chan outcome, 1)
	s.wg.Go(func() {
		defer s.busy[name].Store(false)
		r, items, ok := s.runSource(name, w)
		if !ok {
			return
		}
		done <- outcome{run: r, handled: s.forEach(w.ForEach, r, items)}
	})
	return done, true
}

// runSource runs w, and returns the run with the items it gave, as its
// for_each reads them, once the run is logged. A run that passed but gave
// no list of items is logged as an error.
func (s *Server) runSource(name string, w *Workflow) (Run, []map[string]any, bool) {
	var items []map[string]any
	r, ok := s.run(name, w, nil, func(r *Run) {
		if w.ForEach == nil || r.Status != StatusPassed {
			return
		}
		var err error
		if items, err = readItems(r.ReportPath(), w.ForEach.Items); err != nil {
			r.Status, r.Err = StatusError, err
		}
	})
	return r, items, ok
}

// forEach handles each of items, the items r gave, as f says: at most
// f.Concurrency at once, each by a run of f.Workflow and then of
// f.OnSuccess or f.OnFailure, with the item as its vars. It returns once
// each item started is handled, and reports whether every item was; once
// serve is stopped, no item starts.
func (s *Server) forEach(f *ForEach, r Run, items []map[string]any) bool {
	if f == nil || len(items) == 0 {
		return true
	}
	slots := make(chan struct{}, f.Concurrency)
	var wg sync.WaitGroup
	var all atomic.Bool
	all.Store(true)
	for i, item := range items {
		select {
		case slots <- struct{}{}:
		case <-s.stopping:
		}
		if s.isStopping() {
			all.Store(false)
			break
		}
		wg.Go(func() {
			defer func() { <-slots }()
			if !s.handle(f, r, i, item) {
				all.Store(false)
			}
		})
	}
	wg.Wait()
	return all.Load()
}

// handle runs f.Workflow for item, the item of index i that r gave, and
// then f.OnSuccess or f.OnFailure as it passed or not. It reports whether
// the item was handled: its run passed and so did its OnSuccess, or it
// failed and its OnFailure passed. An item whose vars cannot be given as
// they are is not run, and is logged as an error.
func (s *Server) handle(f *ForEach, from Run, i int, item map[string]any) bool {
	tag := []any{"from", from.ID, "item", i}
	if err := checkItem(item); err != nil {
		s.log.Error("item skipped", append(tag, "workflow", f.Workflow, "error", err.Error())...)
		return false
	}
	r, ok := s.run(f.Workflow, s.config.Workflows[f.Workflow], item, nil, tag...)
	if !ok {
		return false
	}
	next := f.OnFailure
	if r.Status == StatusPassed {
		next = f.OnSuccess
	}
	if next == "" {
		return r.Status == StatusPassed
	}
	after, ok := s.run(next, s.config.Workflows[next], item, nil, append(tag, "for", r.ID)...)
	return ok && after.Status == StatusPassed
}

// checkItem returns why the vars of item cannot be given to its runs as it
// holds them: a key that --vars-file refuses, or a value the report masked,
// as it holds a secret.
func checkItem(item map[string]any) error {
	seen := map[string]bool{}
	for key, v := range item {
		name := strings.TrimSpace(key)
		if name == "" {
			return errors.New("a key of the item is empty, which no var can be named")
		}
		if seen[name] {
			return fmt.Errorf("the item has the key %s twice, once the white space around them is left out", name)
		}
		seen[name] = true
		if masked(v) {
			return fmt.Errorf("the value of %s holds a secret, which the report masks, so it would reach the run as <secret:...>", key)
		}
	}
	return nil
}

// masked reports whether v, a value read from a report, holds a secret the
// report masked.
func masked(v any) bool {
	switch val := v.(type) {
	case string:
		return strings.Contains(val, "<secret:")
	case map[string]any:
		for k, e := range val {
			if masked(k) || masked(e) {
				return true
			}
		}
	case []any:
		if slices.ContainsFunc(val, masked) {
			return true
		}
	}
	return false
}

// run runs w with vars once a worker is free, within its timeout, and logs
// the run with tag, after check has had its say on it. It reports whether
// the run started, which it does not once serve is stopped.
func (s *Server) run(name string, w *Workflow, vars map[string]any, check func(*Run), tag ...any) (Run, bool) {
	select {
	case s.workers <- struct{}{}:
	case <-s.stopping:
		return Run{}, false
	}
	if s.isStopping() {
		<-s.workers
		return Run{}, false
	}
	r := s.runOnce(name, w, vars)
	<-s.workers
	if check != nil {
		check(&r)
	}
	s.logRun(r, tag...)
	return r, true
}

func (s *Server) isStopping() bool {
	select {
	case <-s.stopping:
		return true
	default:
		return false
	}
}

// runOnce runs w with vars, within its timeout, and returns how it went.
func (s *Server) runOnce(name string, w *Workflow, vars map[string]any) Run {
	ctx := s.runs
	if w.Timeout.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.Timeout.Duration)
		defer cancel()
	}
	return s.runner.start(ctx, name, w, vars)
}

// readItems reads the items of a run from its json report at path: the
// output key, a list of objects.
func readItems(path, key string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("the items are not read: %w", err)
	}
	var report struct {
		Outputs map[string]any `json:"outputs"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("the items are not read: %s: %w", path, err)
	}
	v, ok := report.Outputs[key]
	if !ok {
		return nil, fmt.Errorf("the run gives no output %s, which for_each reads the items from", key)
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("output %s is to be a list of objects, the items, not %T", key, v)
	}
	items := make([]map[string]any, len(list))
	for i, e := range list {
		item, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d of output %s is to be an object, whose keys are the vars of its runs, not %T", i, key, e)
		}
		items[i] = item
	}
	return items, nil
}

// logRun writes a line of the log for r, with tag, which tells the item a
// run was for. The report is told only when the run wrote one, which a run
// ended before its jobs were over did not.
func (s *Server) logRun(r Run, tag ...any) {
	attrs := append([]any{
		"workflow", r.Workflow,
		"run", r.ID,
	}, tag...)
	attrs = append(attrs,
		"status", r.Status,
		"exit_status", r.ExitStatus,
		"started_at", r.StartedAt,
		"finished_at", r.FinishedAt,
		"duration_ms", r.FinishedAt.Sub(r.StartedAt).Milliseconds(),
	)
	if _, err := os.Stat(r.ReportPath()); err == nil {
		attrs = append(attrs, "report", r.ReportPath())
	}
	attrs = append(attrs, "output", r.OutputPath())
	if r.Err != nil {
		attrs = append(attrs, "error", r.Err.Error())
	}
	level := slog.LevelInfo
	if r.Status != StatusPassed {
		level = slog.LevelError
	}
	s.log.Log(context.Background(), level, "run", attrs...)
}

func (s *Server) pruneLoop(ctx context.Context) {
	for {
		s.prune(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-time.After(pruneEvery):
		}
	}
}

// prune removes the runs that ended Keep before now: those not going whose
// report and output were last written before then. A directory whose name is not that
// of a run is left as it is.
func (s *Server) prune(now time.Time) {
	for name := range s.config.Workflows {
		dir := filepath.Join(s.config.Results.Dir, name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !isRunID(e.Name()) {
				continue
			}
			run := filepath.Join(dir, e.Name())
			if s.runner.running(run) || now.Sub(lastWritten(run)) < s.config.Results.Keep.Duration {
				continue
			}
			if err := os.RemoveAll(run); err != nil {
				s.log.Warn("a run could not be removed", "workflow", name, "run", e.Name(), "error", err.Error())
			}
		}
	}
}

// lastWritten returns when the run in dir, or what it holds, was last
// written to.
func lastWritten(dir string) time.Time {
	var last time.Time
	for _, p := range []string{dir, filepath.Join(dir, "report.json"), filepath.Join(dir, "output.log")} {
		if info, err := os.Stat(p); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return last
}

func isRunID(name string) bool {
	stamp, _, ok := strings.Cut(name, "-")
	if !ok {
		return false
	}
	_, err := time.Parse(runIDLayout, stamp)
	return err == nil
}
