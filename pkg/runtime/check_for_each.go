package runtime

import (
	"maps"
	"slices"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ForEachNesting reports any node declaring @for-each whose enclosing
// composite chain already iterates: no fan-out exists for an
// iteration nested inside another, and a body that never reads @each
// would otherwise quietly plan a single instance where several were
// declared. The compile command runs this with every composite body
// the stack reaches expanded, so the factory binary never holds the
// construct.
func (c *Checker) ForEachNesting() *lang.ErrorList {
	errs := lang.NewErrorList(0)
	for _, addr := range slices.Sorted(maps.Keys(c.dag.Nodes)) {
		n := c.dag.Nodes[addr]
		if n.ForEach == nil || !underForEachComposite(c.dag.Nodes, n) {
			continue
		}
		errs.Addf(lang.ErrSchema, n.Body.Span().Start,
			"%s: @for-each inside a @for-each composite is not supported", n.Address)
	}
	return errs
}

// underForEachComposite reports whether any composite call site in
// n's ancestry is itself a `@for-each` template.
func underForEachComposite(nodes map[string]*Node, n *Node) bool {
	for cur := n.Composite; cur != ""; {
		b, ok := nodes[cur]
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
