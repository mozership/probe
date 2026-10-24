package grpc

import (
	"context"
	"errors"

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

func Serve() {
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Action{log: log}
	})
}

// Keeps returns the kinds of guard the grpc action keeps to: it calls only
// a method declared without side effects under read-only, and connects
// only to a host the guard allows.
func Keeps() []string {
	return []string{actionrpc.KindReadOnly, actionrpc.KindAllowHost}
}

// Contracts returns the keys of with that ask the action to check a step
// against a contract: proto, the .proto files of the service.
func Contracts() []string {
	return []string{"proto"}
}

// Params returns the keys the grpc action takes in with: those of Req, and
// proto, which it reads apart from Req.
func Params() []string {
	return append(mapping.FieldTags(Req{}), "proto")
}
