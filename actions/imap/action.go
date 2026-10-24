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
		a.log.Trace("imap request prepared", "request", req)
	})
	after := WithAfter(func(res *Res) {
		a.log.Trace("imap response received", "response", res)
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

// ReqFields returns the fields of the req the imap action returns, those of
// Req, and ResFields those of its res, those of Res, for probe check to
// report a step that reads another.
func ReqFields() []string {
	return mapping.FieldTags(Req{})
}

// ResFields returns the fields of the res the imap action returns; see
// ReqFields.
func ResFields() []string {
	return mapping.FieldTags(Res{})
}

// Required returns the keys of with the imap action cannot run without:
// host, username and password. port is 993 when it is not given.
func Required() [][]string {
	return [][]string{{"host"}, {"username"}, {"password"}}
}
