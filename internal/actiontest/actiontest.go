// Package actiontest helps the tests of the built-in actions keep what each
// declares of itself to what it does.
package actiontest

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// CheckFields fails t when ret, what an action returned for a step, has a
// field of req or of res that req and res, the fields the action declares,
// leave out. with is the step's with, whose keys are fields too of an action
// that returns what it is given, or nil for one that does not.
func CheckFields(t testing.TB, req, res []string, with, ret map[string]any) {
	t.Helper()
	if _, ok := ret["res"].(map[string]any); !ok {
		t.Fatalf("the action returned no res: %v", ret)
	}
	var undeclared []string
	for name, declared := range map[string][]string{"req": req, "res": res} {
		m, _ := ret[name].(map[string]any)
		for key := range m {
			if _, given := with[key]; !given && !slices.Contains(declared, key) {
				undeclared = append(undeclared, name+"."+key)
			}
		}
	}
	slices.Sort(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("the action returns %s, which it does not declare", strings.Join(undeclared, ", "))
	}
}

// CheckRequired fails t unless run, given with without the keys of each
// group of required, fails for a want of one of them, before it does
// anything: its error names the key. with holds a key of every group, so
// that it is the missing one the action fails for.
func CheckRequired(t testing.TB, required [][]string, with map[string]any, run func(with map[string]any) error) {
	t.Helper()
	for _, group := range required {
		without := maps.Clone(with)
		for _, key := range group {
			delete(without, key)
		}
		if len(without) == len(with) {
			t.Errorf("with %v holds none of %v, so their want is not tested", with, group)
			continue
		}
		err := run(without)
		if err == nil {
			t.Errorf("the action ran without any of %v, which it declares it needs", group)
			continue
		}
		msg := strings.ToLower(err.Error())
		if !slices.ContainsFunc(group, func(key string) bool { return strings.Contains(msg, key) }) {
			t.Errorf("without any of %v, the action failed with %q, which names none of them", group, err)
		}
	}
}
