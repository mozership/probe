package probe

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mozership/probe/actionref"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/suggest"
)

// defaultsPath returns the path of the defaults of action in the job at jp,
// or in the workflow when jp is empty, as the workflow writes them: by the
// action in full, or by a name its actions give it.
func (c *checker) defaultsPath(jp, action string) string {
	at := join(jp, "defaults") + "."
	path := at + action
	if _, ok := c.lines[path]; ok {
		return path
	}
	for _, name := range slices.Sorted(maps.Keys(c.wf.Actions)) {
		named := at + name
		if _, ok := c.lines[named]; ok && c.wf.Actions[name] == action {
			return named
		}
	}
	return path
}

func (c *checker) checkActions() {
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			path := fmt.Sprintf("jobs[%d].steps[%d].uses", i, j)
			if actionref.IsExternal(st.Uses) {
				if _, err := actionref.Parse(st.Uses); err != nil {
					c.add(SeverityError, c.line(path), c.stepWhere(i, j), "uses: %s", firstLine(err.Error()))
					continue
				}
				c.readParams(st.Uses, c.line(path), c.stepWhere(i, j))
				continue
			}
			if len(c.opts.Actions) == 0 || slices.Contains(c.opts.Actions, st.Uses) {
				continue
			}
			// A name still here is one that is wrong, which is told where
			// the workflow gives it.
			if _, named := c.wf.Actions[st.Uses]; named {
				continue
			}
			msg := fmt.Sprintf("unknown action %q", st.Uses)
			if s := suggest.Closest(st.Uses, c.opts.Actions); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(path), c.stepWhere(i, j), "%s", msg)
		}
	}
}

// checkEmbedded reads the job file of each step that embeds one, and those
// that job embeds in turn, and reports a uses in them that names no action:
// a job is read by the names of the workflow, so a name it uses must be one
// the workflow gives. A path that is a template is not known before the
// run, and a file that cannot be read is left to the run to report.
func (c *checker) checkEmbedded() {
	if len(c.opts.Actions) == 0 {
		return
	}
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			line := c.line(fmt.Sprintf("jobs[%d].steps[%d].with.path", i, j))
			eachEmbeddedStep(st, map[string]bool{}, func(file string, k int, es *Step) {
				uses := es.Uses
				// A name still here is one that is wrong, which is told
				// where the workflow gives it.
				if _, named := c.wf.Actions[uses]; named || slices.Contains(c.opts.Actions, uses) {
					return
				}
				if actionref.IsExternal(uses) {
					if _, err := actionref.Parse(uses); err != nil {
						c.add(SeverityError, line, c.stepWhere(i, j), "%s: step %d: uses: %s", file, k, firstLine(err.Error()))
					}
					return
				}
				msg := fmt.Sprintf("%s: step %d uses %q, which is not an action of Probe or a name under actions", file, k, uses)
				if s := suggest.Closest(uses, append(slices.Sorted(maps.Keys(c.wf.Actions)), c.opts.Actions...)); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				}
				c.add(SeverityError, line, c.stepWhere(i, j), "%s", msg)
			})
		}
	}
}

// readParams reads the params the external action uses declares in its
// action.yml, once for each action, for checkWith to check its with
// against, and the fields of res it declares, for checkResults. An action.yml that cannot be read leaves its with unchecked,
// with a warning, as the run reads it again and fails then if it still
// cannot.
func (c *checker) readParams(uses string, line int, where string) {
	if c.opts.Manifest == nil {
		return
	}
	if c.readManifests == nil {
		c.readManifests = map[string]bool{}
	}
	if c.readManifests[uses] {
		return
	}
	c.readManifests[uses] = true
	m, err := c.opts.Manifest(uses, c.wf.basePath)
	if err != nil {
		c.add(SeverityWarning, line, where, "uses: the keys of with are not checked, as action.yml could not be read: %s", firstLine(err.Error()))
		return
	}
	if m.Params != nil {
		c.opts.Params[uses] = m.Params
	}
	if m.Res != nil {
		c.opts.ResFields[uses] = m.Res
	}
}

// checkWith checks the keys of each step's with, and of the defaults of
// each job and of the workflow, against those the action takes. A key of a
// step's with that came from defaults is checked there, on the line it is
// written on, and a key that holds a template, which names a key only when
// it runs, is not checked.
func (c *checker) checkWith() {
	c.checkDefaults(c.wf.Defaults, "", "")
	shared, _ := c.wf.Defaults.(map[string]any)
	for i, job := range c.wf.Jobs {
		jp := fmt.Sprintf("jobs[%d]", i)
		c.checkDefaults(job.Defaults, jp, c.jobWhere(i))
		defaults, _ := job.Defaults.(map[string]any)
		for j, st := range job.Steps {
			sp := fmt.Sprintf("%s.steps[%d].with", jp, j)
			// A key the step's with lacks comes from the defaults of its
			// job or of the workflow, where it is checked. A key the step
			// writes, by itself or by a YAML alias or merge key, is checked
			// here.
			inherited, _ := defaults[st.Uses].(map[string]any)
			fromWorkflow, _ := shared[st.Uses].(map[string]any)
			own := func(key string) bool {
				if _, written := c.lines[sp+"."+key]; written {
					return true
				}
				_, fromJob := inherited[key]
				_, fromShared := fromWorkflow[key]
				return !fromJob && !fromShared
			}
			c.withKeys(st.Uses, st.With, sp, c.stepWhere(i, j), "with", own)
		}
	}
}

