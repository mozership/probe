package probe

import (
	"path/filepath"

	"github.com/mozership/probe/dag"
	"github.com/mozership/probe/expr"
)

// graphBuilder resolves the files that embedded steps run, for Workflow.Graph.
type graphBuilder struct {
	workflow      *Workflow
	evaluatedVars map[string]any // cached evaluated vars (lazy loaded)
}

// vars returns the workflow's evaluated vars, evaluating them on first use.
func (b *graphBuilder) vars() map[string]any {
	if b.evaluatedVars == nil {
		vars, err := b.workflow.evalVars()
		if err == nil {
			b.evaluatedVars = vars
		}
	}
	return b.evaluatedVars
}

// expandPath expands template variables in the path using evaluated workflow vars
func (b *graphBuilder) expandPath(path string) string {
	vars := b.vars()
	if vars == nil {
		return path
	}

	// Build environment with evaluated vars
	env := map[string]any{
		"vars": vars,
	}

	ev := &expr.Expr{}
	expanded, err := ev.EvalTemplate(path, env)
	if err != nil {
		return path // Return original path if expansion fails
	}
	return expanded
}

// Graph returns the jobs and steps of the workflow in the form the DAG
// renderers draw. The steps of an embedded job are read from its file, at the
// path its `with.path` names after the workflow's vars are expanded, relative
// to the working directory.
func (w *Workflow) Graph() dag.Graph {
	b := &graphBuilder{workflow: w}
	g := dag.Graph{Jobs: make([]dag.Job, 0, len(w.Jobs))}
	for i, job := range w.Jobs {
		// A job made in code without an ID is drawn by the one a run
		// gives it.
		id := job.ID
		if id == "" {
			id = defaultJobID(i)
		}
		gj := dag.Job{ID: id, Name: job.Name, Needs: job.Needs, Steps: make([]dag.Step, 0, len(job.Steps))}
		for _, st := range job.Steps {
			gj.Steps = append(gj.Steps, b.step(st))
		}
		g.Jobs = append(g.Jobs, gj)
	}
	return g
}

// step returns the graph step for st, with the steps of the job it embeds.
func (b *graphBuilder) step(st *Step) dag.Step {
	s := dag.Step{Name: st.Name, Embedded: st.Uses == embeddedAction}
	if s.Name == "" {
		s.Name = st.Uses
	}
	path, ok := embeddedJobFile(st)
	if !ok {
		return s
	}
	// The path is taken from the working directory, as the embedded action
	// takes it when the workflow runs, so the graph shows the file that runs.
	expanded := b.expandPath(path)
	job, err := ReadJobFile(expanded, nil)
	if err != nil {
		return s
	}
	s.EmbeddedFile = filepath.Base(expanded)
	for _, es := range job.Steps {
		name := es.Name
		if name == "" {
			name = es.Uses
		}
		s.EmbeddedSteps = append(s.EmbeddedSteps, name)
	}
	return s
}

// eachEmbeddedStep calls visit for each step of the job file st embeds, when
// st is a step of the embedded action, and of the job files those steps
// embed in turn, with the path of the file as it is written and the position
// of the step in it. seen holds the files read, each of which is read once,
// so that job files that embed one another end.
//
// Only what can be told before a run is read: a path that is a template is
// not followed, and nor is a file that cannot be read.
func eachEmbeddedStep(st *Step, seen map[string]bool, visit func(file string, index int, st *Step)) {
	file, ok := embeddedJobFile(st)
	if !ok || expr.HasTemplate(file) {
		return
	}
	abs, err := filepath.Abs(file)
	if err != nil || seen[abs] {
		return
	}
	seen[abs] = true
	job, err := ReadJobFile(file, nil)
	if err != nil {
		return
	}
	for i, es := range job.Steps {
		if es == nil {
			continue
		}
		visit(file, i, es)
		eachEmbeddedStep(es, seen, visit)
	}
}
