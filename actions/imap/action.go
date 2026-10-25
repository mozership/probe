package imap

import (
	"errors"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
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

// ReqTypes returns the kinds of the fields of the req the imap action
// returns, those of Req, and ResTypes those of its res, those of Res, for
// probe check to report an expression that does with a field what its kind
// does not allow.
func ReqTypes() map[string]string {
	return kind.Fields(Req{})
}

// ResTypes returns the kinds of the fields of the res the imap action
// returns; see ReqTypes.
func ResTypes() map[string]string {
	return kind.Fields(Res{})
}

// Required returns the keys of with the imap action cannot run without:
// host, username and password. port is 993 when it is not given.
func Required() [][]string {
	return [][]string{{"host"}, {"username"}, {"password"}}
}
