package varsfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse(strings.NewReader(`{
  "body": "{\"order\": 42}",
  "receipt": "AQEB<&>",
  "count": 10,
  "big": 12345678901234567890,
  "ratio": 1.50,
  "ok": true,
  "item": {"id": 1.0, "tags": ["a", "<b>"]},
  "list": [1, "x", null],
  "empty": "",
  "cursor": null
}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"body":    `{"order": 42}`,
		"receipt": "AQEB<&>",
		"count":   "10",
		"big":     "12345678901234567890",
		"ratio":   "1.50",
		"ok":      "true",
		"item":    `{"id":1.0,"tags":["a","<b>"]}`,
		"list":    `[1,"x",null]`,
		"empty":   "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %#v, want %#v", got, want)
	}
}

// A name is taken as --var takes it, without the white space around it,
// and a file may start with the mark some editors write.
func TestParse_TrimsNamesAndSkipsTheBOM(t *testing.T) {
	got, err := Parse(strings.NewReader("\uFEFF" + `{" a ": "1", "b\t": 2}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"a": "1", "b": "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %#v, want %#v", got, want)
	}
}

func TestParse_Refuses(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"nothing", "", "no JSON object"},
		{"white space", " \n", "no JSON object"},
		{"a list", `["a"]`, "a JSON object of vars by name is needed, not a list"},
		{"a string", `"a=b"`, "not a string"},
		{"null", `null`, "not null"},
		{"two objects", `{"a": "1"} {"b": "2"}`, "more follows the JSON object"},
		{"text after", `{"a": "1"} x`, "more follows the JSON object"},
		{"not JSON", `a=1`, "invalid character"},
		{"cut short", `{"a": `, "unexpected EOF"},
		{"no name", `{"": "1"}`, "a var has no name"},
		{"a blank name", `{" ": "1"}`, "a var has no name"},
		{"a name twice", `{"a": "1", "a": null}`, "var a is given more than once"},
		{"a name twice once trimmed", `{"a": "1", " a ": "2"}`, "var a is given more than once"},
		{"not closed", `{"a": "1"`, "unexpected end of JSON input"},
		{"where it fails", `{"a": "1",, }`, "at byte 11"},
		{"a name that is not a string", `{1: "1"}`, "object member name must be a string, at byte 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse(%q) = %v, want an error saying %q", tt.input, err, tt.want)
			}
		})
	}
}

func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.json")
	if err := os.WriteFile(path, []byte(`{"a": "1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path, strings.NewReader(`{"b": "2"}`))
	if err != nil || !reflect.DeepEqual(got, map[string]string{"a": "1"}) {
		t.Errorf("Read(file) = %v, %v", got, err)
	}
	got, err = Read("-", strings.NewReader(`{"b": "2"}`))
	if err != nil || !reflect.DeepEqual(got, map[string]string{"b": "2"}) {
		t.Errorf("Read(-) = %v, %v", got, err)
	}
}

func TestRead_NamesWhatFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vars.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, nil); err == nil || !strings.HasPrefix(err.Error(), path+": ") {
		t.Errorf("Read(file) = %v, want the file named", err)
	}
	if _, err := Read("-", strings.NewReader("[]")); err == nil || !strings.HasPrefix(err.Error(), "the standard input: ") {
		t.Errorf("Read(-) = %v, want the standard input named", err)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing.json"), nil); err == nil {
		t.Error("Read of a missing file succeeded")
	}
}
