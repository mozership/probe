package probe

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/goccy/go-yaml"
)

func TestIteration_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    Iteration
		wantErr string
	}{
		{
			name: "a list of maps",
			yaml: "iteration:\n- {id: 1, name: foo}\n- {id: 2, name: bar}\n",
			want: Iteration{Entries: []map[string]any{{"id": uint64(1), "name": "foo"}, {"id": uint64(2), "name": "bar"}}},
		},
		{
			name: "a template",
			yaml: "iteration: \"{{ parse_json(file('cases.json')) }}\"\n",
			want: Iteration{Expr: "{{ parse_json(file('cases.json')) }}"},
		},
		{
			name: "nothing",
			yaml: "iteration:\n",
			want: Iteration{},
		},
		{
			name: "an empty list",
			yaml: "iteration: []\n",
			want: Iteration{Entries: []map[string]any{}},
		},
		{
			name:    "text that is not a template",
			yaml:    "iteration: cases.json\n",
			wantErr: `iteration: "cases.json" is not a list`,
		},
		{
			name:    "a template with text around it",
			yaml:    "iteration: \"cases: {{ vars.cases }}\"\n",
			wantErr: "is not a list",
		},
		{
			name:    "two templates",
			yaml:    "iteration: \"{{ vars.a }}{{ vars.b }}\"\n",
			wantErr: "is not a list",
		},
		{
			name:    "a list of something else",
			yaml:    "iteration: [a, b]\n",
			wantErr: "iteration: entry 0 is a string, want a map",
		},
		{
			name:    "a map",
			yaml:    "iteration: {id: 1}\n",
			wantErr: "iteration: want a list of maps, got a map",
		},
		{
			name:    "a number",
			yaml:    "iteration: 3\n",
			wantErr: "iteration: want a list of maps, got a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Iteration Iteration `yaml:"iteration"`
			}
			err := yaml.Unmarshal([]byte(tt.yaml), &got)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Unmarshal() error = %v, want one saying %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Iteration, tt.want) {
				t.Errorf("iteration = %#v, want %#v", got.Iteration, tt.want)
			}
		})
	}
}

func TestIteration_MarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		it   Iteration
		want string
	}{
		{"entries", Iteration{Entries: []map[string]any{{"id": 1}}}, "iteration:\n- id: 1\n"},
		{"a template", Iteration{Expr: "{{ vars.cases }}"}, "iteration: \"{{ vars.cases }}\"\n"},
		{"nothing", Iteration{}, "iteration: []\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yaml.Marshal(struct {
				Iteration Iteration `yaml:"iteration"`
			}{tt.it})
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Errorf("Marshal() = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestIterationEntries(t *testing.T) {
	entries, err := iterationEntries([]any{map[string]any{"a": 1}, map[string]any{"a": 2}})
	if err != nil || len(entries) != 2 || entries[1]["a"] != 2 {
		t.Errorf("iterationEntries() = %v, %v", entries, err)
	}
	if entries, err := iterationEntries([]any{}); err != nil || len(entries) != 0 {
		t.Errorf("iterationEntries(an empty list) = %v, %v", entries, err)
	}
	if _, err := iterationEntries(make([]any, maxIterationEntries+1)); err == nil || !strings.Contains(err.Error(), "more than the 1048576 an iteration takes") {
		t.Errorf("iterationEntries(a list too long) error = %v, want it refused", err)
	}
	if _, err := iterationEntries(make([]map[string]any, maxIterationEntries+1)); err == nil {
		t.Error("iterationEntries(a list of maps too long) = nil, want it refused")
	}
	for _, tt := range []struct {
		v    any
		want string
	}{
		{nil, "got nil"},
		{"text", "got a string"},
		{true, "got a boolean"},
		{1.5, "got a number"},
		{[]any{map[string]any{}, 3}, "entry 1 is a number"},
		{[]any{nil}, "entry 0 is nil"},
		{[]any{[]any{}}, "entry 0 is a list"},
	} {
		if _, err := iterationEntries(tt.v); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("iterationEntries(%v) error = %v, want one saying %q", tt.v, err, tt.want)
		}
	}
}

