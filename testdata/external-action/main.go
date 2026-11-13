// Command external-action serves an action from outside the probe binary, the
// way a community action does. It greets the name it is given, and logs at
// debug and trace level as it does. It leaves the notes it is given, as
// with.notes, for the steps after it.
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
	res := map[string]any{"greeting": fmt.Sprintf("hello %v", with["name"])}
	if notes, ok := with["notes"]; ok {
		res["notes"] = notes
	}
	return map[string]any{
		"req":    with,
		"res":    res,
		"status": 0,
	}, nil
}

func main() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action { return action{log: log} })
}
