package embedded

import (
	"errors"
	"fmt"
	"github.com/mozership/probe/actionrpc"
	"maps"
	"path/filepath"
	"time"

	"github.com/mozership/probe"
	"github.com/mozership/probe/internal/mapping"
)

type Req struct {
	Path string         `map:"path" validate:"required"`
	Vars map[string]any `map:"vars"`
	cb   *Callback
}

type Res struct {
	Code    int            `map:"code"`
	Outputs map[string]any `map:"outputs"`
	Report  string         `map:"report"`
	Error   string         `map:"error"`
	Dump    bool           `map:"dump"`
}

type Result struct {
	Req    Req           `map:"req"`
	Res    Res           `map:"res"`
	RT     time.Duration `map:"rt"`
	Status int           `map:"status"`
}

type Option func(*Callback)

type Callback struct {
	before func(path string, vars map[string]any)
	after  func(result *Result)
	// runID is the run the job is part of, or empty for a run of its own.
	runID string
	// guard is the guard of the run the job is part of.
	guard actionrpc.Guard
	// actions are the names the workflow that embeds the job gives its
	// external actions.
	actions map[string]string
	// builtin are the names of the actions of Probe.
	builtin []string
	// needsNoCheck are the actions whose steps pass without a test.
	needsNoCheck []string
	// cleanups are those of the run the job is part of, or nil for a job
	// that cleans up after itself.
	cleanups *actionrpc.Cleanups
}

func NewReq() *Req {
	return &Req{
		Vars: make(map[string]any),
	}
}

func (r *Req) Do() (*Result, error) {
	if r.Path == "" {
		return nil, fmt.Errorf("Req.Path is required")
	}

	result := &Result{Req: *r}

	// callback before
	if r.cb != nil && r.cb.before != nil {
		r.cb.before(r.Path, r.Vars)
	}

	start := time.Now()

	absPath, err := filepath.Abs(r.Path)
	if err != nil {
		return result, fmt.Errorf("failed to resolve path: %w", err)
	}
	var names map[string]string
	if r.cb != nil {
		names = r.cb.actions
	}
	job, err := probe.ReadJobFile(absPath, names)
	if err != nil {
		return result, err
	}

	// A local action in the job is found next to the job file, as one in a
	// workflow is found next to the workflow file.
	jobID := "embedded"
	printer := probe.NewPrinter(true)
	var opts []probe.StandaloneOption
	if r.cb != nil && r.cb.runID != "" {
		opts = append(opts, probe.WithRunID(r.cb.runID))
	}
	if r.cb != nil {
		opts = append(opts, probe.WithGuard(r.cb.guard), probe.WithActions(names), probe.WithBuiltinActions(r.cb.builtin), probe.WithNeedsNoCheck(r.cb.needsNoCheck), probe.WithCleanups(r.cb.cleanups))
	}
	run := job.RunStandalone(r.Vars, printer, jobID, filepath.Dir(absPath), opts...)

	result.RT = time.Since(start)

	code := 0
	errorMsg := ""
	if !run.Success {
		code = 1
		// The message res.error has always carried for a failed step.
		errorMsg = "execution error in job_start: job execution failed"
	}
	if run.Err != nil {
		errorMsg = run.Err.Error()
	}
	// A step the guard refused refuses the step that embeds the job, so
	// that the run tells it as such, exits as it would, and does not retry.
	if run.Refused != nil {
		return result, run.Refused
	}

	result.Res = Res{
		Code:    code,
		Outputs: run.Outputs,
		Report:  run.Report,
		Error:   errorMsg,
		Dump:    false, // Don't dump request/response for embedded jobs
	}
	result.Status = code

	// callback after
	if r.cb != nil && r.cb.after != nil {
		r.cb.after(result)
	}

	// A step that fails is the embedded job's result, for the embedding step
	// to test. Only a job that could not run, such as one with an invalid
	// step, is an error.
	if run.Err != nil {
		detailedError := fmt.Sprintf("embedded job execution failed: %s", run.Err)
		if run.Report != "" {
			detailedError += "\nEmbedded job details:\n" + run.Report
		}
		return result, errors.New(detailedError)
	}

	return result, nil
}

func Execute(data map[string]any, opts ...Option) (map[string]any, error) {
	// Create a copy to avoid modifying the original data
	dataCopy := make(map[string]any)
	maps.Copy(dataCopy, data)

	m := mapping.HeaderToStringValue(dataCopy)

	r := NewReq()

	cb := &Callback{}
	for _, opt := range opts {
		opt(cb)
	}
	r.cb = cb

	if err := mapping.MapToStructByTags(m, r); err != nil {
		return map[string]any{}, err
	}

	result, err := r.Do()
	if err != nil {
		// Even on error, try to return a structured result if we have one
		if result != nil {
			mapResult, mapErr := mapping.StructToMapByTags(result)
			if mapErr == nil {
				return mapResult, err
			}
		}
		return map[string]any{}, err
	}

	mapResult, err := mapping.StructToMapByTags(result)
	if err != nil {
		return map[string]any{}, err
	}

	return mapResult, nil
}

func WithBefore(f func(path string, vars map[string]any)) Option {
	return func(c *Callback) {
		c.before = f
	}
}

func WithAfter(f func(result *Result)) Option {
	return func(c *Callback) {
		c.after = f
	}
}

// WithGuard runs the job under the guard of the run of the step that
// embeds it.
func WithGuard(guard actionrpc.Guard) Option {
	return func(c *Callback) {
		c.guard = guard
	}
}

// WithActions reads the job by names, the names the workflow of the step
// that embeds it gives its external actions.
func WithActions(names map[string]string) Option {
	return func(c *Callback) {
		c.actions = names
	}
}

// WithBuiltinActions tells the job the names of the actions of Probe, so
// that a uses that names no action stops the job before any step runs.
func WithBuiltinActions(names []string) Option {
	return func(c *Callback) {
		c.builtin = names
	}
}

// WithNeedsNoCheck tells the job the names of the actions whose steps pass
// without a test, so that a step of one that has none is not counted as
// untested.
func WithNeedsNoCheck(names []string) Option {
	return func(c *Callback) {
		c.needsNoCheck = names
	}
}

// WithCleanups leaves what the steps of the job left behind for the rest of
// the run of the step that embeds it, which cleans up once it is over: the
// actions to call then are named in c.
func WithCleanups(c *actionrpc.Cleanups) Option {
	return func(cb *Callback) {
		cb.cleanups = c
	}
}

// WithRunID runs the job as part of the run id names, the run of the step
// that embeds it, so that the actions of the job are told the same run.
func WithRunID(id string) Option {
	return func(c *Callback) {
		c.runID = id
	}
}
