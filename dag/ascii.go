package dag

import (
	"slices"
	"strings"

	"github.com/mozership/probe/internal/textwidth"
)

const (
	// Node border characters (string)
	nodeTopLeft        = "╭"
	nodeTopRight       = "╮"
	nodeBottomLeft     = "╰"
	nodeBottomRight    = "╯"
	nodeHorizontal     = "─"
	nodeVertical       = "│"
	nodeTeeRight       = "├"
	nodeTeeDown        = "┬"
	nodeTeeUp          = "┴"
	nodeTeeLeft        = "┤"
	nodeCross          = "┼"
	arrowDown          = "↓"
	stepBullet         = "○"
	stepBulletEmbedded = "↗"

	// Tree characters for embedded steps
	treeBranch = "├"
	treeEnd    = "└"

	// ANSI escape codes for dim text
	ansiDim   = "\033[2m"
	ansiReset = "\033[0m"

	// Connection line characters (rune)
	connVertical          = '│'
	connHorizontal        = '─'
	connTeeRight          = '├'
	connTeeLeft           = '┤'
	connTeeDown           = '┬'
	connTeeUp             = '┴'
	connCross             = '┼'
	connCornerTopLeft     = '┌'
	connCornerTopRight    = '┐'
	connCornerBottomLeft  = '└'
	connCornerBottomRight = '┘'

	// Node dimensions
	fixedNodeWidth = 25
)

// asciiNode is a job drawn as a box.
type asciiNode struct {
	Job         *Job
	JobID       string
	Level       int      // Depth in DAG (0 = root)
	Width       int      // Box width
	Lines       []string // Rendered lines
	CenterX     int      // X coordinate of center (for connections)
	HasChildren bool     // Whether this job has dependent jobs
}

// ASCII draws a Graph as boxes of jobs and their steps, joined by lines from
// each job to the jobs that need it.
type ASCII struct{}

// Render draws g, or returns an empty string when it has no jobs.
func (ASCII) Render(g Graph) string {
	return newASCIIRenderer(g).render()
}

// asciiRenderer holds the layout of one Graph while it is drawn.
type asciiRenderer struct {
	graph      Graph
	nodes      []*asciiNode
	levels     [][]int             // levels[level] = []jobIndex
	jobIDToIdx map[string]int      // jobID -> index in graph.Jobs
	children   map[string][]string // jobID -> list of child jobIDs (jobs that depend on this job)
	parents    map[string][]string // jobID -> list of parent jobIDs (jobs this job depends on)
}

func newASCIIRenderer(g Graph) *asciiRenderer {
	r := &asciiRenderer{
		graph:      g,
		nodes:      make([]*asciiNode, len(g.Jobs)),
		jobIDToIdx: make(map[string]int),
		children:   make(map[string][]string),
		parents:    make(map[string][]string),
	}

	// Build job ID to index mapping
	for i, job := range g.Jobs {
		id := job.ID
		r.jobIDToIdx[id] = i
	}

	// Build parent-child relationships
	for _, job := range g.Jobs {
		jobID := job.ID
		for _, need := range job.Needs {
			r.children[need] = append(r.children[need], jobID)
			r.parents[jobID] = append(r.parents[jobID], need)
		}
	}

	return r
}

// render draws the graph.
func (r *asciiRenderer) render() string {
	if len(r.graph.Jobs) == 0 {
		return ""
	}

	r.calculateLevels()
	r.createNodes()

	var result []string

	for level := 0; level < len(r.levels); level++ {
		// Render connection lines from previous level
		if level > 0 {
			connections := r.renderConnections(level - 1)
			result = append(result, connections...)
		}

		// Render nodes at this level
		levelLines := r.renderLevel(level)
		result = append(result, levelLines...)
	}

	return strings.Join(result, "\n") + "\n"
}

// calculateLevels assigns each job to a level based on dependencies (Sugiyama-style)
func (r *asciiRenderer) calculateLevels() {
	jobLevels := make(map[string]int)

	// Calculate level for each job
	var calcLevel func(jobID string) int
	calcLevel = func(jobID string) int {
		if level, exists := jobLevels[jobID]; exists {
			return level
		}

		idx, ok := r.jobIDToIdx[jobID]
		if !ok {
			return 0
		}

		job := r.graph.Jobs[idx]
		if len(job.Needs) == 0 {
			jobLevels[jobID] = 0
			return 0
		}

		maxParentLevel := -1
		for _, need := range job.Needs {
			parentLevel := calcLevel(need)
			if parentLevel > maxParentLevel {
				maxParentLevel = parentLevel
			}
		}

		level := maxParentLevel + 1
		jobLevels[jobID] = level
		return level
	}

	// Calculate levels for all jobs
	maxLevel := 0
	for _, job := range r.graph.Jobs {
		jobID := job.ID
		level := calcLevel(jobID)
		if level > maxLevel {
			maxLevel = level
		}
	}

	// Group jobs by level
	r.levels = make([][]int, maxLevel+1)
	for i, job := range r.graph.Jobs {
		jobID := job.ID
		level := jobLevels[jobID]
		r.levels[level] = append(r.levels[level], i)
	}

	// Sort jobs within each level: jobs with children first, then jobs without children
	for level := range r.levels {
		jobIndices := r.levels[level]
		withChildren := []int{}
		withoutChildren := []int{}

		for _, idx := range jobIndices {
			job := r.graph.Jobs[idx]
			jobID := job.ID
			if len(r.children[jobID]) > 0 {
				withChildren = append(withChildren, idx)
			} else {
				withoutChildren = append(withoutChildren, idx)
			}
		}

		r.levels[level] = append(withChildren, withoutChildren...)
	}
}

