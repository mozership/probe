package dns

import (
	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/mapping"
)

type Action struct {
	log hclog.Logger
}

func (a *Action) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep sends the query under the guard of the run, which may allow only
// some hosts.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	actionrpc.LogParams(a.log, "received dns request parameters", call.With)

	ret, err := Request(call.With, WithGuard(call.Guard))
	actionrpc.LogOutcome(a.log, "dns query", ret, err)

	return ret, nil, err
}

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Keeps returns the kinds of guard the dns action keeps to: a query writes
// nothing, and it is sent only to a server the guard allows.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// Params returns the keys the dns action takes in with.
func Params() []string {
	return mapping.FieldTags(Req{})
}

// ReqFields returns the fields of the req the dns action returns, those of
// Req, and ResFields those of its res, for probe check to report a step that
// reads another.
func ReqFields() []string {
	return mapping.FieldTags(Req{})
}

// ResFields returns the fields of the res the dns action returns; see
// ReqFields.
func ResFields() []string {
	return []string{"answers", "authoritative", "protocol", "rcode", "server", "values"}
}

// Required returns the keys of with the dns action cannot run without: name.
func Required() [][]string {
	return [][]string{{"name"}}
}
