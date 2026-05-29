package runtime

import (
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// CheckReferences reports references that cannot resolve to an input binding,
// node address, or active for-each binding.
func CheckReferences(f *lang.File, libs map[string]*Library) *lang.ErrorList {
	c := &referenceChecker{
		root:      f,
		dag:       BuildDAG(f, libs),
		errs:      lang.NewErrorList(0),
		inputs:    map[string]map[string]bool{"": inputNames(f)},
		locals:    map[string]map[string]bool{"": localNames(f)},
		libraries: map[string]map[string]*Library{"": libs},
		seen:      map[string]bool{},
	}
	c.collectCompositeScopes()
	c.checkDeclarations()
	c.checkNodes()
	c.checkLocals()
	c.checkLocalCycles()
	c.checkConstraints()
	c.checkTypes()
	return c.errs
}

type referenceChecker struct {
	root      *lang.File
	dag       *DAG
	errs      *lang.ErrorList
	inputs    map[string]map[string]bool
	locals    map[string]map[string]bool
	libraries map[string]map[string]*Library
	seen      map[string]bool
}

func (c *referenceChecker) collectCompositeScopes() {
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.inputs[n.Address] = inputNames(n.CompositeBody)
		c.locals[n.Address] = localNames(n.CompositeBody)
		c.libraries[n.Address] = n.Libraries
	}
}

func (c *referenceChecker) checkDeclarations() {
	for _, n := range c.dag.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction:
		default:
			continue
		}
		libs, found := c.libraries[n.Composite]
		if !found || libs == nil {
			continue
		}
		if _, ok := libs[n.Alias]; ok {
			continue
		}
		c.addf(n.Body.Span().Start, `library %q is not imported`, n.Alias)
	}
}

func (c *referenceChecker) checkNodes() {
	for _, n := range c.dag.Nodes {
		c.checkBody(n.Body, n.Composite, n.ForEach != nil)
		if n.IsComposite() {
			c.checkCompositeOutputs(n)
		}
	}
}

func (c *referenceChecker) checkConstraints() {
	c.checkConstraintsBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
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
		inner := lang.OutputValueExpr(fld.Value)
		if inner == nil {
			continue
		}
		c.checkExpr(inner, n.Address, false)
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
		case "local":
			c.checkLocal(dp, scope)
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

func (c *referenceChecker) checkLocal(dp *lang.DotPath, scope string) {
	if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
		return
	}
	name := dp.Segments[0].Name
	if c.locals[scope][name] {
		return
	}
	c.addf(dp.S.Start, `unknown local %q`, name)
}

// checkLocals walks every local's value expression so references made
// inside a `locals:` block (to inputs, nodes, or other locals) are
// validated even though locals are not nodes in the graph.
func (c *referenceChecker) checkLocals() {
	c.checkLocalsBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkLocalsBlock(n.CompositeBody, n.Address)
	}
}

func (c *referenceChecker) checkLocalsBlock(f *lang.File, scope string) {
	block := localsBlock(f)
	if block == nil {
		return
	}
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		c.checkExpr(fld.Value, scope, false)
	}
}

// checkLocalCycles reports a `locals:` block whose entries refer to one
// another in a loop. Declaration order does not matter, so the cycle is
// found structurally rather than by evaluation.
func (c *referenceChecker) checkLocalCycles() {
	c.checkLocalCyclesBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkLocalCyclesBlock(n.CompositeBody, n.Address)
	}
}

func (c *referenceChecker) checkLocalCyclesBlock(f *lang.File, scope string) {
	block := localsBlock(f)
	if block == nil {
		return
	}
	graph := map[string][]string{}
	pos := map[string]lang.Position{}
	var order []string
	for _, fld := range block.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		name := fld.Key.Name
		graph[name] = localRefNames(fld.Value)
		pos[name] = fld.Key.S.Start
		order = append(order, name)
	}
	const (
		unvisited = 0
		active    = 1
		done      = 2
	)
	visiting := map[string]int{}
	var visit func(string) bool
	visit = func(name string) bool {
		visiting[name] = active
		for _, ref := range graph[name] {
			if _, isLocal := graph[ref]; !isLocal {
				continue
			}
			if visiting[ref] == active {
				return true
			}
			if visiting[ref] == unvisited && visit(ref) {
				return true
			}
		}
		visiting[name] = done
		return false
	}
	for _, name := range order {
		if visiting[name] == unvisited && visit(name) {
			c.addf(pos[name], `local %q is part of a cycle`, name)
		}
	}
}