// iterationWorkflow is a workflow of one job whose last step iterates over
// what iteration, a template, comes to. The step before it publishes rows.
// runner runs the actions of both, the first with rows and the last with
// each.
func iterationWorkflow(iteration string, failFast bool) (w *Workflow, runner ActionRunner, rows, each *mockRunner) {
	rows = newMockRunner()
	rows.SetResult("hello", map[string]any{
		"status": 0,
		"res": map[string]any{"rows": []any{
			map[string]any{"id": 1, "name": "ada"},
			map[string]any{"id": 2, "name": "grace"},
		}},
	})
	each = newMockRunner()
	w = &Workflow{
		Name: "Iteration",
		Vars: map[string]any{"none": []any{}, "names": []any{"ada"}},
		Jobs: []Job{{ID: "users", Name: "Users", FailFast: failFast, Steps: []*Step{
			{ID: "list", Name: "List", Uses: "hello", Test: "len(res.rows) == 2", Outputs: map[string]string{"rows": "res.rows", "none": "nil"}},
			{Name: "Greet {{vars.name}}", Uses: "hello", With: map[string]any{"id": "{{vars.id}}"}, Iteration: Iteration{Expr: iteration}},
		}}},
	}
	return w, stepRunners{byName: map[string]ActionRunner{"List": rows}, fallback: each}, rows, each
}

func TestWorkflow_Start_IterationFromTemplate(t *testing.T) {
	t.Run("the outputs of an earlier step", func(t *testing.T) {
		w, runner, _, each := iterationWorkflow("{{ outputs.list.rows }}", false)
		report := startCapturing(t, w, OutputModeSpinner, runner)

		calls := each.Calls["hello"]
		if len(calls) != 2 {
			t.Fatalf("the step ran %d times, want once for each row:\n%s", len(calls), report)
		}
		if got := calls[1].With["id"]; got != 2 {
			t.Errorf("the second run was given id %v, want that of the second row", got)
		}
		for _, name := range []string{"Greet ada", "Greet grace"} {
			if !strings.Contains(report, name) {
				t.Errorf("the report lacks %q:\n%s", name, report)
			}
		}
		if w.exitStatus != 0 {
			t.Errorf("exit status = %d, want 0:\n%s", w.exitStatus, report)
		}
	})

	t.Run("a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cases.json")
		if err := os.WriteFile(path, []byte(`[{"id": 7, "name": "linus"}]`), 0o600); err != nil {
			t.Fatal(err)
		}
		w, runner, _, each := iterationWorkflow("{{ parse_json(file('"+path+"')) }}", false)
		report := startCapturing(t, w, OutputModeSpinner, runner)

		if calls := each.Calls["hello"]; len(calls) != 1 || !strings.Contains(report, "Greet linus") {
			t.Errorf("the step ran %d times, want once with the case of the file:\n%s", len(calls), report)
		}
	})

	t.Run("a list without entries skips the step", func(t *testing.T) {
		w, runner, _, each := iterationWorkflow("{{ vars.none }}", false)
		report := startCapturing(t, w, OutputModeSpinner, runner)

		if len(each.Calls["hello"]) != 0 || !strings.Contains(report, "(SKIPPED)") {
			t.Errorf("the step should be skipped:\n%s", report)
		}
		if w.exitStatus != 0 {
			t.Errorf("exit status = %d, want 0:\n%s", w.exitStatus, report)
		}
	})

	for name, tt := range map[string]struct{ iteration, want string }{
		"what is not a list fails the step":        {"{{ outputs.list.none }}", "iteration: want a list of maps, got nil"},
		"an entry that is not a map fails it":      {"{{ vars.names }}", "iteration: entry 0 is a string, want a map"},
		"a template that cannot be evaluated does": {"{{ outputs.list.rows.nope.x }}", "iteration"},
	} {
		t.Run(name, func(t *testing.T) {
			w, runner, _, each := iterationWorkflow(tt.iteration, false)
			report := startCapturing(t, w, OutputModeSpinner, runner)

			if len(each.Calls["hello"]) != 0 {
				t.Errorf("the step ran:\n%s", report)
			}
			if !strings.Contains(report, tt.want) {
				t.Errorf("the report should say %q:\n%s", tt.want, report)
			}
			if w.exitStatus == 0 {
				t.Errorf("exit status = 0, want the run failed:\n%s", report)
			}
		})
	}

	t.Run("it is not evaluated after a failure in a job with fail_fast", func(t *testing.T) {
		w, runner, rows, each := iterationWorkflow("{{ outputs.list.none }}", true)
		rows.SetError("hello", errors.New("boom"))
		report := startCapturing(t, w, OutputModeSpinner, runner)

		if len(each.Calls["hello"]) != 0 || !strings.Contains(report, "(SKIPPED)") || strings.Contains(report, "want a list") {
			t.Errorf("the step should be skipped without its iteration being read:\n%s", report)
		}
	})
}

