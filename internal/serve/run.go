package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Run statuses, as the serve log tells how a run ended.
const (
	StatusPassed = "passed"
	// StatusFailed is a run whose test or action failed.
	StatusFailed = "failed"
	// StatusError is a run that could not run its workflow, such as one
	// whose workflow cannot run as it is written.
	StatusError   = "error"
	StatusTimeout = "timeout"
	// StatusStopped is a run that serve stopped as it was stopped itself.
	StatusStopped = "stopped"
)

// exitConfigError is the status probe exits with when the workflow or the
// command line is wrong.
const exitConfigError = 2

// runIDLayout names a run by when it started, so that the runs of a
// workflow are listed in the order they ran. A count follows, for runs that
// start at once.
const runIDLayout = "20060102T150405.000000Z"

// Run is a run of a workflow, as serve tells it once it is over.
type Run struct {
	Workflow   string
	ID         string
	StartedAt  time.Time
	FinishedAt time.Time
	// ExitStatus is that of the process of the run; -1 when a signal ended
	// it, or it could not be started.
	ExitStatus int
	Status     string
	// Dir is where the run left its report, report.json, and what it
	// wrote, output.log.
	Dir string
	// Err is why the run could not be started, or nil.
	Err error
}

// ReportPath returns the path of the json report of the run.
func (r Run) ReportPath() string {
	return filepath.Join(r.Dir, "report.json")
}

// OutputPath returns the path of what the run wrote, its standard output
// and its standard error.
func (r Run) OutputPath() string {
	return filepath.Join(r.Dir, "output.log")
}

// runner starts the runs of workflows as processes of probe.
type runner struct {
	// exe is the probe the runs are processes of.
	exe     string
	results string
	// grace is how long a run sent SIGTERM has to stop before it is
	// killed.
	grace time.Duration
	count atomic.Uint64
}

// start runs w, named name, with vars, and returns how the run went once it
// is over. ctx ends the run when it is done before the run is: by timing
// out, or as serve is stopped.
func (rn *runner) start(ctx context.Context, name string, w *Workflow, vars map[string]any) Run {
	started := time.Now()
	id := fmt.Sprintf("%s-%d", started.UTC().Format(runIDLayout), rn.count.Add(1))
	r := Run{Workflow: name, ID: id, StartedAt: started, ExitStatus: -1}
	r.Dir = filepath.Join(rn.results, name, r.ID)
	r.Err = rn.exec(ctx, w, vars, &r)
	r.FinishedAt = time.Now()
	r.Status = status(ctx, r)
	return r
}

func (rn *runner) exec(ctx context.Context, w *Workflow, vars map[string]any, r *Run) error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	stdin, err := varsJSON(w.Vars, vars)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(r.OutputPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	cmd := exec.CommandContext(ctx, rn.exe, args(w, r.ReportPath())...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Env = childEnv(os.Environ())
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = rn.grace
	err = cmd.Run()
	if cmd.ProcessState != nil {
		r.ExitStatus = cmd.ProcessState.ExitCode()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) || (err != nil && ctx.Err() != nil) {
		return nil
	}
	return err
}

// args returns the arguments of the probe that runs w and writes its json
// report to report. The guard is given in full, so that the environment of
// serve cannot loosen or tighten what the serve file says.
func args(w *Workflow, report string) []string {
	a := []string{
		"--output", "stream",
		"--report", "json=" + report,
		"--vars-file", "-",
		fmt.Sprintf("--read-only=%t", w.ReadOnly),
		"--allow-host=" + strings.Join(w.AllowHosts, ","),
		"--allow-action=" + strings.Join(w.AllowActions, ","),
	}
	for _, f := range w.EnvFiles {
		a = append(a, "--env-file", f)
	}
	for _, j := range w.Jobs {
		a = append(a, "--job", j)
	}
	return append(append(a, "--"), w.Files...)
}

// varsJSON returns the vars of a run as --vars-file reads them: those the
// serve file gives the workflow, and over them those of the run.
func varsJSON(own, given map[string]any) ([]byte, error) {
	vars := make(map[string]any, len(own)+len(given))
	maps.Copy(vars, own)
	maps.Copy(vars, given)
	return json.Marshal(vars)
}

// childEnv returns env without the variables that would change where a run
// writes its reports or how it prints, which serve decides.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "PROBE_REPORT", "PROBE_BASELINE", "PROBE_OUTPUT":
			continue
		}
		out = append(out, kv)
	}
	return out
}

func status(ctx context.Context, r Run) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return StatusTimeout
	case ctx.Err() != nil:
		return StatusStopped
	case r.Err != nil:
		return StatusError
	case r.ExitStatus == 0:
		return StatusPassed
	case r.ExitStatus == exitConfigError || r.ExitStatus < 0:
		return StatusError
	default:
		return StatusFailed
	}
}
