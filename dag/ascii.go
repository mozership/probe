package dag

import (
	"cmp"
	"slices"
	"strconv"
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
	nodeSpacing    = 2
)

// columnX returns the x coordinate where a node in column col starts.
func columnX(col int) int {
	return col * (fixedNodeWidth + nodeSpacing)
}

// asciiNode is a job drawn as a box.
type asciiNode struct {
	Job         *Job
	JobID       string
	Level       int      // Depth in DAG (0 = root)
	Width       int      // Box width
	Lines       []string // Rendered lines
	CenterX     int      // X coordinate of center (for connections)
	HasChildren bool     // Whether this job has dependent jobs
	Needs       []string // IDs of the nodes on the level above that this node joins
	Column      int      // Column of the node within its level
	// Pass marks a node that is only a line running through its level, for a
	// need that skips levels. It has no Job.
	Pass bool
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

// createNodes creates an asciiNode for each job, and a pass node for each
// level a need skips.
func (r *asciiRenderer) createNodes() {
	for i, job := range r.graph.Jobs {
		node := &asciiNode{
			Job:   &r.graph.Jobs[i],
			JobID: job.ID,
			Width: fixedNodeWidth, // Use fixed width for all nodes
			Needs: slices.Clone(job.Needs),
		}

		// Find level for this job
		for level, jobIndices := range r.levels {
			if slices.Contains(jobIndices, i) {
				node.Level = level
			}
		}

		r.nodes[i] = node
	}

	r.addPassNodes()

	r.children = make(map[string][]string)
	for _, node := range r.nodes {
		for _, need := range node.Needs {
			r.children[need] = append(r.children[need], node.JobID)
		}
	}
	for _, node := range r.nodes {
		node.HasChildren = len(r.children[node.JobID]) > 0
		if !node.Pass {
			node.Lines = r.renderNode(node)
		}
	}

	r.assignColumns()
}

// addPassNodes routes each need that skips levels through a pass node on
// every level in between, because lines are drawn only between adjacent
// levels. The skipping needs of one job share its pass nodes, so they join
// into one line.
func (r *asciiRenderer) addPassNodes() {
	jobs := r.nodes[:len(r.graph.Jobs)]
	byID := make(map[string]*asciiNode, len(jobs))
	for _, node := range jobs {
		byID[node.JobID] = node
	}

	for _, child := range jobs {
		var far []*asciiNode
		top := child.Level
		for _, need := range child.Needs {
			parent, ok := byID[need]
			if !ok || parent.Level >= child.Level-1 {
				continue
			}
			far = append(far, parent)
			top = min(top, parent.Level)
		}
		if len(far) == 0 {
			continue
		}

		passes := make(map[int]*asciiNode)
		for level := top + 1; level < child.Level; level++ {
			pass := &asciiNode{
				JobID: "\x00pass:" + child.JobID + ":" + strconv.Itoa(level),
				Level: level,
				Width: fixedNodeWidth,
				Pass:  true,
			}
			if above, ok := passes[level-1]; ok {
				pass.Needs = append(pass.Needs, above.JobID)
			}
			passes[level] = pass
			r.nodes = append(r.nodes, pass)
			r.levels[level] = append(r.levels[level], len(r.nodes)-1)
		}
		for _, parent := range far {
			pass := passes[parent.Level+1]
			pass.Needs = append(pass.Needs, parent.JobID)
		}

		needs := []string{}
		for _, need := range child.Needs {
			if !slices.ContainsFunc(far, func(n *asciiNode) bool { return n.JobID == need }) {
				needs = append(needs, need)
			}
		}
		child.Needs = append(needs, passes[child.Level-1].JobID)
	}
}

// assignColumns places the nodes of each level in columns. A pass node takes
// the rightmost column of the nodes it joins, or the next free one after it,
// so its line runs straight down as far as it can. Jobs then fill the free
// columns from the left in order.
func (r *asciiRenderer) assignColumns() {
	columns := make(map[string]int, len(r.nodes))
	for level, indices := range r.levels {
		var passes, jobs []*asciiNode
		for _, idx := range indices {
			if node := r.nodes[idx]; node.Pass {
				passes = append(passes, node)
			} else {
				jobs = append(jobs, node)
			}
		}

		wants := make(map[*asciiNode]int, len(passes))
		for _, pass := range passes {
			for _, need := range pass.Needs {
				wants[pass] = max(wants[pass], columns[need])
			}
		}
		slices.SortStableFunc(passes, func(a, b *asciiNode) int { return cmp.Compare(wants[a], wants[b]) })

		taken := make(map[int]bool)
		for _, pass := range passes {
			col := wants[pass]
			for taken[col] {
				col++
			}
			pass.Column = col
			taken[col] = true
		}
		col := 0
		for _, job := range jobs {
			for taken[col] {
				col++
			}
			job.Column = col
			taken[col] = true
		}

		for _, idx := range indices {
			columns[r.nodes[idx].JobID] = r.nodes[idx].Column
		}
		slices.SortStableFunc(r.levels[level], func(a, b int) int {
			return cmp.Compare(r.nodes[a].Column, r.nodes[b].Column)
		})
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

	// Set center X
	for _, node := range levelNodes {
		node.CenterX = columnX(node.Column) + node.Width/2
	}

	// Render lines
	var result []string
	for lineIdx := 0; lineIdx < maxHeight; lineIdx++ {
		var line strings.Builder
		x := 0
		for _, node := range levelNodes {
			start := columnX(node.Column)
			line.WriteString(strings.Repeat(" ", start-x))
			x = start + node.Width
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

	groups := groupConnections(connections, childIndices, parentPositions, childPositions)
	rows, straight := routeGroups(groups)
	if len(rows) == 0 {
		result = append(result, r.renderVerticalLine(parentPositions, parentsWithConnections, totalWidth))
	}
	for row := range rows {
		result = append(result, renderRoutingRow(row, rows, straight, totalWidth))
	}

	result = append(result, r.renderArrowLine(childPositions, connections, totalWidth))

	return result
}

// connGroup is a set of connections joined by shared parents or children.
// It is drawn as one line on a routing row, and two groups whose lines would
// overlap are drawn on different rows so that they do not read as joined.
type connGroup struct {
	edges    [][2]int // {parent x, child x}
	parents  []int    // x of the parents
	children []int    // x of the children
	lo, hi   int      // span of the line
}

// isStraight reports whether the group is a single line straight down.
func (g *connGroup) isStraight() bool {
	return g.lo == g.hi
}

// overlaps reports whether the lines of g and h share any column.
func (g *connGroup) overlaps(h *connGroup) bool {
	return g.lo <= h.hi && h.lo <= g.hi
}

// feeds reports whether a parent of g sits above a child of h. The line of g
// must then come first, so that the parent's line ends before the child's
// begins and the two do not read as one.
func (g *connGroup) feeds(h *connGroup) bool {
	for _, p := range g.parents {
		if slices.Contains(h.children, p) {
			return true
		}
	}
	return false
}

// groupConnections splits the connections into groups joined by shared
// parents or children, ordered by where their lines start.
func groupConnections(connections map[int][]int, childIndices []int, parentPositions, childPositions map[int]int) []*connGroup {
	// Union parents and children; parent keys are negated to keep them apart
	// from child keys.
	root := make(map[int]int)
	var find func(k int) int
	find = func(k int) int {
		if p, ok := root[k]; ok && p != k {
			root[k] = find(p)
			return root[k]
		}
		root[k] = k
		return k
	}
	parentKey := func(idx int) int { return -idx - 1 }
	for _, childIdx := range childIndices {
		for _, parentIdx := range connections[childIdx] {
			root[find(parentKey(parentIdx))] = find(childIdx)
		}
	}

	byRoot := make(map[int]*connGroup)
	var groups []*connGroup
	for _, childIdx := range childIndices {
		for _, parentIdx := range connections[childIdx] {
			k := find(childIdx)
			g, ok := byRoot[k]
			if !ok {
				g = &connGroup{lo: childPositions[childIdx], hi: childPositions[childIdx]}
				byRoot[k] = g
				groups = append(groups, g)
			}
			px, cx := parentPositions[parentIdx], childPositions[childIdx]
			g.edges = append(g.edges, [2]int{px, cx})
			if !slices.Contains(g.parents, px) {
				g.parents = append(g.parents, px)
			}
			if !slices.Contains(g.children, cx) {
				g.children = append(g.children, cx)
			}
			g.lo = min(g.lo, px, cx)
			g.hi = max(g.hi, px, cx)
		}
	}
	slices.SortStableFunc(groups, func(a, b *connGroup) int { return cmp.Compare(a.lo, b.lo) })
	return groups
}

// routeGroups puts the groups that turn on routing rows, giving overlapping
// groups different rows and a group that feeds another an earlier row. It
// returns the rows and, apart, the groups that run straight down, which
// cross every row.
func routeGroups(groups []*connGroup) (rows [][]*connGroup, straight []*connGroup) {
	var turning []*connGroup
	for _, g := range groups {
		if g.isStraight() {
			straight = append(straight, g)
		} else {
			turning = append(turning, g)
		}
	}

	// Order the groups so that one feeding another comes first, keeping the
	// given order otherwise and when they feed each other.
	var order []*connGroup
	done := make(map[*connGroup]bool)
	for len(order) < len(turning) {
		var next *connGroup
		for _, g := range turning {
			if done[g] {
				continue
			}
			if next == nil {
				next = g
			}
			if !slices.ContainsFunc(turning, func(h *connGroup) bool { return h != g && !done[h] && h.feeds(g) }) {
				next = g
				break
			}
		}
		order = append(order, next)
		done[next] = true
	}

	rowOf := make(map[*connGroup]int)
	for i, g := range order {
		row := 0
		for _, h := range order[:i] {
			if g.overlaps(h) && h.feeds(g) {
				row = max(row, rowOf[h]+1)
			}
		}
		for row < len(rows) && slices.ContainsFunc(rows[row], g.overlaps) {
			row++
		}
		if row == len(rows) {
			rows = append(rows, nil)
		}
		rows[row] = append(rows[row], g)
		rowOf[g] = row
	}
	return rows, straight
}

// renderRoutingRow renders one routing row: the lines of the groups on it,
// the lines of parents whose groups turn on a later row, the lines to
// children whose groups turned on an earlier row, and the straight lines.
// Where one of those passing lines crosses a line of this row without joining
// it, the crossing line is cut beside it, so that it does not read as a
// junction.
func renderRoutingRow(row int, rows [][]*connGroup, straight []*connGroup, totalWidth int) string {
	// Collect connection flags for each position
	// Flags: 1=from_above, 2=to_below, 4=from_left, 8=to_right
	passFlags := make(map[int]int) // lines running straight through the row
	edgeFlags := make(map[int]int) // lines of the groups on the row

	for _, g := range straight {
		passFlags[g.lo] |= 1 | 2
	}
	for i, groups := range rows {
		for _, g := range groups {
			switch {
			case i < row:
				for _, x := range g.children {
					passFlags[x] |= 1 | 2
				}
			case i > row:
				for _, x := range g.parents {
					passFlags[x] |= 1 | 2
				}
			default:
				addEdgeFlags(edgeFlags, g.edges)
			}
		}
	}

	line := make([]rune, totalWidth)
	for i := range line {
		line[i] = ' '
	}
	for pos := range totalWidth {
		if flags := passFlags[pos] | edgeFlags[pos]; flags != 0 {
			line[pos] = flagsToChar(flags)
		}
	}
	for pos := range passFlags {
		if pos >= len(line) || edgeFlags[pos] != 4|8 {
			continue
		}
		line[pos] = connVertical
		for _, side := range []int{pos - 1, pos + 1} {
			if side >= 0 && side < len(line) && passFlags[side] == 0 && edgeFlags[side] == 4|8 {
				line[side] = ' '
			}
		}
	}
	return string(line)
}

// addEdgeFlags adds the flags that draw each {parent x, child x} edge.
func addEdgeFlags(posFlags map[int]int, edges [][2]int) {
	for _, e := range edges {
		parentPos, childPos := e[0], e[1]

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

// buildConnectionMap builds a map of childIdx -> []parentIdx for dependencies at the given level
func (r *asciiRenderer) buildConnectionMap(parentIndices, childIndices []int) map[int][]int {
	connections := make(map[int][]int)
	for _, childIdx := range childIndices {
		childNode := r.nodes[childIdx]
		for _, parentIdx := range parentIndices {
			parentNode := r.nodes[parentIdx]
			if slices.Contains(childNode.Needs, parentNode.JobID) {
				connections[childIdx] = append(connections[childIdx], parentIdx)
			}
		}
	}
	return connections
}

// calculateLevelPositions calculates the center X positions for each job at a level
func (r *asciiRenderer) calculateLevelPositions(jobIndices []int) (positions map[int]int, totalWidth int) {
	positions = make(map[int]int)
	for _, idx := range jobIndices {
		node := r.nodes[idx]
		start := columnX(node.Column)
		positions[idx] = start + node.Width/2
		totalWidth = max(totalWidth, start+node.Width)
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

// renderArrowLine renders the arrow line pointing to child positions. A pass
// node gets a plain line, since the line goes on through its level.
func (r *asciiRenderer) renderArrowLine(childPositions map[int]int, connections map[int][]int, totalWidth int) string {
	line := make([]rune, totalWidth)
	for i := range line {
		line[i] = ' '
	}
	for childIdx := range connections {
		if pos, ok := childPositions[childIdx]; ok && pos < len(line) {
			line[pos] = '↓'
			if r.nodes[childIdx].Pass {
				line[pos] = connVertical
			}
		}
	}
	return string(line)
}
