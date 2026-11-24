package engine

import (
	"context"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actionlog"
	"github.com/mozership/probe/internal/actionref"
	"github.com/mozership/probe/internal/mask"
	"github.com/mozership/probe/sdk"
)

// RunOptions controls how a single action invocation is run and logged.
type RunOptions struct {
	// Verbose raises the action's log level to debug.
	Verbose bool
	// Trace, with Verbose, raises it to trace, to show how the action's
	// process is started, called and stopped as well.
	Trace bool
	// Quiet silences the action's own log records. It is set for a retry
	// attempt that still has another attempt left: actions have no notion of
	// being retried, so a failure there would otherwise be reported at error
	// level even though the step goes on to succeed.
	Quiet bool
	// Masker hides the workflow's secrets in the action's log records.
	Masker *mask.Masker
	// BaseDir is the directory a local action's path is relative to: the
	// directory of the workflow file. The working directory is used when it
	// is empty.
	BaseDir string
	// cleanups are told of the actions that are to clean up once the run
	// is over. Without them no action is told that the runner cleans up.
	// Only PluginActionRunner tells them; see ActionRunner.
	cleanups *cleanups
	// ctx is done once the step gives up on the action, as it does when the
	// step times out, and the action is stopped then. Without it the action
	// runs until it returns.
	ctx context.Context
}

// Context returns the context the action runs in, which is done once the
// step gives up on the action, as it does when the step times out. It is
// never nil: without one, the action runs until it returns.
func (o RunOptions) Context() context.Context {
	if o.ctx == nil {
		return context.Background()
	}
	return o.ctx
}

// logLevel returns the level the action's log records are filtered at.
func (o RunOptions) logLevel() hclog.Level {
	if o.Quiet && !o.Verbose {
		return hclog.Off
	}
	return actionlog.Level(o.Verbose, o.Trace)
}

// ActionRunner runs the action of a step: the action named name, as the
// step writes it in uses, with what call tells it about the step and the
// state the action left in the job. It returns what the action returned and
// the state it leaves, nil to keep the one there was.
//
// Run is called concurrently, from the steps of the jobs that run at once
// and of the runs of an async job, and is to return once opts.Context() is
// done: the step has given up on the action by then, and what Run returns
// after is not used. The actions that clean up once the run is over are
// told so by PluginActionRunner alone, so a runner of its own takes part in
// that only by running the action with PluginActionRunner and opts as it
// was given them.
type ActionRunner interface {
	Run(name string, call sdk.Call, opts RunOptions) (result, newState map[string]any, err error)
}

// PluginActionRunner is the ActionRunner a run uses unless it is given
// another: it runs each action in a process of its own, a built-in one
// served by the probe executable and an external one by its own.
type PluginActionRunner struct{}

// Run runs the action for a step in a process of its own, telling it about
// the step, giving it the state it left in the job and returning the state
// it leaves.
func (p *PluginActionRunner) Run(name string, call sdk.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	// Actions are separate processes: they log to stderr as JSON and the
	// records are re-filtered here, so this level decides what the user sees.
	log := actionlog.New(name, os.Stderr, opts.Masker, opts.logLevel(), opts.Verbose)
	// An action that runs steps of its own, as embedded does, says which
	// external actions they ran that are to clean up, however it ends.
	call.Cleanups = nil
	if opts.cleanups != nil {
		call.Cleanups = &sdk.Cleanups{}
		defer func() { opts.cleanups.owe(call.Cleanups.List()...) }()
	}
	if !actionref.IsExternal(name) {
		return sdk.RunBuiltin(opts.Context(), name, call, log)
	}
	exe, err := actionref.Resolve(name, opts.BaseDir)
	if err != nil {
		return nil, nil, err
	}
	if opts.cleanups != nil && cleansUp(name, opts.BaseDir) {
		full, err := actionref.Full(name, opts.BaseDir)
		if err != nil {
			return nil, nil, err
		}
		opts.cleanups.writtenAs(full, name)
		defer opts.cleanups.begin(full)()
	}
	return sdk.RunExecutable(opts.Context(), exe.Path, exe.SHA256, call, log)
}

// cleansUp reports whether the action.yml of the external action name asks
// for the action to be called once the run is over. It was read when the
// action was resolved.
func cleansUp(name, baseDir string) bool {
	m, err := actionref.ReadManifest(name, baseDir)
	return err == nil && m.Cleanup
}
