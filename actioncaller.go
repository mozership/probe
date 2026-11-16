package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mask"
)

// ExecutableEnv is the environment variable in which the CLI names its own
// executable for the actions it runs, as it sets PROBE_TTY. An ActionCaller
// starts a built-in action from it, as an action is not probe.
const ExecutableEnv = "PROBE_EXECUTABLE"

// ActionCaller calls actions from inside an action, for the step the action
// runs for, as a step of a workflow would: under the guard of the run, as
// part of the run, with the state each action keeps between calls. It is to
// steps what the embedded action is to jobs.
//
// Its methods may be called concurrently.
type ActionCaller struct {
	call    actionrpc.Call
	baseDir string
	log     hclog.Logger
	logOut  io.Writer
	probe   string
	// masker hides in the records of the actions called the credentials
	// of their parameters and results, which the runner of the calling
	// action has not learned.
	masker *mask.Masker

	mu sync.Mutex
	// guard is the guard of the call, with what the action.yml of each
	// external action called declares under guard.
	guard actionrpc.Guard
	// read are the external actions whose action.yml was read, by their
	// names in full, with whether it sets cleanup.
	read   map[string]bool
	states *actionStates

	// runExternal and runBuiltin run an action once; tests replace them.
	runExternal func(ctx context.Context, exe *actionref.Executable, call actionrpc.Call, log hclog.Logger) (map[string]any, map[string]any, error)
	runBuiltin  func(ctx context.Context, probe, name string, call actionrpc.Call, log hclog.Logger) (map[string]any, map[string]any, error)
}

// CallerOption changes how an ActionCaller calls actions.
type CallerOption func(*ActionCaller)

// WithCallerLogOutput writes the records of the actions called to w, as
// JSON at the levels the actions gave them. Without it, they go to stderr,
// where the records of the calling action go when it is served by
// actionrpc.Serve, for the runner to filter. Either way, the credentials
// of what the actions were given and returned are hidden in them.
func WithCallerLogOutput(w io.Writer) CallerOption {
	return func(c *ActionCaller) {
		c.logOut = w
	}
}

// WithProbeExecutable starts built-in actions from the probe executable at
// path, in place of the one ExecutableEnv names.
func WithProbeExecutable(path string) CallerOption {
	return func(c *ActionCaller) {
		c.probe = path
	}
}

// NewActionCaller returns a caller for the step call is for, the call the
// calling action was given. baseDir is the directory a local action that
// call.Actions does not name is resolved against; the working directory
// when it is empty.
func NewActionCaller(call actionrpc.Call, baseDir string, opts ...CallerOption) *ActionCaller {
	c := &ActionCaller{
		call:        call,
		baseDir:     baseDir,
		logOut:      os.Stderr,
		probe:       os.Getenv(ExecutableEnv),
		masker:      mask.New(nil, nil),
		guard:       call.Guard,
		read:        make(map[string]bool),
		states:      newActionStates(),
		runExternal: runExternal,
		runBuiltin:  actionrpc.RunBuiltinFrom,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.log = hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     c.masker.Writer(c.logOut),
		JSONFormat: true,
	})
	return c
}

func runExternal(ctx context.Context, exe *actionref.Executable, call actionrpc.Call, log hclog.Logger) (map[string]any, map[string]any, error) {
	return actionrpc.RunExecutable(ctx, exe.Path, exe.SHA256, call, log)
}

// CallResult is what an action called returned.
type CallResult struct {
	// Result is what the action returned: req, res, rt and status, as a
	// step reads them.
	Result map[string]any
	// Contract is the check the action made of the call against a
	// contract, such as an OpenAPI document; nil when it made none.
	Contract *actionrpc.ContractCheck
}

