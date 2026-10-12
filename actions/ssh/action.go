package ssh

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

// Run runs the command in with over SSH. The password and key passphrase in
// with are hidden by the workflow runner, which learns them before the
// action starts and masks the records this action logs.
func (a *Action) Run(with map[string]any) (map[string]any, error) {
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, errors.New("ssh action requires parameters in 'with' section. Please specify connection details like host, user, cmd")
	}

	actionrpc.LogParams(a.log, "received ssh request parameters", with)

	before := WithBefore(func(host string, port int, user string, cmd string) {
		a.log.Debug("ssh connection prepared", "host", host, "port", port, "user", user, "cmd", cmd)
	})
	// Only the name is logged: the value may be a secret.
	envRefused := WithEnvRefused(func(name string, err error) {
		a.log.Warn("ssh server refused an environment variable; the command runs without it (allow it with AcceptEnv in sshd_config)", "name", name, "error", err)
	})
	ret, err := Execute(with, before, envRefused)

	actionrpc.LogOutcome(a.log, "ssh command", ret, err)

	return ret, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Params returns the keys the ssh action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}
