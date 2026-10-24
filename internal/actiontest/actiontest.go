// Package actiontest helps the tests of the built-in actions keep what each
// declares of itself to what it does.
package actiontest

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
)

// CheckFields fails t when ret, what an action returned for a step given
// with, has a field of req or of res that fields does not declare.
func CheckFields(t testing.TB, fields actionrpc.Fields, with, ret map[string]any) {
	t.Helper()
	if _, ok := ret["res"].(map[string]any); !ok {
		t.Fatalf("the action returned no res: %v", ret)
	}
	if got := fields.Undeclared(with, ret); len(got) > 0 {
		t.Errorf("the action returns %s, which its Fields does not declare", strings.Join(got, ", "))
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
