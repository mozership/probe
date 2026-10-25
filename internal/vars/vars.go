// Package vars evaluates the vars of a workflow: each var after the vars it
// reads, so that a value such as random_str(8) is computed once, and a var
// that reads itself through others is reported as a circular reference.
package vars

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/fielderr"
)

// Eval evaluates the templates in the vars a workflow writes, with env, the
// environment variables, to read. A var can read another as vars.<name>;
// each one is evaluated after the vars it reads, so a value such as
// random_str(8) is computed once and every var that reads it sees the same
// value. A var whose template cannot be evaluated is an error, and so is a
// circular reference.
//
// A var in given, which the person running the workflow gave, is not
// evaluated: it is the text given, whatever the workflow writes for it, and
// the vars that read it see that text. Neither written nor env is changed.
func Eval(written map[string]any, given map[string]string, env map[string]string) (map[string]any, error) {
	if len(given) > 0 {
		written = maps.Clone(written)
		if written == nil {
			written = make(map[string]any, len(given))
		}
		for name, value := range given {
			written[name] = givenVar(value)
		}
	}

	order, err := evalOrder(written)
	if err != nil {
		return nil, err
	}

	vars := make(map[string]any)
	data := make(map[string]any, len(env)+1)
	for k, v := range env {
		data[k] = v
	}

	var errs []error
	failed := make(map[string]bool)
	done := make(map[string]bool)
	var cycle error

	// evaluate evaluates the var k, which path, the vars being evaluated,
	// waits for. A template a var expands is read only when it runs, so a
	// var it reads that has not been evaluated yet, which evalOrder could not
	// see, is evaluated first and k evaluated again after it. k has kept no
	// value then, so a value such as random_str(8) is still computed once.
	var evaluate func(k string, path []string)
	evaluate = func(k string, path []string) {
		if done[k] || failed[k] || cycle != nil {
			return
		}
		v := written[k]
		if given, ok := v.(givenVar); ok {
			vars[k] = string(given)
			done[k] = true
			return
		}

		// A var that reads one that failed is not evaluated: its error
		// would only repeat that one.
		keys, dynamic := expr.Refs(v, "vars")
		if len(failed) > 0 && (dynamic || expr.CallsTemplate(v) || slices.ContainsFunc(keys, func(d string) bool { return failed[d] })) {
			failed[k] = true
			return
		}

		ev := &expr.Expr{BeforeTemplate: func(text string) error {
			keys, dynamic := expr.Refs(text, "vars")
			for _, d := range keys {
				if _, ok := written[d]; ok && !done[d] {
					return &pendingError{name: d}
				}
			}
			// A template that reads vars by a key known only when it runs
			// waits for every other var, as evalOrder orders such a var.
			if dynamic {
				for _, d := range order {
					if d != k && !done[d] {
						return &pendingError{name: d}
					}
				}
			}
			return nil
		}}
		for {
			// Each var reads a copy of the vars evaluated so far. A
			// template such as {{vars}} keeps the map it returns, and the
			// map being filled in would then hold itself.
			data["vars"] = maps.Clone(vars)

			out, err := EvalValue(ev, k, v, data)
			var pending *pendingError
			if errors.As(err, &pending) {
				d := pending.name
				waiting := append(slices.Clone(path), k)
				if i := slices.Index(waiting, d); i >= 0 {
					chain := append(waiting[i:], d)
					cycle = fmt.Errorf("vars: circular reference: %s", strings.Join(chain, " -> "))
					return
				}
				evaluate(d, waiting)
				if cycle != nil {
					return
				}
				if failed[d] {
					failed[k] = true
					return
				}
				continue
			}
			if err != nil {
				errs = append(errs, err)
				failed[k] = true
				return
			}
			vars[k] = out
			done[k] = true
			return
		}
	}
	for _, k := range order {
		evaluate(k, nil)
		if cycle != nil {
			return nil, cycle
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return vars, nil
}

// givenVar is the value of a var the person running the workflow gave. It
// is a type of its own so that it is told from a string the workflow
// writes, whose templates are evaluated.
type givenVar string

// pendingError stops the evaluation of a var whose template reads the var
// name, which has not been evaluated yet.
type pendingError struct {
	name string
}

func (e *pendingError) Error() string {
	return fmt.Sprintf("vars.%s has not been evaluated yet", e.name)
}

// EvalValue evaluates the templates in the var name, a workflow var or a
// step var, with ev and env: a string becomes a string, and a map or a list
// keeps the type of each value that is a single template. Other values are
// kept as they are. An error names the value that could not be evaluated, as
// vars.auth.user.
func EvalValue(ev *expr.Expr, name string, v any, env any) (any, error) {
	switch v := v.(type) {
	case string:
		out, err := ev.EvalTemplate(v, env)
		if err != nil {
			return nil, &expr.FieldError{Path: "vars." + name, Err: err}
		}
		return out, nil
	case map[string]any:
		out, err := ev.EvalTemplateMap(v, env)
		if err != nil {
			return nil, fielderr.Prefix(err, "vars."+name+".")
		}
		return out, nil
	case []any:
		// Evaluated as a map's value is, so that a list renders the
		// templates in it as one nested in a map does. The map is keyed by a
		// name of its own rather than the var's, which, holding a template,
		// would be evaluated as a key is; the errors are named after the var.
		const key = "list"
		out, err := ev.EvalTemplateMap(map[string]any{key: v}, env)
		if err != nil {
			return nil, fielderr.Rename(err, key, "vars."+name)
		}
		return out[key], nil
	default:
		return v, nil
	}
}

// evalOrder returns the names of vars in an order in which each comes after
// the vars it reads. A var that reads vars by a key known only when it runs
// comes after all the others. A var that expands a template, such as one read
// from a file, comes after all the vars that do not, since what the template
// reads is known only when it runs; among themselves they are ordered by what
// they read outside it. Names are otherwise sorted, and Refs returns the keys
// of a map in the order of its sorted keys, so neither the order nor a
// reported cycle depends on map iteration.
func evalOrder(vars map[string]any) ([]string, error) {
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	slices.Sort(names)

	expands := make(map[string]bool, len(vars))
	for _, k := range names {
		expands[k] = expr.CallsTemplate(vars[k])
	}

	deps := make(map[string][]string, len(vars))
	for _, k := range names {
		keys, dynamic := expr.Refs(vars[k], "vars")
		for _, d := range keys {
			// A name that is not a var reads as nil, as it always has.
			if _, ok := vars[d]; ok {
				deps[k] = append(deps[k], d)
			}
		}
		if dynamic {
			// A var that reads vars as a whole is taken to read every other
			// var, but not itself: only a key it names reads itself.
			for _, d := range names {
				if d != k && !slices.Contains(deps[k], d) {
					deps[k] = append(deps[k], d)
				}
			}
		}
		if expands[k] {
			// Two vars that expand templates are not taken to read each
			// other, which would make every pair of them a cycle.
			for _, d := range names {
				if d != k && !expands[d] && !slices.Contains(deps[k], d) {
					deps[k] = append(deps[k], d)
				}
			}
		}
	}

	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(vars))
	order := make([]string, 0, len(vars))
	var path []string
	var visit func(k string) error
	visit = func(k string) error {
		switch state[k] {
		case done:
			return nil
		case visiting:
			i := slices.Index(path, k)
			cycle := append(slices.Clone(path[i:]), k)
			return fmt.Errorf("vars: circular reference: %s", strings.Join(cycle, " -> "))
		}
		state[k] = visiting
		path = append(path, k)
		for _, d := range deps[k] {
			if err := visit(d); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[k] = done
		order = append(order, k)
		return nil
	}
	for _, k := range names {
		if err := visit(k); err != nil {
			return nil, err
		}
	}

	return order, nil
}
