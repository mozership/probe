package hello

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	with := map[string]any{"name": "probe", "count": 2}
	ret, err := (&Action{log: hclog.NewNullLogger()}).Run(with)
	if err != nil {
		t.Fatal(err)
	}
	actiontest.CheckFields(t, ReqFields(), ResFields(), with, ret)
	if res, _ := ret["res"].(map[string]any); res["name"] != "probe" {
		t.Errorf("res = %v, want the keys of with in it", res)
	}
}
