package probe

import (
	"fmt"
	"strings"

	"github.com/mozership/probe/expr"
)

// Iteration is what a step runs once for each entry of, with the entry's
// keys as vars: a list written in the workflow, or a template that gives one
// when the step is reached, such as the cases read from a file or the rows
// an earlier step published.
type Iteration struct {
	// Entries is the list written in the workflow.
	Entries []map[string]any
	// Expr is the template that gives the list, as "{{ outputs.users.rows }}".
	// It is empty when the list is written in the workflow.
	Expr string
}

// UnmarshalYAML reads a list of maps as Entries and a string as Expr.
func (it *Iteration) UnmarshalYAML(unmarshal func(any) error) error {
	var raw any
	if err := unmarshal(&raw); err != nil {
		return err
	}

	switch v := raw.(type) {
	case nil:
		*it = Iteration{}
		return nil
	case string:
		// Anything but one template would come to text, which is not a list.
		if !isOneTemplate(v) {
			return fmt.Errorf("iteration: %q is not a list: give a list of maps, or one template that comes to one, as \"{{ parse_json(file('cases.json')) }}\"", v)
		}
		*it = Iteration{Expr: v}
		return nil
	default:
		entries, err := iterationEntries(v)
		if err != nil {
			return fmt.Errorf("iteration: %w", err)
		}
		*it = Iteration{Entries: entries}
		return nil
	}
}

// MarshalYAML writes the iteration as it is written in a workflow.
func (it Iteration) MarshalYAML() (any, error) {
	if it.Expr != "" {
		return it.Expr, nil
	}
	if it.Entries == nil {
		return []map[string]any{}, nil
	}
	return it.Entries, nil
}

// isOneTemplate reports whether s is one {{ }} template and nothing else,
// whose value keeps its type when it is evaluated.
func isOneTemplate(s string) bool {
	s = strings.TrimSpace(s)
	return len(expr.TemplateExprs(s)) == 1 && expr.ReplaceTemplates(s, "") == ""
}

// maxIterationEntries is how many entries an iteration takes. A step runs
// once for each, one after another, so a list this long is a mistake, such
// as a file read whole where a part of it was meant, rather than a test.
const maxIterationEntries = 1 << 20

// iterationEntries returns v, a list of maps, as the entries of an
// iteration. Anything else is an error that says what v is: a step cannot
// run once for each of something that gives it no vars. So is a list of
// more than maxIterationEntries.
func iterationEntries(v any) ([]map[string]any, error) {
	var items []any
	switch list := v.(type) {
	case []map[string]any:
		if len(list) > maxIterationEntries {
			return nil, tooManyEntries(len(list))
		}
		return list, nil
	case []any:
		items = list
	default:
		return nil, fmt.Errorf("want a list of maps, got %s", kindOf(v))
	}
	if len(items) > maxIterationEntries {
		return nil, tooManyEntries(len(items))
	}

	entries := make([]map[string]any, len(items))
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %d is %s, want a map, whose keys the step reads as vars", i, kindOf(item))
		}
		entries[i] = entry
	}
	return entries, nil
}

func tooManyEntries(n int) error {
	return fmt.Errorf("%d entries are more than the %d an iteration takes", n, maxIterationEntries)
}

// kindOf names what v is, for an error a workflow's author reads.
func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case map[string]any:
		return "a map"
	case []any, []map[string]any:
		return "a list"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "a number"
	}
	return fmt.Sprintf("a %T", v)
}

// iterationEntries returns the entries the step runs once for each of: those
// written in the workflow, or what the template comes to against the step's
// vars and the outputs published so far.
func (st *Step) iterationEntries() ([]map[string]any, error) {
	if st.Iteration.Expr == "" {
		return st.Iteration.Entries, nil
	}
	out, err := st.Expr.EvalTemplateWithTypePreservation(st.Iteration.Expr, st.ctx)
	if err != nil {
		return nil, err
	}
	return iterationEntries(out)
}
