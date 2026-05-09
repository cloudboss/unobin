package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// DAG is a stack's runtime dependency graph: every addressable node
// indexed by its address, and the list of node addresses each one
// depends on, collected from references in the body and from any
// `@depends-on` meta key.
type DAG struct {
	Nodes map[string]*Node
	Edges map[string][]string
}

// BuildDAG walks a parsed stack file and returns its dependency graph.
// The file is assumed to be validated. mods is the imported-module
// table; passed to ExtractNodes so composite call sites are expanded
// before edges are computed.
func BuildDAG(f *lang.File, mods map[string]*Module) *DAG {
	nodes := ExtractNodes(f, mods)
	g := &DAG{
		Nodes: make(map[string]*Node, len(nodes)),
		Edges: make(map[string][]string, len(nodes)),
	}
	for _, n := range nodes {
		g.Nodes[n.Address] = n
	}
	for _, n := range nodes {
		g.Edges[n.Address] = computeDeps(n, g.Nodes)
	}
	return g
}

// TopologicalOrder returns the DAG's nodes in dependency order: every
// node appears after the nodes it references. Edges to non-node addresses
// such as `var.X` are skipped, since vars are bound from inputs and not
// block execution. Returns an error naming the involved addresses when
// the graph contains a cycle.
func (g *DAG) TopologicalOrder() ([]string, error) {
	inDegree := make(map[string]int, len(g.Nodes))
	dependents := make(map[string][]string, len(g.Nodes))
	for addr := range g.Nodes {
		inDegree[addr] = 0
	}
	for from, deps := range g.Edges {
		for _, to := range deps {
			if _, ok := g.Nodes[to]; !ok {
				continue
			}
			dependents[to] = append(dependents[to], from)
			inDegree[from]++
		}
	}

	var ready []string
	for addr, d := range inDegree {
		if d == 0 {
			ready = append(ready, addr)
		}
	}
	sort.Strings(ready)

	order := make([]string, 0, len(g.Nodes))
	for len(ready) > 0 {
		cur := ready[0]
		ready = ready[1:]
		order = append(order, cur)
		next := dependents[cur]
		sort.Strings(next)
		for _, dep := range next {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				ready = append(ready, dep)
			}
		}
		sort.Strings(ready)
	}

	if len(order) != len(g.Nodes) {
		var stuck []string
		for addr, d := range inDegree {
			if d > 0 {
				stuck = append(stuck, addr)
			}
		}
		sort.Strings(stuck)
		return nil, fmt.Errorf("cycle detected among: %v", stuck)
	}
	return order, nil
}

// computeDeps returns the addresses n depends on, taking composite
// scope into account. A composite boundary depends on each of its
// internal nodes so its `outputs:` evaluation runs last. An internal
// node depends on its body's resource refs, rewritten so they point
// at the prefixed sibling addresses, plus the boundary's body refs.
// Var refs inside an internal are dropped: they name composite-scoped
// vars that resolve to call-site args, not anything in parent scope.
// The boundary's body refs are the call-site args themselves, which
// carry the parent-scope dependencies the composite needs resolved
// before any internal can run. Other nodes keep the original behavior:
// body refs and any `@depends-on` entries.
func computeDeps(n *Node, nodes map[string]*Node) []string {
	if n.Kind == NodeComposite {
		return internalsOf(n.Address, nodes)
	}
	deps := bodyDeps(n.Body)
	if n.Composite != "" {
		scoped := make([]string, 0, len(deps))
		for _, d := range deps {
			if strings.HasPrefix(d, "var.") {
				continue
			}
			scoped = append(scoped, scopeRef(d, n.Composite))
		}
		deps = scoped
		if boundary := nodes[n.Composite]; boundary != nil {
			deps = append(deps, Refs(boundary.Body)...)
		}
	}
	return dedupe(deps)
}

func internalsOf(callSite string, nodes map[string]*Node) []string {
	var out []string
	for _, m := range nodes {
		if m.Composite == callSite {
			out = append(out, m.Address)
		}
	}
	sort.Strings(out)
	return out
}

// scopeRef rewrites a root reference into a composite internal
// address. `resource.aws.vpc.this` under call site
// `resource.net.cluster.web` becomes
// `resource.net.cluster.web/aws.vpc.this`. Var refs and unsupported
// kinds pass through unchanged so toposort skips them.
func scopeRef(ref, callSite string) string {
	if strings.HasPrefix(ref, "resource.") {
		return callSite + "/" + strings.TrimPrefix(ref, "resource.")
	}
	return ref
}

func bodyDeps(body lang.Expr) []string {
	deps := Refs(body)
	if obj, ok := body.(*lang.ObjectLit); ok {
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@depends-on" {
				continue
			}
			arr, ok := fld.Value.(*lang.ArrayLit)
			if !ok {
				continue
			}
			for _, el := range arr.Elements {
				dp, ok := el.(*lang.DotPath)
				if !ok {
					continue
				}
				if addr := refAddress(dp); addr != "" {
					deps = append(deps, addr)
				}
			}
		}
	}
	return deps
}
