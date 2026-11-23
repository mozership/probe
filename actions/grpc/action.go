package grpc

import (
	"context"
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
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep makes the call for a step, under the guard of the run, which
// refuses a host it does not allow and, under --read-only, a method its
// definitions do not declare free of side effects.
func (a *Action) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	with := call.With
	// Validate that required parameters are provided
	if len(with) == 0 {
		return map[string]any{}, nil, errors.New("grpc action requires parameters in 'with' section. Please specify request details like addr, service, method")
	}

	actionrpc.LogParams(a.log, "received grpc request parameters", with)

	before := WithBefore(func(ctx context.Context, service, method string) {
		a.log.Trace("grpc request prepared", "service", service, "method", method)
	})
	after := WithAfter(func(res *Res) {
		a.log.Trace("grpc response received", "status", res.StatusCode)
	})
	ret, err := RequestStep(call, before, after)

	actionrpc.LogOutcome(a.log, "grpc request", ret, err)

	return ret, nil, err
}

func serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Spec declares the grpc action. It keeps to every kind of guard: it calls
// only a method declared without side effects under read-only, and
// connects only to a host the guard allows. proto, the .proto files of the
// service, which it reads apart from Req, asks it to check a step against
// them. Its res holds, besides the fields of Res, violations and contract,
// for a call checked against .proto files, and messages and complete, for
// one whose responses stream, and services, methods and types, for a
// listing. It cannot run without addr, nor without service and method
// unless list asks for a listing.
func Spec() actionspec.Spec {
	return actionspec.Spec{
		Serve:     serve,
		Keeps:     []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost},
		Params:    append(mapping.FieldTags(Req{}), "proto"),
		Contracts: []string{"proto"},
		ReqFields: mapping.FieldTags(Req{}),
		ResFields: append(mapping.FieldTags(Res{}), "complete", "contract", "messages", "methods", "services", "types", "violations"),
		ReqTypes:  kind.Fields(Req{}),
		ResTypes:  resTypes(),
		Required:  [][]string{{"addr"}, {"service", "list"}, {"method", "list"}},
		Choices: map[string]actionspec.Choices{
			"protocol": {Values: []string{"grpc", "connect"}},
			"codec":    {Values: []string{"json", "proto"}},
		},
	}
}

// resTypes returns the kinds of the fields of Res and of those the action
// adds to its res.
func resTypes() map[string]string {
	types := kind.Fields(Res{})
	types["complete"] = kind.Bool
	types["contract"] = kind.Map
	types["messages"] = kind.List
	types["methods"] = kind.List
	types["services"] = kind.List
	types["types"] = kind.Map
	types["violations"] = kind.List
	return types
}
