package check

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/mozership/probe/internal/engine"
	"github.com/mozership/probe/internal/expr"
	"github.com/mozership/probe/internal/suggest"
)

// publisher is a step that publishes outputs.
type publisher struct {
	job, step int
	id        string
	keys      []string
}

func (c *checker) checkOutputs() {
	// A step without an id publishes its outputs by their names alone, so it
	// is kept under none. Only the steps that publish outputs are kept under
	// an id.
	byID := map[string][]publisher{}
	byKey := map[string][]publisher{}
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			if st == nil {
				continue
			}
			p := publisher{job: i, step: j, id: st.ID, keys: slices.Sorted(maps.Keys(st.Outputs))}
			if len(p.keys) > 0 && st.ID != "" {
				byID[st.ID] = append(byID[st.ID], p)
			}
			for _, k := range p.keys {
				byKey[k] = append(byKey[k], p)
			}
		}
	}
	needs := c.ancestors()
	// The names read by themselves, as outputs.token, rather than through
	// the id of the step that publishes them.
	flat := map[string]bool{}

	c.eachExpr(func(e exprAt) {
		chains := expr.Reads(e.input, "outputs")
		for _, chain := range chains {
			if len(byID[chain[0]]) == 0 {
				flat[chain[0]] = true
			}
		}
		// outputs.auth.token also reads outputs.auth, which is not reported
		// on its own.
		for _, chain := range outputChains(chains) {
			msg, severity := c.outputRead(chain, byID, byKey, needs, e.job, e.step, e.late)
			if msg != "" {
				c.add(severity, c.line(e.path), e.where, "%s: outputs.%s %s", e.field, strings.Join(chain, "."), msg)
			}
		}
	})
	// The outputs of the workflow are read once every job is over, so a
	// step of any job publishes before them.
	for _, name := range slices.Sorted(maps.Keys(c.wf.Outputs)) {
		path := "outputs." + name
		for _, chain := range outputChains(expr.Reads(c.wf.Outputs[name], "outputs")) {
			if len(byID[chain[0]]) == 0 {
				flat[chain[0]] = true
			}
			if _, msg := publishersOf(chain, byID, byKey); msg != "" {
				c.add(SeverityError, c.line(path), "", "%s: outputs.%s %s", path, strings.Join(chain, "."), msg)
			}
		}
	}
	c.checkOutputNames(byID, byKey, flat, needs)
}

// outputChains returns what chains read from outputs, each as a name, or as
// a step's id and a name, once, in the order they are read. A step's id
// read alone, where a name under it is read too, is left out.
func outputChains(chains [][]string) [][]string {
	deeper := map[string]bool{}
	for _, chain := range chains {
		if len(chain) > 1 {
			deeper[chain[0]] = true
		}
	}
	var out [][]string
	seen := map[string]bool{}
	for _, chain := range chains {
		chain = chain[:min(2, len(chain))]
		key := strings.Join(chain, "\x00")
		if seen[key] || (len(chain) == 1 && deeper[chain[0]]) {
			continue
		}
		seen[key] = true
		out = append(out, chain)
	}
	return out
}

// checkOutputNames warns of an output name that more than one step
// publishes where a value is lost by it. outputs.<name> holds the value of
// the step that publishes it first, and each value is read through the id
// of its step, which draws no warning. What does is a step without an id
// publishing a name another step may publish first, as its value then
// cannot be read, and outputs.<name> read by itself while more than one step publishes
// it. An output named as the id of a step that publishes is warned of
// whoever reads it: outputs.<name> is that step's outputs or the value,
// whichever is published first.
func (c *checker) checkOutputNames(byID, byKey map[string][]publisher, flat map[string]bool, needs []map[int]bool) {
	for _, key := range slices.Sorted(maps.Keys(byKey)) {
		pubs := byKey[key]
		for n, p := range pubs {
			path := fmt.Sprintf("jobs[%d].steps[%d].outputs.%s", p.job, p.step, key)
			where := c.stepWhere(p.job, p.step)
			if owners := byID[key]; len(owners) > 0 && p.id != key {
				owner := owners[0]
				c.add(SeverityWarning, c.line(path), where, "outputs: %s is the id of %s too, so outputs.%s is that step's outputs or this value, whichever is published first; give this output another name", key, c.stepWhere(owner.job, owner.step), key)
				continue
			}
			if p.id == "" && len(pubs) > 1 {
				if other, ok := publishedBefore(p, pubs, needs); ok {
					c.add(SeverityWarning, c.line(path), where, "outputs: %s is also published by %s, so outputs.%s holds the value of whichever publishes it first, and this step has no id to read its own through; give it one", key, c.stepWhere(other.job, other.step), key)
				}
				continue
			}
			if n == 0 {
				continue
			}
			first := c.stepWhere(pubs[0].job, pubs[0].step)
			switch {
			case flat[key]:
				c.add(SeverityWarning, c.line(path), where, "outputs: %s is also published by %s, and outputs.%s, which holds the value of whichever publishes it first, is read; read outputs.%s.%s for this one", key, first, key, p.id, key)
			}
		}
	}
}

