package hello

import (
	"testing"

	"github.com/hashicorp/go-hclog"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	with := map[string]any{"name": "probe", "count": 2}
	ret, err := (&Action{log: hclog.NewNullLogger()}).Run(with)
	if err != nil {
		t.Fatal(err)
	}
	if got := Fields().Undeclared(with, ret); len(got) > 0 {
		t.Errorf("hello returns %v, which Fields does not declare", got)
	}
	if res, _ := ret["res"].(map[string]any); res["name"] != "probe" {
		t.Errorf("res = %v, want the keys of with in it", res)
	}
}
