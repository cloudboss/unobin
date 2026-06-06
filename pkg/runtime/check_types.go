package runtime

import (
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// checkTypes runs the type-compatibility pass over every body in
// the DAG once the reference checker has resolved identifiers. It
// walks each node's body, looks up the field's declared type from
// the resolved library's schema, and asks typecheck.Check to
// validate the expression against it. Composite output and
// constraint predicate bodies are walked too. Anything the schema
// cannot describe (composite outputs, unknown Go field types) comes
// back as typecheck.TUnknown and the inferrer skips silently.
func (c *referenceChecker) checkTypes() {
	for _, n := range c.dag.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction:
		default:
			continue
		}
		targets := c.bodyTargets(n)
		scope := c.scopeFor(n)
		c.checkBodyTypes(n.Body, targets, scope, n)
		c.checkRequiredPresence(n, targets)
	}
	c.checkOutputBodyTypes()
	c.checkConstraintTypes()
}

// checkRequiredPresence reports body fields a node's schema requires
// but the body leaves out. A field is required when its declared type
// is not optional and no default is declared for it; an Unknown type
// stays unchecked, since it may stand for a type the schema cannot
// describe. Nodes whose schema cannot be located check nothing, so a
// missing-schema library does not block compile.
func (c *referenceChecker) checkRequiredPresence(n *Node, targets map[string]typecheck.Type) {
	if len(targets) == 0 {
		return
	}
	obj, ok := n.Body.(*lang.ObjectLit)
	if !ok {
		return
	}
	present := make(map[string]bool, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		present[fld.Key.Name] = true
	}
	defaulted := c.defaultedInputs(n)
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := targets[name]
		if t.Kind == typecheck.Optional || t.Kind == typecheck.Unknown {
			continue
		}
		if present[name] || defaulted[name] {
			continue
		}
		c.addf(n.Body.Span().Start,
			"missing required input %q on %s.%s", name, n.Alias, n.Type)
	}
}

// defaultedInputs returns the top-level input names the node's Go type
// declares a default for, Optional markers included. A nested default
// does not excuse its top-level parent. A composite declares
// optionality in its inputs block instead, so its set is empty.
func (c *referenceChecker) defaultedInputs(n *Node) map[string]bool {
	if n.IsComposite() {
		return nil
	}
	ts := c.lookupTypeSchema(n)
	if ts == nil {
		return nil
	}
	out := make(map[string]bool, len(ts.Defaults))
	for _, d := range ts.Defaults {
		rest, ok := strings.CutPrefix(d.Field, "var.")
		if !ok {
			continue
		}
		if !strings.Contains(rest, ".") {
			out[rest] = true
		}
	}
	return out
}

// bodyTargets returns the per-field type targets for a node body.
// Returns nil when the node's schema cannot be located; the caller
// then runs the body's expressions with no target (free inference)
// so missing-schema libraries do not block compile.
func (c *referenceChecker) bodyTargets(n *Node) map[string]typecheck.Type {
	if n.IsComposite() {
		return compositeInputTargets(n)
	}
	return c.goInputTargets(n)
}

func compositeInputTargets(n *Node) map[string]typecheck.Type {
	if n.CompositeBody == nil {
		return nil
	}
	inputs, ok := topLevelMap(n.CompositeBody.Body)["inputs"].(*lang.ObjectLit)
	if !ok {
		return nil
	}
	fields := typecheck.InputsFromBlock(inputs)
	out := make(map[string]typecheck.Type, len(fields))
	for _, f := range fields {
		t := f.Type
		if f.Optional {
			t = typecheck.TOptional(t)
		}
		out[f.Name] = t
	}
	return out
}

func (c *referenceChecker) goInputTargets(n *Node) map[string]typecheck.Type {
	ts := c.lookupTypeSchema(n)
	if ts == nil || ts.Inputs == nil {
		return nil
	}
	return ts.Inputs
}

func (c *referenceChecker) lookupTypeSchema(n *Node) *TypeSchema {
	libs := c.libraries[n.Composite]
	if libs == nil {
		return nil
	}
	lib := libs[n.Alias]
	if lib == nil || lib.Schema == nil {
		return nil
	}
	switch n.Kind {
	case NodeResource:
		return lib.Schema.Resources[n.Type]
	case NodeData:
		return lib.Schema.DataSources[n.Type]
	case NodeAction:
		return lib.Schema.Actions[n.Type]
	}
	return nil
}

