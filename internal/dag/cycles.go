// Package dag provides generic graph algorithms that work with any data
// structure, and the renderers that draw a workflow's jobs as a graph.
package dag

// DetectCycleFn detects a cycle in a graph using a higher-order function.
// Returns the cycle path if found, nil otherwise.
//
// Parameters:
//   - allIDs: All node identifiers in the graph
//   - getDeps: Function that returns dependencies (children) for a given node
//
// Example:
//
//	getDeps := func(id string) []string {
//	    switch id {
//	    case "A": return []string{"B", "C"}
//	    case "B": return []string{"C"}
//	    default: return nil
//	    }
//	}
//	cycle := DetectCycleFn([]string{"A", "B", "C"}, getDeps)
func DetectCycleFn[ID comparable](allIDs []ID, getDeps func(ID) []ID) []ID {
	visited := make(map[ID]bool)
	recStack := make(map[ID]bool)
	path := make([]ID, 0)

	var dfs func(id ID) []ID
	dfs = func(id ID) []ID {
		if recStack[id] {
			// Found cycle - extract it from path
			for i, pid := range path {
				if pid == id {
					cycle := make([]ID, len(path)-i)
					copy(cycle, path[i:])
					return cycle
				}
			}
			return path
		}

		if visited[id] {
			return nil
		}

		visited[id] = true
		recStack[id] = true
		path = append(path, id)

		for _, dep := range getDeps(id) {
			if cycle := dfs(dep); cycle != nil {
				return cycle
			}
		}

		path = path[:len(path)-1]
		recStack[id] = false
		return nil
	}

	for _, id := range allIDs {
		if cycle := dfs(id); cycle != nil {
			return cycle
		}
	}

	return nil
}
