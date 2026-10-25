package imap

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	kinds := actiontest.NewKinds(t, ReqTypes(), ResTypes())
	host, port := plainIMAPServer(t, 2)
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{
		"host": host, "port": port, "username": "user", "password": "pass", "tls": false, "timeout": "5s",
		"commands": []any{
			map[string]any{"name": "select", "mailbox": "INBOX"},
			map[string]any{"name": "search", "criteria": map[string]any{"flags": []any{}}},
			map[string]any{"name": "fetch", "sequence": "1:2", "dataitem": "FLAGS"},
			map[string]any{"name": "noop"},
		},
	}
	ret, err := a.Run(with)
	if err != nil {
		t.Fatal(err)
	}
	actiontest.CheckFields(t, ReqFields(), ResFields(), nil, ret)
	kinds.Check(ret)
	kinds.Covered()
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	with := map[string]any{"host": "127.0.0.1", "port": 1, "username": "user", "password": "pass", "tls": false, "timeout": "1s"}
	actiontest.CheckRequired(t, Required(), with, func(with map[string]any) error {
		_, err := a.Run(with)
		return err
	})
}
