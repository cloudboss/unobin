package runtime

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang"
)

// LiteralConstraints reports cross-field constraint violations that
// are decidable at compile time. It evaluates each library node's
// fields with no inputs or upstream outputs in scope and checks every
// constraint whose referenced fields all reduce that way (an absent
// field reads as null); a constraint that reads a deferred field is
// left for plan, which checks it once the value is known. Only Go
// libraries declare constraints in their schema, so UB composite nodes
// never match here, and their bodies check at plan.
func (c *Checker) LiteralConstraints() *lang.ErrorList {
	errs := lang.NewErrorList(0)
	for _, n := range c.dag.Nodes {
		if n.IsComposite() {
			continue
		}
		switch n.Kind {
		case NodeResource, NodeData, NodeAction:
		default:
			continue
		}
		lib := c.libraries[n.Composite][n.Alias]
		if lib == nil || lib.Schema == nil {
			continue
		}
		schema := lib.Schema.typeSchema(n.Kind, n.Type)
		if schema == nil || len(schema.Constraints) == 0 {
			continue
		}
		values, deferred, ok := literalValues(n.Body)
		if !ok {
			continue
		}
		pos := n.Body.Span().Start
		entries, perr := lang.ParseSpecs(schema.Constraints)
		for _, e := range perr.Errors() {
			errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, e.Msg)
		}
		eval := func(ex lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &EvalContext{Vars: values, MissingAsNull: true}
			applyBindings(ctx, binds)
			v, err := Eval(ex, ctx)
			if errors.Is(err, ErrEvalNotFound) {
				return nil, nil
			}
			return v, err
		}
		for i, c := range entries {
			if readsDeferred(c, deferred) {
				continue
			}
			checked := lang.CheckConstraintEntry(i, c, values, eval, lang.DisplayNodeRelative)
			for _, e := range checked.Errors() {
				errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, e.Msg)
			}
		}
	}
	return errs
}

// literalValues evaluates every non-meta field of a node body with an
// empty context. values holds each field that reduces without reading
// an input or another node's output; deferred names the fields that do
// not, whose values are only known at plan. ok is false when the body
// is not an object.
func literalValues(body lang.Expr) (map[string]any, map[string]bool, bool) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, nil, false
	}
	values := make(map[string]any, len(obj.Fields))
	deferred := map[string]bool{}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, &EvalContext{})
		if err != nil {
			deferred[fld.Key.Name] = true
			continue
		}
		values[fld.Key.Name] = val
	}
	return values, deferred, true
}

// readsDeferred reports whether the constraint references a field whose
// value is not known until plan.
func readsDeferred(c lang.ConstraintEntry, deferred map[string]bool) bool {
	for _, r := range lang.ConstraintFieldRoots(c) {
		if deferred[r] {
			return true
		}
	}
	return false
}

// typeSchema returns the schema for a node kind's type, or nil when the
// kind is not a resource, data, or action or the type is absent.
func (s *LibrarySchema) typeSchema(kind NodeKind, typ string) *TypeSchema {
	switch kind {
	case NodeResource:
		return s.Resources[typ]
	case NodeData:
		return s.DataSources[typ]
	case NodeAction:
		return s.Actions[typ]
	default:
		return nil
	}
}

// applyBindings copies a constraint's iteration bindings onto the
// context so @each and any chained level name resolve during the
// element's evaluation.
func applyBindings(ctx *EvalContext, binds []lang.EachBinding) {
	if len(binds) == 0 {
		return
	}
	ctx.Each = make(map[string]lang.EachValue, len(binds))
	for _, b := range binds {
		ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
	}
}
