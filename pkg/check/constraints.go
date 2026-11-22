package check

import (
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/runtime"
	"github.com/cloudboss/unobin/pkg/typecheck"
)

// LiteralConstraints reports cross-field violations that can be decided
// without stack inputs or upstream outputs. Missing required inputs are
// left to the type checker.
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
		values, err := runtime.LiteralConstraintInputs(n.Body, schema.Defaults)
		if err != nil {
			errs.Addf(lang.ErrSchema, n.Body.Span().Start, "%s: %v", n.Address, err)
			continue
		}
		if values == nil {
			continue
		}
		deferMissingRequiredInputs(values, schema)
		entries, parseErrs := lang.ParseSpecs(schema.Constraints)
		addLiteralConstraintErrors(errs, n, parseErrs)
		eval := runtime.ConstraintEvaluator(runtime.EvalContext{Inputs: values})
		for i, entry := range entries {
			checked := lang.CheckPartialConstraintEntry(
				i, entry, values, eval, lang.DisplayNodeRelative, runtime.IsPendingValue,
			)
			addLiteralConstraintErrors(errs, n, checked)
		}
	}
	return errs
}

func checkCompositeLiteralConstraints(errs *lang.ErrorList, n *runtime.Node) {
	constraints := n.CompositeSyntaxBody.Constraints
	if len(constraints) == 0 {
		return
	}
	values, err := runtime.LiteralConstraintInputs(n.Body, nil)
	if err != nil {
		errs.Addf(lang.ErrSchema, n.Body.Span().Start, "%s: %v", n.Address, err)
		return
	}
	if values == nil {
		return
	}
	for _, input := range n.CompositeSyntaxBody.Inputs {
		name := input.Name.Name
		if _, ok := values[name]; ok {
			continue
		}
		_, optional, _ := syntaxInputType(input)
		if !optional {
			values[name] = runtime.PendingValue{}
		}
	}
	elements := make([]lang.Expr, len(constraints))
	for i, constraint := range constraints {
		elements[i] = constraint.Value
	}
	eval := runtime.ConstraintEvaluator(runtime.EvalContext{Inputs: values, Libraries: n.Libraries})
	checked := lang.CheckPartialConstraints(
		&lang.ArrayLit{Elements: elements}, values, eval, lang.DisplayNodeRelative,
		runtime.IsPendingValue,
	)
	addLiteralConstraintErrors(errs, n, checked)
}

func addLiteralConstraintErrors(errs *lang.ErrorList, n *runtime.Node, details *lang.ErrorList) {
	pos := n.Body.Span().Start
	for _, err := range details.Errors() {
		errs.Addf(lang.ErrSchema, pos, "%s: %s", n.Address, err.Msg)
	}
}

func deferMissingRequiredInputs(values map[string]any, schema *runtime.TypeSchema) {
	optionalDefaults := topLevelOptionalDefaults(schema.Defaults)
	for name, t := range schema.Inputs {
		if t.Kind == typecheck.Unknown || t.Kind == typecheck.Optional || optionalDefaults[name] {
			continue
		}
		if _, ok := values[name]; !ok {
			values[name] = runtime.PendingValue{}
		}
	}
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
