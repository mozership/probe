package http

import (
	hp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

func TestRequestStepSendsATraceHeader(t *testing.T) {
	var got hp.Header
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	// A step without an id, as in a job run by the embedded action, is named
	// as probe names one in a workflow.
	step := actionrpc.Step{RunID: "7f3a9c21e4b05d68", JobID: "login", Index: 2, Repeat: 1, Attempt: 3}
	tests := []struct {
		name   string
		trace  any
		step   actionrpc.Step
		header string
		want   string
	}{
		{
			name:   "true sends the default header",
			trace:  true,
			step:   step,
			header: "X-Probe-Trace",
			want:   "run=7f3a9c21e4b05d68; job=login; step=step_2; repeat=1; attempt=3",
		},
		{
			name:   "a header name is used as given",
			trace:  "X-Request-Id",
			step:   step,
			header: "X-Request-Id",
			want:   "run=7f3a9c21e4b05d68; job=login; step=step_2; repeat=1; attempt=3",
		},
		{
			name:   "a step with an id is named by it",
			trace:  true,
			step:   actionrpc.Step{RunID: "r", JobID: "login", Index: 2, ID: "auth", Attempt: 1},
			header: "X-Probe-Trace",
			want:   "run=r; job=login; step=auth; repeat=0; attempt=1",
		},
		{
			name:   "values that would break the header are escaped",
			trace:  true,
			step:   actionrpc.Step{RunID: "r", JobID: "log in; now", Index: 0, ID: "ステップ", Attempt: 1},
			header: "X-Probe-Trace",
			want:   "run=r; job=log+in%3B+now; step=%E3%82%B9%E3%83%86%E3%83%83%E3%83%97; repeat=0; attempt=1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret, _, err := RequestStep(actionrpc.Call{
				With: map[string]any{"url": srv.URL, "get": "/", "trace_header": tt.trace},
				Step: tt.step,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v := got.Get(tt.header); v != tt.want {
				t.Errorf("%s = %q, want %q", tt.header, v, tt.want)
			}
			// The header shows in req, as the other headers sent do.
			headers := ret["req"].(map[string]any)["headers"].(map[string]string)
			if headers[tt.header] != tt.want {
				t.Errorf("req.headers = %#v", headers)
			}
			if _, ok := ret["req"].(map[string]any)["trace_header"]; ok {
				t.Error("trace_header should not be carried into req")
			}
		})
	}

	for _, trace := range []any{nil, false} {
		with := map[string]any{"url": srv.URL, "get": "/"}
		if trace != nil {
			with["trace_header"] = trace
		}
		if _, _, err := RequestStep(actionrpc.Call{With: with, Step: step}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v := got.Get(DefaultTraceHeader); v != "" {
			t.Errorf("with trace %v, %s = %q, want none", trace, DefaultTraceHeader, v)
		}
	}
}

func TestRequestStepTraceHeaderRejected(t *testing.T) {
	tests := []struct {
		name    string
		with    map[string]any
		wantErr string
	}{
		{
			name:    "with the same header",
			with:    map[string]any{"trace_header": true, "headers": map[string]any{"x-probe-trace": "mine"}},
			wantErr: "trace_header and a X-Probe-Trace header cannot be given together",
		},
		{
			name:    "a name that is not a header name",
			with:    map[string]any{"trace_header": "X Trace"},
			wantErr: `trace_header must be true, false or a header name, not "X Trace"`,
		},
		{
			name:    "an empty name",
			with:    map[string]any{"trace_header": ""},
			wantErr: `trace_header must be true, false or a header name, not ""`,
		},
		{
			name:    "a number",
			with:    map[string]any{"trace_header": float64(1)},
			wantErr: "trace_header must be true, false or a header name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
				called = true
			}))
			defer srv.Close()

			tt.with["url"] = srv.URL
			tt.with["method"] = "GET"
			_, _, err := RequestStep(actionrpc.Call{With: tt.with})
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error = %v, want %q", err, tt.wantErr)
			}
			if called {
				t.Error("the request should not be sent")
			}
		})
	}
}

// TestTraceIsStillTheTRACEMethod checks that trace stays the shorthand of the
// TRACE method, which trace_header is named apart from.
func TestTraceIsStillTheTRACEMethod(t *testing.T) {
	var method string
	srv := httptest.NewServer(hp.HandlerFunc(func(w hp.ResponseWriter, r *hp.Request) {
		method = r.Method
	}))
	defer srv.Close()
	if _, err := Request(map[string]any{"url": srv.URL, "trace": "/"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != hp.MethodTrace {
		t.Errorf("method = %q, want TRACE", method)
	}
}
