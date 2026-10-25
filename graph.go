package probe

import (
	"path/filepath"

	"github.com/mozership/probe/dag"
	"github.com/mozership/probe/expr"
	"github.com/mozership/probe/internal/vareval"
)

// graphBuilder resolves the files that embedded steps run, for Workflow.Graph.
type graphBuilder struct {
	workflow      *Workflow
	evaluatedVars map[string]any // cached evaluated vars (lazy loaded)
}

// vars returns the workflow's evaluated vars, evaluating them on first use.
func (b *graphBuilder) vars() map[string]any {
	if b.evaluatedVars == nil {
		vars, err := b.workflow.varsWithOwnEnv()
		if err == nil {
			b.evaluatedVars = vars
		}
	}
	return b.evaluatedVars
}

// varsWithOwnEnv evaluates the vars of the workflow with its own
// environment and no vars given, as vareval.Eval does, for the graph drawn
// of it before any run. A run evaluates them with what it is given, in
// workflowRun.evalVars.
func (w *Workflow) varsWithOwnEnv() (map[string]any, error) {
	return vareval.Eval(w.Vars, nil, w.Env())
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
