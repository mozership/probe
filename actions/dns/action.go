package dns

import (
	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actionspec"
	"github.com/mozership/probe/internal/kind"
	"github.com/mozership/probe/internal/mapping"
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

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the dns action. It keeps to every kind of guard: a query
// writes nothing, and it is sent only to a server the guard allows. It
// cannot run without name.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
		Params:    mapping.FieldTags(Req{}),
		Contracts: actionspec.NoContracts(),
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: []string{"answers", "authoritative", "protocol", "rcode", "server", "values"},
		ReqTypes:  kind.Fields(Req{}),
		ResTypes: map[string]string{
			"answers":       kind.List,
			"authoritative": kind.Bool,
			"protocol":      kind.String,
			"rcode":         kind.String,
			"server":        kind.String,
			"values":        kind.List,
		},
		Required: [][]string{{"name"}},
	}
}
