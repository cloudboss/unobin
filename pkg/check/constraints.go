package check

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// LiteralConstraints reports cross-field constraint violations that
// are decidable at compile time. It evaluates each library node's
// fields with no inputs or upstream outputs in scope, applies literal
// defaults, and checks constraints whose referenced roots are known and
// valid at compile time. Optional absent fields read as null; missing
// required roots are left to the required-presence diagnostics. Go nodes
// read constraints from their schemas; composite nodes read them from
// their imported syntax bodies.
func (c *Checker) LiteralConstraints() *lang.ErrorList {
	errs := lang.NewErrorList(0)
	for _, n := range c.dag.Nodes {
		if n.IsComposite() {
			checkCompositeLiteralConstraints(errs, n)
			continue
		}
		switch n.Kind {
		case runtime.NodeResource, runtime.NodeDataSource, runtime.NodeAction:
		default:
			continue
		}
		lib := c.libraries[n.Composite][n.Alias]
		if lib == nil || lib.Schema == nil {
			continue
		}
		schema := lib.Schema.ForType(n.Kind, n.Type)
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
		if err := overlayLiteralConstraintDefaults(values, schema.Defaults, deferred); err != nil {
			errs.Addf(lang.ErrSchema, pos, "%s: %v", n.Address, err)
			continue
		}
		skip := literalConstraintSkipRoots(values, deferred, schema)
		eval := func(ex lang.Expr, binds []lang.EachBinding) (any, error) {
			ctx := &runtime.EvalContext{Inputs: values, MissingAsNull: true}
			runtime.ApplyBindings(ctx, binds)
			v, err := runtime.Eval(ex, ctx)
			if errors.Is(err, runtime.ErrEvalNotFound) {
				return nil, nil
			}
			return v, err
		}
		for i, c := range entries {
			if c.ReadsAny(skip) {
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

func checkCompositeLiteralConstraints(errs *lang.ErrorList, n *runtime.Node) {
	constraints := n.CompositeSyntaxBody.Constraints
	if len(constraints) == 0 {
		return
	}
	values, deferred, ok := literalValues(n.Body)
	if !ok {
		return
	}
	skip := compositeLiteralConstraintSkipRoots(
		values,
		deferred,
		n.CompositeSyntaxBody.Inputs,
	)
	elements := make([]lang.Expr, len(constraints))
	for i, constraint := range constraints {
		if constraintReadsAny(constraint.Value, skip) {
			continue
		}
		elements[i] = constraint.Value
	}
	eval := func(ex lang.Expr, binds []lang.EachBinding) (any, error) {
		ctx := &runtime.EvalContext{
			Inputs:        values,
			Libraries:     n.Libraries,
			MissingAsNull: true,
		}
		runtime.ApplyBindings(ctx, binds)
		v, err := runtime.Eval(ex, ctx)
		if errors.Is(err, runtime.ErrEvalNotFound) {
			return nil, nil
		}
		return v, err
	}
	checked := lang.CheckConstraints(
		&lang.ArrayLit{Elements: elements},
		values,
		eval,
		lang.DisplayNodeRelative,
	)
	pos := n.Body.Span().Start
	for _, err := range checked.Errors() {
		errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, err.Msg)
	}
}

func compositeLiteralConstraintSkipRoots(
	values map[string]any,
	deferred map[string]bool,
	inputs []syntax.InputDecl,
) map[string]bool {
	skip := make(map[string]bool, len(deferred)+len(inputs))
	for name := range deferred {
		skip[name] = true
	}
	for _, input := range inputs {
		name := input.Name.Name
		if _, ok := values[name]; ok {
			continue
		}
		_, optional, _ := syntaxInputType(input)
		if !optional {
			skip[name] = true
		}
	}
	return skip
}

func constraintReadsAny(expr lang.Expr, names map[string]bool) bool {
	read := false
	lang.Walk(expr, func(e lang.Expr) {
		path, ok := e.(*lang.DotPath)
		if !ok || path.Root == nil || path.Root.Name != "input" || len(path.Segments) == 0 {
			return
		}
		if names[path.Segments[0].Name] {
			read = true
		}
	})
	return read
}

func overlayLiteralConstraintDefaults(
	values map[string]any,
	specs []lang.DefaultSpec,
	deferred map[string]bool,
) error {
	for _, s := range specs {
		if s.Optional {
			continue
		}
		path, ok := strings.CutPrefix(s.Field, "input.")
		if !ok {
			continue
		}
		segments := strings.Split(path, ".")
		if deferred[segments[0]] {
			continue
		}
		target := values
		for _, parent := range segments[:len(segments)-1] {
			child, ok := target[parent].(map[string]any)
			if !ok {
				target = nil
				break
			}
			target = child
		}
		if target == nil {
			continue
		}
		leaf := segments[len(segments)-1]
		if _, ok := target[leaf]; ok {
			continue
		}
		val, err := evalLiteralConstraintDefault(path, s.Value)
		if err != nil {
			return err
		}
		target[leaf] = val
	}
	return nil
}

func evalLiteralConstraintDefault(field, src string) (any, error) {
	expr, err := lang.ParseExpr("default", []byte(src))
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	v, err := runtime.Eval(expr, &runtime.EvalContext{})
	if err != nil {
		return nil, fmt.Errorf("default for %q: %v", field, err)
	}
	return v, nil
}

func literalConstraintSkipRoots(
	values map[string]any,
	deferred map[string]bool,
	schema *runtime.TypeSchema,
) map[string]bool {
	skip := make(map[string]bool, len(deferred)+len(schema.Inputs))
	for name := range deferred {
		skip[name] = true
	}
	optionalDefaults := topLevelOptionalDefaults(schema.Defaults)
	for name, t := range schema.Inputs {
		if t.Kind == typecheck.Unknown || t.Kind == typecheck.Optional {
			continue
		}
		if _, ok := values[name]; ok {
			continue
		}
		if optionalDefaults[name] {
			continue
		}
		skip[name] = true
	}
	return skip
}

func topLevelOptionalDefaults(specs []lang.DefaultSpec) map[string]bool {
	out := map[string]bool{}
	for _, s := range specs {
		if !s.Optional {
			continue
		}
		path, ok := strings.CutPrefix(s.Field, "input.")
		if !ok || strings.Contains(path, ".") {
			continue
		}
		out[path] = true
	}
	return out
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
		val, err := runtime.Eval(fld.Value, &runtime.EvalContext{})
		if err != nil {
			deferred[fld.Key.Name] = true
			continue
		}
		values[fld.Key.Name] = val
	}
	return values, deferred, true
}