// createNodes creates an asciiNode for each job
func (r *asciiRenderer) createNodes() {
	for i, job := range r.graph.Jobs {
		jobID := job.ID

		// Check if this job has children (other jobs depend on it)
		hasChildren := len(r.children[jobID]) > 0

		node := &asciiNode{
			Job:         &r.graph.Jobs[i],
			JobID:       jobID,
			Width:       fixedNodeWidth, // Use fixed width for all nodes
			HasChildren: hasChildren,
		}

		// Find level for this job
		for level, jobIndices := range r.levels {
			if slices.Contains(jobIndices, i) {
				node.Level = level
			}
		}

		node.Lines = r.renderNode(node)
		r.nodes[i] = node
	}
}

// renderNode draws the box of a job.
func (r *asciiRenderer) renderNode(node *asciiNode) []string {
	var lines []string
	width := node.Width
	innerWidth := width - 2 // Width inside borders

	// Top border
	lines = append(lines, nodeTopLeft+strings.Repeat(nodeHorizontal, innerWidth)+nodeTopRight)

	// Job name (centered, truncate with ellipsis if too long)
	name := textwidth.Truncate(node.Job.Name, innerWidth-2) // -2 for padding
	padding := innerWidth - textwidth.Width(name)
	leftPad := padding / 2
	rightPad := padding - leftPad
	lines = append(lines, nodeVertical+strings.Repeat(" ", leftPad)+name+strings.Repeat(" ", rightPad)+nodeVertical)

	// Separator
	lines = append(lines, nodeTeeRight+strings.Repeat(nodeHorizontal, innerWidth)+nodeTeeLeft)

	// Steps
	for _, step := range node.Job.Steps {
		stepName := step.Name
		// Use different bullet for embedded actions
		bullet := stepBullet
		if step.Embedded {
			bullet = stepBulletEmbedded
		}
		// Format: " ○ stepname" or " ↗ stepname" with truncation
		prefix := " " + bullet + " "
		prefixWidth := textwidth.Width(prefix)
		maxStepNameWidth := innerWidth - prefixWidth
		truncatedStepName := textwidth.Truncate(stepName, maxStepNameWidth)
		stepLine := prefix + truncatedStepName
		// Pad to inner width
		stepLineWidth := textwidth.Width(stepLine)
		if stepLineWidth < innerWidth {
			stepLine = stepLine + strings.Repeat(" ", innerWidth-stepLineWidth)
		}
		lines = append(lines, nodeVertical+stepLine+nodeVertical)

		// Render embedded steps if this is an embedded action
		if len(step.EmbeddedSteps) > 0 {
			// Render each embedded step with tree characters
			for i, embStepName := range step.EmbeddedSteps {
				// Use ├ for non-last items, └ for last item
				var treeChar string
				if i == len(step.EmbeddedSteps)-1 {
					treeChar = treeEnd
				} else {
					treeChar = treeBranch
				}

				// Format: "   ├ stepname" or "   └ stepname"
				embPrefix := "   " + treeChar + " "
				embPrefixWidth := textwidth.Width(embPrefix)
				maxEmbStepNameWidth := innerWidth - embPrefixWidth
				truncatedEmbStepName := textwidth.Truncate(embStepName, maxEmbStepNameWidth)
				embStepLine := embPrefix + truncatedEmbStepName

				// Pad to inner width
				embStepLineWidth := textwidth.Width(embStepLine)
				if embStepLineWidth < innerWidth {
					embStepLine = embStepLine + strings.Repeat(" ", innerWidth-embStepLineWidth)
				}
				lines = append(lines, nodeVertical+embStepLine+nodeVertical)
			}

			// Add filename at the end with dim color (aligned with tree characters)
			filePrefix := "   "
			filePrefixWidth := textwidth.Width(filePrefix)
			maxFilenameWidth := innerWidth - filePrefixWidth
			truncatedFilename := textwidth.Truncate(step.EmbeddedFile, maxFilenameWidth)
			// Apply dim color
			dimFilename := ansiDim + truncatedFilename + ansiReset
			// Calculate padding based on non-colored text length
			fileLine := filePrefix + dimFilename
			actualWidth := filePrefixWidth + textwidth.Width(truncatedFilename)
			if actualWidth < innerWidth {
				fileLine = filePrefix + dimFilename + strings.Repeat(" ", innerWidth-actualWidth)
			}
			lines = append(lines, nodeVertical+fileLine+nodeVertical)
		}
	}

	// Bottom border - use ┬ in center if job has children
	if node.HasChildren {
		leftWidth := (innerWidth - 1) / 2
		rightWidth := innerWidth - 1 - leftWidth
		bottomBorder := nodeBottomLeft + strings.Repeat(nodeHorizontal, leftWidth) + nodeTeeDown + strings.Repeat(nodeHorizontal, rightWidth) + nodeBottomRight
		lines = append(lines, bottomBorder)
	} else {
		lines = append(lines, nodeBottomLeft+strings.Repeat(nodeHorizontal, innerWidth)+nodeBottomRight)
	}

	return lines
}