// checkDefaults checks v, the defaults of the job at jp, or of the workflow
// when jp is empty: that each names an action, and that its keys are ones
// the action takes.
func (c *checker) checkDefaults(v any, jp, where string) {
	defaults, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, action := range slices.Sorted(maps.Keys(defaults)) {
		path := c.defaultsPath(jp, action)
		if _, named := c.wf.Actions[action]; named && !slices.Contains(c.opts.Actions, action) {
			continue
		}
		if len(c.opts.Actions) > 0 && !slices.Contains(c.opts.Actions, action) && !actionref.IsExternal(action) {
			msg := fmt.Sprintf("defaults: no action is named %q, so its defaults apply to no step", action)
			if s := suggest.Closest(action, c.opts.Actions); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(path), where, "%s", msg)
			continue
		}
		if actionref.IsExternal(action) {
			if _, err := actionref.Parse(action); err == nil {
				c.readParams(action, c.line(path), where)
			}
		}
		with, _ := defaults[action].(map[string]any)
		field := path
		if jp != "" {
			field = strings.TrimPrefix(path, jp+".")
		}
		c.withKeys(action, with, path, where, field, func(string) bool { return true })
	}
}

// withKeys checks the keys of with, at path, against those action takes,
// for each key own says belongs there.
func (c *checker) withKeys(action string, with map[string]any, path, where, field string, own func(string) bool) {
	params, ok := c.opts.Params[action]
	if !ok {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(with)) {
		// A key that holds a template names its key only when it runs. One
		// that holds an opener left unclosed is text, which is sent as it is.
		if slices.Contains(params, key) || len(expr.TemplateExprs(key)) > 0 || !own(key) {
			continue
		}
		msg := fmt.Sprintf("%s: unknown key %q for the %s action", field, key, action)
		if s := suggest.Closest(key, params); s != "" {
			msg += fmt.Sprintf("; did you mean %q?", s)
		}
		c.add(SeverityError, c.line(path+"."+key), where, "%s", msg)
	}
}

// checkRequired reports a step whose with, the defaults merged into it,
// lacks a key its action cannot run without, as the action refuses to run
// it. A with with a key that holds a template, which names its key only
// when it runs, is not checked.
func (c *checker) checkRequired() {
	for i, job := range c.wf.Jobs {
		for j, st := range job.Steps {
			groups := c.opts.Required[st.Uses]
			if len(groups) == 0 || slices.ContainsFunc(slices.Collect(maps.Keys(st.With)), isTemplate) {
				continue
			}
			var missing [][]string
			for _, group := range groups {
				if !slices.ContainsFunc(group, func(key string) bool { _, ok := st.With[key]; return ok }) {
					missing = append(missing, group)
				}
			}
			if len(missing) > 0 {
				path := fmt.Sprintf("jobs[%d].steps[%d].with", i, j)
				c.add(SeverityError, c.line(path), c.stepWhere(i, j), "with: the %s action cannot run without %s, which neither the step nor its defaults give", st.Uses, requiredNames(missing))
			}
		}
	}
}

// requiredNames tells the keys of groups, each of which a step lacks, as the
// first key of each and the keys that stand for them: a group of two as url
// or get, and one of more as method, or a key that stands for it, such as
// get.
func requiredNames(groups [][]string) string {
	var names, others []string
	for _, group := range groups {
		switch len(group) {
		case 1:
			names = append(names, group[0])
		case 2:
			names = append(names, group[0]+" or "+group[1])
		default:
			names = append(names, group[0])
			for _, k := range group[1:] {
				if !slices.Contains(others, k) {
					others = append(others, k)
				}
			}
		}
	}
	out := listNames(names)
	switch {
	case len(others) == 0:
	case len(names) == 1:
		out += fmt.Sprintf(", or a key that stands for it, such as %s", others[0])
	default:
		out += fmt.Sprintf(", or a key that stands for them, such as %s", others[0])
	}
	return out
}

func isTemplate(s string) bool {
	return len(expr.TemplateExprs(s)) > 0
}

func (c *checker) checkNeeds() {
	ids := map[string]bool{}
	for _, job := range c.wf.Jobs {
		ids[job.ID] = true
	}
	unknown := false
	for i, job := range c.wf.Jobs {
		for _, need := range job.Needs {
			if ids[need] {
				continue
			}
			unknown = true
			msg := fmt.Sprintf("needs: no job has the id %q", need)
			if s := suggest.Closest(need, slices.Sorted(maps.Keys(ids))); s != "" {
				msg += fmt.Sprintf("; did you mean %q?", s)
			}
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d].needs", i)), c.jobWhere(i), "%s", msg)
		}
	}
	if unknown {
		return
	}

	s := NewJobScheduler()
	for i := range c.wf.Jobs {
		if err := s.AddJob(&c.wf.Jobs[i]); err != nil {
			c.add(SeverityError, c.line(fmt.Sprintf("jobs[%d]", i)), c.jobWhere(i), "%s", firstLine(err.Error()))
			return
		}
	}
	if err := s.ValidateDependencies(); err != nil {
		c.add(SeverityError, 0, "", "%s", firstLine(err.Error()))
	}
}
