package grpc

import (
	"maps"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/action"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec().ReqTypes, Spec().ResTypes)
	grpcAddr := startUserServer(t)
	s := startConnectServer(t, "")
	files := protoOf(writeProto(t, userProto(t)), false)
	a := &Action{log: hclog.NewNullLogger()}

	for _, protocol := range []map[string]any{
		{"addr": grpcAddr, "metadata": map[string]any{"x-probe": "1"}, "timeout": "5s"},
		{"protocol": "connect", "addr": s.url, "proto": files},
		{"protocol": "connect", "addr": s.url, "codec": "proto", "proto": files},
	} {
		for _, call := range []map[string]any{
			{"method": "GetUser", "body": `{"user_id": "1"}`},
			{"method": "WatchUsers", "body": `{"count": 2}`, "max_messages": 5},
			{"method": "GetUser", "body": `{"user_id": "1"}`, "proto": files},
		} {
			with := map[string]any{"service": "UserService"}
			maps.Copy(with, protocol)
			maps.Copy(with, call)
			ret, _, err := a.RunStep(action.Call{With: with, Contract: &action.Contract{}})
			if err != nil {
				t.Fatalf("%v: %v", with, err)
			}
			actiontest.CheckFields(t, Spec().ReqFields, Spec().ResFields, nil, ret)
			kinds.Check(ret)
		}
	}
	// A listing of the services, and the description of a service.
	for _, with := range []map[string]any{
		{"addr": grpcAddr, "list": true},
		{"addr": grpcAddr, "list": true, "service": "UserService"},
	} {
		ret, _, err := a.RunStep(action.Call{With: with, Contract: &action.Contract{}})
		if err != nil {
			t.Fatalf("%v: %v", with, err)
		}
		actiontest.CheckFields(t, Spec().ReqFields, Spec().ResFields, nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"addr": "127.0.0.1:1", "service": "UserService", "method": "GetUser", "timeout": "1s"}
	actiontest.CheckRequired(t, Spec().Required, with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
