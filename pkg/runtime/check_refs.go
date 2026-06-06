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
		it := c.checkConstraintIteration(constraintForEach(obj), scope)
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent {
				continue
			}
			if fld.Key.Name != "when" && fld.Key.Name != "require" {
				continue
			}
			c.checkConstraintExpr(fld.Value, scope, it)
		}
	}
}

// iterScope is the iteration context a constraint expression is
// checked in: the bare form binds @each, the chained form binds the
// declared level names, and outside @for-each nothing is bound.
type iterScope struct {
	bare  bool
	names map[string]bool
}

// checkConstraintIteration checks an entry's @for-each value and
// returns the bindings its when and require are checked under. Each
// chain level's iterable is checked with only the earlier levels in
// scope; malformed levels are skipped, with validation the place that
// reports them.
func (c *referenceChecker) checkConstraintIteration(
	forEach lang.Expr, scope string,
) iterScope {
	switch fe := forEach.(type) {
	case nil:
		return iterScope{}
	case *lang.ArrayLit:
		it := iterScope{names: make(map[string]bool, len(fe.Elements))}
		for _, el := range fe.Elements {
			obj, ok := el.(*lang.ObjectLit)
			if !ok || len(obj.Fields) != 1 || obj.Fields[0].Key.Kind != lang.FieldIdent {
				continue
			}
			f := obj.Fields[0]
			if !strings.HasPrefix(f.Key.Name, "@") || f.Key.Name == "@each" {
				continue
			}
			c.checkConstraintExpr(f.Value, scope, it)
			it.names[f.Key.Name] = true
		}
		return it
	default:
		c.checkConstraintExpr(forEach, scope, iterScope{})
		return iterScope{bare: true}
	}
}

// constraintForEach returns a constraint entry's @for-each expression,
// or nil when the entry does not iterate.
func constraintForEach(obj *lang.ObjectLit) lang.Expr {
	for _, fld := range obj.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == "@for-each" {
			return fld.Value
		}
	}
	return nil
}

// checkConstraintExpr walks a constraint's when, require, or @for-each
// expression. A constraint checks input values, so var is the only
// address root in scope; a resource, data, action, or local reference
// has no value where constraints evaluate, so it is rejected at
// compile instead of reading as null and silently passing the
// predicate. eachOK admits @each inside an entry that iterates with
// @for-each. Comprehension bindings and library calls resolve as
// anywhere else.
func (c *referenceChecker) checkConstraintExpr(expr lang.Expr, scope string, it iterScope) {
	c.checkExprIdents(expr)
	lang.Walk(expr, func(node lang.Expr) {
		switch n := node.(type) {
		case *lang.DotPath:
			c.checkSplat(n)
			switch {
			case n.Root.Name == "var":
				c.checkVar(n, scope)
			case n.Root.Name == "resource", n.Root.Name == "data",
				n.Root.Name == "action", n.Root.Name == "local":
				c.addf(n.S.Start,
					"a constraint may read inputs only, not %s", namedPathText(n))
			case strings.HasPrefix(n.Root.Name, "@"):
				c.checkBindingPath(n, it)
			}
		case *lang.Call:
			c.checkCall(n, scope)
		}
	})
}

