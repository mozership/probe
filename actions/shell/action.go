package shell

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, errors.New("shell action requires parameters in 'with' section. Please specify command details like cmd")
	}

	actionrpc.LogParams(a.log, "received shell request parameters", with)

	before := WithBefore(func(cmd string, shell string, workdir string) {
		a.log.Debug("shell command prepared", "cmd", cmd, "shell", shell, "workdir", workdir)
	})
	after := WithAfter(func(result *Result) {
		a.log.Debug("shell command completed", "result", result)
	})
	ret, err := Execute(with, before, after)

	actionrpc.LogOutcome(a.log, "shell command", ret, err)

	return ret, err
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
