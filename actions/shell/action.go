package shell

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep runs the command, and tells the runner of one it left running in
// the background, for the runner to stop once the run is over.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	with := call.With
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, nil, errors.New("shell action requires parameters in 'with' section. Please specify command details like cmd")
	}

	actionrpc.LogParams(a.log, "received shell request parameters", with)

	before := WithBefore(func(cmd string, shell string, workdir string) {
		a.log.Trace("shell command prepared", "cmd", cmd, "shell", shell, "workdir", workdir)
	})
	after := WithAfter(func(result *Result) {
		a.log.Trace("shell command completed", "result", result)
		// One run in the foreground has exited by now.
		if result.Req.Background && result.Res.PID > 0 {
			call.Processes.Add(actionrpc.Process{PID: result.Res.PID, Log: result.Res.Log})
		}
	})
	ret, err := Execute(with, before, after)

	actionrpc.LogOutcome(a.log, "shell command", ret, err)

	return ret, nil, err
}

func Serve() {
	stopStartedOnSignal()

	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// stopStartedOnSignal stops the background commands this plugin started
// when it is interrupted, terminated or hung up on, and then lets the signal
// end it. Ctrl+C reaches this plugin with the rest of probe's process group,
// and can do so before the workflow has heard of a command just started. The
// signal is caught rather than ignored, so the commands the plugin runs get
// it as usual.
func stopStartedOnSignal() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-ch
		signal.Stop(ch)
		StopStarted()
		if s, ok := sig.(syscall.Signal); ok {
			_ = syscall.Kill(os.Getpid(), s)
		}
	}()
}

// Params returns the keys the shell action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}

// ReqFields returns the fields of the req the shell action returns, those of
// Req, and ResFields those of its res, those of Res, for probe check to
// report a step that reads another.
func ReqFields() []string {
	return mapping.FieldTags(Req{})
}

// ResFields returns the fields of the res the shell action returns; see
// ReqFields.
func ResFields() []string {
	return mapping.FieldTags(Res{})
}

// ReqTypes returns the kinds of the fields of the req the shell action
// returns, those of Req, and ResTypes those of its res, those of Res, for
// probe check to report an expression that does with a field what its kind
// does not allow.
func ReqTypes() map[string]kind.Kind {
	return kind.Fields(Req{})
}

// ResTypes returns the kinds of the fields of the res the shell action
// returns; see ReqTypes.
func ResTypes() map[string]kind.Kind {
	return kind.Fields(Res{})
}

// Required returns the keys of with the shell action cannot run without:
// cmd.
func Required() [][]string {
	return [][]string{{"cmd"}}
}
