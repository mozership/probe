package actionrpc

import (
	"slices"
	"testing"
)

func TestFieldsOf(t *testing.T) {
	f := Fields{Req: []string{"url"}, Res: []string{"status", "code"}}
	req, res := f.Of(map[string]any{"name": "x"})
	if !slices.Equal(req, []string{"url"}) || !slices.Equal(res, []string{"code", "status"}) {
		t.Errorf("Of() = %v, %v; want the fields declared, sorted, without the keys of with", req, res)
	}
	f.With = true
	req, res = f.Of(map[string]any{"name": "x", "status": 1})
	if !slices.Equal(req, []string{"name", "status", "url"}) || !slices.Equal(res, []string{"code", "name", "status"}) {
		t.Errorf("Of() = %v, %v; want the keys of with among them, once each", req, res)
	}
}

func TestFieldsUndeclared(t *testing.T) {
	f := Fields{Req: []string{"url"}, Res: []string{"code"}}
	ret := map[string]any{
		"req":    map[string]any{"url": "u", "method": "GET"},
		"res":    map[string]any{"code": 200, "body": "", "headers": nil},
		"status": 0,
	}
	want := []string{"req.method", "res.body", "res.headers"}
	if got := f.Undeclared(nil, ret); !slices.Equal(got, want) {
		t.Errorf("Undeclared() = %v, want %v", got, want)
	}
}
