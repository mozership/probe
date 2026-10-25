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

// ActionRunner defines the interface for running actions
type ActionRunner interface {
	RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error)
}

// StepActionRunner is an ActionRunner that also tells an action about the
// step it runs for and carries the state it keeps in a job, as
// actionrpc.StepAction describes. A runner that is not one runs every action
// with its parameters alone.
type StepActionRunner interface {
	ActionRunner
	RunStep(name string, call actionrpc.Call, opts RunOptions) (result, newState map[string]any, err error)
}

// PluginActionRunner implements ActionRunner using the plugin system
type PluginActionRunner struct{}

// RunActions executes an action using the plugin system
func (p *PluginActionRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	result, _, err := p.RunStep(name, actionrpc.Call{With: with}, opts)
	return result, err
}

// RunStep executes an action for a step using the plugin system, telling it
// about the step, giving it the state it left in the job and returning the
// state it leaves.
func (p *PluginActionRunner) RunStep(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
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

// runAction runs the action named name for a step, when runner can tell it
// about one, and returns the state it leaves.
func runAction(runner ActionRunner, name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	if sr, ok := runner.(StepActionRunner); ok {
		return sr.RunStep(name, call, opts)
	}
	result, err := runner.RunActions(name, call.With, opts)
	return result, nil, err
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

// MockActionRunner implements ActionRunner for testing
type MockActionRunner struct {
	Results map[string]map[string]any
	Errors  map[string]error
	// States is the state each action leaves in the job, keyed by its name.
	States map[string]map[string]any
	// Calls records each call of an action for a step, in order.
	Calls map[string][]actionrpc.Call

	mu sync.Mutex
}

// NewMockActionRunner creates a new mock action runner
func NewMockActionRunner() *MockActionRunner {
	return &MockActionRunner{
		Results: make(map[string]map[string]any),
		Errors:  make(map[string]error),
		States:  make(map[string]map[string]any),
		Calls:   make(map[string][]actionrpc.Call),
	}
}

// RunStep records the call and returns the result and the state set for the
// action.
func (m *MockActionRunner) RunStep(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	m.mu.Lock()
	if m.Calls == nil {
		m.Calls = make(map[string][]actionrpc.Call)
	}
	m.Calls[name] = append(m.Calls[name], call)
	newState := m.States[name]
	m.mu.Unlock()
	result, err := m.RunActions(name, call.With, opts)
	if err != nil {
		return nil, nil, err
	}
	return result, newState, nil
}

// SetResult sets the expected result for an action
func (m *MockActionRunner) SetResult(actionName string, result map[string]any) {
	m.Results[actionName] = result
}

// SetError sets the expected error for an action
func (m *MockActionRunner) SetError(actionName string, err error) {
	m.Errors[actionName] = err
}

// RunActions returns the mocked result or error for the given action
func (m *MockActionRunner) RunActions(name string, with map[string]any, opts RunOptions) (map[string]any, error) {
	if err, exists := m.Errors[name]; exists {
		return nil, err
	}

	if result, exists := m.Results[name]; exists {
		return result, nil
	}

	// Default mock response
	return map[string]any{
		"code":    0,
		"mock":    true,
		"action":  name,
		"with":    with,
		"results": map[string]any{},
	}, nil
}