func (c *referenceChecker) scopeFor(n *Node) *typecheck.Scope {
	inputs := c.scopeInputs(n.Composite)
	scope := &typecheck.Scope{
		Inputs:         inputs,
		LookupNode:     c.lookupNodeFor(n.Composite),
		LookupFunction: c.lookupFunctionFor(n.Composite),
	}
	scope.LookupLocal = c.lookupLocalFor(n.Composite, scope)
	return scope
}

// lookupFunctionFor resolves a qualified function call in the given
// scope to its declared signature, so the inferrer can check argument
// types and use the result type. @core resolves against the language's
// own table; a missing library or schema resolves nothing, leaving the
// call to infer Unknown.
func (c *referenceChecker) lookupFunctionFor(
	scope string,
) func(library, name string) (typecheck.FuncSig, bool) {
	return func(library, name string) (typecheck.FuncSig, bool) {
		if library == lang.CoreNamespace {
			sig, ok := CoreFunctionSigs()[name]
			return sig, ok
		}
		libs := c.libraries[scope]
		if libs == nil {
			return typecheck.FuncSig{}, false
		}
		lib := libs[library]
		if lib == nil || lib.Schema == nil {
			return typecheck.FuncSig{}, false
		}
		sig, ok := lib.Schema.Functions[name]
		return sig, ok
	}
}

// lookupLocalFor returns a resolver that infers the type of a local in
// the given scope. The local's expression is inferred against the same
// scope, so a local may read inputs, nodes, and other locals. Results
// are memoized; a local caught mid-inference (a cycle, already reported
// by the reference checker) yields Unknown rather than looping.
func (c *referenceChecker) lookupLocalFor(
	scope string,
	sc *typecheck.Scope,
) typecheck.LookupLocalFn {
	exprs := c.localExprsFor(scope)
	memo := map[string]typecheck.Type{}
	forcing := map[string]bool{}
	return func(name string) (typecheck.Type, bool) {
		expr, ok := exprs[name]
		if !ok {
			return typecheck.Type{}, false
		}
		if t, done := memo[name]; done {
			return t, true
		}
		if forcing[name] {
			return typecheck.TUnknown(), true
		}
		forcing[name] = true
		t := typecheck.Infer(expr, typecheck.TUnknown(), sc, lang.NewErrorList(0))
		delete(forcing, name)
		memo[name] = t
		return t, true
	}
}

func (c *referenceChecker) localExprsFor(scope string) map[string]lang.Expr {
	if scope == "" {
		return localExprs(localsBlock(c.root))
	}
	node, ok := c.dag.Nodes[scope]
	if !ok {
		return nil
	}
	return localExprs(localsBlock(node.CompositeBody))
}

func (c *referenceChecker) scopeInputs(scope string) []typecheck.ObjectField {
	var inputsBlock *lang.ObjectLit
	if scope == "" {
		if c.root != nil && c.root.Body != nil {
			inputsBlock, _ = topLevelMap(c.root.Body)["inputs"].(*lang.ObjectLit)
		}
	} else {
		node, ok := c.dag.Nodes[scope]
		if !ok || node.CompositeBody == nil || node.CompositeBody.Body == nil {
			return nil
		}
		inputsBlock, _ = topLevelMap(node.CompositeBody.Body)["inputs"].(*lang.ObjectLit)
	}
	return typecheck.InputsFromBlock(inputsBlock)
}

func (c *referenceChecker) lookupNodeFor(scope string) typecheck.LookupNodeFn {
	return func(kind, alias, typ, name string) (typecheck.Type, bool) {
		ref := kind + "." + alias + "." + typ + "." + name
		node, ok := c.dag.Nodes[scopeRef(ref, scope)]
		if !ok {
			return typecheck.Type{}, false
		}
		return c.nodeAttrType(node), true
	}
}