// localNames returns the set of names declared in a file's `locals:`
// block.
func localNames(f *lang.File) map[string]bool {
	out := map[string]bool{}
	for name := range localExprs(localsBlock(f)) {
		out[name] = true
	}
	return out
}

// localRefNames returns the local names an expression reads through
// `local.<name>` references, in source order.
func localRefNames(e lang.Expr) []string {
	var out []string
	lang.Walk(e, func(node lang.Expr) {
		dp, ok := node.(*lang.DotPath)
		if !ok || dp.Root.Name != "local" {
			return
		}
		if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
			return
		}
		out = append(out, dp.Segments[0].Name)
	})
	return out
}

func (c *referenceChecker) checkNode(dp *lang.DotPath, scope string) {
	ref := refAddress(dp)
	if ref == "" {
		return
	}
	node, ok := c.dag.Nodes[scopeRef(ref, scope)]
	if !ok {
		kind, _, _ := strings.Cut(ref, ".")
		c.addf(dp.S.Start, `unknown %s %q`, kind, ref)
		return
	}
	c.checkField(dp, node, scope)
}

// checkField reports a trailing field reference whose name is not
// declared in the node's output schema. Returns silently when the
// path has no trailing field, when no schema is available, or when
// the field is present.
func (c *referenceChecker) checkField(dp *lang.DotPath, node *Node, scope string) {
	field := trailingField(dp)
	if field == "" {
		return
	}
	outputs := c.outputsFor(node, scope)
	if outputs == nil {
		return
	}
	if _, ok := outputs[field]; ok {
		return
	}
	c.addf(dp.S.Start, `unknown field %q on %s.%s`, field, node.Alias, node.Type)
}

func (c *referenceChecker) outputsFor(node *Node, scope string) map[string]typecheck.Type {
	if node.IsComposite() {
		return compositeOutputNames(node)
	}
	libs := c.libraries[scope]
	if libs == nil {
		return nil
	}
	lib := libs[node.Alias]
	if lib == nil || lib.Schema == nil {
		return nil
	}
	var ts *TypeSchema
	switch node.Kind {
	case NodeResource:
		ts = lib.Schema.Resources[node.Type]
	case NodeData:
		ts = lib.Schema.DataSources[node.Type]
	case NodeAction:
		ts = lib.Schema.Actions[node.Type]
	}
	if ts == nil {
		return nil
	}
	return ts.Outputs
}

// compositeOutputNames extracts the set of output names declared in
// a composite type's `outputs:` block. Each field carries Unknown
// since the V1 checker validates field-name existence only; the
// returned map shape matches the Go-side schema so callers do not
// branch.
func compositeOutputNames(node *Node) map[string]typecheck.Type {
	if node.CompositeBody == nil {
		return nil
	}
	outputs, ok := topLevelMap(node.CompositeBody.Body)["outputs"].(*lang.ObjectLit)
	if !ok {
		return nil
	}
	out := map[string]typecheck.Type{}
	for _, fld := range outputs.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		out[fld.Key.Name] = typecheck.TUnknown()
	}
	return out
}

// trailingField extracts the field segment from a resource, data,
// or action reference. For `resource.<alias>.<type>.<name>.<field>` it
// returns `<field>`. For
// `resource.<alias>.<type>.<name>['key'].<field>` it skips the index
// segment and returns `<field>`. Returns "" when the path has no
// trailing field segment.
func trailingField(dp *lang.DotPath) string {
	if len(dp.Segments) < 4 {
		return ""
	}
	seg := dp.Segments[3]
	if seg.Index != nil {
		if len(dp.Segments) < 5 {
			return ""
		}
		seg = dp.Segments[4]
	}
	return seg.Name
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
