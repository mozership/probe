package actionrpc

import (
	"reflect"
	"testing"
	"time"
)

type paramsReq struct {
	URL     string            `map:"url" validate:"required"`
	Retries int               `map:"retries"`
	Debug   bool              `map:"debug"`
	Headers map[string]string `map:"headers"`
	Auth    paramsAuth        `map:"auth"`
}

type paramsAuth struct {
	User string `map:"user"`
}

func TestDecodeParams(t *testing.T) {
	req := paramsReq{Headers: map[string]string{"user-agent": "probe"}}
	err := DecodeParams(map[string]any{
		"url":     "https://example.com",
		"retries": "3",
		"debug":   1,
		"headers": map[string]any{"x-retry": 2, "x-empty": nil},
		"auth":    map[string]any{"user": "probe"},
	}, &req)
	if err != nil {
		t.Fatal(err)
	}
	want := paramsReq{
		URL:     "https://example.com",
		Retries: 3,
		Debug:   true,
		Headers: map[string]string{"user-agent": "probe", "x-retry": "2", "x-empty": ""},
		Auth:    paramsAuth{User: "probe"},
	}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("DecodeParams() = %+v, want %+v", req, want)
	}
}

func TestDecodeParamsRefuses(t *testing.T) {
	tests := []struct {
		name string
		with map[string]any
		want string
	}{
		{"a required parameter missing", map[string]any{}, "required field 'url' is missing"},
		{"a fraction for an int", map[string]any{"url": "u", "retries": 1.5}, "cannot convert 1.5 to int for field 'retries': not a whole number"},
		{"a bool that is not 0 or 1", map[string]any{"url": "u", "debug": 2}, "cannot convert 2 to bool for field 'debug': use true or false"},
		{"a list in a map of strings", map[string]any{"url": "u", "headers": map[string]any{"x": []any{1}}}, "cannot convert a list to a string for 'x' in field 'headers'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecodeParams(tt.with, &paramsReq{})
			if err == nil || err.Error() != tt.want {
				t.Errorf("DecodeParams() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEncodeResult(t *testing.T) {
	type res struct {
		Code int        `map:"code"`
		Body []byte     `map:"body"`
		Auth paramsAuth `map:"auth"`
	}
	got, err := EncodeResult(&res{Code: 200, Body: []byte("ok"), Auth: paramsAuth{User: "probe"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"code": 200, "body": "ok", "auth": map[string]any{"user": "probe"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EncodeResult() = %#v, want %#v", got, want)
	}
}

func TestParamNames(t *testing.T) {
	want := []string{"url", "retries", "debug", "headers", "auth"}
	if got := ParamNames(paramsReq{}); !reflect.DeepEqual(got, want) {
		t.Errorf("ParamNames() = %v, want %v", got, want)
	}
	if got := ParamNames(&paramsReq{}); !reflect.DeepEqual(got, want) {
		t.Errorf("ParamNames(pointer) = %v, want %v", got, want)
	}
	if got := ParamNames(1); got != nil {
		t.Errorf("ParamNames(1) = %v, want nil", got)
	}
}

func TestParseTimeout(t *testing.T) {
	for in, want := range map[any]time.Duration{"30s": 30 * time.Second, 5: 5 * time.Second, "1.5": 1500 * time.Millisecond} {
		got, err := ParseTimeout(in)
		if err != nil || got != want {
			t.Errorf("ParseTimeout(%#v) = %v, %v; want %v", in, got, err, want)
		}
	}
	for in, want := range map[any]string{
		"soon": "invalid timeout soon: use a duration such as 30s, or a number of seconds",
		0:      "invalid timeout 0: a timeout must be above zero",
	} {
		if _, err := ParseTimeout(in); err == nil || err.Error() != want {
			t.Errorf("ParseTimeout(%#v) error = %v, want %q", in, err, want)
		}
	}
}
