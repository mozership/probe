package probe

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/jsonutil"
)

func TestReadActionResult(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		want actionResult
	}{
		{
			name: "every part",
			m: map[string]any{
				"req":    map[string]any{"url": "http://example.com"},
				"res":    map[string]any{"code": 200},
				"rt":     "1.5s",
				"status": 0,
			},
			want: actionResult{
				Req:    map[string]any{"url": "http://example.com"},
				Res:    map[string]any{"code": 200},
				RT:     ResponseTime{Duration: "1.5s", Sec: 1.5},
				Status: 0,
			},
		},
		{name: "nothing, which is a failure", m: map[string]any{}, want: actionResult{Status: 1}},
		{name: "a status sent as a number", m: map[string]any{"status": int64(1)}, want: actionResult{Status: 1}},
		{name: "a status sent as text", m: map[string]any{"status": "0"}, want: actionResult{Status: 0}},
		{name: "an rt that is not a duration", m: map[string]any{"rt": "soon", "status": 0}, want: actionResult{}},
		{name: "parts of the wrong kind", m: map[string]any{"req": "GET /", "res": []any{1}, "rt": 5, "status": 0}, want: actionResult{}},
		{
			name: "a JSON body",
			m:    map[string]any{"res": map[string]any{"body": `{"id": 1}`}, "status": 0},
			want: actionResult{Res: map[string]any{"body": jsonutil.Decode(`{"id": 1}`), "rawbody": `{"id": 1}`}},
		},
		{
			name: "a JSON stdout",
			m:    map[string]any{"res": map[string]any{"stdout": `[1, 2]`}, "status": 0},
			want: actionResult{Res: map[string]any{"stdout": `[1, 2]`, "json": jsonutil.Decode(`[1, 2]`)}},
		},
		{
			name: "a json the action returned itself",
			m:    map[string]any{"res": map[string]any{"stdout": `[1]`, "json": "kept"}, "status": 0},
			want: actionResult{Res: map[string]any{"stdout": `[1]`, "json": "kept"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := readActionResult(tt.m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("readActionResult() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// A step whose action returned no rt has none in its context, rather than
// that of an earlier attempt.
func TestStep_processActionResult_ReplacesTheContext(t *testing.T) {
	st := &stepRun{Step: &Step{}}
	jCtx := &JobContext{runEnv: &runEnv{Printer: newBufferPrinter()}}

	st.processActionResult(readActionResult(map[string]any{"res": map[string]any{"code": 500}, "rt": "2s", "status": 1}), jCtx)
	st.processActionResult(readActionResult(map[string]any{"res": map[string]any{"code": 200}, "status": 0}), jCtx)

	if st.ctx.RT != (ResponseTime{}) {
		t.Errorf("RT = %+v, want none", st.ctx.RT)
	}
	if st.ctx.Status != 0 || st.ctx.Res["code"] != 200 {
		t.Errorf("ctx = %+v, want the second result", st.ctx)
	}
}

// What readActionResult adds to res is among the fields addedResFields
// tells check of, for each field it comes from.
func TestAddedResFields(t *testing.T) {
	for _, res := range []map[string]any{
		{"body": `{"id": 1}`, "code": 200},
		{"stdout": `[1, 2]`, "stderr": ""},
		{"body": `{"id": 1}`, "stdout": `{"ok": true}`},
		{"body": "text", "stdout": "text"},
	} {
		declared := slices.Collect(maps.Keys(res))
		r := readActionResult(map[string]any{"res": maps.Clone(res), "status": 0})
		fields := addedResFields(declared)
		for key := range r.Res {
			if !slices.Contains(fields, key) {
				t.Errorf("res of %v gained %s, which addedResFields(%v) = %v leaves out", res, key, declared, fields)
			}
		}
	}
	if got := addedResFields([]string{"code"}); !slices.Equal(got, []string{"code"}) {
		t.Errorf("addedResFields([code]) = %v, want nothing added to a res without body or stdout", got)
	}
}

// TestStep_processActionResult checks that the context a step's test and
// echo read takes the request, the response and the time the action
// returned, with a JSON body read as the value it holds.
func TestStep_processActionResult(t *testing.T) {
	tests := []struct {
		name         string
		actionResult map[string]any
		wantBody     any
		wantRT       ResponseTime
	}{
		{
			name: "normal action result",
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{"status": 200, "body": "response"},
				"rt":  "100ms",
			},
			wantBody: "response",
			wantRT:   ResponseTime{Duration: "100ms", Sec: 0.1},
		},
		{
			name: "action result with JSON body",
			actionResult: map[string]any{
				"req": map[string]any{"url": "http://example.com"},
				"res": map[string]any{
					"status": 200,
					"body":   `{"message": "success"}`,
				},
				"rt": "150ms",
			},
			wantBody: map[string]any{"message": "success"},
			wantRT:   ResponseTime{Duration: "150ms", Sec: 0.15},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &stepRun{
				Step: &Step{},
				expr: &expr.Expr{},
			}

			jCtx := &JobContext{
				runEnv: &runEnv{},
			}

			step.processActionResult(readActionResult(tt.actionResult), jCtx)

			if got := step.ctx.Req["url"]; got != "http://example.com" {
				t.Errorf("req.url = %#v, want the url of the request", got)
			}
			if got := step.ctx.Res["status"]; got != 200 {
				t.Errorf("res.status = %#v, want 200", got)
			}
			if got := step.ctx.Res["body"]; !reflect.DeepEqual(got, tt.wantBody) {
				t.Errorf("res.body = %#v, want %#v", got, tt.wantBody)
			}
			if step.ctx.RT != tt.wantRT {
				t.Errorf("rt = %+v, want %+v", step.ctx.RT, tt.wantRT)
			}
		})
	}
}

func TestParseExitStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{"nil", nil, int(ExitStatusFailure)},
		{"int 0", int(0), int(ExitStatusSuccess)},
		{"int 1", int(1), int(ExitStatusFailure)},
		{"int64 0", int64(0), int(ExitStatusSuccess)},
		{"int64 1", int64(1), int(ExitStatusFailure)},
		{"float64 0", float64(0), int(ExitStatusSuccess)},
		{"float64 1", float64(1), int(ExitStatusFailure)},
		{"string 0", "0", int(ExitStatusSuccess)},
		{"string 1", "1", int(ExitStatusFailure)},
		{"ExitStatus success", ExitStatusSuccess, int(ExitStatusSuccess)},
		{"ExitStatus failure", ExitStatusFailure, int(ExitStatusFailure)},
		{"unknown type", []int{0}, int(ExitStatusFailure)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseExitStatus(tt.input)
			if result != tt.expected {
				t.Errorf("parseExitStatus(%v) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStep_processActionResultStdoutJSON(t *testing.T) {
	tests := []struct {
		name   string
		res    map[string]any
		want   any
		wantOK bool
	}{
		{
			name:   "an object with a trailing newline",
			res:    map[string]any{"stdout": "{\"id\":\"m1\",\"count\":2}\n"},
			want:   map[string]any{"id": "m1", "count": float64(2)},
			wantOK: true,
		},
		{
			name:   "an array",
			res:    map[string]any{"stdout": ` ["a", {"b": true}] `},
			want:   []any{"a", map[string]any{"b": true}},
			wantOK: true,
		},
		{name: "plain text", res: map[string]any{"stdout": "hello\n"}},
		{name: "empty", res: map[string]any{"stdout": ""}},
		{name: "JSON Lines", res: map[string]any{"stdout": "{\"a\":1}\n{\"a\":2}\n"}},
		{name: "braces that are not JSON", res: map[string]any{"stdout": "{key: value}"}},
		{name: "a scalar", res: map[string]any{"stdout": "42\n"}},
		{name: "no stdout", res: map[string]any{"code": 0}},
		{name: "stdout that is not a string", res: map[string]any{"stdout": 1}},
		{
			name:   "a json field the action set itself",
			res:    map[string]any{"stdout": `{"a":1}`, "json": "the action's own"},
			want:   "the action's own",
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, hadStdout := tt.res["stdout"]
			step := &stepRun{Step: &Step{}, expr: &expr.Expr{}}
			jCtx := &JobContext{
				runEnv: &runEnv{
					Printer: newBufferPrinter(),
				},
			}
			step.processActionResult(readActionResult(map[string]any{"res": tt.res}), jCtx)

			got, ok := step.ctx.Res["json"]
			if ok != tt.wantOK {
				t.Fatalf("res.json present = %v, want %v (%#v)", ok, tt.wantOK, got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("res.json = %#v, want %#v", got, tt.want)
			}
			if hadStdout && !reflect.DeepEqual(step.ctx.Res["stdout"], stdout) {
				t.Errorf("res.stdout = %#v, want it kept as %#v", step.ctx.Res["stdout"], stdout)
			}
		})
	}
}
