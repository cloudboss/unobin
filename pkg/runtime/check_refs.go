package runtime

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// CheckReferences reports references that cannot resolve to an input binding,
// node address, or active for-each binding.
func CheckReferences(f *lang.File, mods map[string]*Module) *lang.ErrorList {
	c := &referenceChecker{
		root:    f,
		dag:     BuildDAG(f, mods),
		errs:    lang.NewErrorList(0),
		inputs:  map[string]map[string]bool{"": inputNames(f)},
		modules: map[string]map[string]*Module{"": mods},
		seen:    map[string]bool{},
	}
	c.collectCompositeScopes()
	c.checkDeclarations()
	c.checkNodes()
	c.checkConstraints()
	return c.errs
}

type referenceChecker struct {
	root    *lang.File
	dag     *DAG
	errs    *lang.ErrorList
	inputs  map[string]map[string]bool
	modules map[string]map[string]*Module
	seen    map[string]bool
}

func (c *referenceChecker) collectCompositeScopes() {
	for _, n := range c.dag.Nodes {
		if n.Kind != NodeComposite {
			continue
		}
		c.inputs[n.Address] = inputNames(n.CompositeBody)
		c.modules[n.Address] = n.Modules
	}
}

func (c *referenceChecker) checkDeclarations() {
	for _, n := range c.dag.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction, NodeComposite:
		default:
			continue
		}
		mods, found := c.modules[n.Composite]
		if !found || mods == nil {
			continue
		}
		if _, ok := mods[n.NS]; ok {
			continue
		}
		c.addf(n.Body.Span().Start, `module %q is not imported`, n.NS)
	}
}

func (c *referenceChecker) checkNodes() {
	for _, n := range c.dag.Nodes {
		c.checkBody(n.Body, n.Composite, n.ForEach != nil)
		if n.Kind == NodeComposite {
			c.checkCompositeOutputs(n)
		}
	}
}

func (c *referenceChecker) checkConstraints() {
	c.checkConstraintsBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if n.Kind != NodeComposite || n.CompositeBody == nil {
			continue
		}
		c.checkConstraintsBlock(n.CompositeBody, n.Address)
	}
}

func (c *referenceChecker) checkConstraintsBlock(f *lang.File, scope string) {
	if f == nil || f.Body == nil {
		return
	}
	arr, ok := topLevelMap(f.Body)["constraints"].(*lang.ArrayLit)
	if !ok {
		return
	}
	for _, e := range arr.Elements {
		obj, ok := e.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent {
				continue
			}
			if fld.Key.Name != "when" && fld.Key.Name != "require" {
				continue
			}
			c.checkExpr(fld.Value, scope, false)
		}
	}
}

func (c *referenceChecker) checkCompositeOutputs(n *Node) {
	if n.CompositeBody == nil || n.CompositeBody.Body == nil {
		return
	}
	outputs, ok := topLevelMap(n.CompositeBody.Body)["outputs"].(*lang.ObjectLit)
	if !ok {
		return
	}
	for _, fld := range outputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		c.checkExpr(fld.Value, n.Address, false)
	}
}

func (c *referenceChecker) checkBody(body lang.Expr, scope string, eachOK bool) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		c.checkExpr(body, scope, eachOK)
		return
	}
	for _, fld := range obj.Fields {
		fieldEachOK := eachOK
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "@for-each" {
			fieldEachOK = false
		}
		c.checkExpr(fld.Value, scope, fieldEachOK)
	}
}

func (c *referenceChecker) checkExpr(expr lang.Expr, scope string, eachOK bool) {
	lang.Walk(expr, func(node lang.Expr) {
		dp, ok := node.(*lang.DotPath)
		if !ok {
			return
		}
		switch dp.Root.Name {
		case "var":
			c.checkVar(dp, scope)
		case "resource", "data", "action":
			c.checkNode(dp, scope)
		case "@each":
			c.checkEach(dp, eachOK)
		}
	})
}

func (c *referenceChecker) checkVar(dp *lang.DotPath, scope string) {
	if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
		return
	}
	name := dp.Segments[0].Name
	if c.inputs[scope][name] {
		return
	}
	c.addf(dp.S.Start, `unknown input %q`, name)
}

func (c *referenceChecker) checkNode(dp *lang.DotPath, scope string) {
	ref := refAddress(dp)
	if ref == "" {
		return
	}
	if _, ok := c.dag.Nodes[scopeRef(ref, scope)]; ok {
		return
	}
	kind, _, _ := strings.Cut(ref, ".")
	c.addf(dp.S.Start, `unknown %s %q`, kind, ref)
}

func (c *referenceChecker) checkEach(dp *lang.DotPath, eachOK bool) {
	if !eachOK {
		c.addf(dp.S.Start, "@each is only available inside @for-each")
		return
	}
	if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
		c.addf(dp.S.Start, "@each requires .key or .value")
		return
	}
	switch dp.Segments[0].Name {
	case "key", "value":
	default:
		c.addf(dp.S.Start, "@each.%s is not available", dp.Segments[0].Name)
	}
}

func (c *referenceChecker) addf(pos lang.Position, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	key := fmt.Sprintf("%s:%d:%d:%s", pos.File, pos.Line, pos.Column, msg)
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.errs.Add(&lang.Error{Kind: lang.ErrResolve, Pos: pos, Msg: msg})
}

func inputNames(f *lang.File) map[string]bool {
	names := map[string]bool{}
	if f == nil || f.Body == nil {
		return names
	}
	inputs, ok := topLevelMap(f.Body)["inputs"].(*lang.ObjectLit)
	if !ok {
		return names
	}
	for _, fld := range inputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		names[fld.Key.Name] = true
	}
	return names
}
