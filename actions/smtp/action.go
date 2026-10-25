package smtp

import (
	"errors"

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
	ret, err := Deliver(with, before, after)

	actionrpc.LogOutcome(a.log, "email delivery", ret, err)

	return ret, err
}

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the smtp action. It keeps to no guard, as it does not check
// what it is about to do against one, so that a guard refuses it unless it
// is allowed by name. It cannot run without addr, from and to.
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
		Required:  [][]string{{"addr"}, {"from"}, {"to"}},
	}
}
