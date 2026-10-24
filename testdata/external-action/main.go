// Command external-action serves an action from outside the probe binary, the
// way a community action does. It greets the name it is given, and logs at
// debug and trace level as it does.
package main

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

type action struct{ log hclog.Logger }

func (a action) Run(with map[string]any) (map[string]any, error) {
	a.log.Debug("greeting", "name", with["name"], "with", with)
	a.log.Trace("greeted")
	return map[string]any{
		"req":    with,
		"res":    map[string]any{"greeting": fmt.Sprintf("hello %v", with["name"])},
		"status": 0,
	}, nil
}

func main() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action { return action{log: log} })
}
