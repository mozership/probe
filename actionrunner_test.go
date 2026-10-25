package probe

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mozership/probe/actionrpc"
)

// The runners here stand in for the actions of the steps under test, given
// to a run as Config.ActionRunner or to a job with WithActionRunner.

// mockRunner returns the result, the error and the state set for each action
// by its name, and records each call. Its fields are read and written under
// mu while a run may call it.
type mockRunner struct {
	Results map[string]map[string]any
	Errors  map[string]error
	// States is the state each action leaves in the job, keyed by its name.
	States map[string]map[string]any
	// Calls records each call of an action, in order, keyed by its name.
	Calls map[string][]actionrpc.Call

	mu sync.Mutex
}

func newMockRunner() *mockRunner {
	return &mockRunner{
		Results: make(map[string]map[string]any),
		Errors:  make(map[string]error),
		States:  make(map[string]map[string]any),
		Calls:   make(map[string][]actionrpc.Call),
	}
}

// SetResult sets the result the action returns.
func (m *mockRunner) SetResult(name string, result map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Results[name] = result
}

// SetError sets the error the action fails with.
func (m *mockRunner) SetError(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors[name] = err
}

// Run records the call and returns the error set for the action, or the
// result and the state set for it. An action without a result set returns
// one that says what it was called with.
func (m *mockRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls[name] = append(m.Calls[name], call)
	if err, ok := m.Errors[name]; ok {
		return nil, nil, err
	}
	if result, ok := m.Results[name]; ok {
		return result, m.States[name], nil
	}
	return map[string]any{
		"code":    0,
		"mock":    true,
		"action":  name,
		"with":    call.With,
		"results": map[string]any{},
	}, m.States[name], nil
}

// stepRunners runs the action of each step with the runner given for the
// step's name, as its run evaluates the name, and the action of any other
// step with fallback. It lets the steps of one run each have a runner of
// their own.
type stepRunners struct {
	byName   map[string]ActionRunner
	fallback ActionRunner
}

func (r stepRunners) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	if runner, ok := r.byName[call.Step.Name]; ok {
		return runner.Run(name, call, opts)
	}
	if r.fallback == nil {
		panic(fmt.Sprintf("no runner for the step %q", call.Step.Name))
	}
	return r.fallback.Run(name, call, opts)
}

// slowRunner takes delay to return a result.
type slowRunner struct {
	delay time.Duration
}

func (r *slowRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	time.Sleep(r.delay)
	return map[string]any{"status": 0}, nil, nil
}

// stoppableRunner is an action that runs until the step gives up on it, and
// tells when it was told to stop.
type stoppableRunner struct {
	stopped chan struct{}
	err     error
}

func (r *stoppableRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	<-opts.Context().Done()
	close(r.stopped)
	return nil, nil, r.err
}

// contextRecordingRunner records the context an action is given, and whether
// it was done while the action ran.
type contextRecordingRunner struct {
	ctx    context.Context
	during error
}

func (r *contextRecordingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.ctx, r.during = opts.ctx, opts.Context().Err()
	return map[string]any{"status": 0}, nil, nil
}

// callCountingRunner counts its calls in callCount, and returns result, or
// what resultFunc returns for the count, or err when it is set.
type callCountingRunner struct {
	result     map[string]any
	resultFunc func(count int) map[string]any
	// contractFunc is the check of each call against a contract that the
	// action reports, or nil for none.
	contractFunc func(count int) *actionrpc.ContractCheck
	callCount    *int
	err          error
}

func (r *callCountingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	*r.callCount++
	if r.err != nil {
		return nil, nil, r.err
	}
	if r.contractFunc != nil {
		if check := r.contractFunc(*r.callCount); check != nil {
			call.Contract.Report(*check)
		}
	}
	if r.resultFunc != nil {
		return r.resultFunc(*r.callCount), nil, nil
	}
	return r.result, nil, nil
}

// optionsRecordingRunner records the RunOptions every call receives and
// keeps failing until failUntil attempts have been made.
type optionsRecordingRunner struct {
	mu        sync.Mutex
	opts      []RunOptions
	failUntil int
}

func (r *optionsRecordingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.mu.Lock()
	r.opts = append(r.opts, opts)
	attempt := len(r.opts)
	r.mu.Unlock()

	if attempt <= r.failUntil {
		return nil, nil, fmt.Errorf("action failed on attempt %d", attempt)
	}
	return map[string]any{
		"status": 0,
		"res":    map[string]any{"code": 200},
	}, nil, nil
}

func (r *optionsRecordingRunner) recorded() []RunOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RunOptions(nil), r.opts...)
}

// lateBackgroundRunner leaves a process running in the background, as a
// background shell step does, after delay, and then fails with err when it
// is not nil.
type lateBackgroundRunner struct {
	t     *testing.T
	delay time.Duration
	err   error
	pid   chan int
}

func (r *lateBackgroundRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	time.Sleep(r.delay)
	pid := startGroup(r.t, "sleep 60")
	call.Processes.Add(actionrpc.Process{PID: pid})
	r.pid <- pid
	if r.err != nil {
		return nil, nil, r.err
	}
	return map[string]any{"res": map[string]any{"code": -1}}, nil, nil
}
