package http

import (
	hp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/actiontest"
)

func TestFieldsDeclareWhatItReturns(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		hp.SetCookie(w, &hp.Cookie{Name: "session", Value: "abc", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 1, "name": "probe"}`))
	}))
	defer srv.Close()
	spec := writeSpec(t, srv.URL)

	a := &Action{log: hclog.NewNullLogger()}
	for _, with := range []map[string]any{
		{"url": srv.URL, "method": "GET", "headers": map[string]any{"accept": "application/json"}, "timeout": "5s"},
		{"url": srv.URL, "post": "/users", "body": map[string]any{"name": "probe"}, "headers": map[string]any{"content-type": "application/json"}},
		{"url": srv.URL, "post": "/form", "form": map[string]any{"name": "probe"}, "basic_auth": map[string]any{"username": "u", "password": "p"}},
		{"url": srv.URL, "post": "/upload", "multipart": map[string]any{"title": "hello"}},
		{"url": srv.URL, "get": "/", "keep_cookies": true, "cookies": map[string]any{"theme": "dark"}, "trace_header": "x-probe-step"},
		{"url": srv.URL + "/v1", "get": "/users/1", "openapi": map[string]any{"spec": spec}},
	} {
		ret, _, err := a.RunStep(actionrpc.Call{With: with, Contract: &actionrpc.Contract{}})
		if err != nil {
			t.Fatalf("%v: %v", with, err)
		}
		actiontest.CheckFields(t, Fields(), with, ret)
	}
}

func TestRequired(t *testing.T) {
	a := &Action{log: hclog.NewNullLogger()}
	run := func(with map[string]any) error {
		_, _, err := a.RunStep(actionrpc.Call{With: with})
		return err
	}
	actiontest.CheckRequired(t, Required(), map[string]any{"url": "http://127.0.0.1:1", "method": "GET", "timeout": "1s"}, run)
	// get stands for the method, and for the URL when it is one in full.
	if err := run(map[string]any{"get": "http://127.0.0.1:1/", "timeout": "1s"}); err == nil || strings.Contains(err.Error(), "missing") {
		t.Errorf("err = %v, want the request sent, and failing to connect", err)
	}
}
