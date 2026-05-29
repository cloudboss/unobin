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
// The file is assumed to be validated. libs is the imported-library
// table; passed to ExtractNodes so composite call sites are expanded
// before edges are computed.
func BuildDAG(f *lang.File, libs map[string]*Library) *DAG {
	nodes := ExtractNodes(f, libs)
	g := &DAG{
		Nodes: make(map[string]*Node, len(nodes)),
		Edges: make(map[string][]string, len(nodes)),
	}
	for _, n := range nodes {
		g.Nodes[n.Address] = n
	}
	sl := newScopeLocals(f, g.Nodes)
	for _, n := range nodes {
		g.Edges[n.Address] = computeDeps(n, g.Nodes, sl)
	}
	return g
}

// scopeLocals resolves the `locals:` declarations for an evaluation
// scope. The stack body backs the root scope (the empty call site);
// every other scope is a composite call site whose locals come from
// the boundary node's composite body. Lookups are cached by template
// address.
type scopeLocals struct {
	stack map[string]lang.Expr
	nodes map[string]*Node
	cache map[string]map[string]lang.Expr
}

func newScopeLocals(f *lang.File, nodes map[string]*Node) *scopeLocals {
	return &scopeLocals{
		stack: localExprs(localsBlock(f)),
		nodes: nodes,
		cache: map[string]map[string]lang.Expr{},
	}
}

// forScope returns the locals declared in the scope named by callSite.
// The empty string names the stack root.
func (s *scopeLocals) forScope(callSite string) map[string]lang.Expr {
	if callSite == "" {
		return s.stack
	}
	tmpl := templateAddress(callSite)
	if m, ok := s.cache[tmpl]; ok {
		return m
	}
	var m map[string]lang.Expr
	if boundary, ok := s.nodes[tmpl]; ok {
		m = localExprs(localsBlock(boundary.CompositeBody))
	}
	s.cache[tmpl] = m
	return m
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
// internal nodes so its `outputs:` evaluation runs last. A leaf
// inside a composite walks up the boundary chain and inherits each
// boundary's body refs scoped by that boundary's own enclosing
// scope; this makes deeply nested leaves see the outer call sites'
// args, ensuring root nodes referenced by call args run before the
// leaf. Composite boundaries pick those deps up transitively via
// their leaves and don't walk up themselves. Var refs inside a
// leaf's own body are dropped: they name composite-scoped vars that
// resolve to call-site args, not anything in parent scope. Top-level
// nodes keep the original behavior: body refs and any `@depends-on`
// entries.
func computeDeps(n *Node, nodes map[string]*Node, sl *scopeLocals) []string {
	if n.IsComposite() {
		return internalsOf(n.Address, nodes)
	}
	var deps []string
	bodyRefs := bodyDeps(n.Body, sl.forScope(n.Composite))
	if n.Composite == "" {
		deps = bodyRefs
	} else {
		for _, ref := range bodyRefs {
			if strings.HasPrefix(ref, "var.") {
				continue
			}
			deps = append(deps, scopeRef(ref, n.Composite))
		}
	}
	for current := n.Composite; current != ""; {
		boundary, ok := nodes[current]
		if !ok {
			break
		}
		for _, ref := range refsWithLocals(boundary.Body, sl.forScope(boundary.Composite)) {
			deps = append(deps, scopeRef(ref, boundary.Composite))
		}
		current = boundary.Composite
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

// scopeRef rewrites a reference into a composite internal address.
// `resource.aws.vpc.this` under call site `resource.net.cluster.web`
// becomes `resource.net.cluster.web/resource.aws.vpc.this`; every
// segment keeps its own kind root, so resource, data, and action
// refs all join the same way, matching what `composeAddress` produces.
// Var refs and unsupported kinds pass through unchanged so toposort
// skips them. An empty callSite means the ref is already in its target
// scope (a top-level boundary's body refs, or a no-op when walking up
// past the outermost scope) and the ref returns unchanged.
func scopeRef(ref, callSite string) string {
	if callSite == "" {
		return ref
	}
	if strings.HasPrefix(ref, "resource.") ||
		strings.HasPrefix(ref, "data.") ||
		strings.HasPrefix(ref, "action.") {
		return callSite + "/" + ref
	}
	return ref
}

func bodyDeps(body lang.Expr, locals map[string]lang.Expr) []string {
	deps := refsWithLocals(body, locals)
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
