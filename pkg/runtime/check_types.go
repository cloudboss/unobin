package runtime

import (
	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// checkTypes runs the type-compatibility pass over every body in
// the DAG once the reference checker has resolved identifiers. It
// walks each node's body, looks up the field's declared type from
// the resolved module's schema, and asks typecheck.Check to
// validate the expression against it. Composite output and
// constraint predicate bodies are walked too. Anything the schema
// cannot describe (composite outputs, unknown Go field types) comes
// back as typecheck.TUnknown and the inferrer skips silently.
func (c *referenceChecker) checkTypes() {
	for _, n := range c.dag.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction, NodeComposite:
		default:
			continue
		}
		targets := c.bodyTargets(n)
		scope := c.scopeFor(n)
		c.checkBodyTypes(n.Body, targets, scope, n)
	}
	c.checkOutputBodyTypes()
	c.checkConstraintTypes()
}

// bodyTargets returns the per-field type targets for a node body.
// Returns nil when the node's schema cannot be located; the caller
// then runs the body's expressions with no target (free inference)
// so missing-schema modules do not block compile.
func (c *referenceChecker) bodyTargets(n *Node) map[string]typecheck.Type {
	switch n.Kind {
	case NodeComposite:
		return compositeInputTargets(n)
	default:
		return c.goInputTargets(n)
	}
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
	mods := c.modules[n.Composite]
	if mods == nil {
		return nil
	}
	mod := mods[n.NS]
	if mod == nil || mod.Schema == nil {
		return nil
	}
	switch n.Kind {
	case NodeResource:
		return mod.Schema.Resources[n.Type]
	case NodeData:
		return mod.Schema.DataSources[n.Type]
	case NodeAction:
		return mod.Schema.Actions[n.Type]
	}
	return nil
}

func (c *referenceChecker) scopeFor(n *Node) *typecheck.Scope {
	inputs := c.scopeInputs(n.Composite)
	return &typecheck.Scope{
		Inputs:     inputs,
		LookupNode: c.lookupNodeFor(n.Composite),
	}
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
	return func(kind, ns, typ, name string) (typecheck.Type, bool) {
		ref := kind + "." + ns + "." + typ + "." + name
		node, ok := c.dag.Nodes[scopeRef(ref, scope)]
		if !ok {
			return typecheck.Type{}, false
		}
		return c.nodeOutputType(node), true
	}
}

// nodeOutputType builds an Object Type that describes a node's
// outputs. Go-backed nodes return their TypeSchema.Outputs as an
// Object directly; goschema has already expanded nested struct
// types so the descender can walk through them. Composite nodes
// contribute an Object whose fields all carry Unknown types; the
// field-existence check still catches typos but the type checker
// stops descending past a composite output until composite-output
// typing is implemented.
func (c *referenceChecker) nodeOutputType(node *Node) typecheck.Type {
	if node == nil {
		return typecheck.TUnknown()
	}
	if node.Kind == NodeComposite {
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
	mods := c.modules[node.Composite]
	if mods == nil {
		return typecheck.TUnknown()
	}
	mod := mods[node.NS]
	if mod == nil || mod.Schema == nil {
		return typecheck.TUnknown()
	}
	var ts *TypeSchema
	switch node.Kind {
	case NodeResource:
		ts = mod.Schema.Resources[node.Type]
	case NodeData:
		ts = mod.Schema.DataSources[node.Type]
	case NodeAction:
		ts = mod.Schema.Actions[node.Type]
	}
	if ts == nil || ts.Outputs == nil {
		return typecheck.TUnknown()
	}
	fields := make([]typecheck.ObjectField, 0, len(ts.Outputs))
	for name, t := range ts.Outputs {
		fields = append(fields, typecheck.ObjectField{Name: name, Type: t})
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
			} else if owner != nil && owner.Kind != NodeComposite {
				c.addf(fld.Key.S.Start,
					`unknown field %q on %s.%s`,
					fld.Key.Name, owner.NS, owner.Type)
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
		return eachBindingFromType(t)
	}
	return nil
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
		if n.Kind != NodeComposite || n.CompositeBody == nil {
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
		Inputs:     c.scopeInputs(scope),
		LookupNode: c.lookupNodeFor(scope),
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
		if n.Kind != NodeComposite || n.CompositeBody == nil {
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
		Inputs:     c.scopeInputs(scope),
		LookupNode: c.lookupNodeFor(scope),
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
			typecheck.Check(fld.Value, typecheck.TBoolean(), s, c.errs)
		}
	}
}
