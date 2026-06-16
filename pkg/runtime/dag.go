package runtime

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// DAG is a stack's runtime dependency graph: every addressable node
// indexed by its address, and the list of node addresses each one
// depends on, collected from references in the body and from any
// `@depends-on` meta key.
type DAG struct {
	Nodes map[string]*Node
	Edges map[string][]string
}

// BuildDAG is the generic compatibility entrypoint for tests and
// helpers that still construct lang.File bodies directly. Production
// grammar-first callers use BuildSyntaxDAG.
func BuildDAG(f *lang.File, libs map[string]*Library) *DAG {
	nodes := ExtractNodes(f, libs)
	return buildDAG(nodes, lang.FieldMap(localsBlock(f)))
}

// BuildSyntaxDAG builds the dependency graph from a typed factory or
// composite body.
func BuildSyntaxDAG(body syntax.FactoryBody, libs map[string]*Library) *DAG {
	nodes := ExtractSyntaxNodes(body, libs)
	return buildDAG(nodes, syntaxLocalMap(body.Locals))
}

func buildDAG(nodes []*Node, rootLocals map[string]lang.Expr) *DAG {
	g := &DAG{
		Nodes: make(map[string]*Node, len(nodes)),
		Edges: make(map[string][]string, len(nodes)),
	}
	for _, n := range nodes {
		g.Nodes[n.Address] = n
	}
	sl := newScopeLocals(rootLocals, g.Nodes)
	boundaryRefs := map[string][]string{}
	for _, n := range nodes {
		g.Edges[n.Address] = computeDeps(n, g.Nodes, sl, boundaryRefs)
	}
	return g
}

func syntaxLocalMap(decls []syntax.LocalDecl) map[string]lang.Expr {
	out := map[string]lang.Expr{}
	for _, decl := range decls {
		out[decl.Name.Name] = decl.Value
	}
	return out
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

func newScopeLocals(root map[string]lang.Expr, nodes map[string]*Node) *scopeLocals {
	return &scopeLocals{
		stack: root,
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
		if boundary.CompositeSyntaxBody != nil {
			m = syntaxLocalMap(boundary.CompositeSyntaxBody.Locals)
		} else {
			m = lang.FieldMap(localsBlock(boundary.CompositeBody))
		}
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
	for _, next := range dependents {
		slices.Sort(next)
	}

	var ready []string
	for addr, d := range inDegree {
		if d == 0 {
			ready = append(ready, addr)
		}
	}
	slices.Sort(ready)

	// The queue stays sorted by inserting each newly ready address in
	// place, so the head is always the smallest and the order is
	// deterministic without re-sorting per iteration.
	order := make([]string, 0, len(g.Nodes))
	for len(ready) > 0 {
		cur := ready[0]
		ready = ready[1:]
		order = append(order, cur)
		for _, dep := range dependents[cur] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				i, _ := slices.BinarySearch(ready, dep)
				ready = slices.Insert(ready, i, dep)
			}
		}
	}

	if len(order) != len(g.Nodes) {
		var stuck []string
		for addr, d := range inDegree {
			if d > 0 {
				stuck = append(stuck, addr)
			}
		}
		slices.Sort(stuck)
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
func computeDeps(
	n *Node, nodes map[string]*Node, sl *scopeLocals, boundaryRefs map[string][]string,
) []string {
	if n.IsComposite() {
		return internalsOf(n.Address, nodes)
	}
	deps := bodyDeps(n.Body, sl.forScope(n.Composite), nodes, n.Composite)
	if n.Composite != "" {
		deps = withoutVars(deps)
	}
	for current := n.Composite; current != ""; {
		boundary, ok := nodes[current]
		if !ok {
			break
		}
		refs, ok := boundaryRefs[current]
		if !ok {
			refs = refsWithLocalsInScope(
				boundary.Body,
				sl.forScope(boundary.Composite),
				nodes,
				boundary.Composite,
			)
			boundaryRefs[current] = refs
		}
		deps = append(deps, refs...)
		current = boundary.Composite
	}
	if dep, ok := configurationDep(n, nodes); ok {
		deps = append(deps, dep)
	}
	return dedupe(deps)
}

func withoutVars(refs []string) []string {
	out := refs[:0]
	for _, ref := range refs {
		if strings.HasPrefix(ref, "var.") {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// UnderForEachComposite reports whether any composite call site in
// n's ancestry is itself a `@for-each` template.
func (g *DAG) UnderForEachComposite(n *Node) bool {
	for cur := n.Composite; cur != ""; {
		b, ok := g.Nodes[cur]
		if !ok {
			return false
		}
		if b.IsComposite() && b.ForEach != nil {
			return true
		}
		cur = b.Composite
	}
	return false
}

// ConfigurationSelections returns every configuration selection the
// graph's leaves resolve to, explicit or implicit default, keyed by
// the resolved alias and limited to libraries that declare a
// configuration. The factory's help output uses it to say which
// names an operator must supply.
func (g *DAG) ConfigurationSelections(libs map[string]*Library) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, n := range g.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction:
		default:
			continue
		}
		if n.IsComposite() {
			continue
		}
		alias, configuration := resolvedConfigRef(n, g.Nodes)
		lib := libs[alias]
		if lib == nil || lib.Configuration == nil {
			continue
		}
		set := out[alias]
		if set == nil {
			set = map[string]bool{}
			out[alias] = set
		}
		set[configuration] = true
	}
	return out
}

// configurationDep returns the address of the configuration node a
// leaf's resolved selection names, when the factory defines that
// configuration internally. The edge orders every consumer after the
// values its configuration derives from.
func configurationDep(n *Node, nodes map[string]*Node) (string, bool) {
	switch n.Kind {
	case NodeResource, NodeData, NodeAction:
	default:
		return "", false
	}
	alias, configuration := resolvedConfigRef(n, nodes)
	addr, ok := configurationNodeAddress(nodes, alias, configuration)
	if !ok {
		return "", false
	}
	return addr, true
}

func internalsOf(callSite string, nodes map[string]*Node) []string {
	var out []string
	for _, m := range nodes {
		if m.Composite == callSite {
			out = append(out, m.Address)
		}
	}
	slices.Sort(out)
	return out
}

// scopeRef rewrites a reference into a composite internal address.
// `resource.inner` under call site `resource.outer` becomes
// `resource.outer/resource.inner`; every segment keeps its own kind
// root, so resource, data, and action refs all join the same way.
// Var refs and unsupported kinds pass through unchanged so toposort
// skips them. An empty callSite means the ref is already in its target
// scope (a top-level boundary's body refs, or a no-op when walking up
// past the outermost scope) and the ref returns unchanged.
func ScopeRef(ref, callSite string) string {
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

func bodyDeps(
	body lang.Expr,
	locals map[string]lang.Expr,
	nodes map[string]*Node,
	scope string,
) []string {
	return append(
		refsWithLocalsInScope(body, locals, nodes, scope),
		explicitDeps(body, nodes, scope)...,
	)
}

// explicitDeps returns the addresses a body's @depends-on entry names.
func explicitDeps(body lang.Expr, nodes map[string]*Node, scope string) []string {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil
	}
	var deps []string
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
			if match, ok := RefMatchInScope(dp, nodes, scope); ok {
				deps = append(deps, match.Address)
			}
		}
	}
	return deps
}
