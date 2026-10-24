package actionrpc

import (
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

// Serve serves the action that newAction builds to the workflow runner that
// started this process, and returns once the runner is done with it. The
// action logs through the logger it is given, which writes JSON to stderr
// for the runner to filter.
//
// The error an action returns is what the step's result shows, as it is,
// so it is to say why the action failed in words a person reads. The
// runner shows the action's records at error level only in a verbose run,
// as they would say that error again; Warn is the level of what a person
// must see whatever the step's result says.
//
// The runner marks the errors it sends as the action's own, apart from a
// lost connection. Of an action built against a version of this package
// from before that, an error with the code codes.Unavailable is taken for
// a lost connection until the runner has seen the process go on for a
// moment, which delays the step; building the action against this version
// spares it that.
func Serve(newAction func(log hclog.Logger) Action) {
	log := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		Output:     os.Stderr,
		JSONFormat: true,
	})

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         map[string]plugin.Plugin{"actions": &Plugin{Impl: newAction(log)}},
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}
