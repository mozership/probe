package actionrpc

import (
	"fmt"
	"math"
)

// Levels of a note, how much attention it asks for, as Note.Level has them.
const (
	NoteInfo     = "info"
	NoteWarning  = "warning"
	NoteCritical = "critical"
)

// Note is an entry of the notes context, as a step gives it to an action
// in with, such as with notes: "{{ notes }}": a note an earlier action left
// for people, with the job and the step it comes from. The values Probe
// hides are hidden in Title and Body, and in Job and Step, as
// <secret:NAME> or <redacted>.
type Note struct {
	// Job is the name of the job, and JobID its id.
	Job, JobID string
	// Step is the name of the step, and Index its index in the job.
	Step  string
	Index int
	// Status is the status of the step: passed, failed or untested.
	Status string
	// Level is NoteInfo, NoteWarning or NoteCritical.
	Level string
	// Title is a line naming the note, empty when it has none.
	Title string
	// Body is the note in Markdown.
	Body string
	// Run is, for a step of a job with repeat, the run the note is of,
	// from 1 without the warmup runs, and 0 otherwise.
	Run int
}

// ParseNotes reads v, the value a step gives an action for the notes
// context, or a part of it such as filter(notes, .level == 'critical'). An
// action that passes notes on reads them with it, rather than its own copy
// of what an entry holds.
//
// An entry needs a level of info, warning or critical and a body; the
// other fields are read when they are there. The error says which entry is
// wrong and why, starting with notes, as notes[1]: level "high" is not one
// of info, warning or critical, for the action to put with. before it.
func ParseNotes(v any) ([]Note, error) {
	list, ok := v.([]any)
	if !ok {
		if ms, ok := v.([]map[string]any); ok {
			list = make([]any, len(ms))
			for i, m := range ms {
				list[i] = m
			}
		} else {
			return nil, fmt.Errorf(`notes must be a list of notes, such as "{{ notes }}", not %T`, v)
		}
	}
	out := make([]Note, 0, len(list))
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("notes[%d] must be an entry of notes, not %T", i, e)
		}
		n, err := parseNote(m)
		if err != nil {
			return nil, fmt.Errorf("notes[%d]: %w", i, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func parseNote(m map[string]any) (Note, error) {
	var n Note
	var err error
	str := func(key string, required bool) string {
		if err != nil {
			return ""
		}
		v, ok := m[key]
		if !ok || v == nil {
			if required {
				err = fmt.Errorf("%s is missing", key)
			}
			return ""
		}
		s, ok := v.(string)
		if !ok {
			err = fmt.Errorf("%s must be a string, not %T", key, v)
		}
		return s
	}
	num := func(key string) int {
		if err != nil {
			return 0
		}
		var w int
		w, err = wholeNumber(m[key])
		if err != nil {
			err = fmt.Errorf("%s %w", key, err)
		}
		return w
	}

	n.Level = str("level", true)
	if err == nil && n.Level != NoteInfo && n.Level != NoteWarning && n.Level != NoteCritical {
		return n, fmt.Errorf("level %q is not one of info, warning or critical", n.Level)
	}
	n.Body = str("body", true)
	n.Title = str("title", false)
	n.Job = str("job", false)
	n.JobID = str("job_id", false)
	n.Step = str("step", false)
	n.Status = str("status", false)
	n.Index = num("index")
	n.Run = num("run")
	return n, err
}

// wholeNumber reads v as a whole number, 0 when it is nil. Its error tells
// the type of v, not v.
func wholeNumber(v any) (int, error) {
	switch x := v.(type) {
	case nil:
		return 0, nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) <= math.MaxInt32 {
			return int(x), nil
		}
		// The value is not told: what a step gives in with can hold
		// anything, a secret among it.
		return 0, fmt.Errorf("must be a whole number, not a fraction or one out of range")
	}
	return 0, fmt.Errorf("must be a whole number, not %T", v)
}
