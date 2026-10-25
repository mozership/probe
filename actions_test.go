package probe

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe/actionrpc"
)

func TestMockRunner(t *testing.T) {
	mock := newMockRunner()

	// Test default behavior
	result, _, err := mock.Run("test", actionrpc.Call{With: map[string]any{"key": "value"}}, RunOptions{})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if result["mock"] != true {
		t.Errorf("Expected mock=true in default result")
	}
	if result["action"] != "test" {
		t.Errorf("Expected action='test', got %v", result["action"])
	}

	// Test custom result
	customResult := map[string]any{
		"code":    0,
		"results": map[string]any{"text": "Hello World"},
	}
	mock.SetResult("http", customResult)

	result, _, err = mock.Run("http", actionrpc.Call{}, RunOptions{})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if result["code"] != 0 {
		t.Errorf("Expected code=0, got %v", result["code"])
	}

	// Test error behavior
	testErr := errors.New("test error")
	mock.SetError("failing-action", testErr)

	result, _, err = mock.Run("failing-action", actionrpc.Call{}, RunOptions{})
	if err != testErr {
		t.Errorf("Expected test error, got %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result on error, got %v", result)
	}
}

func TestStepWithMockRunner(t *testing.T) {
	// Create a step with mock runner
	step := newStepRun(&Step{
		Name: "Test Step",
		Uses: "http",
		With: map[string]any{"url": "http://example.com"},
	}, nil)

	// Set up mock runner
	mock := newMockRunner()
	mock.SetResult("http", map[string]any{
		"status": 0,
		"res": map[string]any{
			"code": 200,
			"body": "OK",
		},
	})

	// Create minimal job context for testing, which runs the actions of
	// its steps with the mock
	jCtx := &JobContext{
		runEnv: &runEnv{
			Config: Config{Verbose: false, ActionRunner: mock},
		},
	}

	step.ctx = StepContext{}

	// Execute action
	result, err := step.executeAction("Test Step", jCtx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result.Status != 0 {
		t.Errorf("Expected status=0, got %v", result.Status)
	}
	if result.Res["code"] != 200 {
		t.Errorf("Expected res.code=200, got %v", result.Res["code"])
	}
}

func TestRunOptions_logLevel(t *testing.T) {
	tests := []struct {
		name string
		opts RunOptions
		want hclog.Level
	}{
		{
			name: "default filters an action's debug and info records",
			opts: RunOptions{},
			want: hclog.Warn,
		},
		{
			name: "verbose lets everything through",
			opts: RunOptions{Verbose: true},
			want: hclog.Debug,
		},
		{
			name: "quiet silences an attempt that will be retried",
			opts: RunOptions{Quiet: true},
			want: hclog.Off,
		},
		{
			name: "verbose wins over quiet",
			opts: RunOptions{Verbose: true, Quiet: true},
			want: hclog.Debug,
		},
		{
			name: "trace in a verbose run shows how the action is run too",
			opts: RunOptions{Verbose: true, Trace: true},
			want: hclog.Trace,
		},
		{
			name: "trace outside a verbose run shows no more",
			opts: RunOptions{Trace: true},
			want: hclog.Warn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.logLevel(); got != tt.want {
				t.Errorf("RunOptions%+v.logLevel() = %v, want %v", tt.opts, got, tt.want)
			}
		})
	}
}

// countingRunner is an ActionRunner whose actions count their calls
// in the state they keep. It records the count each call was given, or "-"
// for no state. A call with fail fails, and one with keep leaves no state.
type countingRunner struct {
	mu       sync.Mutex
	received []string
}

func (r *countingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	with, state := call.With, call.State
	r.mu.Lock()
	defer r.mu.Unlock()
	n, _ := state["n"].(int64)
	got := "-"
	if state != nil {
		got = fmt.Sprint(n)
	}
	r.received = append(r.received, name+":"+got)
	if with["fail"] == true {
		return nil, nil, errors.New("failed")
	}
	if with["keep"] == true {
		return map[string]any{"status": 0}, nil, nil
	}
	return map[string]any{"status": 0}, map[string]any{"n": n + 1}, nil
}

func TestActionStateIsKeptInARunOfAJob(t *testing.T) {
	runner := &countingRunner{}
	step := func(uses string, with map[string]any) *Step {
		return &Step{Name: uses, Uses: uses, With: with}
	}

	workflow := &Workflow{
		Name: "state",
		Jobs: []Job{
			{
				Name: "counts",
				ID:   "counts",
				Steps: []*Step{
					step("counter", nil),
					step("counter", map[string]any{"fail": true}),
					step("counter", map[string]any{"keep": true}),
					step("other", nil),
					step("counter", nil),
				},
				Repeat: &Repeat{Count: 2},
			},
		},
		printer: newBufferPrinter(),
	}
	_ = workflow.Start(Config{ActionRunner: runner})

	// A failed call and one that leaves no state keep the state as it was,
	// each action keeps its own, and the second run starts over.
	run := []string{"counter:-", "counter:1", "counter:1", "other:-", "counter:1"}
	want := append(append([]string{}, run...), run...)
	if !reflect.DeepEqual(runner.received, want) {
		t.Errorf("states given = %q, want %q", runner.received, want)
	}
}

