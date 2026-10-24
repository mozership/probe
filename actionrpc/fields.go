package actionrpc

import (
	"maps"
	"slices"
)

// Fields are the top-level fields of the req and the res an action returns
// for a step, as the action declares them, so that probe check can report a
// step that reads one it never returns. A field that is returned only at
// times, such as res.violations of a step checked against a contract, is
// declared too. What is under a field, such as the body of a response, is
// data, and is not declared.
type Fields struct {
	Req []string
	Res []string
	// With is whether the keys of the step's with are fields of req and of
	// res too, as they are of an action that returns what it is given.
	With bool
}

// Of returns the fields of req and of res for a step given with.
func (f Fields) Of(with map[string]any) (req, res []string) {
	req, res = slices.Clone(f.Req), slices.Clone(f.Res)
	if f.With {
		keys := slices.Collect(maps.Keys(with))
		req = append(req, keys...)
		res = append(res, keys...)
	}
	slices.Sort(req)
	slices.Sort(res)
	return slices.Compact(req), slices.Compact(res)
}

// Undeclared returns the fields of the req and the res of ret, what the
// action returned for a step given with, that f does not declare, each as
// req.<field> or res.<field>, sorted. It is how an action's tests keep its
// declaration to what it returns.
func (f Fields) Undeclared(with, ret map[string]any) []string {
	req, res := f.Of(with)
	var out []string
	for _, part := range []struct {
		name     string
		declared []string
	}{{"req", req}, {"res", res}} {
		m, _ := ret[part.name].(map[string]any)
		for key := range m {
			if !slices.Contains(part.declared, key) {
				out = append(out, part.name+"."+key)
			}
		}
	}
	slices.Sort(out)
	return out
}