// publishedBefore returns another step of pubs that may publish the name
// before p does, and whether there is one. A step sure to come after p, as
// a later step of its job or a step of a job that needs p's, does not.
func publishedBefore(p publisher, pubs []publisher, needs []map[int]bool) (publisher, bool) {
	for _, o := range pubs {
		switch {
		case o.job == p.job && o.step == p.step:
		case o.job == p.job && o.step > p.step:
		case o.job != p.job && needs[o.job][p.job]:
		default:
			return o, true
		}
	}
	return publisher{}, false
}

// outputRead returns what is wrong with reading chain from outputs at job i
// and step j, and how bad it is; an empty message when nothing is.
func (c *checker) outputRead(chain []string, byID, byKey map[string][]publisher, needs []map[int]bool, i, j int, late bool) (string, string) {
	pubs, msg := publishersOf(chain, byID, byKey)
	if msg != "" {
		return msg, SeverityError
	}
	return c.ordered(pubs, needs, i, j, late)
}

// publishersOf returns the steps that publish what chain reads from
// outputs, or why none does.
func publishersOf(chain []string, byID, byKey map[string][]publisher) ([]publisher, string) {
	if pubs := byID[chain[0]]; len(pubs) > 0 {
		if len(chain) == 1 {
			return pubs, ""
		}
		var with []publisher
		var keys []string
		for _, p := range pubs {
			if slices.Contains(p.keys, chain[1]) {
				with = append(with, p)
			}
			keys = append(keys, p.keys...)
		}
		if len(with) == 0 {
			slices.Sort(keys)
			return nil, fmt.Sprintf("is not published: step %q publishes %s", chain[0], strings.Join(slices.Compact(keys), ", "))
		}
		return with, ""
	}
	if pubs := byKey[chain[0]]; len(pubs) > 0 {
		return pubs, ""
	}
	return nil, "is not published by any step" + suggest.Hint(chain[0], publishedNames(byID, byKey))
}

// ordered returns what is wrong with reading what pubs publish at job i and
// step j: nothing when one of them publishes it before, an error when each
// of them is a step that comes later, and a warning when one is in a job
// that job i does not need, so that it may or may not have run.
func (c *checker) ordered(pubs []publisher, needs []map[int]bool, i, j int, late bool) (string, string) {
	var unordered []publisher
	for _, p := range pubs {
		if p.job == i {
			if p.step < j || (p.step == j && late) {
				return "", ""
			}
			continue
		}
		if needs[i][p.job] {
			return "", ""
		}
		unordered = append(unordered, p)
	}
	switch len(unordered) {
	case 0:
		return "is read before its step publishes it", SeverityError
	case 1:
		return fmt.Sprintf("may not be published yet: job %q does not need job %q, whose step publishes it", c.wf.Jobs[i].Name, c.wf.Jobs[unordered[0].job].Name), SeverityWarning
	default:
		return fmt.Sprintf("may not be published yet: job %q does not need the jobs whose steps publish it", c.wf.Jobs[i].Name), SeverityWarning
	}
}

func publishedNames(byID, byKey map[string][]publisher) []string {
	names := slices.Collect(maps.Keys(byID))
	for k := range byKey {
		names = append(names, k)
	}
	sort.Strings(names)
	return slices.Compact(names)
}

// ancestors returns, for each job, the jobs it needs, directly or through
// the jobs they need.
func (c *checker) ancestors() []map[int]bool {
	byID := make(map[string]int, len(c.wf.Jobs))
	for i, job := range c.wf.Jobs {
		byID[job.ID] = i
	}
	out := make([]map[int]bool, len(c.wf.Jobs))
	for i := range c.wf.Jobs {
		out[i] = engine.NeededJobs(c.wf.Jobs, byID, i)
	}
	return out
}
