package dns

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec().ReqTypes, Spec().ResTypes)
	server, _, _ := startServer(t, false)
	a := &Action{log: hclog.NewNullLogger()}
	for _, with := range []map[string]any{
		{"name": "example.test", "server": server, "timeout": "2s"},
		{"name": "www.example.test", "type": "A", "server": server, "protocol": "tcp"},
		{"name": "missing.example.test", "server": server},
	} {
		ret, err := a.Run(with)
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
	actiontest.CheckRequired(t, Spec().Required, map[string]any{"name": "example.test", "server": "127.0.0.1:1"}, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