// nodeAttrType builds an Object Type describing what a node exposes to
// references: for a Go-backed leaf, its inputs laid under its outputs,
// matching the runtime merge so a reference to a plain input type-checks
// without the resource echoing it into its output struct. Outputs win on
// a name collision. goschema has already expanded nested struct types so
// the descender can walk through them. Composite nodes contribute an
// Object whose fields are all Unknown; the field-existence check still
// catches typos but the type checker stops descending past a composite
// output until composite-output typing is implemented.
func (c *referenceChecker) nodeAttrType(node *Node) typecheck.Type {
	if node == nil {
		return typecheck.TUnknown()
	}
	if node.IsComposite() {
		names := compositeOutputNames(node)
		fields := make([]typecheck.ObjectField, 0, len(names))
		for name := range names {
			fields = append(fields, typecheck.ObjectField{
				Name: name,
				Type: typecheck.TUnknown(),
			})
		}
		return typecheck.TObject(fields)
	}
	libs := c.libraries[node.Composite]
	if libs == nil {
		return typecheck.TUnknown()
	}
	lib := libs[node.Alias]
	if lib == nil || lib.Schema == nil {
		return typecheck.TUnknown()
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
		return typecheck.TUnknown()
	}
	fields := make([]typecheck.ObjectField, 0, len(ts.Inputs)+len(ts.Outputs))
	at := make(map[string]int, len(ts.Inputs)+len(ts.Outputs))
	for name, t := range ts.Inputs {
		at[name] = len(fields)
		fields = append(fields, typecheck.ObjectField{Name: name, Type: t})
	}
	for name, t := range ts.Outputs {
		if i, ok := at[name]; ok {
			fields[i].Type = t
			continue
		}
		fields = append(fields, typecheck.ObjectField{Name: name, Type: t})
	}
	if len(fields) == 0 {
		return typecheck.TUnknown()
	}
	return typecheck.TObject(fields)
}

func (c *referenceChecker) checkBodyTypes(
	body lang.Expr,
	targets map[string]typecheck.Type,
	scope *typecheck.Scope,
	owner *Node,
) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return
	}
	each := eachBindingFromBody(obj, scope, c.errs)
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		target := typecheck.TUnknown()
		if targets != nil {
			t, ok := targets[fld.Key.Name]
			if ok {
				target = t
			} else if owner != nil && !owner.IsComposite() {
				c.addf(fld.Key.S.Start,
					`unknown field %q on %s.%s`,
					fld.Key.Name, owner.Alias, owner.Type)
				continue
			}
		}
		fieldScope := scope
		if each != nil {
			s := *scope
			s.Each = each
			fieldScope = &s
		}
		typecheck.Check(fld.Value, target, fieldScope, c.errs)
	}
}

// eachBindingFromBody inspects an object literal for an @for-each
// meta key and returns the type pair bound by the iteration. The
// inferrer walks the @for-each value expression in the parent scope
// (no @each binding yet) so the typing reflects what the body sees
// during iteration.
func eachBindingFromBody(
	obj *lang.ObjectLit, scope *typecheck.Scope, errs *lang.ErrorList,
) *typecheck.EachBinding {
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "@for-each" {
			continue
		}
		t := typecheck.Infer(fld.Value, typecheck.TUnknown(), scope, errs)
		checkFanOutIterable(t, fld.Value.Span().Start, errs)
		return eachBindingFromType(t)
	}
	return nil
}

// checkFanOutIterable reports a node @for-each whose iterable can
// never fan out. The runtime iterates maps only, so each instance
// gets a stable key; a list teaches the comprehension that builds
// the map it needs.
func checkFanOutIterable(t typecheck.Type, pos lang.Position, errs *lang.ErrorList) {
	switch t.Unwrap().Kind {
	case typecheck.Unknown, typecheck.Any, typecheck.Map, typecheck.Object:
	case typecheck.List, typecheck.Tuple:
		errs.Addf(lang.ErrType, pos,
			"@for-each: iterable must be a map, got %s; "+
				"turn a list into a map with { for n in ns : n => n }", t)
	default:
		errs.Addf(lang.ErrType, pos, "@for-each: iterable must be a map, got %s", t)
	}
}

func eachBindingFromType(t typecheck.Type) *typecheck.EachBinding {
	switch t.Kind {
	case typecheck.Map:
		value := typecheck.TUnknown()
		if t.Elem != nil {
			value = *t.Elem
		}
		return &typecheck.EachBinding{Key: typecheck.TString(), Value: value}
	case typecheck.Set:
		elem := typecheck.TUnknown()
		if t.Elem != nil {
			elem = *t.Elem
		}
		return &typecheck.EachBinding{Key: elem, Value: elem}
	case typecheck.Object:
		return &typecheck.EachBinding{
			Key:   typecheck.TString(),
			Value: typecheck.TUnknown(),
		}
	}
	return &typecheck.EachBinding{
		Key:   typecheck.TUnknown(),
		Value: typecheck.TUnknown(),
	}
}

// checkOutputBodyTypes walks the root and each composite's
// `outputs:` block. Output expressions have no declared target
// type, so the inferrer runs with TUnknown; the point is to let
// nested field references go through traverseSegments.
func (c *referenceChecker) checkOutputBodyTypes() {
	c.checkOutputsBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkOutputsBlock(n.CompositeBody, n.Address)
	}
}

