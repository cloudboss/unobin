package resolve

import "slices"

// Graph models import dependencies among UB files. Nodes are opaque
// string IDs assigned by the caller - typically a canonical filesystem
// path for local sources or `url@commit/subdir` for remote ones. Edges
// point from a file to each file it imports.
type Graph struct {
	edges map[string][]string
	order []string
}

// NewGraph returns an empty graph.
func NewGraph() *Graph {
	return &Graph{edges: make(map[string][]string)}
}

// AddNode registers id. AddEdge calls AddNode on both endpoints, so
// AddNode is only needed for isolated nodes (e.g., a leaf with no
// imports).
func (g *Graph) AddNode(id string) {
	if _, ok := g.edges[id]; !ok {
		g.edges[id] = nil
		g.order = append(g.order, id)
	}
}

// AddEdge records that `from` imports `to`. Duplicate edges are
// coalesced; both endpoints are registered if missing.
func (g *Graph) AddEdge(from, to string) {
	g.AddNode(from)
	g.AddNode(to)
	if slices.Contains(g.edges[from], to) {
		return
	}
	g.edges[from] = append(g.edges[from], to)
}

// DetectCycles returns every cycle in the graph. Each cycle is the
// sequence of node ids encountered along a back-edge, starting and
// ending at the same node. An acyclic graph returns nil.
func (g *Graph) DetectCycles() [][]string {
	visited := make(map[string]bool, len(g.edges))
	onStack := make(map[string]bool, len(g.edges))
	var cycles [][]string
	var stack []string

	var dfs func(node string)
	dfs = func(node string) {
		visited[node] = true
		onStack[node] = true
		stack = append(stack, node)
		for _, next := range g.edges[node] {
			if !visited[next] {
				dfs(next)
				continue
			}
			if onStack[next] {
				start := -1
				for i, s := range stack {
					if s == next {
						start = i
						break
					}
				}
				if start >= 0 {
					c := make([]string, 0, len(stack)-start+1)
					c = append(c, stack[start:]...)
					c = append(c, next)
					cycles = append(cycles, c)
				}
			}
		}
		onStack[node] = false
		stack = stack[:len(stack)-1]
	}
	for _, node := range g.order {
		if !visited[node] {
			dfs(node)
		}
	}
	return cycles
}
