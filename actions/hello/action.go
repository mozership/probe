package hello

import (
	"maps"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
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

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the hello action. It keeps to every kind of guard, as it
// neither writes nor connects to anything, takes any key in with, and needs
// none. It returns the keys of with as its req, and in its res too, where it
// sets status and dump over the keys of the same names. Only those two are
// of a kind known, as the keys of with may hold anything.
var Spec = actionspec.Spec{
	Serve:      serve,
	Keeps:      []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
	AnyParams:  true,
	Contracts:  actionspec.NoContracts,
	ReqFields:  []string{},
	ResFields:  []string{"dump", "status"},
	ReqTypes:   map[string]string{},
	ResTypes:   map[string]string{"dump": kind.Bool, "status": kind.Number},
	WithFields: true,
	Required:   actionspec.NoneRequired,
}
