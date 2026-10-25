package probe

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mask"
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
	cleanups *cleanups
	// ctx is done once the step gives up on the action, as it does when the
	// step times out, and the action is stopped then. Without it the action
	// runs until it returns.
	ctx context.Context
}

// context returns the context the action runs in.
func (o RunOptions) context() context.Context {
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
	return actionLogLevel(o.Verbose, o.Trace)
}

// ActionRunner runs the action of a step: the action named name, as the
// step writes it in uses, with what call tells it about the step and the
// state the action left in the job. It returns what the action returned and
// the state it leaves, nil to keep the one there was.
type ActionRunner interface {
	Run(name string, call actionrpc.Call, opts RunOptions) (result, newState map[string]any, err error)
}

// PluginActionRunner is the ActionRunner a run uses unless it is given
// another: it runs each action in a process of its own, a built-in one
// served by the probe executable and an external one by its own.
type PluginActionRunner struct{}

// Run runs the action for a step in a process of its own, telling it about
// the step, giving it the state it left in the job and returning the state
// it leaves.
func (p *PluginActionRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	// Actions are separate processes: they log to stderr as JSON and the
	// records are re-filtered here, so this level decides what the user sees.
	log := newActionLog(name, os.Stderr, opts.Masker, opts.logLevel(), opts.Verbose)
	// An action that runs steps of its own, as embedded does, says which
	// external actions they ran that are to clean up, however it ends.
	call.Cleanups = nil
	if opts.cleanups != nil {
		call.Cleanups = &actionrpc.Cleanups{}
		defer func() { opts.cleanups.owe(call.Cleanups.List()...) }()
	}
	if !actionref.IsExternal(name) {
		return actionrpc.RunBuiltin(opts.context(), name, call, log)
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
	return actionrpc.RunExecutable(opts.context(), exe.Path, exe.SHA256, call, log)
}

// cleansUp reports whether the action.yml of the external action name asks
// for the action to be called once the run is over. It was read when the
// action was resolved.
func cleansUp(name, baseDir string) bool {
	m, err := actionref.ReadManifest(name, baseDir)
	return err == nil && m.Cleanup
}

// actionStates holds the state each action keeps in one run of a job, keyed
// by the action's name as the steps write it in uses.
type actionStates struct {
	mu     sync.Mutex
	states map[string]map[string]any
}

func newActionStates() *actionStates {
	return &actionStates{states: make(map[string]map[string]any)}
}

// get returns a copy of the state the action left, or nil when it left none.
// A runner in this process may change what it is given, and the state kept
// must not change with it, should the step then fail or time out.
func (s *actionStates) get(name string) map[string]any {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// What is kept was made sendable when it was set, so this cannot fail.
	state, _ := actionrpc.Sendable(s.states[name])
	return state
}

// set records the state the action leaves, in the form it takes when it is
// sent, which is a copy of it: a runner in this process may still hold the
// state it returned. A nil state keeps the one there was. A state that cannot
// be sent is an error, as it is for an action in a process of its own.
func (s *actionStates) set(name string, state map[string]any) error {
	if s == nil || state == nil {
		return nil
	}
	sendable, err := actionrpc.Sendable(state)
	if err != nil {
		return fmt.Errorf("cannot keep the action's state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[name] = sendable
	return nil
}