// flagsToChar converts connection flags to the appropriate box-drawing character
// Flags: 1=from_above, 2=to_below, 4=from_left, 8=to_right
func flagsToChar(flags int) rune {
	fromAbove := flags&1 != 0
	toBelow := flags&2 != 0
	fromLeft := flags&4 != 0
	toRight := flags&8 != 0

	switch {
	case fromAbove && toBelow && fromLeft && toRight:
		return connCross
	case fromAbove && toBelow && fromLeft:
		return connTeeLeft
	case fromAbove && toBelow && toRight:
		return connTeeRight
	case fromAbove && fromLeft && toRight:
		return connTeeUp
	case toBelow && fromLeft && toRight:
		return connTeeDown
	case fromAbove && toBelow:
		return connVertical
	case fromLeft && toRight:
		return connHorizontal
	case fromAbove && toRight:
		return connCornerBottomLeft
	case fromAbove && fromLeft:
		return connCornerBottomRight
	case toBelow && toRight:
		return connCornerTopLeft
	case toBelow && fromLeft:
		return connCornerTopRight
	case fromAbove:
		return connVertical
	case toBelow:
		return connVertical
	case fromLeft, toRight:
		return connHorizontal
	default:
		return ' '
	}
}

// renderLevel renders all nodes at a given level side by side
func (r *asciiRenderer) renderLevel(level int) []string {
	jobIndices := r.levels[level]
	if len(jobIndices) == 0 {
		return nil
	}

	// Get nodes for this level
	var levelNodes []*asciiNode
	for _, idx := range jobIndices {
		levelNodes = append(levelNodes, r.nodes[idx])
	}

	// Find max height
	maxHeight := 0
	for _, node := range levelNodes {
		if len(node.Lines) > maxHeight {
			maxHeight = len(node.Lines)
		}
	}

	// Calculate positions and set center X
	spacing := 2
	currentX := 0
	for _, node := range levelNodes {
		node.CenterX = currentX + node.Width/2
		currentX += node.Width + spacing
	}

	// Render lines
	var result []string
	for lineIdx := 0; lineIdx < maxHeight; lineIdx++ {
		var line strings.Builder
		for i, node := range levelNodes {
			if i > 0 {
				line.WriteString(strings.Repeat(" ", spacing))
			}
			if lineIdx < len(node.Lines) {
				line.WriteString(node.Lines[lineIdx])
			} else {
				// Node ended but other nodes continue - draw vertical line if this job has children
				if node.HasChildren {
					// Draw vertical line at center position
					centerPos := node.Width / 2
					line.WriteString(strings.Repeat(" ", centerPos))
					line.WriteString("│")
					line.WriteString(strings.Repeat(" ", node.Width-centerPos-1))
				} else {
					line.WriteString(strings.Repeat(" ", node.Width))
				}
			}
		}
		result = append(result, line.String())
	}

	return result
}

// renderConnections renders connection lines between levels
func (r *asciiRenderer) renderConnections(fromLevel int) []string {
	if fromLevel >= len(r.levels)-1 {
		return nil
	}

	parentIndices := r.levels[fromLevel]
	childIndices := r.levels[fromLevel+1]

	if len(parentIndices) == 0 || len(childIndices) == 0 {
		return nil
	}

	connections := r.buildConnectionMap(parentIndices, childIndices)
	if len(connections) == 0 {
		return nil
	}

	parentPositions, parentTotalWidth := r.calculateLevelPositions(parentIndices)
	childPositions, childTotalWidth := r.calculateLevelPositions(childIndices)

	totalWidth := max(parentTotalWidth, childTotalWidth)

	parentsWithConnections := r.getConnectedParents(connections)

	var result []string
	result = append(result, r.renderVerticalLine(parentPositions, parentsWithConnections, totalWidth))

	if r.needsRoutingLines(connections, parentPositions, childPositions) {
		result = append(result, r.renderRoutingLine(connections, parentPositions, childPositions, totalWidth))
	} else {
		result = append(result, r.renderVerticalLine(parentPositions, parentsWithConnections, totalWidth))
	}

	result = append(result, r.renderArrowLine(childPositions, connections, totalWidth))

	return result
}

