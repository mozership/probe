package hello

import (
	"maps"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	start := time.Now()
	a.log.Info("Hello!")

	// Create response data
	res := make(map[string]any)
	maps.Copy(res, with)
	res["status"] = 0   // Always success for hello action (ExitStatusSuccess)
	res["dump"] = false // Don't dump request/response for hello action

	// Return in expected structure
	result := map[string]any{
		"req":    with,
		"res":    res,
		"rt":     time.Since(start).String(),
		"status": 0,
	}

	return result, nil
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Keeps returns the kinds of guard the hello action keeps to: all of them,
// as it neither writes nor connects to anything.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// ReqFields returns the fields of the req the hello action returns besides
// the keys of with, which it returns as req: none.
func ReqFields() []string {
	return []string{}
}

// ResFields returns the fields of the res the hello action returns besides
// the keys of with, which it returns in res too: status and dump.
func ResFields() []string {
	return []string{"dump", "status"}
}
