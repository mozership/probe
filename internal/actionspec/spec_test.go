package actionspec

import (
	"slices"
	"testing"
)

func TestUndeclared(t *testing.T) {
	full := Spec{
		Serve:     func() {},
		Keeps:     KeepsNone,
		Params:    []string{"url"},
		Contracts: NoContracts,
		ReqFields: []string{},
		ResFields: []string{"code"},
		ReqTypes:  map[string]string{},
		ResTypes:  map[string]string{},
		Required:  NoneRequired,
	}
	tests := []struct {
		name string
		spec func(Spec) Spec
		want []string
	}{
		{"every field declared", func(s Spec) Spec { return s }, nil},
		{"any key taken", func(s Spec) Spec { s.Params, s.AnyParams = nil, true; return s }, nil},
		{"nothing declared", func(Spec) Spec { return Spec{} }, []string{"Serve", "Keeps", "Params", "Contracts", "ReqFields", "ResFields", "ReqTypes", "ResTypes", "Required"}},
		{"keeps forgotten", func(s Spec) Spec { s.Keeps = nil; return s }, []string{"Keeps"}},
		{"params given with any key", func(s Spec) Spec { s.AnyParams = true; return s }, []string{"Params with AnyParams"}},
		{"required forgotten", func(s Spec) Spec { s.Required = nil; return s }, []string{"Required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec(full).Undeclared(); !slices.Equal(got, tt.want) {
				t.Errorf("Undeclared() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The values that declare none of something are empty, so that they give
// nothing, and not nil, so that they declare the field.
func TestNoneIsDeclared(t *testing.T) {
	if KeepsNone == nil || len(KeepsNone) != 0 {
		t.Errorf("KeepsNone = %#v, want empty and not nil", KeepsNone)
	}
	if NoContracts == nil || len(NoContracts) != 0 {
		t.Errorf("NoContracts = %#v, want empty and not nil", NoContracts)
	}
	if NoneRequired == nil || len(NoneRequired) != 0 {
		t.Errorf("NoneRequired = %#v, want empty and not nil", NoneRequired)
	}
}
