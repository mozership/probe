// Package notes reads the notes an action leaves for people in res.notes:
// what it found that a later step, such as one that posts to a pull request
// or to a chat, can pass on, each with the attention it asks for.
package notes

import (
	"fmt"
	"slices"
	"strings"
)

// Level is how much attention a note asks for. It is apart from the status
// of the step that left it: a step that passed can leave a critical note.
type Level string

const (
	Info     Level = "info"     // To be read
	Warning  Level = "warning"  // To be looked at
	Critical Level = "critical" // To be acted on
)

// levels are the levels a note can have, in the order they are told in.
var levels = []Level{Info, Warning, Critical}

// Note is one note of an action.
type Note struct {
	Level Level
	// Title is a line naming the note, empty when it has none.
	Title string
	// Body is the note in Markdown.
	Body string
}

// Parse reads v, the value of res.notes, as a list of notes. A nil v is no
// notes. A v that is not a list gives none, and an entry of it that is not
// a note is left out; each error says which and why, for the caller to warn
// of. A note is an object with a level of info, warning or critical, a body
// that is not empty, and a title, if any, that is a string.
func Parse(v any) ([]Note, []error) {
	if v == nil {
		return nil, nil
	}

	var entries []any
	switch list := v.(type) {
	case []any:
		entries = list
	case []map[string]any:
		entries = make([]any, len(list))
		for i, m := range list {
			entries[i] = m
		}
	default:
		return nil, []error{fmt.Errorf("notes is %s, not a list of notes", describe(v))}
	}

	var out []Note
	var errs []error
	for i, e := range entries {
		n, err := parseNote(e)
		if err != nil {
			errs = append(errs, fmt.Errorf("notes[%d]: %w", i, err))
			continue
		}
		out = append(out, n)
	}
	return out, errs
}

func parseNote(v any) (Note, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return Note{}, fmt.Errorf("is %s, not a note", describe(v))
	}

	level, ok := m["level"].(string)
	if !ok {
		if m["level"] == nil {
			return Note{}, fmt.Errorf("has no level; give one of %s", levelList())
		}
		return Note{}, fmt.Errorf("level is %s, not a string; give one of %s", describe(m["level"]), levelList())
	}
	if !slices.Contains(levels, Level(level)) {
		return Note{}, fmt.Errorf("level %q is not one of %s", level, levelList())
	}

	body, ok := m["body"].(string)
	if !ok && m["body"] != nil {
		return Note{}, fmt.Errorf("body is %s, not a string", describe(m["body"]))
	}
	if strings.TrimSpace(body) == "" {
		return Note{}, fmt.Errorf("has no body")
	}

	var title string
	if t, ok := m["title"]; ok && t != nil {
		s, ok := t.(string)
		if !ok {
			return Note{}, fmt.Errorf("title is %s, not a string", describe(t))
		}
		title = s
	}

	return Note{Level: Level(level), Title: title, Body: body}, nil
}

func levelList() string {
	return fmt.Sprintf("%s, %s or %s", levels[0], levels[1], levels[2])
}

// describe says what kind of value v is, for an error that tells why it is
// not what was expected, without quoting it: a value of an action's result
// can hold what is not to be shown.
func describe(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "a number"
	case []any, []map[string]any:
		return "a list"
	case map[string]any:
		return "an object"
	default:
		return fmt.Sprintf("a %T", v)
	}
}
