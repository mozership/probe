package serve

import (
	"context"
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
			case r := <-done:
				if r.Status != StatusPassed {
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

// trigger starts a run of w unless one is going or waiting for a worker,
// as the log then says. It returns what has the run once it is over, and
// whether it started one. A run waiting for a worker when serve is stopped
// is not run, and nothing is sent.
func (s *Server) trigger(name string, w *Workflow) (<-chan Run, bool) {
	if s.isStopping() {
		return nil, false
	}
	if !s.busy[name].CompareAndSwap(false, true) {
		s.log.Warn("run skipped: the run before it is still going", "workflow", name)
		return nil, false
	}
	done := make(chan Run, 1)
	s.wg.Go(func() {
		defer s.busy[name].Store(false)
		select {
		case s.workers <- struct{}{}:
		case <-s.stopping:
			return
		}
		defer func() { <-s.workers }()
		if s.isStopping() {
			return
		}
		r := s.runOnce(name, w, nil)
		s.logRun(r)
		done <- r
	})
	return done, true
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

// logRun writes a line of the log for r. The report is told only when the
// run wrote one, which a run ended before its jobs were over did not.
func (s *Server) logRun(r Run) {
	attrs := []any{
		"workflow", r.Workflow,
		"run", r.ID,
		"status", r.Status,
		"exit_status", r.ExitStatus,
		"started_at", r.StartedAt,
		"finished_at", r.FinishedAt,
		"duration_ms", r.FinishedAt.Sub(r.StartedAt).Milliseconds(),
	}
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
