package actionrpc

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// contractAction reports check, when it is not nil, and then returns err.
type contractAction struct {
	check *ContractCheck
	err   error
}

func (a *contractAction) Run(with map[string]any) (map[string]any, error) {
	return map[string]any{}, a.err
}

func (a *contractAction) RunStep(call Call) (map[string]any, map[string]any, error) {
	if a.check != nil {
		call.Contract.Report(*a.check)
	}
	return map[string]any{}, nil, a.err
}

// The check an action made of a step against a contract reaches the runner
// as the action reported it.
func TestClientRunStepGathersTheContract(t *testing.T) {
	tests := []struct {
		name  string
		check *ContractCheck
		err   error
		want  *ContractCheck
	}{
		{name: "none"},
		{name: "one kept", check: &ContractCheck{Violations: []Violation{}}, want: &ContractCheck{Violations: []Violation{}}},
		{
			name: "one broken, with what it matched",
			check: &ContractCheck{
				Matched:    &ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"},
				Violations: []Violation{{In: "response", Field: "$.id", Reason: "want integer", Message: "body failed"}, {In: "request", Message: "query failed"}},
			},
			want: &ContractCheck{
				Matched:    &ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"},
				Violations: []Violation{{In: "response", Field: "$.id", Reason: "want integer", Message: "body failed"}, {In: "request", Message: "query failed"}},
			},
		},
		// A step whose action failed has no result for a contract to check.
		{name: "of an action that failed", check: &ContractCheck{Violations: []Violation{}}, err: errors.New("boom")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{client: directClient{&Server{Impl: &contractAction{check: tt.check, err: tt.err}}}}
			got := &Contract{}
			if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}, Contract: got}); (err != nil) != (tt.err != nil) {
				t.Fatalf("RunStep() error = %v", err)
			}
			if !reflect.DeepEqual(got.Check(), tt.want) {
				t.Errorf("Check() = %+v, want %+v", got.Check(), tt.want)
			}
		})
	}

	// A runner that does not hear of it is not told.
	c := &Client{client: directClient{&Server{Impl: &contractAction{check: &ContractCheck{}}}}}
	if _, _, err := c.RunStep(context.Background(), Call{With: map[string]any{}}); err != nil {
		t.Errorf("RunStep() without Contract error = %v", err)
	}
}

func TestContract(t *testing.T) {
	var none *Contract
	none.Report(ContractCheck{})
	if got := none.Check(); got != nil {
		t.Errorf("a nil Contract has %+v", got)
	}

	c := &Contract{}
	if got := c.Check(); got != nil {
		t.Errorf("Check() = %+v before a report, want nil", got)
	}
	c.Report(ContractCheck{Violations: []Violation{{In: "response"}}})
	c.Report(ContractCheck{Violations: []Violation{}})
	if got, want := c.Check(), (&ContractCheck{Violations: []Violation{}}); !reflect.DeepEqual(got, want) {
		t.Errorf("Check() = %+v, want the last report, %+v", got, want)
	}
}

func TestContractCheckOf(t *testing.T) {
	tests := []struct {
		name       string
		violations []any
		matched    map[string]any
		want       ContractCheck
	}{
		{name: "nothing broken or matched", want: ContractCheck{Violations: []Violation{}}},
		{
			name: "violations and a match",
			violations: []any{
				map[string]any{"in": "response", "field": "$.id", "reason": "want integer", "message": "body failed"},
				map[string]any{"in": "request", "message": "query failed"},
			},
			matched: map[string]any{"spec": "openapi.yml", "operation": "GET /users/{id}", "response": "200"},
			want: ContractCheck{
				Matched:    &ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}", Response: "200"},
				Violations: []Violation{{In: "response", Field: "$.id", Reason: "want integer", Message: "body failed"}, {In: "request", Message: "query failed"}},
			},
		},
		{
			name:    "an operation that declares no response",
			matched: map[string]any{"spec": "openapi.yml", "operation": "GET /users/{id}"},
			want:    ContractCheck{Matched: &ContractMatch{Spec: "openapi.yml", Operation: "GET /users/{id}"}, Violations: []Violation{}},
		},
		{name: "a violation that is not a map", violations: []any{"broken"}, want: ContractCheck{Violations: []Violation{{}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContractCheckOf(tt.violations, tt.matched); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ContractCheckOf() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
