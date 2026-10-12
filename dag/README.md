# dag

Generic graph algorithms that work with any data structure, and the
renderers that draw a workflow's jobs as a graph.

## Installation

```go
import "github.com/mozership/probe/dag"
```

## Algorithms

### Cycle Detection

Detects cycles in a graph using Depth-First Search (DFS) with a recursion stack.

| Function | Description | Complexity |
|----------|-------------|------------|
| `DetectCycleFn` | Detect cycle and return the cycle path | O(V + E) |

## Usage

Pass a list of IDs and a dependency getter function. Works with any data structure.

```go
// Dependencies: A → B → C → A
getDeps := func(id string) []string {
    switch id {
    case "A":
        return []string{"B"}
    case "B":
        return []string{"C"}
    case "C":
        return []string{"A"}
    default:
        return nil
    }
}
allIDs := []string{"A", "B", "C"}

if cycle := dag.DetectCycleFn(allIDs, getDeps); cycle != nil {
    fmt.Println("Cycle found:", cycle) // [A B C]
}
```

## Rendering

A `Graph` holds what a drawing needs: the jobs in declaration order, the
steps of each, and the jobs each one needs. A `Renderer` draws it as text.

| Renderer | Output |
|----------|--------|
| `ASCII` | Boxes of jobs and their steps, joined by lines to the jobs that need them |
| `Mermaid` | A Mermaid flowchart with a subgraph for each job, its steps linked in run order |

```go
g := dag.Graph{Jobs: []dag.Job{
    {ID: "build", Name: "Build", Steps: []dag.Step{{Name: "Compile"}}},
    {ID: "test", Name: "Test", Needs: []string{"build"}, Steps: []dag.Step{{Name: "Run tests"}}},
}}

fmt.Print(dag.ASCII{}.Render(g))
fmt.Print(dag.Mermaid{}.Render(g))
```

A workflow builds its graph with `Workflow.Graph`, which also reads the steps
of the jobs that embedded steps run.
