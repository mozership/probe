// Command external-action serves an action from outside the probe binary, the
// way a community action does. It greets the name it is given.
package main

import (
	"fmt"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

type action struct{}

func (action) Run(with map[string]any) (map[string]any, error) {
	return map[string]any{
		"req":    with,
		"res":    map[string]any{"greeting": fmt.Sprintf("hello %v", with["name"])},
		"status": 0,
	}, nil
}

func main() {
	actionrpc.Serve(func(hclog.Logger) actionrpc.Action { return action{} })
}
