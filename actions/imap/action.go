package imap

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
		return map[string]any{}, errors.New("imap action requires parameters in 'with' section. Please specify connection details like host, username, password")
	}

	actionrpc.LogParams(a.log, "received imap request parameters", with)

	before := WithBefore(func(req *Req) {
		a.log.Debug("imap request prepared", "request", req)
	})
	after := WithAfter(func(res *Res) {
		a.log.Debug("imap response received", "response", res)
	})
	ret, err := Request(with, before, after)

	actionrpc.LogOutcome(a.log, "imap request", ret, err)

	return ret, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Params returns the keys the imap action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}
