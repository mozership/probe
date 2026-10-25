package imap

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
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

	// The request and the response are logged as the parameters are, with
	// the password hidden and long values such as message bodies truncated.
	before := WithBefore(func(req *Req) {
		a.logStruct("imap request prepared", req)
	})
	after := WithAfter(func(res *Res) {
		a.logStruct("imap response received", res)
	})
	ret, err := Request(with, before, after)

	actionrpc.LogOutcome(a.log, "imap request", ret, err)

	return ret, err
}

// logStruct records v, a Req or a Res, by its map tags through
// actionrpc.LogParams, which hides credentials such as the password.
func (a *Action) logStruct(msg string, v any) {
	m, err := mapping.StructToMapByTags(v)
	if err != nil {
		a.log.Trace(msg, "error", err)
		return
	}
	actionrpc.LogParams(a.log, msg, m)
}

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the imap action. It keeps to no guard, as it does not check
// what it is about to do against one, so that a guard refuses it unless it
// is allowed by name. It cannot run without host, username and password;
// port is 993 when it is not given.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     actionspec.KeepsNone(),
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: mapping.FieldTags(Res{}),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  kind.Fields(Res{}),
		Required:  [][]string{{"host"}, {"username"}, {"password"}},
	}
}
