package http

import (
	"bytes"
	hp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

// TestActionLogsHideCookies checks that the records the action logs while it
// runs hide the cookies sent and received, which the runner learns only once
// it has the result, and still say what was sent and what came back.
func TestActionLogsHideCookies(t *testing.T) {
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		hp.SetCookie(w, &hp.Cookie{Name: "session", Value: "server-set-value", Path: "/"})
		w.WriteHeader(hp.StatusTeapot)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	a := &Action{log: hclog.New(&hclog.LoggerOptions{Output: &buf, Level: hclog.Debug, JSONFormat: true})}
	if _, _, err := a.RunStep(actionrpc.Call{With: map[string]any{
		"url":     srv.URL,
		"get":     "/path",
		"headers": map[string]any{"cookie": "theme=header-sent-value", "x-trace": "abc"},
	}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, v := range []string{"server-set-value", "header-sent-value"} {
		if strings.Contains(out, v) {
			t.Errorf("the log should not hold %q, got %s", v, out)
		}
	}
	for _, want := range []string{"http request prepared", "/path", "X-Trace", "http response received", "418"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log should hold %q, got %s", want, out)
		}
	}
	if strings.Contains(out, "don't serialize") {
		t.Errorf("the records should be logged whole, got %s", out)
	}
}
