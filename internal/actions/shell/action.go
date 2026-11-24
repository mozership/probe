package shell

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/internal/mapping"
	"github.com/mozership/probe/sdk"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(sdk.Call{With: with})
	return ret, err
}

// RunStep runs the command, and tells the runner of one it left running in
// the background, for the runner to stop once the run is over.
func (a *Action) RunStep(call sdk.Call) (map[string]any, map[string]any, error) {
	with := call.With
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, nil, errors.New("shell action requires parameters in 'with' section. Please specify command details like cmd")
	}

	sdk.LogParams(a.log, "received shell request parameters", with)

	before := WithBefore(func(cmd string, shell string, workdir string) {
		a.log.Trace("shell command prepared", "cmd", cmd, "shell", shell, "workdir", workdir)
	})
	after := WithAfter(func(result *Result) {
		a.log.Trace("shell command completed", "result", result)
		// One run in the foreground has exited by now.
		if result.Req.Background && result.Res.PID > 0 {
			call.Processes.Add(sdk.Process{PID: result.Res.PID, Log: result.Res.Log})
		}
	})
	ret, err := Execute(with, before, after)

	sdk.LogOutcome(a.log, "shell command", ret, err)

	return ret, nil, err
}

func serve() {
	stopStartedOnSignal()

	sdk.Serve(func(log hclog.Logger) sdk.Action {
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

// Spec declares the shell action. It keeps to no guard, as it does not check
// what it is about to do against one, so that a guard refuses it unless it
// is allowed by name. It cannot run without cmd.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     actionspec.KeepsNone(),
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: mapping.FieldTags(Res{}),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  kind.Fields(Res{}),
		Required:  [][]string{{"cmd"}},
		Choices:   actionspec.NoChoices(),
	}
}
