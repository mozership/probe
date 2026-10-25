// Package actiontest helps the tests of the built-in actions keep what each
// declares of itself to what it does.
package actiontest

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mozership/probe/actionrpc"
	"github.com/mozership/probe/internal/kind"
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

// Kinds checks that the fields of the req and the res an action returns are
// of the kinds it declares, as the runner receives them: after they have
// been sent from the action's process, which sends a time.Duration as text
// and every number as one kind. A field that is nil, as one an action leaves
// empty may be, is of any kind.
type Kinds struct {
	t        testing.TB
	req, res map[string]kind.Kind
	// seen are the fields a value of was checked, as req.<name> and
	// res.<name>.
	seen map[string]bool
}

// NewKinds returns a Kinds that checks what an action returns against req
// and res, the kinds of the fields of its req and res it declares.
func NewKinds(t testing.TB, req, res map[string]kind.Kind) *Kinds {
	return &Kinds{t: t, req: req, res: res, seen: map[string]bool{}}
}

// Check fails the test when a field of ret, what the action returned for a
// step, is of another kind than the one declared for it.
func (k *Kinds) Check(ret map[string]any) {
	k.t.Helper()
	sent, err := actionrpc.Sendable(ret)
	if err != nil {
		k.t.Fatalf("the result cannot be sent: %v", err)
	}
	for _, name := range []string{"req", "res"} {
		declared := k.req
		if name == "res" {
			declared = k.res
		}
		m, _ := sent[name].(map[string]any)
		for _, key := range slices.Sorted(maps.Keys(declared)) {
			v := m[key]
			if v == nil {
				continue
			}
			k.seen[name+"."+key] = true
			if got, _ := kind.Of(v); got != declared[key] {
				k.t.Errorf("%s.%s is %v (%T), which is not a %s as declared", name, key, v, v, declared[key])
			}
		}
	}
}

// Covered fails the test for a field declared that no result checked had a
// value of, as its kind is then not known to be right.
func (k *Kinds) Covered() {
	k.t.Helper()
	var unseen []string
	for _, name := range []string{"req", "res"} {
		declared := k.req
		if name == "res" {
			declared = k.res
		}
		for key := range declared {
			if !k.seen[name+"."+key] {
				unseen = append(unseen, name+"."+key)
			}
		}
	}
	slices.Sort(unseen)
	if len(unseen) > 0 {
		k.t.Errorf("no result checked has %s, so their kinds are not known to be right", strings.Join(unseen, ", "))
	}
}
