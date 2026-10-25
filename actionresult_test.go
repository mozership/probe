package probe

import (
	"maps"
	"reflect"
	"slices"
	"testing"

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
