package notes

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want []Note
		// errs are what the errors say, each in part, in order.
		errs []string
	}{
		{name: "nil is no notes", in: nil},
		{name: "an empty list is no notes", in: []any{}},
		{
			name: "every level, in order",
			in: []any{
				map[string]any{"level": "critical", "title": "Shipping form", "body": "Rejects **94105**."},
				map[string]any{"level": "warning", "body": "Slow."},
				map[string]any{"level": "info", "title": nil, "body": "Done in 22 steps."},
			},
			want: []Note{
				{Level: Critical, Title: "Shipping form", Body: "Rejects **94105**."},
				{Level: Warning, Body: "Slow."},
				{Level: Info, Body: "Done in 22 steps."},
			},
		},
		{
			name: "a list of objects as a Go action returns it",
			in:   []map[string]any{{"level": "info", "body": "b"}},
			want: []Note{{Level: Info, Body: "b"}},
		},
		{
			name: "fields other than those of a note are ignored",
			in:   []any{map[string]any{"level": "info", "body": "b", "severity": "high"}},
			want: []Note{{Level: Info, Body: "b"}},
		},
		{
			name: "the body is kept as it is",
			in:   []any{map[string]any{"level": "info", "body": "\n  - a\n  - b\n"}},
			want: []Note{{Level: Info, Body: "\n  - a\n  - b\n"}},
		},
		{name: "a string is not a list", in: "Found nothing", errs: []string{"notes is a string, not a list of notes"}},
		{name: "an object is not a list", in: map[string]any{"level": "info", "body": "b"}, errs: []string{"notes is an object"}},
		{name: "a number is not a list", in: 1.0, errs: []string{"notes is a number"}},
		{
			name: "the notes that are not are left out, and the others kept",
			in: []any{
				"a string",
				map[string]any{"level": "high", "body": "b"},
				map[string]any{"level": "info", "body": "kept"},
				map[string]any{"body": "b"},
				map[string]any{"level": 1.0, "body": "b"},
				map[string]any{"level": "info"},
				map[string]any{"level": "info", "body": " \n\t"},
				map[string]any{"level": "info", "body": []any{"b"}},
				map[string]any{"level": "info", "body": "b", "title": 1.0},
				map[string]any{"level": "Info", "body": "b"},
			},
			want: []Note{{Level: Info, Body: "kept"}},
			errs: []string{
				"notes[0]: is a string, not a note",
				`notes[1]: level "high" is not one of info, warning or critical`,
				"notes[3]: has no level; give one of info, warning or critical",
				"notes[4]: level is a number, not a string",
				"notes[5]: has no body",
				"notes[6]: has no body",
				"notes[7]: body is a list, not a string",
				"notes[8]: title is a number, not a string",
				`notes[9]: level "Info" is not one of`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := Parse(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() notes = %#v, want %#v", got, tt.want)
			}
			if len(errs) != len(tt.errs) {
				t.Fatalf("Parse() errors = %v, want %d of them", errs, len(tt.errs))
			}
			for i, err := range errs {
				if !strings.Contains(err.Error(), tt.errs[i]) {
					t.Errorf("Parse() error %d = %q, want it to say %q", i, err, tt.errs[i])
				}
			}
		})
	}
}

// TestParseDoesNotQuoteValues keeps what an action returned out of the
// errors, but for the level, which is a word: a body or a title can hold
// what is not to be shown.
func TestParseDoesNotQuoteValues(t *testing.T) {
	_, errs := Parse([]any{
		map[string]any{"level": "info", "body": "b", "title": map[string]any{"token": "s3cr3t"}},
		map[string]any{"level": map[string]any{"token": "s3cr3t"}, "body": "b"},
		[]any{"s3cr3t"},
	})
	if len(errs) != 3 {
		t.Fatalf("Parse() errors = %v, want 3", errs)
	}
	for _, err := range errs {
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("Parse() error %q quotes the value", err)
		}
	}
}