func TestActionStateIsNotSharedBetweenJobs(t *testing.T) {
	runner := &countingRunner{}
	job := func(id string) Job {
		return Job{
			Name:  id,
			ID:    id,
			Steps: []*Step{{Name: "a", Uses: "counter"}, {Name: "b", Uses: "counter"}},
		}
	}
	second := job("second")
	second.Needs = []string{"first"}
	workflow := &Workflow{
		Name:    "state",
		Jobs:    []Job{job("first"), second},
		printer: newBufferPrinter(),
	}
	if err := workflow.Start(Config{ActionRunner: runner}); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	want := []string{"counter:-", "counter:1", "counter:-", "counter:1"}
	if !reflect.DeepEqual(runner.received, want) {
		t.Errorf("states given = %q, want %q", runner.received, want)
	}
}

func TestMockRunnerCarriesState(t *testing.T) {
	m := newMockRunner()
	m.States["counter"] = map[string]any{"n": 1}

	_, state, err := m.Run("counter", actionrpc.Call{State: map[string]any{"n": 0}}, RunOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(state, map[string]any{"n": 1}) {
		t.Errorf("state = %v", state)
	}
	if calls := m.Calls["counter"]; len(calls) != 1 || !reflect.DeepEqual(calls[0].State, map[string]any{"n": 0}) {
		t.Errorf("calls = %v", calls)
	}
}

// mutatingRunner changes the state it is given before it answers, failing
// when with.fail is set, and records the list it was given.
type mutatingRunner struct {
	got []any
}

func (r *mutatingRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	with, state := call.With, call.State
	list, _ := state["list"].([]any)
	r.got = append(r.got, fmt.Sprint(list))
	if state != nil {
		list[0] = "changed"
		state["extra"] = true
	}
	if with["fail"] == true {
		return nil, nil, errors.New("failed")
	}
	newState := map[string]any{"list": []any{"kept"}}
	return map[string]any{"status": 0}, newState, nil
}

func TestActionStateIsNotChangedByTheRunner(t *testing.T) {
	runner := &mutatingRunner{}
	step := func(with map[string]any) *Step {
		return &Step{Name: "s", Uses: "mutating", With: with}
	}
	workflow := &Workflow{
		Name: "state",
		Jobs: []Job{{
			Name:  "mutates",
			ID:    "mutates",
			Steps: []*Step{step(nil), step(map[string]any{"fail": true}), step(nil)},
		}},
		printer: newBufferPrinter(),
	}
	_ = workflow.Start(Config{ActionRunner: runner})

	// The failed step changed what it was given, which is not what the job
	// keeps, and the state kept is not what the first step went on to hold.
	want := []any{"[]", "[kept]", "[kept]"}
	if !reflect.DeepEqual(runner.got, want) {
		t.Errorf("states given = %v, want %v", runner.got, want)
	}
}

// typedStateRunner keeps state in containers of other types than the ones
// the protocol carries, holds on to the state it returned, and changes it in
// the next call, which fails.
type typedStateRunner struct {
	kept map[string]any
	got  []string
}

func (r *typedStateRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	r.got = append(r.got, fmt.Sprint(call.State))
	if r.kept != nil {
		r.kept["tags"].([]string)[0] = "changed"
		r.kept["labels"].(map[string]string)["a"] = "changed"
		return nil, nil, errors.New("failed")
	}
	r.kept = map[string]any{"tags": []string{"kept"}, "labels": map[string]string{"a": "kept"}}
	return map[string]any{"status": 0}, r.kept, nil
}

func TestActionStateOfOtherTypesIsNotChangedByTheRunner(t *testing.T) {
	runner := &typedStateRunner{}
	step := &Step{Name: "s", Uses: "typed"}
	workflow := &Workflow{
		Name: "state",
		Jobs: []Job{{
			Name:  "typed",
			ID:    "typed",
			Steps: []*Step{step, {Name: "s", Uses: "typed"}, {Name: "s", Uses: "typed"}},
		}},
		printer: newBufferPrinter(),
	}
	_ = workflow.Start(Config{ActionRunner: runner})

	// The state is given in the form the protocol carries, and keeps what
	// was returned, not what the runner changed it to afterwards.
	want := []string{"map[]", "map[labels:map[a:kept] tags:[kept]]", "map[labels:map[a:kept] tags:[kept]]"}
	if !reflect.DeepEqual(runner.got, want) {
		t.Errorf("states given = %q, want %q", runner.got, want)
	}
}

// unsendableStateRunner leaves a state that cannot be sent.
type unsendableStateRunner struct{}

func (unsendableStateRunner) Run(name string, call actionrpc.Call, opts RunOptions) (map[string]any, map[string]any, error) {
	return map[string]any{"status": 0}, map[string]any{"bad": map[struct{}]int{{}: 1}}, nil
}

func TestActionStateThatCannotBeSentFailsTheStep(t *testing.T) {
	jCtx := &JobContext{
		runEnv: &runEnv{
			Printer: newBufferPrinter(),
		},
		states: newActionStates(),
	}
	st := newStepRun(&Step{Name: "s", Uses: "unsendable"}, nil)
	_, err := st.executeSingleAction(unsendableStateRunner{}, map[string]any{}, jCtx, false)
	if err == nil || !strings.Contains(err.Error(), "cannot keep the action's state") {
		t.Errorf("error = %v, want one about the state", err)
	}
}
