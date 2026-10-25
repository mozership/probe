package mapping

import (
	"reflect"
	"testing"
	"time"

	"github.com/mozership/probe/internal/duration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTimeout(t *testing.T) {
	tests := []struct {
		in      any
		want    time.Duration
		wantErr bool
	}{
		{"30s", 30 * time.Second, false},
		{"1m30s", 90 * time.Second, false},
		{" 2 ", 2 * time.Second, false},
		{"1.5", 1500 * time.Millisecond, false},
		{2, 2 * time.Second, false},
		{int64(3), 3 * time.Second, false},
		{0.25, 250 * time.Millisecond, false},
		{10 * time.Second, 10 * time.Second, false},
		{"soon", 0, true},
		{0, 0, true},
		{"-1s", 0, true},
		{true, 0, true},
		{20_000_000_000, 0, true}, // would wrap to about 49 years
		{int64(duration.MaxSeconds) + 1, 0, true},
		{1e20, 0, true},
		{"1e20", 0, true},
		{"NaN", 0, true},
		{-5, 0, true},
		{time.Duration(0), 0, true},
		{1e-10, 0, true},          // below a nanosecond: would truncate to zero
		{"0.0000000001", 0, true}, // the same as a numeric string
		{1e-9, time.Nanosecond, false},
		{4.1, 4100 * time.Millisecond, false},
		{"8.2", 8200 * time.Millisecond, false},
		{int64(duration.MaxSeconds), time.Duration(duration.MaxSeconds) * time.Second, false},
	}
	for _, tt := range tests {
		got, err := ParseTimeout(tt.in)
		if tt.wantErr {
			assert.Error(t, err, "%#v", tt.in)
			continue
		}
		require.NoError(t, err, "%#v", tt.in)
		assert.Equal(t, tt.want, got, "%#v", tt.in)
	}
}

// The error of a refused timeout names it and says why.
func TestParseTimeoutSaysWhy(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"soon", "invalid timeout soon: use a duration such as 30s, or a number of seconds"},
		{"0", "invalid timeout 0: a timeout must be above zero"},
		{0, "invalid timeout 0: a timeout must be above zero"},
		{"-1s", "invalid timeout -1s: a duration cannot be below zero"},
		{1e20, "invalid timeout 1e+20: a duration cannot be longer than about 292 years"},
		{"0.0000000001", "invalid timeout 0.0000000001: a duration above zero cannot be shorter than a nanosecond"},
	}
	for _, tt := range tests {
		_, err := ParseTimeout(tt.in)
		require.Error(t, err, "%#v", tt.in)
		assert.Equal(t, tt.want, err.Error(), "%#v", tt.in)
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		char     string
		expected string
	}{
		{"hyphen separated", "content-type", "-", "Content-Type"},
		{"underscore separated", "user_name", "_", "User_Name"},
		{"single word", "hello", "-", "Hello"},
		{"empty string", "", "-", ""},
		{"multiple separators", "a-b-c-d", "-", "A-B-C-D"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TitleCase(tt.input, tt.char)
			if result != tt.expected {
				t.Errorf("TitleCase() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEnvToStringValue(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want map[string]any
	}{
		{
			name: "nested map values become strings",
			data: map[string]any{
				"cmd": "run",
				"env": map[string]any{"PORT": 8080, "RATIO": 0.5, "DEBUG": true, "NAME": "x", "EMPTY": nil},
			},
			want: map[string]any{
				"cmd": "run",
				"env": map[string]any{"PORT": "8080", "RATIO": "0.5", "DEBUG": "true", "NAME": "x", "EMPTY": ""},
			},
		},
		{
			name: "flat keys are folded into env",
			data: map[string]any{"cmd": "run", "env__A": "1", "env__B": 2},
			want: map[string]any{"cmd": "run", "env": map[string]any{"A": "1", "B": "2"}},
		},
		{
			name: "nested map wins over a flat key",
			data: map[string]any{"env": map[string]any{"MODE": "nested"}, "env__MODE": "flat"},
			want: map[string]any{"env": map[string]any{"MODE": "nested"}},
		},
		{
			name: "map of strings is accepted",
			data: map[string]any{"env": map[string]string{"A": "1"}},
			want: map[string]any{"env": map[string]any{"A": "1"}},
		},
		{
			name: "a bare prefix is not a variable",
			data: map[string]any{"env__": "x"},
			want: map[string]any{"env__": "x"},
		},
		{
			name: "no env at all",
			data: map[string]any{"cmd": "run"},
			want: map[string]any{"cmd": "run"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvToStringValue(tt.data)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("EnvToStringValue() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEnvToStringValue_LeavesInputAlone(t *testing.T) {
	data := map[string]any{"env__A": "1", "env": map[string]any{"B": 2}}
	_ = EnvToStringValue(data)

	if _, ok := data["env__A"]; !ok {
		t.Error("the flat key should stay in the input")
	}
	if data["env"].(map[string]any)["B"] != 2 {
		t.Error("the nested map in the input should keep its original value")
	}
}

// TestEnvToStringValue_MapsOntoStruct checks the result lands in a
// map[string]string field, numbers included, which is what the shell and ssh
// requests declare.
func TestEnvToStringValue_MapsOntoStruct(t *testing.T) {
	type req struct {
		Env map[string]string `map:"env"`
	}
	r := &req{Env: map[string]string{}}
	err := MapToStructByTags(EnvToStringValue(map[string]any{
		"env":      map[string]any{"PORT": 8080},
		"env__APP": "probe",
	}), r)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"PORT": "8080", "APP": "probe"}; !reflect.DeepEqual(r.Env, want) {
		t.Errorf("Env = %v, want %v", r.Env, want)
	}
}
