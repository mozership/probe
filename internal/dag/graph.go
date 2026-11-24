package dag

// Graph is what a workflow's DAG is drawn from: its jobs in declaration
// order, the steps of each, and the jobs each one needs.
type Graph struct {
	Jobs []Job
}

// Job is one node of a Graph.
type Job struct {
	// ID identifies the job in other jobs' Needs. It is the job's id, or its
	// name when it has none.
	ID    string
	Name  string
	Needs []string
	Steps []Step
}

// Step is one step of a Job.
type Step struct {
	// Name is the step's name, or the action it uses when it has none.
	Name string
	// Embedded is set for a step that runs a job from another file.
	Embedded bool
	// EmbeddedFile is the base name of that file, and EmbeddedSteps the names
	// of its steps. Both are empty when the file could not be read or has no
	// steps.
	EmbeddedFile  string
	EmbeddedSteps []string
}

// Renderer draws a Graph as text.
type Renderer interface {
	Render(g Graph) string
}
