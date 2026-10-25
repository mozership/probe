package smtp

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, ReqTypes(), ResTypes())
	addr := startMockServer(t, false)
	a := &Action{log: hclog.NewNullLogger()}
	for _, with := range []map[string]any{
		{"addr": addr, "from": "from@example.com", "to": "to@example.com", "subject": "hi", "myhostname": "probe.test", "starttls": "off"},
		{"addr": addr, "from": "from@example.com", "to": "to@example.com", "session": 2, "message": 2, "length": 64},
	} {
		ret, err := a.Run(with)
		if err != nil {
			t.Fatal(err)
		}
		actiontest.CheckFields(t, ReqFields(), ResFields(), nil, ret)
		kinds.Check(ret)
	}
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"addr": "127.0.0.1:1", "from": "from@example.com", "to": "to@example.com"}
	actiontest.CheckRequired(t, Required(), with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
