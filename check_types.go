package probe

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/internal/kind"
)

// The shapes of the names an expression of a step, or the skipif of a job,
// reads, from the fields of what it is evaluated against. What a step's
// action returns, under res and req, is added to them for the fields read
// once it has.
var stepShapes = contextShapes(reflect.TypeFor[StepContext]())

// contextShapes returns the shapes of the names that a value of t, a struct
// an expression is evaluated against, gives by its fields' expr tags.
func contextShapes(t reflect.Type) map[string]expr.Shape {
	out := map[string]expr.Shape{}
	for f := range t.Fields() {
		if name := f.Tag.Get("expr"); name != "" {
			out[name] = typeShape(f.Type)
		}
	}
	return out
}

func typeShape(t reflect.Type) expr.Shape {
	if t.Kind() == reflect.Struct {
		return expr.Shape{Kind: kind.Map, Fields: contextShapes(t)}
	}
	k, _ := kind.OfType(t)
	return expr.Shape{Kind: k}
}

// checkTypes reports the expressions and templates that are sure to go
// wrong by the kinds of the values they read: an operator given values it
// does not take, as "id-" + 1 is, which fails, a comparison of values of
// kinds that are never equal, as res.code == "200 OK" is, which is never
// true, and a test or a skipif that does not come to true or false, which
// fails the step, or leaves it unskipped. The kinds known are those of
// what is written, such as 1 and "id-", of what the functions expressions
// have return, such as len(), of the fields of what a step is evaluated
// against, such as status, and of the fields an action declares of its res
// and req. Nothing else is known: vars and outputs are read as they are
// given, by --var too, and what is under a field, such as res.body, is
// data. A value whose kind is not known is taken to be of the kind that
// would be right.
func (c *checker) checkTypes() {
	check := func(path, where, field, input string, template bool, names map[string]expr.Shape, outcome string) {
		if strings.TrimSpace(input) == "" {
			return
		}
		err := expr.CheckTypes(input, names, outcome != "")
		if err == nil {
			return
		}
		msg := firstLine(err.Error())
		var hints []string
		if h, ok := errors.AsType[*expr.HintError](err); ok {
			msg, hints = h.Err.Error(), h.Hints
		}
		if errors.Is(err, expr.ErrNotBool) {
			msg += ", " + outcome
		}
		if template {
			msg = fmt.Sprintf("template {{%s}}: %s", input, msg)
		}
		for _, h := range hints {
			msg += "; " + h
		}
		c.addOnce(SeverityError, path, where, "%s: %s", field, msg)
	}

	forStrings("vars", c.wf.Vars, func(path, s string) {
		for _, input := range expr.TemplateExprs(s) {
			check(path, "", path, input, true, nil, "")
		}
	})
	c.eachExpr(func(e exprAt) {
		prefix := fmt.Sprintf("jobs[%d].", e.job)
		if e.step >= 0 {
			prefix = fmt.Sprintf("jobs[%d].steps[%d].", e.job, e.step)
		}
		field := strings.TrimPrefix(e.path, prefix)
		template := e.field != "test" && e.field != "skipif" && !strings.HasPrefix(e.field, "outputs.")
		var names map[string]expr.Shape
		// The name of a job is evaluated against the job, whose fields are
		// not those of a step, so only what it writes is known.
		if e.step >= 0 || e.field != "name" {
			names = stepShapes
		}
		if e.step >= 0 && readsResult(e.field) {
			names = c.resultShapes(c.wf.Jobs[e.job].Steps[e.step])
		}
		var outcome string
		switch {
		case e.field == "test":
			outcome = "so the step fails"
		case e.field == "skipif" && e.step >= 0:
			outcome = "so the step is not skipped, whatever it comes to"
		case e.field == "skipif":
			outcome = "so the job is not skipped, whatever it comes to"
		}
		check(e.path, e.where, field, e.input, template, names, outcome)
	})
	for i, job := range c.wf.Jobs {
		if job.Repeat == nil {
			continue
		}
		names := repeatShapes(job)
		for _, t := range job.Repeat.tests() {
			check(fmt.Sprintf("jobs[%d].repeat.%s", i, t.key), c.jobWhere(i), "repeat."+t.key, t.test, false, names, "so the job fails")
		}
	}
}

// resultShapes returns the shapes of the names an expression of st reads
// once its action has returned: those of stepShapes, with the fields of res
// and req whose kinds the action declares. A step of an external action
// reads its res and req as maps whose fields may hold anything.
func (c *checker) resultShapes(st *Step) map[string]expr.Shape {
	out := maps.Clone(stepShapes)
	// What an external action returns is checked by no kind, whatever its
	// name: it may hold anything.
	if actionref.IsExternal(st.Uses) {
		return out
	}
	for name, types := range map[string]map[string]string{
		"req": c.opts.ReqTypes[st.Uses],
		"res": readResTypes(c.opts.ResTypes[st.Uses]),
	} {
		fields := make(map[string]expr.Shape, len(types))
		for field, k := range types {
			fields[field] = expr.Shape{Kind: k}
		}
		out[name] = expr.Shape{Kind: kind.Map, Fields: fields}
	}
	return out
}

// repeatShapes returns the shapes of the names the tests of job, a repeated
// job, read: the fields of each of its steps, under steps and under
// baseline.steps, and vars, which may hold anything.
func repeatShapes(job Job) map[string]expr.Shape {
	number := expr.Shape{Kind: kind.Number}
	rt := expr.Shape{Kind: kind.Map, Fields: map[string]expr.Shape{}}
	for _, f := range repeatRTFields {
		rt.Fields[f] = number
	}
	run := func(fields []string) expr.Shape {
		s := expr.Shape{Kind: kind.Map, Fields: map[string]expr.Shape{}}
		for _, f := range fields {
			s.Fields[f] = number
		}
		s.Fields[runFieldRT] = rt
		return s
	}
	steps := expr.Shape{Kind: kind.Map, Fields: map[string]expr.Shape{}}
	baseline := expr.Shape{Kind: kind.Map, Fields: map[string]expr.Shape{}}
	for j, st := range job.Steps {
		if st == nil {
			continue
		}
		id := st.idAt(j)
		steps.Fields[id] = run(repeatRunFields)
		baseline.Fields[id] = run(baselineRunFields)
	}
	return map[string]expr.Shape{
		"vars":     {Kind: kind.Map},
		"steps":    steps,
		"baseline": {Kind: kind.Map, Fields: map[string]expr.Shape{"steps": baseline}},
	}
}