// Call runs the action uses once with with, and returns what it returned.
// uses is read as a step's uses is: a name the workflow gives an action
// under actions, an external action, or an action of Probe.
//
// On the first call of an external action, its action.yml is read: what it
// declares under guard is put in the guard, and the action is told to
// clean up once the run is over when it sets cleanup. An action the guard
// does not let run is refused with an actionrpc.Refused error, which the
// calling action can return as its own. The state an action leaves is
// given to its next call. Once ctx is done, the action is stopped, and
// ctx's error is returned.
func (c *ActionCaller) Call(ctx context.Context, uses string, with map[string]any) (*CallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := c.full(uses)
	if err != nil {
		return nil, err
	}
	external := actionref.IsExternal(full)
	if external {
		if err := withContext(ctx, func() error { return c.readManifest(full) }); err != nil {
			return nil, err
		}
	} else if c.probe == "" {
		return nil, fmt.Errorf("cannot call the action %s of Probe: %s is not set, as the Probe that runs this action is too old to set it", uses, ExecutableEnv)
	}

	c.mu.Lock()
	guard := c.guard
	cleansUp := c.read[full]
	c.mu.Unlock()
	if !guard.Runs(full) && !slices.Contains(guard.AllowActions, uses) {
		flags := make([]string, 0, 2)
		for _, kind := range guard.Missing(full) {
			flags = append(flags, "--"+kind)
		}
		return nil, actionrpc.Refuse("the action %s does not keep to %s, which the run is under; let it run with --allow-action %s", uses, strings.Join(flags, " and "), full)
	}

	call := actionrpc.Call{
		With:      with,
		State:     c.states.get(full),
		Step:      c.call.Step,
		Guard:     guard,
		Actions:   c.call.Actions,
		Processes: &actionrpc.Processes{},
		Contract:  &actionrpc.Contract{},
	}
	// The runner of the calling action cleans up once the run is over:
	// the action called is owed its cleanup from before it runs, as it may
	// leave something behind however it ends, and so are those it says it
	// ran.
	if c.call.Cleanups != nil {
		if cleansUp {
			c.call.Cleanups.Add(full)
		}
		call.Cleanups = &actionrpc.Cleanups{}
		defer func() { c.call.Cleanups.Add(call.Cleanups.List()...) }()
	}
	// A process the action leaves running is stopped once the run is over,
	// with those of the calling action.
	defer func() { c.call.Processes.Add(call.Processes.List()...) }()

	// The credentials the action is about to be sent are learned before
	// it runs, so that its own records already have them hidden, as for
	// a step.
	c.masker.Learn(with)
	log := c.log.Named(uses)
	var ret, state map[string]any
	if external {
		var exe *actionref.Executable
		err := withContext(ctx, func() error {
			var err error
			exe, err = actionref.Resolve(full, "")
			return err
		})
		if err != nil {
			return nil, err
		}
		ret, state, err = c.runExternal(ctx, exe, call, log)
		if err != nil {
			return nil, err
		}
	} else {
		ret, state, err = c.runBuiltin(ctx, c.probe, full, call, log)
		if err != nil {
			return nil, err
		}
	}
	if err := c.states.set(full, state); err != nil {
		return nil, err
	}
	if res, ok := ret["res"].(map[string]any); ok {
		c.masker.Learn(res)
	}
	return &CallResult{Result: ret, Contract: call.Contract.Check()}, nil
}

// full returns uses as a step's uses names the action: a name the workflow
// gives an action as the action it stands for, a local action as the
// absolute path of its directory, and a remote or built-in one as it is.
func (c *ActionCaller) full(uses string) (string, error) {
	if uses == "" {
		return "", errors.New("no action to call: uses is empty")
	}
	if named, ok := c.call.Actions[uses]; ok {
		if !actionref.IsExternal(named) {
			return named, nil
		}
		// A local action is named by an absolute path, which is taken to
		// the directory a symbolic link on the way leads to, as when the
		// action is called by the path, for both to keep one state and to
		// be cleaned up once.
		return actionref.Full(named, "")
	}
	if !actionref.IsExternal(uses) {
		return uses, nil
	}
	return actionref.Full(uses, c.baseDir)
}

// readManifest reads the action.yml of the external action full on its
// first call, and puts what it declares under guard in the guard. What it
// declares replaces what the guard came with, as for a step.
func (c *ActionCaller) readManifest(full string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.read[full]; ok {
		return nil
	}
	m, err := actionref.ReadManifest(full, "")
	if err != nil {
		return NewConfigurationError("resolve_action", "failed to resolve an external action", err)
	}
	c.guard = c.guard.WithKeeps(full, m.Guard)
	c.read[full] = m.Cleanup
	return nil
}

// withContext runs f, which cannot be stopped, such as the download of an
// external action, and returns its error, or ctx's error once ctx is done
// before f returns. f carries on then, and what it fetches is kept for a
// later call.
func withContext(ctx context.Context, f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
