package ssh

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
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

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Params returns the keys the ssh action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}

// ReqFields returns the fields of the req the ssh action returns, those of
// Req, and ResFields those of its res, those of Res, for probe check to
// report a step that reads another.
func ReqFields() []string {
	return mapping.FieldTags(Req{})
}

// ResFields returns the fields of the res the ssh action returns; see
// ReqFields.
func ResFields() []string {
	return mapping.FieldTags(Res{})
}

// ReqTypes returns the kinds of the fields of the req the ssh action
// returns, those of Req, and ResTypes those of its res, those of Res, for
// probe check to report an expression that does with a field what its kind
// does not allow.
func ReqTypes() map[string]kind.Kind {
	return kind.Fields(Req{})
}

// ResTypes returns the kinds of the fields of the res the ssh action
// returns; see ReqTypes.
func ResTypes() map[string]kind.Kind {
	return kind.Fields(Res{})
}

// Required returns the keys of with the ssh action cannot run without: host,
// user and cmd, and password or key_file, to log in with.
func Required() [][]string {
	return [][]string{{"host"}, {"user"}, {"cmd"}, {"password", "key_file"}}
}
