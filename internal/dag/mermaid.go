package dag

import (
	"fmt"
	"regexp"
	"strings"
)

// Mermaid draws a Graph as a Mermaid flowchart: a subgraph for each job with
// its steps linked in run order, and an edge from each job to the jobs that
// need it.
type Mermaid struct{}

// Render draws g, or returns an empty string when it has no jobs.
func (m Mermaid) Render(g Graph) string {
	if len(g.Jobs) == 0 {
		return ""
	}

	var sb strings.Builder

	// Mermaid flowchart header (left-right direction)
	sb.WriteString("flowchart LR\n")

	// Jobs and steps share one namespace of node IDs, and sanitizing can map
	// different job IDs to the same one, so every node is given its own ID
	// here and edges look the job's up.
	ids := nodeIDs{}
	safeIDs := make([]string, len(g.Jobs))
	byJobID := make(map[string]string, len(g.Jobs))

	// Render node definitions with subgraphs for steps
	for i, job := range g.Jobs {
		safeID := ids.allocate(m.sanitizeID(job.ID))
		safeIDs[i] = safeID
		if _, ok := byJobID[job.ID]; !ok {
			byJobID[job.ID] = safeID
		}
		displayName := m.escapeLabel(job.Name)

		// Create subgraph for job with steps
		if len(job.Steps) > 0 {
			fmt.Fprintf(&sb, "    subgraph %s[\"%s\"]\n", safeID, displayName)
			// Jobs flow left to right; the steps inside a job run top to
			// bottom, in the order the edges below link them.
			sb.WriteString("        direction TB\n")
			stepIndex := 0
			var stepIDs []string
			for _, step := range job.Steps {
				stepID := ids.allocate(fmt.Sprintf("%s_step%d", safeID, stepIndex))
				stepLabel := m.escapeLabel(step.Name)
				fmt.Fprintf(&sb, "        %s[\"%s\"]\n", stepID, stepLabel)
				stepIDs = append(stepIDs, stepID)
				stepIndex++

				// Render the steps of an embedded job after the step that runs it
				for _, embStepName := range step.EmbeddedSteps {
					embStepID := ids.allocate(fmt.Sprintf("%s_step%d", safeID, stepIndex))
					embStepLabel := m.escapeLabel(embStepName)
					fmt.Fprintf(&sb, "        %s[\"%s\"]\n", embStepID, embStepLabel)
					stepIDs = append(stepIDs, embStepID)
					stepIndex++
				}
			}
			// Link the steps in the order they run, embedded ones included.
			for i := 1; i < len(stepIDs); i++ {
				fmt.Fprintf(&sb, "        %s --> %s\n", stepIDs[i-1], stepIDs[i])
			}
			sb.WriteString("    end\n")
		} else {
			// Job without steps - simple node
			fmt.Fprintf(&sb, "    %s[\"%s\"]\n", safeID, displayName)
		}
	}

	sb.WriteString("\n")

	// Render edges (dependencies)
	for i, job := range g.Jobs {
		for _, need := range job.Needs {
			safeNeedID, ok := byJobID[need]
			if !ok {
				// A need that names no job of the graph still gets an edge,
				// to a node Mermaid creates for it. The node gets an ID of
				// its own, so that it is not taken for a job or step drawn
				// above, and every reference to the same need shares it.
				safeNeedID = ids.allocate(m.sanitizeID(need))
				byJobID[need] = safeNeedID
			}
			fmt.Fprintf(&sb, "    %s --> %s\n", safeNeedID, safeIDs[i])
		}
	}

	return sb.String()
}

// sanitizeID converts a job ID/name to a valid Mermaid node ID
// Mermaid IDs should be alphanumeric with underscores
//
// Different IDs can sanitize to the same one (e.g., "unit-test" and
// "unit.test" both become "unit_test"); nodeIDs keeps the nodes apart.
func (Mermaid) sanitizeID(id string) string {
	// Replace non-alphanumeric characters with underscores
	reg := regexp.MustCompile(`[^a-zA-Z0-9_]`)
	sanitized := reg.ReplaceAllString(id, "_")

	// Ensure it starts with a letter (prepend 'n' if it starts with a number)
	if len(sanitized) > 0 && sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "n" + sanitized
	}

	// Ensure it's not empty
	if sanitized == "" {
		sanitized = "node"
	}

	return sanitized
}

// escapeLabel escapes special characters for Mermaid labels
func (Mermaid) escapeLabel(label string) string {
	// Escape double quotes
	label = strings.ReplaceAll(label, "\"", "#quot;")
	return label
}

// nodeIDs hands out the node IDs of one flowchart, each at most once. An ID
// already taken gets the first free numeric suffix, so "unit_test" becomes
// "unit_test_2", and a flowchart without collisions keeps its IDs as they are.
type nodeIDs map[string]bool

func (ids nodeIDs) allocate(base string) string {
	id := base
	for n := 2; ids[id]; n++ {
		id = fmt.Sprintf("%s_%d", base, n)
	}
	ids[id] = true
	return id
}
