package probe

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestOutputsFlatAccess(t *testing.T) {
	outputs := NewOutputs()

	// Test case 1: Normal case - no conflicts
	err := outputs.Set("auth", map[string]any{
		"token":   "secret123",
		"user_id": "user456",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Both access methods should work
	stepOutputs, exists := outputs.Get("auth")
	if !exists {
		t.Fatal("Expected step 'auth' to exist")
	}
	if stepOutputs["token"] != "secret123" {
		t.Errorf("Expected token 'secret123', got %v", stepOutputs["token"])
	}

	flatToken, exists := outputs.GetFlat("token")
	if !exists {
		t.Fatal("Expected flat access to 'token' to exist")
	}
	if flatToken != "secret123" {
		t.Errorf("Expected flat token 'secret123', got %v", flatToken)
	}

	flatUserID, exists := outputs.GetFlat("user_id")
	if !exists {
		t.Fatal("Expected flat access to 'user_id' to exist")
	}
	if flatUserID != "user456" {
		t.Errorf("Expected flat user_id 'user456', got %v", flatUserID)
	}

	// Test case 2: Conflict with step ID
	err = outputs.Set("token", map[string]any{
		"value": "different_token",
	})
	if err == nil {
		t.Errorf("Expected conflict error but got none")
	}

	// Verify all data structure after conflict
	allData := outputs.GetAll()
	expected := map[string]any{
		"auth": map[string]any{
			"token":   "secret123",
			"user_id": "user456",
		},
		"token":   "secret123",
		"user_id": "user456",
		"value":   "different_token",
	}

	if !reflect.DeepEqual(allData, expected) {
		t.Errorf("GetAll() data mismatch.\nExpected: %+v\nGot: %+v", expected, allData)
	}

	// Flat access for "token" should still point to original
	flatToken, exists = outputs.GetFlat("token")
	if !exists {
		t.Fatalf("Expected flat access to 'token' to still exist. Current data: %+v", outputs.data)
	}
	if flatToken != "secret123" {
		t.Errorf("Expected original flat token 'secret123', got %v", flatToken)
	}

	// Step-based access should NOT work for conflicting step because flat data takes priority
	tokenStepOutputs, exists := outputs.Get("token")
	if exists {
		t.Errorf("Expected step 'token' to NOT exist due to flat data conflict, but got: %+v", tokenStepOutputs)
	}

	// Test case 3: Duplicate output names
	err = outputs.Set("profile", map[string]any{
		"token": "profile_token", // Conflicts with existing "token"
		"name":  "john_doe",
	})
	// The first publisher keeps the name, and the later one's value is read
	// through its id, which is no cause for a warning
	if err != nil {
		t.Errorf("Expected no warning for profile's token, read through its id, got: %v", err)
	}

	// Original flat access should be preserved
	flatToken, exists = outputs.GetFlat("token")
	if !exists {
		t.Fatal("Expected flat access to 'token' to still exist")
	}
	if flatToken != "secret123" {
		t.Errorf("Expected original flat token 'secret123', got %v", flatToken)
	}

	// New non-conflicting output should be accessible via flat access
	flatName, exists := outputs.GetFlat("name")
	if !exists {
		t.Fatal("Expected flat access to 'name' to exist")
	}
	if flatName != "john_doe" {
		t.Errorf("Expected flat name 'john_doe', got %v", flatName)
	}

	// Check conflicts
	conflicts := outputs.GetConflicts()
	if len(conflicts["token"]) != 2 {
		t.Errorf("Expected 2 conflicts for 'token', got %v", conflicts["token"])
	}

	expectedSteps := map[string]bool{"auth": true, "profile": true}
	for _, stepID := range conflicts["token"] {
		if !expectedSteps[stepID] {
			t.Errorf("Unexpected step in conflicts: %s", stepID)
		}
	}
}

func TestOutputsGetAll(t *testing.T) {
	outputs := NewOutputs()

	err := outputs.Set("auth", map[string]any{
		"token": "secret123",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	err = outputs.Set("profile", map[string]any{
		"name": "john_doe",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	all := outputs.GetAll()

	// Check step-based access
	authOutputs, ok := all["auth"].(map[string]any)
	if !ok {
		t.Fatal("Expected auth outputs to be map[string]any")
	}
	if authOutputs["token"] != "secret123" {
		t.Errorf("Expected auth token 'secret123', got %v", authOutputs["token"])
	}

	// Check flat access
	if all["token"] != "secret123" {
		t.Errorf("Expected flat token 'secret123', got %v", all["token"])
	}

	if all["name"] != "john_doe" {
		t.Errorf("Expected flat name 'john_doe', got %v", all["name"])
	}
}

func TestOutputsNoFlatAccessForStepIDConflict(t *testing.T) {
	outputs := NewOutputs()

	// Create a step with ID "token"
	err := outputs.Set("token", map[string]any{
		"value": "step_value",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Another step with output name "token": the name stays the step's,
	// and the value is read through the id of its own step, without a word
	err = outputs.Set("auth", map[string]any{
		"token": "auth_token",
	})
	if err != nil {
		t.Errorf("Expected no warning, got: %v", err)
	}

	// Flat access to "token" should not work because "token" is a step ID
	_, exists := outputs.GetFlat("token")
	if exists {
		t.Error("Expected flat access to 'token' to not exist due to step ID conflict")
	}

	// Step-based access should still work
	tokenStep, exists := outputs.Get("token")
	if !exists {
		t.Fatal("Expected step 'token' to exist")
	}
	if tokenStep["value"] != "step_value" {
		t.Errorf("Expected step token value 'step_value', got %v", tokenStep["value"])
	}

	authStep, exists := outputs.Get("auth")
	if !exists {
		t.Fatal("Expected step 'auth' to exist")
	}
	if authStep["token"] != "auth_token" {
		t.Errorf("Expected auth token 'auth_token', got %v", authStep["token"])
	}
}

func TestOutputsConflictWarning(t *testing.T) {
	outputs := NewOutputs()

	// First, create a flat output 'foo'
	err := outputs.Set("step1", map[string]any{
		"foo": "flat_value",
	})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Now try to create a step with ID 'foo' - this should return an error
	err = outputs.Set("foo", map[string]any{
		"bar": "step_value",
	})
	if err == nil {
		t.Error("Expected conflict error when step ID conflicts with flat output name")
	}

	// Verify the error message
	expectedMsg := "the outputs of 'foo' cannot be read as outputs.foo.<name>, since outputs.foo is an output published before"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}

	// Flat access should still work
	flatFoo, exists := outputs.GetFlat("foo")
	if !exists {
		t.Error("Expected flat access to 'foo' to still work")
	}
	if flatFoo != "flat_value" {
		t.Errorf("Expected flat foo 'flat_value', got %v", flatFoo)
	}

	// Step-based access should not work due to conflict
	_, exists = outputs.Get("foo")
	if exists {
		t.Error("Expected step-based access to 'foo' to fail due to conflict")
	}
}

func TestOutputsFirstPublisherKeepsName(t *testing.T) {
	// The first value stays, and each later one is read through its step's
	// id, so no publisher is warned.
	outputs := NewOutputs()
	for _, step := range []string{"a", "b", "c"} {
		if err := outputs.Set(step, map[string]any{"token": step}); err != nil {
			t.Errorf("Unexpected warning for %s: %v", step, err)
		}
	}
	if v, _ := outputs.GetFlat("token"); v != "a" {
		t.Errorf("flat token = %v, want a", v)
	}
	for _, step := range []string{"a", "b", "c"} {
		if got, _ := outputs.Get(step); got["token"] != step {
			t.Errorf("outputs.%s.token = %v, want %s", step, got["token"], step)
		}
	}
}

func TestOutputsSameStepAgain(t *testing.T) {
	// A step publishing again is not a conflict with itself, and its flat
	// outputs follow its latest values as its step-keyed ones do.
	outputs := NewOutputs()
	for i := 1; i <= 3; i++ {
		if err := outputs.Set("poll", map[string]any{"count": i}); err != nil {
			t.Fatalf("run %d: unexpected warning: %v", i, err)
		}
	}
	if v, _ := outputs.GetFlat("count"); v != 3 {
		t.Errorf("flat count = %v, want 3", v)
	}
}

func TestOutputsNameTakenByStepID(t *testing.T) {
	// A name that is a step's id holds that step's outputs. A step with an
	// id that publishes the name is read through its id without a word; one
	// without an id is told, rather than its value dropping silently.
	outputs := NewOutputs()
	_ = outputs.Set("token", map[string]any{"value": "v"})
	if err := outputs.Set("auth", map[string]any{"token": "auth"}); err != nil {
		t.Errorf("auth: got %v, want no warning", err)
	}
	err := outputs.SetFlat("job_0.step_2", map[string]any{"token": "flat"})
	var taken *NameTakenError
	if !errors.As(err, &taken) {
		t.Fatalf("got %v, want a name-taken warning", err)
	}
	if msg := taken.Error(); !strings.Contains(msg, "has the name of the step 'token'") || !strings.Contains(msg, "give the step an id") {
		t.Errorf("warning = %q, want it to name the step and the way to read the value", msg)
	}
	if got, _ := outputs.Get("token"); got["value"] != "v" {
		t.Errorf("outputs.token = %v, want the step's outputs", got)
	}
}

func TestOutputsNameTakenAdviceWhenStepIDTaken(t *testing.T) {
	// "first" publishes the names "second" and "token". The step "second"
	// then cannot be read through its id, so the warning must not send the
	// reader there.
	outputs := NewOutputs()
	_ = outputs.Set("first", map[string]any{"second": "x", "token": "first-token"})
	err := outputs.Set("second", map[string]any{"token": "second-token"})
	if err == nil {
		t.Fatal("Expected a conflict for the step id and a warning for the name")
	}
	if strings.Contains(err.Error(), "read outputs.second.token") {
		t.Errorf("warning points to outputs.second.token, which does not exist: %v", err)
	}
	if !strings.Contains(err.Error(), "this one cannot be read") {
		t.Errorf("warning = %v, want it to say the value cannot be read", err)
	}
}

func TestOutputsWarningsAndErrorsApart(t *testing.T) {
	// A taken name is a warning, a taken step id an error, so the caller can
	// report each as what it is.
	outputs := NewOutputs()
	_ = outputs.Set("first", map[string]any{"second": "x", "token": "t"})
	err := outputs.Set("second", map[string]any{"token": "u"})
	var warnings, others int
	for _, e := range unwrapJoined(err) {
		var taken *NameTakenError
		if errors.As(e, &taken) {
			warnings++
		} else {
			others++
		}
	}
	if warnings != 1 || others != 1 {
		t.Errorf("got %d warnings and %d errors, want 1 and 1: %v", warnings, others, err)
	}
}

func TestRunOutputs(t *testing.T) {
	parent := NewOutputs()
	_ = parent.Set("setup", map[string]any{"base": "b", "token": "parent-token"})

	run := newRunOutputs(parent)
	if err := run.Set("pub", map[string]any{"v": "run"}); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	// The run sees the parent's outputs and its own
	if v, _ := run.GetFlat("base"); v != "b" {
		t.Errorf("run base = %v, want the parent's", v)
	}
	if got, _ := run.Get("pub"); got["v"] != "run" {
		t.Errorf("run pub.v = %v, want its own", got["v"])
	}
	if all := run.GetAll(); all["base"] != "b" || all["v"] != "run" {
		t.Errorf("run GetAll = %v, want both", all)
	}

	// The parent does not see the run's until it is published
	if _, ok := parent.Get("pub"); ok {
		t.Error("parent has the run's outputs before publish")
	}

	// A name the parent's steps took stays theirs, as in a workflow; a step
	// without an id that publishes it is told
	err := run.SetFlat("job_0.step_1", map[string]any{"token": "run-token"})
	var taken *NameTakenError
	if !errors.As(err, &taken) {
		t.Errorf("SetFlat() = %v, want a name-taken warning", err)
	}
	if v, _ := run.GetFlat("token"); v != "parent-token" {
		t.Errorf("run token = %v, want the parent's", v)
	}

	if err := parent.publish(run); err == nil {
		t.Error("publish() gave no warning for the taken name")
	}
	if got, _ := parent.Get("pub"); got["v"] != "run" {
		t.Errorf("parent pub.v = %v after publish, want the run's", got["v"])
	}
	if v, _ := parent.GetFlat("v"); v != "run" {
		t.Errorf("parent v = %v after publish, want the run's", v)
	}
}

// A step without an id publishes its outputs by their names alone, and
// keeps a name it published first as a step with an id does.
func TestOutputs_SetFlat(t *testing.T) {
	o := NewOutputs()
	if err := o.SetFlat("job_0.step_0", map[string]any{"pid": 1}); err != nil {
		t.Fatalf("SetFlat() error = %v", err)
	}
	if v, ok := o.GetFlat("pid"); !ok || v != 1 {
		t.Errorf("GetFlat(pid) = %v, %v, want 1", v, ok)
	}
	if _, ok := o.Get(""); ok {
		t.Error("the outputs are kept under an empty id")
	}

	// The same step publishing again replaces its value.
	if err := o.SetFlat("job_0.step_0", map[string]any{"pid": 2}); err != nil {
		t.Fatalf("SetFlat() again error = %v", err)
	}
	if v, _ := o.GetFlat("pid"); v != 2 {
		t.Errorf("GetFlat(pid) = %v, want the step's latest, 2", v)
	}

	// Another step of the same position in another job does not.
	err := o.SetFlat("job_1.step_0", map[string]any{"pid": 3})
	var taken *NameTakenError
	if !errors.As(err, &taken) || !strings.Contains(err.Error(), "give the step an id to read this one through it") {
		t.Errorf("SetFlat() of another step error = %v, want the name taken", err)
	}
	if v, _ := o.GetFlat("pid"); v != 2 {
		t.Errorf("GetFlat(pid) = %v, want the first step's, 2", v)
	}
}