// namedPathText renders a dot path's root and named segments for a
// diagnostic, indexes left out.
func namedPathText(dp *lang.DotPath) string {
	parts := []string{dp.Root.Name}
	for _, seg := range dp.Segments {
		if seg.Name != "" {
			parts = append(parts, seg.Name)
		}
	}
	return strings.Join(parts, ".")
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
	c.checkExprIdents(expr)
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
				c.checkBindingPath(n, iterScope{bare: eachOK})
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
// existence and argument-count checks against the library's declared
// function set. A call against a UB library is always an error: only
// Go libraries export functions. A Go library with no schema is left
// alone, since schemas exist only at compile and the runtime's own
// re-check of the embedded source sees none. An unreadable Go library
// never reaches here; its schema read fails the compile first.
func (c *referenceChecker) checkCall(call *lang.Call, scope string) {
	if call.Library == nil || call.Func == nil {
		return
	}
	if call.Library.Name == lang.CoreNamespace {
		sig, ok := CoreFunctionSigs()[call.Func.Name]
		if !ok {
			c.addf(call.Func.S.Start, `%s has no function %q`,
				lang.CoreNamespace, call.Func.Name)
			return
		}
		c.checkCallArity(call, sig)
		return
	}
	libs := c.libraries[scope]
	if libs == nil {
		return
	}
	lib := libs[call.Library.Name]
	if lib == nil {
		return
	}
	if lib.Schema == nil {
		if hasComposites(lib) && len(lib.Functions) == 0 {
			c.addf(call.Func.S.Start,
				"library %q is implemented in unobin and exports no functions",
				call.Library.Name)
		}
		return
	}
	sig, ok := lib.Schema.Functions[call.Func.Name]
	if !ok {
		c.addf(call.Func.S.Start, `library %q has no function %q`,
			call.Library.Name, call.Func.Name)
		return
	}
	c.checkCallArity(call, sig)
}

// checkCallArity reports a call whose argument count does not fit the
// function's signature.
func (c *referenceChecker) checkCallArity(call *lang.Call, sig typecheck.FuncSig) {
	n := len(call.Args)
	fixed := len(sig.Params)
	variadic := sig.Variadic != nil
	if (variadic && n < fixed) || (!variadic && n != fixed) {
		c.addf(call.Func.S.Start, "%s",
			arityMessage(call.Library.Name, call.Func.Name, fixed, variadic, n))
	}
}

// hasComposites reports whether the library exports any UB-implemented
// types, the mark of a UB library when no schema is present.
func hasComposites(l *Library) bool {
	return len(l.ResourceComposites)+len(l.DataComposites)+len(l.ActionComposites) > 0
}

// arityMessage describes the argument count a function expects against the
// count it was given, for a call the reference checker rejected.
func arityMessage(library, function string, fixed int, variadic bool, got int) string {
	want := argCount(fixed)
	if variadic {
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
	known := func(s string) bool { return c.inputs[scope][s] }
	if prefix, rest, ok := hyphenSubtraction(name, known); ok {
		c.addf(dp.S.Start, `unknown input %q; write var.%s - %s to subtract`,
			name, prefix, subtrahendText("var.", rest))
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
	known := func(s string) bool { return c.locals[scope][s] }
	if prefix, rest, ok := hyphenSubtraction(name, known); ok {
		c.addf(dp.S.Start, `unknown local %q; write local.%s - %s to subtract`,
			name, prefix, subtrahendText("local.", rest))
		return
	}
	c.addf(dp.S.Start, `unknown local %q`, name)
}

// subtrahendText renders the right side of a suggested subtraction: a
// number stays bare, a known name takes the same reference root as
// the left side.
func subtrahendText(root, rest string) string {
	if allDigits(rest) {
		return rest
	}
	return root + rest
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
	field, idx := trailingField(dp)
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
	known := func(s string) bool {
		_, ok := attrs[s]
		return ok
	}
	if prefix, rest, ok := hyphenSubtraction(field, known); ok && allDigits(rest) {
		head := dotPathString(&lang.DotPath{Root: dp.Root, Segments: dp.Segments[:idx]})
		c.addf(dp.S.Start, `unknown field %q on %s.%s; write %s.%s - %s to subtract`,
			field, node.Alias, node.Type, head, prefix, rest)
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
// or action reference, returning its name and segment index. For
// resource.<alias>.<type>.<name>.<field> it returns <field> at 3; an
// index segment after the name, as in
// resource.<alias>.<type>.<name>['key'].<field>, is skipped and the
// field sits at 4. Returns "" when the path has no trailing field
// segment.
func trailingField(dp *lang.DotPath) (string, int) {
	if len(dp.Segments) < 4 {
		return "", -1
	}
	idx := 3
	if dp.Segments[idx].Index != nil {
		if len(dp.Segments) < 5 {
			return "", -1
		}
		idx = 4
	}
	return dp.Segments[idx].Name, idx
}

func (c *referenceChecker) checkBindingPath(dp *lang.DotPath, it iterScope) {
	name := dp.Root.Name
	switch {
	case name == lang.CoreNamespace:
		c.addf(dp.S.Start, "%s names functions; call one, e.g. %s.length(...)",
			lang.CoreNamespace, lang.CoreNamespace)
		return
	case name == "@each" && it.bare:
	case name == "@each" && it.names != nil:
		c.addf(dp.S.Start,
			"@each is not bound in a chained @for-each; reference a declared level")
		return
	case name == "@each":
		c.addf(dp.S.Start, "@each is only available inside @for-each")
		return
	case it.names[name]:
	default:
		c.addf(dp.S.Start, "%s is not bound; declare it as a chain level", name)
		return
	}
	if len(dp.Segments) == 0 || dp.Segments[0].Name == "" {
		c.addf(dp.S.Start, "%s requires .key or .value", name)
		return
	}
	switch seg := dp.Segments[0].Name; seg {
	case "key", "value":
	default:
		known := func(s string) bool { return s == "key" || s == "value" }
		if prefix, rest, ok := hyphenSubtraction(seg, known); ok && allDigits(rest) {
			c.addf(dp.S.Start, "%s.%s is not available; write %s.%s - %s to subtract",
				name, seg, name, prefix, rest)
			return
		}
		c.addf(dp.S.Start, "%s.%s is not available", name, seg)
	}
}

// checkExprIdents reports bare identifiers in an expression that no
// enclosing comprehension binds. In an expression a bare word is
// either a binding or a mistake: unquoted string data, or arithmetic
// swallowed by greedy kebab-case lexing (n-1 is one identifier).
// Slots whose vocabulary is bare words by design (type expressions,
// constraint kinds, format names) are schema positions, not
// expressions, and are never walked here.
func (c *referenceChecker) checkExprIdents(expr lang.Expr) {
	walkFreeIdents(expr, nil, func(id *lang.Ident, bound map[string]bool) {
		c.checkIdent(id, bound)
	})
}

func (c *referenceChecker) checkIdent(id *lang.Ident, bound map[string]bool) {
	name := id.Name
	if name == lang.CoreNamespace {
		c.addf(id.S.Start, "%s names functions; call one, e.g. %s.length(...)",
			lang.CoreNamespace, lang.CoreNamespace)
		return
	}
	if strings.HasPrefix(name, "@") {
		c.addf(id.S.Start, "%s cannot stand alone; read %s.key or %s.value", name, name, name)
		return
	}
	known := func(s string) bool { return bound[s] }
	if prefix, rest, ok := hyphenSubtraction(name, known); ok {
		c.addf(id.S.Start, "unknown name %q; write %s - %s to subtract", name, prefix, rest)
		return
	}
	c.addf(id.S.Start, "unknown name %q; write '%s' for a string", name, name)
}

// walkFreeIdents visits every bare identifier in e that no enclosing
// comprehension binds, passing the binding names in scope at that
// identifier. A comprehension's source reads the outer scope; its
// key, value, and filter see the comprehension's own names too. Dot
// path roots and call names are not bare identifiers and stay with
// their own checks.
func walkFreeIdents(e lang.Expr, bound map[string]bool, visit func(*lang.Ident, map[string]bool)) {
	if e == nil {
		return
	}
	switch v := e.(type) {
	case *lang.Ident:
		if !bound[v.Name] {
			visit(v, bound)
		}
	case *lang.ObjectLit:
		for _, fld := range v.Fields {
			walkFreeIdents(fld.Value, bound, visit)
		}
	case *lang.ArrayLit:
		for _, el := range v.Elements {
			walkFreeIdents(el, bound, visit)
		}
	case *lang.Call:
		for _, a := range v.Args {
			walkFreeIdents(a, bound, visit)
		}
	case *lang.Infix:
		walkFreeIdents(v.Left, bound, visit)
		walkFreeIdents(v.Right, bound, visit)
	case *lang.Prefix:
		walkFreeIdents(v.Expr, bound, visit)
	case *lang.DotPath:
		for _, seg := range v.Segments {
			walkFreeIdents(seg.Index, bound, visit)
		}
	case *lang.Conditional:
		walkFreeIdents(v.Cond, bound, visit)
		walkFreeIdents(v.Then, bound, visit)
		walkFreeIdents(v.Else, bound, visit)
	case *lang.Comprehension:
		walkFreeIdents(v.Source, bound, visit)
		inner := make(map[string]bool, len(bound)+len(v.Names))
		maps.Copy(inner, bound)
		for _, n := range v.Names {
			inner[n] = true
		}
		walkFreeIdents(v.Key, inner, visit)
		walkFreeIdents(v.Value, inner, visit)
		walkFreeIdents(v.Filter, inner, visit)
	case *lang.InterpolatedString:
		for _, part := range v.Parts {
			walkFreeIdents(part.Expr, bound, visit)
		}
	}
}

// hyphenSubtraction splits an unknown kebab-case name at the hyphen
// that reads as subtraction: the prefix must be a known name and the
// rest a whole number or another known name. Splits are tried from
// the rightmost hyphen so the longest known prefix wins.
func hyphenSubtraction(name string, known func(string) bool) (string, string, bool) {
	for i := len(name) - 1; i > 0; i-- {
		if name[i] != '-' {
			continue
		}
		prefix, rest := name[:i], name[i+1:]
		if !known(prefix) {
			continue
		}
		if allDigits(rest) || known(rest) {
			return prefix, rest, true
		}
	}
	return "", "", false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
