package probe

import (
	"errors"
	"reflect"
	"testing"
)

// operation returns the operation of the ProbeError err is, or holds.
func operation(t *testing.T, err error) string {
	t.Helper()
	var pe *ProbeError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want a ProbeError", err)
	}
	return pe.Operation
}

// A run narrowed to some jobs has those jobs and what they need, and says so,
// while the workflow keeps every job for the next run.
func TestWorkflow_prepare_Selection(t *testing.T) {
	w := newOrderTestWorkflow(true)
	w.Jobs[1].Needs = []string{"a"}

	r, err := w.prepare(Config{Jobs: []string{"b"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.jobIDs(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the run has jobs %q, want %q", got, want)
	}
	if r.selection == nil {
		t.Error("the run does not say what it was narrowed to")
	}
	if got := r.scheduler.Runnable(); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("runnable jobs = %q, want only a, which b needs", got)
	}
	if len(w.Jobs) != 3 {
		t.Errorf("the workflow has %d jobs after prepare, want 3", len(w.Jobs))
	}

	every, err := w.prepare(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := every.jobIDs(), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a run of every job has jobs %q, want %q", got, want)
	}
	if every.selection != nil {
		t.Error("a run of every job says it was narrowed")
	}
}

// A workflow made in code is given the IDs of its jobs as a loaded one is.
func TestWorkflow_prepare_AssignsJobIDs(t *testing.T) {
	w := newOrderTestWorkflow(false)
	r, err := w.prepare(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.jobIDs(), []string{"job_0", "job_1", "job_2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("job IDs = %q, want %q", got, want)
	}
}

// prepare refuses a run for the first of what keeps it from running, in the
// order validate.go sets out.
func TestWorkflow_prepare_Refuses(t *testing.T) {
	tests := []struct {
		name string
		edit func(w *Workflow)
		c    Config
		want string
	}{
		{
			name: "repeat before problems",
			edit: func(w *Workflow) {
				w.Jobs[0].Repeat = &Repeat{Count: 5, Async: true}
				w.Jobs[1].Steps[0].Test = "res.code =="
			},
			want: "invalid_repeat",
		},
		{
			name: "problems",
			edit: func(w *Workflow) { w.Jobs[1].Steps[0].Test = "res.code ==" },
			want: "invalid_workflow",
		},
		{
			name: "problems before an unknown need",
			edit: func(w *Workflow) {
				w.Jobs[0].Needs = []string{"missing"}
				w.Jobs[1].Steps[0].Test = "res.code =="
			},
			want: "invalid_workflow",
		},
		{
			name: "an action no job that runs names",
			edit: func(w *Workflow) { w.Jobs[2].Steps[0].Uses = "nope" },
			c:    Config{Jobs: []string{"a"}, Actions: []string{"hello"}},
			want: "resolve_action",
		},
		{
			name: "an action a job that runs names",
			edit: func(w *Workflow) { w.Jobs[0].Steps[0].Uses = "nope" },
			c:    Config{Actions: []string{"hello"}},
			want: "resolve_action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newOrderTestWorkflow(true)
			tt.edit(w)
			r, err := w.prepare(tt.c)
			if err == nil {
				t.Fatalf("prepare() = %+v, want it refused", r)
			}
			if got := operation(t, err); got != tt.want {
				t.Errorf("prepare() error = %v, want one of %s", err, tt.want)
			}
		})
	}

	t.Run("an unknown need", func(t *testing.T) {
		w := newOrderTestWorkflow(true)
		w.Jobs[0].Needs = []string{"missing"}
		if _, err := w.prepare(Config{}); err == nil {
			t.Error("prepare() = nil, want a need that is no job refused")
		}
	})

	t.Run("an unknown job selected", func(t *testing.T) {
		w := newOrderTestWorkflow(true)
		if _, err := w.prepare(Config{Jobs: []string{"z"}}); err == nil {
			t.Error("prepare() = nil, want a selection of no job refused")
		}
	})
}

// What one run finds out and is given is not another's: its outputs, its
// failure, its ID and the environment it was given.
func TestWorkflow_prepare_RunsAreTheirOwn(t *testing.T) {
	w := newOrderTestWorkflow(true)
	w.env = map[string]string{"OWN": "workflow"}

	first, err := w.prepare(Config{Env: map[string]string{"GIVEN": "first", "OWN": "given"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.prepare(Config{})
	if err != nil {
		t.Fatal(err)
	}

	if first.outputs == second.outputs {
		t.Error("two runs share their outputs")
	}
	if first.id == "" || first.id == second.id {
		t.Errorf("run IDs = %q and %q, want two of their own", first.id, second.id)
	}
	first.failed.Store(true)
	if second.failed.Load() {
		t.Error("a run is failed by another")
	}

	if got, want := first.env, map[string]string{"GIVEN": "first", "OWN": "workflow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("env of the first run = %v, want %v, the workflow's over what it was given", got, want)
	}
	if got, want := second.env, map[string]string{"OWN": "workflow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("env of the second run = %v, want %v, without what the first was given", got, want)
	}
	if got, want := w.Env(), map[string]string{"OWN": "workflow"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Env() = %v, want the workflow's own %v", got, want)
	}
}
