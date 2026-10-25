package kind

import (
	"maps"
	"testing"
	"time"
)

func TestOf(t *testing.T) {
	tests := []struct {
		v    any
		want Kind
		ok   bool
	}{
		{int64(200), Number, true},
		{1.5, Number, true},
		{3, Number, true},
		{"OK", String, true},
		{true, Bool, true},
		{map[string]any{"a": 1}, Map, true},
		{[]any{1}, List, true},
		{nil, "", false},
		{[]string{"a"}, "", false},
		{time.Second, "", false},
	}
	for _, tt := range tests {
		got, ok := Of(tt.v)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Of(%#v) = %q, %v; want %q, %v", tt.v, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFields(t *testing.T) {
	type nested struct {
		A int `map:"a"`
	}
	type res struct {
		Code     int               `map:"code"`
		Rows     int64             `map:"rows"`
		Rate     float64           `map:"rate"`
		Status   string            `map:"status"`
		Raw      []byte            `map:"raw"`
		Timeout  time.Duration     `map:"timeout"`
		At       time.Time         `map:"at"`
		OK       bool              `map:"ok"`
		Headers  map[string]string `map:"headers"`
		Data     nested            `map:"data"`
		Items    []nested          `map:"items"`
		Params   []any             `map:"params"`
		Complete *bool             `map:"complete"`
		Body     any               `map:"body"`
		Untagged string
		hidden   string `map:"hidden"`
	}
	_ = res{}.hidden
	want := map[string]Kind{
		"code": Number, "rows": Number, "rate": Number, "status": String, "raw": String,
		"timeout": String, "at": String, "ok": Bool, "headers": Map, "data": Map,
		"items": List, "params": List, "complete": Bool,
	}
	if got := Fields(&res{}); !maps.Equal(got, want) {
		t.Errorf("Fields() = %v, want %v", got, want)
	}
	if got := Fields(1); got != nil {
		t.Errorf("Fields(1) = %v, want nil", got)
	}
}
