package runtime

import (
	"errors"

	"github.com/cloudboss/unobin/pkg/lang"
)

// CheckLiteralConstraints reports cross-field constraint violations on
// library nodes whose every field is known at compile time. It evaluates
// each such node's fields with no inputs or upstream outputs in scope; a
// node that reads either is left for plan, which checks it once those
// values are known. Only Go libraries carry constraints in their schema,
// so UB composite nodes never match here, and their bodies check at plan.
func CheckLiteralConstraints(f *lang.File, libs map[string]*Library) *lang.ErrorList {
	errs := lang.NewErrorList(0)
	dag := BuildDAG(f, libs)
	scopes := map[string]map[string]*Library{"": libs}
	for _, n := range dag.Nodes {
		if n.Kind == NodeComposite {
			scopes[n.Address] = n.Libraries
		}
	}
	for _, n := range dag.Nodes {
		switch n.Kind {
		case NodeResource, NodeData, NodeAction:
		default:
			continue
		}
		lib := scopes[n.Composite][n.Alias]
		if lib == nil || lib.Schema == nil {
			continue
		}
		schema := lib.Schema.typeSchema(n.Kind, n.Type)
		if schema == nil || len(schema.Constraints) == 0 {
			continue
		}
		values, ok := literalValues(n.Body)
		if !ok {
			continue
		}
		pos := n.Body.Span().Start
		entries, perr := lang.ParseSpecs(schema.Constraints)
		for _, e := range perr.Errors() {
			errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, e.Msg)
		}
		eval := func(ex lang.Expr) (any, error) {
			v, err := Eval(ex, &EvalContext{Vars: values})
			if errors.Is(err, ErrEvalNotFound) {
				return nil, nil
			}
			return v, err
		}
		for _, e := range lang.CheckConstraintEntries(entries, values, eval).Errors() {
			errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, e.Msg)
		}
	}
	return errs
}

// literalValues evaluates every non-meta field of a node body with an
// empty context. It returns the field map and true only when every field
// reduces without reading an input or another node's output; otherwise it
// returns false so the caller defers the node to plan.
func literalValues(body lang.Expr) (map[string]any, bool) {
	obj, ok := body.(*lang.ObjectLit)
	if !ok {
		return nil, false
	}
	out := make(map[string]any, len(obj.Fields))
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		val, err := Eval(fld.Value, &EvalContext{})
		if err != nil {
			return nil, false
		}
		out[fld.Key.Name] = val
	}
	return out, true
}

// typeSchema returns the schema for a node kind's type, or nil when the
// kind is not a library category or the type is absent.
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
