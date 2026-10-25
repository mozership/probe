package ssh

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
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
		a.log.Trace("ssh connection prepared", "host", host, "port", port, "user", user, "cmd", cmd)
	})
	// Only the name is logged: the value may be a secret.
	envRefused := WithEnvRefused(func(name string, err error) {
		a.log.Warn("ssh server refused an environment variable; the command runs without it (allow it with AcceptEnv in sshd_config)", "name", name, "error", err)
	})
	ret, err := Execute(with, before, envRefused)

	actionrpc.LogOutcome(a.log, "ssh command", ret, err)

	return ret, err
}

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the ssh action. It keeps to no guard, as it does not check
// what it is about to do against one, so that a guard refuses it unless it
// is allowed by name. It cannot run without host, user and cmd, and password
// or key_file, to log in with.
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
		Required:  [][]string{{"host"}, {"user"}, {"cmd"}, {"password", "key_file"}},
	}
}
