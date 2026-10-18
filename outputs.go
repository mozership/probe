package probe

import (
	"errors"
	"fmt"
	"maps"
	"sort"
	"sync"
)

// Outputs manages step outputs across the entire workflow
type Outputs struct {
	data map[string]any // stores both stepID->outputs and outputName->value
	// owners records which step published each flat output name. The first
	// step to publish a name keeps it.
	owners map[string]string
	// parent is the workflow's store when this one holds a single run of a
	// job repeated asynchronously. The run reads its own outputs before the
	// parent's, so that it never sees another run's, and writes only its
	// own; published lists what it wrote, in order, for publish to replay.
	parent    *Outputs
	published []publication
	mu        sync.RWMutex
}

// publication is one Set call of a run.
type publication struct {
	stepID    string
	publisher string
	outputs   map[string]any
}

// NewOutputs creates a new Outputs instance
func NewOutputs() *Outputs {
	return &Outputs{
		data:   make(map[string]any),
		owners: make(map[string]string),
	}
}

// newRunOutputs returns a store for one run of a job repeated
// asynchronously, in front of parent.
func newRunOutputs(parent *Outputs) *Outputs {
	run := NewOutputs()
	run.parent = parent
	return run
}

// publish copies into o what the run wrote, in the order it wrote it, so
// that later jobs read the outputs that run left.
func (o *Outputs) publish(run *Outputs) error {
	run.mu.RLock()
	published := run.published
	run.mu.RUnlock()

	var errs []error
	for _, p := range published {
		errs = append(errs, o.set(p.stepID, p.publisher, p.outputs))
	}
	return errors.Join(errs...)
}

// lookupLocked returns the entry under name, the run's own before its
// parent's. o.mu must be held.
func (o *Outputs) lookupLocked(name string) (any, bool) {
	if v, ok := o.data[name]; ok {
		return v, true
	}
	if o.parent == nil {
		return nil, false
	}
	o.parent.mu.RLock()
	defer o.parent.mu.RUnlock()
	v, ok := o.parent.data[name]
	return v, ok
}

// ownerLocked returns the step that published the flat name, looking in
// the parent too. o.mu must be held.
func (o *Outputs) ownerLocked(name string) (string, bool) {
	if owner, ok := o.owners[name]; ok {
		return owner, true
	}
	if o.parent == nil {
		return "", false
	}
	o.parent.mu.RLock()
	defer o.parent.mu.RUnlock()
	owner, ok := o.parent.owners[name]
	return owner, ok
}

// Set stores outputs for a step with flat access support
func (o *Outputs) Set(stepID string, outputs map[string]any) error {
	return o.set(stepID, stepID, outputs)
}

// SetFlat stores the outputs of a step without an id, which are read by
// their names alone. owner names the step, the same each time it
// publishes, for a later step's name to be told from its own.
func (o *Outputs) SetFlat(owner string, outputs map[string]any) error {
	return o.set("", owner, outputs)
}

