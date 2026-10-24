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
// The runner shows the action's records by their levels:
//
//   - Warn is shown in every run. It is the level of what a person must see
//     whatever the step's result says.
//   - Error is shown only in a verbose run (-v): the error an action returns
//     is what the step's result shows, as it is, so it is to say why the
//     action failed in words a person reads, and a record at error level
//     would only say it again.
//   - Debug and Info are shown in a verbose run, alongside the step's request
//     and response, which the runner shows itself. They are for what the
//     action did that its result does not tell.
//   - Trace is shown only with -vv, along with how the runner started,
//     called and stopped the action's process. It is for what helps to
//     debug the action, such as the parameters it received and the result
//     it returned, or the error, which LogParams and LogOutcome log.
//
// The runner marks the errors it sends as the action's own, apart from a
// lost connection. Of an action built against a version of this package
// from before that, an error with the code codes.Unavailable is taken for
// a lost connection until the runner has seen the process go on for a
// moment, which delays the step; building the action against this version
// spares it that.
func Serve(newAction func(log hclog.Logger) Action) {
	log := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         map[string]plugin.Plugin{"actions": &Plugin{Impl: newAction(log)}},
		GRPCServer:      plugin.DefaultGRPCServer,
		// go-plugin's own records, such as the address it serves on, are
		// at trace level, as they are in the runner.
		Logger: pluginLog{Logger: log},
	})
}