func TestLoad_Iteration(t *testing.T) {
	p, err := loadWorkflow(t, `name: iteration
jobs:
- name: J
  steps:
  - uses: http
    iteration:
    - {path: /a}
    - {path: /b}
  - uses: http
    iteration: "{{ parse_json(file('cases.json')) }}"
  - uses: http
`)
	if err != nil {
		t.Fatal(err)
	}
	steps := p.workflow.Jobs[0].Steps
	if got := steps[0].Iteration; len(got.Entries) != 2 || got.Entries[1]["path"] != "/b" || got.Expr != "" {
		t.Errorf("steps[0].iteration = %#v, want the two entries", got)
	}
	if got := steps[1].Iteration; got.Expr != "{{ parse_json(file('cases.json')) }}" || got.Entries != nil {
		t.Errorf("steps[1].iteration = %#v, want the template", got)
	}
	if got := steps[2].Iteration; got.Expr != "" || len(got.Entries) != 0 {
		t.Errorf("steps[2].iteration = %#v, want none", got)
	}

	_, err = loadWorkflow(t, "name: iteration\njobs:\n- name: J\n  steps:\n  - uses: http\n    iteration: [a, b]\n")
	if err == nil || !strings.Contains(err.Error(), "entry 0 is a string") {
		t.Errorf("Load() = %v, want the iteration refused", err)
	}
}