func (c *referenceChecker) checkOutputsBlock(f *lang.File, scope string) {
	if f == nil || f.Body == nil {
		return
	}
	obj, ok := topLevelMap(f.Body)["outputs"].(*lang.ObjectLit)
	if !ok {
		return
	}
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(scope),
		LookupNode:     c.lookupNodeFor(scope),
		LookupFunction: c.lookupFunctionFor(scope),
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		typecheck.Infer(fld.Value, typecheck.TUnknown(), s, c.errs)
	}
}

// checkConstraintTypes runs the inferrer over each constraint's
// `when:` and `require:` expressions with TBoolean as the target so
// non-boolean predicates report a clear mismatch.
func (c *referenceChecker) checkConstraintTypes() {
	c.checkConstraintTypesBlock(c.root, "")
	for _, n := range c.dag.Nodes {
		if !n.IsComposite() {
			continue
		}
		c.checkConstraintTypesBlock(n.CompositeBody, n.Address)
	}
}

func (c *referenceChecker) checkConstraintTypesBlock(f *lang.File, scope string) {
	if f == nil || f.Body == nil {
		return
	}
	arr, ok := topLevelMap(f.Body)["constraints"].(*lang.ArrayLit)
	if !ok {
		return
	}
	s := &typecheck.Scope{
		Inputs:         c.scopeInputs(scope),
		LookupNode:     c.lookupNodeFor(scope),
		LookupFunction: c.lookupFunctionFor(scope),
	}
	for _, e := range arr.Elements {
		obj, ok := e.(*lang.ObjectLit)
		if !ok {
			continue
		}
		entryScope := s
		if forEach := constraintForEach(obj); forEach != nil {
			withEach := *s
			t := typecheck.Infer(forEach, typecheck.TUnknown(), s, c.errs)
			if bareConstraintIterable(forEach) {
				checkConstraintIterable(t, forEach.Span().Start, c.errs)
			}
			withEach.Each = eachBindingFor(t)
			entryScope = &withEach
		}
		for _, fld := range obj.Fields {
			if fld.Key.Kind != lang.FieldIdent {
				continue
			}
			if fld.Key.Name != "when" && fld.Key.Name != "require" {
				continue
			}
			typecheck.Check(fld.Value, typecheck.TBoolean(), entryScope, c.errs)
		}
	}
}

// bareConstraintIterable reports whether a constraint @for-each value
// gets the iterable kind check. The chained form (an array of level
// objects) validates its levels elsewhere, and a dot path rooted
// outside var already failed the inputs-only rule, so typing it on
// top would report the same mistake twice.
func bareConstraintIterable(forEach lang.Expr) bool {
	switch fe := forEach.(type) {
	case *lang.ArrayLit:
		return false
	case *lang.DotPath:
		return fe.Root == nil || fe.Root.Name == "var"
	}
	return true
}

// checkConstraintIterable reports a bare constraint @for-each whose
// iterable is not a list or a map, the kinds the predicate runtime
// iterates.
func checkConstraintIterable(t typecheck.Type, pos lang.Position, errs *lang.ErrorList) {
	switch t.Unwrap().Kind {
	case typecheck.Unknown, typecheck.Any, typecheck.List, typecheck.Set,
		typecheck.Map, typecheck.Object, typecheck.Tuple:
		return
	}
	errs.Addf(lang.ErrType, pos, "@for-each: iterable must be a list or a map, got %s", t)
}

// eachBindingFor maps an @for-each iterable's inferred type onto the
// @each binding: a list binds an integer key and the element type, a
// map binds a string key and the value type, anything else binds
// Unknown so the entry still checks without claiming a type.
func eachBindingFor(iterable typecheck.Type) *typecheck.EachBinding {
	switch iterable.Kind {
	case typecheck.List:
		if iterable.Elem != nil {
			return &typecheck.EachBinding{Key: typecheck.TInteger(), Value: *iterable.Elem}
		}
	case typecheck.Map:
		if iterable.Elem != nil {
			return &typecheck.EachBinding{Key: typecheck.TString(), Value: *iterable.Elem}
		}
	case typecheck.Object:
		return &typecheck.EachBinding{Key: typecheck.TString(), Value: typecheck.TUnknown()}
	case typecheck.Tuple:
		return &typecheck.EachBinding{Key: typecheck.TInteger(), Value: typecheck.TUnknown()}
	}
	return &typecheck.EachBinding{Key: typecheck.TUnknown(), Value: typecheck.TUnknown()}
}