// buildConnectionMap builds a map of childIdx -> []parentIdx for dependencies at the given level
func (r *asciiRenderer) buildConnectionMap(parentIndices, childIndices []int) map[int][]int {
	connections := make(map[int][]int)
	for _, childIdx := range childIndices {
		childNode := r.nodes[childIdx]
		for _, parentIdx := range parentIndices {
			parentNode := r.nodes[parentIdx]
			if slices.Contains(childNode.Job.Needs, parentNode.JobID) {
				connections[childIdx] = append(connections[childIdx], parentIdx)
			}
		}
	}
	return connections
}

// calculateLevelPositions calculates the center X positions for each job at a level
func (r *asciiRenderer) calculateLevelPositions(jobIndices []int) (positions map[int]int, totalWidth int) {
	const spacing = 2
	positions = make(map[int]int)
	currentX := 0
	for _, idx := range jobIndices {
		node := r.nodes[idx]
		positions[idx] = currentX + node.Width/2
		currentX += node.Width + spacing
	}
	if currentX > 0 {
		totalWidth = currentX - spacing
	}
	return positions, totalWidth
}

// getConnectedParents returns a set of parent indices that have connections
func (r *asciiRenderer) getConnectedParents(connections map[int][]int) map[int]bool {
	parentsWithConnections := make(map[int]bool)
	for _, parentList := range connections {
		for _, parentIdx := range parentList {
			parentsWithConnections[parentIdx] = true
		}
	}
	return parentsWithConnections
}

// needsRoutingLines checks if routing lines are needed (when parent and child positions differ)
func (r *asciiRenderer) needsRoutingLines(connections map[int][]int, parentPositions, childPositions map[int]int) bool {
	for childIdx, parents := range connections {
		childPos := childPositions[childIdx]
		for _, parentIdx := range parents {
			if parentPositions[parentIdx] != childPos {
				return true
			}
		}
	}
	return false
}

// renderVerticalLine renders a line with vertical bars at the specified positions
func (r *asciiRenderer) renderVerticalLine(positions map[int]int, connectedIndices map[int]bool, totalWidth int) string {
	line := make([]rune, totalWidth)
	for i := range line {
		line[i] = ' '
	}
	for idx := range connectedIndices {
		if pos, ok := positions[idx]; ok && pos < len(line) {
			line[pos] = '│'
		}
	}
	return string(line)
}

// renderRoutingLine renders the routing line with appropriate box-drawing characters
func (r *asciiRenderer) renderRoutingLine(connections map[int][]int, parentPositions, childPositions map[int]int, totalWidth int) string {
	// Collect connection flags for each position
	// Flags: 1=from_above, 2=to_below, 4=from_left, 8=to_right
	posFlags := make(map[int]int)

	for childIdx, parents := range connections {
		childPos := childPositions[childIdx]

		for _, parentIdx := range parents {
			parentPos := parentPositions[parentIdx]

			if parentPos == childPos {
				// Straight vertical
				posFlags[parentPos] |= 1 | 2 // from_above | to_below
			} else if parentPos < childPos {
				// Parent is left of child
				posFlags[parentPos] |= 1 | 8 // from_above | to_right
				posFlags[childPos] |= 4 | 2  // from_left | to_below
				for i := parentPos + 1; i < childPos; i++ {
					posFlags[i] |= 4 | 8 // horizontal
				}
			} else {
				// Parent is right of child
				posFlags[childPos] |= 8 | 2  // to_right | to_below
				posFlags[parentPos] |= 4 | 1 // from_left | from_above
				for i := childPos + 1; i < parentPos; i++ {
					posFlags[i] |= 4 | 8 // horizontal
				}
			}
		}
	}

	line := make([]rune, totalWidth)
	for i := range line {
		line[i] = ' '
	}
	for pos, flags := range posFlags {
		if pos < len(line) {
			line[pos] = flagsToChar(flags)
		}
	}
	return string(line)
}

// renderArrowLine renders the arrow line pointing to child positions
func (r *asciiRenderer) renderArrowLine(childPositions map[int]int, connections map[int][]int, totalWidth int) string {
	line := make([]rune, totalWidth)
	for i := range line {
		line[i] = ' '
	}
	for childIdx := range connections {
		if pos, ok := childPositions[childIdx]; ok && pos < len(line) {
			line[pos] = '↓'
		}
	}
	return string(line)
}
