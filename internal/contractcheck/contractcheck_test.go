package contractcheck

import (
	"reflect"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

func TestFlag(t *testing.T) {
	tests := []struct {
		name    string
		o       map[string]any
		def     bool
		want    bool
		wantErr string
	}{
		{"missing takes the default", map[string]any{}, true, true, ""},
		{"missing takes a false default", map[string]any{}, false, false, ""},
		{"true", map[string]any{"strict": true}, false, true, ""},
		{"false over a true default", map[string]any{"strict": false}, true, false, ""},
		{"a string is an error", map[string]any{"strict": "yes"}, false, false, "openapi.strict must be true or false"},
		{"nil is an error", map[string]any{"strict": nil}, true, false, "openapi.strict must be true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Flag(tt.o, "openapi", "strict", tt.def)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Flag() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Flag() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Flag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestViolation(t *testing.T) {
	tests := []struct {
		name                       string
		in, message, reason, field string
		want                       map[string]any
		wantViolation              actionrpc.Violation
	}{
		{
			name: "all of it", in: "request", message: "m", reason: "r", field: "$.id",
			want:          map[string]any{"in": "request", "message": "m", "reason": "r", "field": "$.id"},
			wantViolation: actionrpc.Violation{In: "request", Field: "$.id", Reason: "r", Message: "m"},
		},
		{
			name: "no reason or field", in: "response", message: "m",
			want:          map[string]any{"in": "response", "message": "m"},
			wantViolation: actionrpc.Violation{In: "response", Message: "m"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Violation(tt.in, tt.message, tt.reason, tt.field)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Violation() = %v, want %v", got, tt.want)
			}
			// The runner reads the violation back with ContractCheckOf.
			check := actionrpc.ContractCheckOf([]any{got}, nil)
			if len(check.Violations) != 1 || check.Violations[0] != tt.wantViolation {
				t.Errorf("ContractCheckOf(Violation()) = %+v, want %+v", check.Violations, tt.wantViolation)
			}
		})
	}
}

func TestReport(t *testing.T) {
	violations := []any{Violation("response", "m", "", "")}
	matched := map[string]any{"spec": "api.yaml", "operation": "GET /users", "response": "200"}
	tests := []struct {
		name       string
		ret        map[string]any
		violations []any
		matched    map[string]any
		wantCheck  *actionrpc.ContractCheck
		wantRes    map[string]any
	}{
		{
			name:       "violations and a match",
			ret:        map[string]any{"res": map[string]any{"code": 200}},
			violations: violations,
			matched:    matched,
			wantCheck: &actionrpc.ContractCheck{
				Matched:    &actionrpc.ContractMatch{Spec: "api.yaml", Operation: "GET /users", Response: "200"},
				Violations: []actionrpc.Violation{{In: "response", Message: "m"}},
			},
			wantRes: map[string]any{"code": 200, "violations": violations, "contract": matched},
		},
		{
			name:       "no violations found",
			ret:        map[string]any{"res": map[string]any{}},
			violations: []any{},
			wantCheck:  &actionrpc.ContractCheck{Violations: []actionrpc.Violation{}},
			wantRes:    map[string]any{"violations": []any{}},
		},
		{
			name:    "no contract",
			ret:     map[string]any{"res": map[string]any{"code": 200}},
			wantRes: map[string]any{"code": 200},
		},
		{
			name:       "no res",
			ret:        map[string]any{},
			violations: violations,
			wantCheck: &actionrpc.ContractCheck{
				Violations: []actionrpc.Violation{{In: "response", Message: "m"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &actionrpc.Contract{}
			Report(c, tt.ret, tt.violations, tt.matched)
			if got := c.Check(); !reflect.DeepEqual(got, tt.wantCheck) {
				t.Errorf("Check() = %+v, want %+v", got, tt.wantCheck)
			}
			res, _ := tt.ret["res"].(map[string]any)
			if tt.wantRes == nil {
				if res != nil {
					t.Errorf("res = %v, want none", res)
				}
				return
			}
			if !reflect.DeepEqual(res, tt.wantRes) {
				t.Errorf("res = %v, want %v", res, tt.wantRes)
			}
		})
	}
}
