package actionrpc

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseNotes(t *testing.T) {
	got, err := ParseNotes([]any{
		map[string]any{"job": "Sign-up", "job_id": "signup", "step": "A new user", "index": 2.0, "status": "passed", "level": "critical", "title": "No +", "body": "b1"},
		map[string]any{"level": "info", "body": "b2", "run": int64(3), "title": nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Note{
		{Job: "Sign-up", JobID: "signup", Step: "A new user", Index: 2, Status: "passed", Level: NoteCritical, Title: "No +", Body: "b1"},
		{Level: NoteInfo, Body: "b2", Run: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseNotes() =\n%+v\nwant\n%+v", got, want)
	}

	if got, err := ParseNotes([]map[string]any{{"level": "warning", "body": "b"}}); err != nil || len(got) != 1 || got[0].Level != NoteWarning {
		t.Errorf("ParseNotes([]map[string]any) = %+v, %v", got, err)
	}
	if got, err := ParseNotes([]any{}); err != nil || got == nil || len(got) != 0 {
		t.Errorf("ParseNotes(empty) = %#v, %v; want an empty list", got, err)
	}
}

func TestParseNotesRefuses(t *testing.T) {
	for _, tc := range []struct {
		v    any
		want string
	}{
		{"{{ notes }}", `notes must be a list of notes, such as "{{ notes }}", not string`},
		{nil, "notes must be a list of notes"},
		{[]any{"x"}, "notes[0] must be an entry of notes, not string"},
		{[]any{map[string]any{"level": "info", "body": "b"}, map[string]any{"level": "high", "body": "b"}}, `notes[1]: level "high" is not one of info, warning or critical`},
		{[]any{map[string]any{"body": "b"}}, "notes[0]: level is missing"},
		{[]any{map[string]any{"level": "info"}}, "notes[0]: body is missing"},
		{[]any{map[string]any{"level": "info", "body": "b", "title": 1}}, "notes[0]: title must be a string, not int"},
		{[]any{map[string]any{"level": "info", "body": "b", "run": 1.5}}, "notes[0]: run must be a whole number, not 1.5"},
		{[]any{map[string]any{"level": "info", "body": "b", "index": "2"}}, "notes[0]: index must be a whole number"},
	} {
		if _, err := ParseNotes(tc.v); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseNotes(%v) error = %v, want one with %q", tc.v, err, tc.want)
		}
	}
}