// probe check reads the template of an iteration as it reads any other: one
// that does not parse, and an output it reads before the step that publishes
// it has run, are told on its line.
func TestCheck_IterationTemplate(t *testing.T) {
	path := writeWorkflow(t, t.TempDir(), "workflow.yml", `name: iteration
jobs:
- name: J
  steps:
  - uses: http
    with:
      url: http://localhost
    iteration: "{{ parse_json(file('cases.json') }}"
    test: res.code == 200
  - uses: http
    with:
      url: http://localhost
    iteration: "{{ outputs.later.rows }}"
    test: res.code == 200
  - id: later
    uses: http
    with:
      url: http://localhost
    test: res.code == 200
    outputs:
      rows: res.body.rows
`)
	findings, err := Check(path, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkFindings(t, findings, []want{
		{SeverityError, 8, "iteration: template {{ parse_json(file('cases.json') }}"},
		{SeverityError, 13, "iteration: outputs.later.rows"},
	})
}

// In a repeated job, an iteration may come to another number of entries in
// each run. The steps after it are then run with another index in each, and
// are still counted as themselves, by where they are written.
func TestWorkflow_Start_RepeatWithIterationThatChanges(t *testing.T) {
	// The colors are put back as they were, for the tests after this one.
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	each := newMockRunner()
	after := newMockRunner()
	w := &Workflow{
		Name: "Repeat",
		Vars: map[string]any{"runs": []any{
			[]any{},
			[]any{map[string]any{"n": "a"}, map[string]any{"n": "b"}},
			[]any{map[string]any{"n": "a"}},
		}},
		Jobs: []Job{{ID: "work", Name: "Work", Repeat: &Repeat{Count: 3}, Steps: []*Step{
			// No entry in the first run, two in the second, one in the third.
			{Name: "Each {{vars.n ?? 'none'}}", Uses: "hello", Test: "true", Iteration: Iteration{Expr: "{{ vars.runs[repeat_index] }}"}},
			{Name: "After", Uses: "hello", Test: "true"},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner, stepRunners{byName: map[string]ActionRunner{"After": after}, fallback: each})

	if got := len(each.Calls["hello"]); got != 3 {
		t.Errorf("the iterated step ran %d times, want 3:\n%s", got, report)
	}
	if got := len(after.Calls["hello"]); got != 3 {
		t.Errorf("the step after it ran %d times, want once in each run:\n%s", got, report)
	}
	for _, want := range []string{
		// The first entry: skipped for want of one in the first run, which
		// is neither a success nor a failure, and run in the two others.
		"0. Each none (repeating 3 times) ✓ 2/3 success (66.7%), 1 skipped",
		// The second entry is run in the second run alone.
		"1. Each b (repeating 1 times) ✓ 1/3 success (33.3%)",
		// The step after it is one step, run in every run.
		"2. After (repeating 3 times) ✓ 3/3 success (100.0%)",
	} {
		// The spaces a report is laid out with are not what is checked.
		if !strings.Contains(strings.Join(strings.Fields(report), " "), want) {
			t.Errorf("the report lacks %q:\n%s", want, report)
		}
	}
}

func TestStep_counterKey(t *testing.T) {
	// A step run without a job is kept by its index.
	if got := (&stepRun{Step: &Step{}, idx: 3}).counterKey(); got != 3 {
		t.Errorf("counterKey() = %d, want the index", got)
	}
	a := &stepRun{Step: &Step{}, idx: 5, pos: 1, ordinal: 0, placed: true}
	b := &stepRun{Step: &Step{}, idx: 5, pos: 2, ordinal: 0, placed: true}
	c := &stepRun{Step: &Step{}, idx: 1, pos: 1, ordinal: 4, placed: true}
	if a.counterKey() == b.counterKey() || a.counterKey() == c.counterKey() {
		t.Errorf("keys = %d, %d, %d, want one for each place", a.counterKey(), b.counterKey(), c.counterKey())
	}
	if a.counterKey() >= c.counterKey() || c.counterKey() >= b.counterKey() {
		t.Errorf("keys = %d, %d, %d, want them in the order the steps run in", a.counterKey(), c.counterKey(), b.counterKey())
	}
	// The last entry an iteration takes is not taken for the next step's.
	last := &stepRun{Step: &Step{}, pos: 1, ordinal: maxIterationEntries - 1, placed: true}
	if last.counterKey() >= b.counterKey() || last.counterKey() <= c.counterKey() {
		t.Errorf("key = %d, want it before that of the next step, %d", last.counterKey(), b.counterKey())
	}
}

// An iteration that comes to no entry in any run of a repeated job leaves
// its step skipped, not passed.
func TestWorkflow_Start_RepeatWithIterationAlwaysEmpty(t *testing.T) {
	noColor := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = noColor }()

	each := newMockRunner()
	w := &Workflow{
		Name: "Repeat",
		Vars: map[string]any{"none": []any{}},
		Jobs: []Job{{ID: "work", Name: "Work", Repeat: &Repeat{Count: 2}, Steps: []*Step{
			{Name: "Each", Uses: "hello", Test: "true", Iteration: Iteration{Expr: "{{ vars.none }}"}},
		}}},
	}
	report := startCapturing(t, w, OutputModeSpinner, each)

	if len(each.Calls["hello"]) != 0 || !strings.Contains(report, "skipped in 2/2 runs") || strings.Contains(report, "success") && !strings.Contains(report, "All jobs succeeded") {
		t.Errorf("the step should be reported as skipped in both runs:\n%s", report)
	}
	if w.exitStatus != 0 {
		t.Errorf("exit status = %d, want 0:\n%s", w.exitStatus, report)
	}
}
