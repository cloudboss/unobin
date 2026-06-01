package runtime

import (
	"fmt"
	"maps"
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
		switch n := node.(type) {
		case *lang.DotPath:
			c.checkSplat(n)
			switch n.Root.Name {
			case "var":
				c.checkVar(n, scope)
			case "resource", "data", "action":
				c.checkNode(n, scope)
			case "local":
				c.checkLocal(n, scope)
			case "@each":
				c.checkEach(n, eachOK)
			}
		case *lang.Call:
			c.checkCall(n, scope)
		}
	})
}

// checkSplat reports a splat that ends a path. A trailing `[*]` projects
// the segments to its right, of which there are none, so it reduces to
// the list itself; the author meant to read a field from each element.
func (c *referenceChecker) checkSplat(dp *lang.DotPath) {
	n := len(dp.Segments)
	if n == 0 {
		return
	}
	if dp.Segments[n-1].Splat {
		c.addf(dp.Segments[n-1].S.Start, "splat [*] must be followed by a field, like list[*].id")
	}
}

// checkCall reports a library-qualified function call whose function is
// not declared by the imported library. Bare calls and unimported
// aliases are rejected earlier by lang.ValidateCalls; this adds the
// existence check against the library's Go function set. A library with
// no schema (a UB library, or one whose source the dev CLI could not
// read) is left alone, since its function set is not known here.
func (c *referenceChecker) checkCall(call *lang.Call, scope string) {
	if call.Library == nil || call.Func == nil {
		return
	}
	libs := c.libraries[scope]
	if libs == nil {
		return
	}
	lib := libs[call.Library.Name]
	if lib == nil || lib.Schema == nil {
		return
	}
	arity, ok := lib.Schema.Functions[call.Func.Name]
	if !ok {
		c.addf(call.Func.S.Start, `library %q has no function %q`,
			call.Library.Name, call.Func.Name)
		return
	}
	n := len(call.Args)
	if (arity.Variadic && n < arity.ArgCount) || (!arity.Variadic && n != arity.ArgCount) {
		c.addf(call.Func.S.Start, "%s",
			arityMessage(call.Library.Name, call.Func.Name, arity, n))
	}
}

// arityMessage describes the argument count a function expects against the
// count it was given, for a call the reference checker rejected.
func arityMessage(library, function string, arity FunctionArity, got int) string {
	want := argCount(arity.ArgCount)
	if arity.Variadic {
		want = "at least " + want
	}
	return fmt.Sprintf("%s.%s takes %s, got %d", library, function, want, got)
}

// argCount renders an argument count with the right singular or plural noun.
func argCount(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
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
	attrs := c.attrsFor(node, scope)
	if attrs == nil {
		return
	}
	if _, ok := attrs[field]; ok {
		return
	}
	c.addf(dp.S.Start, `unknown field %q on %s.%s`, field, node.Alias, node.Type)
}

// attrsFor returns the field names a node exposes to references. A
// Go-backed leaf exposes its inputs as well as its outputs, so a plain
// input is readable without being echoed into the output struct. A
// composite stays opaque except its declared outputs.
func (c *referenceChecker) attrsFor(node *Node, scope string) map[string]typecheck.Type {
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
	if ts.Inputs == nil {
		return ts.Outputs
	}
	attrs := make(map[string]typecheck.Type, len(ts.Inputs)+len(ts.Outputs))
	maps.Copy(attrs, ts.Inputs)
	maps.Copy(attrs, ts.Outputs)
	return attrs
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
