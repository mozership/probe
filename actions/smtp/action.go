package smtp

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, errors.New("smtp action requires parameters in 'with' section. Please specify email details like addr, from, to")
	}

	actionrpc.LogParams(a.log, "received smtp request parameters", with)

	before := WithBefore(func(from string, to string, subject string) {
		a.log.Trace("email prepared", "from", from, "to", to, "subject", subject)
	})
	after := WithAfter(func(result *Result) {
		a.log.Trace("email delivery completed", "result", result)
	})
	ret, err := Send(with, before, after)

	actionrpc.LogOutcome(a.log, "email delivery", ret, err)

	return ret, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Params returns the keys the smtp action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}
