package ssh

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, Spec().ReqTypes, Spec().ResTypes)
	a := &Action{log: hclog.NewNullLogger()}
	for _, handle := range []handler{echo, func(session) (uint32, bool) { return 3, true }} {
		srv := newTestServer(t, handle)
		with := map[string]any{
			"host":              srv.host(),
			"port":              srv.port(),
			"user":              testUser,
			"password":          testPassword,
			"cmd":               "uptime",
			"strict_host_check": false,
			"workdir":           "/tmp",
			"timeout":           "5s",
			"env":               map[string]any{"PROBE_STAGE": "e2e"},
		}
		ret, err := a.Run(with)
		if err != nil {
			t.Fatal(err)
		}
		actiontest.CheckFields(t, Spec().ReqFields, Spec().ResFields, nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"host": "127.0.0.1", "port": 1, "user": testUser, "password": testPassword, "cmd": "uptime", "timeout": "1s"}
	actiontest.CheckRequired(t, Spec().Required, with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