// set stores outputs under stepID, unless it is empty, and by their names
// alone, as publisher published them.
func (o *Outputs) set(stepID, publisher string, outputs map[string]any) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.parent != nil {
		o.published = append(o.published, publication{stepID: stepID, publisher: publisher, outputs: outputs})
	}

	// Check if stepID conflicts with existing flat data
	stepIDConflictsWithFlat := false
	var conflictError error
	if existingValue, exists := o.lookupLocked(stepID); stepID != "" && exists {
		if _, isMap := existingValue.(map[string]any); !isMap {
			// stepID conflicts with existing flat data - this will prevent step-based access
			stepIDConflictsWithFlat = true
			conflictError = fmt.Errorf("cannot create step-based outputs for '%s' because flat output with same name exists", stepID)
		}
	}

	// Store step-based outputs only if no conflict with flat data
	if stepID != "" && !stepIDConflictsWithFlat {
		o.data[stepID] = outputs
	}

	// Store flat outputs. The first step to publish a name keeps it; a later
	// step's value under the same name is read through its step id, and is
	// reported so that it is not lost without a word.
	var errs []error
	if conflictError != nil {
		errs = append(errs, conflictError)
	}
	names := make([]string, 0, len(outputs))
	for outputName := range outputs {
		names = append(names, outputName)
	}
	sort.Strings(names)
	for _, outputName := range names {
		owner, owned := o.ownerLocked(outputName)
		existing, _ := o.lookupLocked(outputName)
		_, isStep := existing.(map[string]any)

		// Where this value can still be read when the name alone is taken:
		// through the step's id, unless that was taken as well above.
		instead := fmt.Sprintf("read outputs.%s.%s for this one", stepID, outputName)
		switch {
		case stepID == "":
			instead = "give the step an id to read this one through it"
		case stepIDConflictsWithFlat:
			instead = fmt.Sprintf("this one cannot be read, since outputs.%s is an output name too", stepID)
		}
		switch {
		case owned && owner == publisher:
			// The same step publishing again keeps the name, with its
			// latest value, as outputs.<step_id>.<name> has.
			o.data[outputName] = outputs[outputName]
		case owned:
			errs = append(errs, &NameTakenError{fmt.Sprintf("output '%s' of '%s' is also published by '%s', which came first, so outputs.%s keeps the value of '%s'; %s",
				outputName, publisher, owner, outputName, owner, instead)})
		case isStep:
			// A step ID of the same name keeps the name for its outputs.
			errs = append(errs, &NameTakenError{fmt.Sprintf("output '%s' of '%s' has the name of the step '%s', so outputs.%s is that step's outputs; %s",
				outputName, publisher, outputName, outputName, instead)})
		default:
			o.data[outputName] = outputs[outputName]
			o.owners[outputName] = publisher
		}
	}

	return errors.Join(errs...)
}

// NameTakenError reports an output whose name was taken before it was
// published, so that outputs.<name> holds something else. It is a warning:
// the step's value is kept under its id when that can be.
type NameTakenError struct {
	msg string
}

func (e *NameTakenError) Error() string {
	return e.msg
}

// Get retrieves outputs for a step (existing functionality)
func (o *Outputs) Get(stepID string) (map[string]any, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	value, exists := o.lookupLocked(stepID)
	if !exists {
		return nil, false
	}

	if outputs, ok := value.(map[string]any); ok {
		return outputs, true
	}

	return nil, false
}

// GetFlat retrieves output by name directly (new functionality)
func (o *Outputs) GetFlat(outputName string) (any, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	value, exists := o.lookupLocked(outputName)
	if !exists {
		return nil, false
	}

	// If it's a map[string]any, it's step-based data, not flat data
	if _, isMap := value.(map[string]any); isMap {
		return nil, false
	}

	return value, true
}

// GetAll returns all outputs (safe copy for expression evaluation). A run's
// own outputs stand in front of its parent's.
func (o *Outputs) GetAll() map[string]any {
	copy := make(map[string]any)
	if o.parent != nil {
		copy = o.parent.GetAll()
	}

	o.mu.RLock()
	defer o.mu.RUnlock()

	for k, v := range o.data {
		if stepOutputs, ok := v.(map[string]any); ok {
			// This is step-based data, create a deep copy
			copyOutputs := make(map[string]any)
			maps.Copy(copyOutputs, stepOutputs)
			copy[k] = copyOutputs
		} else {
			// This is flat data, copy directly
			copy[k] = v
		}
	}

	return copy
}

// GetAllWithFlat returns all outputs including flat access for expression evaluation
func (o *Outputs) GetAllWithFlat() map[string]any {
	o.mu.RLock()
	defer o.mu.RUnlock()

	copy := make(map[string]any)

	for k, v := range o.data {
		if stepOutputs, ok := v.(map[string]any); ok {
			// This is step-based data, create a deep copy
			copyOutputs := make(map[string]any)
			maps.Copy(copyOutputs, stepOutputs)
			copy[k] = copyOutputs
		} else {
			// This is flat data, copy directly
			copy[k] = v
		}
	}

	return copy
}

// GetConflicts returns information about conflicted output names
func (o *Outputs) GetConflicts() map[string][]string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	conflicts := make(map[string][]string)
	outputUsage := make(map[string][]string)

	// Scan all step-based outputs
	for stepID, value := range o.data {
		if stepOutputs, ok := value.(map[string]any); ok {
			for outputName := range stepOutputs {
				outputUsage[outputName] = append(outputUsage[outputName], stepID)
			}
		}
	}

	// Find conflicts (output names used by multiple steps)
	for outputName, stepIDs := range outputUsage {
		if len(stepIDs) > 1 {
			conflicts[outputName] = stepIDs
		}
	}

	return conflicts
}
